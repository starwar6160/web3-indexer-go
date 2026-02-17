# Multi-stage build for Go indexer
# Stage 1: Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git make

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the indexer binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o bin/indexer ./cmd/indexer

# Stage 2: Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates postgresql-client curl tzdata

# Create non-root user
RUN adduser -D -g '' appuser

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/bin/indexer .
# Copy migrations
COPY migrations ./migrations

# Set ownership
RUN chown -R appuser:appuser /app

# Switch to non-root user
USER appuser

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:8080/api/status || exit 1

# Run the indexer
ENTRYPOINT ["./indexer"]
