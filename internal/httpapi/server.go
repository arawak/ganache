package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/arawak/ganache/internal/config"
	"github.com/arawak/ganache/internal/media"
	"github.com/arawak/ganache/internal/store"
	"github.com/arawak/ganache/internal/swaggerui"
)

type Server struct {
	cfg     *config.Config
	store   *store.Store
	media   *media.Manager
	apiKeys *APIKeyStore
	logger  *slog.Logger
}

var (
	openapiOnce sync.Once
	openapiData []byte
	openapiErr  error
	openapiFile string
)

func loadOpenAPI(path string) ([]byte, error) {
	openapiOnce.Do(func() {
		openapiFile = path
		if openapiFile == "" {
			// Try common locations
			candidates := []string{
				"openapi.yaml",
				"/app/openapi.yaml",
				filepath.Join(filepath.Dir(os.Args[0]), "openapi.yaml"),
			}
			for _, candidate := range candidates {
				if _, err := os.Stat(candidate); err == nil {
					openapiFile = candidate
					break
				}
			}
		}
		if openapiFile == "" {
			openapiErr = fmt.Errorf("openapi.yaml not found in any expected location")
			return
		}
		openapiData, openapiErr = os.ReadFile(openapiFile)
	})
	return openapiData, openapiErr
}

func NewRouter(cfg *config.Config, st *store.Store, mediaMgr *media.Manager, apiKeys *APIKeyStore, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}
	s := &Server{cfg: cfg, store: st, media: mediaMgr, apiKeys: apiKeys, logger: logger}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(loggingMiddleware(logger))

	if len(cfg.CORSAllowedOrigins) > 0 {
		c := cors.New(cors.Options{
			AllowedOrigins:   cfg.CORSAllowedOrigins,
			AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Authorization", "Content-Type", "Accept", "X-Api-Key"},
			AllowCredentials: true,
		})
		r.Use(c.Handler)
	}

	r.Get("/healthz", s.GetHealthz)
	r.Get("/readyz", s.GetReadyz)
	r.Get(cfg.OpenAPIPath, s.serveOpenAPI)
	r.Mount(cfg.SwaggerUIPath, swaggerui.Handler(cfg.OpenAPIPath))

	wrapper := ServerInterfaceWrapper{Handler: s, ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
	}}

	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware())
		r.With(s.requirePermissions(PermCanSearch)).Get("/api/assets", wrapper.SearchAssets)
		r.With(s.requirePermissions(PermCanUpload)).Post("/api/assets", wrapper.UploadAsset)
		r.With(s.requirePermissions(PermCanDelete)).Delete("/api/assets/{id}", wrapper.DeleteAsset)
		r.With(s.requirePermissions(PermCanSearch)).Get("/api/assets/{id}", wrapper.GetAsset)
		r.With(s.requirePermissions(PermCanUpdate)).Patch("/api/assets/{id}", wrapper.UpdateAsset)
		r.With(s.requirePermissions(PermCanSearch)).Get("/api/tags", wrapper.ListTags)
	})

	r.Group(func(r chi.Router) {
		if !cfg.PublicMedia {
			r.Use(s.authMiddleware())
			r.Use(s.requirePermissions(PermCanSearch))
		}
		r.Get("/media/{id}/{variant}", wrapper.GetMediaVariant)
	})

	r.Group(func(r chi.Router) {
		if !cfg.PublicMedia {
			r.Use(s.authMiddleware())
			r.Use(s.requirePermissions(PermCanSearch))
		}
		r.Get("/media/{id}/{variant}", wrapper.GetMediaVariant)
	})

	return r
}

