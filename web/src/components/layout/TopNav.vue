<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import axios from 'axios'
import { useAuth } from '@/composables/useAuth'
import { useTheme } from '@/composables/useTheme'
import { useConnections } from '@/composables/useConnections'
import ConnectionsDropdown from '@/components/layout/ConnectionsDropdown.vue'

const props = defineProps<{ activeConnId: number | null }>()
const emit = defineEmits<{
  (e: 'select-conn', id: number): void
}>()

const route = useRoute()
const router = useRouter()
const { user, authEnabled, logout, hasAnyPermission } = useAuth()
const { mode, toggleTheme } = useTheme()
const { connections } = useConnections()

// Active connection info for display
const activeConn = computed(() =>
  props.activeConnId != null ? connections.value.find(c => c.id === props.activeConnId) ?? null : null
)
const driverColor: Record<string, string> = { sqlite: '#4b5563', postgres: '#336791', mysql: '#f29111', mariadb: '#c0392b', mssql: '#cc2927', redis: '#c6302b', memcache: '#16a34a', kafka: '#231f20', mongodb: '#00a35c', cassandra: '#1f6feb', elasticsearch: '#00bfb3', opensearch: '#005eb8', s3_aws: '#f59e0b', s3_gcp: '#4285f4', s3_oss: '#ff6a00', s3_obs: '#c00000' }
const driverLabel: Record<string, string> = { sqlite: 'SL', postgres: 'PG', mysql: 'MY', mariadb: 'MB', mssql: 'MS', redis: 'RD', memcache: 'MC', kafka: 'KF', mongodb: 'MG', cassandra: 'CA', elasticsearch: 'ES', opensearch: 'OS', s3_aws: 'S3', s3_gcp: 'GCS', s3_oss: 'OSS', s3_obs: 'OBS' }

// Nav group dropdown
const openMenu = ref<string | null>(null)
const dropdownPos = ref<{ top: number; left: number } | null>(null)
const dropdownPanelEl = ref<HTMLElement | null>(null)
// Connections panel — kept separate so outside-click logic doesn't conflict
const connPanelOpen = ref(false)

const PANEL_WIDTH = 280
const DROPDOWN_GUTTER = 24
const connPanelStyle = computed(() => {
  if (!connBtnRef.value) return {}
  const rect = connBtnRef.value.getBoundingClientRect()
  const spaceRight = window.innerWidth - rect.left
  const left = spaceRight >= PANEL_WIDTH + 8 ? rect.left : Math.max(8, window.innerWidth - PANEL_WIDTH - 8)
  return {
    position: 'fixed' as const,
    top: `${rect.bottom + 6}px`,
    left: `${left}px`,
    zIndex: 9999,
  }
})
const userMenuOpen = ref(false)
const notificationUnread = ref(0)

const navRef = ref<HTMLElement | null>(null)
const userRef = ref<HTMLElement | null>(null)
const connBtnRef = ref<HTMLElement | null>(null)
const connPanelRef = ref<HTMLElement | null>(null)
const notificationPoll = ref<number | null>(null)
const canViewNotifications = computed(() => hasAnyPermission(['notifications.view']))

// ── Navigation structure ─────────────────────────────────────────
type NavLink = {
  name: string
  label: string
  icon: string
  permissionsAny?: string[]
  query?: Record<string, string>
}

type MenuItem = NavLink & {
  desc: string
  section?: string
}

type MenuGroup = {
  id: string
  label: string
  icon: string
  items: MenuItem[]
}

// Direct links (no dropdown)
const directLinks = computed(() => {
  const links: NavLink[] = []
  return links.filter((link) => !link.permissionsAny?.length || hasAnyPermission(link.permissionsAny))
})

