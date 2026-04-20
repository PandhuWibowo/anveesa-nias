# Ops And Platform

## Scheduler

Route:
- `/scheduler`

Purpose:
- Schedules recurring tasks or query jobs.

Screenshot:
- `docs/screenshots/scheduler-page.png`

## Backup

Route:
- `/backup`

Purpose:
- Handles backup and restore operations.

Screenshot:
- `docs/screenshots/backup-page.png`

## Production Runtime Notes

Important backend/runtime requirements:
- PostgreSQL or MySQL database configured through `DATABASE_URL`
- `JWT_SECRET` set in production
- `NIAS_ENCRYPTION_KEY` set in production
- `CORS_ORIGIN` set to the deployed frontend origin

Data Scripts native runtimes:
- `node`
- `python3`
- `php`

Container deployment:
- Rebuild image after Dockerfile updates.
- Restart the container after changing image or env vars.
- Verify routes like `/api/data-change-plans` after deploy.

Recommended operational screenshots:
- `docs/screenshots/docker-container-status.png`
- `docs/screenshots/health-endpoint-check.png`
