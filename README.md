# Ganache — Technical Overview

Ganache is a lightweight media catalog and image-serving service. It provides an HTTP API for uploading, organizing, annotating, searching, and serving images. It is designed to be easy to self-host on modest hardware: local filesystem storage for binaries/assets and a relational database for metadata.

## Goals

* **Lean**: low baseline RAM/CPU; no heavyweight CMS or search engine required.
* **Generic**: not coupled to any particular site/app.
* **API-first**: admin UI and editor integrations (e.g., Quill) are built on top of the API.
* **Fast image delivery**: optimized variants with strong caching headers.
* **Good enough search**: full-text search across title/caption/tags plus tag filtering.

## Non-goals (v1)

* S3/MinIO/object-store abstraction (local disk only in v1)
* AI auto-tagging / face recognition
* Complex workflows (approval, versioning, scheduled publishing)
* Distributed processing / job queues
* Multi-tenant architecture

## Architecture

### Components

* **HTTP API (Go)**

  * Handles upload, metadata CRUD, search, and serving bytes.
* **MariaDB**

  * Stores metadata, tags, and relationships.
* **Local filesystem**

  * Stores original files and derived variants (content-addressed).

### Data flow

1. Client uploads an image (multipart form).
2. Service streams upload to disk while computing SHA-256.
3. Service inspects image dimensions/type and enforces limits.
4. Service generates variants (thumb/content) and writes them to disk.
5. Service upserts metadata and tag relationships in MariaDB.
6. Client queries/searches assets via API; selects an image for embedding.
7. Public site/editor uses `/media/{id}/{variant}` URLs to render.

## Storage model

### Filesystem layout

Files are stored by **content hash** (SHA-256) for deduplication and cache stability.

Example base directory:

```
/srv/ganache/
  original/ab/cd/<sha256>.<ext>
  content/ab/cd/<sha256>.webp
  thumb/ab/cd/<sha256>.webp
```

Where `ab` and `cd` are the first 4 hex chars split into two directories.

### Variants

* **original**: stored as uploaded (validated)
* **content**: resized for articles/pages (e.g., max width 1600px) + WebP
* **thumb**: small preview (e.g., max width 400px) + WebP

Variant sizes should be configurable.

### Deduplication behavior

* The SHA-256 hash is unique per binary content.
* Uploading an identical file returns the existing asset record (or creates a new logical record referencing the same hash—v1 will likely return the existing record).

## Database model

Ganache keeps tags normalized for filtering/autocomplete but also maintains a denormalized tag text for simple FULLTEXT search.

### Tables (v1)

* `asset`

  * identity + hash + source filename
  * width/height/bytes/mime
  * metadata fields (title/caption/credit/source/usage notes)
  * `tag_text` (denormalized string for FULLTEXT)
  * timestamps + soft delete
* `tag`

  * unique tag names
* `asset_tag`

  * many-to-many join

### Search strategy

* `FULLTEXT(title, caption, tag_text)` for “one box” searching.
* optional tag filter(s) via `asset_tag`.
* sort by `created_at` (newest first) or relevance (when FULLTEXT used).

## HTTP API

### Principles

* JSON for metadata endpoints.
* Multipart upload for file ingestion.
* Stable image URLs for editor integration.
* Strong caching for derived variants.

### Endpoint sketch (v1)

#### Upload

`POST /api/assets` (multipart/form-data)

* `file` (required)
* `title`, `caption`, `credit`, `source`, `usage_notes` (optional)
* `tags[]` (optional, repeatable)

Returns:

* asset id
* metadata
* variant URLs

#### Get asset

`GET /api/assets/{id}`

#### Update asset metadata

`PATCH /api/assets/{id}`

* updates metadata + tags

#### Delete asset

`DELETE /api/assets/{id}`

* soft delete

#### Search/browse

`GET /api/assets?q=...&tag=...&page=...&pageSize=...&sort=newest`

#### Tag autocomplete (optional but recommended)

`GET /api/tags?prefix=...`

#### Serve image bytes

