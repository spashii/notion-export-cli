package exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spashii/notion-export-cli/internal/notion"
)

type Config struct {
	Client    *notion.Client
	OutputDir string
	Writer    io.Writer
}

type Exporter struct {
	client          *notion.Client
	outputDir       string
	writer          io.Writer
	manifest        Manifest
	coverage        Coverage
	visitedPages    map[string]string
	visitedDatabase map[string]string
	visitedSources  map[string]string
	usedNames       map[string]map[string]int
}

func New(cfg Config) *Exporter {
	writer := cfg.Writer
	if writer == nil {
		writer = io.Discard
	}
	return &Exporter{
		client:          cfg.Client,
		outputDir:       filepath.Clean(cfg.OutputDir),
		writer:          writer,
		manifest:        Manifest{StartedAt: time.Now().UTC(), Roots: []ManifestRoot{}, Pages: []ManifestPage{}, Databases: []ManifestDatabase{}, DataSources: []ManifestDataSource{}},
		visitedPages:    map[string]string{},
		visitedDatabase: map[string]string{},
		visitedSources:  map[string]string{},
		usedNames:       map[string]map[string]int{},
	}
}

func (e *Exporter) ExportRoot(ctx context.Context, input string) error {
	id, err := notion.ParseID(input)
	if err != nil {
		return err
	}
	if err := e.prepare(); err != nil {
		return err
	}

	if page, err := e.client.RetrievePage(ctx, id); err == nil {
		path, err := e.exportPage(ctx, page, e.outputDir, "", true)
		if err != nil {
			return err
		}
		e.manifest.Roots = append(e.manifest.Roots, ManifestRoot{Type: "page", ID: page.ID, Title: page.Title(), Path: e.relative(path)})
		return e.finish()
	}

	if database, raw, err := e.client.RetrieveDatabaseRaw(ctx, id); err == nil {
		path, err := e.exportDatabase(ctx, database, raw, e.outputDir, "")
		if err != nil {
			return err
		}
		e.manifest.Roots = append(e.manifest.Roots, ManifestRoot{Type: "database", ID: database.ID, Title: database.DisplayTitle(), Path: e.relative(path)})
		return e.finish()
	}

	if dataSource, raw, err := e.client.RetrieveDataSourceRaw(ctx, id); err == nil {
		path, err := e.exportDataSource(ctx, dataSource, raw, e.outputDir, "", true)
		if err != nil {
			return err
		}
		e.manifest.Roots = append(e.manifest.Roots, ManifestRoot{Type: "data_source", ID: dataSource.ID, Title: dataSource.DisplayTitle(), Path: e.relative(path)})
		return e.finish()
	}

	return fmt.Errorf("%s is not accessible as a page, database, or data source", id)
}