// Grouped dropdown menus (filtered by permissions)
const allMenuGroups: MenuGroup[] = [
  {
    id: 'analytics',
    label: 'Analytics',
    icon: 'dashboard',
    items: [
      { name: 'analytics', label: 'Analytics Home', desc: 'Start analysis workflows and review available analytics surfaces', icon: 'dashboard', permissionsAny: ['analytics.view'] },
      { name: 'dashboards', label: 'Dashboards', desc: 'Compose saved queries into chart blocks and shared analytics views', icon: 'dashboard', permissionsAny: ['dashboards.manage'] },
      { name: 'saved-queries', label: 'Saved Queries', desc: 'Reusable SQL and dataset-style query library', icon: 'saved', permissionsAny: ['savedqueries.manage'] },
      { name: 'ai-analytics', label: 'AI Analytics', desc: 'Ask business questions and generate safe read-only analytics queries', icon: 'spark', permissionsAny: ['ai.use'] },
      { name: 'settings', label: 'AI Settings', desc: 'Manage your personal AI provider key, base URL, and model', icon: 'settings', permissionsAny: ['ai.use', 'ai.manage'] },
    ],
  },
  {
    id: 'observability',
    label: 'Observability',
    icon: 'observability',
    items: [
        { name: 'discover', label: 'Discover', desc: 'Explore and trace logs — filter by level, app, environment with a live log stream', icon: 'discover', permissionsAny: ['discover.view', 'observability.view', 'connections.view'] },
                { name: 'uptime', label: 'Uptime', desc: 'Heartbeat monitor status, response times, TLS expiry, and 24h timeline per endpoint', icon: 'uptime', permissionsAny: ['uptime.view', 'observability.view', 'connections.view'] },
    ],
  },
  {
    id: 'database',
    label: 'Database',
    icon: 'table',
    items: [
      { name: 'data', label: 'SQL Studio', desc: 'Browse tables, inspect schema, and run SQL in the main workbench', icon: 'table', section: 'Relational', permissionsAny: ['sqlstudio.access'] },
      { name: 'database-objects', label: 'DB Objects', desc: 'Browse indexes, views, functions, procedures, triggers, sequences, and types', icon: 'diff', section: 'Relational', permissionsAny: ['sqlstudio.access'] },
      { name: 'er', label: 'ER Diagram', desc: 'Visualize relationships between tables before building analysis', icon: 'er', section: 'Relational', permissionsAny: ['er.view'] },
      { name: 'diff', label: 'Schema Diff', desc: 'Compare schema structure across environments', icon: 'diff', section: 'Relational', permissionsAny: ['schema.diff.view'] },
      { name: 'row-history', label: 'Row History', desc: 'See row-level INSERT, UPDATE, DELETE changes', icon: 'rowhistory', section: 'Relational', permissionsAny: ['rowhistory.view'] },
      { name: 'redis', label: 'Redis', desc: 'Scan keys and inspect Redis values from managed connections', icon: 'table', section: 'Cache & Search', permissionsAny: ['redis.view'] },
      { name: 'memcache', label: 'Memcache', desc: 'Read, write, delete, flush, and inspect Memcache values', icon: 'table', section: 'Cache & Search', permissionsAny: ['redis.view'] },
      { name: 'mongodb', label: 'MongoDB', desc: 'Inspect databases, collections, stats, and sample documents', icon: 'table', section: 'Document Database', permissionsAny: ['mongodb.view'] },
      { name: 'cassandra', label: 'Cassandra', desc: 'Browse keyspaces, inspect wide-column tables, and run CQL', icon: 'layers', section: 'Wide-column Database', permissionsAny: ['cassandra.view'] },
      { name: 'search', label: 'Search Browser', desc: 'Inspect Elasticsearch and OpenSearch indices, queries, and documents', icon: 'search', section: 'Cache & Search', permissionsAny: ['schema.browse', 'connections.view'] },
      { name: 'search-policies', label: 'Search Policies', desc: 'Manage ILM policies, index templates, app-level rules, and shard allocation', icon: 'policy', section: 'Cache & Search', permissionsAny: ['schema.browse', 'connections.view'] },
      { name: 'laravel-queue', label: 'Laravel Queue', desc: 'Inspect Redis-backed Laravel queue jobs, delayed jobs, and reserved jobs', icon: 'queue', section: 'Messaging', permissionsAny: ['queues.view'] },
      { name: 'kafka', label: 'Kafka', desc: 'Inspect Kafka topics, partitions, and consumer groups', icon: 'kafka', section: 'Messaging', permissionsAny: ['kafka.view'] },
    ],
  },
  {
    id: 'operate',
    label: 'Operations',
    icon: 'activity',
    items: [
      { name: 'query-performance',label: 'Query Performance',desc: 'Slow queries, errors, and execution trends', icon: 'performance',  section: 'Monitoring', permissionsAny: ['performance.view'] },
      { name: 'database-logs',    label: 'DB Logs',         desc: 'Slow query log, error log, and SQL audit',   icon: 'audit',        section: 'Monitoring', permissionsAny: ['schema.browse', 'connections.view'] },
      { name: 'database-audit',   label: 'DB Audit',        desc: 'Live sessions and external access signals', icon: 'shieldlog',    section: 'Monitoring', permissionsAny: ['databaseaudit.view'] },
      { name: 'audit',            label: 'Audit Log',       desc: 'Track access, actions, and query events', icon: 'audit',         section: 'Monitoring', permissionsAny: ['audit.view'] },
      { name: 'health',           label: 'Health',          desc: 'Connection and service health status', icon: 'health',           section: 'Monitoring', permissionsAny: ['health.view'] },
      { name: 'watcher',          label: 'Watchers',        desc: 'Monitor important table or query activity', icon: 'watcher',     section: 'Monitoring', permissionsAny: ['watchers.manage'] },
      { name: 'approvals',        label: 'Approvals',       desc: 'Review and approve controlled SQL changes', icon: 'workflow',    section: 'Governance', permissionsAny: ['approvals.view', 'query.approve'] },
      { name: 'change-sets',      label: 'Change Sets',     desc: 'Plan, validate, and run database changes', icon: 'changeset',   section: 'Governance', permissionsAny: ['changesets.manage', 'query.approve'] },
      { name: 'data-scripts',     label: 'Data Scripts',    desc: 'Preview programmable data updates before approval', icon: 'changeset', section: 'Governance', permissionsAny: ['datascripts.manage', 'query.approve'] },
      { name: 'backup',           label: 'Backup',          desc: 'Request database downloads or use direct backup and restore', icon: 'backup', section: 'Governance', permissionsAny: ['backups.manage', 'query.execute', 'query.approve'] },
      { name: 'scheduler',        label: 'Scheduler',       desc: 'Schedule recurring queries and jobs', icon: 'scheduler',        section: 'Governance', permissionsAny: ['schedules.manage'] },
      { name: 'data-pipelines',   label: 'Data Pipelines',  desc: 'Build and run visual ETL pipelines — source, transform, sink nodes on a drag-and-drop canvas', icon: 'pipeline', section: 'Governance', permissionsAny: ['pipelines.view'] },
      { name: 'workflows',        label: 'Workflows',       desc: 'Configure approval workflows and routing', icon: 'workflow',    section: 'Governance', permissionsAny: ['workflows.manage'] },
    ],
  },
  {
    id: 'admin',
    label: 'Admin',
    icon: 'settings',
    items: [
      { name: 'connections', label: 'Connections', desc: 'Manage environments and database access points', icon: 'plug', permissionsAny: ['connections.view'] },
      { name: 'permissions', label: 'Roles & Permissions', desc: 'Define roles and application permission policy', icon: 'rbac', permissionsAny: ['roles.manage'] },
      { name: 'permissions', label: 'Access Groups', desc: 'Manage folder-based connection access groups', icon: 'rbac', permissionsAny: ['folders.manage'], query: { tab: 'groups' } },
    ],
  },
]