func (s *Server) authMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch s.cfg.AuthMode {
			case config.AuthNone:
				next.ServeHTTP(w, r)
				return
			case config.AuthAPIKey:
				apiKey := strings.TrimSpace(r.Header.Get("X-Api-Key"))
				if apiKey == "" {
					writeError(w, http.StatusUnauthorized, "unauthorized", "missing api key", nil)
					return
				}
				if s.apiKeys == nil {
					writeError(w, http.StatusInternalServerError, "internal", "api key store not initialized", nil)
					return
				}
				entry, ok := s.apiKeys.Lookup(apiKey)
				if !ok {
					writeError(w, http.StatusUnauthorized, "unauthorized", "invalid api key", nil)
					return
				}
				principal := newPrincipalFromAPIKey(entry)
				next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
				return
			case config.AuthOIDC:
				writeError(w, http.StatusNotImplemented, "not_implemented", "oidc auth mode is not implemented yet", nil)
				return
			default:
				writeError(w, http.StatusUnauthorized, "unauthorized", "auth mode not supported", nil)
				return
			}
		})
	}
}

func (s *Server) requirePermissions(perms ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.cfg.AuthMode == config.AuthNone {
				next.ServeHTTP(w, r)
				return
			}
			principal, ok := PrincipalFromContext(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required", nil)
				return
			}
			for _, perm := range perms {
				if !principal.HasPermission(perm) {
					writeError(w, http.StatusForbidden, "forbidden", "insufficient permissions", nil)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) serveOpenAPI(w http.ResponseWriter, _ *http.Request) {
	data, err := loadOpenAPI("")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "unable to load openapi.yaml", map[string]any{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(data); err != nil {
		s.logger.Error("failed to write openapi response", "error", err)
	}
}

func (s *Server) GetHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, Health{Status: Ok})
}

func (s *Server) GetReadyz(w http.ResponseWriter, _ *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "database unreachable", map[string]any{"error": err.Error()})
		return
	}
	if err := s.media.IsWritable(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "storage not writable", map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, Health{Status: Ok})
}

func (s *Server) SearchAssets(w http.ResponseWriter, r *http.Request, params SearchAssetsParams) {
	pageSize := derefInt(params.PageSize, 30)
	if pageSize < 1 {
		pageSize = 1
	}
	if pageSize > 200 {
		pageSize = 200
	}

	page := derefInt(params.Page, 1)
	if page < 1 {
		page = 1
	}

	sp := store.SearchParams{
		Query:          getStringPtr(params.Q),
		Tags:           derefStringSlice(params.Tag),
		Page:           page,
		PageSize:       pageSize,
		Sort:           string(derefSort(params.Sort)),
		IncludeDeleted: derefBool(params.IncludeDeleted, false),
	}
	s.logger.Debug("search", "query", sp.Query, "tags", sp.Tags, "page", sp.Page, "pageSize", sp.PageSize, "sort", sp.Sort)
	assets, total, err := s.store.SearchAssets(r.Context(), sp)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "failed to search", map[string]any{"error": err.Error()})
		return
	}
	resp := AssetSearchResponse{Page: sp.Page, PageSize: sp.PageSize, Total: total}
	for i := range assets {
		resp.Items = append(resp.Items, s.toAPIAsset(&assets[i]))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) UploadAsset(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadBytes+1024)
	if err := r.ParseMultipartForm(s.cfg.MaxUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "failed to parse multipart", map[string]any{"error": err.Error()})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "file is required", nil)
		return
	}
	defer file.Close()

	save, err := s.media.Save(r.Context(), file, header.Filename, s.cfg.MaxUploadBytes, s.cfg.MaxPixels)
	if err != nil {
		status := http.StatusBadRequest
		switch err {
		case media.ErrTooLarge:
			status = http.StatusBadRequest
		case media.ErrInvalidImage:
			status = http.StatusBadRequest
		default:
			status = http.StatusInternalServerError
		}
		writeError(w, status, "upload_failed", err.Error(), nil)
		return
	}

	title := formValue(r.MultipartForm.Value, "title")
	caption := formValue(r.MultipartForm.Value, "caption")
	credit := formValue(r.MultipartForm.Value, "credit")
	source := formValue(r.MultipartForm.Value, "source")
	usageNotes := formValue(r.MultipartForm.Value, "usageNotes")
	tags := r.MultipartForm.Value["tags"]

	// Validate field lengths
	if len(title) > 255 {
		writeError(w, http.StatusBadRequest, "bad_request", "title exceeds maximum length of 255 characters", nil)
		return
	}
	if len(credit) > 255 {
		writeError(w, http.StatusBadRequest, "bad_request", "credit exceeds maximum length of 255 characters", nil)
		return
	}
	if len(source) > 255 {
		writeError(w, http.StatusBadRequest, "bad_request", "source exceeds maximum length of 255 characters", nil)
		return
	}
	for _, tag := range tags {
		if len(tag) > 255 {
			writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("tag '%s' exceeds maximum length of 255 characters", tag), nil)
			return
		}
	}

	assetInput := store.AssetCreate{
		Title:            title,
		Caption:          caption,
		Credit:           credit,
		Source:           source,
		UsageNotes:       usageNotes,
		Tags:             tags,
		Width:            save.Width,
		Height:           save.Height,
		Bytes:            save.Bytes,
		Mime:             save.Mime,
		OriginalFilename: header.Filename,
		SHA256:           save.SHA256,
	}

	s.logger.Debug("upload asset", "title", assetInput.Title, "tagCount", len(assetInput.Tags))

	asset, err := s.store.CreateAsset(r.Context(), assetInput)
	if err != nil {
		if errors.Is(err, store.ErrDuplicate) && asset != nil {
			writeJSON(w, http.StatusConflict, s.toAPIAsset(asset))
			return
		}
		s.logger.Error("failed to create asset", "error", err, "title", assetInput.Title, "tags", assetInput.Tags)
		writeError(w, http.StatusInternalServerError, "internal", "failed to persist asset", map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, s.toAPIAsset(asset))
}

