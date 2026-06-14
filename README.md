# notion-export-cli

Local-first Notion exporter.

Site: https://spashii.github.io/notion-export-cli/

## Build

```sh
go mod tidy
go build -o bin/notion-export ./cmd/notion-export
```

## Auth

```sh
notion-export auth login
notion-export auth status --verify
notion-export doctor
```

Credential order:

1. `NOTION_API_TOKEN`
2. `NOTION_TOKEN`
3. System keychain profile
4. Explicit plaintext fallback file

Plaintext storage is only used when the keychain is unavailable and `--plaintext-ok` is passed.

## Export

One page, database, or data source:

```sh
notion-export export "https://www.notion.so/..." ./notion-out
```

Default output directory is `./notion-out`:

```sh
notion-export export "https://www.notion.so/..."
```

Clean stale files first:

```sh
notion-export export --clean "https://www.notion.so/..."
```

Export all accessible root pages and data sources:

```sh
notion-export export --all ./notion-out
```

## Output

- Markdown files for pages.
- `index.md` for pages with child pages or databases.
- Downloaded Notion-hosted assets in `extracted_assets/`.
- Markdown and HTML-ish asset URLs rewritten to local relative paths.
- Database/data-source folders with `_database.json` and `_data_source_<id>.json` sidecars.
- `_assets.json` with stable asset paths, canonical unsigned source URLs, checksums, sizes, and content types.
- `_manifest.json` with exported objects and paths.
- `_coverage.json` with failures, duplicate skips, and unknown block recovery notes.

Asset extraction is idempotent. Assets are keyed by the SHA-256 of the canonical source URL without query parameters. Existing files are reused when recorded size and checksum still match. Partial downloads are written to temporary files and atomically renamed.

## Rate limits

Notion documents an average limit of 3 requests per second per connection. The CLI defaults to `2.5` requests per second. Override with `--rps`.