// Filter menu groups and items based on user permissions
const menuGroups = computed(() => {
  return allMenuGroups.map(group => {
    // Filter items within the group
    const filteredItems = group.items.filter((item: any) => {
      if (item.permissionsAny?.length && !hasAnyPermission(item.permissionsAny)) {
        return false
      }
      return true
    })

    // If all items are filtered out, hide the group
    if (filteredItems.length === 0) {
      return null
    }
    
    return {
      ...group,
      items: filteredItems
    }
  }).filter((g): g is NonNullable<typeof g> => g !== null)
})

// Is any item in a group active?
function groupActive(group: { items: Array<{ name: string }> }) {
  return group.items.some(i => i.name === route.name)
}

function groupedDropdownSections(items: MenuItem[]) {
  const sections: Array<{ label: string; items: MenuItem[] }> = []
  for (const item of items) {
    const label = item.section || ''
    let section = sections.find(s => s.label === label)
    if (!section) {
      section = { label, items: [] }
      sections.push(section)
    }
    section.items.push(item)
  }
  return sections
}

function itemActive(item: MenuItem) {
  if (route.name !== item.name) return false
  if (item.query?.tab) return route.query.tab === item.query.tab
  return !route.query.tab
}

function iconProps(icon: string) {
  return { width: '14', height: '14', viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '2', 'stroke-linecap': 'round', 'stroke-linejoin': 'round' }
}

const ICON_PATHS: Record<string, string> = {
  dashboard: '<rect x="2" y="2" width="9" height="11" rx="1"/><rect x="13" y="2" width="9" height="7" rx="1"/><rect x="2" y="15" width="9" height="7" rx="1"/><rect x="13" y="11" width="9" height="11" rx="1"/>',
  table: '<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/>',
  er: '<rect x="2" y="3" width="6" height="6" rx="1"/><rect x="16" y="3" width="6" height="6" rx="1"/><rect x="9" y="15" width="6" height="6" rx="1"/><line x1="8" y1="6" x2="16" y2="6"/><line x1="12" y1="9" x2="12" y2="15"/>',
  saved: '<path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/><polyline points="7 3 7 8 15 8"/>',
  spark: '<path d="M12 3l1.9 4.6L18.5 9 13.9 10.4 12 15l-1.9-4.6L5.5 9l4.6-1.4L12 3z"/><path d="M19 14l.95 2.05L22 17l-2.05.95L19 20l-.95-2.05L16 17l2.05-.95L19 14z"/><path d="M5 14l.95 2.05L8 17l-2.05.95L5 20l-.95-2.05L2 17l2.05-.95L5 14z"/>',
  workflow: '<circle cx="5" cy="6" r="2"/><circle cx="19" cy="6" r="2"/><circle cx="12" cy="18" r="2"/><path d="M7 6h10"/><path d="M6.5 7.5l4 8"/><path d="M17.5 7.5l-4 8"/>',
  changeset: '<path d="M14 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V8z"/><polyline points="14 3 14 8 19 8"/><path d="M8 13h8"/><path d="M8 17h6"/>',
  plug: '<path d="M18.36 6.64a9 9 0 1 1-12.73 0"/><line x1="12" y1="2" x2="12" y2="12"/>',
  settings: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>',
  users: '<path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/>',
  rbac: '<rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>',
  diff: '<line x1="18" y1="20" x2="18" y2="10"/><line x1="12" y1="20" x2="12" y2="4"/><line x1="6" y1="20" x2="6" y2="14"/>',
  backup: '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/>',
  scheduler: '<circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>',
  pipeline: '<circle cx="5" cy="12" r="2"/><circle cx="19" cy="6" r="2"/><circle cx="19" cy="18" r="2"/><path d="M7 12h10"/><path d="M17 6H9a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h8"/>',
  watcher: '<path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/>',
  audit: '<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/>',
  performance: '<path d="M3 12a9 9 0 1 1 18 0"/><path d="M12 12l4-4"/><path d="M12 12l-2 5"/><path d="M7 17h10"/>',
  shieldlog: '<path d="M12 3l7 3v6c0 5-3.5 8-7 9-3.5-1-7-4-7-9V6l7-3z"/><path d="M9 12h6"/><path d="M12 9v6"/>',
  health: '<polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/>',
  rowhistory: '<path d="M3 3h18v4H3z"/><path d="M3 10h18v4H3z"/><path d="M3 17h18v4H3z"/>',
  queue: '<path d="M4 6h11"/><path d="M4 12h11"/><path d="M4 18h11"/><path d="M18 7l3 3-3 3"/><path d="M15 10h6"/>',
  kafka: '<circle cx="12" cy="12" r="2.5"/><circle cx="5" cy="6" r="2"/><circle cx="19" cy="6" r="2"/><circle cx="5" cy="18" r="2"/><circle cx="19" cy="18" r="2"/><path d="M7 7.7l3.1 2.7"/><path d="M17 7.7l-3.1 2.7"/><path d="M7 16.3l3.1-2.7"/><path d="M17 16.3l-3.1-2.7"/>',
  search: '<circle cx="10" cy="10" r="6"/><path d="M14.5 14.5L21 21"/><path d="M7 10h6"/>',
  policy: '<path d="M12 2l7 4v6c0 5-3.5 8-7 9-3.5-1-7-4-7-9V6l7-4z"/><path d="M9 12l2 2 4-4"/>',
  observability: '<path d="M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7-10-7-10-7z"/><circle cx="12" cy="12" r="3"/><path d="M12 5v-2"/><path d="M12 21v-2"/><path d="M5 12H3"/><path d="M21 12h-2"/>',
  discover: '<path d="M3 3h7v7H3z"/><path d="M14 3h7v7h-7z"/><path d="M3 14h7v7H3z"/><circle cx="17.5" cy="17.5" r="3.5"/><path d="M20 20l2 2"/>',
  'watcher-es': '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 3"/><path d="M7 17l-2 2"/><path d="M17 17l2 2"/>',
  'service-health': '<path d="M22 12h-4l-3 9L9 3l-3 9H2"/>',
  uptime: '<circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/><path d="M12 22v-2"/><path d="M12 4V2"/><path d="M4.93 4.93l1.41 1.41"/><path d="M17.66 17.66l1.41 1.41"/>',
  layers: '<polygon points="12 2 2 7 12 12 22 7 12 2"/><polyline points="2 17 12 22 22 17"/><polyline points="2 12 12 17 22 12"/>',
}

