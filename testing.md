# Testing Ganache

This project includes comprehensive HTTP smoke tests via REST Client / httpyac with assertions.

## Prerequisites
- MariaDB running (e.g., `docker compose up -d mariadb`)
- Migrations applied: `GANACHE_DB_DSN='ganache:ganache@tcp(localhost:3306)/ganache?parseTime=true&multiStatements=true' make migrate-up`
- Service running: `make run` (loads `.env` automatically)

## HTTP smoke suite
- File: `tests/smoke.http`
- Sample images: `tests/sample1.jpg`, `tests/sample2.webp`, `tests/sample3.png`
- Variables:
  - `@baseUrl` (default `http://localhost:8080`)
  - `@token` (any non-empty bearer; defaults to `dummy-token`)
  - `@greavesAssetId` (set manually after upload for realistic tests)

## Test Coverage

### Basic smoke tests
- Health and readiness checks
- Upload asset (sample2.webp) with basic metadata
- Search all assets with pagination
- Search with filters (query + tag)
- Get asset by ID
- Media variant serving (thumb)

### Realistic use case tests (Justin Greaves cricket photo)
These tests demonstrate real-world workflows for a photo management system:

1. **Upload**: Upload sample1.jpg (Justin Greaves double century photo) with:
   - Descriptive title and caption
   - Credit and source information
   - Initial tag (cricket)

2. **Full-text searches**:
   - Search by person name ("justin greaves")
   - Search by event ("test match")
   - Search by subject ("celebration", "west indies")

3. **Tag-based searches**:
   - Single tag filter (cricket)
   - Multiple tag filters (cricket + new-zealand)
   - Combined query + tag filters

4. **Metadata management**:
   - Get asset by ID
   - Update usage notes via PATCH
   - Add comprehensive tags via PATCH (11 total tags)
   - Verify tags were updated

5. **Tag operations**:
   - List all tags
   - Tag autocomplete (prefix search)

6. **Media serving**:
   - Serve thumb variant
   - Serve content variant  
   - Serve original variant (validates JPEG format)

7. **Deduplication**:
   - Re-upload same file to verify SHA-256 detection returns 409

### Running the tests

**IntelliJ IDEA / JetBrains HTTP Client**:
1. Open `tests/smoke.http`
2. Run basic tests sequentially from the top
3. For realistic tests:
   - Run "Upload Justin Greaves" test
   - Note the asset ID from the response
   - Update `@greavesAssetId` at the top of the file
   - Run remaining realistic tests sequentially

**VS Code REST Client**:
Same workflow as IntelliJ

**Command line with httpyac**:
```bash
httpyac tests/smoke.http --all
```

### Assertions included
- Status codes (200, 201, 409, 404)
- Response structure validation
- Metadata field validation (title, caption, credit, source, tags)
- Variant URLs present (thumb, content, original)
- Image dimensions and file properties
- ETag and Cache-Control headers
- Full-text search relevance
- Tag filtering accuracy
- Duplicate detection via SHA-256

## Curl alternative (no assertions)
```bash
BASE=http://localhost:8080
TOKEN="Bearer dummy-token"

# Upload with metadata
ASSET=$(curl -sf -H "Authorization: $TOKEN" \
  -F file=@tests/sample1.jpg \
  -F title="Justin Greaves celebrates double century" \
  -F caption="Test match photo" \
  -F tags=cricket \
  "$BASE/api/assets")

ID=$(echo "$ASSET" | jq -r .id)

# Get asset details
curl -sf -H "Authorization: $TOKEN" "$BASE/api/assets/$ID" | jq

# Search for cricket photos
curl -sf -H "Authorization: $TOKEN" "$BASE/api/assets?tag=cricket" | jq

# Download variants
curl -sf "$BASE/media/$ID/thumb" -o /tmp/thumb.webp
curl -sf "$BASE/media/$ID/content" -o /tmp/content.webp
curl -sf "$BASE/media/$ID/original" -o /tmp/original.jpg
```

## Integration tests (Go)
Run Go integration tests:
```bash
go test ./... -v
```

## Notes
- Auth mode `bearer` only checks that the header is present (not validated)
- Storage defaults to `tmp-storage/`; can be configured via `GANACHE_STORAGE_ROOT`
- Deduplication works via SHA-256 hash; re-uploading returns 409 with existing asset
- Tags support full-text search across title, caption, and tag names
- All image variants are served with appropriate caching headers
