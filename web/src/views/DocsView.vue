<script setup lang="ts">
const sections = [
  {
    id: 'auth',
    title: 'Authentication And Navigation',
    description: 'Login, welcome flow, top navigation, and connection switching.',
    routeHints: ['/login', '/welcome'],
    screenshots: ['login-page.png', 'welcome-page.png', 'top-navigation.png', 'connection-picker.png'],
    features: [
      { name: 'Login', detail: 'Authenticates users when app auth is enabled.' },
      { name: 'Welcome', detail: 'Landing page and safe fallback route after authentication.' },
      { name: 'Top Navigation', detail: 'Primary app menus for Overview, Explore, Monitor, Change, and Admin.' },
      { name: 'Connection Picker', detail: 'Changes the active database connection context used across the product.' },
    ],
  },
  {
    id: 'explore',
    title: 'Explore And Query',
    description: 'Data browsing, SQL work, saved queries, and ER diagrams.',
    routeHints: ['/data', '/saved-queries', '/er'],
    screenshots: ['data-view-overview.png', 'data-view-sql-panel.png', 'data-view-query-results.png', 'saved-queries-page.png', 'er-diagram-page.png'],
    features: [
      { name: 'Query And Data', detail: 'Main workspace for table browsing, schema inspection, and SQL execution.' },
      { name: 'Saved Queries', detail: 'Reusable SQL library for repeated tasks.' },
      { name: 'ER Diagram', detail: 'Visual database relationship explorer.' },
    ],
  },
  {
    id: 'monitor',
    title: 'Monitoring And Audit',
    description: 'Operational visibility across performance, access, and history.',
    routeHints: ['/dashboard', '/query-performance', '/database-audit', '/audit', '/row-history', '/watcher', '/health'],
    screenshots: ['dashboard-page.png', 'query-performance-page.png', 'database-audit-page.png', 'audit-log-page.png', 'row-history-page.png', 'watchers-page.png', 'health-page.png'],
    features: [
      { name: 'Dashboard', detail: 'High-level operational overview.' },
      { name: 'Query Performance', detail: 'Slow query, error, and trend visibility.' },
      { name: 'Database Audit', detail: 'Live session and external access signals.' },
      { name: 'Audit Log', detail: 'App-level audit trail.' },
      { name: 'Row History', detail: 'Row-level data change history.' },
      { name: 'Watchers', detail: 'Targeted monitoring for important activity.' },
      { name: 'Health', detail: 'Connection and service health status.' },
    ],
  },
  {
    id: 'analytics',
    title: 'Analytics Dashboards',
    description: 'Saved-query dashboards, direct SQL previews, chart views, exports, and public embeds.',
    routeHints: ['/analytics', '/ai-analytics', '/dashboards', '/shared-dashboards/:token', '/embed/dashboards/:token', '/embed/dashboards/:token/blocks/:blockId'],
    screenshots: ['analytics-home-page.png', 'analytics-dashboards-page.png', 'dashboard-export-menu.png', 'dashboard-embed-view.png'],
    features: [
      { name: 'Analytics Home', detail: 'Entry point for saved-query analytics, AI reports, and dashboard building.' },
      { name: 'Dashboard Builder', detail: 'Create dashboard views from saved queries or direct SQL, preview the result, then save only when the view is correct.' },
      { name: 'Charts', detail: 'Render table, KPI, bar, horizontal bar, line, area, scatter, pie, and donut views with animated on-screen charts.' },
      { name: 'View Filters', detail: 'Per-view search and column filters keep each dashboard block independently focused.' },
      { name: 'Exports', detail: 'Export a single view or the whole dashboard to PDF, PNG, Excel, CSV, SQL, or JSON with output matched to the dashboard.' },
      { name: 'Shared Views', detail: 'Read-only share links expose dashboards by token for users who do not need the full app.' },
      { name: 'Embeds', detail: 'Public iframe views can be embedded per dashboard or per chart.' },
    ],
  },
  {
    id: 'change',
    title: 'Change Management',
    description: 'SQL approvals, change sets, schema diff, and programmable data changes.',
    routeHints: ['/approvals', '/change-sets', '/data-scripts', '/data-script-requests', '/diff'],
    screenshots: ['approval-requests-list.png', 'change-sets-page.png', 'data-scripts-editor.png', 'data-script-requests-list.png', 'schema-diff-page.png'],
    features: [
      { name: 'Approval Requests', detail: 'Controlled SQL write approval flow.' },
      { name: 'Change Sets', detail: 'Validated database change packaging and execution.' },
      { name: 'Data Scripts', detail: 'Native JavaScript, Python, and PHP data change scripting.' },
      { name: 'Data Script Requests', detail: 'Global queue of all data script drafts and approvals.' },
      { name: 'Schema Diff', detail: 'Schema comparison between environments.' },
    ],
  },
  {
    id: 'admin',
    title: 'Admin And Governance',
    description: 'Connections, users, permissions, workflows, and security.',
    routeHints: ['/connections', '/users', '/permissions', '/workflows', '/security'],
    screenshots: ['connections-page.png', 'users-page.png', 'permissions-page.png', 'workflows-page.png', 'security-page.png'],
    features: [
      { name: 'Connections', detail: 'Database connection and environment management.' },
      { name: 'Users', detail: 'Application user administration.' },
      { name: 'Permissions', detail: 'Roles, folders, and permission policy.' },
      { name: 'Approval Workflows', detail: 'Connection-aware approval routing and approvers.' },
      { name: 'Security', detail: 'Security-related user or system controls.' },
    ],
  },
  {
    id: 'ops',
    title: 'Ops And Platform',
    description: 'Scheduler, backup, health checks, and runtime signals.',
    routeHints: ['/scheduler', '/backup'],
    screenshots: ['scheduler-page.png', 'backup-page.png', 'health-endpoint-check.png'],
    features: [
      { name: 'Scheduler', detail: 'Recurring jobs and scheduled execution.' },
      { name: 'Backup', detail: 'Backup and restore operations.' },
      { name: 'Runtime Health', detail: 'Health checks and service status signals.' },
    ],
  },
  {
    id: 'project',
    title: 'Open Source Project',
    description: 'Public repository guidance, demo access, contribution flow, support, and security policy.',
    routeHints: ['https://github.com/PandhuWibowo/anveesa-nias', 'README.md', 'docs/DEMO.md', 'CONTRIBUTING.md', 'SECURITY.md'],
    screenshots: [],
    features: [
      { name: 'Demo System', detail: 'Public demo: https://nias.anveesa.com with username admin and password Admin123!.' },
      { name: 'GitHub Repository', detail: 'Source code and issues live at https://github.com/PandhuWibowo/anveesa-nias.' },
      { name: 'Contributing', detail: 'Contribution rules, local development checks, and pull request expectations.' },
      { name: 'Security', detail: 'Private vulnerability reporting and safe handling of credentials and share links.' },
      { name: 'License', detail: 'The project is released under the MIT License.' },
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
            This page mirrors the repo documentation and gives a practical checklist by feature area. Use the screenshot filenames as your capture plan, then save the actual images into `docs/screenshots/` in the repository.
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
            <div class="docs-card">
              <div class="docs-label">Features</div>
              <div class="docs-list">
                <div v-for="feature in section.features" :key="feature.name" class="docs-list__item">
                  <strong>{{ feature.name }}</strong>
                  <span>{{ feature.detail }}</span>
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
  border-radius: 16px;
  background: var(--bg-elevated);
  padding: 16px;
  min-width: 0;
}

.docs-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.docs-list__item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 12px 14px;
  border-radius: 12px;
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
  border-radius: 12px;
  border: 1px dashed var(--border);
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.5;
}

@media (max-width: 900px) {
  .docs-grid {
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