function iconPath(icon: string): string {
  return ICON_PATHS[icon] ?? '<circle cx="12" cy="12" r="4"/>'
}

function toggleMenu(id: string, event: MouseEvent) {
  if (openMenu.value === id) {
    openMenu.value = null
    dropdownPos.value = null
    connPanelOpen.value = false
    return
  }
  connPanelOpen.value = false
  const trigger = event.currentTarget as HTMLElement
  const rect = trigger.getBoundingClientRect()
  // Anchor below the trigger — left will be clamped after render
  dropdownPos.value = { top: rect.bottom + 6, left: rect.left }
  openMenu.value = id
}

const dropdownStyle = computed(() => {
  if (!dropdownPos.value) return {}
  return {
    position: 'fixed' as const,
    top: `${dropdownPos.value.top}px`,
    left: `${dropdownPos.value.left}px`,
  }
})

function clampDropdownLeft(left: number, width: number) {
  const maxLeft = Math.max(DROPDOWN_GUTTER, window.innerWidth - width - DROPDOWN_GUTTER)
  return Math.min(Math.max(DROPDOWN_GUTTER, left), maxLeft)
}

// After the dropdown renders, place wide menus inward and keep all menus inside the viewport.
watch(openMenu, async () => {
  if (!openMenu.value) return
  await nextTick()
  const el = dropdownPanelEl.value
  if (!el || !dropdownPos.value) return
  const rect = el.getBoundingClientRect()

  if (openMenu.value === 'database') {
    dropdownPos.value = {
      top: dropdownPos.value.top,
      left: clampDropdownLeft((window.innerWidth - rect.width) / 2, rect.width),
    }
    return
  }

  dropdownPos.value = {
    top: dropdownPos.value.top,
    left: clampDropdownLeft(dropdownPos.value.left, rect.width),
  }
})

function navigate(item: MenuItem) {
  openMenu.value = null
  router.push({ name: item.name, query: item.query })
}

async function handleLogout() {
  userMenuOpen.value = false
  await logout()
  notificationUnread.value = 0
  router.push({ name: 'login' })
}

async function loadNotificationUnread() {
  if (!authEnabled.value || !canViewNotifications.value) {
    notificationUnread.value = 0
    return
  }
  try {
    const { data } = await axios.get<{ count: number }>('/api/notifications/unread')
    notificationUnread.value = Number(data?.count || 0)
  } catch {
    notificationUnread.value = 0
  }
}

function startNotificationPolling() {
  stopNotificationPolling()
  if (!authEnabled.value || !canViewNotifications.value) return
  void loadNotificationUnread()
  notificationPoll.value = window.setInterval(() => {
    void loadNotificationUnread()
  }, 30000)
}

function stopNotificationPolling() {
  if (notificationPoll.value !== null) {
    window.clearInterval(notificationPoll.value)
    notificationPoll.value = null
  }
}

function handleOutside(e: MouseEvent) {
  const t = e.target as Node
  if (navRef.value && !navRef.value.contains(t)) openMenu.value = null
  // Only close the connections panel if the click is outside BOTH the trigger
  // button AND the teleported panel (which lives in <body>)
  if (
    connBtnRef.value && !connBtnRef.value.contains(t) &&
    connPanelRef.value && !connPanelRef.value.contains(t)
  ) {
    connPanelOpen.value = false
  }
  if (userRef.value && !userRef.value.contains(t)) userMenuOpen.value = false
}

onMounted(() => {
  document.addEventListener('mousedown', handleOutside)
  startNotificationPolling()
})
onBeforeUnmount(() => {
  document.removeEventListener('mousedown', handleOutside)
  stopNotificationPolling()
})

