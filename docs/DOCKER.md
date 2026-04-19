# Docker Deployment Guide

This guide covers deploying Anveesa Nias using Docker and Docker Compose.

## Prerequisites

- Docker Engine 20.10+
- Docker Compose 2.0+

## Quick Start

### 1. Clone and Configure

```bash
# Clone the repository
git clone <your-repository-url>
cd anveesa-nias

# Copy environment template
cp .env.example .env
```

### 2. Configure Environment Variables

Edit `.env` and set the following **required** production variables:

```bash
# Environment
NIAS_ENV=production

# Security - REQUIRED in production
JWT_SECRET=your-super-secure-jwt-secret-min-32-chars
NIAS_ENCRYPTION_KEY=your-32-byte-encryption-key-here

# Default Admin Account - CHANGE THESE!
DEFAULT_ADMIN_USERNAME=admin
DEFAULT_ADMIN_PASSWORD=YourSecurePassword123!

# CORS (adjust to your domain)
CORS_ORIGIN=https://yourdomain.com
```

**🔒 Security Best Practices:**

Generate secure keys:
```bash
# Generate JWT secret (64 chars)
openssl rand -hex 32

# Generate encryption key (32 chars)
openssl rand -hex 16
```

### 3. Start the Application

```bash
# Start in detached mode
docker-compose up -d

# View logs
docker-compose logs -f nias

# Check status
docker-compose ps
```

The application will be available at `http://localhost:8080`

## Default Admin Account

### Automatic Creation

When the application starts for the first time with an empty database:

1. A default admin account is **automatically created**
2. Credentials are printed to the console logs
3. You can customize the credentials via environment variables

### Check Logs for Credentials

```bash
docker-compose logs nias | grep "Default admin"
```

Example output:
```
✓ Default admin account created: admin
  Username: admin
  Password: Admin123! (CHANGE THIS IMMEDIATELY!)
  Please change the password after first login!
```

### Customize Default Credentials

**Before first run**, set these in your `.env` file or `docker-compose.yml`:

```env
DEFAULT_ADMIN_USERNAME=myadmin
DEFAULT_ADMIN_PASSWORD=MySecurePassword123!
```

Or via Docker Compose:

```yaml
environment:
  - DEFAULT_ADMIN_USERNAME=myadmin
  - DEFAULT_ADMIN_PASSWORD=MySecurePassword123!
```

**⚠️ IMPORTANT:**
- In **production**, you **MUST** set a secure `DEFAULT_ADMIN_PASSWORD`
- The default password `Admin123!` will cause startup failure in production if not changed
- Change the admin password immediately after first login

## Environment Variables Reference

### Required (Production)

| Variable | Description | Example |
|----------|-------------|---------|
| `JWT_SECRET` | JWT signing key (min 32 chars) | `$(openssl rand -hex 32)` |
| `NIAS_ENCRYPTION_KEY` | Credentials encryption key (32 chars) | `$(openssl rand -hex 16)` |
| `DEFAULT_ADMIN_PASSWORD` | Initial admin password | `SecurePass123!` |

### Optional Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `NIAS_ENV` | `production` | Environment mode |
| `PORT` | `8080` | Server port |
| `HOST` | `0.0.0.0` | Server bind address |
| `DEFAULT_ADMIN_USERNAME` | `admin` | Initial admin username |
| `CORS_ORIGIN` | `http://localhost:8080` | Allowed CORS origins |
| `BACKUP_ENABLED` | `true` | Enable automatic backups |
| `BACKUP_HOURS` | `24` | Backup interval |
| `JWT_EXPIRY_HOURS` | `72` | JWT token expiry |
| `RATE_LIMIT_ENABLED` | `true` | Enable rate limiting |
| `RATE_LIMIT_REQUESTS` | `100` | Max requests per window |
| `RATE_LIMIT_WINDOW` | `60` | Rate limit window (seconds) |

## Data Persistence

### Volumes

Docker Compose creates two named volumes:

```yaml
volumes:
  nias-data:       # Application database
  nias-backups:    # Automatic backups
```

### Backup and Restore

