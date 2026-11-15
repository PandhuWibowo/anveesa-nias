# Anveesa Nias

**English** · [Bahasa Indonesia](README.id.md) · [简体中文](README.zh-CN.md) · [日本語](README.ja.md)

Anveesa Nias is an open-source internal developer platform. It gives engineering teams one web app to explore and change databases safely, build dashboards, run infrastructure tools, and audit who did what, all under one permission model and one approval flow.

It is a single Go binary (stdlib `net/http`) that serves the API and a Vue 3 + Vite frontend.

📖 **[Wiki](https://github.com/PandhuWibowo/anveesa-nias/wiki)**: architecture, design decisions, known pain points and roadmap, in English, Bahasa Indonesia, 简体中文 and 日本語.

## What's inside

**Databases and data stores**
- PostgreSQL, MySQL/MariaDB, SQL Server and SQLite connections, optionally through an SSH tunnel; credentials are stored AES-encrypted.
- SQL Studio: editor with schema-aware autocomplete, history, snippets, transactions, import, row editing, EXPLAIN, ER diagram, schema diff, row history.
- PostgreSQL/MySQL operations: live sessions, slow/error/audit logs, DB users & grants (plan → preview → apply or request approval), replication, server parameters.
- Redis, Memcache, MongoDB, Cassandra, Kafka, Laravel queues, Elasticsearch/OpenSearch, Alibaba Cloud SLS logs, and object storage buckets.

**Analytics**
- Dashboards with table, KPI, bar, line, area, scatter, pie and donut charts; export to PDF, PNG, Excel, CSV, SQL and JSON; public links and iframe embeds.
- Saved queries with folders, and AI-assisted "Ask your data" (any OpenAI-compatible provider).

**Change management and automation**
- Multi-step approval workflows for write SQL, change sets, data scripts and DB grants.
- Scheduled queries, data pipelines, backups to buckets, and data watchers.

**Infrastructure**
- Docker, Nginx, native crontab editing, and an SFTP file browser over SSH (with sudo elevation for restricted paths).
- Read-only Kubernetes explorer with live logs and an exec terminal.

**Monitoring and governance**
- Health, query performance, log discovery, uptime checks, infra metrics and alerts.
- Users, roles, access groups, per-connection SQL permissions, enforced MFA, and an audit log that covers every tool.
- UI in English and 简体中文.

## Quick start

Prerequisites: Go 1.25+ and Node.js 20+.

```bash
git clone https://github.com/PandhuWibowo/anveesa-nias.git
cd anveesa-nias
cp .env.example .env
make secrets      # prints JWT_SECRET, NIAS_ENCRYPTION_KEY and DEFAULT_ADMIN_PASSWORD; paste them into .env
make install      # npm install + go mod tidy
make dev          # backend on :8080, frontend on :5173
```

Open http://localhost:5173. The default `.env.example` uses SQLite (`DB_DRIVER=sqlite`, `DATABASE_URL=data.db`), so no database server is needed to try it.

### With Docker Compose (PostgreSQL)

```bash
cp .env.example .env   # set JWT_SECRET, NIAS_ENCRYPTION_KEY, DEFAULT_ADMIN_PASSWORD
docker compose -f deploy/compose/docker-compose.yml --env-file .env up -d --build
```

Open http://localhost:8080. Other variants (production, RDS, MySQL, Kafka, Cassandra) live in [`deploy/compose/`](deploy/compose/).

## First login

On first boot, if the `users` table is empty, Anveesa Nias creates one admin account from `.env`:

1. Run `make secrets` and put the printed `DEFAULT_ADMIN_PASSWORD` in `.env` (set `DEFAULT_ADMIN_USERNAME` too if you don't want `admin`).
2. Start the app and log in with those credentials.
3. Change the password right after logging in.

If `DEFAULT_ADMIN_PASSWORD` is unset it falls back to the placeholder `admin123`, which the server refuses to use when `NIAS_ENV=production`. This only applies to an empty database.

## Configuration

Start from `.env.example` and never commit real secrets. The most important variables:

| Variable | Purpose |
| --- | --- |
| `DB_DRIVER` | Internal app database: `sqlite`, `postgres` or `mysql`. |
| `DATABASE_URL` | SQLite file path, or a PostgreSQL/MySQL connection string. |
| `JWT_SECRET` | Signs login tokens. At least 32 characters in production (`openssl rand -hex 32`). |
| `NIAS_ENCRYPTION_KEY` | Exactly 32 characters; encrypts stored credentials (`openssl rand -hex 16`). Changing it makes stored credentials unreadable. |
| `DEFAULT_ADMIN_USERNAME` / `DEFAULT_ADMIN_PASSWORD` | First admin account, created only when no users exist. |
| `NIAS_ENV` | `development` or `production` (stricter validation). |
| `CORS_ORIGIN` | Allowed browser origins. |
| `REDIS_URL` | Optional Redis for cache and rate limiting; falls back to in-process memory. |
| `AI_API_KEY`, `AI_BASE_URL`, `AI_MODEL` | Optional AI provider for "Ask your data". |

## Development

```bash
make dev           # backend + frontend
make dev-server    # backend only (:8080)
make dev-web       # frontend only (:5173)
make test          # go test + vue-tsc type-check
make build         # web/dist + bin/nias
```

Backend tests: `cd server && go test ./...`. Tests ending in `_local_test.go` need a local database.

## Repository layout

```text
.
├── server/          Go backend (main.go, handlers/, middleware/, db/, config/, cache/)
├── web/             Vue 3 frontend (views/, components/, composables/, i18n/)
├── deploy/compose/  Docker Compose variants
├── docs/            User and feature guides
├── DESIGN.md        UI design direction
└── Makefile
```

## Project status

Anveesa Nias is stable since `v1.0.0`. REST API routes, permission constants and the connection/credential storage schema follow [semver](https://semver.org/): incompatible changes bump the major version. See [CHANGELOG.md](CHANGELOG.md) for release history and the [roadmap in the wiki](https://github.com/PandhuWibowo/anveesa-nias/wiki/EN-Roadmap) for what's next.

## Documentation

- [Wiki](https://github.com/PandhuWibowo/anveesa-nias/wiki)
- [Documentation index](docs/README.md)
- [Feature guide](docs/FEATURES.md)
- [Open-source project guide](docs/OPEN_SOURCE.md)

## Contributing

Contributions are welcome. Please read the [contributing guide](CONTRIBUTING.md), the [code of conduct](CODE_OF_CONDUCT.md) and the [contributor guide in the wiki](https://github.com/PandhuWibowo/anveesa-nias/wiki/EN-Contributor-Guide). Issues labelled [`good first issue`](https://github.com/PandhuWibowo/anveesa-nias/labels/good%20first%20issue) are a good place to start.

**Security:** please don't report vulnerabilities in public issues. Follow the [security policy](SECURITY.md).

## Donations

If Anveesa Nias is useful to you and you are in Indonesia, donations can be sent by bank transfer:

| Bank | Account Name | Account Number |
| --- | --- | --- |
| BCA | Pandhu Wibowo | `6043081611` |
| BNI | Pandhu Wibowo | `1487723030` |

PayPal: <https://www.paypal.com/paypalme/pandhuwibowo>

## License

Anveesa Nias is released under the [MIT License](LICENSE).
