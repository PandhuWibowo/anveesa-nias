# Monitoring And Audit

Monitoring and audit features help teams understand application usage, database activity, performance risk, and historical changes.

## Dashboard

Route:
- `/dashboard`

Purpose:
- Shows a high-level operational overview.
- Gives users a quick status page before drilling into deeper monitoring screens.

Use cases:
- Check whether the app and configured connections look healthy.
- Give administrators a summary before investigating details.
- Provide a landing point for operational monitoring.

Typical workflow:
1. Open the dashboard.
2. Review high-level metrics and status cards.
3. Navigate to detailed monitoring pages when something needs attention.

Expected result:
- Users can quickly identify whether they need to investigate performance, audit, health, or access activity.

Screenshot:
- `docs/screenshots/dashboard-page.png`

## Query Performance

Route:
- `/query-performance`

Purpose:
- Surfaces slow queries, failed queries, and execution trends.
- Helps teams find expensive SQL and recurring errors.

Use cases:
- Investigate why a report is slow.
- Find queries that frequently fail.
- Identify whether a recent change increased query duration.
- Decide which saved queries need optimization.

Typical workflow:
1. Open query performance.
2. Filter or scan by connection, status, duration, or time period.
3. Inspect slow or failed entries.
4. Use the SQL and metadata to optimize indexes, query structure, or usage patterns.

Expected result:
- The user can identify problematic queries and the context where they occurred.

Notes:
- A slow query may be caused by missing indexes, large result sets, network latency, locking, or inefficient SQL.
- Use database-level tooling for final query-plan diagnosis.

Screenshot:
- `docs/screenshots/query-performance-page.png`

## Database Audit

Route:
- `/database-audit`

Purpose:
- Shows active database session and access signals.
- Helps administrators understand who or what is connected to databases.

Use cases:
- Investigate suspicious database activity.
- See long-running sessions.
- Identify clients or application names using database resources.
- Review active users during an incident.

Typical workflow:
1. Open database audit.
2. Review active sessions and access metadata.
3. Filter or inspect sessions that look unusual.
4. Cross-reference with audit logs or query performance when needed.

Expected result:
- Administrators can see database activity signals in one place.

Screenshot:
- `docs/screenshots/database-audit-page.png`

## Audit Log

Route:
- `/audit`

Purpose:
- Tracks app-level actions and audit events.
- Provides accountability for sensitive workflows.

Use cases:
- Review who created, edited, or deleted records inside the app.
- Investigate user activity during a support case.
- Confirm that a permission or approval action happened.
- Export audit data for review.

Typical workflow:
1. Open audit log.
2. Search by user, action, connection, or text.
3. Sort or filter the table.
4. Inspect details for the relevant event.

Expected result:
- Users can reconstruct important app-level activity.

Screenshot:
- `docs/screenshots/audit-log-page.png`

## Row History

Route:
- `/row-history`

Purpose:
- Shows row-level INSERT, UPDATE, and DELETE history where row-history tracking is available.

Use cases:
- Find who changed a specific record.
- Compare before and after values.
- Support rollback or correction workflows.
- Explain data changes to another team.

Typical workflow:
1. Open row history.
2. Search or filter by table, row, action, or user.
3. Inspect before/after values.
4. Decide whether follow-up action is needed.

Expected result:
- Users can understand the history of a row-level change.

Screenshot:
- `docs/screenshots/row-history-page.png`

## Watchers

Route:
- `/watcher`

Purpose:
- Monitors important query or table activity using recurring checks.

Use cases:
- Track a business-critical count.
- Watch for failed jobs or stale data.
- Monitor a threshold that should trigger review.
- Keep a lightweight trend signal visible without building a full dashboard.

Typical workflow:
1. Create a watcher with a query that returns a focused value.
2. Set an interval.
3. Review current value and recent samples.
4. Adjust the watcher as the monitored process changes.

Expected result:
- Users can follow a small set of important signals over time.

Screenshot:
- `docs/screenshots/watchers-page.png`

## Health

Route:
- `/health`

Purpose:
- Displays service and connection health.
- Helps users separate application availability problems from database availability problems.

Use cases:
- Check whether the backend is ready.
- Verify whether configured connections are reachable.
- Confirm service status after a configuration change.

Typical workflow:
1. Open health.
2. Review service and connection status.
3. Investigate failed checks from the relevant system layer.

Expected result:
- Users can quickly understand whether the app and database targets are healthy.

Screenshot:
- `docs/screenshots/health-page.png`