#### Manual Backup

```bash
# Create backup
docker-compose exec nias cp /app/data/nias.db /app/backups/manual_backup_$(date +%Y%m%d).db

# Or copy from host
docker cp anveesa-nias:/app/data/nias.db ./backup_$(date +%Y%m%d).db
```

#### Restore from Backup

```bash
# Stop the application
docker-compose down

# Copy backup to volume
docker run --rm -v nias-data:/data -v $(pwd):/backup alpine \
  cp /backup/your_backup.db /data/nias.db

# Start the application
docker-compose up -d
```

### Volume Management

```bash
# Inspect volumes
docker volume inspect anveesa-nias_nias-data
docker volume inspect anveesa-nias_nias-backups

# Backup volume to host
docker run --rm -v anveesa-nias_nias-data:/data -v $(pwd):/backup alpine \
  tar czf /backup/nias-data-backup.tar.gz -C /data .

# Restore volume from host
docker run --rm -v anveesa-nias_nias-data:/data -v $(pwd):/backup alpine \
  tar xzf /backup/nias-data-backup.tar.gz -C /data
```

## Production Deployment

### 1. Use HTTPS

Add a reverse proxy (Nginx, Traefik, or Caddy) in front of the application:

**Example with Traefik:**

```yaml
services:
  nias:
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.nias.rule=Host(`db.yourdomain.com`)"
      - "traefik.http.routers.nias.entrypoints=websecure"
      - "traefik.http.routers.nias.tls.certresolver=letsencrypt"
```

### 2. Restrict Network Access

```yaml
services:
  nias:
    networks:
      - internal
    # Remove port mapping if behind reverse proxy
    # ports:
    #   - "8080:8080"
```

### 3. Set Resource Limits

```yaml
services:
  nias:
    deploy:
      resources:
        limits:
          cpus: '1'
          memory: 512M
        reservations:
          cpus: '0.5'
          memory: 256M
```

### 4. Enable Log Rotation

```yaml
services:
  nias:
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
```

### 5. Health Checks

The container includes built-in health checks:

```yaml
healthcheck:
  test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8080/health"]
  interval: 30s
  timeout: 5s
  retries: 3
```

## Troubleshooting

### Check Logs

```bash
# View all logs
docker-compose logs -f

# View last 100 lines
docker-compose logs --tail=100 nias

# View logs since 1 hour ago
docker-compose logs --since 1h nias
```

### Database Locked Error

If you see `SQLITE_BUSY` errors:

```bash
# Restart the application
docker-compose restart nias
```

The latest version includes fixes for SQLite locking issues (WAL mode + busy timeout).

### Cannot Login - Wrong Credentials

```bash
# Check if admin was created
docker-compose logs nias | grep "Default admin"

# Reset by removing database (⚠️ destroys all data!)
docker-compose down -v
docker-compose up -d
```

### Permission Denied Errors

```bash
# Fix volume permissions
docker-compose down
docker volume rm anveesa-nias_nias-data
docker-compose up -d
```

## Updating

### Update to Latest Version

```bash
# Pull latest changes
git pull origin main

# Rebuild and restart
docker-compose down
docker-compose build --no-cache
docker-compose up -d
```

### Zero-Downtime Update (with backup)

```bash
# Backup current database
docker cp anveesa-nias:/app/data/nias.db ./backup_before_update.db

# Pull and rebuild
git pull
docker-compose build

# Stop old, start new
docker-compose up -d

# Verify
docker-compose logs -f
```

## Monitoring

### Container Stats

```bash
docker stats anveesa-nias
```

### Health Check

```bash
curl http://localhost:8080/health
curl http://localhost:8080/ready
curl http://localhost:8080/version
```

## Uninstall

### Remove Application (Keep Data)

```bash
docker-compose down
```

### Complete Removal (Including Data)

```bash
# ⚠️ WARNING: This will delete all data permanently!
docker-compose down -v
docker rmi anveesa-nias:latest
```

## Support

For issues, questions, or contributions:
- GitHub Issues: [your-repo-url/issues]
- Documentation: [your-docs-url]
