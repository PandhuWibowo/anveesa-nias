<script setup lang="ts">
type FeatureDoc = {
  name: string
  detail: string
  useCases?: string[]
  workflow?: string[]
  expected?: string
  notes?: string[]
}

type DocsSection = {
  id: string
  title: string
  description: string
  routeHints: string[]
  screenshots: string[]
  features: FeatureDoc[]
}

const sections: DocsSection[] = [
  {
    id: 'auth',
    title: 'Authentication And Navigation',
    description: 'Login, welcome flow, top navigation, and connection switching.',
    routeHints: ['/login', '/welcome'],
    screenshots: ['login-page.png', 'welcome-page.png', 'top-navigation.png', 'connection-picker.png'],
    features: [
      {
        name: 'Login',
        detail: 'Authenticates users before they can access protected database tooling.',
        useCases: ['Sign in before opening database tools.', 'Keep audit and approval activity tied to a real user.', 'Reset a shared browser session by signing out and signing in again.'],
        workflow: ['Open the app.', 'Enter username and password.', 'Submit the login form.', 'Continue to the requested page or the default landing page.'],
        expected: 'A valid user enters the app, while invalid credentials show an error without exposing sensitive account details.',
        notes: ['Do not share accounts because audit logs and approvals depend on user identity.'],
      },
      {
        name: 'Welcome',
        detail: 'Gives users a safe starting page after login or when a route is unavailable.',
        useCases: ['Orient new users.', 'Give limited-permission users a useful landing page.', 'Provide clear links into analytics, data, monitoring, and admin areas.'],
        workflow: ['Sign in.', 'Review available product areas.', 'Open the feature area allowed for the account.'],
        expected: 'Users see a clear starting point and can continue into permitted workflows.',
      },
      {
        name: 'Top Navigation',
        detail: 'Groups the main product surfaces into predictable menus.',
        useCases: ['Move from SQL exploration to audit logs during an investigation.', 'Open admin screens for user or permission changes.', 'Check notifications after approval activity.'],
        workflow: ['Use direct links such as Analytics and Docs.', 'Open grouped menus such as Build, Operate, Govern, and Admin.', 'Confirm the active connection before database-specific work.'],
        expected: 'Users can reach major features without remembering every route.',
      },
      {
        name: 'Connection Picker',
        detail: 'Selects the active database connection context used by connection-aware screens.',
        useCases: ['Switch between staging and reporting databases.', 'Run a saved query against the right connection.', 'Verify connection-specific permissions.'],
        workflow: ['Open the connection picker.', 'Select a database connection.', 'Confirm that the active connection label changed.', 'Continue to SQL, dashboards, monitoring, or governance screens.'],
        expected: 'Connection-sensitive pages refresh around the selected database target.',
        notes: ['Always check the active connection before running write operations.'],
      },
    ],
  },
  {
    id: 'explore',
    title: 'Explore And Query',
    description: 'Data browsing, SQL work, saved queries, and ER diagrams.',
    routeHints: ['/data', '/saved-queries', '/er'],
    screenshots: ['data-view-overview.png', 'data-view-sql-panel.png', 'data-view-query-results.png', 'saved-queries-page.png', 'er-diagram-page.png'],
    features: [
      {
        name: 'Query And Data',
        detail: 'Main workspace for schema inspection, table browsing, and SQL execution.',
        useCases: ['Inspect a table before writing SQL.', 'Run read-only investigation queries.', 'Validate data after an approved change.', 'Export a small result set for review.'],
        workflow: ['Select a connection.', 'Browse schemas, tables, views, and columns.', 'Preview a table or write SQL.', 'Run the query.', 'Review, sort, inspect, or export results.'],
        expected: 'Query results render as a table with clear database feedback when errors occur.',
        notes: ['Use approvals and permission controls for sensitive write workflows.'],
      },
      {
        name: 'Saved Queries',
        detail: 'Stores reusable SQL so teams do not rewrite important queries repeatedly.',
        useCases: ['Save a daily operations query.', 'Reuse an investigation query.', 'Create query assets that can become dashboard blocks.', 'Share known-safe SQL patterns.'],
        workflow: ['Create a saved query.', 'Choose the target connection.', 'Write SQL with readable aliases.', 'Save and reopen it from analytics or dashboard flows.'],
        expected: 'The query can be executed consistently and reused as a dataset-style asset.',
        notes: ['Avoid saving secrets in SQL text.'],
      },
      {
        name: 'AI Analytics',
        detail: 'Helps turn business questions into draft analytics SQL and report ideas.',
        useCases: ['Ask for a trend query without writing SQL from scratch.', 'Draft SQL faster and review before execution.', 'Explore dashboard ideas from a business question.'],
        workflow: ['Choose connection or schema context.', 'Ask an analytics question.', 'Review generated SQL and explanation.', 'Execute only after confirming the SQL is appropriate.'],
        expected: 'Users get a useful draft query and explanation while staying responsible for review.',
        notes: ['Treat AI output as a draft.', 'Do not paste secrets into prompts.'],
      },
      {
        name: 'ER Diagram',
        detail: 'Visual database relationship explorer.',
        useCases: ['Learn an unfamiliar database.', 'Identify join paths between tables.', 'Document relationships for onboarding.', 'Validate foreign-key assumptions.'],
        workflow: ['Select a connection.', 'Load the schema.', 'Inspect tables and relationships.', 'Use the diagram to plan joins or explain structure.'],
        expected: 'Database structure becomes easier to understand visually.',
      },
    ],
  },
  {
    id: 'monitor',
    title: 'Monitoring And Audit',
    description: 'Operational visibility across performance, access, and history.',
    routeHints: ['/dashboard', '/query-performance', '/database-audit', '/audit', '/row-history', '/watcher', '/health'],
    screenshots: ['dashboard-page.png', 'query-performance-page.png', 'database-audit-page.png', 'audit-log-page.png', 'row-history-page.png', 'watchers-page.png', 'health-page.png'],
    features: [
      {
        name: 'Dashboard',
        detail: 'High-level operational overview for application and connection status.',
        useCases: ['Check whether the app and configured connections look healthy.', 'Start an operational investigation.', 'Navigate into more detailed monitoring pages.'],
        workflow: ['Open Operations Overview.', 'Review status cards and metrics.', 'Drill into performance, audit, health, or access activity.'],
        expected: 'Users quickly identify whether deeper investigation is needed.',
      },
      {
        name: 'Query Performance',
        detail: 'Surfaces slow queries, failed queries, and execution trends.',
        useCases: ['Investigate why a report is slow.', 'Find recurring query errors.', 'Identify saved queries that need optimization.'],
        workflow: ['Open Query Performance.', 'Filter or scan by connection, status, duration, or time.', 'Inspect slow or failed entries.', 'Use SQL metadata to optimize.'],
        expected: 'Problematic queries and their execution context are visible.',
      },
      {
        name: 'Database Audit',
        detail: 'Shows live database session and access signals.',
        useCases: ['Investigate suspicious database activity.', 'See long-running sessions.', 'Identify clients or application names using resources.'],
        workflow: ['Open Database Audit.', 'Review active sessions.', 'Inspect unusual sessions.', 'Cross-reference with audit logs or query performance.'],
        expected: 'Administrators can see database activity signals in one place.',
      },
      {
        name: 'Audit Log',
        detail: 'Tracks app-level actions and query events.',
        useCases: ['Review who changed or accessed something.', 'Investigate user activity during support cases.', 'Confirm permission or approval actions.'],
        workflow: ['Open Audit Log.', 'Search by user, action, connection, or text.', 'Sort or filter the table.', 'Inspect the event details.'],
        expected: 'Important app activity can be reconstructed from audit records.',
      },
      {
        name: 'Row History',
        detail: 'Shows row-level INSERT, UPDATE, and DELETE history when tracking is available.',
        useCases: ['Find who changed a record.', 'Compare before and after values.', 'Support rollback or correction workflows.'],
        workflow: ['Open Row History.', 'Search by table, row, action, or user.', 'Inspect before and after values.', 'Decide whether correction is needed.'],
        expected: 'Users can understand the history of a row-level change.',
      },
      {
        name: 'Watchers',
        detail: 'Recurring checks for important query or table activity.',
        useCases: ['Track a business-critical count.', 'Watch for failed jobs or stale data.', 'Monitor a threshold that needs review.'],
        workflow: ['Create a watcher with a focused query.', 'Set an interval.', 'Review current value and recent samples.', 'Adjust the watcher as the process changes.'],
        expected: 'Important signals can be followed over time without building a full dashboard.',
      },
      {
        name: 'Health',
        detail: 'Displays service and connection health signals.',
        useCases: ['Check whether the backend is ready.', 'Verify database reachability.', 'Confirm service status after configuration changes.'],
        workflow: ['Open Health.', 'Review service and connection status.', 'Investigate failed checks from the relevant system layer.'],
        expected: 'Users can separate application availability problems from database availability problems.',
      },
    ],
  },
  {
    id: 'analytics',
    title: 'Analytics Dashboards',
    description: 'Saved-query dashboards, direct SQL previews, chart views, exports, and public embeds.',
    routeHints: ['/analytics', '/ai-analytics', '/dashboards', '/shared-dashboards/:token', '/embed/dashboards/:token', '/embed/dashboards/:token/blocks/:blockId'],
    screenshots: ['analytics-home-page.png', 'analytics-dashboards-page.png', 'dashboard-export-menu.png', 'dashboard-embed-view.png'],
    features: [
      {
        name: 'Analytics Home',
        detail: 'Entry point for saved-query analytics, AI reports, and dashboard building.',
        useCases: ['Open recent saved queries.', 'Start an AI analytics question.', 'Continue into dashboard builder.'],
        workflow: ['Open Analytics.', 'Review recent assets.', 'Choose Saved Queries, AI Analytics, or Dashboards.'],
        expected: 'Users can move to the correct analytics workflow quickly.',
      },
      {
        name: 'Dashboard Builder',
        detail: 'Creates dashboard pages made of multiple chart or table views.',
        useCases: ['Build an operations dashboard.', 'Test direct SQL before saving a block.', 'Compare multiple business metrics on one screen.', 'Prepare dashboards for sharing, exporting, scheduling, or embedding.'],
        workflow: ['Select or create a dashboard.', 'Add a saved-query block or direct SQL block.', 'Preview the result.', 'Choose chart fields and layout size.', 'Save only after the preview is correct.'],
        expected: 'The saved dashboard shows the same layout, chart choices, and data mapping configured by the user.',
      },
      {
        name: 'Charts',
        detail: 'Renders table, KPI, bar, horizontal bar, line, area, scatter, pie, and donut views with animated on-screen charts.',
        useCases: ['Show time trends with line or area charts.', 'Compare categories with bar charts.', 'Show one number with KPI.', 'Inspect exact rows with table blocks.'],
        workflow: ['Preview query data.', 'Pick the chart type that matches the data shape.', 'Map label, value, and series fields.', 'Confirm the chart tells the same story as the table.'],
        expected: 'Labels, numbers, and wording remain visible on screen and in exports.',
      },
      {
        name: 'View Filters',
        detail: 'Per-view search and column filters keep each dashboard block independently focused.',
        useCases: ['Filter a table without changing a chart.', 'Inspect one user or status inside a single block.', 'Compare filtered and unfiltered blocks on one dashboard.'],
        workflow: ['Open a dashboard block.', 'Use the block-level search or column filter.', 'Review the filtered result.', 'Clear the filter when finished.'],
        expected: 'Filters affect the selected view without unexpectedly rewriting the whole dashboard.',
      },
      {
        name: 'Exports',
        detail: 'Exports one view or the whole dashboard to PDF, PNG, Excel, CSV, SQL, or JSON.',
        useCases: ['Send a PDF weekly report.', 'Use PNG in a presentation.', 'Open CSV or Excel for spreadsheet analysis.', 'Export JSON for another system.', 'Export SQL for review.'],
        workflow: ['Open the dashboard.', 'Choose single-view or full-screen export.', 'Select the format.', 'Download the file.', 'Confirm the file matches the dashboard content.'],
        expected: 'Visual exports match the dashboard layout as-is, while data exports preserve the best available result data.',
        notes: ['PDF and PNG are visual outputs.', 'Excel, CSV, SQL, and JSON are data-oriented outputs.'],
      },
      {
        name: 'Shared Views',
        detail: 'Read-only share links expose dashboards by token for users who do not need the full app.',
        useCases: ['Share a metrics page with stakeholders.', 'Open a dashboard on a wallboard.', 'Send a lightweight read-only report link.'],
        workflow: ['Open a dashboard.', 'Copy the share link.', 'Send it to the intended viewer.', 'Rotate or disable the token when access should change.'],
        expected: 'Viewers can see the shared dashboard without entering the full app workflow.',
        notes: ['Treat share links as sensitive.'],
      },
      {
        name: 'Embeds',
        detail: 'Public iframe views can be embedded per dashboard or per chart.',
        useCases: ['Embed a dashboard in an internal portal.', 'Embed one KPI inside another product.', 'Show one chart without exposing the full builder.'],
        workflow: ['Open dashboard builder.', 'Copy the dashboard or chart embed code.', 'Paste the iframe into the target website.', 'Confirm the embedded size and rendering.'],
        expected: 'Embedded views render without the full app navigation and match the dashboard view.',
        notes: ['Only embed dashboards that are safe for the target audience.'],
      },
    ],
  },
  {
    id: 'change',
    title: 'Change Management',
    description: 'SQL approvals, change sets, schema diff, and programmable data changes.',
    routeHints: ['/approvals', '/change-sets', '/data-scripts', '/data-script-requests', '/diff'],
    screenshots: ['approval-requests-list.png', 'change-sets-page.png', 'data-scripts-editor.png', 'data-script-requests-list.png', 'schema-diff-page.png'],
    features: [
      {
        name: 'Approval Requests',
        detail: 'Controlled SQL write approval flow.',
        useCases: ['Review proposed database writes.', 'Require another user to approve sensitive SQL.', 'Keep audit history for approved changes.'],
        workflow: ['Create or receive an approval request.', 'Review SQL, connection, and context.', 'Approve, reject, or request changes.', 'Execute only after policy allows it.'],
        expected: 'Sensitive SQL changes move through a controlled review path.',
      },
      {
        name: 'Change Sets',
        detail: 'Packages database changes so they can be reviewed, validated, and executed together.',
        useCases: ['Prepare related schema or data changes.', 'Review a change before running it.', 'Track execution status for a planned database update.'],
        workflow: ['Create a change set.', 'Add SQL changes.', 'Validate the change.', 'Submit or execute according to permissions.'],
        expected: 'Teams can manage database changes as grouped, reviewable work.',
      },
      {
        name: 'Data Scripts',
        detail: 'Native JavaScript, Python, and PHP scripting for programmable data changes.',
        useCases: ['Preview a scripted data migration.', 'Use language logic for complex transformations.', 'Save script versions for review.'],
        workflow: ['Create or edit a script.', 'Select language and connection.', 'Preview the plan.', 'Save a version or submit a request.'],
        expected: 'Scripted changes can be inspected before execution.',
      },
      {
        name: 'Data Script Requests',
        detail: 'Global queue of data script drafts, plans, and approvals.',
        useCases: ['Review submitted script plans.', 'Track pending script work.', 'Audit who requested and approved a script.'],
        workflow: ['Open Script Requests.', 'Filter or select a request.', 'Review plan and metadata.', 'Approve or reject based on policy.'],
        expected: 'Data script approvals are visible and manageable in one queue.',
      },
      {
        name: 'Schema Diff',
        detail: 'Schema comparison between database environments.',
        useCases: ['Compare staging and production schema.', 'Find missing tables, columns, or indexes.', 'Review drift before deployment.'],
        workflow: ['Choose source and target connections.', 'Run the comparison.', 'Review differences.', 'Plan changes from the diff.'],
        expected: 'Schema drift is visible before it becomes a runtime problem.',
      },
    ],
  },
  {
    id: 'admin',
    title: 'Admin And Governance',
    description: 'Connections, users, permissions, workflows, and security.',
    routeHints: ['/connections', '/users', '/permissions', '/workflows', '/security'],
    screenshots: ['connections-page.png', 'users-page.png', 'permissions-page.png', 'workflows-page.png', 'security-page.png'],
    features: [
      {
        name: 'Connections',
        detail: 'Database connection and environment management.',
        useCases: ['Add a new database target.', 'Separate production, staging, and reporting access.', 'Update stored connection settings.'],
        workflow: ['Open Connections.', 'Create or edit a connection.', 'Set driver, host, credentials, and environment metadata.', 'Test and save.'],
        expected: 'Database targets become available according to permissions.',
      },
      {
        name: 'Users',
        detail: 'Application user administration.',
        useCases: ['Create users.', 'Deactivate accounts.', 'Reset access for team changes.', 'Review account status.'],
        workflow: ['Open Users.', 'Create or select a user.', 'Update profile, role, or status.', 'Save changes.'],
        expected: 'Only authorized users can access the application.',
      },
      {
        name: 'Permissions',
        detail: 'Roles, folders, and permission policy.',
        useCases: ['Grant feature access.', 'Restrict connection access.', 'Separate admin, analyst, and reviewer responsibilities.'],
        workflow: ['Open Permissions.', 'Review roles or folders.', 'Adjust allowed actions.', 'Save and test with the affected account.'],
        expected: 'Users see and use only the features and connections they are allowed to access.',
      },
      {
        name: 'Approval Workflows',
        detail: 'Connection-aware approval routing and approvers.',
        useCases: ['Require approvals for production writes.', 'Route requests to the right reviewers.', 'Apply different rules by connection.'],
        workflow: ['Open Workflows.', 'Create or edit a workflow.', 'Choose connection scope and approvers.', 'Save the policy.'],
        expected: 'Approval requests follow the right review path for the target connection.',
      },
      {
        name: 'Security',
        detail: 'Security-related user and system controls.',
        useCases: ['Review security settings.', 'Manage authentication-related controls.', 'Reduce risk around privileged access.'],
        workflow: ['Open Security.', 'Review available settings.', 'Update controls according to policy.', 'Verify user access behavior.'],
        expected: 'Security-sensitive behavior is configured intentionally and visibly.',
      },
    ],
  },
  {
    id: 'ops',
    title: 'Ops And Platform',
    description: 'Scheduler, backup, health checks, and runtime signals.',
    routeHints: ['/scheduler', '/backup'],
    screenshots: ['scheduler-page.png', 'backup-page.png', 'health-endpoint-check.png'],
    features: [
      {
        name: 'Scheduler',
        detail: 'Recurring jobs and scheduled execution.',
        useCases: ['Run recurring queries.', 'Schedule dashboard-related work.', 'Automate routine operational checks.'],
        workflow: ['Open Scheduler.', 'Create a schedule.', 'Choose target and interval.', 'Save and monitor execution status.'],
        expected: 'Recurring work runs according to the configured schedule.',
      },
      {
        name: 'Backup',
        detail: 'Backup and restore operations.',
        useCases: ['Request database downloads.', 'Prepare a restore operation.', 'Keep operational backups available.'],
        workflow: ['Open Backup.', 'Choose the backup action.', 'Select the connection or target.', 'Run and review the result.'],
        expected: 'Backup operations are visible and controlled from the app.',
      },
      {
        name: 'Runtime Health',
        detail: 'Health checks and service status signals.',
        useCases: ['Confirm the API is running.', 'Check dependency health.', 'Troubleshoot service readiness.'],
        workflow: ['Open Health or call the health endpoint.', 'Review status output.', 'Investigate failed dependency checks.'],
        expected: 'Operators can see whether the service is ready and healthy.',
      },
    ],
  },
  {
    id: 'project',
    title: 'Open Source Project',
    description: 'Public repository guidance, demo access, contribution flow, support, and security policy.',
    routeHints: ['https://github.com/PandhuWibowo/anveesa-nias', 'README.md', 'docs/DEMO.md', 'CONTRIBUTING.md', 'SECURITY.md'],
    screenshots: [],
    features: [
      {
        name: 'Demo System',
        detail: 'Public demo: https://nias.anveesa.com with username admin and password Admin123!.',
        useCases: ['Evaluate the product without local setup.', 'Show the app to contributors or stakeholders.', 'Capture screenshots for documentation.'],
        expected: 'Users can try the system with demo credentials only.',
        notes: ['Do not enter private credentials or sensitive data into the demo.'],
      },
      {
        name: 'GitHub Repository',
        detail: 'Source code and issues live at https://github.com/PandhuWibowo/anveesa-nias.',
        useCases: ['Review source code.', 'Open issues.', 'Submit pull requests.', 'Track project changes.'],
        expected: 'Open-source collaboration happens through the public repository.',
      },
      {
        name: 'Contributing',
        detail: 'Contribution rules, local development checks, and pull request expectations.',
        useCases: ['Prepare a bug fix.', 'Improve documentation.', 'Add a focused feature proposal.'],
        workflow: ['Read CONTRIBUTING.md.', 'Make a focused change.', 'Run relevant checks.', 'Open a pull request with context.'],
        expected: 'Contributions are easier to review and maintain.',
      },
      {
        name: 'Security',
        detail: 'Private vulnerability reporting and safe handling of credentials and share links.',
        useCases: ['Report a vulnerability privately.', 'Understand sensitive configuration handling.', 'Avoid exposing dashboard share tokens.'],
        expected: 'Security issues are handled responsibly without public disclosure first.',
      },
      {
        name: 'License',
        detail: 'The project is released under the MIT License.',
        useCases: ['Understand reuse rights.', 'Check license obligations before embedding or modifying the project.'],
        expected: 'Users know the project license before adopting it.',
      },
    ],
  },
]
</script>

