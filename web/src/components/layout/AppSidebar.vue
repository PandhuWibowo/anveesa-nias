<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import axios from 'axios'
import { useAuth } from '@/composables/useAuth'
import { useTheme } from '@/composables/useTheme'
import { useConnections } from '@/composables/useConnections'
import { useCommandPalette } from '@/composables/useCommandPalette'
import { MAIN_SECTIONS, SETTINGS_SECTION, sectionOf, workspaceMeta, type NavItem, type NavSection } from '@/navigation'
import { useNavigation } from '@/composables/useNavigation'
import { DRIVER_COLOR, DRIVER_NAME } from '@/utils/drivers'
import NavIcon from '@/components/ui/NavIcon.vue'
import DriverIcon from '@/components/ui/DriverIcon.vue'
import ConnectionsDropdown from '@/components/layout/ConnectionsDropdown.vue'

const props = defineProps<{ activeConnId: number | null; collapsed: boolean; mobileOpen: boolean }>()
const emit = defineEmits<{
  (e: 'select-conn', id: number): void
  (e: 'toggle-collapse'): void
  (e: 'close-mobile'): void
}>()

const route = useRoute()
const router = useRouter()
const { user, authEnabled, logout, hasAnyPermission } = useAuth()
const { mode, toggleTheme } = useTheme()
const { connections } = useConnections()
const palette = useCommandPalette()

const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)
const modKey = isMac ? '⌘' : 'Ctrl'

// ── Active connection ────────────────────────────────────────────
const activeConn = computed(() =>
  props.activeConnId != null ? connections.value.find((c) => c.id === props.activeConnId) ?? null : null,
)

// ── Sections ─────────────────────────────────────────────────────
// The sidebar lists sections, not pages. A section opens on the page you last used
// there; its other pages are tabs at the top of the content panel (SectionTabs).
const nav = useNavigation()

const activeDriver = computed(() => activeConn.value?.driver ?? null)
const currentTab = computed(() => (typeof route.query.tab === 'string' ? route.query.tab : null))
const currentSection = computed(() => sectionOf(route.name as string, currentTab.value))

type SidebarEntry = { section: NavSection; label: string; icon: string; target: NavItem | undefined }

const entries = computed<SidebarEntry[]>(() =>
  MAIN_SECTIONS.map((section) => {
    const meta = section.connectionScoped ? workspaceMeta(activeDriver.value) : { label: section.label, icon: section.icon }
    return { section, ...meta, target: nav.entryFor(section, activeDriver.value) }
  }).filter((e) => e.target || e.section.connectionScoped),
)

const settingsTarget = computed(() => nav.entryFor(SETTINGS_SECTION, activeDriver.value))

function openEntry(e: SidebarEntry) {
  emit('close-mobile')
  // With no usable connection, the connection section's job is to get you one.
  if (!e.target) return router.push({ name: 'connections', query: { new: '1' } })
  router.push({ name: e.target.name, query: e.target.query })
}

function openSettings() {
  emit('close-mobile')
  const t = settingsTarget.value
  if (t) router.push({ name: t.name, query: t.query })
}

function hrefFor(item: NavItem | undefined) {
  return item ? router.resolve({ name: item.name, query: item.query }).href : router.resolve({ name: 'connections' }).href
}

// ── Connection switcher ──────────────────────────────────────────
const connOpen = ref(false)
const connBtn = ref<HTMLElement | null>(null)
const connPanel = ref<HTMLElement | null>(null)
const connPanelStyle = computed(() => {
  if (!connOpen.value || !connBtn.value) return {}
  const r = connBtn.value.getBoundingClientRect()
  const width = 300
  // Collapsed rail: open beside the button. Otherwise: drop below it.
  if (props.collapsed && window.innerWidth > 900) {
    return { position: 'fixed' as const, top: `${r.top}px`, left: `${r.right + 8}px`, zIndex: 900 }
  }
  const left = Math.max(8, Math.min(r.left, window.innerWidth - width - 8))
  return { position: 'fixed' as const, top: `${r.bottom + 6}px`, left: `${left}px`, zIndex: 900 }
})