func (e *Exporter) ExportAll(ctx context.Context) error {
	if err := e.prepare(); err != nil {
		return err
	}

	pages, err := e.client.Search(ctx, "page")
	if err != nil {
		return err
	}
	dataSources, err := e.client.Search(ctx, "data_source")
	if err != nil {
		return err
	}

	pageIDs := map[string]bool{}
	for _, page := range pages {
		pageIDs[page.ID] = true
	}
	dataSourceIDs := map[string]bool{}
	for _, dataSource := range dataSources {
		dataSourceIDs[dataSource.ID] = true
	}

	roots := 0
	for _, result := range pages {
		if !isRootPage(result.Parent, pageIDs, dataSourceIDs) {
			continue
		}
		page, err := e.client.RetrievePage(ctx, result.ID)
		if err != nil {
			e.recordFailure("retrieve_page", result.ID, err)
			continue
		}
		path, err := e.exportPage(ctx, page, e.outputDir, result.DisplayTitle(), true)
		if err != nil {
			e.recordFailure("export_page", result.ID, err)
			continue
		}
		e.manifest.Roots = append(e.manifest.Roots, ManifestRoot{Type: "page", ID: result.ID, Title: result.DisplayTitle(), Path: e.relative(path)})
		roots++
	}

	for _, result := range dataSources {
		if result.DatabaseParent.PageID != "" && pageIDs[result.DatabaseParent.PageID] {
			continue
		}
		dataSource, raw, err := e.client.RetrieveDataSourceRaw(ctx, result.ID)
		if err != nil {
			e.recordFailure("retrieve_data_source", result.ID, err)
			continue
		}
		path, err := e.exportDataSource(ctx, dataSource, raw, e.outputDir, result.DisplayTitle(), true)
		if err != nil {
			e.recordFailure("export_data_source", result.ID, err)
			continue
		}
		e.manifest.Roots = append(e.manifest.Roots, ManifestRoot{Type: "data_source", ID: result.ID, Title: result.DisplayTitle(), Path: e.relative(path)})
		roots++
	}

	if roots == 0 {
		fmt.Fprintln(e.writer, "No root pages or data sources found; exporting accessible pages flat.")
		for _, result := range pages {
			page, err := e.client.RetrievePage(ctx, result.ID)
			if err != nil {
				e.recordFailure("retrieve_page", result.ID, err)
				continue
			}
			path, err := e.exportPage(ctx, page, e.outputDir, result.DisplayTitle(), false)
			if err != nil {
				e.recordFailure("export_page", result.ID, err)
				continue
			}
			e.manifest.Roots = append(e.manifest.Roots, ManifestRoot{Type: "page", ID: result.ID, Title: result.DisplayTitle(), Path: e.relative(path)})
		}
	}

	return e.finish()
}

func (e *Exporter) exportPage(ctx context.Context, page *notion.Page, parentDir, titleHint string, forceFolder bool) (string, error) {
	if path, ok := e.visitedPages[page.ID]; ok {
		e.coverage.SkippedDuplicates++
		return path, nil
	}

	title := strings.TrimSpace(page.Title())
	if title == "" {
		title = strings.TrimSpace(titleHint)
	}
	if title == "" {
		title = "Untitled page"
	}

	blocks, err := e.client.ListBlockChildren(ctx, page.ID)
	if err != nil {
		e.recordFailure("list_block_children", page.ID, err)
	}
	structuralChildren := childBlocks(blocks)

	markdown, err := e.client.RetrievePageMarkdown(ctx, page.ID)
	if err != nil {
		return "", err
	}
	content := e.pageMarkdownWithRecoveredUnknowns(ctx, page.ID, markdown)

	var pagePath string
	var markdownPath string
	if forceFolder || len(structuralChildren) > 0 {
		pagePath = filepath.Join(parentDir, e.reserveName(parentDir, title, page.ID, ""))
		markdownPath = filepath.Join(pagePath, "index.md")
		if err := os.MkdirAll(pagePath, 0o755); err != nil {
			return "", err
		}
	} else {
		markdownPath = filepath.Join(parentDir, e.reserveName(parentDir, title, page.ID, ".md"))
		pagePath = markdownPath
		if err := os.MkdirAll(parentDir, 0o755); err != nil {
			return "", err
		}
	}

	if err := writeFile(markdownPath, []byte(renderPageMarkdown(page, title, content))); err != nil {
		return "", err
	}

	e.visitedPages[page.ID] = pagePath
	e.coverage.PagesExported++
	e.manifest.Pages = append(e.manifest.Pages, ManifestPage{
		ID:             page.ID,
		Title:          title,
		Path:           e.relative(markdownPath),
		URL:            page.URL,
		CreatedTime:    page.CreatedTime,
		LastEditedTime: page.LastEditedTime,
	})
	fmt.Fprintf(e.writer, "exported page: %s\n", e.relative(markdownPath))

	for _, block := range structuralChildren {
		switch block.Type {
		case "child_page":
			childPage, err := e.client.RetrievePage(ctx, block.ID)
			if err != nil {
				e.recordFailure("retrieve_child_page", block.ID, err)
				continue
			}
			if _, err := e.exportPage(ctx, childPage, pageDirForChildren(pagePath), block.ChildPage.Title, false); err != nil {
				e.recordFailure("export_child_page", block.ID, err)
			}
		case "child_database":
			if database, raw, err := e.client.RetrieveDatabaseRaw(ctx, block.ID); err == nil {
				if _, err := e.exportDatabase(ctx, database, raw, pageDirForChildren(pagePath), block.ChildDatabase.Title); err != nil {
					e.recordFailure("export_child_database", block.ID, err)
				}
				continue
			}
			if dataSource, raw, err := e.client.RetrieveDataSourceRaw(ctx, block.ID); err == nil {
				if _, err := e.exportDataSource(ctx, dataSource, raw, pageDirForChildren(pagePath), block.ChildDatabase.Title, true); err != nil {
					e.recordFailure("export_child_data_source", block.ID, err)
				}
				continue
			}
			e.recordFailure("retrieve_child_database", block.ID, fmt.Errorf("block is not accessible as database or data source"))
		}
	}

	return pagePath, nil
}

