# Ops And Platform

Ops and platform features cover scheduled work, backups, health visibility, and runtime signals inside the application.

## Scheduler

Route:
- `/scheduler`

Purpose:
- Schedules recurring tasks or query jobs.
- Turns repeated manual checks into planned jobs.

Use cases:
- Run a recurring report query.
- Schedule a repeated maintenance check.
- Automate a data validation query.
- Keep operational tasks visible to administrators.

Typical workflow:
1. Open scheduler.
2. Create a scheduled job.
3. Select the target query, task, or connection context.
4. Set interval or timing.
5. Save and monitor recent runs.

Expected result:
- The task runs according to its schedule and exposes status/history for review.

Notes:
- Keep scheduled queries focused.
- Avoid schedules that generate unnecessary database load.
- Review failed jobs regularly.

Screenshot:
- `docs/screenshots/scheduler-page.png`

## Backup

Route:
- `/backup`

Purpose:
- Provides backup and restore operations where supported by the app configuration.

Use cases:
- Create a manual backup before a risky administrative change.
- Review backup status.
- Restore app-managed data in a controlled situation.

Typical workflow:
1. Open backup.
2. Review available backup actions.
3. Trigger backup or restore when appropriate.
4. Confirm the result and audit any follow-up actions.

Expected result:
- Backup operations are visible and deliberate.

Notes:
- Backups can contain sensitive connection metadata and audit data.
- Store backup artifacts securely.

Screenshot:
- `docs/screenshots/backup-page.png`

## Runtime Health

Route:
- `/health`

Purpose:
- Shows whether core services and connection checks are healthy.

Use cases:
- Confirm the backend is responding.
- Check a connection after credential rotation.
- Verify service readiness after configuration changes.

Typical workflow:
1. Open health.
2. Review status cards and failed checks.
3. Investigate any failing dependency or connection.

Expected result:
- Users can distinguish between app-level and database-level health problems.

Screenshot:
- `docs/screenshots/health-endpoint-check.png`

## Data Script Runtime Notes

Purpose:
- Documents the native runtimes used by data-script execution.

Supported runtime commands:
- `node`
- `python3`
- `php`

Use cases:
- A script author chooses the best language for a planned data transformation.
- A maintainer checks which runtime must be available for a script type.

Notes:
- Script behavior should be deterministic and reviewable.
- Avoid scripts that depend on hidden local state.