// ── User menu ────────────────────────────────────────────────────
const userOpen = ref(false)
const userWrap = ref<HTMLElement | null>(null)
const initial = computed(() => user.value?.username?.[0]?.toUpperCase() ?? 'U')

async function handleLogout() {
  userOpen.value = false
  await logout()
  router.push({ name: 'login' })
}

function goAccount(name: string) {
  userOpen.value = false
  router.push({ name })
  emit('close-mobile')
}

// ── Notifications ────────────────────────────────────────────────
const canViewNotifications = computed(() => hasAnyPermission(['notifications.view']))
const unread = ref(0)
let poll: number | null = null

async function loadUnread() {
  if (!authEnabled.value || !canViewNotifications.value) { unread.value = 0; return }
  try {
    const { data } = await axios.get<{ count: number }>('/api/notifications/unread')
    unread.value = Number(data?.count || 0)
  } catch {
    unread.value = 0
  }
}
function startPolling() {
  if (poll !== null) window.clearInterval(poll)
  poll = null
  if (!authEnabled.value || !canViewNotifications.value) return
  void loadUnread()
  poll = window.setInterval(loadUnread, 30000)
}
watch([() => authEnabled.value, canViewNotifications, () => user.value?.id], startPolling, { immediate: true })
watch(() => route.name, (n) => { if (n === 'notifications') void loadUnread() })

// ── Outside click ────────────────────────────────────────────────
function onDocDown(e: MouseEvent) {
  const t = e.target as Node
  if (connOpen.value && !connBtn.value?.contains(t) && !connPanel.value?.contains(t)) connOpen.value = false
  if (userOpen.value && !userWrap.value?.contains(t)) userOpen.value = false
}
onMounted(() => document.addEventListener('mousedown', onDocDown))
onBeforeUnmount(() => {
  document.removeEventListener('mousedown', onDocDown)
  if (poll !== null) window.clearInterval(poll)
})
watch(() => route.fullPath, () => { connOpen.value = false; userOpen.value = false })
</script>

