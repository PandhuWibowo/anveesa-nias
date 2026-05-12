## Summary

Adds Search Policy Management as a dedicated feature for Elasticsearch and OpenSearch connections. Users can now view and manage ILM lifecycle policies, index templates, app-level monitoring rules, and per-index shard/allocation settings — all from a single view. Also adds a per-index delete action in the Search Browser with confirmation, and fixes a PostgreSQL type error when reading nullable timestamp columns.

## Type of Change

- [ ] Bug fix
- [x] New feature
- [x] Enhancement to existing feature
- [ ] Refactor (no functional change)
- [ ] Documentation
- [ ] Chore / dependency update

## What Changed

- **Search Browser** — added a delete button on each index row (hover to reveal, confirmation modal before deleting)
- **Search Policies view** — new route `/search-policies` with 4 tabs:
  - **ILM Policies** — list, create, edit, and delete built-in ES/OS lifecycle policies
  - **Index Templates** — list, create, edit, and delete index templates with full JSON editor
  - **App Policies** — NIAS-managed rules (size alert, auto-delete by size or age) stored in the internal DB; includes a Run button that evaluates violations across all indices and shows results inline
  - **Shard Rules** — browse per-index shard/replica/allocation settings and update dynamic settings
- **Backend** — new `search_management.go` (ILM, template, index settings proxies) and `search_app_policies.go` (CRUD + evaluation engine); 12 new routes registered
- **DB migration** — new `search_app_policies` table (additive)
- **Navigation** — "Search Policies" added to the Database → Search & Observability section in TopNav with a dedicated icon
- **Bug fix** — `COALESCE(last_run_at, '')` on a `TIMESTAMP` column caused PostgreSQL error 22007; fixed by removing COALESCE and scanning with `sql.NullString`
- **PR template** — updated with Type of Change, What Changed, Related Issues sections and expanded checklist

## Related Issues

<!-- Link any related issues: Closes #123 -->

## Verification

- [ ] `cd server && go test ./...`
- [ ] `cd web && npm run build`
- [ ] Manual UI verification, if applicable
- [ ] Tested on PostgreSQL / MySQL (if DB changes are included)
- [ ] Tested on SQLite (default local setup)

## Checklist

- [ ] The change is focused and reviewable.
- [ ] New API endpoints are protected with appropriate permission checks.
- [ ] Database migrations are backward-compatible (additive only — no column drops or renames).
- [ ] Error messages are user-friendly and do not leak internal details.
- [ ] Documentation was updated when behavior or configuration changed.
- [ ] No secrets, local databases, logs, or generated build output are committed.
- [ ] Security and permission impact was considered.

## Screenshots

Add screenshots or video for visible UI changes.
