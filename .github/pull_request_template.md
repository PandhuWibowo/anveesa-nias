## Summary

<!-- 1-3 sentences describing what this PR does and why -->

## Type of Change

- [ ] Bug fix
- [ ] New feature
- [ ] Enhancement to existing feature
- [ ] Refactor (no functional change)
- [ ] Documentation
- [ ] Chore / dependency update

## What Changed

<!-- Bullet points grouped by area: frontend / backend / db / bug fix -->

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