<template>
  <aside class="sb" :class="{ 'sb--collapsed': collapsed, 'sb--mobile-open': mobileOpen }" aria-label="Primary">
    <!-- Brand -->
    <div class="sb__brand">
      <router-link :to="{ name: 'welcome' }" class="sb__logo" title="Home" @click="emit('close-mobile')">
        <span class="sb__mark">
          <NavIcon name="nias" :size="15" />
        </span>
        <span class="sb__name">Nias</span>
      </router-link>
      <button class="sb__icon-btn sb__collapse" type="button" :title="collapsed ? 'Expand sidebar' : 'Collapse sidebar'" :aria-label="collapsed ? 'Expand sidebar' : 'Collapse sidebar'" @click="emit('toggle-collapse')">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <rect x="3" y="4" width="18" height="16" rx="2" /><path d="M9 4v16" />
        </svg>
      </button>
      <button class="sb__icon-btn sb__close" type="button" aria-label="Close menu" @click="emit('close-mobile')">
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" aria-hidden="true"><path d="M6 6l12 12M18 6 6 18" /></svg>
      </button>
    </div>

    <!-- Search + connection -->
    <div class="sb__top">
      <button class="sb__search" type="button" :title="`Search (${modKey}K)`" @click="palette.show(); emit('close-mobile')">
        <NavIcon name="search" :size="15" />
        <span class="sb__search-label">Search</span>
        <kbd class="sb__kbd">{{ modKey }}K</kbd>
      </button>

      <button
        ref="connBtn"
        class="sb__conn"
        :class="{ 'is-open': connOpen }"
        type="button"
        :title="activeConn ? `${activeConn.name} · ${DRIVER_NAME[activeConn.driver] ?? activeConn.driver}` : 'Choose a connection'"
        @click="connOpen = !connOpen; userOpen = false"
      >
        <span v-if="activeConn" class="sb__driver" :style="{ background: DRIVER_COLOR[activeConn.driver] ?? '#4b5563' }">
          <DriverIcon :driver="activeConn.driver" :size="13" />
        </span>
        <span v-else class="sb__driver sb__driver--empty"><NavIcon name="plug" :size="13" /></span>
        <span class="sb__conn-text">
          <span class="sb__conn-name">{{ activeConn ? activeConn.name : 'No connection' }}</span>
          <span class="sb__conn-sub">{{ activeConn ? (activeConn.host || DRIVER_NAME[activeConn.driver]) : 'Choose one to start' }}</span>
        </span>
        <svg class="sb__chev" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m8 9 4-4 4 4M8 15l4 4 4-4" /></svg>
      </button>
      <Teleport to="body">
        <div v-if="connOpen" ref="connPanel" :style="connPanelStyle">
          <ConnectionsDropdown
            :activeConnId="activeConnId"
            @select-conn="(id: number) => emit('select-conn', id)"
            @close="connOpen = false"
          />
        </div>
      </Teleport>
    </div>

    <!-- Navigation -->
    <nav class="sb__nav">
      <router-link
        :to="{ name: 'welcome' }"
        class="sb__item"
        :class="{ 'is-active': route.name === 'welcome' }"
        :aria-current="route.name === 'welcome' ? 'page' : undefined"
        :title="collapsed ? 'Home' : undefined"
        @click="emit('close-mobile')"
      >
        <NavIcon name="home" :size="16" />
        <span class="sb__item-label">Home</span>
      </router-link>

      <div class="sb__sep" />

      <a
        v-for="e in entries"
        :key="e.section.id"
        :href="hrefFor(e.target)"
        class="sb__item"
        :class="{ 'is-active': currentSection?.id === e.section.id, 'sb__item--muted': !e.target }"
        :aria-current="currentSection?.id === e.section.id ? 'page' : undefined"
        :title="collapsed ? e.label : undefined"
        @click.prevent="openEntry(e)"
      >
        <NavIcon :name="e.icon" :size="16" />
        <span class="sb__item-label">{{ e.label }}</span>
      </a>
    </nav>

    <!-- Footer -->
    <div class="sb__foot">
      <router-link
        v-if="canViewNotifications"
        :to="{ name: 'notifications' }"
        class="sb__item"
        :class="{ 'is-active': route.name === 'notifications' }"
        :title="collapsed ? 'Notifications' : undefined"
        @click="emit('close-mobile')"
      >
        <span class="sb__bell">
          <NavIcon name="bell" :size="16" />
          <span v-if="unread > 0 && collapsed" class="sb__bell-dot" />
        </span>
        <span class="sb__item-label">Notifications</span>
        <span v-if="unread > 0" class="sb__count">{{ unread > 99 ? '99+' : unread }}</span>
      </router-link>

      <a
        v-if="settingsTarget"
        :href="hrefFor(settingsTarget)"
        class="sb__item"
        :class="{ 'is-active': currentSection?.id === 'settings' }"
        :aria-current="currentSection?.id === 'settings' ? 'page' : undefined"
        :title="collapsed ? 'Settings' : undefined"
        @click.prevent="openSettings"
      >
        <NavIcon name="settings" :size="16" />
        <span class="sb__item-label">Settings</span>
      </a>

      <div class="sb__foot-row">
        <div v-if="authEnabled" ref="userWrap" class="sb__user-wrap">
          <button class="sb__user" type="button" :aria-expanded="userOpen" @click="userOpen = !userOpen; connOpen = false">
            <span class="sb__avatar">{{ initial }}</span>
            <span class="sb__user-text">
              <span class="sb__user-name">{{ user?.username ?? 'User' }}</span>
              <span class="sb__user-role">{{ user?.role }}</span>
            </span>
          </button>
          <Transition name="pop">
            <div v-if="userOpen" class="sb__menu" role="menu">
              <button class="sb__menu-item" role="menuitem" type="button" @click="goAccount('security')">
                <NavIcon name="lock" :size="15" /> Password and 2FA
              </button>
              <button class="sb__menu-item" role="menuitem" type="button" @click="goAccount('docs')">
                <NavIcon name="book" :size="15" /> Documentation
              </button>
              <div class="sb__menu-sep" />
              <button class="sb__menu-item sb__menu-item--danger" role="menuitem" type="button" @click="handleLogout">
                <NavIcon name="logout" :size="15" /> Sign out
              </button>
            </div>
          </Transition>
        </div>
        <router-link v-else :to="{ name: 'docs' }" class="sb__item sb__docs" :title="collapsed ? 'Documentation' : undefined">
          <NavIcon name="book" :size="16" />
          <span class="sb__item-label">Documentation</span>
        </router-link>

        <button
          class="sb__icon-btn sb__theme"
          type="button"
          :title="mode === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'"
          :aria-label="mode === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'"
          @click="toggleTheme"
        >
          <NavIcon :name="mode === 'dark' ? 'sun' : 'moon'" :size="16" />
        </button>
      </div>
    </div>
  </aside>
