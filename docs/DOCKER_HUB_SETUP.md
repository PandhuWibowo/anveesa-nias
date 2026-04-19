# Docker Hub Setup - Complete Guide

## 🎯 Overview

Your application is now configured for Docker Hub distribution:

- **End users** → Pull pre-built images (no build needed)
- **You (maintainer)** → Build and publish to Docker Hub
- **Updates** → Simple pull and restart

---

## 📦 What Was Created

### For End Users (Pull from Docker Hub)

| File | Description |
|------|-------------|
| `docker-compose.prod.yml` | SQLite deployment (pull from Docker Hub) |
| `docker-compose.prod-postgres.yml` | PostgreSQL deployment (pull from Docker Hub) |
| `.env.production` | Production configuration template |
| `INSTALL.md` | Complete installation guide for users |

### For Maintainers (Build and Publish)

| File | Description |
|------|-------------|
| `Dockerfile` | Multi-stage build (frontend + backend) |
| `.dockerignore` | Optimize build (exclude unnecessary files) |
| `BUILD_AND_PUBLISH.md` | Instructions to build and push to Docker Hub |
| `docker-compose.yml` | Development build (local development) |
| `docker-compose.postgres.yml` | Development with PostgreSQL |

### Documentation

| File | Description |
|------|-------------|
| `README.md` | Updated with Docker Hub instructions |
| `DEPLOY_POSTGRES.md` | PostgreSQL deployment guide |
| `POSTGRES_IMPLEMENTATION.md` | PostgreSQL feature documentation |

---

## 🚀 For You (Maintainer) - First Time Setup

### Step 1: Create Docker Hub Repository

1. Go to https://hub.docker.com
2. Create account or login
3. Create repository:
   - Name: `nias`
   - Visibility: Public (or Private)
   - Description: "Database management studio"

Your image will be: `your-dockerhub-username/nias`

### Step 2: Build Multi-Platform Image

```bash
# Login to Docker Hub
docker login

# Create buildx builder (first time only)
docker buildx create --name multiplatform --use
docker buildx inspect --bootstrap

# Build and push for multiple platforms
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t your-dockerhub-username/nias:latest \
  -t your-dockerhub-username/nias:v1.0.0 \
  --push \
  .
```

Replace `your-dockerhub-username` with your actual Docker Hub username.

### Step 3: Update Config Files

Update these files to use your Docker Hub username:

**docker-compose.prod.yml:**
```yaml
services:
  nias:
    image: your-dockerhub-username/nias:latest  # Update this line
```

**docker-compose.prod-postgres.yml:**
```yaml
services:
  nias:
    image: your-dockerhub-username/nias:latest  # Update this line
```

**INSTALL.md, README.md:**
- Replace `anveesa/nias` with `your-dockerhub-username/nias`
- Replace repository URLs with your GitHub repository

### Step 4: Commit and Push

```bash
git add .
git commit -m "feat: add Docker Hub support"
git push origin main

# Create version tag
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

### Step 5: Verify on Docker Hub

1. Go to https://hub.docker.com/r/your-dockerhub-username/nias
2. Check:
   - ✅ Tags: `latest` and `v1.0.0` are visible
   - ✅ Platforms: `linux/amd64` and `linux/arm64` are available
   - ✅ Image size is reasonable (~50-100MB)

---

## 👥 For End Users - Installation

### Simple Installation (SQLite)

```bash
# 1. Create directory
mkdir anveesa-nias && cd anveesa-nias

# 2. Download config
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/.env.production -o .env
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/deploy/compose/docker-compose.prod.yml

# 3. Edit .env - set required values
nano .env
# Set: JWT_SECRET, NIAS_ENCRYPTION_KEY, DEFAULT_ADMIN_PASSWORD

# 4. Start
docker-compose -f docker-compose.prod.yml up -d

# 5. Access
open http://localhost:8080
```

### Production Installation (PostgreSQL)

```bash
# Download PostgreSQL compose file
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/deploy/compose/docker-compose.prod-postgres.yml

# Edit .env - also set POSTGRES_PASSWORD
nano .env

# Start
docker-compose -f docker-compose.prod-postgres.yml up -d
```

---

## 🔄 Update Process

### For End Users (Super Simple!)

```bash
# Pull latest image
docker pull your-dockerhub-username/nias:latest

# Restart
docker-compose -f docker-compose.prod.yml restart
# OR
docker-compose -f docker-compose.prod-postgres.yml restart
```

**Migrations run automatically!** No manual steps needed.

### For Maintainers (When You Make Changes)

```bash
# 1. Make code changes
# 2. Test locally
# 3. Build and push new image
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t your-dockerhub-username/nias:latest \
  -t your-dockerhub-username/nias:v1.1.0 \
  --push \
  .

# 4. Users just pull and restart (see above)
```

---

## 📋 File Structure Summary

```
anveesa-nias/
├── Dockerfile                           # Multi-stage build
├── .dockerignore                        # Optimize builds
│
├── docker-compose.yml                   # Development (build locally)
├── docker-compose.dev.yml              # Development variant
├── docker-compose.postgres.yml         # Development with PostgreSQL
│
├── docker-compose.prod.yml             # Production SQLite (Docker Hub)
├── docker-compose.prod-postgres.yml    # Production PostgreSQL (Docker Hub)
│
├── .env.example                         # Development config
├── .env.postgres.example               # PostgreSQL config
├── .env.production                      # Production config template
│
├── README.md                            # Main documentation
├── INSTALL.md                          # User installation guide
├── BUILD_AND_PUBLISH.md               # Maintainer build guide
├── DEPLOY_POSTGRES.md                  # PostgreSQL deployment
├── POSTGRES_IMPLEMENTATION.md          # PostgreSQL features
└── DOCKER_HUB_SETUP.md                # This file
```

---

## 🎯 Key Differences

### docker-compose.yml (Development - Build Locally)

```yaml
services:
  nias:
    build:
      context: .
      dockerfile: Dockerfile  # Builds from source
