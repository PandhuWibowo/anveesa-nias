<script setup lang="ts">
import { ref, watch, onMounted, onBeforeUnmount, computed } from 'vue'
import DataSessionPane from '@/components/database/DataSessionPane.vue'
import { useConnections } from '@/composables/useConnections'
import { useTheme } from '@/composables/useTheme'
import { pendingSQL } from '@/composables/usePendingSQL'

const props = defineProps<{ activeConnId?: number | null }>()

const { connections } = useConnections()
const { mode } = useTheme()

interface Session {
  id: string
  connId: number | null
  initialSQL: string | null
  initialDb?: string
  initialTable?: string
  initialTab?: 'data' | 'explorer'
}

interface PersistedSession { connId: number; db?: string; table?: string; tab?: 'data' | 'explorer' }
interface PersistedState { sessions: PersistedSession[]; activeConnId: number | null }

const LS_KEY = 'dv_state'

function loadPersistedState(): PersistedState | null {
  try { return JSON.parse(localStorage.getItem(LS_KEY) ?? 'null') } catch { return null }
}

function persistState() {
  const state: PersistedState = {
    sessions: sessions.value
      .filter(s => s.connId != null)
      .map(s => ({ connId: s.connId as number, db: s.initialDb, table: s.initialTable, tab: s.initialTab })),
    activeConnId: sessions.value.find(s => s.id === activeSessionId.value)?.connId ?? null,
  }
  localStorage.setItem(LS_KEY, JSON.stringify(state))
}

const persisted = loadPersistedState()

let sessionCounter = 0
const sessions = ref<Session[]>(
  (persisted?.sessions ?? []).map(s => ({
    id: `session-${++sessionCounter}`,
    connId: s.connId,
    initialSQL: null,
    initialDb: s.db,
    initialTable: s.table,
    initialTab: s.tab,
  }))
)
const activeSessionId = ref<string>(
  persisted?.activeConnId
    ? (sessions.value.find(s => s.connId === persisted!.activeConnId)?.id ?? sessions.value[0]?.id ?? '')
    : (sessions.value[0]?.id ?? '')
)

watch(sessions, persistState, { deep: true })
watch(activeSessionId, persistState)

function sessionLabel(s: Session) {
  const conn = s.connId ? connections.value.find(c => c.id === s.connId) : null
  return conn?.name ?? 'No connection'
}

function driverColor(s: Session) {
  const conn = s.connId ? connections.value.find(c => c.id === s.connId) : null
  const map: Record<string, string> = { postgres: '#336791', mysql: '#f29111', mariadb: '#c0392b', mssql: '#cc2927' }
  return conn ? (map[conn.driver] ?? '#888') : '#888'
}

function driverLabel(s: Session) {
  const conn = s.connId ? connections.value.find(c => c.id === s.connId) : null
  const map: Record<string, string> = { postgres: 'PG', mysql: 'MY', mariadb: 'MB', mssql: 'MS' }
  return conn ? (map[conn.driver] ?? '??') : '??'
}

function openSession(connId: number | null, initialSQL?: string | null) {
  const id = `session-${++sessionCounter}`
  sessions.value.push({ id, connId: connId, initialSQL: initialSQL ?? null })
  activeSessionId.value = id
}

function closeSession(id: string) {
  const idx = sessions.value.findIndex(s => s.id === id)
  if (idx === -1) return
  sessions.value.splice(idx, 1)
  if (activeSessionId.value === id) {
    activeSessionId.value = sessions.value[Math.max(0, idx - 1)]?.id ?? ''
  }
}

// Picker for new sessions
const pickerOpen = ref(false)
const pickerSearch = ref('')
const pickerRef = ref<HTMLElement | null>(null)
const addBtnRef = ref<HTMLButtonElement | null>(null)
const pickerPos = ref({ top: 0, left: 0 })

const pickerStyle = computed(() => ({
  position: 'fixed' as const,
  top: pickerPos.value.top + 'px',
  left: pickerPos.value.left + 'px',
  zIndex: 9999,
}))

const filteredConns = computed(() =>
  pickerSearch.value
    ? connections.value.filter(c => c.name.toLowerCase().includes(pickerSearch.value.toLowerCase()))
    : connections.value
)

function togglePicker() {
  if (pickerOpen.value) { pickerOpen.value = false; return }
  if (addBtnRef.value) {
    const r = addBtnRef.value.getBoundingClientRect()
    const dropW = 280
    // Right-align to the button when it would overflow the right edge
    const left = (r.right + dropW > window.innerWidth - 8)
      ? Math.max(8, r.right - dropW)
      : r.left
    pickerPos.value = { top: r.bottom + 4, left }
  }
  pickerOpen.value = true
}

