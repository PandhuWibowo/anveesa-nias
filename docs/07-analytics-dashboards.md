# Analytics Dashboards

Analytics dashboards turn SQL results into reusable views, shareable reports, exported files, and embeddable dashboard surfaces.

## Analytics Home

Route:
- `/analytics`

Purpose:
- Acts as the entry point for saved query analytics, dashboard building, and AI-assisted reporting.
- Gives users a single place to continue from reusable SQL assets into visual analysis.

Use cases:
- A data analyst wants to see recent saved queries before creating a dashboard.
- A product manager needs a quick path into AI Analytics for a business question.
- A team lead wants to open dashboard builder without remembering the dashboard route.
- A maintainer wants a lightweight analytics landing page that connects saved queries, dashboards, and reports.

Typical workflow:
1. Open Analytics from the main navigation.
2. Review recent saved queries or pinned reports.
3. Continue to Saved Queries, AI Analytics, or Dashboards.
4. Build or review the analytics asset that matches the current task.

Expected result:
- Users understand which analytics surfaces are available and can move to the correct workflow quickly.

Screenshot:
- `docs/screenshots/analytics-home-page.png`

## Dashboard Builder

Route:
- `/dashboards`

Purpose:
- Creates dashboard pages made of multiple chart or table views.
- Supports saved-query blocks and direct SQL blocks.
- Lets users preview a query result before saving the dashboard view.

Use cases:
- Build an operations dashboard from saved queries.
- Test a new SQL query directly in the dashboard builder before saving it.
- Compare several business metrics on one screen.
- Create separate dashboard blocks for a chart and the underlying table.
- Prepare a dashboard that can later be shared, exported, scheduled, or embedded.

Typical workflow:
1. Select or create a dashboard.
2. Add a block from a saved query or enter direct SQL.
3. Preview the SQL result.
4. Choose a chart type and configure labels, values, dimensions, and layout size.
5. Save the block only after the preview looks correct.
6. Arrange blocks on the dashboard canvas.

Expected result:
- The dashboard shows the same layout and chart choices that the user configured.
- Direct SQL previews let users validate data before committing a view.
- Saved-query blocks stay reusable and easier to maintain.

Design notes:
- Use clear block titles because exports and embeds use the same names.
- Use SQL column aliases that are readable to business users.
- Keep one dashboard focused on one audience or decision area.

Screenshot:
- `docs/screenshots/analytics-dashboards-page.png`

## Chart Views

Route:
- `/dashboards`

Purpose:
- Renders query results as visual views inside dashboard blocks.
- Supports both summary views and detailed result inspection.

Supported chart types:
- Table: best for row-level review and exact values.
- KPI: best for one primary number.
- Bar: best for category comparison.
- Horizontal bar: best for long category labels.
- Line: best for trends over time.
- Area: best for trend volume and cumulative feeling.
- Scatter: best for relationships between two numeric values.
- Pie: best for small part-of-total comparisons.
- Donut: best for compact distribution views.

Use cases:
- Show daily login sessions as a bar or line chart.
- Show total active users as a KPI.
- Show login distribution by user as a donut chart.
- Show user ID versus login count as a scatter chart.
- Show the raw SQL result as a table beside the chart.

Typical workflow:
1. Preview the query result.
2. Pick the chart type that matches the shape of the data.
3. Select label, value, and optional series fields.
4. Save the block.
5. Confirm that the chart tells the same story as the table result.

Expected result:
- Charts keep labels, numbers, and wording visible on screen and in exports.
- Animated charts improve the on-screen experience without changing the exported data.

Notes:
- A chart is only as correct as its SQL result and field mapping.
- Use table blocks when the exact rows matter more than the visual pattern.

## View Filters

Route:
- `/dashboards`

Purpose:
- Adds filter controls inside each dashboard view instead of relying only on outside dashboard-level filters.
- Lets each block stay independently focused.

Use cases:
- Search a table block without changing a chart block.
- Filter one chart by a category while leaving other dashboard blocks unchanged.
- Inspect a specific user, status, date, or keyword inside one result set.
- Compare filtered and unfiltered blocks on the same dashboard.

