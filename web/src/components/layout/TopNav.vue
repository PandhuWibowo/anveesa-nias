<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import { useTheme } from '@/composables/useTheme'
import { useConnections } from '@/composables/useConnections'
import ConnectionsDropdown from '@/components/layout/ConnectionsDropdown.vue'

const props = defineProps<{ activeConnId: number | null }>()
const emit = defineEmits<{
  (e: 'global-search'): void
  (e: 'select-conn', id: number): void
}>()

const route = useRoute()
const router = useRouter()
const { user, authEnabled, logout } = useAuth()
const { mode, toggleTheme } = useTheme()
const { connections } = useConnections()

// Active connection info for display
const activeConn = computed(() =>
  props.activeConnId != null ? connections.value.find(c => c.id === props.activeConnId) ?? null : null
)
const driverColor: Record<string, string> = { postgres: '#336791', mysql: '#f29111', sqlite: '#0f80cc', mssql: '#cc2927' }
const driverLabel: Record<string, string> = { postgres: 'PG', mysql: 'MY', sqlite: 'SQ', mssql: 'MS' }

// Check if user has permission
const hasPermission = (permission: string): boolean => {
  if (!authEnabled.value || !user.value) return true // No auth = full access
  if (user.value.role === 'admin') return true // Admin has all permissions
  // For now, just check role-based access
  return false
}

const isAdmin = computed(() => !authEnabled.value || user.value?.role === 'admin')

// Nav group dropdown (data / admin / tools / monitor)
const openMenu = ref<string | null>(null)
// Connections panel — kept separate so outside-click logic doesn't conflict
const connPanelOpen = ref(false)
const userMenuOpen = ref(false)

const navRef = ref<HTMLElement | null>(null)
const userRef = ref<HTMLElement | null>(null)
const connBtnRef = ref<HTMLElement | null>(null)

// ── Navigation structure ─────────────────────────────────────────
// Direct links (no dropdown)
const directLinks: any[] = []

// Grouped dropdown menus (filtered by permissions)
const allMenuGroups = [
  {
    id: 'data',
    label: 'Data',
    icon: 'table',
    items: [
      { name: 'dashboard',     label: 'Dashboard',     desc: 'Database statistics & overview',       icon: 'dashboard' },
      { name: 'data',          label: 'Data Browser',  desc: 'Browse data, schema & run SQL queries', icon: 'table'   },
      { name: 'connections',   label: 'Connections',   desc: 'Manage your database connections',     icon: 'plug'      },
      { name: 'er',            label: 'ER Diagram',    desc: 'Entity relationship visualization',    icon: 'er'        },
      { name: 'saved-queries', label: 'Saved Queries', desc: 'Your saved SQL queries',               icon: 'saved'     },
      { name: 'approvals',     label: 'Approvals',     desc: 'Review and execute SQL approval requests', icon: 'workflow' },
    ],
  },
  {
    id: 'admin',
    label: 'Administration',
    icon: 'settings',
    requiresAdmin: true,
    items: [
      { name: 'permissions', label: 'Permissions', desc: 'Roles, access groups & permissions',  icon: 'rbac'  },
      { name: 'workflows',   label: 'Workflows',   desc: 'Configure approval workflows',        icon: 'workflow' },
    ],
  },
  {
    id: 'tools',
    label: 'Tools',
    icon: 'wrench',
    requiresAdmin: true,
    items: [
      { name: 'diff',      label: 'Schema Diff',  desc: 'Compare schemas across connections', icon: 'diff'      },
      { name: 'backup',    label: 'Backup',       desc: 'Backup & restore databases',         icon: 'backup'    },
      { name: 'scheduler', label: 'Scheduler',    desc: 'Schedule automated queries',         icon: 'scheduler' },
      { name: 'watcher',   label: 'Watcher',      desc: 'Monitor live table changes',         icon: 'watcher'   },
    ],
  },
  {
    id: 'monitor',
    label: 'Monitoring',
    icon: 'activity',
    items: [
      { name: 'audit',       label: 'Audit Log',   desc: 'Query execution history & errors',  icon: 'audit',      requiresAdmin: false },
      { name: 'health',      label: 'Health',      desc: 'Connection pool & server status',   icon: 'health',     requiresAdmin: true },
      { name: 'row-history', label: 'Row History', desc: 'Track INSERT / UPDATE / DELETE',    icon: 'rowhistory', requiresAdmin: true },
    ],
  },
]

