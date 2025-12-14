BINARY ?= ganache
PKG := github.com/example/ganache
LDFLAGS ?=

GANACHE_DB_DSN ?= root:root@tcp(localhost:3306)/ganache?parseTime=true&multiStatements=true
GANACHE_STORAGE_ROOT ?= $(PWD)/.data/storage
GANACHE_BIND ?= :8080
GANACHE_AUTH_MODE ?= none
GANACHE_PUBLIC_MEDIA ?= true
GANACHE_MAX_UPLOAD_BYTES ?= 20000000
GANACHE_MAX_PIXELS ?= 50000000

.PHONY: build run test race itest gen migrate-up migrate-down lint fmt vet snapshot release

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/ganache

run: gen
	GANACHE_DB_DSN=$(GANACHE_DB_DSN) \
	GANACHE_STORAGE_ROOT=$(GANACHE_STORAGE_ROOT) \
	GANACHE_BIND=$(GANACHE_BIND) \
	GANACHE_AUTH_MODE=$(GANACHE_AUTH_MODE) \
	GANACHE_PUBLIC_MEDIA=$(GANACHE_PUBLIC_MEDIA) \
	GANACHE_MAX_UPLOAD_BYTES=$(GANACHE_MAX_UPLOAD_BYTES) \
	GANACHE_MAX_PIXELS=$(GANACHE_MAX_PIXELS) \
		go run ./cmd/ganache

test:
	go test ./...

race:
	go test -race ./...

itest:
	go test -tags=integration ./...

gen:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen --config internal/httpapi/oapi.cfg.yaml openapi.yaml

migrate-up:
	GANACHE_DB_DSN='$(GANACHE_DB_DSN)' go run ./cmd/migrate -dir=up

migrate-down:
	GANACHE_DB_DSN='$(GANACHE_DB_DSN)' go run ./cmd/migrate -dir=down

lint:
	golangci-lint run

fmt:
	gofmt -w .

vet:
	go vet ./...

snapshot:
	goreleaser release --snapshot --clean

release:
	goreleaser release --clean
