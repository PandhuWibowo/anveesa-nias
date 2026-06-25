# AGENT.md — Anveesa Nias

Codebase context for AI agents and automated tooling.

## What This Is

**Anveesa Nias** is an open-source internal developer platform (IDP).

- **Backend**: Go HTTP API in `server/` — stdlib `net/http`, no router framework, single `main.go` for all route registration.
- **Frontend**: Vue 3 + Vite SPA in `web/` — one large `.vue` file per feature page, composables for shared state/API calls.
- **Internal DB**: PostgreSQL or MySQL only (no SQLite for the app's own storage). Inline migrations in `server/db/db.go` — no migration tool.
- **User DBs**: connects to PostgreSQL, MySQL, MariaDB, SQLite, SQL Server, Redis, Memcache, Kafka, MongoDB, Cassandra, Elasticsearch, OpenSearch, S3-compatible stores.

## Key Invariants

- All route registration is in `server/main.go → registerRoutes()`. Adding a new API endpoint always means a change here.
- New DB tables go as raw SQL strings at the end of the `stmts` slice in `server/db/db.go`. `ALTER TABLE ADD COLUMN` errors (duplicate column) are silently ignored — safe to add idempotently.
- Handler files live in `server/handlers/`. Each handler returns `http.HandlerFunc`. Permission checks use `mw.RequireAnyAppPermission(handlers.PermXxx)` wrappers applied in `registerRoutes`.
- Permission constants are in `server/handlers/models.go`. Reuse existing ones; only add new ones when a genuinely new access domain is introduced.
- Credentials (DB passwords, SSH keys) are AES-256-GCM encrypted before storage using `encryptCredential()` / `decryptCredential()` in `handlers/connections.go`. Never store plaintext secrets in the DB.
- Frontend composables in `web/src/composables/` own all API calls. Vue views import composables, not axios directly (there are some legacy exceptions in large view files).
- `appdb.ConvertQuery()` translates `?` placeholders for the active driver (PostgreSQL uses `$1,$2,...`; MySQL/SQLite use `?`). Always wrap raw SQL through this when the query contains parameters.

## Project Structure

```
server/
  main.go                      # Entry point + all route registration
  config/config.go             # Env-var config, validation
  db/db.go                     # Internal DB init, inline migrations, seed
  handlers/
    models.go                  # Shared structs, permission constants
    connections.go             # Connection CRUD + pool management
    connection_templates.go    # Connection template presets (host/port/db, no creds)
    auth.go                    # JWT login/register/2FA/sessions
    query.go                   # SQL execution, explain, history
    pool.go                    # Live *sql.DB connection pool
    nginx.go                   # SSH-based nginx management
    docker.go                  # SSH-tunneled Docker daemon management
    ai.go                      # AI-assisted SQL/analytics
    kafka.go                   # Kafka topic browse/produce/consume
    redis.go                   # Redis key browser
    scheduler.go               # Cron-style scheduled queries
    workflow_approval.go       # Multi-step query approval flows
    # ... one file per feature area
  middleware/
    auth.go                    # InjectUserContext (JWT → headers)
    permissions.go             # RequireAnyAppPermission, RequireDbPermission
    cors.go / security.go / recovery.go / rate_limit.go
  cache/
    store.go                   # Store interface
    memory.go / redis.go       # MemoryStore (default) + RedisStore

web/src/
  router/index.ts              # Vue Router, requiresAuth + requiredPermissionsAny meta
  layouts/AppLayout.vue        # Authenticated shell, nav sidebar
  views/                       # One .vue per page (can be 30–150 KB)
    ConnectionsView.vue        # Connection list + new/edit form + template picker
    NginxView.vue              # SSH-based nginx management + host connect/disconnect
    DockerView.vue             # Docker host management
    # ...
  composables/
    useConnections.ts          # Connection CRUD state
    useConnectionTemplates.ts  # Connection template CRUD state
    useAuth.ts                 # Auth state, permissions
    # ...
  components/
    ui/SearchSelect.vue        # Searchable dropdown (search input + keyboard nav)
    nginx/NginxEditor.vue      # CodeMirror nginx config editor
```

## API Conventions

- JSON in, JSON out. `Content-Type: application/json` always set in handlers.
- Auth via `Authorization: Bearer <jwt>` header. Middleware extracts claims into `X-User-ID`, `X-User-Role`, `X-User-Permissions` headers for downstream handlers.
- Error shape: `{"error": "message"}` with appropriate HTTP status.
- Created resources return `201 Created` with the new resource body.
- Delete returns `204 No Content`.
- Pagination is query-param based (`?page=`, `?limit=`) where applicable.

## Notable Features (recent additions)

### Connection Templates (`/api/connection-templates`)
Reusable host+port+database presets — no credentials stored. Users pick a template when creating a connection; the form fills host/port/database/driver/ssl/tags, leaving username/password blank. Managed via `handlers/connection_templates.go` and `composables/useConnectionTemplates.ts`. Non-admins can only edit/delete templates they own; shared templates are visible to all.

### Nginx SSH Host Connect/Disconnect (`NginxView.vue`)
`hostStatus` ref (`unknown | connecting | connected | error`) tracks SSH reachability of the selected host. Selecting a host auto-pings via `GET /api/docker/hosts/{id}/ping`. Connect button re-pings; Disconnect stops log following/polling and resets to the no-host state.

## Running Locally

```bash
cp .env.example .env          # fill DATABASE_URL, DB_DRIVER, JWT_SECRET, NIAS_ENCRYPTION_KEY
make install                  # npm install + go mod tidy
make dev                      # backend :8080 + frontend :5173
```

Type-check and lint before committing:

```bash
cd web && npm run type-check
cd server && go build ./...
```
