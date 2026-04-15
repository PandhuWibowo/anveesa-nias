# Anveesa Nias — Database Studio

A fast, local-first database management studio inspired by CloudBeaver and pgAdmin. Built with Vue 3 + Vite (Bun) frontend and a Go backend.

## Features

- **Multi-database support** — PostgreSQL, MySQL, SQLite, SQL Server
- **SQL Query Editor** — with syntax-aware textarea, Ctrl+Enter to run, query history
- **Schema Browser** — tree view of databases, tables, views and columns
- **Data Browser** — paginated table viewer with sorting and CSV export
- **Connection Manager** — save, test and delete connections
- **Dark / Light theme** — adapts to system preference, persists per-user
- **Authentication** — optional JWT-based login (disabled by default)

## Quick Start

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

| Variable          | Default                          | Description                        |
|-------------------|----------------------------------|------------------------------------|
| `PORT`            | `8080`                           | HTTP port                          |
| `DB_PATH`         | `data.db`                        | Internal SQLite path               |
| `JWT_SECRET`      | (dev default)                    | **Change in production!**          |
| `JWT_EXPIRY_HOURS`| `72`                             | Token TTL                          |
| `AUTH_ENABLED`    | `true`                           | Set `false` to disable login       |
| `CORS_ORIGIN`     | `http://localhost:5173`          | Allowed CORS origin                |

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