Typical workflow:
1. Open a dashboard block.
2. Use the block-level search field or column filter.
3. Review the filtered rows or chart result.
4. Clear the filter to return to the full block result.

Expected result:
- Filters affect the selected view and do not unexpectedly rewrite the whole dashboard.

Notes:
- Prefer server-side SQL filters for very large datasets.
- Use block filters for focused inspection after the dashboard query has already returned data.

## Export Features

Route:
- `/dashboards`

Purpose:
- Exports dashboard content for reporting, sharing, backup, review, or downstream processing.
- Supports exporting either one dashboard view or the whole dashboard screen.

Supported formats:
- PDF: visual report for review, email, or archive.
- PNG: image snapshot of a view or dashboard screen.
- Excel: formatted workbook with summary, separate sheets, tables, and charts where supported.
- CSV: tabular data export for spreadsheets or external tools.
- SQL: SQL text export for audit, review, or migration into another query tool.
- JSON: structured dashboard export that includes dashboard metadata, block definitions, SQL, columns, rows, counts, durations, and errors.

Use cases:
- Export the whole dashboard as a PDF for a weekly report.
- Export a single chart as PNG for a presentation.
- Export a table block as CSV for spreadsheet analysis.
- Export the dashboard as Excel when stakeholders need formatted sheets and workbook charts.
- Export JSON when another system needs the dashboard data and metadata.
- Export SQL when a reviewer wants to inspect the exact query behind the result.

Typical workflow:
1. Open the dashboard.
2. Choose whether to export a single view or the full screen.
3. Select the target format.
4. Download the generated file.
5. Open the file and confirm that the exported layout, labels, numbers, and rows match the dashboard view.

Expected result:
- Visual exports match the existing dashboard layout as-is, including scattered layouts and block placement.
- Data exports preserve the best available result data instead of only exporting a screenshot.
- Excel exports are readable, styled, and split into useful sheets.
- JSON exports are intentionally structured, not a visual file.

Format notes:
- PDF and PNG are visual outputs.
- Excel and CSV are spreadsheet/data outputs.
- SQL exports query definitions.
- JSON exports machine-readable dashboard metadata and query results.

Screenshot:
- `docs/screenshots/dashboard-export-menu.png`

## Shared Dashboards

Route:
- `/shared-dashboards/:token`

Purpose:
- Provides a public read-only dashboard view through a share token.
- Lets teams share dashboards with people who do not need full application access.

Use cases:
- Share a read-only metrics page with a stakeholder.
- Open a dashboard on a wallboard or monitoring display.
- Send a report link for lightweight review.

Typical workflow:
1. Create or open a dashboard.
2. Enable or copy the share link.
3. Send the shared dashboard URL to the intended viewer.
4. Rotate or disable the token when the share should no longer be available.

Expected result:
- Viewers can see the shared dashboard without entering the full app workflow.
- Shared pages remain read-only.

Security notes:
- Treat share links as sensitive.
- Do not share dashboards that expose private, secret, or regulated data.
- Rotate shared links when access should change.

## Dashboard Embeds

Routes:
- `/embed/dashboards/:token`
- `/embed/dashboards/:token/blocks/:blockId`

Purpose:
- Provides iframe-compatible public views for embedding dashboards or individual charts in another website.
- Supports both full-dashboard embeds and per-chart embeds.

Use cases:
- Embed a full analytics dashboard in an internal portal.
- Embed one KPI or chart inside a customer support page.
- Place a live operational chart in a documentation or status page.
- Show one dashboard block in another product without exposing the full dashboard builder.

Typical workflow:
1. Open the dashboard builder.
2. Copy the embed code for the whole dashboard or a single chart.
3. Paste the iframe into the target website.
4. Confirm that the embedded view renders at the expected size.
5. Adjust the containing website layout if the iframe needs more width or height.

Expected result:
- Embedded dashboards and charts render without the full app navigation.
- Per-chart embeds show only the selected dashboard block.
- The embedded output matches the dashboard view rather than using a separate design.

Security notes:
- Embed URLs are public when token access is enabled.
- Only embed dashboards that are safe for the target audience.
- Disable or rotate tokens if an embed should stop working.

Screenshot:
- `docs/screenshots/dashboard-embed-view.png`
