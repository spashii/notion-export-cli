package exporter

import "time"

type Manifest struct {
	StartedAt   time.Time            `json:"started_at"`
	FinishedAt  time.Time            `json:"finished_at"`
	Roots       []ManifestRoot       `json:"roots"`
	Pages       []ManifestPage       `json:"pages"`
	Databases   []ManifestDatabase   `json:"databases"`
	DataSources []ManifestDataSource `json:"data_sources"`
}

type ManifestRoot struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	Path  string `json:"path,omitempty"`
}

type ManifestPage struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Path           string `json:"path"`
	URL            string `json:"url,omitempty"`
	CreatedTime    string `json:"created_time,omitempty"`
	LastEditedTime string `json:"last_edited_time,omitempty"`
}

type ManifestDatabase struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Path  string `json:"path"`
	URL   string `json:"url,omitempty"`
}

type ManifestDataSource struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Path  string `json:"path"`
	URL   string `json:"url,omitempty"`
}

type Coverage struct {
	PagesExported       int               `json:"pages_exported"`
	DatabasesExported   int               `json:"databases_exported"`
	DataSourcesExported int               `json:"data_sources_exported"`
	AssetsExported      int               `json:"assets_exported,omitempty"`
	AssetsDownloaded    int               `json:"assets_downloaded,omitempty"`
	AssetsReused        int               `json:"assets_reused,omitempty"`
	AssetsFailed        int               `json:"assets_failed,omitempty"`
	SkippedDuplicates   int               `json:"skipped_duplicates"`
	UnknownBlocks       []UnknownBlock    `json:"unknown_blocks,omitempty"`
	Failures            []CoverageFailure `json:"failures,omitempty"`
}

type UnknownBlock struct {
	PageID  string `json:"page_id"`
	BlockID string `json:"block_id"`
	Reason  string `json:"reason"`
}

type CoverageFailure struct {
	Stage string `json:"stage"`
	ID    string `json:"id,omitempty"`
	Error string `json:"error"`
}

type AssetRecord struct {
	SourceHash   string `json:"source_hash"`
	CanonicalURL string `json:"canonical_url"`
	Path         string `json:"path"`
	ContentType  string `json:"content_type,omitempty"`
	Size         int64  `json:"size,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
	DownloadedAt string `json:"downloaded_at,omitempty"`
	LastSeenAt   string `json:"last_seen_at,omitempty"`
}
