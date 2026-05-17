import { createRouter, createWebHistory } from 'vue-router'
import axios from 'axios'
import { useAuth } from '@/composables/useAuth'

const LAST_ROUTE_KEY = 'nias:lastRoute'

function restoredStartRoute() {
  const saved = localStorage.getItem(LAST_ROUTE_KEY)
  if (!saved || saved === '/' || saved === '/dashboard' || saved.startsWith('/login') || saved.startsWith('/shared-dashboards') || saved.startsWith('/embed/')) {
    return { name: 'analytics' }
  }
  return saved
}

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/shared-dashboards/:token',
      name: 'shared-dashboard',
      component: () => import('@/views/AnalyticsDashboardsView.vue'),
      meta: { guest: true, publicDashboard: true },
    },
    {
      path: '/embed/dashboards/:token',
      name: 'embed-dashboard',
      component: () => import('@/views/AnalyticsDashboardsView.vue'),
      meta: { guest: true, publicDashboard: true, embed: true },
    },
    {
      path: '/embed/dashboards/:token/blocks/:blockId',
      name: 'embed-dashboard-block',
      component: () => import('@/views/AnalyticsDashboardsView.vue'),
      meta: { guest: true, publicDashboard: true, embed: true },
    },
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/LoginView.vue'),
      meta: { guest: true },
    },
    {
      path: '/',
      component: () => import('@/layouts/AppLayout.vue'),
      meta: { requiresAuth: true },
      children: [
        {
          path: '',
          redirect: restoredStartRoute,
        },
        {
          path: 'analytics',
          name: 'analytics',
          component: () => import('@/views/AnalyticsHomeView.vue'),
          meta: { requiredPermissionsAny: ['analytics.view'] },
        },
        {
          path: 'welcome',
          name: 'welcome',
          component: () => import('@/views/WelcomeView.vue'),
        },
        {
          path: 'docs',
          name: 'docs',
          component: () => import('@/views/DocsView.vue'),
        },
        {
          path: 'users',
          name: 'users',
          component: () => import('@/views/UsersView.vue'),
          meta: { requiredPermissionsAny: ['users.manage'] },
        },
        {
          path: 'audit',
          name: 'audit',
          component: () => import('@/views/AuditLogView.vue'),
          meta: { requiredPermissionsAny: ['audit.view'] },
        },
        {
          path: 'query-performance',
          name: 'query-performance',
          component: () => import('@/views/QueryPerformanceView.vue'),
          meta: { requiredPermissionsAny: ['performance.view'] },
        },
        {
          path: 'database-audit',
          name: 'database-audit',
          component: () => import('@/views/DatabaseAuditView.vue'),
          meta: { requiredPermissionsAny: ['databaseaudit.view'] },
        },
        {
          path: 'database-logs',
          name: 'database-logs',
          component: () => import('@/views/DatabaseLogsView.vue'),
          meta: { requiredPermissionsAny: ['schema.browse', 'connections.view'] },
        },
        {
          path: 'diff',
          name: 'diff',
          component: () => import('@/views/SchemaDiffView.vue'),
          meta: { requiredPermissionsAny: ['schema.diff.view'] },
        },
        {
          path: 'scheduler',
          name: 'scheduler',
          component: () => import('@/views/SchedulerView.vue'),
          meta: { requiredPermissionsAny: ['schedules.manage'] },
        },
        {
          path: 'notifications',
          name: 'notifications',
          component: () => import('@/views/NotificationsView.vue'),
          meta: { requiredPermissionsAny: ['notifications.view'] },
        },
        {
          path: 'ai-analytics',
          name: 'ai-analytics',
          component: () => import('@/views/AIAnalyticsView.vue'),
          meta: { requiredPermissionsAny: ['ai.use'] },
        },
        {
          path: 'backup',
          name: 'backup',
          component: () => import('@/views/BackupView.vue'),
          meta: { requiredPermissionsAny: ['backups.manage', 'query.execute', 'query.approve'] },
        },
        {
          path: 'health',
          name: 'health',
          component: () => import('@/views/HealthView.vue'),
          meta: { requiredPermissionsAny: ['health.view'] },
        },
        {
          path: 'watcher',
          name: 'watcher',
          component: () => import('@/views/WatcherView.vue'),
          meta: { requiredPermissionsAny: ['watchers.manage'] },
        },
        {
          path: 'query',
          redirect: { name: 'data' },
        },
        {
          path: 'schema',
          redirect: { name: 'data' },
        },
        {
          path: 'data',
          name: 'data',
          component: () => import('@/views/DataView.vue'),
          meta: { requiredPermissionsAny: ['sqlstudio.access'] },
        },
        {
          path: 'database-objects',
          name: 'database-objects',
          component: () => import('@/views/DatabaseObjectsView.vue'),
          meta: { requiredPermissionsAny: ['sqlstudio.access'] },
        },
        {
          path: 'redis',
          name: 'redis',
          component: () => import('@/views/RedisView.vue'),
          meta: { requiredPermissionsAny: ['redis.view'] },
        },
        {
          path: 'memcache',
          name: 'memcache',
          component: () => import('@/views/MemcacheView.vue'),
          meta: { requiredPermissionsAny: ['redis.view'] },
        },
        {
          path: 'mongodb',
          name: 'mongodb',
          component: () => import('@/views/MongoDBView.vue'),
          meta: { requiredPermissionsAny: ['mongodb.view'] },
        },
        {
          path: 'cassandra',
          name: 'cassandra',
          component: () => import('@/views/CassandraView.vue'),
          meta: { requiredPermissionsAny: ['cassandra.view'] },
        },
        {
          path: 'search',
          name: 'search',
          component: () => import('@/views/SearchView.vue'),
          meta: { requiredPermissionsAny: ['schema.browse', 'connections.view'] },
        },
        {
          path: 'search-policies',
          name: 'search-policies',
          component: () => import('@/views/SearchPoliciesView.vue'),
          meta: { requiredPermissionsAny: ['schema.browse', 'connections.view'] },
        },
        {
          path: 'discover',
          name: 'discover',
          component: () => import('@/views/DiscoverView.vue'),
          meta: { requiredPermissionsAny: ['schema.browse', 'connections.view'] },
        },
        {
          path: 'uptime',
          name: 'uptime',
          component: () => import('@/views/UptimeView.vue'),
          meta: { requiredPermissionsAny: ['schema.browse', 'connections.view'] },
        },
        {
          path: 'laravel-queue',
          name: 'laravel-queue',
          component: () => import('@/views/LaravelQueueView.vue'),
          meta: { requiredPermissionsAny: ['queues.view'] },
        },
        {
          path: 'kafka',
          name: 'kafka',
          component: () => import('@/views/KafkaView.vue'),
          meta: { requiredPermissionsAny: ['kafka.view'] },
        },
        {
          path: 'connections',
          name: 'connections',
          component: () => import('@/views/ConnectionsView.vue'),
          meta: { requiredPermissionsAny: ['connections.view'] },
        },
        {
          path: 'settings',
          name: 'settings',
          component: () => import('@/views/SettingsView.vue'),
          meta: { requiredPermissionsAny: ['ai.use', 'ai.manage'] },
        },
        {
          path: 'er',
          name: 'er',
          component: () => import('@/views/ERDiagramView.vue'),
          meta: { requiredPermissionsAny: ['er.view'] },
        },
        {
          path: 'saved-queries',
          name: 'saved-queries',
          component: () => import('@/views/SavedQueriesView.vue'),
          meta: { requiredPermissionsAny: ['savedqueries.manage'] },
        },
        {
          path: 'dashboards',
          name: 'dashboards',
          component: () => import('@/views/AnalyticsDashboardsView.vue'),
          meta: { requiredPermissionsAny: ['dashboards.manage'] },
        },
        {
          path: 'approvals',
          name: 'approvals',
          component: () => import('@/views/ApprovalRequestsView.vue'),
          meta: { requiredPermissionsAny: ['approvals.view', 'query.approve'] },
        },
        {
          path: 'change-sets',
          name: 'change-sets',
          component: () => import('@/views/ChangeSetsView.vue'),
          meta: { requiredPermissionsAny: ['changesets.manage', 'query.approve'] },
        },
        {
          path: 'data-scripts',
          name: 'data-scripts',
          component: () => import('@/views/DataScriptsView.vue'),
          meta: { requiredPermissionsAny: ['datascripts.manage', 'query.approve'] },
        },
        {
          path: 'data-script-requests',
          name: 'data-script-requests',
          component: () => import('@/views/DataScriptRequestsView.vue'),
          meta: { requiredPermissionsAny: ['scriptrequests.view', 'query.approve'] },
        },
        {
          path: 'permissions',
          name: 'permissions',
          component: () => import('@/views/PermissionsView.vue'),
          meta: { requiredPermissionsAny: ['roles.manage', 'folders.manage', 'users.manage'] },
        },
        {
          path: 'workflows',
          name: 'workflows',
          component: () => import('@/views/ApprovalWorkflowsView.vue'),
          meta: { requiredPermissionsAny: ['workflows.manage'] },
        },
        {
          path: 'rbac',
          redirect: { name: 'permissions' },
        },
        {
          path: 'row-history',
          name: 'row-history',
          component: () => import('@/views/RowHistoryView.vue'),
          meta: { requiredPermissionsAny: ['rowhistory.view'] },
        },
        {
          path: 'security',
          name: 'security',
          component: () => import('@/views/SecurityView.vue'),
          meta: { requiredPermissionsAny: ['security.self'] },
        },
      ],
    },
    {
      path: '/:pathMatch(.*)*',
      redirect: '/',
    },
  ],
})

