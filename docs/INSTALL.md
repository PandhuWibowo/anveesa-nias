# Installation Guide - Docker Hub

Quick installation using pre-built Docker images from Docker Hub.

## 🚀 Quick Install (3 Steps)

### Step 1: Create Configuration

```bash
# Create a directory for your installation
mkdir anveesa-nias && cd anveesa-nias

# Download production config
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/.env.production
mv .env.production .env

# Download docker-compose file (choose one)
# For SQLite (simple, single container):
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/deploy/compose/docker-compose.prod.yml

# OR for PostgreSQL (recommended for production):
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/deploy/compose/docker-compose.prod-postgres.yml
```

### Step 2: Configure Security

Edit `.env` and set **required** values:

```bash
nano .env

# Set these values:
JWT_SECRET=$(openssl rand -hex 32)
NIAS_ENCRYPTION_KEY=$(openssl rand -hex 16)
DEFAULT_ADMIN_PASSWORD=YourSecurePassword123!

# For PostgreSQL, also set:
POSTGRES_PASSWORD=$(openssl rand -hex 32)
```

### Step 3: Start Application

```bash
# For SQLite:
docker-compose -f docker-compose.prod.yml up -d

# OR for PostgreSQL:
docker-compose -f docker-compose.prod-postgres.yml up -d

# Check logs
docker logs -f anveesa-nias
```

**Access:** `http://localhost:8080`  
**Login:** Use `DEFAULT_ADMIN_USERNAME` and `DEFAULT_ADMIN_PASSWORD` from your `.env`

---

## 📦 Installation Options

### Option 1: SQLite (Simple)

**Best for:**
- Small teams (1-10 users)
- Development/testing
- Quick setup
- Low traffic

**Pros:**
- ✅ Single container
- ✅ Zero configuration
- ✅ Very simple

**Cons:**
- ⚠️ Not recommended for high concurrency

**Install:**
```bash
# Download config
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/.env.production -o .env
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/deploy/compose/docker-compose.prod.yml

# Configure
nano .env  # Set JWT_SECRET, NIAS_ENCRYPTION_KEY, DEFAULT_ADMIN_PASSWORD

# Start
docker-compose -f docker-compose.prod.yml up -d
```

### Option 2: PostgreSQL (Recommended)

**Best for:**
- Production deployments
- Teams (10+ users)
- High traffic
- Multiple concurrent users

**Pros:**
- ✅ No locking issues
- ✅ Better performance
- ✅ Production-ready
- ✅ Easy backups

**Install:**
```bash
# Download config
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/.env.production -o .env
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/deploy/compose/docker-compose.prod-postgres.yml

# Configure
nano .env  # Set all required passwords

# Start
docker-compose -f docker-compose.prod-postgres.yml up -d
```

### Option 3: AWS RDS (Fully Managed)

**Best for:**
- Enterprise production
- Cloud-native deployments
- Teams requiring high availability
- AWS infrastructure

**Pros:**
- ✅ Fully managed by AWS
- ✅ Automatic backups & updates
- ✅ Multi-AZ high availability
- ✅ SSL/TLS encryption
- ✅ Supports PostgreSQL, MySQL, MariaDB
- ✅ No database maintenance

**Install:**
```bash
# 1. Create RDS instance in AWS Console (PostgreSQL, MySQL, or MariaDB)

# 2. Download config
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/.env.rds-postgres
mv .env.rds-postgres .env

# OR for MySQL/MariaDB:
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/.env.rds-mysql
mv .env.rds-mysql .env

# 3. Download docker-compose
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/deploy/compose/docker-compose.rds-postgres.yml

# OR for MySQL:
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/deploy/compose/docker-compose.rds-mysql.yml

# 4. Configure with your RDS endpoint
nano .env  # Set RDS_DATABASE_URL with your RDS endpoint

# 5. Start
docker-compose -f docker-compose.rds-postgres.yml up -d
```

