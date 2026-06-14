package exporter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

const assetsDirName = "extracted_assets"
const assetsIndexName = "_assets.json"

var assetURLPattern = regexp.MustCompile(`(?i)(?:https?://|file://)[^\s<>"')]+`)

type assetManager struct {
	outputDir string
	assetsDir string
	client    *http.Client
	coverage  *Coverage
	records   map[string]*AssetRecord
	seen      map[string]bool
}

func newAssetManager(outputDir string, coverage *Coverage) *assetManager {
	return &assetManager{
		outputDir: outputDir,
		assetsDir: filepath.Join(outputDir, assetsDirName),
		client:    &http.Client{Timeout: 2 * time.Minute},
		coverage:  coverage,
		records:   map[string]*AssetRecord{},
		seen:      map[string]bool{},
	}
}

func (m *assetManager) load() error {
	path := filepath.Join(m.outputDir, assetsIndexName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var records []AssetRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return err
	}
	for i := range records {
		record := records[i]
		if record.SourceHash == "" || record.Path == "" {
			continue
		}
		m.records[record.SourceHash] = &record
	}
	return nil
}

func (m *assetManager) writeIndex() error {
	if len(m.records) == 0 {
		return nil
	}
	return writeJSON(filepath.Join(m.outputDir, assetsIndexName), m.recordsList())
}

func (m *assetManager) recordsList() []AssetRecord {
	records := make([]AssetRecord, 0, len(m.records))
	for _, record := range m.records {
		records = append(records, *record)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].SourceHash < records[j].SourceHash
	})
	return records
}

func (m *assetManager) rewriteAndFetch(ctx context.Context, markdownPath, content string) string {
	return rewriteAssetURLs(content, func(raw string) (string, bool) {
		return m.resolve(ctx, markdownPath, raw)
	})
}

func rewriteAssetURLs(content string, replace func(raw string) (string, bool)) string {
	return assetURLPattern.ReplaceAllStringFunc(content, func(raw string) string {
		if replacement, ok := replace(raw); ok {
			return replacement
		}
		return raw
	})
}

func (m *assetManager) resolve(ctx context.Context, markdownPath, raw string) (string, bool) {
	downloadURL, canonicalURL, ok := canonicalAssetURL(raw)
	if !ok {
		return "", false
	}

	sourceHash := sha256Hex(canonicalURL)
	record := m.records[sourceHash]
	if record == nil {
		record = m.findExistingAsset(sourceHash, canonicalURL)
	}

	alreadySeen := m.seen[sourceHash]
	if record != nil && m.recordValid(record) {
		m.records[sourceHash] = record
		m.markSeen(record, alreadySeen)
		return m.relativeAssetPath(markdownPath, record.Path), true
	}

	record, err := m.download(ctx, sourceHash, canonicalURL, downloadURL, record)
	if err != nil {
		m.coverage.AssetsFailed++
		m.coverage.Failures = append(m.coverage.Failures, CoverageFailure{Stage: "download_asset", ID: sourceHash, Error: err.Error()})
		return "", false
	}

	m.records[sourceHash] = record
	if !alreadySeen {
		m.coverage.AssetsExported++
		m.coverage.AssetsDownloaded++
	}
	m.seen[sourceHash] = true
	return m.relativeAssetPath(markdownPath, record.Path), true
}

func (m *assetManager) markSeen(record *AssetRecord, alreadySeen bool) {
	now := time.Now().UTC().Format(time.RFC3339)
	record.LastSeenAt = now
	if !alreadySeen {
		m.coverage.AssetsExported++
		m.coverage.AssetsReused++
	}
	m.seen[record.SourceHash] = true
}

func (m *assetManager) relativeAssetPath(markdownPath, assetRelPath string) string {
	assetPath := filepath.Join(m.outputDir, filepath.FromSlash(assetRelPath))
	rel, err := filepath.Rel(filepath.Dir(markdownPath), assetPath)
	if err != nil {
		return filepath.ToSlash(assetRelPath)
	}
	return filepath.ToSlash(rel)
}

func (m *assetManager) recordValid(record *AssetRecord) bool {
	assetPath := filepath.Join(m.outputDir, filepath.FromSlash(record.Path))
	info, err := os.Stat(assetPath)
	if err != nil || info.IsDir() {
		return false
	}
	if record.Size > 0 && info.Size() != record.Size {
		return false
	}
	if record.SHA256 == "" {
		return true
	}
	sha, err := fileSHA256(assetPath)
	return err == nil && sha == record.SHA256
}

func (m *assetManager) findExistingAsset(sourceHash, canonicalURL string) *AssetRecord {
	entries, err := os.ReadDir(filepath.Join(m.assetsDir, sourceHash))
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		assetPath := filepath.Join(m.assetsDir, sourceHash, entry.Name())
		info, err := os.Stat(assetPath)
		if err != nil || info.IsDir() {
			continue
		}
		sha, err := fileSHA256(assetPath)
		if err != nil {
			continue
		}
		return &AssetRecord{
			SourceHash:   sourceHash,
			CanonicalURL: canonicalURL,
			Path:         filepath.ToSlash(filepath.Join(assetsDirName, sourceHash, entry.Name())),
			Size:         info.Size(),
			SHA256:       sha,
		}
	}
	return nil
}

