package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const DefaultBaseURL = "https://api.notion.com"
const DefaultVersion = "2026-03-11"
const LegacyDatabaseVersion = "2022-06-28"

// Notion documents an average limit of 3 requests/second per connection.
// The default stays below that to leave headroom for clock jitter and retries.
const DocumentedAverageRPS = 3.0
const DefaultRPS = 2.5

const statusServiceOverload = 529

type Config struct {
	Token         string
	BaseURL       string
	NotionVersion string
	RPS           float64
	HTTPClient    *http.Client
}

type Client struct {
	token         string
	baseURL       string
	notionVersion string
	httpClient    *http.Client
	limiter       *rate.Limiter
}

type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e APIError) Error() string {
	if e.Code == "" && e.Message == "" {
		return fmt.Sprintf("notion api error: HTTP %d", e.StatusCode)
	}
	if e.Code == "" {
		return fmt.Sprintf("notion api error: HTTP %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("notion api error: HTTP %d %s: %s", e.StatusCode, e.Code, e.Message)
}

type User struct {
	Object string `json:"object"`
	ID     string `json:"id"`
	Type   string `json:"type"`
	Name   string `json:"name"`
	Bot    *struct {
		Owner struct {
			Type string `json:"type"`
		} `json:"owner"`
		WorkspaceName string `json:"workspace_name"`
	} `json:"bot,omitempty"`
	Person *struct {
		Email string `json:"email"`
	} `json:"person,omitempty"`
}

func (u User) DisplayName() string {
	if strings.TrimSpace(u.Name) != "" {
		return u.Name
	}
	if u.Type != "" {
		return u.Type
	}
	return "unknown"
}

func (u User) SubjectType() string {
	if u.Type != "" {
		if u.Type == "bot" && u.Bot != nil && u.Bot.Owner.Type != "" {
			return "bot (" + u.Bot.Owner.Type + "-owned)"
		}
		return u.Type
	}
	if u.Bot != nil {
		return "bot"
	}
	if u.Person != nil {
		return "person"
	}
	return "unknown"
}

func (u User) IsBot() bool {
	return u.Type == "bot" || u.Bot != nil
}

func (u User) IsWorkspaceBot() bool {
	return u.IsBot() && u.Bot != nil && u.Bot.Owner.Type == "workspace"
}

func NewClient(cfg Config) *Client {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	version := cfg.NotionVersion
	if version == "" {
		version = DefaultVersion
	}
	rps := cfg.RPS
	if rps <= 0 {
		rps = DefaultRPS
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}

	return &Client{
		token:         cfg.Token,
		baseURL:       baseURL,
		notionVersion: version,
		httpClient:    httpClient,
		limiter:       rate.NewLimiter(rate.Limit(rps), 1),
	}
}

func (c *Client) Verify(ctx context.Context) (*User, error) {
	var user User
	if err := c.Do(ctx, http.MethodGet, "/v1/users/me", nil, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

func (c *Client) Do(ctx context.Context, method, path string, body any, out any) error {
	data, err := c.DoRaw(ctx, method, path, body)
	if err != nil {
		return err
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

func (c *Client) DoRaw(ctx context.Context, method, path string, body any) ([]byte, error) {
	return c.doRaw(ctx, method, path, body, c.notionVersion)
}

func (c *Client) DoRawVersion(ctx context.Context, method, path string, body any, notionVersion string) ([]byte, error) {
	if notionVersion == "" {
		notionVersion = c.notionVersion
	}
	return c.doRaw(ctx, method, path, body, notionVersion)
}

func (c *Client) doRaw(ctx context.Context, method, path string, body any, notionVersion string) ([]byte, error) {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}

	for attempt := 0; attempt < 6; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}

		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(encoded)
		}

		req, err := http.NewRequestWithContext(ctx, method, c.url(path), reader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Notion-Version", notionVersion)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if attempt == 5 || ctx.Err() != nil {
				return nil, err
			}
			if err := sleep(ctx, backoff(attempt)); err != nil {
				return nil, err
			}
			continue
		}

		data, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return data, nil
		}

		apiErr := decodeAPIError(resp.StatusCode, data)
		if shouldRetry(resp.StatusCode) && attempt < 5 {
			wait := retryAfter(resp.Header.Get("Retry-After"))
			if wait == 0 {
				wait = backoff(attempt)
			}
			if err := sleep(ctx, wait); err != nil {
				return nil, err
			}
			continue
		}

		return nil, apiErr
	}

	return nil, fmt.Errorf("notion api request exhausted retries")
}

func (c *Client) url(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.baseURL + path
}

func decodeAPIError(statusCode int, data []byte) APIError {
	var payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(data, &payload)
	if payload.Message == "" {
		payload.Message = strings.TrimSpace(string(data))
	}
	return APIError{StatusCode: statusCode, Code: payload.Code, Message: payload.Message}
}

func shouldRetry(statusCode int) bool {
	switch statusCode {
	case http.StatusTooManyRequests, http.StatusConflict, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, statusServiceOverload:
		return true
	default:
		return statusCode >= 500
	}
}

func retryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func backoff(attempt int) time.Duration {
	d := time.Duration(1<<attempt) * time.Second
	if d > 30*time.Second {
		return 30 * time.Second
	}
	return d
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
