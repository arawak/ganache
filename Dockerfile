# Build stage
FROM golang:1.24-alpine AS builder
WORKDIR /src

ARG VERSION=dev

COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -trimpath \
    -o /out/ganache ./cmd/ganache

RUN cp openapi.yaml /out/openapi.yaml && \
    cp -r migrations /out/migrations

# Final image
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app

COPY --from=builder /out/ganache /app/ganache
COPY --from=builder /out/openapi.yaml /app/openapi.yaml
COPY --from=builder /out/migrations /app/migrations

ENV GANACHE_BIND=:8080 \
    GANACHE_STORAGE_ROOT=/data/storage

VOLUME ["/data/storage"]
EXPOSE 8080
ENTRYPOINT ["/app/ganache"]
