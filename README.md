# Anveesa Nias

Anveesa Nias is an open-source database studio for teams that need to explore data, run SQL, manage saved queries, build analytics dashboards, and audit database activity from a web UI.

It is built with a Go HTTP API and a Vue 3 + Vite frontend.

## Features

- Multi-database connections for PostgreSQL, MySQL, SQLite, and SQL Server-oriented workflows.
- SQL editor, saved queries, query history, and schema browsing.
- Analytics dashboards with tables, KPIs, bar, horizontal bar, line, area, scatter, pie, and donut charts.
- Dashboard export to PDF, PNG, Excel, CSV, SQL, and JSON.
- Public dashboard sharing and iframe embed support per dashboard or per chart.
- User management, roles, permissions, 2FA, notifications, and approval workflows.
- Audit, monitoring, backups, scheduler, and operational views.
- SQLite or PostgreSQL internal storage, with optional Redis-backed cache and rate limiting.

## Project Status

This repository is being prepared for public open-source collaboration. APIs may still change before a stable `v1.0.0` release.

## Demo

A public demo is available at [nias.anveesa.com](https://nias.anveesa.com).

| Field | Value |
| --- | --- |
| URL | `https://nias.anveesa.com` |
| Username | `admin` |
| Password | `Admin123!` |

The demo is for evaluation only. Do not enter private credentials or sensitive data.

## Local Development

Prerequisites:

- Go 1.22+
- Node.js 20+

Install and run:

```bash
cd server
go mod download

cd ../web
npm install

cd ..
make dev-server
# In another shell:
cd web && npm run dev
```

Development URLs:

- Frontend: http://localhost:5173
- Backend: http://localhost:8080

## Verification

```bash
cd server && go test ./...
cd ../web && npm run build
```

## Configuration

Start from `.env.example` and never commit real secrets. Important variables:

| Variable | Purpose |
| --- | --- |
| `JWT_SECRET` | Secret used to sign authentication tokens. Use `openssl rand -hex 32`. |
| `NIAS_ENCRYPTION_KEY` | 32-character key used to encrypt stored database credentials. Use `openssl rand -hex 16`. |
| `DEFAULT_ADMIN_USERNAME` | First admin username when no users exist. |
| `DEFAULT_ADMIN_PASSWORD` | First admin password. Change it immediately after first login. |
| `DB_DRIVER` | Internal app database driver, usually `sqlite` or `postgres`. |
| `DATABASE_URL` | PostgreSQL connection URL when `DB_DRIVER=postgres`. |
| `CORS_ORIGIN` | Allowed browser origins. |
| `REDIS_URL` | Optional Redis cache/rate-limit store. Falls back to in-process memory when unset. |

## Repository Layout

```text
.
├── docs/                 User, product, and implementation docs
├── server/               Go backend
├── web/                  Vue frontend
├── Makefile
└── README.md
```

## Documentation

- [Documentation index](docs/README.md)
- [Feature guide](docs/FEATURES.md)
- [Open-source project guide](docs/OPEN_SOURCE.md)
- [Demo guide](docs/DEMO.md)
- [Donation guide](docs/DONATION.md)

## Donations

If Anveesa Nias is useful for you and you are in Indonesia, donations can be sent by bank transfer:

| Bank | Account Name | Account Number |
| --- | --- | --- |
| BCA | Pandhu Wibowo | `6043081611` |
| BNI | Pandhu Wibowo | `1487723030` |

## Contributing

Contributions are welcome. Please read:

- [Contributing guide](CONTRIBUTING.md)
- [Code of conduct](CODE_OF_CONDUCT.md)
- [Security policy](SECURITY.md)

Small fixes, documentation improvements, reproducible bug reports, and focused feature proposals are especially useful.

## License

Anveesa Nias is released under the [MIT License](LICENSE).
