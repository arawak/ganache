package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

const (
	DefaultBind                  = ":8080"
	DefaultStorageRoot           = "/srv/ganache"
	DefaultMaxUploadBytes  int64 = 20 * 1024 * 1024
	DefaultMaxPixels             = 50_000_000
	DefaultContentMaxWidth       = 1600
	DefaultThumbMaxWidth         = 400
)

type AuthMode string

const (
	AuthNone   AuthMode = "none"
	AuthAPIKey AuthMode = "apikey"
	AuthOIDC   AuthMode = "oidc"
)

type Config struct {
	Bind               string
	DBDSN              string
	StorageRoot        string
	MaxUploadBytes     int64
	MaxPixels          int
	ContentMaxWidth    int
	ThumbMaxWidth      int
	PublicMedia        bool
	AuthMode           AuthMode
	APIKeysFile        string
	CORSAllowedOrigins []string
	LogLevel           string
	SwaggerUIPath      string
	OpenAPIPath        string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	publicMedia, err := getBool("GANACHE_PUBLIC_MEDIA", true)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Bind:               getenv("GANACHE_BIND", DefaultBind),
		StorageRoot:        getenv("GANACHE_STORAGE_ROOT", DefaultStorageRoot),
		MaxUploadBytes:     getInt64("GANACHE_MAX_UPLOAD_BYTES", DefaultMaxUploadBytes),
		MaxPixels:          getInt("GANACHE_MAX_PIXELS", DefaultMaxPixels),
		ContentMaxWidth:    getInt("GANACHE_CONTENT_MAX_WIDTH", DefaultContentMaxWidth),
		ThumbMaxWidth:      getInt("GANACHE_THUMB_MAX_WIDTH", DefaultThumbMaxWidth),
		PublicMedia:        publicMedia,
		AuthMode:           AuthMode(getenv("GANACHE_AUTH_MODE", string(AuthAPIKey))),
		CORSAllowedOrigins: splitAndTrim(os.Getenv("GANACHE_CORS_ALLOWED_ORIGINS")),
		LogLevel:           os.Getenv("GANACHE_LOG_LEVEL"),
		SwaggerUIPath:      "/swagger",
		OpenAPIPath:        "/openapi.yaml",
	}

	cfg.DBDSN = os.Getenv("GANACHE_DB_DSN")
	if cfg.DBDSN == "" {
		return nil, fmt.Errorf("GANACHE_DB_DSN is required")
	}

	switch cfg.AuthMode {
	case AuthNone, AuthAPIKey, AuthOIDC:
	default:
		return nil, fmt.Errorf("invalid GANACHE_AUTH_MODE: %s", cfg.AuthMode)
	}
	if !cfg.PublicMedia && cfg.AuthMode != AuthAPIKey {
		return nil, fmt.Errorf("GANACHE_PUBLIC_MEDIA=false requires GANACHE_AUTH_MODE=apikey")
	}

	if cfg.AuthMode == AuthAPIKey {
		cfg.APIKeysFile = getenv("GANACHE_API_KEYS_FILE", "api-keys.yaml")
		if cfg.APIKeysFile == "" {
			return nil, fmt.Errorf("GANACHE_API_KEYS_FILE is required when GANACHE_AUTH_MODE=apikey")
		}
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
		fmt.Fprintf(os.Stderr, "warning: invalid value for %s=%q, using default %d\n", key, v, def)
	}
	return def
}

func getInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		i, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			return i
		}
		fmt.Fprintf(os.Stderr, "warning: invalid value for %s=%q, using default %d\n", key, v, def)
	}
	return def
}

func getBool(key string, def bool) (bool, error) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def, nil
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "y":
		return true, nil
	case "0", "false", "no", "n":
		return false, nil
	default:
		return false, fmt.Errorf("invalid %s: expected a boolean, got %q", key, v)
	}
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
