<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import { formatServerTimestamp, parseServerTimestamp } from '@/utils/datetime'

interface NativeAccessSession {
  conn_id: number
  conn_name: string
  driver: string
  username: string
  client_addr: string
  application_name: string
  database_name: string
  session_state: string
  command: string
  duration_sec: number
  wait_event: string
  started_at: string
  query_started_at: string
  query_text: string
}

interface NativeAuditCapability {
  conn_id: number
  conn_name: string
  driver: string
  level: string
  message: string
}

interface NativeAuditResponse {
  sessions: NativeAccessSession[]
  capabilities: NativeAuditCapability[]
}

interface NativeAuditHistoryEntry {
  conn_id: number
  conn_name: string
  driver: string
  occurred_at: string
  username: string
  client_addr: string
  command_type: string
  statement: string
  thread_id: number
  database_name: string
}

interface NativeAuditHistoryNotice {
  conn_id: number
  conn_name: string
  driver: string
  level: string
  message: string
}

interface NativeAuditHistoryResponse {
  entries: NativeAuditHistoryEntry[]
  notices: NativeAuditHistoryNotice[]
}

type AuditTab = 'sessions' | 'history'

const { connections, fetchConnections } = useConnections()

const activeTab = ref<AuditTab>('sessions')
const sessions = ref<NativeAccessSession[]>([])
const capabilities = ref<NativeAuditCapability[]>([])
const historyEntries = ref<NativeAuditHistoryEntry[]>([])
const historyNotices = ref<NativeAuditHistoryNotice[]>([])
const loading = ref(false)
const autoRefresh = ref(true)
const selectedConnId = ref<number | 'all'>('all')
const search = ref('')
let timer: ReturnType<typeof setInterval>

function truncateText(value: string, max = 220): string {
  const compact = value.replace(/\s+/g, ' ').trim()
  if (compact.length <= max) return compact
  return compact.slice(0, max - 1) + '…'
}

async function load() {
  loading.value = true
  try {
    await fetchConnections()
    const params: Record<string, string | number> = {}
    if (selectedConnId.value !== 'all') params.conn_id = selectedConnId.value

    const [liveResp, historyResp] = await Promise.all([
      axios.get<NativeAuditResponse>('/api/database-audit/native', { params }),
      axios.get<NativeAuditHistoryResponse>('/api/database-audit/history/native', { params }),
    ])

    sessions.value = liveResp.data?.sessions ?? []
    capabilities.value = liveResp.data?.capabilities ?? []
    historyEntries.value = historyResp.data?.entries ?? []
    historyNotices.value = historyResp.data?.notices ?? []
  } finally {
    loading.value = false
  }
}

const filteredSessions = computed(() => {
  const term = search.value.trim().toLowerCase()
  return [...sessions.value]
    .filter((session) => {
      if (selectedConnId.value !== 'all' && session.conn_id !== selectedConnId.value) return false
      if (!term) return true
      const haystack = [
        session.conn_name,
        session.username,
        session.client_addr,
        session.application_name,
        session.database_name,
        session.query_text,
        session.session_state,
      ].join(' ').toLowerCase()
      return haystack.includes(term)
    })
    .sort((a, b) => {
      const aTs = parseServerTimestamp(a.query_started_at || a.started_at).getTime()
      const bTs = parseServerTimestamp(b.query_started_at || b.started_at).getTime()
      return bTs - aTs
    })
})

const filteredHistory = computed(() => {
  const term = search.value.trim().toLowerCase()
  return [...historyEntries.value]
    .filter((entry) => {
      if (selectedConnId.value !== 'all' && entry.conn_id !== selectedConnId.value) return false
      if (!term) return true
      const haystack = [
        entry.conn_name,
        entry.username,
        entry.client_addr,
        entry.database_name,
        entry.command_type,
        entry.statement,
      ].join(' ').toLowerCase()
      return haystack.includes(term)
    })
    .sort((a, b) => parseServerTimestamp(b.occurred_at).getTime() - parseServerTimestamp(a.occurred_at).getTime())
})

const sessionCount = computed(() => filteredSessions.value.length)
const connectionCount = computed(() => new Set(filteredSessions.value.map((s) => s.conn_id)).size)
const waitingCount = computed(() => filteredSessions.value.filter((s) => !!s.wait_event).length)
const distinctUsers = computed(() => new Set(filteredSessions.value.map((s) => `${s.conn_id}:${s.username}`)).size)
const historyCount = computed(() => filteredHistory.value.length)

onMounted(() => {
  void load()
  timer = setInterval(() => { if (autoRefresh.value) void load() }, 5000)
})
onBeforeUnmount(() => clearInterval(timer))
watch(selectedConnId, () => { void load() })
</script>

