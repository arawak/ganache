//go:build integration

package ganache

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arawak/ganache/internal/config"
	"github.com/arawak/ganache/internal/httpapi"
	"github.com/arawak/ganache/internal/media"
	"github.com/arawak/ganache/internal/store"
	"github.com/arawak/ganache/migrations"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type assetResponse struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Caption string `json:"caption"`
	Credit  string `json:"credit"`
	Source  string `json:"source"`
}

func startMaria(t *testing.T, ctx context.Context) (testcontainers.Container, string) {
	req := testcontainers.ContainerRequest{
		Image:        "mariadb:11.4",
		Env:          map[string]string{"MARIADB_ROOT_PASSWORD": "root", "MARIADB_DATABASE": "ganache", "MARIADB_USER": "ganache", "MARIADB_PASSWORD": "ganache"},
		ExposedPorts: []string{"3306/tcp"},
		WaitingFor: wait.ForExec([]string{"mariadb", "-u", "ganache", "-pganache", "-e", "SELECT 1"}).
			WithStartupTimeout(60 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	if err != nil {
		t.Fatalf("start mariadb: %v", err)
	}
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "3306/tcp")
	if err != nil {
		t.Fatalf("mapped port: %v", err)
	}
	dsn := fmt.Sprintf("ganache:ganache@tcp(%s:%s)/ganache?parseTime=true&multiStatements=true", host, port.Port())
	return container, dsn
}

func TestEndToEnd(t *testing.T) {
	ctx := context.Background()

	container, dsn := startMaria(t, ctx)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	if err := migrations.Up(dsn); err != nil {
		t.Fatalf("migrations failed: %v", err)
	}

	db, err := sqlx.Connect("mysql", dsn)
	if err != nil {
		t.Fatalf("db connect: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	root := t.TempDir()
	cfg := &config.Config{
		Bind:               ":0",
		DBDSN:              dsn,
		StorageRoot:        root,
		MaxUploadBytes:     config.DefaultMaxUploadBytes,
		MaxPixels:          config.DefaultMaxPixels,
		PublicMedia:        true,
		AuthMode:           config.AuthNone,
		CORSAllowedOrigins: nil,
		SwaggerUIPath:      "/swagger",
		OpenAPIPath:        "/openapi.yaml",
	}
	st := store.New(db)
	mediaMgr := media.NewManager(root)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ts := httptest.NewServer(httpapi.NewRouter(cfg, st, mediaMgr, nil, logger))
	t.Cleanup(ts.Close)

	assetID := uploadAndValidate(t, ts.URL+"/api/assets")
	getAsset(t, ts.URL+"/api/assets/", assetID)
	patchAsset(t, ts.URL+"/api/assets/", assetID)
	searchAsset(t, ts.URL+"/api/assets", assetID)
	mediaURL := fmt.Sprintf("%s/media/%d/thumb", ts.URL, assetID)
	validateMedia(t, mediaURL)
	deleteAsset(t, ts.URL+"/api/assets/", assetID)
	ensureDeleted(t, ts.URL+"/api/assets", assetID)
	readyz(t, ts.URL+"/readyz")
}

func uploadAndValidate(t *testing.T, url string) int64 {

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	w, err := mw.CreateFormFile("file", "sample.png")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	if err := png.Encode(w, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}

	_ = mw.WriteField("title", "Test Title")
	_ = mw.WriteField("caption", "Caption here")
	_ = mw.WriteField("credit", "Author")
	_ = mw.WriteField("source", "Camera")
	_ = mw.WriteField("usageNotes", "notes")
	_ = mw.WriteField("tags", "TagOne")
	_ = mw.WriteField("tags", "TagTwo")
	mw.Close()

	req, _ := http.NewRequest(http.MethodPost, url, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status %d body %s", resp.StatusCode, string(body))
	}
	var asset httpapi.Asset
	if err := json.NewDecoder(resp.Body).Decode(&asset); err != nil {
		t.Fatalf("decode asset: %v", err)
	}
	if asset.Id == 0 {
		t.Fatalf("missing asset id")
	}
	return asset.Id
}

func getAsset(t *testing.T, base string, id int64) {
	resp, err := http.Get(fmt.Sprintf("%s%d", base, id))
	if err != nil {
		t.Fatalf("get asset: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status %d body %s", resp.StatusCode, string(body))
	}
}

func patchAsset(t *testing.T, base string, id int64) {
	payload := map[string]any{"title": "Updated", "tags": []string{"tagone", "tagtwo", "tagthree"}}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s%d", base, id), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status %d body %s", resp.StatusCode, string(body))
	}
}

func searchAsset(t *testing.T, url string, id int64) {
	resp, err := http.Get(url + "?q=Updated&tag=tagtwo&page=1&pageSize=10&sort=relevance")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("search status %d body %s", resp.StatusCode, string(body))
	}
	var res httpapi.AssetSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if res.Total == 0 || len(res.Items) == 0 || res.Items[0].Id != id {
		t.Fatalf("search did not return asset: %+v", res)
	}
}

