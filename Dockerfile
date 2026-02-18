# ==============================================================================
# Web3 Indexer - Ultra-Lean Multi-Stage Docker Build
# ==============================================================================
# Stage 1: Build (Builder)
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# 📦 Leverage Docker cache: copy dependency files first
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# 🎯 Ultra-lean binary: strip debug symbols and reduce size by ~30%
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o bin/indexer ./cmd/indexer

# ==============================================================================
# Stage 2: Runtime (Production)
FROM alpine:3.21

# 🛡️ Minimal runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    curl \
    && rm -rf /var/cache/apk/*

# 🌍 Set timezone to Japan (Yokohama Lab)
ENV TZ=Asia/Tokyo

WORKDIR /app

# 📥 Copy binary from builder stage
COPY --from=builder /app/bin/indexer .

# 📋 Copy migrations
COPY migrations ./migrations

# 🛡️ Create non-root user for security
RUN adduser -D -g '' appuser && \
    mkdir -p logs && \
    chown -R appuser:appuser /app

# 🔒 Force secure directory permissions (0o750 = rwxr-x---)
RUN chmod 0750 logs

# Switch to non-root user (Defense in Depth)
USER appuser

# 🏥 Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:${PORT:-8080}/api/status || exit 1

# 🚀 Production entrypoint
ENTRYPOINT ["./indexer"]