router.beforeEach((to) => {
  const { isAuthenticated, authEnabled, hasAnyPermission, mustSetupMfa } = useAuth()

  // Skip auth check if auth is not enabled
  if (!authEnabled.value) {
    return
  }

  // Guest routes (e.g., login) - redirect if already authenticated
  if (to.meta.guest && !to.meta.publicDashboard && isAuthenticated.value) {
    return { name: 'welcome' }
  }

  // Check if route or any parent requires authentication
  const requiresAuth = to.matched.some(record => record.meta.requiresAuth)
  
  // If not authenticated and trying to access protected route, redirect to login
  if (!isAuthenticated.value && !to.meta.guest) {
    return { name: 'login' }
  }

  if (isAuthenticated.value && mustSetupMfa.value && to.name !== 'security' && !to.meta.guest) {
    return { name: 'security', query: { setup: 'mfa' } }
  }

  if (isAuthenticated.value && mustSetupMfa.value && to.name === 'security') {
    return
  }

  const requiredPermissions = to.meta.requiredPermissionsAny as string[] | undefined
  if (requiredPermissions?.length && authEnabled.value) {
    if (!isAuthenticated.value) {
      return { name: 'login' }
    }
    if (!hasAnyPermission(requiredPermissions)) {
      return { name: 'welcome' }
    }
  }
})

let lastAuditKey = ''

router.afterEach((to, from) => {
  if (to.name === from.name && to.fullPath === from.fullPath) {
    return
  }
  if (to.name === 'login') {
    return
  }
  if (!to.meta.guest && to.fullPath !== '/') {
    localStorage.setItem(LAST_ROUTE_KEY, to.fullPath)
  }
  const { authEnabled, isAuthenticated } = useAuth()
  if (authEnabled.value && !isAuthenticated.value) {
    return
  }
  const auditKey = `${String(to.name ?? '')}:${to.fullPath}`
  if (auditKey === lastAuditKey) {
    return
  }
  lastAuditKey = auditKey
  void axios.post('/api/audit/access', {
    action: 'open_feature',
    target: String(to.name ?? to.path),
    details: to.fullPath,
  }).catch(() => {})
})

export default router
