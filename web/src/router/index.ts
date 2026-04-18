import { createRouter, createWebHistory } from 'vue-router'
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
          path: 'users',
          name: 'users',
          component: () => import('@/views/UsersView.vue'),
          meta: { requiresAdmin: true },
        },
        {
          path: 'audit',
          name: 'audit',
          component: () => import('@/views/AuditLogView.vue'),
          meta: { requiresAdmin: false },
        },
        {
          path: 'diff',
          name: 'diff',
          component: () => import('@/views/SchemaDiffView.vue'),
          meta: { requiresAdmin: true },
        },
        {
          path: 'scheduler',
          name: 'scheduler',
          component: () => import('@/views/SchedulerView.vue'),
          meta: { requiresAdmin: true },
        },
        {
          path: 'backup',
          name: 'backup',
          component: () => import('@/views/BackupView.vue'),
          meta: { requiresAdmin: true },
        },
        {
          path: 'health',
          name: 'health',
          component: () => import('@/views/HealthView.vue'),
          meta: { requiresAdmin: true },
        },
        {
          path: 'watcher',
          name: 'watcher',
          component: () => import('@/views/WatcherView.vue'),
          meta: { requiresAdmin: true },
        },
        {
          path: 'query',
          redirect: { name: 'data' },
        },
        {
          path: 'schema',
          name: 'schema',
          component: () => import('@/views/SchemaView.vue'),
        },
        {
          path: 'data',
          name: 'data',
          component: () => import('@/views/DataView.vue'),
        },
        {
          path: 'connections',
          name: 'connections',
          component: () => import('@/views/ConnectionsView.vue'),
          meta: { requiresAdmin: false },
        },
        {
          path: 'er',
          name: 'er',
          component: () => import('@/views/ERDiagramView.vue'),
        },
        {
          path: 'saved-queries',
          name: 'saved-queries',
          component: () => import('@/views/SavedQueriesView.vue'),
        },
        {
          path: 'permissions',
          name: 'permissions',
          component: () => import('@/views/PermissionsView.vue'),
          meta: { requiresAdmin: true },
        },
        {
          path: 'rbac',
          redirect: { name: 'permissions' },
        },
        {
          path: 'row-history',
          name: 'row-history',
          component: () => import('@/views/RowHistoryView.vue'),
          meta: { requiresAdmin: true },
        },
        {
          path: 'security',
          name: 'security',
          component: () => import('@/views/SecurityView.vue'),
          meta: { requiresAdmin: false },
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
  const { isAuthenticated, authEnabled, user } = useAuth()

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

  // Admin-only routes - require admin role
  if (to.meta.requiresAdmin && authEnabled.value) {
    if (!isAuthenticated.value) {
      return { name: 'login' }
    }
    if (user.value?.role !== 'admin') {
      // Non-admin users trying to access admin routes - redirect to home
      return { name: 'welcome' }
    }
  }
})

export default router