func (s *Server) GetAsset(w http.ResponseWriter, r *http.Request, id AssetId) {
	asset, err := s.store.GetAsset(r.Context(), id, false)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "asset not found", nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "failed to retrieve asset", map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.toAPIAsset(asset))
}

func (s *Server) UpdateAsset(w http.ResponseWriter, r *http.Request, id AssetId) {
	var payload AssetUpdate
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid json", nil)
		return
	}

	// Validate field lengths
	if payload.Title != nil && len(*payload.Title) > 255 {
		writeError(w, http.StatusBadRequest, "bad_request", "title exceeds maximum length of 255 characters", nil)
		return
	}
	if payload.Credit != nil && len(*payload.Credit) > 255 {
		writeError(w, http.StatusBadRequest, "bad_request", "credit exceeds maximum length of 255 characters", nil)
		return
	}
	if payload.Source != nil && len(*payload.Source) > 255 {
		writeError(w, http.StatusBadRequest, "bad_request", "source exceeds maximum length of 255 characters", nil)
		return
	}
	if payload.Tags != nil {
		for _, tag := range *payload.Tags {
			if len(tag) > 255 {
				writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("tag '%s' exceeds maximum length of 255 characters", tag), nil)
				return
			}
		}
	}

	upd := store.AssetUpdate{
		Title:      payload.Title,
		Caption:    payload.Caption,
		Credit:     payload.Credit,
		Source:     payload.Source,
		UsageNotes: payload.UsageNotes,
		Tags:       payload.Tags,
	}
	asset, err := s.store.UpdateAsset(r.Context(), id, upd)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "asset not found", nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "failed to update asset", map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.toAPIAsset(asset))
}