</template>

<style scoped>
.sb {
  width: var(--sidebar-w);
  flex-shrink: 0;
  height: 100%;
  display: flex;
  flex-direction: column;
  /* The sidebar sits on the canvas; the content panel floats beside it. */
  background: var(--canvas);
  transition: width 180ms var(--ease);
  overflow: hidden;
  z-index: 40;
}

/* Brand */
.sb__brand {
  display: flex;
  align-items: center;
  gap: 4px;
  height: 52px;
  padding: 0 10px 0 14px;
  flex-shrink: 0;
}
.sb__logo {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 1;
  min-width: 0;
  text-decoration: none;
  color: var(--text-primary);
}
.sb__mark {
  width: 28px;
  height: 28px;
  border-radius: 30%;
  display: grid;
  place-items: center;
  /* Nias signature: the only place pink appears in the app (DESIGN.md › Palette). */
  background: var(--signature-bg);
  color: var(--signature);
  flex-shrink: 0;
}
.sb__name {
  font-size: 15px;
  font-weight: 600;
  letter-spacing: -0.01em;
}
.sb__icon-btn {
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border: none;
  border-radius: var(--r-sm);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  flex-shrink: 0;
  transition: background var(--dur) var(--ease), color var(--dur) var(--ease);
}
.sb__icon-btn:hover { background: var(--bg-hover-strong); color: var(--text-primary); }
.sb__close { display: none; }

/* Search + connection */
.sb__top {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 4px 10px 10px;
  flex-shrink: 0;
}
.sb__search {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 32px;
  padding: 0 8px 0 10px;
  border: 1px solid transparent;
  border-radius: var(--r-sm);
  background: var(--surface);
  box-shadow: var(--shadow-panel);
  color: var(--text-muted);
  font: inherit;
  font-size: 13px;
  cursor: pointer;
  transition: border-color var(--dur) var(--ease), color var(--dur) var(--ease);
}
.sb__search:hover { border-color: var(--border-2); color: var(--text-2); }
.sb__search-label { flex: 1; text-align: left; }
.sb__kbd {
  font-family: var(--mono);
  font-size: 11px;
  padding: 1px 5px;
  border-radius: 4px;
  border: 1px solid var(--border-2);
  background: var(--bg-body);
  color: var(--text-muted);
}