```

**For:** Developers working on code

### docker-compose.prod.yml (Production - Pull from Docker Hub)

```yaml
services:
  nias:
    image: your-dockerhub-username/nias:latest  # Pulls pre-built
```

**For:** End users deploying

---

## ✅ Checklist Before First Release

### Code Ready

- [ ] All features working
- [ ] Tests passing
- [ ] Documentation complete
- [ ] Version numbers updated

### Docker Hub

- [ ] Docker Hub account created
- [ ] Repository created (public or private)
- [ ] Logged in: `docker login`
- [ ] Buildx setup: `docker buildx create --name multiplatform --use`

### Build and Push

- [ ] Multi-platform build successful
- [ ] Image pushed to Docker Hub
- [ ] Both platforms available (amd64, arm64)
- [ ] Tags correct (latest, version)

### Configuration Files

- [ ] `docker-compose.prod.yml` uses correct image name
- [ ] `docker-compose.prod-postgres.yml` uses correct image name
- [ ] `.env.production` has secure defaults
- [ ] All documentation updated with correct image name and URLs

### Testing

- [ ] Pulled image works: `docker pull your-dockerhub-username/nias:latest`
- [ ] SQLite deployment tested
- [ ] PostgreSQL deployment tested
- [ ] Default admin account created
- [ ] Can login and create connections
- [ ] Update process tested (pull + restart)

### Documentation

- [ ] README.md updated
- [ ] INSTALL.md complete
- [ ] All file URLs updated
- [ ] GitHub repository created
- [ ] Release notes written

---

## 🚀 Release Process

### 1. Prepare Release

```bash
# Ensure clean state
git status

# Update version
# (in main.go, package.json, etc.)

# Commit
git add .
git commit -m "chore: bump version to v1.0.0"
git push
```

### 2. Build and Push

```bash
# Login
docker login

# Build and push
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t your-dockerhub-username/nias:latest \
  -t your-dockerhub-username/nias:v1.0.0 \
  --push \
  .
```

### 3. Tag and Release

```bash
# Create Git tag
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0

# Create GitHub release
# Go to GitHub → Releases → Create new release
```

### 4. Announce

- Update README.md with latest version
- Notify users
- Update documentation site (if any)

---

## 📊 Image Details

### Size

- Frontend build: ~20MB (minified)
- Backend binary: ~15MB (compiled Go)
- Base image: ~10MB (Alpine Linux)
- **Total: ~50MB** (compressed)

### Platforms

- `linux/amd64` - Intel/AMD processors
- `linux/arm64` - ARM processors (Apple Silicon, AWS Graviton)

### Tags

- `latest` - Latest stable release
- `v1.0.0` - Specific version
- `v1.0` - Minor version
- `v1` - Major version
- `dev` - Development/testing (optional)

---

## 🆘 Troubleshooting

### Build Fails

```bash
# Clean Docker
docker system prune -a

# Clean buildx cache
docker buildx prune

# Recreate builder
docker buildx rm multiplatform
docker buildx create --name multiplatform --use
```

### Push Fails - Authentication

```bash
# Re-login
docker logout
docker login

# Use access token instead of password
# Generate at: https://hub.docker.com/settings/security
```

### Image Too Large

Check what's in the image:
```bash
docker history your-dockerhub-username/nias:latest
```

Common issues:
- Source code included (check .dockerignore)
- node_modules included (should be excluded)
- Development dependencies (use multi-stage build)

---

## 📚 Next Steps

1. **First Release:**
   - Follow "Release Process" above
   - Build and push v1.0.0
   - Test end-user installation

2. **Setup CI/CD (Optional):**
   - GitHub Actions to auto-build on tag push
   - See `BUILD_AND_PUBLISH.md` for workflow example

3. **Documentation Site:**
   - Create docs site (GitHub Pages, GitBook, etc.)
   - Link from README

4. **Community:**
   - Add CONTRIBUTING.md
   - Setup issue templates
   - Add CODE_OF_CONDUCT.md

---

## 🎉 Summary

**What You Did:**
- ✅ Created production-ready Docker setup
- ✅ Separated development and production configs
- ✅ Pre-built images for easy deployment
- ✅ Multi-platform support (Intel, ARM, Apple Silicon)
- ✅ Simple update process (pull + restart)
- ✅ Complete documentation for users and maintainers

**What Users Get:**
- ✅ Pull image from Docker Hub (no build needed)
- ✅ 3-command installation
- ✅ Automatic migrations
- ✅ Easy updates

**Result:**
- 🚀 Professional, production-ready deployment
- 📦 Simple installation process
- 🔄 Easy maintenance and updates
- 📚 Complete documentation

---

## 📖 Quick Reference

```bash
# Maintainer: Build and publish
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t your-username/nias:latest \
  -t your-username/nias:v1.0.0 \
  --push .

# User: Install (SQLite)
curl -O .env && curl -O docker-compose.prod.yml
nano .env  # Configure
docker-compose -f docker-compose.prod.yml up -d

# User: Install (PostgreSQL)
curl -O .env && curl -O docker-compose.prod-postgres.yml
nano .env  # Configure  
docker-compose -f docker-compose.prod-postgres.yml up -d

# User: Update
docker pull your-username/nias:latest
docker-compose -f docker-compose.prod*.yml restart
```

**You're ready to publish!** 🎉
