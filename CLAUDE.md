# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

Anveesa Nias is an open-source database studio. A Go HTTP API (`server/`) serves a Vue 3 + Vite frontend (`web/`). The app connects to user databases (PostgreSQL, MySQL, SQLite, SQL Server) and also uses its own internal database (postgres or mysql only — sqlite mentioned in docs is not supported by the current config validator) to store users, connections, audit logs, etc.

## Commands

### Development

```bash
make dev          # Start both backend (port 8080) and frontend (port 5173) concurrently
make dev-server   # Backend only
make dev-web      # Frontend only
make install      # Install all dependencies (npm + go mod tidy)
```

### Build

```bash
make build        # Build frontend (web/dist/) and backend binary (bin/nias)
make build-prod   # Same but with version/buildTime linker flags
```

### Tests & Linting

```bash
make test                                    # Backend go test + frontend type-check
make lint                                    # go vet + eslint

# Backend only
cd server && go test ./...
cd server && go test -run TestFunctionName ./path/to/package

# Frontend only
cd web && npm run type-check                 # vue-tsc
cd web && npm run lint                       # eslint
cd web && npm run format                     # prettier
```

### Environment Setup

Copy `.env.example` to `.env` and set at minimum:
- `DATABASE_URL` — internal app DB connection string
- `DB_DRIVER` — `postgres` or `mysql`
- `JWT_SECRET` — at least 32 chars in production
- `NIAS_ENCRYPTION_KEY` — exactly 32 chars (encrypts stored DB credentials)

Generate secrets: `make secrets`

## Architecture

### Backend (`server/`)

- **`main.go`** — single 1130-line file containing `main()`, `registerRoutes()`, and local helpers. All route registration lives here. Uses stdlib `net/http` with `http.ServeMux`; no external router framework.
- **`config/config.go`** — loads all config from env vars; validates required fields; rejects `DB_DRIVER` values other than `postgres`/`mysql`.
- **`db/db.go`** — initializes the internal app DB, runs inline migrations (no migration tool), seeds the default admin user. The global `db.DB *sql.DB` is used directly throughout.
- **`handlers/`** — one file per feature area (e.g. `connections.go`, `auth.go`, `analytics_dashboards.go`). Each handler is a function returning `http.HandlerFunc`. `models.go` defines shared types, all permission constants, and `DbPerm`/`AppPerm` helpers.
- **`middleware/`** — `InjectUserContext` extracts JWT claims and injects them into `context.Context`; `RequireAppPermission` / `RequireAnyAppPermission` gate handlers by app-level permission strings; `RequireDbPermission` gates by per-connection DB operation permissions; `CORS`, `SecurityHeaders`, `Recovery`, and rate limiters wrap the global handler.
- **`cache/`** — `Store` interface with two backends: `MemoryStore` (default) and `RedisStore`. Initialized once at startup; falls back to memory if Redis is unreachable.

### Permission Model

Two independent permission layers:

1. **App permissions** — string constants defined in `handlers/models.go` (e.g. `"connections.view"`, `"query.execute"`, `"dashboards.manage"`). Stored per-user/role in the internal DB. Enforced by `mw.RequireAnyAppPermission` middleware.
2. **DB permissions** — `DbPerm` type (`select`, `insert`, `update`, `delete`, `create`, `alter`, `drop`). Per-connection, per-user grants. Enforced by `mw.RequireDbPermission` / `mw.RequireDbPermissionForSQL` which parses the SQL statement to detect required permission.

Connection credentials are AES-encrypted at rest using `NIAS_ENCRYPTION_KEY`.

### Frontend (`web/src/`)

- **`router/index.ts`** — Vue Router with `requiresAuth` and `requiredPermissionsAny` meta. Route guard redirects unauthenticated users to `/login` and unauthorized users to `/welcome`. Navigation events are sent to `/api/audit/access` via `router.afterEach`.
- **`layouts/AppLayout.vue`** — wraps all authenticated views; provides the nav sidebar.
- **`views/`** — one large `.vue` file per page/feature. Many are 30–100 KB; they contain all component logic, template, and styles for that feature.
- **`composables/`** — shared reactive state and API calls (`useAuth`, `useConnections`, `usePermissions`, `useSchema`, etc.). Components call these rather than making axios calls directly.

### Connection Pool (`handlers/pool.go`)

Maintains a pool of live `*sql.DB` connections to user databases. Connections are opened on first use and reused across requests. SSH tunnel support is included for tunneled connections.

### Key Integrations

- **Kafka** — `handlers/kafka.go` for topic browsing, message production/consumption.
- **Redis** — `handlers/redis.go` for key browsing and ops (separate from the app's own cache).
- **Laravel Queue** — `handlers/laravel_queue.go` for queue monitoring and job management.
- **AI** — `handlers/ai.go` for AI-assisted SQL/analytics; provider configured via `AI_API_KEY`, `AI_BASE_URL`, `AI_MODEL` env vars.
- **Scheduler** — `handlers/scheduler.go` for cron-style scheduled queries.
- **Approval Workflows** — `handlers/workflow_approval.go` for multi-step query approval flows.
