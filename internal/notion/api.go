package notion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type Parent struct {
	Type         string `json:"type"`
	PageID       string `json:"page_id,omitempty"`
	DatabaseID   string `json:"database_id,omitempty"`
	DataSourceID string `json:"data_source_id,omitempty"`
	BlockID      string `json:"block_id,omitempty"`
	Workspace    bool   `json:"workspace,omitempty"`
}

type RichText struct {
	PlainText string `json:"plain_text"`
}

type Option struct {
	Name string `json:"name"`
}

type PageProperty struct {
	ID          string     `json:"id"`
	Type        string     `json:"type"`
	Title       []RichText `json:"title,omitempty"`
	RichText    []RichText `json:"rich_text,omitempty"`
	Select      *Option    `json:"select,omitempty"`
	MultiSelect []Option   `json:"multi_select,omitempty"`
	Status      *Option    `json:"status,omitempty"`
}

type Page struct {
	Object         string                  `json:"object"`
	ID             string                  `json:"id"`
	CreatedTime    string                  `json:"created_time"`
	LastEditedTime string                  `json:"last_edited_time"`
	URL            string                  `json:"url"`
	Parent         Parent                  `json:"parent"`
	Properties     map[string]PageProperty `json:"properties"`
}

func (p Page) Title() string {
	for _, property := range p.Properties {
		if property.Type == "title" {
			return richTextPlain(property.Title)
		}
	}
	return ""
}

type PageMarkdown struct {
	Object          string   `json:"object"`
	ID              string   `json:"id"`
	Markdown        string   `json:"markdown"`
	Truncated       bool     `json:"truncated"`
	UnknownBlockIDs []string `json:"unknown_block_ids"`
}

type Block struct {
	Object      string `json:"object"`
	ID          string `json:"id"`
	Type        string `json:"type"`
	HasChildren bool   `json:"has_children"`
	ChildPage   struct {
		Title string `json:"title"`
	} `json:"child_page,omitempty"`
	ChildDatabase struct {
		Title string `json:"title"`
	} `json:"child_database,omitempty"`
}

type Database struct {
	Object      string     `json:"object"`
	ID          string     `json:"id"`
	Title       []RichText `json:"title"`
	URL         string     `json:"url"`
	Parent      Parent     `json:"parent"`
	DataSources []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"data_sources"`
}

func (d Database) DisplayTitle() string {
	if title := richTextPlain(d.Title); title != "" {
		return title
	}
	return "Untitled database"
}

type DataSource struct {
	Object         string     `json:"object"`
	ID             string     `json:"id"`
	Title          []RichText `json:"title"`
	URL            string     `json:"url"`
	Parent         Parent     `json:"parent"`
	DatabaseParent Parent     `json:"database_parent"`
}

type View struct {
	Object       string `json:"object"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	DataSourceID string `json:"data_source_id"`
	URL          string `json:"url"`
}

func (d DataSource) DisplayTitle() string {
	if title := richTextPlain(d.Title); title != "" {
		return title
	}
	return "Untitled data source"
}

type SearchResult struct {
	Object         string                     `json:"object"`
	ID             string                     `json:"id"`
	Parent         Parent                     `json:"parent"`
	DatabaseParent Parent                     `json:"database_parent"`
	URL            string                     `json:"url"`
	Properties     map[string]json.RawMessage `json:"properties"`
	Title          []RichText                 `json:"title"`
}

func (s SearchResult) DisplayTitle() string {
	switch s.Object {
	case "page":
		for _, raw := range s.Properties {
			var property PageProperty
			if err := json.Unmarshal(raw, &property); err != nil {
				continue
			}
			if property.Type == "title" {
				return richTextPlain(property.Title)
			}
		}
		return ""
	case "data_source":
		return richTextPlain(s.Title)
	default:
		return ""
	}
}

type paginatedBlocks struct {
	Results    []Block `json:"results"`
	HasMore    bool    `json:"has_more"`
	NextCursor string  `json:"next_cursor"`
}

type paginatedPages struct {
	Results    []Page `json:"results"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor"`
}

type paginatedSearch struct {
	Results    []SearchResult `json:"results"`
	HasMore    bool           `json:"has_more"`
	NextCursor string         `json:"next_cursor"`
}

type paginatedViews struct {
	Results    []View `json:"results"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor"`
}