<template>
  <div class="page-shell docs-view">
    <div class="page-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Help</div>
            <div class="page-title">Product Docs</div>
            <div class="page-subtitle">In-app feature documentation and screenshot checklist for the current Anveesa Nias surface.</div>
          </div>
        </section>

        <section class="page-card docs-overview">
          <div class="docs-overview__title">How To Use This Page</div>
          <div class="docs-overview__text">
            This page mirrors the repo documentation inside the dashboard. Open a feature area, review the purpose and use cases, then use the screenshot filenames as your capture plan for `docs/screenshots/`.
          </div>
        </section>

        <section v-for="section in sections" :key="section.id" class="page-card docs-section">
          <div class="docs-section__head">
            <div>
              <div class="docs-section__title">{{ section.title }}</div>
              <div class="docs-section__subtitle">{{ section.description }}</div>
            </div>
          </div>

          <div class="docs-block">
            <div class="docs-label">Routes</div>
            <div class="docs-chip-row">
              <span v-for="routeHint in section.routeHints" :key="routeHint" class="docs-chip">{{ routeHint }}</span>
            </div>
          </div>

          <div class="docs-grid">
            <div class="docs-card docs-card--wide">
              <div class="docs-label">Features</div>
              <div class="docs-feature-list">
                <div v-for="feature in section.features" :key="feature.name" class="docs-feature">
                  <div class="docs-feature__head">
                    <strong>{{ feature.name }}</strong>
                    <span>{{ feature.detail }}</span>
                  </div>

                  <div v-if="feature.useCases?.length" class="docs-feature__block">
                    <div class="docs-feature__label">Use Cases</div>
                    <ul>
                      <li v-for="item in feature.useCases" :key="item">{{ item }}</li>
                    </ul>
                  </div>

                  <div v-if="feature.workflow?.length" class="docs-feature__block">
                    <div class="docs-feature__label">Workflow</div>
                    <ol>
                      <li v-for="item in feature.workflow" :key="item">{{ item }}</li>
                    </ol>
                  </div>

                  <div v-if="feature.expected" class="docs-feature__block">
                    <div class="docs-feature__label">Expected Result</div>
                    <p>{{ feature.expected }}</p>
                  </div>

                  <div v-if="feature.notes?.length" class="docs-feature__block">
                    <div class="docs-feature__label">Notes</div>
                    <ul>
                      <li v-for="item in feature.notes" :key="item">{{ item }}</li>
                    </ul>
                  </div>
                </div>
              </div>
            </div>

            <div class="docs-card">
              <div class="docs-label">Screenshot Checklist</div>
              <div v-if="section.screenshots.length" class="docs-list">
                <div v-for="screenshot in section.screenshots" :key="screenshot" class="docs-list__item">
                  <strong>{{ screenshot }}</strong>
                  <span>Place under `docs/screenshots/{{ screenshot }}`</span>
                </div>
              </div>
              <div v-else class="docs-empty-note">No screenshot checklist for this documentation area.</div>
            </div>
          </div>
        </section>
      </div>
    </div>
  </div>