`GET /media/{id}/{variant}` where variant is:

* `thumb`
* `content`
* `original`

Response headers:

* Derived variants: `Cache-Control: public, max-age=31536000, immutable`
* `ETag` support for conditional requests
* `Content-Type` set appropriately

## Editor integration (Quill and others)

Ganache is designed so that editor plugins can:

1. Upload an image to `/api/assets` and get back a URL (and ID).
2. Insert the image by URL into the editor content.
3. Optionally store the `assetId` in editor-specific metadata (e.g., a custom embed node), enabling future enhancements like swapping variants or tracking usage.

Ganache does **not** mandate an editor format. It exposes stable URLs and IDs so the embedding strategy can evolve.

## Security

### AuthN/AuthZ

* `/api/*` endpoints are intended to require auth; the concrete mechanism is configured via `GANACHE_AUTH_MODE`:
  * `none` — no authentication enforced (local/dev only).
  * `apikey` — require a configured API key on `/api/*`.
  * `oidc` — planned: validate JWTs from an OpenID Connect / OAuth2 provider.
* `/media/*` is public by default and can be protected by setting `GANACHE_PUBLIC_MEDIA=false`.
* `/healthz` and `/readyz` are always unauthenticated.

### API key authentication (design)

When `GANACHE_AUTH_MODE=apikey`, Ganache authenticates requests using an `X-Api-Key` header and an API key list loaded from a configuration file.

* Clients send:
  * `X-Api-Key: <secret>` on each request to `/api/*` (and `/media/*` when `GANACHE_PUBLIC_MEDIA=false`).
* API keys are defined in a **YAML** file pointed to by `GANACHE_API_KEYS_FILE`. If `GANACHE_API_KEYS_FILE` is not set, Ganache will look for a default `api-keys.yaml` next to the binary/working directory. The file contains a **list/array of key objects**; each object includes:
  * `id` — a stable label (e.g., `caribbeancricket_admin`).
  * `key` — the secret value clients send in `X-Api-Key`.
  * `permissions` — a list of permission strings (see below).

  Example `api-keys.yaml`:
  ```yaml
  - id: caribbeancricket_admin
    key: "super-long-random-secret-1"
    permissions:
      - can_search
      - can_upload
      - can_update
      - can_delete

  - id: caribbeancricket_readonly
    key: "super-long-random-secret-2"
    permissions:
      - can_search
  ```

* On startup in `apikey` mode, Ganache loads this file and builds an in-memory lookup from key value to its id + permissions.
* If the header is missing or the key is unknown, the request fails with `401 unauthorized`; if the key is known but lacks required permissions for the endpoint, the request fails with `403 forbidden`.

### Permissions model

Authentication (API key now, OIDC/JWT later) is separate from authorization. Every authenticated principal carries a set of **permission strings**; handlers only check for these strings.

* Canonical permissions (initial set):
  * `can_search` — search/list/read assets and tags.
  * `can_upload` — upload new assets.
  * `can_update` — edit asset metadata and tags.
  * `can_delete` — delete assets (soft delete in v1).
* Endpoint mapping (v1):
  * `GET /api/assets`, `GET /api/assets/{id}`, `GET /api/tags` → require `can_search`.
  * `POST /api/assets` → require `can_upload`.
  * `PATCH /api/assets/{id}` → require `can_update`.
  * `DELETE /api/assets/{id}` → require `can_delete`.
  * `/media/{id}/{variant}`:
    * When `GANACHE_PUBLIC_MEDIA=true` → no auth required.
    * When `GANACHE_PUBLIC_MEDIA=false` → require at least `can_search`.
* Future OIDC/JWT integration will map token claims (e.g., `permissions`) into the same string permissions so handlers remain unchanged.

### Upload safety

* Enforce maximum upload size and maximum pixel dimensions.
* Validate content type by sniffing file headers (not just extension).
* Strip EXIF by default (configurable).
* Reject unsupported formats.

## Performance and resource use

