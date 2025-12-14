package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"

	"github.com/arawak/ganache/internal/config"
	"github.com/arawak/ganache/internal/httpapi"
	"github.com/arawak/ganache/internal/media"
	"github.com/arawak/ganache/internal/store"
	"github.com/arawak/ganache/migrations"
)

var version = "dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil)).With("version", version)

	var apiKeys *httpapi.APIKeyStore
	if cfg.AuthMode == config.AuthAPIKey {
		apiKeys, err = httpapi.LoadAPIKeys(cfg.APIKeysFile)
		if err != nil {
			logger.Error("failed to load api keys", "error", err)
			os.Exit(1)
		}
	}

	db, err := sqlx.Open("mysql", cfg.DBDSN)
	if err != nil {
		logger.Error("failed to open db", "error", err)
		os.Exit(1)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := migrations.Up(cfg.DBDSN); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	storeSvc := store.New(db)
	mediaMgr := media.NewManager(cfg.StorageRoot)
	router := httpapi.NewRouter(cfg, storeSvc, mediaMgr, apiKeys, logger)

	srv := &http.Server{Addr: cfg.Bind, Handler: router}
	go func() {
		logger.Info("server starting", "addr", cfg.Bind)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	logger.Info("shutting down gracefully")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}

	if err := db.Close(); err != nil {
		logger.Error("database close error", "error", err)
	}
}
