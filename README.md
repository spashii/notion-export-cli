# notion-export-cli

Local-first Notion exporter.

Current scaffold focuses on authentication and API safety:

- `auth login` stores a Notion Personal Access Token in the system keychain.
- `auth status` shows the active credential source without printing secrets.
- `auth logout` removes stored credentials.
- `doctor` verifies the token against `GET /v1/users/me` through a throttled client.

## Install Dependencies

Go is required. After installing Go, run:

```bash
go mod tidy
go build -o bin/notion-export ./cmd/notion-export
```

## Auth

```bash
notion-export auth login
notion-export auth status --verify
notion-export doctor
```

Credential priority:

1. `NOTION_API_TOKEN`
2. `NOTION_TOKEN`
3. System keychain profile
4. Explicit plaintext fallback file

Plaintext token storage is only used if the keychain is unavailable and you explicitly approve it.

## Export

Export one page, database, or data source by URL or ID:

```bash
notion-export export "https://www.notion.so/..." ./notion-out
```

The output directory defaults to `./notion-out`, so this also works:

```bash
notion-export export "https://www.notion.so/..."
```

Use `--clean` to remove stale files from a previous export first:

```bash
notion-export export --clean "https://www.notion.so/..."
```

Export all accessible root pages and data sources in the authenticated workspace:

```bash
notion-export export --all ./notion-out
```

Output includes:

- Markdown files for pages.
- `index.md` for pages with child pages or databases.
- Database/data-source folders with `_database.json` and `_data_source_<id>.json` sidecars.
- Linked database views are resolved through the Views API when the database block has no direct data sources.
- Legacy inline databases without `data_sources` are queried through the database query endpoint.
- `_manifest.json` with exported objects and paths.
- `_coverage.json` with failures, duplicate skips, and unknown block recovery notes.

## Rate Limits

Notion documents an average limit of 3 requests per second per connection. The CLI defaults to `2.5` requests per second to leave headroom for jitter and retries. Override it with `--rps` if needed.