**📖 Complete RDS guide:** [DEPLOY_RDS.md](./DEPLOY_RDS.md)

---

## 🔐 Security Configuration

### Required Environment Variables

These **must** be set before starting:

```bash
# JWT Secret (32+ characters)
# Generate with: openssl rand -hex 32
JWT_SECRET=your-generated-secret-here

# Encryption Key (exactly 32 characters)  
# Generate with: openssl rand -hex 16
NIAS_ENCRYPTION_KEY=your-generated-key-here

# Default Admin Password
DEFAULT_ADMIN_PASSWORD=YourSecurePassword123!

# PostgreSQL Password (if using PostgreSQL)
POSTGRES_PASSWORD=your-postgres-password-here
```

### Generate Secure Values

```bash
# All at once:
echo "JWT_SECRET=$(openssl rand -hex 32)"
echo "NIAS_ENCRYPTION_KEY=$(openssl rand -hex 16)"
echo "POSTGRES_PASSWORD=$(openssl rand -hex 32)"
```

---

## 📝 Complete .env Example

### For SQLite Deployment

```bash
# Required
JWT_SECRET=a1b2c3d4e5f6...  # 64 characters
NIAS_ENCRYPTION_KEY=a1b2c3d4e5f6...  # 32 characters
DEFAULT_ADMIN_PASSWORD=MySecurePassword123!

# Optional
NIAS_ENV=production
PORT=8080
DEFAULT_ADMIN_USERNAME=admin
CORS_ORIGIN=https://yourdomain.com
BACKUP_ENABLED=true
BACKUP_HOURS=24
RATE_LIMIT_ENABLED=true
LOG_LEVEL=info
```

### For PostgreSQL Deployment

```bash
# Required
JWT_SECRET=a1b2c3d4e5f6...  # 64 characters
NIAS_ENCRYPTION_KEY=a1b2c3d4e5f6...  # 32 characters
DEFAULT_ADMIN_PASSWORD=MySecurePassword123!
POSTGRES_PASSWORD=secure-postgres-password-here

# PostgreSQL
POSTGRES_DB=nias
POSTGRES_USER=nias

# Optional
NIAS_ENV=production
PORT=8080
DEFAULT_ADMIN_USERNAME=admin
CORS_ORIGIN=https://yourdomain.com
RATE_LIMIT_ENABLED=true
LOG_LEVEL=info
```

---

## 🔄 Update to Latest Version

```bash
# Pull latest image
docker pull anveesa/nias:latest

# Restart (SQLite)
docker-compose -f docker-compose.prod.yml down
docker-compose -f docker-compose.prod.yml up -d

# OR restart (PostgreSQL)
docker-compose -f docker-compose.prod-postgres.yml down
docker-compose -f docker-compose.prod-postgres.yml up -d
```

**Migrations run automatically on startup!** No manual steps needed.

---

## 🔍 Verify Installation

```bash
# Check containers are running
docker ps

# Check logs
docker logs anveesa-nias

# Look for:
# ✓ Default admin account created: admin
#   Username: admin
#   Password: YourPassword
#   Please change the password after first login!

# Check health
curl http://localhost:8080/health

# Check version
curl http://localhost:8080/version
```

---

## 🌐 Access Application

1. **Open browser:** `http://localhost:8080` (or your server IP)
2. **Login:**
   - Username: From `DEFAULT_ADMIN_USERNAME` (default: `admin`)
   - Password: From `DEFAULT_ADMIN_PASSWORD` in your `.env`
3. **Change password** immediately after first login
4. **Create connections** to your databases

---

## 📊 Post-Installation

### 1. Change Admin Password

After first login:
1. Click your username (top right)
2. Go to Settings
3. Change password
4. Logout and login with new password

### 2. Set Up Connections

1. Go to "Connections" tab
2. Click "New Connection"
3. Fill in your database details:
   - PostgreSQL, MySQL, SQLite, or SQL Server
   - Host, port, database name
   - Username and password
