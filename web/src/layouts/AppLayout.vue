<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import TopNav from '@/components/layout/TopNav.vue'
import ToastContainer from '@/components/ui/ToastContainer.vue'
import ConfirmModal from '@/components/ui/ConfirmModal.vue'
import StatusBar from '@/components/layout/StatusBar.vue'
import SchemaSearch from '@/components/ui/SchemaSearch.vue'
import { useConnections } from '@/composables/useConnections'

const router = useRouter()
const { fetchConnections } = useConnections()

const activeConnId = ref<number | null>(null)
const schemaSearchOpen = ref(false)

onMounted(() => {
  fetchConnections()
  window.addEventListener('keydown', handleGlobal)
})
onBeforeUnmount(() => window.removeEventListener('keydown', handleGlobal))

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
  router.push({ name: type === 'table' ? 'data' : 'schema' })
}
</script>

<template>
  <div class="app-shell">
    <TopNav
      :activeConnId="activeConnId"
      @select-conn="handleConnSelect"
      @global-search="schemaSearchOpen = true"
    />

    <main class="main-area" style="flex:1;min-height:0;overflow:hidden">
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
