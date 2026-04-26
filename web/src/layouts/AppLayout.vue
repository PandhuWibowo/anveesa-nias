<script setup lang="ts">
import { ref, watch, onMounted, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import TopNav from '@/components/layout/TopNav.vue'
import ToastContainer from '@/components/ui/ToastContainer.vue'
import ConfirmModal from '@/components/ui/ConfirmModal.vue'
import StatusBar from '@/components/layout/StatusBar.vue'
import SchemaSearch from '@/components/ui/SchemaSearch.vue'
import { useConnections } from '@/composables/useConnections'

const LS_KEY = 'activeConnId'

const router = useRouter()
const { connections, fetchConnections } = useConnections()

const stored = localStorage.getItem(LS_KEY)
const activeConnId = ref<number | null>(stored ? Number(stored) : null)
const schemaSearchOpen = ref(false)

onMounted(async () => {
  await fetchConnections()
  // Validate that the restored ID still exists; clear it if not
  if (activeConnId.value !== null && !connections.value.find(c => c.id === activeConnId.value)) {
    activeConnId.value = null
  }
  window.addEventListener('keydown', handleGlobal)
})
onBeforeUnmount(() => window.removeEventListener('keydown', handleGlobal))

watch(activeConnId, (id) => {
  if (id === null) localStorage.removeItem(LS_KEY)
  else localStorage.setItem(LS_KEY, String(id))
})

function handleGlobal(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
    e.preventDefault()
    schemaSearchOpen.value = true
  }
}

function handleConnSelect(id: number) {
  activeConnId.value = id
}

function handleSearchNavigate({ connId, table, type }: { connId: number; table: string; type: string }) {
  activeConnId.value = connId
  router.push({ name: 'data' })
}
</script>

<template>
  <div class="app-shell">
    <TopNav
      :activeConnId="activeConnId"
      @select-conn="handleConnSelect"
      @global-search="schemaSearchOpen = true"
    />

    <main class="main-area">
      <router-view :activeConnId="activeConnId" @set-conn="handleConnSelect" />
    </main>

    <StatusBar :activeConnId="activeConnId" />

    <SchemaSearch
      :show="schemaSearchOpen"
      @close="schemaSearchOpen = false"
      @navigate="handleSearchNavigate"
    />
    <ToastContainer />
    <ConfirmModal />
  </div>
</template>
