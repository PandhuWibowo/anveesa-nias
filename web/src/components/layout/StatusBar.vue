<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useConnections } from '@/composables/useConnections'
import { useTheme } from '@/composables/useTheme'

const props = defineProps<{ activeConnId: number | null }>()
const { connections } = useConnections()
const router = useRouter()
const { theme, toggleTheme } = useTheme()

const activeConn = computed(() =>
  props.activeConnId ? connections.value.find((c) => c.id === props.activeConnId) : null,
)

const driverLabel: Record<string, string> = {
  postgres: 'PostgreSQL',
  mysql: 'MySQL',
  sqlite: 'SQLite',
  mssql: 'SQL Server',
}
</script>

<template>
  <footer class="statusbar">
    <!-- Connection status -->
    <div
      class="statusbar__item statusbar__item--clickable"
      @click="router.push({ name: activeConn ? 'query' : 'connections' })"
      :title="activeConn ? `Open query editor for ${activeConn.name}` : 'Add a connection'"
    >
      <div class="statusbar__dot" :class="activeConn ? 'statusbar__dot--ok' : 'statusbar__dot--err'" />
      <span>{{ activeConn ? activeConn.name : 'No connection' }}</span>
    </div>

    <template v-if="activeConn">
      <div class="statusbar__sep" />
      <div class="statusbar__item">
        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="opacity:0.7"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5V19A9 3 0 0 0 21 19V5"/><path d="M3 12A9 3 0 0 0 21 12"/></svg>
        <span>{{ driverLabel[activeConn.driver] ?? activeConn.driver }}</span>
      </div>
      <div class="statusbar__sep" />
      <div class="statusbar__item">
        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="opacity:0.7"><rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/></svg>
        <span>{{ activeConn.host }}<template v-if="activeConn.port">:{{ activeConn.port }}</template></span>
      </div>
      <div class="statusbar__sep" />
      <div class="statusbar__item">
        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="opacity:0.7"><path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/></svg>
        <span>{{ activeConn.database }}</span>
      </div>
    </template>

    <div class="statusbar__spacer" />

    <!-- Theme toggle -->
    <button class="statusbar__theme-btn" @click="toggleTheme" :title="theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'">
      <svg v-if="theme === 'dark'" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>
      <svg v-else width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>
    </button>

    <div class="statusbar__sep" />

    <!-- Version -->
    <div class="statusbar__version">Anveesa Nias · v0.1.0</div>
  </footer>
</template>
