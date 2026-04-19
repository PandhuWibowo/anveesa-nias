# Build and Publish to Docker Hub

Instructions for maintainers to build and publish Docker images.

## 🎯 Overview

End users will pull pre-built images from Docker Hub:
- **Image:** `anveesa/nias`
- **Tags:** `latest`, `v1.0.0`, `dev`

## 📋 Prerequisites

1. Docker installed
2. Docker Hub account
3. Access to `anveesa` organization on Docker Hub (or use your own)

## 🔑 Docker Hub Login

```bash
docker login

# Enter your Docker Hub username and password
```

## 🏗️ Build Image

### Build for Current Platform

```bash
# Build the image
docker build -t anveesa/nias:latest .

# Tag with version
docker build -t anveesa/nias:v1.0.0 .
```

### Build for Multiple Platforms (Recommended)

Build for both AMD64 (Intel/AMD) and ARM64 (Apple Silicon, ARM servers):

```bash
# Create buildx builder (first time only)
docker buildx create --name multiplatform --use
docker buildx inspect --bootstrap

# Build and push for multiple platforms
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t anveesa/nias:latest \
  -t anveesa/nias:v1.0.0 \
  --push \
  .
```

## 📤 Push to Docker Hub

### Push Single Platform

```bash
# Push latest
docker push anveesa/nias:latest

# Push specific version
docker push anveesa/nias:v1.0.0
```

### Push Multiple Platforms

The `--push` flag in `buildx` command above already pushes.

## 🏷️ Tagging Strategy

### Version Tags

```bash
# Major.Minor.Patch
docker tag anveesa/nias:latest anveesa/nias:v1.0.0
docker tag anveesa/nias:latest anveesa/nias:v1.0
docker tag anveesa/nias:latest anveesa/nias:v1

docker push anveesa/nias:v1.0.0
docker push anveesa/nias:v1.0
docker push anveesa/nias:v1
```

### Development Tags

```bash
# Development/testing
docker tag anveesa/nias:latest anveesa/nias:dev
docker push anveesa/nias:dev
```

## 🚀 Release Process

### 1. Prepare Release

```bash
# Ensure all changes are committed
git status

# Update version in code if needed
# (version in main.go, package.json, etc.)

# Tag the release
git tag -a v1.0.0 -m "Release version 1.0.0"
git push origin v1.0.0
```

### 2. Build and Test Locally

```bash
# Build image
docker build -t anveesa/nias:v1.0.0 .

# Test image locally
docker run -d \
  -p 8080:8080 \
  -e JWT_SECRET=test-secret-min-32-characters-here \
  -e NIAS_ENCRYPTION_KEY=test-key-exactly-32-chars-here \
  -e DEFAULT_ADMIN_PASSWORD=TestPass123! \
  anveesa/nias:v1.0.0

# Verify
curl http://localhost:8080/health
curl http://localhost:8080/version

# Stop test container
docker stop $(docker ps -q --filter ancestor=anveesa/nias:v1.0.0)
```

### 3. Build for Multiple Platforms

```bash
# Build and push for all platforms
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t anveesa/nias:latest \
  -t anveesa/nias:v1.0.0 \
  -t anveesa/nias:v1.0 \
  -t anveesa/nias:v1 \
  --push \
  .
```

### 4. Verify on Docker Hub

Visit: https://hub.docker.com/r/anveesa/nias

Check:
- ✅ All tags present
- ✅ Multiple platforms available (amd64, arm64)
- ✅ Image size reasonable
- ✅ Latest tag points to correct version

### 5. Update Documentation

```bash
# Update CHANGELOG.md
# Update README.md with new version
# Commit changes
git add CHANGELOG.md README.md
git commit -m "chore: update docs for v1.0.0"
git push
```

### 6. Create GitHub Release

1. Go to GitHub repository
2. Click "Releases" → "Create a new release"
3. Choose tag: `v1.0.0`
4. Title: `v1.0.0 - Release Name`
5. Description: Changelog highlights
6. Publish release

## 🔄 Automated CI/CD (Optional)

### GitHub Actions Example

Create `.github/workflows/docker-publish.yml`:

```yaml
name: Docker Build and Push

on:
  push:
    branches: [ main ]
    tags: [ 'v*' ]
  pull_request:
    branches: [ main ]

env:
  IMAGE_NAME: anveesa/nias

jobs:
  build:
    runs-on: ubuntu-latest
    
    steps:
      - name: Checkout code
        uses: actions/checkout@v3
      
      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v2
      
      - name: Login to Docker Hub
        if: github.event_name != 'pull_request'
        uses: docker/login-action@v2
        with:
          username: ${{ secrets.DOCKERHUB_USERNAME }}
          password: ${{ secrets.DOCKERHUB_TOKEN }}
      
      - name: Extract metadata
        id: meta
        uses: docker/metadata-action@v4
        with:
          images: ${{ env.IMAGE_NAME }}
          tags: |
            type=ref,event=branch
            type=ref,event=pr
            type=semver,pattern={{version}}
            type=semver,pattern={{major}}.{{minor}}
            type=semver,pattern={{major}}
            type=raw,value=latest,enable={{is_default_branch}}
      
      - name: Build and push
        uses: docker/build-push-action@v4
        with:
          context: .
          platforms: linux/amd64,linux/arm64
          push: ${{ github.event_name != 'pull_request' }}
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

### Setup GitHub Secrets

Add to repository secrets:
- `DOCKERHUB_USERNAME` - Your Docker Hub username
- `DOCKERHUB_TOKEN` - Docker Hub access token (not password!)

Generate token: https://hub.docker.com/settings/security

## 🧪 Testing Images

### Test SQLite Deployment

```bash
docker run -d \
  --name nias-test \
  -p 8080:8080 \
  -e JWT_SECRET=$(openssl rand -hex 32) \
  -e NIAS_ENCRYPTION_KEY=$(openssl rand -hex 16) \
  -e DEFAULT_ADMIN_PASSWORD=TestPass123! \
  anveesa/nias:latest