func (c *Client) RetrievePage(ctx context.Context, id string) (*Page, error) {
	var page Page
	if err := c.Do(ctx, http.MethodGet, "/v1/pages/"+id, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func (c *Client) RetrievePageMarkdown(ctx context.Context, id string) (*PageMarkdown, error) {
	var markdown PageMarkdown
	if err := c.Do(ctx, http.MethodGet, "/v1/pages/"+id+"/markdown", nil, &markdown); err != nil {
		return nil, err
	}
	return &markdown, nil
}

func (c *Client) ListBlockChildren(ctx context.Context, id string) ([]Block, error) {
	var blocks []Block
	var cursor string
	for {
		path := "/v1/blocks/" + id + "/children?page_size=100"
		if cursor != "" {
			path += "&start_cursor=" + url.QueryEscape(cursor)
		}

		var page paginatedBlocks
		if err := c.Do(ctx, http.MethodGet, path, nil, &page); err != nil {
			return nil, err
		}
		blocks = append(blocks, page.Results...)
		if !page.HasMore {
			return blocks, nil
		}
		cursor = page.NextCursor
	}
}

func (c *Client) RetrieveDatabaseRaw(ctx context.Context, id string) (*Database, []byte, error) {
	raw, err := c.DoRaw(ctx, http.MethodGet, "/v1/databases/"+id, nil)
	if err != nil {
		return nil, nil, err
	}
	var database Database
	if err := decode(raw, &database); err != nil {
		return nil, nil, err
	}
	return &database, raw, nil
}

func (c *Client) RetrieveDataSourceRaw(ctx context.Context, id string) (*DataSource, []byte, error) {
	raw, err := c.DoRaw(ctx, http.MethodGet, "/v1/data_sources/"+id, nil)
	if err != nil {
		return nil, nil, err
	}
	var dataSource DataSource
	if err := decode(raw, &dataSource); err != nil {
		return nil, nil, err
	}
	return &dataSource, raw, nil
}

func (c *Client) ListViewsByDatabase(ctx context.Context, databaseID string) ([]View, error) {
	var views []View
	var cursor string
	for {
		path := "/v1/views?database_id=" + url.QueryEscape(databaseID) + "&page_size=100"
		if cursor != "" {
			path += "&start_cursor=" + url.QueryEscape(cursor)
		}

		var page paginatedViews
		if err := c.Do(ctx, http.MethodGet, path, nil, &page); err != nil {
			return nil, err
		}

		for _, ref := range page.Results {
			view, err := c.RetrieveView(ctx, ref.ID)
			if err != nil {
				return nil, err
			}
			views = append(views, *view)
		}

		if !page.HasMore {
			return views, nil
		}
		cursor = page.NextCursor
	}
}

func (c *Client) RetrieveView(ctx context.Context, id string) (*View, error) {
	var view View
	if err := c.Do(ctx, http.MethodGet, "/v1/views/"+id, nil, &view); err != nil {
		return nil, err
	}
	return &view, nil
}

func (c *Client) QueryDataSource(ctx context.Context, id string) ([]Page, error) {
	var pages []Page
	var cursor string
	for {
		body := map[string]any{"page_size": 100}
		if cursor != "" {
			body["start_cursor"] = cursor
		}

		var page paginatedPages
		if err := c.Do(ctx, http.MethodPost, "/v1/data_sources/"+id+"/query", body, &page); err != nil {
			return nil, err
		}
		pages = append(pages, page.Results...)
		if !page.HasMore {
			return pages, nil
		}
		cursor = page.NextCursor
	}
}

func (c *Client) QueryDatabase(ctx context.Context, id string) ([]Page, error) {
	var pages []Page
	var cursor string
	for {
		body := map[string]any{"page_size": 100}
		if cursor != "" {
			body["start_cursor"] = cursor
		}

		var page paginatedPages
		raw, err := c.DoRawVersion(ctx, http.MethodPost, "/v1/databases/"+id+"/query", body, LegacyDatabaseVersion)
		if err != nil {
			return nil, err
		}
		if err := decode(raw, &page); err != nil {
			return nil, err
		}
		pages = append(pages, page.Results...)
		if !page.HasMore {
			return pages, nil
		}
		cursor = page.NextCursor
	}
}

func (c *Client) Search(ctx context.Context, objectType string) ([]SearchResult, error) {
	var results []SearchResult
	var cursor string
	for {
		body := map[string]any{
			"page_size": 100,
			"sort": map[string]string{
				"timestamp": "last_edited_time",
				"direction": "descending",
			},
		}
		if objectType != "" {
			body["filter"] = map[string]string{"property": "object", "value": objectType}
		}
		if cursor != "" {
			body["start_cursor"] = cursor
		}

		var page paginatedSearch
		if err := c.Do(ctx, http.MethodPost, "/v1/search", body, &page); err != nil {
			return nil, err
		}
		results = append(results, page.Results...)
		if !page.HasMore {
			return results, nil
		}
		cursor = page.NextCursor
	}
}

func richTextPlain(values []RichText) string {
	var builder strings.Builder
	for _, value := range values {
		builder.WriteString(value.PlainText)
	}
	return strings.TrimSpace(builder.String())
}

func decode(data []byte, out any) error {
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode notion response: %w", err)
	}
	return nil
}