<template>
  <div class="page-shell dba-root">
    <div class="page-scroll dba-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Observability</div>
            <div class="page-title">Database Audit</div>
            <div class="page-subtitle">Trace native database sessions and outside access signals. Live sessions show who is connected now; history appears only when the database exposes audit logs through SQL.</div>
          </div>
          <div class="page-hero__actions">
            <label class="dba-auto-toggle">
              <input type="checkbox" v-model="autoRefresh" />
              Auto-refresh
            </label>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="load" :disabled="loading">
              <svg :class="loading ? 'spin' : ''" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.08-4.43"/></svg>
              Refresh
            </button>
          </div>
        </section>

        <div class="dba-stats">
          <div class="page-panel dba-stat"><div class="dba-stat__label">Sessions</div><div class="dba-stat__value">{{ sessionCount.toLocaleString() }}</div></div>
          <div class="page-panel dba-stat"><div class="dba-stat__label">History Rows</div><div class="dba-stat__value">{{ historyCount.toLocaleString() }}</div></div>
          <div class="page-panel dba-stat"><div class="dba-stat__label">Connections</div><div class="dba-stat__value">{{ connectionCount.toLocaleString() }}</div></div>
          <div class="page-panel dba-stat"><div class="dba-stat__label">Distinct Users</div><div class="dba-stat__value">{{ distinctUsers.toLocaleString() }}</div></div>
          <div class="page-panel dba-stat"><div class="dba-stat__label">Waiting</div><div class="dba-stat__value dba-stat__value--warn">{{ waitingCount.toLocaleString() }}</div></div>
        </div>

        <section class="page-panel dba-toolbar">
          <div class="dba-toolbar__grid">
            <label class="dba-field">
              <span>View</span>
              <select v-model="activeTab">
                <option value="sessions">Live sessions</option>
                <option value="history">History</option>
              </select>
            </label>
            <label class="dba-field">
              <span>Connection</span>
              <select v-model="selectedConnId">
                <option value="all">All connections</option>
                <option v-for="conn in connections" :key="conn.id" :value="conn.id">{{ conn.name }}</option>
              </select>
            </label>
            <label class="dba-field dba-field--wide">
              <span>Search</span>
              <input v-model="search" type="text" placeholder="Filter by user, host, app, database, or SQL" />
            </label>
          </div>
        </section>

        <section class="page-panel dba-capabilities">
          <div class="dba-section-title">Audit Capabilities</div>
          <div class="dba-capability-list">
            <div v-for="cap in capabilities" :key="`live-${cap.conn_id}-${cap.message}`" class="dba-capability" :class="`dba-capability--${cap.level}`">
              <strong>{{ cap.conn_name }}</strong> · {{ cap.driver }} · {{ cap.message }}
            </div>
            <div v-for="cap in historyNotices" :key="`history-${cap.conn_id}-${cap.message}`" class="dba-capability" :class="`dba-capability--${cap.level}`">
              <strong>{{ cap.conn_name }}</strong> · {{ cap.driver }} · {{ cap.message }}
            </div>
          </div>
        </section>

        <section class="dba-list" v-if="activeTab === 'sessions'">
          <div v-if="loading" class="page-panel dba-empty">Loading native session activity…</div>
          <div v-else-if="filteredSessions.length === 0" class="page-panel dba-empty">No native sessions matched the current filter.</div>

          <article v-for="session in filteredSessions" :key="`${session.conn_id}-${session.username}-${session.client_addr}-${session.query_started_at}-${session.query_text}`" class="page-panel dba-card">
            <div class="dba-card__top">
              <div class="dba-card__badges">
                <span class="dba-pill dba-pill--conn">{{ session.conn_name }}</span>
                <span class="dba-pill dba-pill--user">{{ session.username || 'unknown-user' }}</span>
                <span class="dba-pill dba-pill--host">{{ session.client_addr || 'local/hidden host' }}</span>
                <span class="dba-pill" :class="session.wait_event ? 'dba-pill--warn' : 'dba-pill--ok'">{{ session.session_state || session.command || 'active' }}</span>
              </div>
              <div class="dba-card__time">
                {{ session.query_started_at ? formatServerTimestamp(session.query_started_at) : (session.started_at ? formatServerTimestamp(session.started_at) : '—') }}
              </div>
            </div>

            <div class="dba-card__meta">
              <span v-if="session.application_name">app {{ session.application_name }}</span>
              <span v-if="session.database_name">db {{ session.database_name }}</span>
              <span>{{ session.duration_sec.toLocaleString() }}s</span>
              <span v-if="session.wait_event">wait {{ session.wait_event }}</span>
            </div>

            <div class="dba-card__sql" :title="session.query_text">{{ truncateText(session.query_text || '(no current statement exposed)') }}</div>
          </article>
        </section>

        <section class="dba-list" v-else>
          <div v-if="loading" class="page-panel dba-empty">Loading native audit history…</div>
          <div v-else-if="filteredHistory.length === 0" class="page-panel dba-empty">No native audit history matched the current filter.</div>

          <article v-for="entry in filteredHistory" :key="`${entry.conn_id}-${entry.thread_id}-${entry.occurred_at}-${entry.statement}`" class="page-panel dba-card dba-card--history">
            <div class="dba-card__top">
              <div class="dba-card__badges">
                <span class="dba-pill dba-pill--conn">{{ entry.conn_name }}</span>
                <span class="dba-pill dba-pill--user">{{ entry.username || 'unknown-user' }}</span>
                <span class="dba-pill dba-pill--host">{{ entry.client_addr || 'host unavailable' }}</span>
                <span class="dba-pill dba-pill--ok">{{ entry.command_type || 'event' }}</span>
              </div>
              <div class="dba-card__time">{{ entry.occurred_at ? formatServerTimestamp(entry.occurred_at) : '—' }}</div>
            </div>

            <div class="dba-card__meta">
              <span v-if="entry.database_name">db {{ entry.database_name }}</span>
              <span>thread {{ entry.thread_id }}</span>
            </div>

            <div class="dba-card__sql" :title="entry.statement">{{ truncateText(entry.statement || '(no statement text)') }}</div>
          </article>
        </section>
      </div>
    </div>
  </div>