// Filter menu groups and items based on user permissions
const menuGroups = computed(() => {
  return allMenuGroups.map(group => {
    // Filter items within the group
    const filteredItems = group.items.filter((item: any) => {
      // If item requires admin and user is not admin, hide it
      if (item.requiresAdmin && !isAdmin.value) {
        return false
      }
      return true
    })
    
    // If group requires admin and user is not admin, hide entire group
    if (group.requiresAdmin && !isAdmin.value) {
      return null
    }
    
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
function groupActive(group: typeof allMenuGroups[0]) {
  return group.items.some(i => i.name === route.name)
}

function toggleMenu(id: string) {
  openMenu.value = openMenu.value === id ? null : id
  connPanelOpen.value = false
}

function navigate(name: string) {
  openMenu.value = null
  router.push({ name })
}

function handleLogout() {
  userMenuOpen.value = false
  logout()
  router.push({ name: 'login' })
}

function handleOutside(e: MouseEvent) {
  const t = e.target as Node
  // Close nav dropdowns when clicking outside the nav area
  if (navRef.value && !navRef.value.contains(t)) openMenu.value = null
  // Close connections panel when clicking outside its wrapper
  if (connBtnRef.value && !connBtnRef.value.contains(t)) connPanelOpen.value = false
  // Close user menu when clicking outside it
  if (userRef.value && !userRef.value.contains(t)) userMenuOpen.value = false
}

onMounted(() => document.addEventListener('mousedown', handleOutside))
onBeforeUnmount(() => document.removeEventListener('mousedown', handleOutside))
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
      <div v-if="connPanelOpen" class="topnav__conn-panel">
        <ConnectionsDropdown
          :activeConnId="activeConnId"
          @select-conn="(id) => { emit('select-conn', id) }"
          @close="connPanelOpen = false"
        />
      </div>
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
          @click="toggleMenu(group.id)"
          type="button"
        >
          <!-- Group icons -->
          <svg v-if="group.icon === 'table'" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
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
        <div v-if="openMenu === group.id" class="topnav__dropdown">
          <div class="topnav__dropdown-label">{{ group.label }}</div>
          <button
            v-for="item in group.items"
            :key="item.name"
            class="topnav__dropdown-item"
            :class="{ 'topnav__dropdown-item--active': route.name === item.name }"
            @click="navigate(item.name)"
            type="button"
          >
            <div class="topnav__dropdown-icon">
              <svg v-if="item.icon === 'dashboard'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="2" width="9" height="11" rx="1"/><rect x="13" y="2" width="9" height="7" rx="1"/><rect x="2" y="15" width="9" height="7" rx="1"/><rect x="13" y="11" width="9" height="11" rx="1"/></svg>
              <svg v-else-if="item.icon === 'layers'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="12 2 2 7 12 12 22 7 12 2"/><polyline points="2 17 12 22 22 17"/><polyline points="2 12 12 17 22 12"/></svg>
              <svg v-else-if="item.icon === 'table'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
              <svg v-else-if="item.icon === 'er'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="3" width="6" height="6" rx="1"/><rect x="16" y="3" width="6" height="6" rx="1"/><rect x="9" y="15" width="6" height="6" rx="1"/><line x1="8" y1="6" x2="16" y2="6"/><line x1="12" y1="9" x2="12" y2="15"/></svg>
              <svg v-else-if="item.icon === 'saved'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/><polyline points="7 3 7 8 15 8"/></svg>
              <svg v-else-if="item.icon === 'workflow'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="5" cy="6" r="2"/><circle cx="19" cy="6" r="2"/><circle cx="12" cy="18" r="2"/><path d="M7 6h10"/><path d="M6.5 7.5l4 8"/><path d="M17.5 7.5l-4 8"/></svg>
              <svg v-else-if="item.icon === 'plug'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18.36 6.64a9 9 0 1 1-12.73 0"/><line x1="12" y1="2" x2="12" y2="12"/></svg>
              <svg v-else-if="item.icon === 'users'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>
              <svg v-else-if="item.icon === 'rbac'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>
              <svg v-else-if="item.icon === 'diff'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="20" x2="18" y2="10"/><line x1="12" y1="20" x2="12" y2="4"/><line x1="6" y1="20" x2="6" y2="14"/></svg>
              <svg v-else-if="item.icon === 'backup'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
              <svg v-else-if="item.icon === 'scheduler'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
              <svg v-else-if="item.icon === 'watcher'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
              <svg v-else-if="item.icon === 'audit'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/></svg>
              <svg v-else-if="item.icon === 'health'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>
              <svg v-else-if="item.icon === 'rowhistory'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 3h18v4H3z"/><path d="M3 10h18v4H3z"/><path d="M3 17h18v4H3z"/></svg>
            </div>
            <div class="topnav__dropdown-info">
              <span class="topnav__dropdown-name">{{ item.label }}</span>
              <span class="topnav__dropdown-desc">{{ item.desc }}</span>
            </div>
            <svg v-if="route.name === item.name" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" style="color:var(--brand);flex-shrink:0">
              <polyline points="20 6 9 17 4 12"/>
            </svg>
          </button>
        </div>
      </div>
    </nav>

    <!-- Right actions -->
    <div class="topnav__actions">
      <!-- Search -->
      <button class="topnav__action-btn" title="Global search (⌘K)" @click="$emit('global-search')">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
        <kbd class="topnav__kbd">⌘K</kbd>
      </button>

      <!-- Theme -->
      <button class="topnav__action-btn" :title="`Switch to ${mode === 'dark' ? 'light' : 'dark'} mode`" @click="toggleTheme">
        <svg v-if="mode === 'dark'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>
        <svg v-else width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>
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
  height: 48px;
  background: var(--bg-surface);
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
  padding: 0 14px;
  gap: 6px;
  z-index: 200;
  position: relative;
}

/* ── Brand ── */
.topnav__brand {
  display: flex;
  align-items: center;
  gap: 9px;
  flex-shrink: 0;
}
.topnav__logo {
  width: 30px;
  height: 30px;
  background: var(--brand-dim);
  border: 1px solid var(--brand-ring);
  border-radius: 8px;
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
  font-size: 13px;
  color: var(--text-secondary);
  font-weight: 400;
}
.topnav__brand-name strong {
  color: var(--brand);
  font-weight: 700;
}
.topnav__brand-version {
  font-size: 9.5px;
  color: var(--text-muted);
  letter-spacing: 0.3px;
}

.topnav__divider {
  width: 1px;
  height: 20px;
  background: var(--border);
  flex-shrink: 0;
  margin: 0 4px;
}

/* ── Connections button ── */
.topnav__conn-wrap {
  position: relative;
  flex-shrink: 0;
}
.topnav__conn-btn {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 5px 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-elevated);
  cursor: pointer;
  font-size: 12.5px;
  color: var(--text-muted);
  white-space: nowrap;
  max-width: 240px;
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
  width: 22px;
  height: 16px;
  border-radius: 3px;
  font-size: 9px;
  font-weight: 700;
  color: #fff;
  letter-spacing: 0.3px;
}
.topnav__conn-name {
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 120px;
}
.topnav__conn-host {
  font-size: 11px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 80px;
}
.topnav__conn-panel {
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  z-index: 9999;
}

/* ── Nav ── */
.topnav__nav {
  display: flex;
  align-items: center;
  gap: 2px;
  flex: 1;
  min-width: 0;
}

/* Direct link */
.topnav__link {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  border-radius: 6px;
  font-size: 13px;
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
  gap: 6px;
  padding: 6px 10px;
  border: none;
  border-radius: 6px;
  background: transparent;
  font-size: 13px;
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
  width: 5px;
  height: 5px;
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
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  min-width: 260px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: var(--shadow-lg);
  z-index: 9999;
  overflow: hidden;
  padding: 6px;
}

.topnav__dropdown-label {
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.7px;
  color: var(--text-muted);
  padding: 6px 10px 4px;
}

.topnav__dropdown-item {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 8px 10px;
  border: none;
  border-radius: 7px;
  background: transparent;
  cursor: pointer;
  text-align: left;
  transition: background 0.1s;
}
.topnav__dropdown-item:hover { background: var(--bg-surface); }
.topnav__dropdown-item--active { background: var(--brand-dim); }

.topnav__dropdown-icon {
  width: 30px;
  height: 30px;
  border-radius: 7px;
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
  font-size: 12.5px;
  font-weight: 600;
  color: var(--text-primary);
}
.topnav__dropdown-desc {
  font-size: 11px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ── Right actions ── */
.topnav__actions {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-left: 8px;
  flex-shrink: 0;
}

.topnav__action-btn {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 5px 8px;
  border: none;
  background: transparent;
  color: var(--text-muted);
  border-radius: 6px;
  cursor: pointer;
  transition: color 0.12s, background 0.12s;
}
.topnav__action-btn:hover { color: var(--text-primary); background: var(--bg-hover); }

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
  gap: 6px;
  padding: 4px 8px 4px 4px;
  border: 1px solid var(--border);
  background: var(--bg-elevated);
  border-radius: 8px;
  cursor: pointer;
  transition: border-color 0.12s, background 0.12s;
}
.topnav__user-btn:hover { border-color: var(--brand); }

.topnav__avatar {
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: var(--brand);
  color: var(--brand-fg, #fff);
  font-size: 10px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.topnav__avatar--lg { width: 32px; height: 32px; font-size: 13px; }

.topnav__username {
  font-size: 12px;
  font-weight: 500;
  color: var(--text-secondary);
}

.topnav__user-menu {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  min-width: 190px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: var(--shadow-lg);
  z-index: 9999;
  overflow: hidden;
}

.topnav__user-info {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 14px;
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
  gap: 8px;
  width: 100%;
  padding: 10px 14px;
  background: transparent;
  border: none;
  font-size: 12.5px;
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
</style>
