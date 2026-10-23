// Single source of truth for app navigation. The sidebar shows a handful of
// sections; each section's pages appear as tabs at the top of the content
// panel (SectionTabs.vue). The command palette and Home read the same data.
// Permission strings mirror the route guards in router/index.ts.

export type NavItem = {
  name: string
  label: string
  desc: string
  icon: string
  permissionsAny?: string[]
  query?: Record<string, string>
  keywords?: string
  /** Only offered when the active connection uses one of these drivers. */
  drivers?: string[]
}

export type NavSection = {
  id: string
  label: string
  icon: string
  items: NavItem[]
  /** Sections whose tools depend on the active connection (label and tabs follow it). */
  connectionScoped?: boolean
}

const SQL = ['postgres', 'mysql', 'mariadb', 'mssql', 'sqlite']
const SEARCH = ['elasticsearch', 'opensearch']
const BUCKETS = ['s3_aws', 's3_gcp', 's3_oss', 's3_obs']

export const NAV_SECTIONS: NavSection[] = [
  {
    id: 'workspace',
    label: 'Connection tools',
    icon: 'database',
    connectionScoped: true,
    items: [
      { name: 'data', label: 'SQL Studio', desc: 'Browse tables and run SQL', icon: 'table', drivers: SQL, permissionsAny: ['sqlstudio.access'], keywords: 'query editor postgres mysql' },
      { name: 'database-objects', label: 'Objects', desc: 'Indexes, views, functions, triggers', icon: 'objects', drivers: SQL, permissionsAny: ['sqlstudio.access'] },
      { name: 'er', label: 'ER diagram', desc: 'Table relationships', icon: 'er', drivers: SQL, permissionsAny: ['er.view'] },
      { name: 'diff', label: 'Schema diff', desc: 'Compare schemas across environments', icon: 'diff', drivers: SQL, permissionsAny: ['schema.diff.view'] },
      { name: 'row-history', label: 'Row history', desc: 'Row-level change log', icon: 'rowhistory', drivers: SQL, permissionsAny: ['rowhistory.view'] },
      { name: 'database-audit', label: 'Sessions', desc: 'Live sessions and external access', icon: 'shield', drivers: SQL, permissionsAny: ['databaseaudit.view'] },
      { name: 'database-logs', label: 'Logs', desc: 'Slow log, error log, SQL audit', icon: 'audit', drivers: ['postgres'], permissionsAny: ['schema.browse', 'connections.view'], keywords: 'database logs slow query' },
      { name: 'db-users', label: 'Users', desc: 'Database users and grants', icon: 'users', drivers: ['postgres', 'mysql', 'mariadb'], permissionsAny: ['dbusers.manage'], keywords: 'database users grants' },
      { name: 'pg-replication', label: 'Replication', desc: 'Publications and subscriptions', icon: 'replicate', drivers: ['postgres'], permissionsAny: ['pgreplication.view', 'pgreplication.manage'], keywords: 'postgres' },
      { name: 'pg-parameters', label: 'Parameters', desc: 'pg_settings and ALTER SYSTEM', icon: 'sliders', drivers: ['postgres'], permissionsAny: ['pgparameters.view', 'pgparameters.manage'], keywords: 'postgres settings' },
      { name: 'redis', label: 'Redis', desc: 'Scan and inspect keys', icon: 'key', drivers: ['redis'], permissionsAny: ['redis.view'] },
      { name: 'laravel-queue', label: 'Laravel queue', desc: 'Pending, delayed, reserved jobs', icon: 'queue', drivers: ['redis'], permissionsAny: ['queues.view'] },
      { name: 'memcache', label: 'Memcache', desc: 'Read, write, flush values', icon: 'key', drivers: ['memcache'], permissionsAny: ['redis.view'] },
      { name: 'mongodb', label: 'MongoDB', desc: 'Collections and documents', icon: 'document', drivers: ['mongodb'], permissionsAny: ['mongodb.view'] },
      { name: 'cassandra', label: 'Cassandra', desc: 'Keyspaces and CQL', icon: 'layers', drivers: ['cassandra'], permissionsAny: ['cassandra.view'] },
      { name: 'kafka', label: 'Kafka', desc: 'Topics, partitions, consumer groups', icon: 'kafka', drivers: ['kafka'], permissionsAny: ['kafka.view'] },
      { name: 'search', label: 'Indices', desc: 'Elasticsearch and OpenSearch indices', icon: 'search', drivers: SEARCH, permissionsAny: ['schema.browse', 'connections.view'], keywords: 'elasticsearch opensearch search' },
      { name: 'search-policies', label: 'Policies', desc: 'ILM, templates, shard allocation', icon: 'policy', drivers: SEARCH, permissionsAny: ['schema.browse', 'connections.view'], keywords: 'search ilm' },
      { name: 'cloud-storage', label: 'Files', desc: 'Browse buckets', icon: 'cloud', drivers: BUCKETS, permissionsAny: ['cloudstorage.access', 'cloudstorage.manage'], keywords: 'object storage s3 bucket' },
    ],
  },
  {
    id: 'analytics',
    label: 'Analytics',
    icon: 'dashboard',
    items: [
      { name: 'analytics', label: 'Overview', desc: 'Saved queries, dashboards, reports', icon: 'home', permissionsAny: ['analytics.view'] },
      { name: 'dashboards', label: 'Dashboards', desc: 'Saved queries as chart blocks', icon: 'dashboard', permissionsAny: ['dashboards.manage'] },
      { name: 'saved-queries', label: 'Saved queries', desc: 'Reusable SQL library', icon: 'saved', permissionsAny: ['savedqueries.manage'] },
      { name: 'ai-analytics', label: 'Ask your data', desc: 'Plain-language questions to read-only SQL', icon: 'chat', permissionsAny: ['ai.use'], keywords: 'ai analytics llm' },
    ],
  },
  {
    id: 'changes',
    label: 'Changes',
    icon: 'check',
    items: [
      { name: 'approvals', label: 'Approvals', desc: 'Review controlled SQL changes', icon: 'check', permissionsAny: ['approvals.view', 'query.approve'] },
      { name: 'change-sets', label: 'Change sets', desc: 'Plan and run database changes', icon: 'changeset', permissionsAny: ['changesets.manage', 'query.approve'] },
      { name: 'data-scripts', label: 'Data scripts', desc: 'Preview programmable updates', icon: 'code', permissionsAny: ['datascripts.manage', 'query.approve'] },
      { name: 'data-script-requests', label: 'Script requests', desc: 'Pending data script runs', icon: 'inbox', permissionsAny: ['scriptrequests.view', 'query.approve'] },
      { name: 'workflows', label: 'Approval rules', desc: 'Routing for approval workflows', icon: 'workflow', permissionsAny: ['workflows.manage'], keywords: 'workflows' },
    ],
  },
  {
    id: 'automation',
    label: 'Automation',
    icon: 'clock',
    items: [
      { name: 'scheduler', label: 'Schedules', desc: 'Recurring queries and reports', icon: 'clock', permissionsAny: ['schedules.manage'], keywords: 'scheduled queries' },
      { name: 'data-pipelines', label: 'Pipelines', desc: 'Visual ETL canvas', icon: 'pipeline', permissionsAny: ['pipelines.view'], keywords: 'etl' },
      { name: 'backup', label: 'Backups', desc: 'Download, back up, restore', icon: 'backup', permissionsAny: ['backups.manage', 'query.execute', 'query.approve'], keywords: 'restore dump' },
      { name: 'watcher', label: 'Watchers', desc: 'Watch tables and queries', icon: 'eye', permissionsAny: ['watchers.manage'] },
    ],
  },
  {
    id: 'monitoring',
    label: 'Monitoring',
    icon: 'pulse',
    items: [
      { name: 'health', label: 'Health', desc: 'Connection and service health', icon: 'pulse', permissionsAny: ['health.view'] },
      { name: 'query-performance', label: 'Query performance', desc: 'Slow queries and errors', icon: 'gauge', permissionsAny: ['performance.view'] },
      { name: 'discover', label: 'Logs', desc: 'Search and tail logs', icon: 'logs', permissionsAny: ['discover.view', 'observability.view', 'connections.view'], keywords: 'discover' },
      { name: 'uptime', label: 'Uptime', desc: 'Heartbeats, latency, TLS expiry', icon: 'uptime', permissionsAny: ['uptime.view', 'observability.view', 'connections.view'] },
      { name: 'infra-metrics', label: 'Infra metrics', desc: 'Host and pod metrics', icon: 'gauge', permissionsAny: ['observability.view', 'connections.view'] },
      { name: 'audit', label: 'Audit log', desc: 'Access, actions, query events', icon: 'audit', permissionsAny: ['audit.view'] },
    ],
  },
  {
    id: 'infrastructure',
    label: 'Infrastructure',
    icon: 'server',
    items: [
      { name: 'ssh-hosts', label: 'Servers', desc: 'SSH hosts used by Docker, Files, Nginx, Cron', icon: 'server', permissionsAny: ['docker.view', 'docker.manage', 'sftp.access', 'sftp.manage'], keywords: 'ssh hosts' },
      { name: 'docker', label: 'Docker', desc: 'Containers, images, compose', icon: 'docker', permissionsAny: ['docker.view', 'docker.manage'] },
      { name: 'nginx', label: 'Nginx', desc: 'Configs, sites, access logs', icon: 'nginx', permissionsAny: ['nginx.view', 'nginx.manage', 'nginx.reload'] },
      { name: 'cron', label: 'Cron', desc: 'Native crontab on each server', icon: 'clock', permissionsAny: ['cron.view', 'cron.manage'] },
      { name: 'sftp', label: 'Files', desc: 'Browse server files over SFTP', icon: 'folder', permissionsAny: ['sftp.access', 'sftp.manage'], keywords: 'sftp upload download' },
      { name: 'kubernetes', label: 'Kubernetes', desc: 'Workloads, services, pod logs', icon: 'kube', permissionsAny: ['kube.view', 'kube.manage'], keywords: 'k8s pods' },
      { name: 'kube-clusters', label: 'Clusters', desc: 'Kubeconfig connections', icon: 'kube', permissionsAny: ['kube.view', 'kube.manage'], keywords: 'k8s kubernetes' },
    ],
  },
  {
    id: 'settings',
    label: 'Settings',
    icon: 'settings',
    items: [
      { name: 'connections', label: 'Connections', desc: 'Database and service endpoints', icon: 'plug', permissionsAny: ['connections.view'] },
      { name: 'users', label: 'Users', desc: 'Accounts and roles', icon: 'users', permissionsAny: ['users.manage'] },
      { name: 'permissions', label: 'Roles', desc: 'Application permission policy', icon: 'lock', permissionsAny: ['roles.manage'], keywords: 'permissions rbac' },
      { name: 'permissions', label: 'Access groups', desc: 'Folder-based connection access', icon: 'lock', permissionsAny: ['folders.manage'], query: { tab: 'groups' } },
      { name: 'alert-settings', label: 'Alert channels', desc: 'Telegram, Discord, Slack, webhooks', icon: 'bell', permissionsAny: ['settings.alerts'] },
      { name: 'alerts', label: 'Alert history', desc: 'Every delivery across channels', icon: 'audit', permissionsAny: ['settings.alerts'] },
      { name: 'settings', label: 'AI provider', desc: 'Your API key, base URL, model', icon: 'sliders', permissionsAny: ['ai.use', 'ai.manage'] },
      { name: 'security', label: 'Security', desc: 'Password, 2FA, sessions', icon: 'shield', keywords: '2fa mfa password' },
    ],
  },
]