func validateMedia(t *testing.T, url string) {
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("media get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("media status %d body %s", resp.StatusCode, string(body))
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatalf("missing etag")
	}
	cache := resp.Header.Get("Cache-Control")
	if cache == "" {
		t.Fatalf("missing cache control")
	}

	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("If-None-Match", etag)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("etag request: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotModified {
		body, _ := io.ReadAll(resp2.Body)
		t.Fatalf("expected 304 got %d body %s", resp2.StatusCode, string(body))
	}
}

func deleteAsset(t *testing.T, base string, id int64) {
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s%d", base, id), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("delete status %d body %s", resp.StatusCode, string(body))
	}
}

func ensureDeleted(t *testing.T, url string, id int64) {
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("search post-delete: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("search status %d body %s", resp.StatusCode, string(body))
	}
	var res httpapi.AssetSearchResponse
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if res.Total != 0 {
		t.Fatalf("expected no results after delete")
	}
}

func readyz(t *testing.T, url string) {
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("readyz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("readyz status %d body %s", resp.StatusCode, string(body))
	}
}

func TestReuploadDeletedAsset(t *testing.T) {
	ctx := context.Background()

	container, dsn := startMaria(t, ctx)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	if err := migrations.Up(dsn); err != nil {
		t.Fatalf("migrations failed: %v", err)
	}

	db, err := sqlx.Connect("mysql", dsn)
	if err != nil {
		t.Fatalf("db connect: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	root := t.TempDir()
	cfg := &config.Config{
		Bind:               ":0",
		DBDSN:              dsn,
		StorageRoot:        root,
		MaxUploadBytes:     config.DefaultMaxUploadBytes,
		MaxPixels:          config.DefaultMaxPixels,
		PublicMedia:        true,
		AuthMode:           config.AuthNone,
		CORSAllowedOrigins: nil,
		SwaggerUIPath:      "/swagger",
		OpenAPIPath:        "/openapi.yaml",
	}
	st := store.New(db)
	mediaMgr := media.NewManager(root)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ts := httptest.NewServer(httpapi.NewRouter(cfg, st, mediaMgr, nil, logger))
	t.Cleanup(ts.Close)

	// Create a test image with specific pixel values to ensure consistent SHA256
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.RGBA{R: 42, G: 137, B: 255, A: 255})
		}
	}

	// Step 1: Upload the image
	var buf1 bytes.Buffer
	mw1 := multipart.NewWriter(&buf1)
	w1, _ := mw1.CreateFormFile("file", "test.png")
	png.Encode(w1, img)
	_ = mw1.WriteField("title", "First Upload")
	mw1.Close()

	req1, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/assets", &buf1)
	req1.Header.Set("Content-Type", mw1.FormDataContentType())
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatalf("first upload: %v", err)
	}
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp1.Body)
		t.Fatalf("first upload status %d body %s", resp1.StatusCode, string(body))
	}
	var asset1 httpapi.Asset
	if err := json.NewDecoder(resp1.Body).Decode(&asset1); err != nil {
		t.Fatalf("decode first asset: %v", err)
	}
	originalID := asset1.Id

	// Step 2: Delete the asset
	delReq, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/assets/%d", ts.URL, originalID), nil)
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(delResp.Body)
		t.Fatalf("delete status %d body %s", delResp.StatusCode, string(body))
	}

	// Step 3: Re-upload the same image
	var buf2 bytes.Buffer
	mw2 := multipart.NewWriter(&buf2)
	w2, _ := mw2.CreateFormFile("file", "test.png")
	png.Encode(w2, img)
	_ = mw2.WriteField("title", "Reuploaded Asset")
	_ = mw2.WriteField("caption", "New caption")
	mw2.Close()

	req2, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/assets", &buf2)
	req2.Header.Set("Content-Type", mw2.FormDataContentType())
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("reupload: %v", err)
	}
	defer resp2.Body.Close()

	// Should return 201 Created, not 409 Conflict
	if resp2.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp2.Body)
		t.Fatalf("reupload expected 201 Created, got status %d body %s", resp2.StatusCode, string(body))
	}

	var asset2 httpapi.Asset
	if err := json.NewDecoder(resp2.Body).Decode(&asset2); err != nil {
		t.Fatalf("decode reuploaded asset: %v", err)
	}

	// Step 4: Verify the asset was undeleted (same ID, no deletedAt)
	if asset2.Id != originalID {
		t.Fatalf("expected same asset ID %d, got %d", originalID, asset2.Id)
	}
	if asset2.DeletedAt != nil {
		t.Fatalf("expected deletedAt to be nil, got %v", asset2.DeletedAt)
	}
	if asset2.Title != "Reuploaded Asset" {
		t.Fatalf("expected title 'Reuploaded Asset', got %q", asset2.Title)
	}
	if asset2.Caption != "New caption" {
		t.Fatalf("expected caption 'New caption', got %q", asset2.Caption)
	}

	// Step 5: Verify the asset is visible in search
	searchResp, err := http.Get(ts.URL + "/api/assets?q=Reuploaded")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	defer searchResp.Body.Close()
	var searchRes httpapi.AssetSearchResponse
	if err := json.NewDecoder(searchResp.Body).Decode(&searchRes); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if searchRes.Total == 0 {
		t.Fatalf("reuploaded asset not found in search results")
	}
	if searchRes.Items[0].Id != originalID {
		t.Fatalf("search returned wrong asset ID")
	}
}