* Image processing happens on upload (not on read path).
* Prefer libvips for low memory usage and speed.
* Streaming upload prevents buffering large payloads in RAM.
* Serving is mostly static-file reads with minimal logic and strong caching.

## Configuration

All configuration via environment variables (v1):

* `GANACHE_DB_DSN` (MariaDB DSN)
* `GANACHE_STORAGE_ROOT` (e.g., `/srv/ganache`)
* `GANACHE_MAX_UPLOAD_BYTES`
* `GANACHE_MAX_PIXELS`
* `GANACHE_CONTENT_MAX_WIDTH`
* `GANACHE_THUMB_MAX_WIDTH`
* `GANACHE_PUBLIC_MEDIA` (true/false)
* `GANACHE_AUTH_MODE` (one of: `none`, `apikey`, `oidc`)
* `GANACHE_API_KEYS_FILE` (optional; path to YAML file defining API keys; used when `GANACHE_AUTH_MODE=apikey`. Defaults to `api-keys.yaml` if unset.)
* `GANACHE_CORS_ALLOWED_ORIGINS` (comma-separated)
* `GANACHE_LOG_LEVEL` (optional)

## Deployment

* Single container or single binary on a VM.
* Mount a persistent volume for `GANACHE_STORAGE_ROOT`.
* MariaDB schema managed via migrations.

Recommended reverse proxy behavior:

* Gzip/brotli for JSON endpoints
* Long cache for `/media/*` variants
* Range requests optional (not needed for images, but harmless)

## Observability

* Text logs to stdout
* Basic metrics (optional v1.1): request counts, latencies, upload size, variant generation time
* Health endpoints:

  * `GET /healthz` (process OK)
  * `GET /readyz` (DB reachable, storage writable)

## Roadmap ideas (later)

* Additional variants (e.g., `small`, `large`, `avif`)
* Bulk tag operations
* Asset usage tracking (which articles/pages reference an asset)
* Background reprocessing (e.g., regenerate variants after config changes)
* Pluggable storage backend (S3-compatible)
* Access-controlled private assets (signed URLs)

## Quickstart

Config values can come from env vars or a local `.env` (auto-loaded).

1. Start services: `docker compose up --build`
2. Run migrations (with compose DB up): `GANACHE_DB_DSN='ganache:ganache@tcp(localhost:3306)/ganache?parseTime=true&multiStatements=true' make migrate-up`
3. Upload an image (with auth disabled or using a valid API key, depending on `GANACHE_AUTH_MODE`):
   ```bash
   curl -X POST \
     -H "X-Api-Key: your-api-key-here" \
     -F "file=@./tests/sample1.jpg" \
     -F "title=Sample Image" \
     -F "tags=test" \
     http://localhost:8080/api/assets
   ```
4. Browse Swagger UI at `http://localhost:8080/swagger`
5. Run comprehensive HTTP tests: see `tests/smoke.http` and `testing.md`

## CI and releases

- CI (`.github/workflows/ci.yml`): gofmt check, golangci-lint, `go test ./...`, plus `go test -race ./...` on Linux.
- Releases (`.github/workflows/release.yml`): tag `v*` to trigger GoReleaser using `.goreleaser.yaml`, building `ganache` and `ganache-migrate` for linux/darwin/windows on amd64/arm64, producing archives, checksums, and SBOMs.
- Signing is optional: set `COSIGN_KEY` secret to sign checksums; otherwise the workflow skips signing.
- Local dry-run: install GoReleaser and run `goreleaser release --snapshot --clean`.
- Ship a release: `git tag vX.Y.Z && git push origin vX.Y.Z`.

## API Versioning

- Current API is unversioned and treated as the stable v1 surface.
- No breaking changes will be made within 1.x; breaking changes will move to `/api/v2/...` only for affected endpoints.
- Media URLs (`/media/{id}/{variant}`) remain unversioned and stable to avoid breaking embedded assets.
- Header-based versioning is not used to keep clients simple; paths will carry versions when needed.
- Deprecations will be noted in release notes/changelog with guidance before removal.
