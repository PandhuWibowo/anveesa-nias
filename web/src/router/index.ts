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
      meta: { requiresAuth: false },
      children: [
        {
          path: '',
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
        },
        {
          path: 'audit',
          name: 'audit',
          component: () => import('@/views/AuditLogView.vue'),
        },
        {
          path: 'diff',
          name: 'diff',
          component: () => import('@/views/SchemaDiffView.vue'),
        },
        {
          path: 'scheduler',
          name: 'scheduler',
          component: () => import('@/views/SchedulerView.vue'),
        },
        {
          path: 'backup',
          name: 'backup',
          component: () => import('@/views/BackupView.vue'),
        },
        {
          path: 'health',
          name: 'health',
          component: () => import('@/views/HealthView.vue'),
        },
        {
          path: 'watcher',
          name: 'watcher',
          component: () => import('@/views/WatcherView.vue'),
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
          path: 'rbac',
          name: 'rbac',
          component: () => import('@/views/RBACView.vue'),
        },
        {
          path: 'row-history',
          name: 'row-history',
          component: () => import('@/views/RowHistoryView.vue'),
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
  const { isAuthenticated, authEnabled } = useAuth()

  if (to.meta.guest && isAuthenticated.value) {
    return { name: 'welcome' }
  }
  if (to.meta.requiresAuth && authEnabled.value && !isAuthenticated.value) {
    return { name: 'login' }
  }
})

export default router