.sb__conn {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 6px 8px;
  border: 1px solid transparent;
  border-radius: var(--r-sm);
  background: transparent;
  color: var(--text-primary);
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: background var(--dur) var(--ease);
}
.sb__conn:hover,
.sb__conn.is-open { background: var(--bg-hover-strong); }
.sb__driver {
  width: 24px;
  height: 24px;
  border-radius: 6px;
  display: grid;
  place-items: center;
  flex-shrink: 0;
  color: #fff;
}
.sb__driver--empty {
  background: var(--bg-surface);
  border: 1px dashed var(--border-2);
  color: var(--text-muted);
}
.sb__conn-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  line-height: 1.25;
}
.sb__conn-name,
.sb__conn-sub {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sb__conn-name { font-size: 13px; font-weight: 500; }
.sb__conn-sub { font-size: 11.5px; color: var(--text-muted); }
.sb__chev { color: var(--text-muted); flex-shrink: 0; }

/* Navigation */
.sb__nav {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
  padding: 4px 10px 12px;
  scrollbar-width: thin;
}
.sb__nav {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.sb__sep {
  height: 1px;
  margin: 6px 8px;
  background: var(--border);
}
.sb__item--muted { color: var(--muted); }
.sb__item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 10px;
  height: 34px;
  padding: 0 9px;
  border-radius: var(--r-sm);
  color: var(--text-secondary);
  text-decoration: none;
  font-size: 13.5px;
  white-space: nowrap;
  transition: background var(--dur) var(--ease), color var(--dur) var(--ease);
}
.sb__item svg { flex-shrink: 0; color: var(--text-muted); transition: color var(--dur) var(--ease); }
.sb__item:hover { background: var(--bg-hover-strong); color: var(--text-primary); }
.sb__item:hover svg { color: var(--text-secondary); }
.sb__item.is-active {
  background: var(--surface);
  color: var(--accent);
  font-weight: 500;
  box-shadow: var(--shadow-panel);
}
.sb__item.is-active svg { color: var(--accent); }
.sb__item-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* Footer */
.sb__foot {
  flex-shrink: 0;
  padding: 8px 10px 10px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.sb__bell { position: relative; display: flex; }
.sb__bell-dot {
  position: absolute;
  top: -1px;
  right: -1px;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--danger);
  box-shadow: 0 0 0 2px var(--canvas);
}
.sb__count {
  min-width: 20px;
  padding: 1px 6px;
  border-radius: 999px;
  background: var(--danger);
  color: #fff;
  font-size: 11px;
  font-weight: 600;
  text-align: center;
  font-variant-numeric: tabular-nums;
}
.sb__foot-row {
  display: flex;
  align-items: center;
  gap: 4px;
}
.sb__user-wrap { position: relative; flex: 1; min-width: 0; }
.sb__docs { flex: 1; min-width: 0; }
.sb__user {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 5px 6px;
  border: none;
  border-radius: var(--r-sm);
  background: transparent;
  color: var(--text-primary);
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: background var(--dur) var(--ease);
}
.sb__user:hover { background: var(--bg-hover-strong); }
.sb__avatar {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  flex-shrink: 0;
  background: var(--bg-surface);
  border: 1px solid var(--border-2);
  font-size: 12px;
  font-weight: 600;
  color: var(--text-secondary);
}
.sb__user-text {
  min-width: 0;
  display: flex;
  flex-direction: column;
  line-height: 1.25;
}
.sb__user-name,
.sb__user-role {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sb__user-name { font-size: 13px; font-weight: 500; }
.sb__user-role { font-size: 11.5px; color: var(--text-muted); text-transform: capitalize; }

.sb__menu {
  position: absolute;
  left: 0;
  bottom: calc(100% + 6px);
  min-width: 210px;
  padding: 4px;
  background: var(--bg-elevated);
  border: 1px solid var(--border-2);
  border-radius: var(--r);
  box-shadow: var(--shadow-lg);
  z-index: 60;
}
.sb__menu-item {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 7px 9px;
  border: none;
  border-radius: var(--r-sm);
  background: transparent;
  color: var(--text-secondary);
  font: inherit;
  font-size: 13px;
  text-align: left;
  cursor: pointer;
}
.sb__menu-item:hover { background: var(--bg-hover-strong); color: var(--text-primary); }
.sb__menu-item--danger:hover { background: var(--danger-bg); color: var(--danger); }
.sb__menu-sep { height: 1px; margin: 4px 2px; background: var(--border); }

.pop-enter-active, .pop-leave-active { transition: opacity 110ms var(--ease), transform 110ms var(--ease); }
.pop-enter-from, .pop-leave-to { opacity: 0; transform: translateY(4px); }

/* Collapsed rail */
.sb--collapsed { width: var(--sidebar-w-collapsed); }
.sb--collapsed .sb__name,
.sb--collapsed .sb__search-label,
.sb--collapsed .sb__kbd,
.sb--collapsed .sb__conn-text,
.sb--collapsed .sb__chev,
.sb--collapsed .sb__item-label,
.sb--collapsed .sb__count,
.sb--collapsed .sb__user-text { display: none; }
.sb--collapsed .sb__brand { flex-direction: column; height: auto; padding: 12px 0 6px; gap: 8px; }
.sb--collapsed .sb__logo { flex: none; }
.sb--collapsed .sb__top { padding: 4px 12px 10px; align-items: center; }
.sb--collapsed .sb__search { width: 32px; padding: 0; justify-content: center; }
.sb--collapsed .sb__conn { padding: 4px; }
.sb--collapsed .sb__nav { padding: 4px 12px 12px; }
.sb--collapsed .sb__item { width: 32px; padding: 0; justify-content: center; }
.sb--collapsed .sb__foot { align-items: center; padding: 8px 12px 10px; }
.sb--collapsed .sb__foot-row { flex-direction: column-reverse; }
.sb--collapsed .sb__user { padding: 3px; }
.sb--collapsed .sb__menu { left: calc(100% + 10px); bottom: 0; }

/* Mobile drawer */
@media (max-width: 900px) {
  .sb,
  .sb--collapsed {
    position: fixed;
    inset: 0 auto 0 0;
    width: min(300px, 86vw);
    transform: translateX(-100%);
    transition: transform 200ms var(--ease);
    box-shadow: var(--shadow-lg);
    z-index: 120;
  }
  .sb--mobile-open { transform: translateX(0); }
  /* Off-screen drawer: no shadow bleeding in from the edge, and out of the tab order. */
  .sb:not(.sb--mobile-open) { box-shadow: none; visibility: hidden; transition: transform 200ms var(--ease), visibility 0s 200ms; }
  .sb__collapse { display: none; }
  .sb__close { display: grid; }
  /* The drawer is always the full layout, even if desktop was collapsed. */
  .sb--collapsed .sb__name,
  .sb--collapsed .sb__search-label,
  .sb--collapsed .sb__conn-text,
  .sb--collapsed .sb__chev,
  .sb--collapsed .sb__item-label,
  .sb--collapsed .sb__user-text { display: revert; }
  .sb--collapsed .sb__count { display: block; }
  .sb__kbd { display: none; }
  .sb--collapsed .sb__brand { flex-direction: row; height: 52px; padding: 0 10px 0 14px; }
  .sb--collapsed .sb__logo { flex: 1; }
  .sb--collapsed .sb__top { padding: 4px 10px 10px; align-items: stretch; }
  .sb--collapsed .sb__search { width: auto; padding: 0 8px 0 10px; justify-content: flex-start; }
  .sb--collapsed .sb__conn { padding: 6px 8px; }
  .sb--collapsed .sb__nav { padding: 4px 10px 12px; }
  .sb--collapsed .sb__item { width: auto; padding: 0 8px; justify-content: flex-start; }
  .sb--collapsed .sb__foot { align-items: stretch; padding: 8px 10px 10px; }
  .sb--collapsed .sb__foot-row { flex-direction: row; }
  .sb--collapsed .sb__menu { left: 0; bottom: calc(100% + 6px); }
  .sb__item { height: 38px; }
}

@media (prefers-reduced-motion: reduce) {
  .sb, .sb--collapsed { transition: none; }
}
</style>