4. Test connection
5. Save

### 3. Create Additional Users (Optional)

For teams:
1. Go to Administration → Users
2. Click "Create User"
3. Set username, password, and role
4. Users can then login with their credentials

### 4. Configure Backups (SQLite only)

Backups are automatic for SQLite deployments:
- Location: `/app/backups` inside container
- Frequency: Every 24 hours (configurable via `BACKUP_HOURS`)
- Retention: Last 7 backups

**Access backups:**
```bash
# List backups
docker exec anveesa-nias ls -lh /app/backups

# Copy backup to host
docker cp anveesa-nias:/app/backups/nias_backup_20260418.db ./backup.db
```

---

## 🐛 Troubleshooting

### Cannot Start - Missing Environment Variables

**Error:** "JWT_SECRET is required"

**Fix:** Make sure your `.env` file has all required variables set.

### Cannot Login

**Check logs for admin credentials:**
```bash
docker logs anveesa-nias | grep "Default admin"
```

**Reset admin password:**
```bash
# Stop container
docker-compose -f docker-compose.prod.yml down

# Remove data volume (⚠️ destroys all data!)
docker volume rm anveesa-nias_nias-data

# Start again (creates new admin)
docker-compose -f docker-compose.prod.yml up -d
```

### Port 8080 Already in Use

**Change port in `.env`:**
```bash
PORT=8090  # or any available port
```

Then restart:
```bash
docker-compose -f docker-compose.prod.yml up -d
```

### Database Connection Issues (PostgreSQL)

**Check PostgreSQL is healthy:**
```bash
docker ps  # Should show "healthy" status

docker logs anveesa-nias-postgres
```

**Test PostgreSQL connection:**
```bash
docker exec anveesa-nias-postgres psql -U nias -d nias -c "SELECT 1;"
```

---

## 📦 Docker Hub Image

**Image:** `anveesa/nias:latest`

**Tags:**
- `latest` - Latest stable release
- `v1.0.0` - Specific version
- `dev` - Development/testing

**Pull manually:**
```bash
docker pull anveesa/nias:latest
```

---

## 🗑️ Uninstall

### Remove Application (Keep Data)

```bash
docker-compose -f docker-compose.prod.yml down
# OR
docker-compose -f docker-compose.prod-postgres.yml down
```

### Complete Removal (Including Data)

```bash
# ⚠️ WARNING: This deletes all data permanently!

# For SQLite:
docker-compose -f docker-compose.prod.yml down -v

# For PostgreSQL:
docker-compose -f docker-compose.prod-postgres.yml down -v

# Remove image
docker rmi anveesa/nias:latest
```

---

## 🆘 Get Help

- **Documentation:** [README.md](https://github.com/your-org/anveesa-nias)
- **Issues:** [GitHub Issues](https://github.com/your-org/anveesa-nias/issues)
- **Docker Hub:** [anveesa/nias](https://hub.docker.com/r/anveesa/nias)

---

## ✅ Quick Reference

```bash
# Install (SQLite)
curl -O .env && curl -O docker-compose.prod.yml
nano .env  # Set secrets
docker-compose -f docker-compose.prod.yml up -d

# Install (PostgreSQL)  
curl -O .env && curl -O docker-compose.prod-postgres.yml
nano .env  # Set secrets
docker-compose -f docker-compose.prod-postgres.yml up -d

# Update
docker pull anveesa/nias:latest
docker-compose -f docker-compose.prod*.yml restart

# Logs
docker logs -f anveesa-nias

# Backup (SQLite)
docker cp anveesa-nias:/app/data/nias.db ./backup.db

# Backup (PostgreSQL)
docker exec anveesa-nias-postgres pg_dump -U nias nias > backup.sql

# Uninstall
docker-compose -f docker-compose.prod*.yml down -v
```