</template>

<style scoped>
.dba-root { background: var(--bg-body); }
.dba-auto-toggle { display:flex; align-items:center; gap:6px; font-size:12px; color:var(--text-muted); cursor:pointer; }
.dba-stats {
  display:grid;
  grid-template-columns:repeat(auto-fit, minmax(140px, 1fr));
  gap:12px;
}
.dba-stat { padding:14px 16px; }
.dba-stat__label {
  font-size:11px; font-weight:700; text-transform:uppercase; letter-spacing:0.35px; color:var(--text-muted);
}
.dba-stat__value {
  margin-top:6px; font-size:24px; font-weight:700; color:var(--text-primary); font-variant-numeric:tabular-nums;
}
.dba-stat__value--warn { color:#d97706; }
.dba-toolbar, .dba-capabilities { padding:16px; }
.dba-toolbar__grid {
  display:grid; grid-template-columns:repeat(auto-fit, minmax(180px, 1fr)); gap:12px;
}
.dba-field { display:flex; flex-direction:column; gap:6px; }
.dba-field--wide { grid-column:span 2; }
.dba-field span, .dba-section-title {
  font-size:11px; font-weight:700; text-transform:uppercase; letter-spacing:0.35px; color:var(--text-muted);
}
.dba-field select, .dba-field input {
  height:38px; border-radius:8px; border:1px solid var(--border); background:var(--bg-panel); color:var(--text-primary); padding:0 12px; font-size:13px;
}
.dba-capability-list {
  display:flex; flex-direction:column; gap:8px; margin-top:12px;
}
.dba-capability {
  padding:10px 12px; border-radius:8px; border:1px solid var(--border); background:var(--bg-panel); font-size:12px; color:var(--text-secondary);
}
.dba-capability--info { border-left:3px solid var(--brand); }
.dba-capability--warning { border-left:3px solid #f59e0b; }
.dba-capability--error { border-left:3px solid #ef4444; }
.dba-capability--unsupported { border-left:3px solid var(--text-muted); }
.dba-list { display:flex; flex-direction:column; gap:12px; }
.dba-card {
  padding:16px; display:flex; flex-direction:column; gap:10px; border-left:3px solid var(--brand);
}
.dba-card--history { border-left-color:#0ea5e9; }
.dba-card__top {
  display:flex; align-items:center; justify-content:space-between; gap:10px;
}
.dba-card__badges {
  display:flex; flex-wrap:wrap; gap:8px;
}
.dba-pill {
  display:inline-flex; align-items:center; border-radius:999px; padding:4px 8px; font-size:11px; font-weight:700; letter-spacing:0.2px;
}
.dba-pill--conn { background:var(--brand-dim); color:var(--brand); }
.dba-pill--user { background:var(--bg-hover); color:var(--text-secondary); }
.dba-pill--host { background:rgba(14,165,233,0.12); color:#0369a1; }
.dba-pill--warn { background:rgba(245,158,11,0.14); color:#b45309; }
.dba-pill--ok { background:rgba(16,185,129,0.14); color:#047857; }
.dba-card__time, .dba-card__meta {
  font-size:12px; color:var(--text-muted); font-variant-numeric:tabular-nums;
}
.dba-card__meta {
  display:flex; flex-wrap:wrap; gap:12px;
}
.dba-card__sql {
  font-size:13px; line-height:1.6; color:var(--text-primary); font-family:var(--mono, monospace); word-break:break-word;
}
.dba-empty {
  padding:28px; text-align:center; color:var(--text-muted);
}
@media (max-width: 840px) {
  .dba-field--wide { grid-column:span 1; }
  .dba-card__top {
    align-items:flex-start; flex-direction:column;
  }
}
</style>