func (m *assetManager) download(ctx context.Context, sourceHash, canonicalURL, downloadURL string, previous *AssetRecord) (*AssetRecord, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "notion-export-cli")

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	contentType := cleanContentType(resp.Header.Get("Content-Type"))
	filename := ""
	if previous != nil && previous.Path != "" {
		filename = filepath.Base(filepath.FromSlash(previous.Path))
	}
	if filename == "" || filename == "." || filename == string(os.PathSeparator) {
		filename = assetFilename(canonicalURL, contentType)
	}

	dir := filepath.Join(m.assetsDir, sourceHash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	tmp, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	h := sha256.New()
	size, copyErr := copyAndHash(tmp, resp.Body, h)
	closeErr := tmp.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	if closeErr != nil {
		return nil, closeErr
	}

	assetPath := filepath.Join(dir, filename)
	if err := os.Rename(tmpPath, assetPath); err != nil {
		if removeErr := os.Remove(assetPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return nil, err
		}
		if renameErr := os.Rename(tmpPath, assetPath); renameErr != nil {
			return nil, renameErr
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	return &AssetRecord{
		SourceHash:   sourceHash,
		CanonicalURL: canonicalURL,
		Path:         filepath.ToSlash(filepath.Join(assetsDirName, sourceHash, filename)),
		ContentType:  contentType,
		Size:         size,
		SHA256:       hex.EncodeToString(h.Sum(nil)),
		DownloadedAt: now,
		LastSeenAt:   now,
	}, nil
}

func copyAndHash(dst io.Writer, src io.Reader, h hash.Hash) (int64, error) {
	return io.Copy(io.MultiWriter(dst, h), src)
}

func canonicalAssetURL(raw string) (downloadURL, canonicalURL string, ok bool) {
	value := strings.TrimSpace(html.UnescapeString(raw))
	if source, unwrapped := unwrapFileSource(value); unwrapped {
		value = source
	}

	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || !isNotionAssetURL(parsed) {
		return "", "", false
	}

	canonical := *parsed
	canonical.RawQuery = ""
	canonical.ForceQuery = false
	canonical.Fragment = ""
	canonical.RawFragment = ""
	canonical.User = nil
	return parsed.String(), canonical.String(), true
}

func unwrapFileSource(raw string) (string, bool) {
	if !strings.HasPrefix(strings.ToLower(raw), "file://") {
		return "", false
	}
	encoded := raw[len("file://"):]
	decoded, err := url.PathUnescape(encoded)
	if err != nil {
		decoded, err = url.QueryUnescape(encoded)
	}
	if err != nil {
		return "", false
	}

	var wrapper struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal([]byte(decoded), &wrapper); err == nil && strings.TrimSpace(wrapper.Source) != "" {
		return strings.TrimSpace(wrapper.Source), true
	}
	if strings.HasPrefix(decoded, "http://") || strings.HasPrefix(decoded, "https://") {
		return decoded, true
	}
	return "", false
}

func isNotionAssetURL(value *url.URL) bool {
	host := strings.ToLower(value.Hostname())
	escapedPath := strings.ToLower(value.EscapedPath())
	if host == "secure.notion-static.com" || host == "file.notion.so" || strings.HasPrefix(host, "prod-files-secure.s3.") && strings.HasSuffix(host, ".amazonaws.com") {
		return true
	}
	return strings.HasSuffix(host, ".amazonaws.com") && strings.Contains(escapedPath, "secure.notion-static.com")
}

func assetFilename(canonicalURL, contentType string) string {
	name := "asset"
	if parsed, err := url.Parse(canonicalURL); err == nil {
		base := path.Base(parsed.EscapedPath())
		if decoded, err := url.PathUnescape(base); err == nil && strings.TrimSpace(decoded) != "" && decoded != "." && decoded != "/" {
			name = decoded
		}
	}

	name = sanitizeAssetFilename(name)
	if filepath.Ext(name) == "" {
		name += extensionForContentType(contentType)
	}
	return name
}

func sanitizeAssetFilename(value string) string {
	value = sanitizeName(value)
	var builder strings.Builder
	lastWasDash := false
	for _, r := range value {
		allowed := unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-'
		switch {
		case allowed:
			builder.WriteRune(r)
			lastWasDash = false
		case !lastWasDash:
			builder.WriteRune('-')
			lastWasDash = true
		}
	}
	name := strings.Trim(builder.String(), ".-")
	if name == "" {
		return "asset"
	}
	return truncateRunes(name, 120)
}

func extensionForContentType(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/svg+xml":
		return ".svg"
	}
	if contentType == "" {
		return ""
	}
	extensions, err := mime.ExtensionsByType(contentType)
	if err != nil || len(extensions) == 0 {
		return ""
	}
	return extensions[0]
}

func cleanContentType(value string) string {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return strings.TrimSpace(value)
	}
	return mediaType
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