func (e *Exporter) exportDatabase(ctx context.Context, database *notion.Database, raw []byte, parentDir, titleHint string) (string, error) {
	if path, ok := e.visitedDatabase[database.ID]; ok {
		e.coverage.SkippedDuplicates++
		return path, nil
	}

	var linkedViews []notion.View
	var linkedViewsErr error
	viewsLoaded := false
	if len(database.DataSources) == 0 {
		linkedViews, linkedViewsErr = e.client.ListViewsByDatabase(ctx, database.ID)
		viewsLoaded = true
	}

	title := strings.TrimSpace(database.DisplayTitle())
	if isUntitled(title, "database") {
		title = e.databaseFallbackTitle(ctx, linkedViews, titleHint)
	}
	dir := filepath.Join(parentDir, e.reserveName(parentDir, title, database.ID, ""))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := writeFile(filepath.Join(dir, "_database.json"), prettyJSON(raw)); err != nil {
		return "", err
	}

	e.visitedDatabase[database.ID] = dir
	e.coverage.DatabasesExported++
	e.manifest.Databases = append(e.manifest.Databases, ManifestDatabase{ID: database.ID, Title: title, Path: e.relative(dir), URL: database.URL})
	fmt.Fprintf(e.writer, "exported database: %s\n", e.relative(dir))

	for _, ref := range database.DataSources {
		dataSource, dataSourceRaw, err := e.client.RetrieveDataSourceRaw(ctx, ref.ID)
		if err != nil {
			e.recordFailure("retrieve_database_data_source", ref.ID, err)
			continue
		}
		useSubdir := len(database.DataSources) > 1
		if _, err := e.exportDataSource(ctx, dataSource, dataSourceRaw, dir, ref.Name, useSubdir); err != nil {
			e.recordFailure("export_database_data_source", ref.ID, err)
		}
	}

	if len(database.DataSources) == 0 {
		linkedDataSources := 0
		if linkedViewsErr != nil {
			e.recordFailure("list_database_views", database.ID, linkedViewsErr)
		} else if viewsLoaded && len(linkedViews) > 0 {
			if err := writeJSON(filepath.Join(dir, "_views.json"), linkedViews); err != nil {
				e.recordFailure("write_database_views", database.ID, err)
			}

			for _, view := range linkedViews {
				if view.DataSourceID == "" {
					continue
				}
				dataSource, dataSourceRaw, err := e.client.RetrieveDataSourceRaw(ctx, view.DataSourceID)
				if err != nil {
					e.recordFailure("retrieve_view_data_source", view.DataSourceID, err)
					continue
				}
				useSubdir := len(linkedViews) > 1
				if _, err := e.exportDataSource(ctx, dataSource, dataSourceRaw, dir, view.Name, useSubdir); err != nil {
					e.recordFailure("export_view_data_source", view.DataSourceID, err)
					continue
				}
				linkedDataSources++
			}
		}

		if linkedDataSources > 0 {
			return dir, nil
		}

		pages, err := e.client.QueryDatabase(ctx, database.ID)
		if err != nil {
			return dir, err
		}
		for _, row := range pages {
			row := row
			if _, err := e.exportPage(ctx, &row, dir, row.Title(), false); err != nil {
				e.recordFailure("export_database_row", row.ID, err)
			}
		}
	}

	return dir, nil
}

