# Authentication And Navigation

This section explains the entry points and navigation patterns that shape the daily user experience.

## Login

Route:
- `/login`

Purpose:
- Authenticates users before they can access protected database tooling.
- Keeps database connections, saved queries, audit logs, and admin screens behind application-level access control.

Use cases:
- A team member signs in before using the data workspace.
- An administrator verifies that only active users can access internal database tools.
- A shared browser session is reset by logging out and signing in with the correct account.

Typical workflow:
1. Open the app.
2. Enter username and password.
3. Submit the form.
4. Continue to the default landing page or the route requested before authentication.

Expected result:
- A valid user is redirected into the app.
- Invalid credentials show an error and do not reveal whether the username exists.

Notes:
- Authentication behavior depends on server configuration.
- Users should not share accounts because audit, approval, and row-history features rely on user identity.

Screenshot:
- `docs/screenshots/login-page.png`

## Welcome

Route:
- `/welcome`

Purpose:
- Gives users an initial orientation after login.
- Works as a safe fallback when a user lacks access to a requested route.

Use cases:
- A new user needs to understand where to start.
- A user with limited permissions lands somewhere useful instead of seeing a dead end.
- A maintainer wants a neutral page that links into primary product areas.

Typical workflow:
1. Sign in.
2. Review available product areas.
3. Navigate to analytics, data browsing, monitoring, or admin screens depending on permissions.

Expected result:
- Users see a clear starting point and can continue to the workflows they are allowed to use.

Screenshot:
- `docs/screenshots/welcome-page.png`

## Top Navigation

Purpose:
- Provides the global navigation shell for the product.
- Groups features into predictable areas so users can move between data work, monitoring, change management, and governance.

Primary elements:
- Brand and version.
- Active connection indicator.
- Direct links for Analytics and Docs.
- Grouped menus for Analytics, Database, Messaging, Operations, Governance, and Admin.
- Notification entry point.
- User/account menu.

Menu map:
- Analytics: Analytics Home, Dashboards, Saved Queries, AI Analytics, and AI Settings.
- Database: SQL Studio, ER Diagram, Schema Diff, Row History, and Redis Browser.
- Messaging: Laravel Queue and Kafka.
- Operations: Operations Overview, Query Performance, Database Audit, Audit Log, Notifications, Watchers, and Health.
- Governance: Approvals, Change Sets, Data Scripts, Script Requests, Backup, Scheduler, and Workflows.
- Admin: Connections, Users, Roles & Permissions, and Access Groups.

Use cases:
- Switching from SQL exploration to audit logs while investigating an incident.
- Opening admin screens to change a user role.
- Checking notifications after a workflow approval request.

Typical workflow:
1. Use the navigation groups to choose an area.
2. Confirm the active connection context when working with database-specific screens.
3. Use the user menu for account-level actions.

Expected result:
- Users can reach major features without remembering every route.
- Navigation items appear only when the current user has the required permission.

Notes:
- Global schema search and the `Cmd/Ctrl+K` shortcut were removed from the navigation shell.

Screenshot:
- `docs/screenshots/top-navigation.png`

## Product Docs

Route:
- `/docs`

Purpose:
- Mirrors the repo documentation inside the application.
- Gives users a feature guide, route map, and screenshot capture checklist without leaving the dashboard.

Use cases:
- A contributor checks which screens need screenshots.
- A tester confirms expected behavior while moving through feature areas.
- A user finds the route or menu where a feature lives.

Typical workflow:
1. Open Docs from the top navigation.
2. Use the Docs Menu to jump to the relevant feature area.
3. Review routes, expected results, and screenshot filenames.
4. Capture or update documentation assets as needed.

Expected result:
- In-app docs stay aligned with the Markdown feature guide and current navigation menus.
- The Docs Menu gives quick access to every major documentation section.

Screenshot:
- `docs/screenshots/docs-page.png`

## Connection Picker

Purpose:
- Selects the active database connection context used by connection-aware screens.
- Reduces accidental work on the wrong database by keeping the current target visible.

Use cases:
- A data analyst switches between staging and reporting databases.
- A developer runs the same saved query against different connections.
- An admin verifies permission boundaries for a specific connection.

Typical workflow:
1. Open the connection picker.
2. Select the desired database connection.
3. Confirm that the active connection label changes.
4. Continue to schema browsing, SQL execution, dashboards, or monitoring.

Expected result:
- Connection-sensitive screens refresh around the selected target.
- If a user lacks access to a connection, it should not be usable from the picker.

Notes:
- Always check the active connection before running write operations.
- Permission and approval behavior may vary by connection.

Screenshot:
- `docs/screenshots/connection-picker.png`
