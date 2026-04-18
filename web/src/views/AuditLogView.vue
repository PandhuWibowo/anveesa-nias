<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import axios from 'axios'

interface AuditEntry {
  id: number
  event_type: 'query_execution' | 'feature_access'
  action: string
  target: string
  details: string
  username: string
  conn_id: number | null
  conn_name: string
  sql: string
  duration_ms: number
  row_count: number
  error: string
  executed_at: string
}

interface Stats { total: number; errors: number; avg_ms: number; query_count: number; feature_count: number }

const entries = ref<AuditEntry[]>([])
const stats = ref<Stats | null>(null)
const loading = ref(false)
const filter = ref('')
const eventType = ref<'all' | 'query_execution' | 'feature_access'>('all')
const limit = ref(200)
const expanded = ref<number | null>(null)

// Column visibility & sorting
type ColumnKey = 'time' | 'type' | 'user' | 'connection' | 'sql' | 'duration' | 'rows' | 'status'
const allColumns: ColumnKey[] = ['time', 'type', 'user', 'connection', 'sql', 'duration', 'rows', 'status']
const visibleColumns = ref<Set<ColumnKey>>(new Set(allColumns))
const showColumnMenu = ref(false)
const sortKey = ref<keyof AuditEntry | ''>('')
const sortDir = ref<'asc' | 'desc'>('desc')

const columnMap: Record<ColumnKey, { label: string; key: keyof AuditEntry }> = {
  time: { label: 'Time', key: 'executed_at' },
  type: { label: 'Type', key: 'event_type' },
  user: { label: 'User', key: 'username' },
  connection: { label: 'Connection', key: 'conn_name' },
  sql: { label: 'SQL', key: 'sql' },
  duration: { label: 'Duration', key: 'duration_ms' },
  rows: { label: 'Rows', key: 'row_count' },
  status: { label: 'Status', key: 'error' },
}

function toggleColumn(col: ColumnKey) {
  if (visibleColumns.value.has(col)) {
    visibleColumns.value.delete(col)
  } else {
    visibleColumns.value.add(col)
  }
  visibleColumns.value = new Set(visibleColumns.value)
}

function sortBy(key: keyof AuditEntry) {
  if (sortKey.value === key) {
    sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc'
  } else {
    sortKey.value = key
    sortDir.value = 'asc'
  }
}

const sortedEntries = computed(() => {
  if (!sortKey.value) return entries.value
  const sorted = [...entries.value]
  sorted.sort((a, b) => {
    const aVal = a[sortKey.value as keyof AuditEntry]
    const bVal = b[sortKey.value as keyof AuditEntry]
    if (aVal === bVal) return 0
    if (aVal == null) return sortDir.value === 'asc' ? -1 : 1
    if (bVal == null) return sortDir.value === 'asc' ? 1 : -1
    const cmp = aVal > bVal ? 1 : -1
    return sortDir.value === 'asc' ? cmp : -cmp
  })
  return sorted
})

async function load() {
  loading.value = true
  try {
    const [{ data: e }, { data: s }] = await Promise.all([
      axios.get<AuditEntry[]>('/api/admin/audit', { params: { q: filter.value || undefined, limit: limit.value, event_type: eventType.value } }),
      axios.get<Stats>('/api/admin/audit/stats'),
    ])
    entries.value = e ?? []
    stats.value = s
  } finally {
    loading.value = false
  }
}

function eventLabel(entry: AuditEntry) {
  return entry.event_type === 'feature_access' ? 'Feature Access' : 'Query Execution'
}

function eventSummary(entry: AuditEntry) {
  if (entry.event_type === 'feature_access') {
    return entry.target || entry.details || 'Feature access'
  }
  return entry.sql
}

async function clearAll() {
  if (!confirm('Clear entire audit log?')) return
  await axios.delete('/api/admin/audit')
  await load()
}

onMounted(load)
</script>

