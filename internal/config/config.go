package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

const (
	DefaultBind                 = ":8080"
	DefaultStorageRoot          = "/srv/ganache"
	DefaultMaxUploadBytes int64 = 20 * 1024 * 1024
	DefaultMaxPixels            = 50_000_000
)

type AuthMode string

const (
	AuthNone   AuthMode = "none"
	AuthBearer AuthMode = "bearer"
	AuthOIDC   AuthMode = "oidc"
)

type Config struct {
	Bind               string
	DBDSN              string
	StorageRoot        string
	MaxUploadBytes     int64
	MaxPixels          int
	PublicMedia        bool
	AuthMode           AuthMode
	CORSAllowedOrigins []string
	LogLevel           string
	SwaggerUIPath      string
	OpenAPIPath        string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		Bind:               getenv("GANACHE_BIND", DefaultBind),
		StorageRoot:        getenv("GANACHE_STORAGE_ROOT", DefaultStorageRoot),
		MaxUploadBytes:     getInt64("GANACHE_MAX_UPLOAD_BYTES", DefaultMaxUploadBytes),
		MaxPixels:          getInt("GANACHE_MAX_PIXELS", DefaultMaxPixels),
		PublicMedia:        getBool("GANACHE_PUBLIC_MEDIA", true),
		AuthMode:           AuthMode(getenv("GANACHE_AUTH_MODE", string(AuthBearer))),
		CORSAllowedOrigins: splitAndTrim(os.Getenv("GANACHE_CORS_ALLOWED_ORIGINS")),
		LogLevel:           os.Getenv("GANACHE_LOG_LEVEL"),
		SwaggerUIPath:      "/swagger",
		OpenAPIPath:        "/openapi.yaml",
	}

	cfg.DBDSN = os.Getenv("GANACHE_DB_DSN")
	if cfg.AuthMode != AuthNone && cfg.DBDSN == "" {
		// DB is still required; compose will set it automatically.
		// For non-compose usage, require DB.
		return nil, fmt.Errorf("GANACHE_DB_DSN is required")
	}

	switch cfg.AuthMode {
	case AuthNone, AuthBearer, AuthOIDC:
	default:
		return nil, fmt.Errorf("invalid GANACHE_AUTH_MODE: %s", cfg.AuthMode)
	}

	return cfg, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		i, err := strconv.Atoi(v)
		if err == nil {
			return i
		}
	}
	return def
}

func getInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		i, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			return i
		}
	}
	return def
}

func getBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		v = strings.ToLower(strings.TrimSpace(v))
		return v == "1" || v == "true" || v == "yes" || v == "y"
	}
	return def
}

func splitAndTrim(input string) []string {
	if input == "" {
		return nil
	}
	parts := strings.Split(input, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
