package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoadPublicMedia(t *testing.T) {
	t.Chdir(t.TempDir()) // Keep .env files out of these cases.
	t.Setenv("GANACHE_DB_DSN", "test-dsn")

	for _, tc := range []struct {
		name       string
		value      string
		unset      bool
		auth       AuthMode
		wantPublic bool
		wantErr    string
	}{
		{name: "unset defaults to public", unset: true, auth: AuthNone, wantPublic: true},
		{name: "public without auth", value: "true", auth: AuthNone, wantPublic: true},
		{name: "public alias", value: " YES ", auth: AuthNone, wantPublic: true},
		{name: "private with api key", value: "false", auth: AuthAPIKey},
		{name: "private alias", value: " N ", auth: AuthAPIKey},
		{name: "reject empty", auth: AuthAPIKey, wantErr: "invalid GANACHE_PUBLIC_MEDIA"},
		{name: "reject typo", value: "flase", auth: AuthAPIKey, wantErr: "invalid GANACHE_PUBLIC_MEDIA"},
		{name: "reject private without auth", value: "false", auth: AuthNone, wantErr: "requires GANACHE_AUTH_MODE=apikey"},
		{name: "reject private with unimplemented auth", value: "false", auth: AuthOIDC, wantErr: "requires GANACHE_AUTH_MODE=apikey"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GANACHE_AUTH_MODE", string(tc.auth))
			t.Setenv("GANACHE_PUBLIC_MEDIA", tc.value)
			if tc.unset {
				if err := os.Unsetenv("GANACHE_PUBLIC_MEDIA"); err != nil {
					t.Fatal(err)
				}
			}

			cfg, err := Load()
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Load() error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.PublicMedia != tc.wantPublic {
				t.Errorf("PublicMedia = %t, want %t", cfg.PublicMedia, tc.wantPublic)
			}
		})
	}
}