func (e *Exporter) databaseFallbackTitle(ctx context.Context, views []notion.View, titleHint string) string {
	if !isUntitled(titleHint, "database") {
		return strings.TrimSpace(titleHint)
	}

	for _, view := range views {
		if !isUntitled(view.Name, "database") {
			return strings.TrimSpace(view.Name)
		}
	}

	for _, view := range views {
		if view.DataSourceID == "" {
			continue
		}
		dataSource, _, err := e.client.RetrieveDataSourceRaw(ctx, view.DataSourceID)
		if err != nil {
			continue
		}
		if title := dataSource.DisplayTitle(); !isUntitled(title, "data source") {
			return title
		}
	}

	return "_database"
}

func (e *Exporter) exportDataSource(ctx context.Context, dataSource *notion.DataSource, raw []byte, parentDir, titleHint string, useSubdir bool) (string, error) {
	if path, ok := e.visitedSources[dataSource.ID]; ok {
		e.coverage.SkippedDuplicates++
		return path, nil
	}

	title := strings.TrimSpace(dataSource.DisplayTitle())
	if isUntitled(title, "data source") {
		if strings.TrimSpace(titleHint) != "" {
			title = strings.TrimSpace(titleHint)
		}
	}
	if isUntitled(title, "data source") {
		title = "_database"
	}

	dir := parentDir
	if useSubdir {
		dir = filepath.Join(parentDir, e.reserveName(parentDir, title, dataSource.ID, ""))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := writeFile(filepath.Join(dir, "_data_source_"+notion.ShortID(dataSource.ID)+".json"), prettyJSON(raw)); err != nil {
		return "", err
	}

	e.visitedSources[dataSource.ID] = dir
	e.coverage.DataSourcesExported++
	e.manifest.DataSources = append(e.manifest.DataSources, ManifestDataSource{ID: dataSource.ID, Title: title, Path: e.relative(dir), URL: dataSource.URL})
	fmt.Fprintf(e.writer, "exported data source: %s\n", e.relative(dir))

	pages, err := e.client.QueryDataSource(ctx, dataSource.ID)
	if err != nil {
		return dir, err
	}
	for _, row := range pages {
		row := row
		if _, err := e.exportPage(ctx, &row, dir, row.Title(), false); err != nil {
			e.recordFailure("export_data_source_row", row.ID, err)
		}
	}

	return dir, nil
}

func isUntitled(value, objectType string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	switch strings.ToLower(value) {
	case "untitled", "untitled " + objectType:
		return true
	default:
		return false
	}
}

func (e *Exporter) pageMarkdownWithRecoveredUnknowns(ctx context.Context, pageID string, markdown *notion.PageMarkdown) string {
	content := markdown.Markdown
	for _, blockID := range markdown.UnknownBlockIDs {
		blockMarkdown, err := e.client.RetrievePageMarkdown(ctx, blockID)
		if err != nil {
			e.coverage.UnknownBlocks = append(e.coverage.UnknownBlocks, UnknownBlock{PageID: pageID, BlockID: blockID, Reason: err.Error()})
			continue
		}
		e.coverage.UnknownBlocks = append(e.coverage.UnknownBlocks, UnknownBlock{PageID: pageID, BlockID: blockID, Reason: "recovered_as_appended_subtree"})
		content += "\n\n<!-- notion-export: recovered unknown block " + blockID + " -->\n\n" + blockMarkdown.Markdown
	}
	if markdown.Truncated && len(markdown.UnknownBlockIDs) == 0 {
		e.coverage.UnknownBlocks = append(e.coverage.UnknownBlocks, UnknownBlock{PageID: pageID, Reason: "markdown_response_truncated_without_unknown_block_ids"})
	}
	return content
}

func (e *Exporter) prepare() error {
	return os.MkdirAll(e.outputDir, 0o755)
}

func (e *Exporter) finish() error {
	e.manifest.FinishedAt = time.Now().UTC()
	if err := writeJSON(filepath.Join(e.outputDir, "_manifest.json"), e.manifest); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(e.outputDir, "_coverage.json"), e.coverage); err != nil {
		return err
	}
	fmt.Fprintf(e.writer, "done: %d pages, %d databases, %d data sources, %d failures\n", e.coverage.PagesExported, e.coverage.DatabasesExported, e.coverage.DataSourcesExported, len(e.coverage.Failures))
	return nil
}

