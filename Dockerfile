# Build stage
FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/ganache ./cmd/ganache

# Final image
FROM alpine:3.20
WORKDIR /app
COPY --from=builder /out/ganache /app/ganache
COPY openapi.yaml /app/openapi.yaml
COPY migrations /app/migrations
ENV GANACHE_BIND=:8080
EXPOSE 8080
ENTRYPOINT ["/app/ganache"]