/** Sections shown in the main sidebar list (Settings lives in the sidebar footer). */
export const MAIN_SECTIONS = NAV_SECTIONS.filter((s) => s.id !== 'settings')
export const SETTINGS_SECTION = NAV_SECTIONS.find((s) => s.id === 'settings')!

/** Flat groups for the command palette and Home. */
export type NavGroup = { id: string; label: string; items: NavItem[] }
export const NAV_GROUPS: NavGroup[] = NAV_SECTIONS.map((s) => ({ id: s.id, label: s.label, items: s.items }))

export function navKey(item: Pick<NavItem, 'name' | 'query'>) {
  return item.query?.tab ? `${item.name}:${item.query.tab}` : item.name
}

/** What the connection-scoped section is called for the active connection's driver. */
export function workspaceMeta(driver: string | null | undefined): { label: string; icon: string } {
  if (!driver) return { label: 'Connect a database', icon: 'plug' }
  if (SQL.includes(driver)) return { label: 'Database', icon: 'database' }
  if (SEARCH.includes(driver)) return { label: 'Search', icon: 'search' }
  if (BUCKETS.includes(driver)) return { label: 'Storage', icon: 'cloud' }
  const names: Record<string, { label: string; icon: string }> = {
    redis: { label: 'Redis', icon: 'key' },
    memcache: { label: 'Memcache', icon: 'key' },
    mongodb: { label: 'MongoDB', icon: 'document' },
    cassandra: { label: 'Cassandra', icon: 'layers' },
    kafka: { label: 'Kafka', icon: 'kafka' },
  }
  return names[driver] ?? { label: 'Connection', icon: 'plug' }
}