watch([() => authEnabled.value, canViewNotifications, () => user.value?.id], () => {
  startNotificationPolling()
}, { immediate: true })
</script>

<template>
  <header class="topnav">

    <!-- Brand -->
    <div class="topnav__brand">
      <div class="topnav__logo">
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
          <ellipse cx="12" cy="5" rx="9" ry="3"/>
          <path d="M3 5V19A9 3 0 0 0 21 19V5"/>
          <path d="M3 12A9 3 0 0 0 21 12"/>
        </svg>
      </div>
      <div class="topnav__brand-text">
        <span class="topnav__brand-name">Anveesa <strong>Nias</strong></span>
        <span class="topnav__brand-version">v0.1.0</span>
      </div>
    </div>

    <div class="topnav__divider"></div>

    <!-- Connections picker -->
    <div class="topnav__conn-wrap" ref="connBtnRef">
      <button
        class="topnav__conn-btn"
          :class="{ 'topnav__conn-btn--open': connPanelOpen, 'topnav__conn-btn--active': activeConn }"
          @click="connPanelOpen = !connPanelOpen; openMenu = null"
        type="button"
        title="Switch connection"
      >
        <span v-if="activeConn" class="topnav__conn-badge" :style="{ background: driverColor[activeConn.driver] }">
          {{ driverLabel[activeConn.driver] ?? '??' }}
        </span>
        <svg v-else width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="color:var(--text-muted)">
          <path d="M18.36 6.64a9 9 0 1 1-12.73 0"/><line x1="12" y1="2" x2="12" y2="12"/>
        </svg>
        <span class="topnav__conn-name">{{ activeConn ? activeConn.name : 'No connection' }}</span>
        <span v-if="activeConn?.host" class="topnav__conn-host">{{ activeConn.host }}</span>
        <svg class="topnav__chevron" :class="{ 'topnav__chevron--up': connPanelOpen }" width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
          <polyline points="6 9 12 15 18 9"/>
        </svg>
      </button>

      <!-- Connections dropdown panel -->
      <Teleport to="body">
        <div v-if="connPanelOpen" ref="connPanelRef" class="topnav__conn-panel" :style="connPanelStyle">
          <ConnectionsDropdown
            :activeConnId="activeConnId"
            @select-conn="(id) => { emit('select-conn', id) }"
            @close="connPanelOpen = false"
          />
        </div>
      </Teleport>
    </div>

    <div class="topnav__divider"></div>

    <!-- Navigation -->
    <nav class="topnav__nav" ref="navRef">

      <!-- Direct links -->
      <router-link
        v-for="link in directLinks"
        :key="link.name"
        :to="{ name: link.name }"
        class="topnav__link"
        :class="{ 'topnav__link--active': route.name === link.name }"
      >
        <!-- icon -->
        <svg v-if="link.icon === 'grid'" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/></svg>
        <svg v-else-if="link.icon === 'dashboard'" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="2" width="9" height="11" rx="1"/><rect x="13" y="2" width="9" height="7" rx="1"/><rect x="2" y="15" width="9" height="7" rx="1"/><rect x="13" y="11" width="9" height="11" rx="1"/></svg>
        <svg v-else-if="link.icon === 'book'" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/></svg>
        <svg v-else-if="link.icon === 'terminal'" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>
        {{ link.label }}
      </router-link>

      <div class="topnav__divider"></div>

      <!-- Dropdown groups -->
      <div
        v-for="group in menuGroups"
        :key="group.id"
        class="topnav__menu"
      >
        <button
          class="topnav__menu-trigger"
          :class="{
            'topnav__menu-trigger--active': groupActive(group),
            'topnav__menu-trigger--open': openMenu === group.id,
          }"
          @click="toggleMenu(group.id, $event)"
          type="button"
        >
          <!-- Group icons -->
          <svg v-if="group.icon === 'table'" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
          <svg v-else-if="group.icon === 'dashboard'" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="2" width="9" height="11" rx="1"/><rect x="13" y="2" width="9" height="7" rx="1"/><rect x="2" y="15" width="9" height="7" rx="1"/><rect x="13" y="11" width="9" height="11" rx="1"/></svg>
          <svg v-else-if="group.icon === 'observability'" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7-10-7-10-7z"/><circle cx="12" cy="12" r="3"/></svg>
          <svg v-else-if="group.icon === 'settings'" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
          <svg v-else-if="group.icon === 'wrench'" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/></svg>
          <svg v-else-if="group.icon === 'activity'" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>

          <span>{{ group.label }}</span>

          <!-- Active dot indicator -->
          <span v-if="groupActive(group)" class="topnav__active-dot"></span>

          <svg class="topnav__chevron" :class="{ 'topnav__chevron--up': openMenu === group.id }" width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="6 9 12 15 18 9"/>
          </svg>
        </button>

        <!-- Dropdown panel -->
        <div
          v-if="openMenu === group.id"
          :ref="el => { dropdownPanelEl = el as HTMLElement | null }"
          class="topnav__dropdown"
          :class="{ 'topnav__dropdown--cols': groupedDropdownSections(group.items).length > 1 }"
          :style="dropdownStyle"
        >
          <!-- Single-section: flat list with header -->
          <template v-if="groupedDropdownSections(group.items).length === 1">
            <div class="topnav__dropdown-label">{{ group.label }}</div>
            <button
              v-for="item in groupedDropdownSections(group.items)[0].items"
              :key="`${item.name}:${item.label}`"
              class="topnav__dropdown-item"
              :class="{ 'topnav__dropdown-item--active': itemActive(item) }"
              @click="navigate(item)"
              type="button"
            >
              <div class="topnav__dropdown-icon">
                <component :is="'svg'" v-bind="iconProps(item.icon)" v-html="iconPath(item.icon)" />
              </div>
              <div class="topnav__dropdown-info">
                <span class="topnav__dropdown-name">{{ item.label }}</span>
                <span class="topnav__dropdown-desc">{{ item.desc }}</span>
              </div>
              <svg v-if="itemActive(item)" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" style="color:var(--brand);flex-shrink:0"><polyline points="20 6 9 17 4 12"/></svg>
            </button>
          </template>

          <!-- Multi-section: columns layout -->
          <template v-else>
            <div class="topnav__dropdown-cols-wrap">
              <div
                v-for="section in groupedDropdownSections(group.items)"
                :key="section.label || 'default'"
                class="topnav__dropdown-col"
              >
                <div class="topnav__dropdown-section">{{ section.label || group.label }}</div>
                <button
                  v-for="item in section.items"
                  :key="`${item.name}:${item.label}`"
                  class="topnav__dropdown-item"
                  :class="{ 'topnav__dropdown-item--active': itemActive(item) }"
                  @click="navigate(item)"
                  type="button"
                >
                  <div class="topnav__dropdown-icon">
                    <component :is="'svg'" v-bind="iconProps(item.icon)" v-html="iconPath(item.icon)" />
                  </div>
                  <div class="topnav__dropdown-info">
                    <span class="topnav__dropdown-name">{{ item.label }}</span>
                    <span class="topnav__dropdown-desc">{{ item.desc }}</span>
                  </div>
                  <svg v-if="itemActive(item)" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" style="color:var(--brand);flex-shrink:0"><polyline points="20 6 9 17 4 12"/></svg>
                </button>
              </div>
            </div>
          </template>
        </div>
      </div>
    </nav>

    <!-- Right actions -->
    <div class="topnav__actions">
      <!-- Theme -->
      <button class="topnav__action-btn" :title="`Switch to ${mode === 'dark' ? 'light' : 'dark'} mode`" @click="toggleTheme">
        <svg v-if="mode === 'dark'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>
        <svg v-else width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>
      </button>

      <button
        v-if="canViewNotifications"
        class="topnav__action-btn topnav__action-btn--icon"
        :class="{ 'topnav__action-btn--active': route.name === 'notifications' }"
        title="Notifications"
        @click="router.push({ name: 'notifications' })"
      >
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9"/>
          <path d="M13.73 21a2 2 0 0 1-3.46 0"/>
        </svg>
        <span v-if="notificationUnread > 0" class="topnav__notif-badge">{{ notificationUnread > 99 ? '99+' : notificationUnread }}</span>
      </button>

      <!-- User -->
      <div class="topnav__user-wrap" ref="userRef" v-if="authEnabled">
        <button class="topnav__user-btn" @click="userMenuOpen = !userMenuOpen">
          <div class="topnav__avatar">{{ user?.username?.[0]?.toUpperCase() ?? 'U' }}</div>
          <span class="topnav__username">{{ user?.username ?? 'User' }}</span>
          <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" :style="{ transform: userMenuOpen ? 'rotate(180deg)' : '', transition: 'transform 0.15s' }">
            <polyline points="6 9 12 15 18 9"/>
          </svg>
        </button>

        <div v-if="userMenuOpen" class="topnav__user-menu">
          <div class="topnav__user-info">
            <div class="topnav__avatar topnav__avatar--lg">{{ user?.username?.[0]?.toUpperCase() ?? 'U' }}</div>
            <div>
              <div style="font-size:12.5px;font-weight:600;color:var(--text-primary)">{{ user?.username }}</div>
              <div class="topnav__role-badge">{{ user?.role }}</div>
            </div>
          </div>
          <div class="topnav__menu-sep"></div>
          <button class="topnav__user-menu-item topnav__user-menu-item--nav" @click="userMenuOpen = false; router.push({ name: 'docs' })">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/>
            </svg>
            Docs
          </button>
          <button class="topnav__user-menu-item topnav__user-menu-item--nav" @click="userMenuOpen = false; router.push({ name: 'security' })">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <rect x="3" y="11" width="18" height="11" rx="2" ry="2"/>
              <path d="M7 11V7a5 5 0 0 1 10 0v4"/>
            </svg>
            Security & 2FA
          </button>
          <button class="topnav__user-menu-item topnav__user-menu-item--logout" @click="handleLogout">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/></svg>
            Sign out
          </button>
        </div>
      </div>
    </div>

  </header>
