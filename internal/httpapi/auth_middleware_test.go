package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arawak/ganache/internal/config"
)

func TestAuthMiddlewareAPIKeySuccess(t *testing.T) {
	store := &APIKeyStore{byKey: map[string]*APIKey{
		"secret": {ID: "test", Permissions: []string{PermCanSearch}},
	}}
	s := &Server{cfg: &config.Config{AuthMode: config.AuthAPIKey}, apiKeys: store}

	nextCalled := false
	h := s.authMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Api-Key", "secret")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if !nextCalled {
		t.Fatalf("expected next handler to be called")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAuthMiddlewareMissingKey(t *testing.T) {
	s := &Server{cfg: &config.Config{AuthMode: config.AuthAPIKey}, apiKeys: &APIKeyStore{byKey: map[string]*APIKey{}}}
	h := s.authMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddlewareInvalidKey(t *testing.T) {
	s := &Server{cfg: &config.Config{AuthMode: config.AuthAPIKey}, apiKeys: &APIKeyStore{byKey: map[string]*APIKey{}}}
	h := s.authMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Api-Key", "bad")

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRequirePermissionsEnforces(t *testing.T) {
	store := &APIKeyStore{byKey: map[string]*APIKey{
		"secret": {ID: "test", Permissions: []string{PermCanSearch}},
	}}
	s := &Server{cfg: &config.Config{AuthMode: config.AuthAPIKey}, apiKeys: store}

	nextCalled := false
	h := s.authMiddleware()(s.requirePermissions(PermCanSearch)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Api-Key", "secret")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if !nextCalled {
		t.Fatalf("expected next handler to be called")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestRequirePermissionsForbidden(t *testing.T) {
	store := &APIKeyStore{byKey: map[string]*APIKey{
		"secret": {ID: "test", Permissions: []string{PermCanSearch}},
	}}
	s := &Server{cfg: &config.Config{AuthMode: config.AuthAPIKey}, apiKeys: store}

	h := s.authMiddleware()(s.requirePermissions(PermCanDelete)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Api-Key", "secret")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestRequirePermissionsIgnoredWhenAuthNone(t *testing.T) {
	s := &Server{cfg: &config.Config{AuthMode: config.AuthNone}}

	nextCalled := false
	h := s.requirePermissions(PermCanDelete)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	h.ServeHTTP(rec, req)

	if !nextCalled {
		t.Fatalf("expected next handler to be called")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
