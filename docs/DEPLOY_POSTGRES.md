# PostgreSQL Deployment Guide

This guide covers deploying Anveesa Nias with PostgreSQL for production use.

## 🎯 Why PostgreSQL?

✅ **Better concurrency** - No database locking issues
✅ **Production-ready** - Battle-tested for high-traffic applications  
✅ **Easy backups** - Built-in backup tools (pg_dump, pg_restore)
✅ **Scalability** - Can handle millions of rows easily
✅ **ACID compliance** - Full transaction support

## 🚀 Quick Start

### Option 1: Docker Compose (Recommended)

```bash
# 1. Clone the repository
git clone <repository-url>
cd anveesa-nias

# 2. Create environment file
cp .env.postgres.example .env

# 3. Edit .env and set secure values:
nano .env

# Required changes:
# - POSTGRES_PASSWORD=<strong-password>
# - JWT_SECRET=<32+ characters>
# - NIAS_ENCRYPTION_KEY=<32 characters>
# - DEFAULT_ADMIN_PASSWORD=<strong-password>

# 4. Start services
docker-compose -f docker-compose.postgres.yml up -d

# 5. Check logs
docker-compose -f docker-compose.postgres.yml logs -f nias

# 6. Access application
# Open http://localhost:8080
# Login with DEFAULT_ADMIN_USERNAME and DEFAULT_ADMIN_PASSWORD
```

### Option 2: External PostgreSQL

If you have an existing PostgreSQL database:

```bash
# 1. Set environment variables
export DB_DRIVER=postgres
export DATABASE_URL="postgres://<db-user>:<db-password>@localhost:5432/nias?sslmode=disable"
export JWT_SECRET="<CHANGE_ME_JWT_SECRET_MIN_32_CHARS>"
export NIAS_ENCRYPTION_KEY="your-32-byte-key"
export DEFAULT_ADMIN_PASSWORD="<YOUR_ADMIN_PASSWORD>"

# 2. Run the application
./nias-server
```

## 📋 Environment Variables

### Required for PostgreSQL

| Variable | Description | Example |
|----------|-------------|---------|
| `DB_DRIVER` | Database type | `postgres` |
| `DATABASE_URL` | PostgreSQL connection string | `postgres://<db-user>:<db-password>@<db-host>:5432/<db-name>` |
| `JWT_SECRET` | JWT signing key (32+ chars) | Generated with `openssl rand -hex 32` |
| `NIAS_ENCRYPTION_KEY` | Credential encryption key (32 chars) | Generated with `openssl rand -hex 16` |
| `DEFAULT_ADMIN_PASSWORD` | Initial admin password | `<YOUR_ADMIN_PASSWORD>` |

### PostgreSQL Connection URL Format

```
postgres://<db-user>:<db-password>@<db-host>:<db-port>/<db-name>?sslmode=disable
```

Examples:
```bash
# Local PostgreSQL
DATABASE_URL=postgres://nias:<db-password>@localhost:5432/nias?sslmode=disable

# Docker Compose (container name as hostname)
DATABASE_URL=postgres://nias:<db-password>@postgres:5432/nias?sslmode=disable

# Cloud PostgreSQL (AWS RDS, DigitalOcean, etc.)
DATABASE_URL=postgres://<db-user>:<db-password>@db.example.com:5432/nias?sslmode=require

# With SSL
DATABASE_URL=postgres://<db-user>:<db-password>@<db-host>:5432/nias?sslmode=require
```

## 🔄 Migration from SQLite

### Step 1: Export Data from SQLite

```bash
# Export connections, users, etc.
sqlite3 data.db <<EOF
.mode insert connections
.output connections.sql
SELECT * FROM connections;
.output stdout
EOF

# Repeat for other tables: users, query_history, audit_log, etc.
```

### Step 2: Import to PostgreSQL

```bash
# Method 1: Using the application (recommended)
# 1. Start with PostgreSQL
# 2. Default admin account will be created
# 3. Manually recreate connections via UI

# Method 2: Direct SQL import (advanced)
# Convert SQLite SQL to PostgreSQL format and import
psql -U nias -d nias -f connections.sql
```

### Step 3: Verify

```bash
# Check if data migrated successfully
psql -U nias -d nias -c "SELECT COUNT(*) FROM connections;"
psql -U nias -d nias -c "SELECT COUNT(*) FROM users;"
```

## 🗄️ Database Management

### Backup

```bash
# Full database backup
pg_dump -U nias -h localhost -d nias -F c -f nias_backup_$(date +%Y%m%d).dump

# Schema only
pg_dump -U nias -h localhost -d nias -s -f nias_schema.sql

# Using Docker
docker exec anveesa-nias-postgres pg_dump -U nias nias > backup.sql
```

### Restore

```bash
# Restore from custom format
pg_restore -U nias -h localhost -d nias nias_backup.dump

# Restore from SQL file
psql -U nias -h localhost -d nias < backup.sql

# Using Docker
docker exec -i anveesa-nias-postgres psql -U nias nias < backup.sql
```

### Connect to Database

```bash
# Using psql
psql -U nias -h localhost -d nias

# Using Docker
docker exec -it anveesa-nias-postgres psql -U nias nias

# Useful commands inside psql:
\dt              # List tables
\d users         # Describe users table
\l               # List databases
\du              # List users
\q               # Quit
```

## 🔧 Maintenance

### Check Connection Status

```bash
# Using psql
psql -U nias -h localhost -d nias -c "SELECT version();"

# Check active connections
psql -U nias -h localhost -d nias -c "SELECT COUNT(*) FROM pg_stat_activity WHERE datname='nias';"
```

### Vacuum (Clean up)

