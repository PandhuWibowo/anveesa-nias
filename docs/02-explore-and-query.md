# Explore And Query

This section covers the day-to-day workflows for browsing database structure, running SQL, saving reusable queries, understanding relationships, and inspecting messaging metadata.

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

## AI Settings

Route:
- `/settings`

Purpose:
- Stores personal AI provider settings used by AI-assisted SQL and analytics workflows.
- Lets users configure their API key, base URL, and model when personal provider settings are allowed.

Use cases:
- A user connects AI features to their own provider account.
- A maintainer tests model configuration without changing global defaults.
- A team member verifies which AI settings are active before using AI Analytics.

Typical workflow:
1. Open Build, then AI Settings.
2. Enter or update provider details.
3. Save the settings.
4. Return to AI Analytics or SQL assistance and confirm the provider behavior.

Expected result:
- AI-enabled screens use the saved settings or the configured fallback provider.

Notes:
- Treat provider keys as secrets.
- Use the minimum provider access required for analytics and SQL assistance.

Screenshot:
- `docs/screenshots/ai-settings-page.png`

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

## Kafka Browser

Route:
- `/kafka`

Purpose:
- Inspects Kafka topic metadata, partition counts, replication factors, latest messages, and consumer groups from configured Kafka connections.
- Gives operators a read-only view into Kafka structure without requiring a separate Kafka console.

Use cases:
- Confirm that expected topics exist after a deployment.
- Check partition and replication settings before investigating consumer behavior.
- Preview recent messages in a topic without joining a consumer group.
- Produce a controlled test message in development or test clusters.
- Review known consumer groups for a broker connection.
- Inspect committed offsets and lag for a consumer group.
- Validate Kafka connectivity from the application environment.

Typical workflow:
1. Create or select a Kafka connection from Admin, then Connections.
2. Open Messaging, then Kafka.
3. Choose the Kafka connection.
4. Review topic metadata, latest messages, consumer groups, and lag.
5. Use Produce or Manage only when the account has elevated Kafka permissions.

Expected result:
- Kafka topics, latest messages, and consumer groups load for the selected broker when the account has `kafka.view`.
- Message production requires `kafka.produce`.
- Topic creation, deletion, and partition increases require `kafka.manage`.
- Connection or broker errors are shown without exposing stored credentials.

Risk notes:
- Topic message preview is read-only.
- Producing messages can affect downstream consumers and should be limited to trusted roles.
- Topic deletion is irreversible and should be restricted to administrators.
- Treat broker addresses and SASL credentials as sensitive connection configuration.

Screenshot:
- `docs/screenshots/kafka-page.png`
