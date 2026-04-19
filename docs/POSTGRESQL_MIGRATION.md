# PostgreSQL Migration Complete ✅

## What Changed

**SQLite has been completely removed** from Anveesa Nias. The application now **requires PostgreSQL or MySQL**.

### Changes Made

1. **Removed SQLite Support**
   - SQLite driver still in go.mod (for backward compatibility)
   - Application will refuse to start if `DB_DRIVER=sqlite`
   - All `*.db` files deleted
   - Default database driver changed from `sqlite` to `postgres`

2. **Added Automatic .env Loading**
   - Server now automatically loads `.env` file from project root
   - No need to export environment variables manually
   - Uses `github.com/joho/godotenv`

3. **Database Performance Improvements**
   - Increased connection pool limits for concurrent queries
   - Fixed N+1 query problem in workflow approval endpoints
   - Added request timeouts to prevent hanging queries
   - Increased graceful shutdown timeout from 30s to 60s

## How to Use

### Development Setup

1. **Ensure PostgreSQL is running:**
   ```bash
   psql postgres -c "SELECT 1;"
   ```

2. **Database is already created:**
   - User: `nias_dev`
   - Password: `dev_password`
   - Database: `nias_dev`

3. **Configuration is ready:**
   - `.env` file already created in project root
   - Points to PostgreSQL on localhost:5432

4. **Start the server:**
   ```bash
   cd /Users/pandhuwibowo/Portfolio/anveesa/anveesa-nias
   bun run dev
   ```

   The server will automatically:
   - Load `.env` from project root
   - Connect to PostgreSQL
   - Run migrations
   - Create admin account if needed

### Configuration File

Your `.env` file (already created):

```env
# Database - PostgreSQL (NO MORE SQLITE!)
DB_DRIVER=postgres
DATABASE_URL=postgres://nias_dev:dev_password@localhost:5432/nias_dev?sslmode=disable

# Server
PORT=8080
HOST=0.0.0.0

# Development
NIAS_ENV=development
AUTH_ENABLED=true
CORS_ORIGIN=http://localhost:5173
RATE_LIMIT_ENABLED=false
LOG_LEVEL=info
```

### Production Deployment

For production, use one of these templates:
- `.env.rds-postgres` - AWS RDS PostgreSQL
- `.env.rds-mysql` - AWS RDS MySQL

Copy the template to `.env` and fill in your credentials:

```bash
cp .env.rds-postgres .env
# Edit .env with your production values
```

## What Was Fixed

The original issue ("context deadline exceeded" errors) was caused by:

1. **SQLite with 1 connection limit** - blocking concurrent queries
2. **N+1 query problem** - making 31+ queries for 10 workflows
3. **Short shutdown timeout** - 30 seconds wasn't enough

Now with PostgreSQL:
- ✅ Up to 25 concurrent connections
- ✅ Batch queries (4 queries regardless of workflow count)
- ✅ 60-second graceful shutdown
- ✅ Request timeouts prevent hanging
- ✅ Much faster performance

## Troubleshooting

### "Database connection failed"

Check PostgreSQL is running:
```bash
psql postgres -c "SELECT 1;"
```

### "No .env file found"

The warning is normal - it just means environment variables are being used directly. If you have a `.env` file, make sure it's in the project root (not in `server/`).

### "role nias_dev does not exist"

Recreate the database:
```bash
psql postgres -c "DROP DATABASE IF EXISTS nias_dev; DROP USER IF EXISTS nias_dev;"
psql postgres -c "CREATE USER nias_dev WITH PASSWORD 'dev_password';"
psql postgres -c "CREATE DATABASE nias_dev OWNER nias_dev;"
```

## Files Changed

- `server/main.go` - Added .env loading
- `server/config/config.go` - Removed SQLite support, made PostgreSQL default
- `server/db/db.go` - Increased connection pool, removed SQLite restrictions
- `server/handlers/workflow_approval.go` - Optimized queries, added timeouts
- `.env` - Created with PostgreSQL configuration

## Reverting to SQLite (Not Recommended)

If you **really** need SQLite:

1. Revert the config changes in `server/config/config.go`
2. Set `DB_DRIVER=sqlite` and `DB_PATH=data.db` in `.env`
3. Restart the server

But seriously, use PostgreSQL for production!
