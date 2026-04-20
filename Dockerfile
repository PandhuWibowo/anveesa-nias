# ══════════════════════════════════════════════════════════════════════════════
# Anveesa Nias - Production Docker Image
# ══════════════════════════════════════════════════════════════════════════════

# Stage 1: Build frontend
FROM oven/bun:1 AS frontend-builder

WORKDIR /app/web

# Install dependencies
COPY web/package.json web/bun.lock* ./
RUN bun install --frozen-lockfile

# Build frontend
COPY web/ ./
RUN bun run build

# Stage 2: Build backend
FROM golang:1.22-alpine AS backend-builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Download Go modules
COPY server/go.mod server/go.sum ./
RUN go mod download

# Build binary
COPY server/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o nias-server .

# Stage 3: Final image
FROM alpine:3.19

WORKDIR /app

# Install runtime dependencies required by the app and native data-script runtimes
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    nodejs \
    npm \
    python3 \
    php83-cli

# Create non-root user
RUN adduser -D -u 1000 nias
RUN mkdir -p /app/data /app/backups && chown -R nias:nias /app

# Copy built artifacts
COPY --from=backend-builder /app/nias-server /app/
COPY --from=frontend-builder /app/web/dist /app/static

# Switch to non-root user
USER nias

# Environment defaults
ENV NIAS_ENV=production
ENV PORT=8080
ENV HOST=0.0.0.0
ENV BACKUP_DIR=/app/backups
ENV BACKUP_ENABLED=true
ENV DATA_SCRIPT_NODE_BIN=/usr/bin/node
ENV DATA_SCRIPT_PYTHON_BIN=/usr/bin/python3
ENV DATA_SCRIPT_PHP_BIN=/usr/bin/php

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Expose port
EXPOSE 8080

# Run server
CMD ["/app/nias-server"]
