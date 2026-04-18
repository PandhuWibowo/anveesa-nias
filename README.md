# Anveesa Nias — Database Studio

A fast, local-first database management studio inspired by CloudBeaver and pgAdmin. Built with Vue 3 + Vite (Bun) frontend and a Go backend.

**Docker Hub:** [`anveesa/nias`](https://hub.docker.com/r/anveesa/nias)

## ⚡ Quick Install

```bash
# 1. Download config
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/.env.production -o .env

# 2. Set secrets (required!)
nano .env  # Set JWT_SECRET, NIAS_ENCRYPTION_KEY, DEFAULT_ADMIN_PASSWORD

# 3. Start
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/docker-compose.prod.yml
docker-compose -f docker-compose.prod.yml up -d
```

**Access:** http://localhost:8080

📖 **Full installation guide:** [INSTALL.md](./INSTALL.md)

## Features

- **Multi-database support** — PostgreSQL, MySQL, SQLite, SQL Server
- **SQL Query Editor** — with syntax-aware textarea, Ctrl+Enter to run, query history
- **Schema Browser** — tree view of databases, tables, views and columns
- **Data Browser** — paginated table viewer with sorting and CSV export
- **Connection Manager** — save, test and delete connections
- **Dark / Light theme** — adapts to system preference, persists per-user
- **Authentication** — optional JWT-based login (disabled by default)
- **PostgreSQL or SQLite** — Choose your internal database (SQLite for dev, PostgreSQL for production)
- **Automatic Migrations** — Database schema updates automatically on startup

## 🚀 Quick Start with Docker (Recommended)

**Pull and run from Docker Hub** - no build required:

```bash
# Create directory
mkdir anveesa-nias && cd anveesa-nias

# Download config
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/.env.production -o .env

# Edit .env - set JWT_SECRET, NIAS_ENCRYPTION_KEY, DEFAULT_ADMIN_PASSWORD
nano .env

# Download and start
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/docker-compose.prod.yml
docker-compose -f docker-compose.prod.yml up -d
```

**Access:** http://localhost:8080

See [INSTALL.md](./INSTALL.md) for complete installation guide.

---

## 🛠️ Development Setup

For developers who want to modify the code:

### Prerequisites
- [Bun](https://bun.sh) ≥ 1.0
- [Go](https://go.dev) ≥ 1.22

### Install dependencies

```bash
make install
```

### Start development servers

```bash
make dev
```

- Frontend: http://localhost:5173
- Backend API: http://localhost:8080

## Project Structure

```
anveesa-nias/
├── Makefile
├── web/                    # Vue 3 + Vite + TypeScript frontend
│   ├── src/
│   │   ├── components/
│   │   │   ├── database/   # QueryEditor, DataTable, SchemaTree
│   │   │   ├── layout/     # AppSidebar, StatusBar
│   │   │   └── ui/         # Toast, ConfirmModal
│   │   ├── composables/    # useAuth, useTheme, useConnections, useQuery, useSchema
│   │   ├── layouts/        # AppLayout
│   │   ├── router/
│   │   ├── styles/         # main.css (design tokens, components)
│   │   └── views/          # Welcome, Query, Schema, Data, Connections, Login
└── server/                 # Go HTTP API
    ├── config/             # Environment config
    ├── db/                 # SQLite internal store (connections, users)
    ├── handlers/           # auth, connections, query, schema
    ├── middleware/          # CORS, JWT auth
    └── main.go
```

## Environment Variables (server)

| Variable                  | Default                          | Description                        |
|---------------------------|----------------------------------|------------------------------------|
| `PORT`                    | `8080`                           | HTTP port                          |
| `DB_PATH`                 | `data.db`                        | Internal SQLite path               |
| `JWT_SECRET`              | (dev default)                    | **Change in production!**          |
| `JWT_EXPIRY_HOURS`        | `72`                             | Token TTL                          |
| `AUTH_ENABLED`            | `true`                           | Set `false` to disable login       |
| `DEFAULT_ADMIN_USERNAME`  | `admin`                          | Default admin username on first install |
| `DEFAULT_ADMIN_PASSWORD`  | `Admin123!`                      | Default admin password on first install |
| `CORS_ORIGIN`             | `http://localhost:5173`          | Allowed CORS origin                |

## 📦 Installation Methods

### Method 1: Docker Hub (Recommended)

**For end users** - Pull pre-built image:

```bash
# Download config files
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/.env.production -o .env
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/docker-compose.prod.yml

# Configure
nano .env  # Set JWT_SECRET, NIAS_ENCRYPTION_KEY, DEFAULT_ADMIN_PASSWORD

# Start
docker-compose -f docker-compose.prod.yml up -d
```

**📖 Full guide:** [INSTALL.md](./INSTALL.md)

### Method 2: Build from Source

**For developers** - Build locally:

```bash
git clone <repository-url>
cd anveesa-nias

# Build and run
docker-compose up -d
```

---

## Database Options

Anveesa Nias supports **multiple database backends**:

### SQLite (Default - Simple)
- ✅ Zero configuration
- ✅ Single file database  
- ✅ Perfect for small teams
- ⚠️ Limited concurrency

**Use:** `docker-compose.prod.yml`

### PostgreSQL (Recommended - Production)
- ✅ Better concurrency
- ✅ Production-ready
- ✅ Easy backups
- ✅ No locking issues

**Use:** `docker-compose.prod-postgres.yml`

**📖 Full guide:** [DEPLOY_POSTGRES.md](./DEPLOY_POSTGRES.md)

### AWS RDS (Cloud - Fully Managed)
- ✅ Fully managed by AWS
- ✅ Automatic backups and updates
- ✅ Multi-AZ high availability
- ✅ Supports PostgreSQL, MySQL, MariaDB
- ✅ SSL/TLS encryption
- ✅ Perfect for production workloads

**Use:** `docker-compose.rds-postgres.yml` or `docker-compose.rds-mysql.yml`

**📖 Full guide:** [DEPLOY_RDS.md](./DEPLOY_RDS.md)

### Default Admin Account

On first installation, if no users exist in the database, a default admin account is automatically created:

- **Username**: `admin` (or set via `DEFAULT_ADMIN_USERNAME`)
- **Password**: `Admin123!` (or set via `DEFAULT_ADMIN_PASSWORD`)

**⚠️ IMPORTANT**: Change the default password immediately after first login, especially in production!

To set custom credentials before first run:

```bash
# In .env file or docker-compose.yml
DEFAULT_ADMIN_USERNAME=youradmin
DEFAULT_ADMIN_PASSWORD=YourSecurePassword123!
```

### Docker Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `JWT_SECRET` | **Yes (production)** | - | JWT signing key (min 32 chars) |
| `NIAS_ENCRYPTION_KEY` | **Yes (production)** | - | Encryption key for credentials (32 chars) |
| `DEFAULT_ADMIN_USERNAME` | No | `admin` | Initial admin username |
| `DEFAULT_ADMIN_PASSWORD` | Recommended | `Admin123!` | Initial admin password |
| `CORS_ORIGIN` | No | `http://localhost:8080` | Allowed CORS origins |
| `BACKUP_ENABLED` | No | `true` | Enable automatic backups |
| `BACKUP_HOURS` | No | `24` | Backup interval in hours |

### Persistent Data

The Docker setup uses named volumes for data persistence:
- `nias-data`: Database and application data
- `nias-backups`: Automatic database backups

## Build for Production

```bash
make build
./bin/nias
```

## API Reference

| Method | Path | Description |
|--------|------|-------------|
| GET    | `/api/auth/setup` | Check if auth is enabled |
| POST   | `/api/auth/login` | Login → JWT token |
| POST   | `/api/auth/register` | Register user |
| GET    | `/api/connections` | List saved connections |
| POST   | `/api/connections` | Create connection |
| DELETE | `/api/connections/:id` | Delete connection |
| POST   | `/api/connections/test` | Test DSN without saving |
| POST   | `/api/connections/:id/query` | Execute SQL |
| GET    | `/api/connections/:id/schema` | List databases & tables |
| GET    | `/api/connections/:id/schema/:db/tables/:table/columns` | Table columns |
| GET    | `/api/connections/:id/schema/:db/tables/:table/data` | Paginated table data |