</template>

<style scoped>
.docs-overview,
.docs-section {
  padding: 20px;
}

.docs-overview__title,
.docs-section__title {
  font-size: 15px;
  font-weight: 700;
  color: var(--text-primary);
}

.docs-overview__text,
.docs-section__subtitle {
  margin-top: 6px;
  font-size: 13px;
  color: var(--text-secondary);
  line-height: 1.6;
}

.docs-block {
  margin-top: 16px;
}

.docs-label {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--text-muted);
  margin-bottom: 10px;
}

.docs-chip-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.docs-chip {
  display: inline-flex;
  align-items: center;
  padding: 6px 10px;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: var(--bg-elevated);
  color: var(--text-secondary);
  font-size: 12px;
}

.docs-grid {
  margin-top: 16px;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.docs-card {
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-elevated);
  padding: 16px;
  min-width: 0;
}

.docs-card--wide {
  grid-column: 1 / -1;
}

.docs-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.docs-feature-list {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.docs-feature {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 14px;
  border-radius: 8px;
  background: rgba(255,255,255,0.02);
  border: 1px solid var(--border);
  min-width: 0;
}

.docs-feature__head {
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.docs-feature__head strong {
  font-size: 14px;
  color: var(--text-primary);
}

.docs-feature__head span {
  font-size: 12px;
  color: var(--text-secondary);
  line-height: 1.5;
}

.docs-feature__block {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.docs-feature__label {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--text-muted);
}

.docs-feature__block p,
.docs-feature__block ul,
.docs-feature__block ol {
  margin: 0;
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.55;
}

.docs-feature__block ul,
.docs-feature__block ol {
  padding-left: 18px;
}

.docs-feature__block li + li {
  margin-top: 4px;
}

.docs-list__item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 12px 14px;
  border-radius: 8px;
  background: rgba(255,255,255,0.02);
  border: 1px solid var(--border);
  min-width: 0;
}

.docs-list__item strong {
  font-size: 13px;
  color: var(--text-primary);
}

.docs-list__item span {
  font-size: 12px;
  color: var(--text-secondary);
  line-height: 1.5;
  overflow-wrap: anywhere;
}

.docs-empty-note {
  padding: 12px 14px;
  border-radius: 8px;
  border: 1px dashed var(--border);
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.5;
}

@media (max-width: 900px) {
  .docs-grid {
    grid-template-columns: 1fr;
  }

  .docs-feature-list {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 720px) {
  .docs-overview,
  .docs-section {
    padding: 16px;
  }
}
</style>
