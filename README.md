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

* `/api/*` endpoints require auth (e.g., JWT bearer token).
* `/media/*` can be public (common case) or protected (configurable).

Authorization model (v1):

* `media.read` — read/search metadata
* `media.write` — upload/edit/delete metadata

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
* `GANACHE_ENABLE_PUBLIC_MEDIA` (true/false)
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
3. Upload an image:
   ```bash
   curl -X POST -H "Authorization: Bearer dummy-token" \
     -F "file=@./tests/sample1.jpg" \
     -F "title=Sample Image" \
     -F "tags=test" \
     http://localhost:8080/api/assets
   ```
4. Browse Swagger UI at `http://localhost:8080/swagger`
5. Run comprehensive HTTP tests: see `tests/smoke.http` and `testing.md`
