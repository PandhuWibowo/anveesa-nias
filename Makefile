.PHONY: dev build clean install test lint docker docker-build docker-tag docker-push docker-up docker-down docker-logs

COMPOSE_FILE := deploy/compose/docker-compose.yml
IMAGE_NAME ?= anveesa/nias
IMAGE_TAG ?= latest
PUSH_LATEST ?= 0

# ══════════════════════════════════════════════════════════════════════════════
# Development
# ══════════════════════════════════════════════════════════════════════════════

# Start both the Go server and Vite dev server concurrently
dev:
	@trap 'kill 0' INT; \
	  (cd server && go run .) & \
	  (until nc -z localhost 8080 2>/dev/null; do sleep 0.3; done; cd web && bun run dev) & \
	  wait

# Frontend only
dev-web:
	@cd web && bun run dev

# Backend only
dev-server:
	@cd server && go run .

# Install all dependencies
install:
	@echo "Installing frontend dependencies..."
	@cd web && bun install
	@echo "Installing backend dependencies..."
	@cd server && go mod tidy
	@echo "Done!"

# ══════════════════════════════════════════════════════════════════════════════
# Build
# ══════════════════════════════════════════════════════════════════════════════

# Build frontend and backend
build:
	@echo "Building frontend..."
	@cd web && bun run build
	@echo "Building backend..."
	@mkdir -p bin
	@cd server && go build -ldflags="-s -w" -o ../bin/nias .
	@echo "Build complete: bin/nias"

# Build for production with version info
build-prod:
	@echo "Building for production..."
	@cd web && bun run build
	@mkdir -p bin
	@cd server && go build -ldflags="-s -w -X main.version=$$(git describe --tags --always 2>/dev/null || echo 'dev') -X main.buildTime=$$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o ../bin/nias .
	@echo "Production build complete: bin/nias"

# Run the compiled binary
start:
	@./bin/nias

# ══════════════════════════════════════════════════════════════════════════════
# Testing & Quality
# ══════════════════════════════════════════════════════════════════════════════

# Run tests
test:
	@cd server && go test -v ./...
	@cd web && bun run type-check

# Lint code
lint:
	@cd server && go vet ./...
	@cd web && bun run lint 2>/dev/null || true

# Security check
security:
	@echo "Running security checks..."
	@cd server && go list -json -m all | docker run --rm -i sonatypecommunity/nancy:latest sleuth 2>/dev/null || true
	@echo "Done!"

# ══════════════════════════════════════════════════════════════════════════════
# Docker
# ══════════════════════════════════════════════════════════════════════════════

# Build Docker image
docker-build:
	@docker build -t $(IMAGE_NAME):$(IMAGE_TAG) .

# Add latest tag to an existing image tag
docker-tag:
	@docker tag $(IMAGE_NAME):$(IMAGE_TAG) $(IMAGE_NAME):latest

# Build and push Docker image with optional latest tag
docker-push: docker-build
	@docker push $(IMAGE_NAME):$(IMAGE_TAG)
	@if [ "$(PUSH_LATEST)" = "1" ]; then \
		docker tag $(IMAGE_NAME):$(IMAGE_TAG) $(IMAGE_NAME):latest; \
		docker push $(IMAGE_NAME):latest; \
	fi

# Start with Docker Compose
docker-up:
	@docker-compose -f $(COMPOSE_FILE) up -d

# Stop Docker Compose
docker-down:
	@docker-compose -f $(COMPOSE_FILE) down

# View Docker logs
docker-logs:
	@docker-compose -f $(COMPOSE_FILE) logs -f

# Full Docker rebuild and restart
docker-restart: docker-down docker-build docker-up

# ══════════════════════════════════════════════════════════════════════════════
# Database
# ══════════════════════════════════════════════════════════════════════════════

# Backup database
backup:
	@mkdir -p backups
	@cp data.db backups/nias_backup_$$(date +%Y%m%d_%H%M%S).db
	@echo "Backup created in backups/"

# ══════════════════════════════════════════════════════════════════════════════
# Utilities
# ══════════════════════════════════════════════════════════════════════════════

# Generate secure secrets
secrets:
	@echo "JWT_SECRET=$$(openssl rand -hex 32)"
	@echo "NIAS_ENCRYPTION_KEY=$$(openssl rand -hex 16)"

# Clean build artifacts
clean:
	@rm -rf bin web/dist
	@echo "Cleaned!"

# Show help
help:
	@echo "Anveesa Nias - Database Management Tool"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Development:"
	@echo "  dev          Start frontend and backend in development mode"
	@echo "  dev-web      Start frontend only"
	@echo "  dev-server   Start backend only"
	@echo "  install      Install all dependencies"
	@echo ""
	@echo "Build:"
	@echo "  build        Build frontend and backend"
	@echo "  build-prod   Build for production with version info"
	@echo "  start        Run the compiled binary"
	@echo ""
	@echo "Docker:"
	@echo "  docker-build Build Docker image (IMAGE_NAME=anveesa/nias IMAGE_TAG=v1.0.0)"
	@echo "  docker-tag   Tag an existing image as latest"
	@echo "  docker-push  Build and push image (set PUSH_LATEST=1 to also push latest)"
	@echo "  docker-up    Start with Docker Compose"
	@echo "  docker-down  Stop Docker Compose"
	@echo "  docker-logs  View Docker logs"
	@echo ""
	@echo "Utilities:"
	@echo "  test         Run tests"
	@echo "  lint         Run linters"
	@echo "  backup       Backup database"
	@echo "  secrets      Generate secure secrets"
	@echo "  clean        Clean build artifacts"