</template>

<style scoped>
/* ── Shell ── */
.topnav {
  display: flex;
  align-items: center;
  height: 38px;
  background: var(--bg-surface);
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
  padding: 0 8px;
  gap: 3px;
  z-index: 200;
  position: relative;
}

/* ── Brand ── */
.topnav__brand {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}
.topnav__logo {
  width: 26px;
  height: 26px;
  background: var(--brand-dim);
  border: 1px solid var(--brand-ring);
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--brand);
  flex-shrink: 0;
}
.topnav__brand-text {
  display: flex;
  flex-direction: column;
  line-height: 1.2;
}
.topnav__brand-name {
  font-size: 11.5px;
  color: var(--text-secondary);
  font-weight: 400;
}
.topnav__brand-name strong {
  color: var(--brand);
  font-weight: 700;
}
.topnav__brand-version {
  font-size: 8.5px;
  color: var(--text-muted);
  letter-spacing: 0.3px;
}

.topnav__divider {
  width: 1px;
  height: 16px;
  background: var(--border);
  flex-shrink: 0;
  margin: 0 2px;
}

/* ── Connections button ── */
.topnav__conn-wrap {
  position: relative;
  flex-shrink: 0;
}
.topnav__conn-btn {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 3px 7px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-elevated);
  cursor: pointer;
  font-size: 11.5px;
  color: var(--text-muted);
  white-space: nowrap;
  max-width: 180px;
  transition: border-color 0.12s, background 0.12s, color 0.12s;
}
.topnav__conn-btn:hover,
.topnav__conn-btn--open {
  border-color: var(--brand);
  background: var(--bg-surface);
  color: var(--text-primary);
}
.topnav__conn-btn--active { color: var(--text-primary); }