<template>
  <div class="page-shell al-root">
    <div class="page-scroll al-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Observability</div>
            <div class="page-title">Audit Log</div>
            <div class="page-subtitle">Review executed SQL, latency, errors, and operator activity across every managed connection.</div>
          </div>
          <div v-if="stats" class="page-metrics al-stats">
            <div class="page-metric al-stat"><span class="page-metric__value">{{ stats.total.toLocaleString() }}</span><span class="page-metric__label">Total</span></div>
            <div class="page-metric al-stat"><span class="page-metric__value">{{ stats.feature_count.toLocaleString() }}</span><span class="page-metric__label">Feature Access</span></div>
            <div class="page-metric al-stat"><span class="page-metric__value">{{ stats.query_count.toLocaleString() }}</span><span class="page-metric__label">Query Execution</span></div>
            <div class="page-metric al-stat"><span class="page-metric__value al-stat-err">{{ stats.errors.toLocaleString() }}</span><span class="page-metric__label">Errors</span></div>
            <div class="page-metric al-stat"><span class="page-metric__value">{{ Math.round(stats.avg_ms) }}ms</span><span class="page-metric__label">Avg Latency</span></div>
          </div>
        </section>

      <!-- Toolbar -->
      <section class="page-panel al-toolbar-wrap">
      <div class="page-toolbar al-toolbar">
        <input
          class="al-search"
          v-model="filter"
          placeholder="Filter by SQL, feature, user, or connection…"
          @keydown.enter="load"
        />
        <select class="al-limit" v-model="eventType" @change="load">
          <option value="all">All events</option>
          <option value="feature_access">Feature access</option>
          <option value="query_execution">Query execution</option>
        </select>
        <select class="al-limit" v-model="limit" @change="load">
          <option :value="100">100</option>
          <option :value="200">200</option>
          <option :value="500">500</option>
          <option :value="1000">1000</option>
        </select>
        
        <!-- Column visibility toggle -->
        <div class="col-vis-wrapper" @click.stop>
          <button class="base-btn base-btn--ghost base-btn--sm" @click="showColumnMenu = !showColumnMenu">
            <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
            Columns
          </button>
          <div v-if="showColumnMenu" class="col-vis-menu">
            <div class="col-vis-header">
              <span style="font-weight:600;font-size:11px">Column Visibility</span>
            </div>
            <div class="col-vis-list">
              <label v-for="[key, col] in Object.entries(columnMap)" :key="key" class="col-vis-item">
                <input type="checkbox" :checked="visibleColumns.has(key as ColumnKey)" @change="toggleColumn(key as ColumnKey)" />
                <span>{{ col.label }}</span>
              </label>
            </div>
          </div>
        </div>
        
        <button class="base-btn base-btn--ghost base-btn--sm" @click="load">Refresh</button>
        <button class="base-btn base-btn--ghost base-btn--sm" style="color:var(--danger)" @click="clearAll">Clear All</button>
      </div>
      </section>

      <!-- Table -->
      <section class="page-panel al-table-wrap">
        <div v-if="loading" class="al-loading">
          <svg class="spin" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
        </div>
        <table v-else class="al-table">
          <thead>
            <tr>
              <th v-if="visibleColumns.has('time')" class="al-th-sort" @click="sortBy('executed_at')">
                Time
                <span class="sort-icon">{{ sortKey === 'executed_at' ? (sortDir === 'asc' ? '↑' : '↓') : '↕' }}</span>
              </th>
              <th v-if="visibleColumns.has('user')" class="al-th-sort" @click="sortBy('username')">
                User
                <span class="sort-icon">{{ sortKey === 'username' ? (sortDir === 'asc' ? '↑' : '↓') : '↕' }}</span>
              </th>
              <th v-if="visibleColumns.has('type')" class="al-th-sort" @click="sortBy('event_type')">
                Type
                <span class="sort-icon">{{ sortKey === 'event_type' ? (sortDir === 'asc' ? '↑' : '↓') : '↕' }}</span>
              </th>
              <th v-if="visibleColumns.has('connection')" class="al-th-sort" @click="sortBy('conn_name')">
                Connection
                <span class="sort-icon">{{ sortKey === 'conn_name' ? (sortDir === 'asc' ? '↑' : '↓') : '↕' }}</span>
              </th>
              <th v-if="visibleColumns.has('sql')" class="al-th-sort" @click="sortBy('sql')">
                SQL
                <span class="sort-icon">{{ sortKey === 'sql' ? (sortDir === 'asc' ? '↑' : '↓') : '↕' }}</span>
              </th>
              <th v-if="visibleColumns.has('duration')" class="al-th-right al-th-sort" @click="sortBy('duration_ms')">
                Duration
                <span class="sort-icon">{{ sortKey === 'duration_ms' ? (sortDir === 'asc' ? '↑' : '↓') : '↕' }}</span>
              </th>
              <th v-if="visibleColumns.has('rows')" class="al-th-right al-th-sort" @click="sortBy('row_count')">
                Rows
                <span class="sort-icon">{{ sortKey === 'row_count' ? (sortDir === 'asc' ? '↑' : '↓') : '↕' }}</span>
              </th>
              <th v-if="visibleColumns.has('status')">Status</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="e in sortedEntries" :key="e.id">
              <tr class="al-row" :class="{ 'al-row--err': e.error, 'al-row--open': expanded === e.id }" @click="expanded = expanded === e.id ? null : e.id">
                <td v-if="visibleColumns.has('time')" class="al-td-dim al-td-nowrap">{{ new Date(e.executed_at).toLocaleTimeString() }}</td>
                <td v-if="visibleColumns.has('user')" class="al-td-user">{{ e.username || '—' }}</td>
                <td v-if="visibleColumns.has('type')">
                  <span class="al-badge" :class="e.event_type === 'feature_access' ? 'al-badge--feature' : 'al-badge--query'">
                    {{ eventLabel(e) }}
                  </span>
                </td>
                <td v-if="visibleColumns.has('connection')" class="al-td-dim">{{ e.conn_name || '—' }}</td>
                <td v-if="visibleColumns.has('sql')" class="al-td-sql">{{ eventSummary(e) }}</td>
                <td v-if="visibleColumns.has('duration')" class="al-td-right al-td-num">{{ e.event_type === 'query_execution' ? `${e.duration_ms}ms` : '—' }}</td>
                <td v-if="visibleColumns.has('rows')" class="al-td-right al-td-num">{{ e.event_type === 'query_execution' ? e.row_count : '—' }}</td>
                <td v-if="visibleColumns.has('status')">
                  <span class="al-badge" :class="e.error ? 'al-badge--err' : 'al-badge--ok'">
                    {{ e.error ? 'Error' : (e.event_type === 'feature_access' ? 'Viewed' : 'OK') }}
                  </span>
                </td>
              </tr>
              <tr v-if="expanded === e.id" class="al-detail-row">
                <td :colspan="visibleColumns.size">
                  <div class="al-detail">
                    <div v-if="e.error" class="al-detail-error">{{ e.error }}</div>
                    <pre class="al-detail-sql">{{ e.event_type === 'feature_access' ? `${e.action}\n${e.target}\n${e.details}`.trim() : e.sql }}</pre>
                    <div class="al-detail-meta">
                      {{ new Date(e.executed_at).toLocaleString() }}
                      <template v-if="e.event_type === 'query_execution'">
                        · {{ e.duration_ms }}ms
                        · {{ e.row_count }} rows
                      </template>
                      <template v-else>
                        · {{ e.action || 'open_feature' }}
                        <span v-if="e.target">· {{ e.target }}</span>
                      </template>
                    </div>
                  </div>
                </td>
              </tr>
            </template>
            <tr v-if="sortedEntries.length === 0">
              <td :colspan="visibleColumns.size" style="text-align:center;color:var(--text-muted);padding:24px;font-size:13px">
                No audit log entries.
              </td>
            </tr>
          </tbody>
        </table>
      </section>
      </div>
    </div>
  </div>
