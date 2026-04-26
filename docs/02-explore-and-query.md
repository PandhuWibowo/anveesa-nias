# Explore And Query

This section covers the day-to-day workflows for browsing database structure, running SQL, saving reusable queries, and understanding relationships.

## Query And Data

Route:
- `/data`

Purpose:
- Main workspace for schema inspection, table browsing, and SQL execution.
- Gives users a single place to move from database structure to query results.

Use cases:
- Inspect a table before writing a query.
- Run a read-only investigation query.
- Validate data after an approved change.
- Export a small result set for offline review.
- Compare row values while debugging a customer issue.

Typical workflow:
1. Select a database connection.
2. Browse schemas, tables, views, and columns.
3. Open a table preview or write SQL.
4. Run the query.
5. Review results, sort columns, inspect cells, or export when needed.

Expected result:
- Query results render as a table.
- Errors return clear database feedback without exposing unnecessary server internals.
- The active connection remains visible so users know what target they are querying.

Feature details:
- Schema tree helps users discover tables and columns.
- SQL editor supports repeated query work.
- Result tables support sorting, pagination, and column visibility.
- Cell inspection helps with long text, JSON, or truncated values.

Risk notes:
- Treat any write-capable SQL workflow as sensitive.
- Use approvals and permission controls for non-read-only operations.

Suggested screenshots:
- `docs/screenshots/data-view-overview.png`
- `docs/screenshots/data-view-table-browser.png`
- `docs/screenshots/data-view-sql-panel.png`
- `docs/screenshots/data-view-query-results.png`

## Saved Queries

Route:
- `/saved-queries`

Purpose:
- Stores reusable SQL so teams do not repeatedly rewrite important queries.
- Provides a bridge between ad hoc SQL and dashboard/report workflows.

Use cases:
- Save a daily operations query.
- Reuse an investigation query across multiple incidents.
- Create a query that can later become a dashboard view.
- Share a known-safe SQL pattern with teammates.

Typical workflow:
1. Create a saved query with a descriptive name.
2. Attach it to a target connection.
3. Write or paste SQL.
4. Save the query.
5. Reopen it later from saved query lists, dashboards, or analytics flows.

Expected result:
- The saved query can be reopened and executed consistently.
- Query names should make the business purpose clear.

Good saved-query naming:
- `Daily Auth Session Breakdown`
- `Failed Payments by Gateway`
- `New Users by Signup Source`
- `Slow Queries Over 500ms`

Notes:
- Prefer clear column aliases because dashboards and exports use result column names.
- Avoid saving secrets inside SQL text.

Screenshot:
- `docs/screenshots/saved-queries-page.png`

## AI Analytics

Route:
- `/ai-analytics`

Purpose:
- Helps users turn business questions into safe analytics SQL and reusable report ideas.

Use cases:
- A non-specialist asks for a trend query without writing SQL from scratch.
- A developer drafts SQL faster and then reviews it before execution.
- A product team explores what dashboard views might answer a question.

Typical workflow:
1. Choose a connection or schema context.
2. Ask an analytics question in natural language.
3. Review generated SQL and explanation.
4. Execute only after confirming the SQL is appropriate.
5. Save useful output as a query or report when supported.

Expected result:
- Generated SQL should be treated as a draft, not an automatic source of truth.
- The user remains responsible for reviewing the query and target connection.

Notes:
- AI output can be wrong or incomplete.
- Prefer read-only generated SQL.
- Never paste secrets into prompts.

## ER Diagram

Route:
- `/er`

Purpose:
- Visualizes database entities and relationships.
- Helps users understand schema structure without reading every table definition manually.

Use cases:
- Learn an unfamiliar database.
- Identify join paths between tables.
- Document domain relationships for onboarding.
- Verify whether foreign key relationships match application assumptions.

Typical workflow:
1. Select a connection.
2. Load the schema or database.
3. Inspect tables and relationships.
4. Use the diagram to plan joins or explain structure to teammates.

Expected result:
- Tables and relationships are easier to understand visually.

Screenshot:
- `docs/screenshots/er-diagram-page.png`