.topnav__conn-badge {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 15px;
  border-radius: 3px;
  font-size: 8.5px;
  font-weight: 700;
  color: #fff;
  letter-spacing: 0.3px;
}
.topnav__conn-name {
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 105px;
}
.topnav__conn-host {
  font-size: 10px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 58px;
}
.topnav__conn-panel {
  max-width: calc(100vw - 16px);
  width: min(280px, calc(100vw - 16px));
}

/* ── Nav ── */
.topnav__nav {
  display: flex;
  align-items: center;
  gap: 1px;
  flex: 1;
  min-width: 0;
}

/* Direct link */
.topnav__link {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 3px 7px;
  border-radius: 5px;
  font-size: 11.5px;
  font-weight: 500;
  color: var(--text-muted);
  text-decoration: none;
  white-space: nowrap;
  transition: color 0.12s, background 0.12s;
  flex-shrink: 0;
}
.topnav__link:hover { color: var(--text-primary); background: var(--bg-hover); }
.topnav__link--active { color: var(--brand); background: var(--brand-dim); }

/* Dropdown trigger */
.topnav__menu { position: relative; }

.topnav__menu-trigger {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 3px 7px;
  border: none;
  border-radius: 5px;
  background: transparent;
  font-size: 11.5px;
  font-weight: 500;
  color: var(--text-muted);
  cursor: pointer;
  white-space: nowrap;
  transition: color 0.12s, background 0.12s;
  position: relative;
}
.topnav__menu-trigger:hover { color: var(--text-primary); background: var(--bg-hover); }
.topnav__menu-trigger--active { color: var(--text-primary); }
.topnav__menu-trigger--open { color: var(--text-primary); background: var(--bg-hover); }

.topnav__active-dot {
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: var(--brand);
  flex-shrink: 0;
}

.topnav__chevron {
  color: var(--text-muted);
  transition: transform 0.15s;
  flex-shrink: 0;
}
.topnav__chevron--up { transform: rotate(180deg); }

/* Dropdown panel */
.topnav__dropdown {
  position: fixed;
  min-width: 230px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: var(--shadow-lg);
  z-index: 9999;
  overflow: hidden;
  padding: 4px;
}

/* Multi-column mega-menu variant */
.topnav__dropdown--cols {
  min-width: 0;
  padding: 0;
}

.topnav__dropdown-cols-wrap {
  display: flex;
  align-items: flex-start;
  gap: 0;
}

.topnav__dropdown-col {
  flex: 1;
  min-width: 165px;
  max-width: 205px;
  padding: 4px;
}

.topnav__dropdown-col + .topnav__dropdown-col {
  border-left: 1px solid var(--border);
}

.topnav__dropdown-label {
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.7px;
  color: var(--text-muted);
  padding: 5px 8px 3px;
}

.topnav__dropdown-section {
  padding: 6px 8px 3px;
  color: var(--text-muted);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0;
  text-transform: uppercase;
}

.topnav__dropdown-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 6px 8px;
  border: none;
  border-radius: 6px;
  background: transparent;
  cursor: pointer;
  text-align: left;
  transition: background 0.1s;
}
.topnav__dropdown-item:hover { background: var(--bg-surface); }
.topnav__dropdown-item--active { background: var(--brand-dim); }

.topnav__dropdown-icon {
  width: 24px;
  height: 24px;
  border-radius: 6px;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-secondary);
  flex-shrink: 0;
  transition: background 0.1s, color 0.1s;
}
.topnav__dropdown-item--active .topnav__dropdown-icon,
.topnav__dropdown-item:hover .topnav__dropdown-icon {
  background: var(--brand-dim);
  color: var(--brand);
  border-color: var(--brand-ring);
}