func (e *Exporter) reserveName(parentDir, title, id, ext string) string {
	base := sanitizeName(title)
	name := base + ext
	used := e.usedNames[parentDir]
	if used == nil {
		used = map[string]int{}
		e.usedNames[parentDir] = used
	}
	if used[name] == 0 {
		used[name] = 1
		return name
	}

	fallbackBase := base + " " + notion.ShortID(id)
	name = fallbackBase + ext
	for used[name] > 0 {
		used[name]++
		name = fmt.Sprintf("%s %d%s", fallbackBase, used[name], ext)
	}
	used[name] = 1
	return name
}

func (e *Exporter) relative(path string) string {
	rel, err := filepath.Rel(e.outputDir, path)
	if err != nil {
		return path
	}
	return rel
}

func (e *Exporter) recordFailure(stage, id string, err error) {
	e.coverage.Failures = append(e.coverage.Failures, CoverageFailure{Stage: stage, ID: id, Error: err.Error()})
}

func childBlocks(blocks []notion.Block) []notion.Block {
	var children []notion.Block
	for _, block := range blocks {
		if block.Type == "child_page" || block.Type == "child_database" {
			children = append(children, block)
		}
	}
	return children
}

func isRootPage(parent notion.Parent, pageIDs, dataSourceIDs map[string]bool) bool {
	switch parent.Type {
	case "workspace":
		return true
	case "page_id":
		return !pageIDs[parent.PageID]
	case "data_source_id":
		return !dataSourceIDs[parent.DataSourceID]
	case "database_id":
		return !dataSourceIDs[parent.DatabaseID]
	default:
		return true
	}
}

func pageDirForChildren(pagePath string) string {
	if strings.HasSuffix(pagePath, ".md") {
		return strings.TrimSuffix(pagePath, ".md")
	}
	return pagePath
}

func renderPageMarkdown(page *notion.Page, title, content string) string {
	frontmatter := map[string]string{
		"title":            title,
		"notion_id":        page.ID,
		"notion_url":       page.URL,
		"created_time":     page.CreatedTime,
		"last_edited_time": page.LastEditedTime,
	}
	return renderFrontmatter(frontmatter) + strings.TrimSpace(content) + "\n"
}

func renderFrontmatter(values map[string]string) string {
	keys := []string{"title", "notion_id", "notion_url", "created_time", "last_edited_time"}
	var builder strings.Builder
	builder.WriteString("---\n")
	for _, key := range keys {
		if values[key] == "" {
			continue
		}
		encoded, _ := json.Marshal(values[key])
		builder.WriteString(key)
		builder.WriteString(": ")
		builder.Write(encoded)
		builder.WriteString("\n")
	}
	builder.WriteString("---\n\n")
	return builder.String()
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFile(path, data)
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func prettyJSON(raw []byte) []byte {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return raw
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return raw
	}
	return append(data, '\n')
}