```bash
# Analyze and optimize
psql -U nias -d nias -c "VACUUM ANALYZE;"

# Full vacuum (requires exclusive lock)
psql -U nias -d nias -c "VACUUM FULL;"
```

### Monitor Performance

```bash
# Check slow queries
psql -U nias -d nias -c "
SELECT query, calls, total_time, mean_time 
FROM pg_stat_statements 
ORDER BY mean_time DESC 
LIMIT 10;"

# Check table sizes
psql -U nias -d nias -c "
SELECT tablename, pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) 
FROM pg_tables 
WHERE schemaname='public' 
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;"
```

## 🚀 Production Deployment

### 1. Use Managed PostgreSQL

**Recommended providers:**
- AWS RDS for PostgreSQL
- DigitalOcean Managed Databases
- Google Cloud SQL
- Azure Database for PostgreSQL
- Heroku Postgres
- Supabase

**Benefits:**
- Automatic backups
- High availability
- Monitoring included
- Automatic updates
- SSL/TLS encryption

### 2. Security Best Practices

```bash
# Use strong passwords
POSTGRES_PASSWORD=$(openssl rand -hex 32)

# Enable SSL
DATABASE_URL=postgres://<db-user>:<db-password>@<db-host>:5432/nias?sslmode=require

# Use environment variables (never commit credentials)
# Use secrets management (AWS Secrets Manager, Vault, etc.)

# Restrict network access
# - Use firewalls
# - Use VPC/private networks
# - Use connection pooling (PgBouncer)
```

### 3. Scaling

```bash
# Increase connection pool
# In config or environment:
DB_MAX_CONNECTIONS=50
DB_MAX_IDLE=10

# Use read replicas for reporting
# - Primary for writes
# - Replicas for read-only queries

# Use connection pooling
# - PgBouncer
# - pgpool-II
```

## 🐛 Troubleshooting

### Cannot Connect

```bash
# Check if PostgreSQL is running
docker ps | grep postgres

# Check PostgreSQL logs
docker logs anveesa-nias-postgres

# Test connection manually
psql -U nias -h localhost -d nias -c "SELECT 1;"

# Check connection from app container
docker exec anveesa-nias ping postgres
```

### Too Many Connections

```bash
# Check current connections
psql -U nias -d nias -c "SELECT COUNT(*) FROM pg_stat_activity;"

# Kill idle connections
psql -U nias -d nias -c "
SELECT pg_terminate_backend(pid) 
FROM pg_stat_activity 
WHERE datname='nias' 
AND state='idle' 
AND state_change < NOW() - INTERVAL '1 hour';"

# Increase max_connections in postgresql.conf
# max_connections = 200
```

### Slow Queries

```bash
# Enable slow query log (postgresql.conf)
log_min_duration_statement = 1000  # Log queries > 1s

# Check for missing indexes
psql -U nias -d nias -c "
SELECT schemaname, tablename, indexname 
FROM pg_indexes 
WHERE schemaname='public';"

# Analyze query plan
psql -U nias -d nias
EXPLAIN ANALYZE SELECT * FROM connections LIMIT 10;
```

### Disk Space Issues

```bash
# Check database size
psql -U nias -d nias -c "SELECT pg_size_pretty(pg_database_size('nias'));"

# Cleanup old data
psql -U nias -d nias -c "DELETE FROM audit_log WHERE executed_at < NOW() - INTERVAL '90 days';"

# Vacuum to reclaim space
psql -U nias -d nias -c "VACUUM FULL;"
```

## 📦 Update Procedure

### For Docker Deployments

```bash
# 1. Backup database
docker exec anveesa-nias-postgres pg_dump -U nias nias > backup_before_update.sql

# 2. Pull latest image
docker-compose -f docker-compose.postgres.yml pull

# 3. Restart services (migrations run automatically)
docker-compose -f docker-compose.postgres.yml up -d

# 4. Check logs for migration success
docker-compose -f docker-compose.postgres.yml logs nias | grep -i migration

# 5. Verify application is running
curl http://localhost:8080/health
```

### Migrations are Automatic

- ✅ Migrations run automatically on startup
- ✅ Idempotent - safe to run multiple times
- ✅ No manual SQL needed
- ✅ Supports both SQLite → PostgreSQL and PostgreSQL → PostgreSQL upgrades

## 📚 Additional Resources

- [PostgreSQL Documentation](https://www.postgresql.org/docs/)
- [Docker PostgreSQL Guide](https://hub.docker.com/_/postgres)
- [PostgreSQL Performance Tuning](https://wiki.postgresql.org/wiki/Performance_Optimization)
- [Backup Best Practices](https://www.postgresql.org/docs/current/backup.html)

## 🆘 Support

If you encounter issues:

1. Check logs: `docker-compose -f docker-compose.postgres.yml logs`
2. Verify environment variables: `docker-compose -f docker-compose.postgres.yml config`
3. Test database connection: `docker exec anveesa-nias-postgres psql -U nias -d nias -c "SELECT 1;"`
4. Check application health: `curl http://localhost:8080/health`
5. Open GitHub issue with logs and configuration

## ✅ Quick Checklist

Before deploying to production:

- [ ] Strong PostgreSQL password set
- [ ] JWT_SECRET is 32+ characters
- [ ] NIAS_ENCRYPTION_KEY is exactly 32 characters
- [ ] DEFAULT_ADMIN_PASSWORD changed from default
- [ ] CORS_ORIGIN set to your actual domain
- [ ] SSL/TLS enabled for database connection
- [ ] Automatic backups configured
- [ ] Monitoring set up
- [ ] Tested full backup and restore procedure
- [ ] Firewall rules configured
- [ ] Rate limiting enabled