.topnav__dropdown-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1;
  min-width: 0;
}
.topnav__dropdown-name {
  font-size: 11.5px;
  font-weight: 600;
  color: var(--text-primary);
}
.topnav__dropdown-desc {
  font-size: 10px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ── Right actions ── */
.topnav__actions {
  display: flex;
  align-items: center;
  gap: 2px;
  margin-left: 3px;
  flex-shrink: 0;
}

.topnav__action-btn {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 7px;
  border: none;
  background: transparent;
  color: var(--text-muted);
  border-radius: 6px;
  cursor: pointer;
  transition: color 0.12s, background 0.12s;
}
.topnav__action-btn:hover { color: var(--text-primary); background: var(--bg-hover); }
.topnav__action-btn--icon {
  position: relative;
  justify-content: center;
  width: 28px;
  height: 28px;
  padding: 0;
}
.topnav__action-btn--active {
  color: var(--brand);
  background: var(--brand-dim);
}

.topnav__notif-badge {
  position: absolute;
  top: -4px;
  right: -3px;
  min-width: 15px;
  height: 15px;
  padding: 0 4px;
  border-radius: 999px;
  background: var(--danger);
  color: #fff;
  font-size: 8.5px;
  font-weight: 700;
  line-height: 15px;
  text-align: center;
  border: 2px solid var(--bg-surface);
}

.topnav__kbd {
  font-size: 10px;
  color: var(--text-muted);
  border: 1px solid var(--border);
  border-radius: 3px;
  padding: 1px 4px;
  font-family: inherit;
}

/* ── User menu ── */
.topnav__user-wrap { position: relative; }

.topnav__user-btn {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 3px 7px 3px 3px;
  border: 1px solid var(--border);
  background: var(--bg-elevated);
  border-radius: 7px;
  cursor: pointer;
  transition: border-color 0.12s, background 0.12s;
}
.topnav__user-btn:hover { border-color: var(--brand); }

.topnav__avatar {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  background: var(--brand);
  color: var(--brand-fg, #fff);
  font-size: 9.5px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.topnav__avatar--lg { width: 30px; height: 30px; font-size: 12px; }

.topnav__username {
  font-size: 11.5px;
  font-weight: 500;
  color: var(--text-secondary);
}

.topnav__user-menu {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  min-width: 176px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: var(--shadow-lg);
  z-index: 9999;
  overflow: hidden;
}

.topnav__user-info {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
}

.topnav__role-badge {
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--brand);
  margin-top: 2px;
}

.topnav__menu-sep {
  height: 1px;
  background: var(--border);
}

.topnav__user-menu-item {
  display: flex;
  align-items: center;
  gap: 7px;
  width: 100%;
  padding: 8px 12px;
  background: transparent;
  border: none;
  font-size: 11.5px;
  color: var(--text-secondary);
  cursor: pointer;
  transition: background 0.1s, color 0.1s;
  text-align: left;
}
.topnav__user-menu-item--nav:hover { 
  background: var(--bg-surface); 
  color: var(--text-primary); 
}
.topnav__user-menu-item--logout:hover { 
  background: var(--bg-surface); 
  color: var(--danger); 
}

@media (max-width: 960px) {
  .topnav {
    gap: 4px;
    padding-inline: 6px;
  }

  .topnav__brand-text {
    display: none;
  }

  .topnav__divider {
    margin-inline: 2px;
  }

  .topnav__conn-btn {
    max-width: 190px;
  }

  .topnav__nav {
    overflow-x: auto;
    scrollbar-width: thin;
    -webkit-overflow-scrolling: touch;
  }

  .topnav__nav::-webkit-scrollbar {
    height: 3px;
  }
}

@media (max-width: 760px) {
  .topnav {
    height: auto;
    min-height: var(--topbar-h);
    flex-wrap: wrap;
    align-items: center;
    padding: 5px 7px;
  }

  .topnav__logo {
    width: 26px;
    height: 26px;
  }

  .topnav__divider {
    display: none;
  }

  .topnav__conn-wrap {
    flex: 1 1 auto;
    min-width: 0;
  }

  .topnav__conn-btn {
    width: 100%;
    max-width: none;
    min-width: 0;
    height: 30px;
  }

  .topnav__conn-name {
    max-width: none;
    min-width: 0;
  }

  .topnav__conn-host {
    display: none;
  }

  .topnav__actions {
    margin-left: auto;
  }

  .topnav__kbd,
  .topnav__username {
    display: none;
  }

  .topnav__user-btn {
    width: 32px;
    height: 30px;
    justify-content: center;
    padding: 0;
  }

  .topnav__nav {
    order: 10;
    flex: 1 0 100%;
    width: 100%;
    padding-top: 3px;
    gap: 3px;
  }

  .topnav__link,
  .topnav__menu-trigger {
    height: 30px;
    padding: 0 8px;
    border-radius: 7px;
  }

  .topnav__dropdown {
    max-width: calc(100vw - 16px);
    max-height: min(68vh, 480px);
    overflow-y: auto;
    border-radius: 10px;
    -webkit-overflow-scrolling: touch;
  }

  .topnav__dropdown-cols-wrap {
    flex-direction: column;
    gap: 0;
  }

  .topnav__dropdown-col + .topnav__dropdown-col {
    border-left: none;
    border-top: 1px solid var(--border);
  }

  .topnav__dropdown-item {
    align-items: flex-start;
    padding: 8px;
  }

  .topnav__dropdown-desc {
    display: -webkit-box;
    overflow: hidden;
    white-space: normal;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
  }

  .topnav__user-menu {
    position: fixed;
    top: 44px;
    right: 8px;
    width: min(240px, calc(100vw - 16px));
    min-width: 0;
    max-height: calc(100dvh - 60px);
    overflow-y: auto;
  }
}

@media (max-width: 460px) {
  .topnav {
    padding-inline: 6px;
  }

  .topnav__conn-badge {
    display: none;
  }

  .topnav__conn-btn {
    padding-inline: 7px;
  }

  .topnav__actions {
    gap: 2px;
  }

  .topnav__action-btn--icon {
    width: 28px;
    height: 28px;
  }

  .topnav__link,
  .topnav__menu-trigger {
    font-size: 11.5px;
    padding-inline: 7px;
  }

  .topnav__dropdown {
    max-width: calc(100vw - 12px);
  }
}
</style>
