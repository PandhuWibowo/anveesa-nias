<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { useRoute } from 'vue-router'
import AppSidebar from '@/components/layout/AppSidebar.vue'
import CommandPalette from '@/components/layout/CommandPalette.vue'
import SectionTabs from '@/components/layout/SectionTabs.vue'
import NavIcon from '@/components/ui/NavIcon.vue'
import ToastContainer from '@/components/ui/ToastContainer.vue'
import ConfirmModal from '@/components/ui/ConfirmModal.vue'
import { useConnections } from '@/composables/useConnections'
import { useCommandPalette } from '@/composables/useCommandPalette'
import { NAV_GROUPS, navKey, sectionOf, workspaceMeta } from '@/navigation'
import { useNavigation } from '@/composables/useNavigation'

const LS_KEY = 'activeConnId'
const COLLAPSED_KEY = 'nias:sidebarCollapsed'

const route = useRoute()
const { connections, fetchConnections } = useConnections()
const palette = useCommandPalette()

const stored = localStorage.getItem(LS_KEY)
const activeConnId = ref<number | null>(stored ? Number(stored) : null)
const collapsed = ref(localStorage.getItem(COLLAPSED_KEY) === '1')
const mobileOpen = ref(false)

onMounted(async () => {
  await fetchConnections()
  // Validate that the restored ID still exists; clear it if not
  if (activeConnId.value !== null && !connections.value.find(c => c.id === activeConnId.value)) {
    activeConnId.value = null
  }
  // Start with a usable connection instead of an empty picker.
  if (activeConnId.value === null) {
    const first = connections.value.find(c => !c.disconnected)
    if (first) activeConnId.value = first.id
  }
})

watch(activeConnId, (id) => {
  if (id === null) localStorage.removeItem(LS_KEY)
  else localStorage.setItem(LS_KEY, String(id))
})

watch(collapsed, (v) => localStorage.setItem(COLLAPSED_KEY, v ? '1' : '0'))

function handleConnSelect(id: number) {
  activeConnId.value = id
}

const nav = useNavigation()
const activeDriver = computed(() => connections.value.find(c => c.id === activeConnId.value)?.driver ?? null)

// Record visited pages: the palette offers them first, and each sidebar section
// reopens on the page you last used in it.
const allItems = NAV_GROUPS.flatMap((g) => g.items)
watch(() => route.fullPath, () => {
  mobileOpen.value = false
  const tab = typeof route.query.tab === 'string' ? route.query.tab : undefined
  const match = allItems.find((i) => i.name === route.name && i.query?.tab === tab)
    ?? allItems.find((i) => i.name === route.name && !i.query)
  if (!match) return
  palette.pushRecent(navKey(match))
  const section = sectionOf(match.name, match.query?.tab)
  if (section) nav.rememberVisit(section, match)
}, { immediate: true })

const currentLabel = computed(() => {
  const tab = typeof route.query.tab === 'string' ? route.query.tab : null
  const section = sectionOf(route.name as string, tab)
  if (!section) return route.name === 'welcome' ? 'Home' : 'Nias'
  return section.connectionScoped ? workspaceMeta(activeDriver.value).label : section.label
})

function onKey(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
    e.preventDefault()
    palette.toggle()
  } else if ((e.metaKey || e.ctrlKey) && e.key === '\\') {
    e.preventDefault()
    collapsed.value = !collapsed.value
  }
}
onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <div class="app-shell">
    <AppSidebar
      :activeConnId="activeConnId"
      :collapsed="collapsed"
      :mobileOpen="mobileOpen"
      @select-conn="handleConnSelect"
      @toggle-collapse="collapsed = !collapsed"
      @close-mobile="mobileOpen = false"
    />
    <div v-if="mobileOpen" class="app-scrim" @click="mobileOpen = false" />

    <div class="app-content">
      <header class="mobile-bar">
        <button class="mobile-bar__btn" type="button" aria-label="Open menu" @click="mobileOpen = true">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" aria-hidden="true"><path d="M4 7h16M4 12h16M4 17h16" /></svg>
        </button>
        <span class="mobile-bar__title">{{ currentLabel }}</span>
        <button class="mobile-bar__btn" type="button" aria-label="Search" @click="palette.show()">
          <NavIcon name="search" :size="18" />
        </button>
      </header>

      <SectionTabs :active-driver="activeDriver" />
      <main class="main-area">
        <router-view :activeConnId="activeConnId" @set-conn="handleConnSelect" />
      </main>
    </div>

    <CommandPalette @select-conn="handleConnSelect" />
    <ToastContainer />
    <ConfirmModal />
  </div>
</template>