</template>

<style scoped>
.al-root { background: var(--bg-body); }
.al-stats { display: flex; gap: 16px; }
.al-stat-err { color: var(--danger); }
.al-loading { display: flex; align-items: center; justify-content: center; padding: 40px; color: var(--text-muted); }
.al-toolbar-wrap { padding: 14px; }
.al-toolbar { display: flex; align-items: center; gap: 8px; }
.al-search {
  flex: 1; padding: 6px 12px;
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 6px; color: var(--text-primary); font-size: 13px;
  font-family: inherit; outline: none;
}
.al-search:focus { border-color: var(--brand); }
.al-limit {
  padding: 5px 8px;
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 6px; color: var(--text-primary); font-size: 12px;
  cursor: pointer; outline: none;
}
.al-table-wrap { overflow: hidden; }
.al-table { width: 100%; border-collapse: collapse; font-size: 12.5px; }
.al-table th {
  padding: 10px 14px; background: rgba(255, 255, 255, 0.02);
  border-bottom: 1px solid var(--border);
  font-size: 10.5px; font-weight: 600; text-transform: uppercase;
  letter-spacing: 0.12em; color: var(--text-muted); text-align: left;
}
.al-th-sort {
  cursor: pointer;
  user-select: none;
  transition: color 0.12s, background 0.12s;
}
.al-th-sort:hover {
  color: var(--text-primary);
  background: var(--bg-hover);
}
.sort-icon {
  margin-left: 4px;
  font-size: 10px;
  color: var(--text-muted);
}
.al-table td { padding: 7px 14px; border-bottom: 1px solid var(--border); color: var(--text-primary); }
.al-row { cursor: pointer; transition: background 0.1s; }
.al-row:hover td { background: var(--bg-hover); }
.al-row--err td { color: var(--danger); }
.al-row--open td { background: var(--bg-hover); }
.al-td-dim { color: var(--text-muted); }
.al-td-nowrap { white-space: nowrap; }
.al-td-user { font-weight: 600; }
.al-td-sql {
  max-width: 300px; overflow: hidden; text-overflow: ellipsis;
  white-space: nowrap; font-family: var(--mono, monospace); font-size: 11.5px;
}
.al-td-right { text-align: right; }
.al-td-num { font-variant-numeric: tabular-nums; font-family: var(--mono, monospace); }
.al-th-right { text-align: right; }
.al-badge { padding: 4px 10px; border-radius: 999px; font-size: 10px; font-weight: 700; letter-spacing: 0.12em; text-transform: uppercase; }
.al-badge--ok { background: rgba(74,222,128,0.15); color: #4ade80; }
.al-badge--err { background: rgba(248,113,113,0.15); color: #f87171; }
.al-badge--feature { background: rgba(245, 158, 11, 0.15); color: #f59e0b; }
.al-badge--query { background: rgba(92, 184, 165, 0.15); color: var(--brand); }
.al-detail-row td { padding: 0 !important; }
.al-detail { padding: 12px 16px; background: var(--bg-body); border-bottom: 1px solid var(--border); }
.al-detail-error { color: var(--danger); font-size: 12px; margin-bottom: 8px; }
.al-detail-sql {
  margin: 0 0 8px; padding: 10px 12px;
  background: var(--bg-elevated); border: 1px solid var(--border); border-radius: 6px;
  font-family: var(--mono, monospace); font-size: 12px; line-height: 1.6;
  color: var(--text-primary); white-space: pre-wrap; word-break: break-all;
}
.al-detail-meta { font-size: 11px; color: var(--text-muted); }

/* Column visibility dropdown */
.col-vis-wrapper { position: relative; }
.col-vis-menu {
  position: absolute;
  top: 100%;
  right: 0;
  margin-top: 4px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 6px;
  box-shadow: 0 4px 12px rgba(0,0,0,0.15);
  min-width: 180px;
  max-height: 320px;
  display: flex;
  flex-direction: column;
  z-index: 100;
}
.col-vis-header {
  padding: 8px 10px;
  border-bottom: 1px solid var(--border);
  color: var(--text-secondary);
}
.col-vis-list {
  overflow-y: auto;
  max-height: 260px;
  padding: 4px;
}
.col-vis-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-radius: 4px;
  font-size: 12px;
  color: var(--text-primary);
  cursor: pointer;
  transition: background 0.12s;
}
.col-vis-item:hover { background: var(--bg-hover); }
.col-vis-item input[type="checkbox"] { cursor: pointer; }

@media (max-width: 900px) {
  .al-toolbar {
    flex-wrap: wrap;
  }

  .al-search {
    min-width: 100%;
  }
}
</style>