func (s *Server) DeleteAsset(w http.ResponseWriter, r *http.Request, id AssetId) {
	if err := s.store.DeleteAsset(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "asset not found", nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "failed to delete asset", map[string]any{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ListTags(w http.ResponseWriter, r *http.Request, params ListTagsParams) {
	page := derefInt(params.Page, 1)
	if page < 1 {
		page = 1
	}

	size := derefInt(params.PageSize, 100)
	if size < 1 {
		size = 1
	}
	if size > 500 {
		size = 500
	}

	tags, total, err := s.store.ListTags(r.Context(), getStringPtr(params.Prefix), page, size)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "failed to list tags", map[string]any{"error": err.Error()})
		return
	}
	resp := TagListResponse{Items: make([]Tag, 0, len(tags)), Page: page, PageSize: size, Total: total}
	for _, t := range tags {
		resp.Items = append(resp.Items, Tag{Name: t})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) GetMediaVariant(w http.ResponseWriter, r *http.Request, id AssetId, variant GetMediaVariantParamsVariant) {
	asset, err := s.store.GetAsset(r.Context(), id, false)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, "not_found", "asset not found", nil)
		return
	}
	var path string
	ext := guessExt(asset.OriginalFilename)
	switch variant {
	case GetMediaVariantParamsVariantThumb:
		path = s.media.PathForVariant(asset.SHA256, media.VariantThumb, ext)
	case GetMediaVariantParamsVariantContent:
		path = s.media.PathForVariant(asset.SHA256, media.VariantContent, ext)
	case GetMediaVariantParamsVariantOriginal:
		path = s.media.PathForVariant(asset.SHA256, media.VariantOriginal, ext)
	default:
		writeError(w, http.StatusNotFound, "not_found", "variant not found", nil)
		return
	}

	etag := fmt.Sprintf("\"%s-%s\"", asset.SHA256, variant)
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	file, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "variant not found", nil)
		return
	}
	defer file.Close()

	info, _ := file.Stat()
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if mimeType == "" {
		mimeType = asset.Mime
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("ETag", etag)
	cache := "public, max-age=86400"
	if variant != GetMediaVariantParamsVariantOriginal {
		cache = "public, max-age=31536000, immutable"
	}
	w.Header().Set("Cache-Control", cache)
	if info != nil {
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	}
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, file); err != nil {
		s.logger.Error("failed to copy file to response", "error", err, "path", path)
	}
}

func (s *Server) toAPIAsset(a *store.Asset) Asset {
	orig := a.OriginalFilename
	sha := a.SHA256
	return Asset{
		Id:               a.ID,
		Title:            a.Title,
		Caption:          a.Caption,
		Credit:           a.Credit,
		Source:           a.Source,
		UsageNotes:       a.UsageNotes,
		Tags:             a.Tags,
		Width:            a.Width,
		Height:           a.Height,
		Bytes:            a.Bytes,
		Mime:             a.Mime,
		OriginalFilename: &orig,
		Sha256:           &sha,
		CreatedAt:        a.CreatedAt,
		UpdatedAt:        a.UpdatedAt,
		DeletedAt:        a.DeletedAt,
		Variants: AssetVariantUrls{
			Thumb:    fmt.Sprintf("/media/%d/thumb", a.ID),
			Content:  fmt.Sprintf("/media/%d/content", a.ID),
			Original: fmt.Sprintf("/media/%d/original", a.ID),
		},
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	writeJSON(w, status, Error{Code: code, Message: message, Details: &details})
}

func loggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			logger.Info("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start).String())
		})
	}
}

func getStringPtr(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func derefStringSlice(v *[]string) []string {
	if v == nil {
		return nil
	}
	return *v
}

func derefInt(v *int, def int) int {
	if v == nil {
		return def
	}
	return *v
}

func derefBool(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}

func derefSort(v *SearchAssetsParamsSort) SearchAssetsParamsSort {
	if v == nil {
		return SearchAssetsParamsSort(SortNewest)
	}
	return *v
}

func formValue(values map[string][]string, key string) string {
	if values == nil {
		return ""
	}
	vals := values[key]
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

func guessExt(filename string) string {
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(filename)))
	if ext == "" {
		ext = ".bin"
	}
	return ext
}