# Test
curl http://localhost:8080/health
curl http://localhost:8080/version

# Cleanup
docker stop nias-test && docker rm nias-test
```

### Test PostgreSQL Deployment

```bash
# Start PostgreSQL
docker run -d \
  --name postgres-test \
  -e POSTGRES_DB=nias \
  -e POSTGRES_USER=nias \
  -e POSTGRES_PASSWORD=testpass \
  postgres:16-alpine

# Wait for PostgreSQL to be ready
sleep 5

# Start Nias
docker run -d \
  --name nias-test \
  --link postgres-test:postgres \
  -p 8080:8080 \
  -e DB_DRIVER=postgres \
  -e DATABASE_URL=postgres://nias:testpass@postgres:5432/nias?sslmode=disable \
  -e JWT_SECRET=$(openssl rand -hex 32) \
  -e NIAS_ENCRYPTION_KEY=$(openssl rand -hex 16) \
  -e DEFAULT_ADMIN_PASSWORD=TestPass123! \
  anveesa/nias:latest

# Test
curl http://localhost:8080/health

# Cleanup
docker stop nias-test postgres-test
docker rm nias-test postgres-test
```

## 📊 Image Size Optimization

Current image size: ~50-100MB (after multi-stage build)

### Check Image Size

```bash
docker images anveesa/nias
```

### Optimization Tips

Already implemented:
- ✅ Multi-stage build
- ✅ Alpine-based images
- ✅ Minimal runtime dependencies
- ✅ Compiled binary (no source code)

## 🗂️ Image Manifest

View platforms:

```bash
docker manifest inspect anveesa/nias:latest
```

Should show:
- `linux/amd64` - Intel/AMD processors
- `linux/arm64` - ARM processors (Apple Silicon, AWS Graviton)

## 📝 Checklist for Each Release

- [ ] All tests pass locally
- [ ] Version updated in code
- [ ] Git tag created and pushed
- [ ] Image built for multiple platforms
- [ ] Image tested on both amd64 and arm64
- [ ] Pushed to Docker Hub with all tags
- [ ] Verified on Docker Hub (all tags, platforms)
- [ ] Documentation updated (CHANGELOG, README)
- [ ] GitHub release created
- [ ] End-user installation tested

## 🔐 Security Scanning

Scan images for vulnerabilities:

```bash
# Using Docker Scout (built-in)
docker scout cves anveesa/nias:latest

# Using Trivy
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  aquasec/trivy:latest \
  image anveesa/nias:latest
```

## 📚 Docker Hub Repository Settings

### Description

```
Anveesa Nias - Modern database management studio

A fast, local-first database management tool for PostgreSQL, MySQL, SQLite, and SQL Server. Built with Vue 3 and Go.

Features:
- Multi-database support
- SQL query editor with history
- Schema browser and data viewer
- Authentication and RBAC
- Approval workflows
- Audit logging

Documentation: https://github.com/your-org/anveesa-nias
```

### README (Docker Hub)

Link to installation guide:
```markdown
# Anveesa Nias

Modern database management studio for PostgreSQL, MySQL, SQLite, and SQL Server.

## Quick Start

### SQLite (Simple)

```bash
docker run -d -p 8080:8080 \
  -e JWT_SECRET=$(openssl rand -hex 32) \
  -e NIAS_ENCRYPTION_KEY=$(openssl rand -hex 16) \
  -e DEFAULT_ADMIN_PASSWORD=YourPassword123! \
  anveesa/nias:latest
```

### PostgreSQL (Production)

See: https://github.com/your-org/anveesa-nias/blob/main/docs/INSTALL.md

## Documentation

- Installation: https://github.com/your-org/anveesa-nias/blob/main/docs/INSTALL.md
- PostgreSQL: https://github.com/your-org/anveesa-nias/blob/main/docs/DEPLOY_POSTGRES.md
- GitHub: https://github.com/your-org/anveesa-nias
```

## 🆘 Troubleshooting Build Issues

### Build Fails - No Space

```bash
# Clean up Docker
docker system prune -a

# Clean buildx cache
docker buildx prune
```

### Platform Not Supported

```bash
# Check available platforms
docker buildx ls

# Create new builder
docker buildx rm multiplatform
docker buildx create --name multiplatform --use
```

### Push Fails - Authentication

```bash
# Re-login
docker logout
docker login

# Use token instead of password
# Generate at: https://hub.docker.com/settings/security
```

## 📖 References

- [Docker Build Documentation](https://docs.docker.com/engine/reference/commandline/build/)
- [Docker Buildx](https://docs.docker.com/buildx/working-with-buildx/)
- [Docker Hub](https://hub.docker.com/)
- [Multi-platform Images](https://docs.docker.com/build/building/multi-platform/)