/** The section that owns a route (tab-aware for pages like Roles / Access groups). */
export function sectionOf(name: string | null | undefined, tab?: string | null): NavSection | undefined {
  if (!name) return undefined
  return NAV_SECTIONS.find((s) => s.items.some((i) => i.name === name && (!i.query?.tab || i.query.tab === tab)))
    ?? NAV_SECTIONS.find((s) => s.items.some((i) => i.name === name))
}

// 24×24 stroke icons. One set, one stroke weight, so nav reads as one family.
export const ICONS: Record<string, string> = {
  // The Nias product glyph, shared with anveesa.com (AppIcon › chart).
  nias: '<path d="M3 3v18h18"/><path d="M7 16v-4"/><path d="M12 16V8"/><path d="M17 16v-7"/>',
  home: '<path d="M3 10.5 12 3l9 7.5"/><path d="M5 9v11h14V9"/>',
  dashboard: '<rect x="3" y="3" width="8" height="10" rx="1.5"/><rect x="13" y="3" width="8" height="6" rx="1.5"/><rect x="3" y="15" width="8" height="6" rx="1.5"/><rect x="13" y="11" width="8" height="10" rx="1.5"/>',
  saved: '<path d="M6 3h12v18l-6-4-6 4z"/>',
  chat: '<path d="M4 5h16v11H9l-5 4z"/><path d="M8 10h8"/>',
  table: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 10h18M9 10v10"/>',
  objects: '<path d="M4 6h16M4 12h16M4 18h10"/>',
  er: '<rect x="3" y="3" width="7" height="6" rx="1"/><rect x="14" y="15" width="7" height="6" rx="1"/><path d="M6.5 9v3.5a2 2 0 0 0 2 2h6"/>',
  diff: '<path d="M8 3v12"/><path d="M5 12l3 3 3-3"/><path d="M16 21V9"/><path d="M13 12l3-3 3 3"/>',
  rowhistory: '<path d="M3 12a9 9 0 1 0 3-6.7L3 8"/><path d="M3 3v5h5"/><path d="M12 7v5l3 2"/>',
  key: '<circle cx="8" cy="15" r="4"/><path d="M11 12l9-9"/><path d="M17 6l3 3"/>',
  document: '<path d="M14 3H6v18h12V7z"/><path d="M14 3v4h4"/>',
  layers: '<path d="M12 3 2 8l10 5 10-5z"/><path d="M2 13l10 5 10-5"/>',
  search: '<circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/>',
  policy: '<path d="M12 3 5 6v6c0 4.5 3 7.5 7 9 4-1.5 7-4.5 7-9V6z"/><path d="m9 12 2 2 4-4"/>',
  kafka: '<circle cx="12" cy="12" r="2.5"/><circle cx="5" cy="5" r="2"/><circle cx="19" cy="5" r="2"/><circle cx="5" cy="19" r="2"/><circle cx="19" cy="19" r="2"/><path d="m6.5 6.5 3.7 3.7M17.5 6.5l-3.7 3.7M6.5 17.5l3.7-3.7M17.5 17.5l-3.7-3.7"/>',
  queue: '<path d="M4 6h12M4 12h12M4 18h8"/><path d="m18 15 3 3-3 3"/>',
  logs: '<path d="M4 5h16M4 10h16M4 15h10M4 20h6"/>',
  uptime: '<path d="M3 12h4l3-7 4 14 3-7h4"/>',
  gauge: '<path d="M4 18a8 8 0 1 1 16 0"/><path d="m12 18 4-6"/>',
  audit: '<path d="M14 3H6v18h12V7z"/><path d="M14 3v4h4"/><path d="M9 13h6M9 17h4"/>',
  shield: '<path d="M12 3 5 6v6c0 4.5 3 7.5 7 9 4-1.5 7-4.5 7-9V6z"/>',
  pulse: '<path d="M3 12h4l2-4 4 8 2-4h6"/>',
  eye: '<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/>',
  check: '<circle cx="12" cy="12" r="9"/><path d="m8 12 3 3 5-6"/>',
  changeset: '<path d="M6 3v12"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="6" r="3"/><path d="M18 9a9 9 0 0 1-9 9"/>',
  code: '<path d="m8 8-4 4 4 4M16 8l4 4-4 4M14 4l-4 16"/>',
  inbox: '<path d="M3 13h5l1.5 3h5L16 13h5"/><path d="M5 5h14l2 8v6H3v-6z"/>',
  backup: '<path d="M12 3v12"/><path d="m7 10 5 5 5-5"/><path d="M4 17v3h16v-3"/>',
  clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  pipeline: '<circle cx="5" cy="12" r="2"/><circle cx="19" cy="5" r="2"/><circle cx="19" cy="19" r="2"/><path d="M7 12h4m0 0c0-4 4-7 6-7m-6 7c0 4 4 7 6 7"/>',
  workflow: '<rect x="3" y="3" width="6" height="6" rx="1"/><rect x="15" y="15" width="6" height="6" rx="1"/><path d="M9 6h4a2 2 0 0 1 2 2v7"/>',
  server: '<rect x="3" y="4" width="18" height="7" rx="1.5"/><rect x="3" y="13" width="18" height="7" rx="1.5"/><path d="M7 7.5h.01M7 16.5h.01"/>',
  docker: '<rect x="3" y="10" width="3" height="3"/><rect x="7" y="10" width="3" height="3"/><rect x="11" y="10" width="3" height="3"/><rect x="7" y="6" width="3" height="3"/><rect x="11" y="6" width="3" height="3"/><path d="M3 13h13c2.5 0 4-1 4.5-3 1 .3 2 .2 2.5-.3-.3 1.8-1.7 3.3-3.5 3.3"/><path d="M3 13c0 4 3 7 8 7 5 0 8-3 9-7"/>',
  kube: '<path d="M12 2 3 7v10l9 5 9-5V7z"/><circle cx="12" cy="12" r="2.5"/>',
  nginx: '<path d="M12 2 3 7v10l9 5 9-5V7z"/><path d="M9 16V9l6 7V9"/>',
  folder: '<path d="M3 6a1 1 0 0 1 1-1h5l2 2h9a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1z"/>',
  cloud: '<path d="M17.5 19a4.5 4.5 0 0 0 0-9 6 6 0 0 0-11.4-1.5A4 4 0 0 0 6.5 19z"/>',
  replicate: '<rect x="3" y="3" width="11" height="11" rx="1.5"/><path d="M10 21h9a2 2 0 0 0 2-2v-9"/>',
  sliders: '<path d="M4 6h10M18 6h2M4 12h4M12 12h8M4 18h12M20 18h0"/><circle cx="16" cy="6" r="2"/><circle cx="10" cy="12" r="2"/><circle cx="18" cy="18" r="2"/>',
  plug: '<path d="M9 3v5M15 3v5"/><path d="M6 8h12v3a6 6 0 0 1-12 0z"/><path d="M12 17v4"/>',
  users: '<circle cx="9" cy="8" r="4"/><path d="M2 21a7 7 0 0 1 14 0"/><path d="M16 4a4 4 0 0 1 0 8M22 21a7 7 0 0 0-4-6.3"/>',
  lock: '<rect x="4" y="11" width="16" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/>',
  bell: '<path d="M18 9a6 6 0 0 0-12 0c0 6-3 8-3 8h18s-3-2-3-8"/><path d="M10 21a2 2 0 0 0 4 0"/>',
  book: '<path d="M4 4h6a2 2 0 0 1 2 2v14a2 2 0 0 0-2-2H4z"/><path d="M20 4h-6a2 2 0 0 0-2 2v14a2 2 0 0 1 2-2h6z"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
  moon: '<path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/>',
  logout: '<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><path d="m16 17 5-5-5-5M21 12H9"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  settings: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"/>',
  file: '<path d="M14 3H6v18h12V7z"/><path d="M14 3v4h4"/>',
  image: '<rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="9" cy="10" r="2"/><path d="m21 16-5-5-9 9"/>',
  archive: '<rect x="3" y="4" width="18" height="5" rx="1"/><path d="M5 9v11h14V9"/><path d="M10 13h4"/>',
  link: '<path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1"/><path d="M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1"/>',
  terminal: '<path d="m5 7 5 5-5 5"/><path d="M13 17h6"/>',
  globe: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/>',
  map: '<path d="M9 4 3 6v14l6-2 6 2 6-2V4l-6 2z"/><path d="M9 4v14M15 6v14"/>',
  camera: '<path d="M4 8h3l2-3h6l2 3h3v11H4z"/><circle cx="12" cy="13" r="3.5"/>',
  broadcast: '<circle cx="12" cy="12" r="2"/><path d="M8.5 8.5a5 5 0 0 0 0 7M15.5 8.5a5 5 0 0 1 0 7M5.6 5.6a9 9 0 0 0 0 12.8M18.4 5.6a9 9 0 0 1 0 12.8"/>',
  chart: '<path d="M4 20V10M10 20V4M16 20v-7M22 20H2"/>',
  refresh: '<path d="M20 11a8 8 0 1 0-2.3 5.7"/><path d="M20 4v7h-7"/>',
  close: '<path d="M6 6l12 12M18 6 6 18"/>',
  alert: '<path d="M12 3 2 20h20z"/><path d="M12 10v4M12 17h.01"/>',
  database: '<ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v14c0 1.7 3.6 3 8 3s8-1.3 8-3V5"/><path d="M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3"/>',
}

export function iconSvg(name: string) {
  return ICONS[name] ?? '<circle cx="12" cy="12" r="4"/>'
}