function pickConn(connId: number) {
  openSession(connId)
  pickerOpen.value = false
  pickerSearch.value = ''
}

// Close picker when clicking outside
function onDocClick(e: MouseEvent) {
  if (!pickerOpen.value) return
  const target = e.target as Node
  if (!pickerRef.value?.contains(target) && !addBtnRef.value?.contains(target)) {
    pickerOpen.value = false
  }
}
onMounted(() => document.addEventListener('mousedown', onDocClick, true))
onBeforeUnmount(() => document.removeEventListener('mousedown', onDocClick, true))

// Bootstrap or switch session when the global active connection changes
watch(() => props.activeConnId, (id) => {
  if (id == null) return
  const existing = sessions.value.find(s => s.connId === id)
  if (existing) {
    // Session already open — just bring it to focus
    activeSessionId.value = existing.id
  } else {
    openSession(id)
  }
}, { immediate: true })

// Handle pending SQL from Saved Queries
onMounted(() => {
  if (pendingSQL.value) {
    const sql = pendingSQL.value
    pendingSQL.value = null
    if (sessions.value.length > 0) {
      // Inject into the active session's pane via initial SQL on a new session
      const activeSession = sessions.value.find(s => s.id === activeSessionId.value)
      if (activeSession) {
        activeSession.initialSQL = sql
      }
    } else {
      openSession(props.activeConnId ?? null, sql)
    }
  }
})

function onPickerKeydown(e: KeyboardEvent) { if (e.key === 'Escape') pickerOpen.value = false }

function handleTableSelected(sessionId: string, db: string, table: string) {
  const session = sessions.value.find(s => s.id === sessionId)
  if (session) { session.initialDb = db; session.initialTable = table }
}
</script>

<template>
  <div style="display:flex;flex-direction:column;width:100%;height:100%;min-height:0;overflow:hidden">

    <!-- Session tab bar -->
    <div class="sess-bar">
      <!-- Scrollable session tabs (overflow:auto here clips nothing — + DB is outside) -->
      <div class="sess-tabs">
        <button
          v-for="s in sessions"
          :key="s.id"
          class="sess-tab"
          :class="{ 'sess-tab--active': activeSessionId === s.id }"
          @click="activeSessionId = s.id"
        >
          <span class="sess-tab__badge" :style="{ background: driverColor(s) + '33', color: driverColor(s) }">
            {{ driverLabel(s) }}
          </span>
          <span class="sess-tab__label">{{ sessionLabel(s) }}</span>
          <span class="sess-tab__close" @click.stop="closeSession(s.id)">×</span>
        </button>
      </div>

      <!-- + DB button lives OUTSIDE sess-tabs so the dropdown is never clipped -->
      <div class="sess-add-wrap">
        <button ref="addBtnRef" class="sess-add" @click="togglePicker" title="Open another database">
          <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
          DB
        </button>

        <!-- Connection picker dropdown — uses fixed positioning so nothing clips it -->
        <Teleport to="body">
          <div v-if="pickerOpen" ref="pickerRef" class="sess-picker" :style="pickerStyle" @keydown="onPickerKeydown">
            <div class="sess-picker__header">Open a connection</div>
            <div style="padding:6px 10px">
              <input v-model="pickerSearch" class="base-input" placeholder="Search…" style="width:100%;font-size:12px" autofocus />
            </div>
            <div class="sess-picker__list">
              <button
                v-for="conn in filteredConns"
                :key="conn.id"
                class="sess-picker__item"
                @click="pickConn(conn.id)"
              >
                <span class="sess-picker__badge" :style="{ background: ({'postgres':'#336791','mysql':'#f29111','mariadb':'#c0392b','mssql':'#cc2927'} as Record<string,string>)[conn.driver] ?? '#888' }">
                  {{ ({'postgres':'PG','mysql':'MY','mariadb':'MB','mssql':'MS'} as Record<string,string>)[conn.driver] ?? '??' }}
                </span>
                <div>
                  <div style="font-size:13px;font-weight:600;color:var(--text-primary)">{{ conn.name }}</div>
                  <div style="font-size:11px;color:var(--text-muted);font-family:var(--mono,monospace)">{{ conn.host }}{{ conn.port ? ':' + conn.port : '' }}</div>
                </div>
              </button>
              <div v-if="filteredConns.length === 0" style="padding:16px;text-align:center;font-size:12px;color:var(--text-muted)">No connections found</div>
            </div>
          </div>
        </Teleport>
      </div>
    </div>

    <!-- Session panes (kept alive while hidden) -->
    <div v-if="sessions.length === 0" class="data-empty-state">
      <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" style="opacity:0.3"><path d="M18.36 6.64a9 9 0 1 1-12.73 0"/><line x1="12" y1="2" x2="12" y2="12"/></svg>
      <div class="data-empty-state__text">
        <p style="font-size:14px;font-weight:600;margin:0;color:var(--text-primary)">No database sessions open</p>
        <p style="font-size:13px;margin:6px 0 0;color:var(--text-muted)">Click <strong style="color:var(--brand)">+ DB</strong> to open a database connection</p>
      </div>
    </div>

    <template v-for="s in sessions" :key="s.id">
      <div v-show="activeSessionId === s.id" style="flex:1;min-height:0;overflow:hidden;display:flex;flex-direction:column">
        <KeepAlive>
          <DataSessionPane
            :key="s.id"
            :conn-id="s.connId"
            :dark-mode="mode === 'dark'"
            :initial-s-q-l="s.initialSQL"
            :initial-db="s.initialDb"
            :initial-table="s.initialTable"
            @table-selected="(db, table) => handleTableSelected(s.id, db, table)"
          />
        </KeepAlive>
      </div>
    </template>
  </div>
