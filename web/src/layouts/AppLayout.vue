<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'
import TopNav from '@/components/layout/TopNav.vue'
import ToastContainer from '@/components/ui/ToastContainer.vue'
import ConfirmModal from '@/components/ui/ConfirmModal.vue'
import StatusBar from '@/components/layout/StatusBar.vue'
import { useConnections } from '@/composables/useConnections'

const LS_KEY = 'activeConnId'

const { connections, fetchConnections } = useConnections()

const stored = localStorage.getItem(LS_KEY)
const activeConnId = ref<number | null>(stored ? Number(stored) : null)

onMounted(async () => {
  await fetchConnections()
  // Validate that the restored ID still exists; clear it if not
  if (activeConnId.value !== null && !connections.value.find(c => c.id === activeConnId.value)) {
    activeConnId.value = null
  }
})

watch(activeConnId, (id) => {
  if (id === null) localStorage.removeItem(LS_KEY)
  else localStorage.setItem(LS_KEY, String(id))
})

function handleConnSelect(id: number) {
  activeConnId.value = id
}
</script>

<template>
  <div class="app-shell">
    <TopNav
      :activeConnId="activeConnId"
      @select-conn="handleConnSelect"
    />

    <main class="main-area">
      <router-view :activeConnId="activeConnId" @set-conn="handleConnSelect" />
    </main>

    <StatusBar :activeConnId="activeConnId" />

    <ToastContainer />
    <ConfirmModal />
  </div>
</template>
