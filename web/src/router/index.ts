import { createRouter, createWebHistory } from 'vue-router'
import axios from 'axios'
import { useAuth } from '@/composables/useAuth'

const router = createRouter({
  history: createWebHistory(),
  routes: [
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
          redirect: { name: 'data' },
        },
        {
          path: 'welcome',
          name: 'welcome',
          component: () => import('@/views/WelcomeView.vue'),
        },
        {
          path: 'dashboard',
          name: 'dashboard',
          component: () => import('@/views/DashboardView.vue'),
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
          meta: { requiredPermissionsAny: ['audit.view'] },
        },
        {
          path: 'database-audit',
          name: 'database-audit',
          component: () => import('@/views/DatabaseAuditView.vue'),
          meta: { requiredPermissionsAny: ['audit.view'] },
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
          path: 'backup',
          name: 'backup',
          component: () => import('@/views/BackupView.vue'),
          meta: { requiredPermissionsAny: ['backups.manage'] },
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
          meta: { requiredPermissionsAny: ['query.execute'] },
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
          meta: { requiredPermissionsAny: ['connections.view', 'query.execute', 'schema.browse'] },
        },
        {
          path: 'connections',
          name: 'connections',
          component: () => import('@/views/ConnectionsView.vue'),
          meta: { requiredPermissionsAny: ['connections.view'] },
        },
        {
          path: 'er',
          name: 'er',
          component: () => import('@/views/ERDiagramView.vue'),
          meta: { requiredPermissionsAny: ['schema.browse'] },
        },
        {
          path: 'saved-queries',
          name: 'saved-queries',
          component: () => import('@/views/SavedQueriesView.vue'),
          meta: { requiredPermissionsAny: ['savedqueries.manage'] },
        },
        {
          path: 'approvals',
          name: 'approvals',
          component: () => import('@/views/ApprovalRequestsView.vue'),
          meta: { requiredPermissionsAny: ['query.execute', 'query.approve'] },
        },
        {
          path: 'change-sets',
          name: 'change-sets',
          component: () => import('@/views/ChangeSetsView.vue'),
          meta: { requiredPermissionsAny: ['query.execute', 'query.approve'] },
        },
        {
          path: 'data-scripts',
          name: 'data-scripts',
          component: () => import('@/views/DataScriptsView.vue'),
          meta: { requiredPermissionsAny: ['query.execute', 'query.approve'] },
        },
        {
          path: 'data-script-requests',
          name: 'data-script-requests',
          component: () => import('@/views/DataScriptRequestsView.vue'),
          meta: { requiredPermissionsAny: ['query.execute', 'query.approve'] },
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
  const { isAuthenticated, authEnabled, hasAnyPermission } = useAuth()

  // Skip auth check if auth is not enabled
  if (!authEnabled.value) {
    return
  }

  // Guest routes (e.g., login) - redirect if already authenticated
  if (to.meta.guest && isAuthenticated.value) {
    return { name: 'welcome' }
  }

  // Check if route or any parent requires authentication
  const requiresAuth = to.matched.some(record => record.meta.requiresAuth)
  
  // If not authenticated and trying to access protected route, redirect to login
  if (!isAuthenticated.value && !to.meta.guest) {
    return { name: 'login' }
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