</template>

<style scoped>
/* ── Session tab bar ─────────────────────────────────────────────── */
.sess-bar {
  display: flex;
  align-items: center;
  background: var(--bg-surface);
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
  min-height: 42px;
  overflow: visible;
  position: relative;
}

.sess-tabs {
  display: flex;
  align-items: stretch;
  flex: 1;
  overflow-x: auto;
  scrollbar-width: none;
  gap: 2px;
  padding: 0 4px;
}
.sess-tabs::-webkit-scrollbar { display: none; }

.sess-tab {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 0 15px;
  min-width: 0;
  max-width: 240px;
  height: 38px;
  border: none;
  background: transparent;
  color: var(--text-muted);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  white-space: nowrap;
  transition: all .15s ease;
  border-radius: 6px;
  flex-shrink: 0;
  position: relative;
}
.sess-tab:hover { color: var(--text-primary); background: var(--bg-elevated); }
.sess-tab--active { color: var(--text-primary); background: var(--bg-body); box-shadow: 0 1px 3px rgba(0,0,0,.06), inset 0 0 0 1px var(--border); }

.sess-tab__badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 18px;
  border-radius: 4px;
  font-size: 9px;
  font-weight: 700;
  flex-shrink: 0;
  letter-spacing: 0.3px;
}

.sess-tab__label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.sess-tab__close {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  border-radius: 3px;
  font-size: 15px;
  color: var(--text-muted);
  line-height: 1;
  flex-shrink: 0;
  transition: background .1s, color .1s;
}
.sess-tab__close:hover { background: var(--bg-elevated); color: var(--text-primary); }

.sess-add {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 15px;
  height: 38px;
  border: none;
  background: transparent;
  color: var(--brand);
  font-size: 12.5px;
  font-weight: 700;
  cursor: pointer;
  white-space: nowrap;
  transition: all .15s ease;
  border-radius: 6px;
  margin-left: 4px;
}
.sess-add:hover { background: var(--brand-dim); }

/* ── + DB wrapper (outside scroll container) ────────────────────── */
.sess-add-wrap {
  flex-shrink: 0;
  position: relative;
  border-left: 1px solid var(--border);
}

/* ── Connection picker dropdown (teleported to body) ────────────── */
.sess-picker {
  width: 300px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: 0 16px 48px rgba(0,0,0,.35), 0 4px 16px rgba(0,0,0,.25);
  overflow: hidden;
}

.sess-picker__header {
  padding: 12px 14px 8px;
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.6px;
  color: var(--text-muted);
  background: color-mix(in srgb, var(--bg-surface) 50%, transparent);
}

.sess-picker__list {
  max-height: 280px;
  overflow-y: auto;
  padding: 6px;
}

.sess-picker__item {
  display: flex;
  align-items: center;
  gap: 11px;
  width: 100%;
  padding: 10px 12px;
  background: none;
  border: none;
  border-radius: 7px;
  cursor: pointer;
  text-align: left;
  transition: all .12s ease;
}
.sess-picker__item:hover { background: var(--bg-surface); transform: translateX(2px); }

.sess-picker__badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 22px;
  border-radius: 4px;
  font-size: 10px;
  font-weight: 700;
  color: #fff;
  flex-shrink: 0;
}

/* ── Empty State ──────────────────────────────────────────────── */
.data-empty-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 16px;
  color: var(--text-muted);
  padding: 40px;
  text-align: center;
}
.data-empty-state__text {
  max-width: 400px;
}
</style>
