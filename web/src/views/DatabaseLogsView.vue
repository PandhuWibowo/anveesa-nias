<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'

// ── Types ────────────────────────────────────────────────────────────

interface SlowQueryRow {
  query_id: string
  query: string
  statement_type: string
  database: string
  username: string
  calls: number
  avg_ms: number
  min_ms: number
  max_ms: number
  total_ms: number
  rows: number
}

interface SlowQueryResponse {
  rows: SlowQueryRow[]
  total: number
  threshold_ms: number
  source: string
  notice?: string
}

interface ErrorLogRow {
  log_time: string
  severity: string
  message: string
  detail: string
  hint: string
  query: string
  username: string
  database_name: string
  app_name: string
  remote_host: string
  sql_state: string
}

interface ErrorLogResponse {
  rows: ErrorLogRow[]
  total: number
  source: string
  notice?: string
}

// ── Connections ──────────────────────────────────────────────────────

const { connections, fetchConnections } = useConnections()
const pgConnections = computed(() => connections.value.filter(c => c.driver === 'postgres'))
const selectedConnId = ref<number | null>(null)

// ── Tab state ────────────────────────────────────────────────────────

type Tab = 'slow' | 'error' | 'audit'
const activeTab = ref<Tab>('slow')

// ── Slow Query state ─────────────────────────────────────────────────

const slowRows = ref<SlowQueryRow[]>([])
const slowTotal = ref(0)
const slowSource = ref('')
const slowNotice = ref('')
const slowLoading = ref(false)
const slowThreshold = ref(1000)
const slowPage = ref(1)
const slowLimit = ref(25)
const slowDbFilter = ref('')
const slowUserFilter = ref('')
const slowStmtFilter = ref('')
const selectedSlow = ref<SlowQueryRow | null>(null)

const slowFiltered = computed(() => {
  if (!slowStmtFilter.value) return slowRows.value
  return slowRows.value.filter(r => r.statement_type === slowStmtFilter.value)
})

const slowTotalPages = computed(() => Math.max(1, Math.ceil(slowTotal.value / slowLimit.value)))

async function loadSlowQueries() {
  if (!selectedConnId.value) return
  slowLoading.value = true
  slowNotice.value = ''
  try {
    const { data } = await axios.get<SlowQueryResponse>(
      `/api/connections/${selectedConnId.value}/db-logs/slow-queries`,
      {
        params: {
          threshold_ms: slowThreshold.value,
          limit: slowLimit.value,
          page: slowPage.value,
          db: slowDbFilter.value || undefined,
          user: slowUserFilter.value || undefined,
        },
      },
    )
    slowRows.value = data.rows ?? []
    slowTotal.value = data.total ?? 0
    slowSource.value = data.source ?? ''
    slowNotice.value = data.notice ?? ''
  } catch (e: any) {
    slowNotice.value = e?.response?.data?.error || e?.message || 'Failed to load slow queries'
    slowRows.value = []
  } finally {
    slowLoading.value = false
  }
}

function slowGoPage(p: number) {
  slowPage.value = p
  loadSlowQueries()
}

function slowApply() {
  slowPage.value = 1
  loadSlowQueries()
}

// ── Error Log state ──────────────────────────────────────────────────

const errorRows = ref<ErrorLogRow[]>([])
const errorTotal = ref(0)
const errorSource = ref('')
const errorNotice = ref('')
const errorLoading = ref(false)
const errorPage = ref(1)
const errorLimit = ref(50)
const errorLevels = ref<string[]>([])
const errorFrom = ref('')
const errorTo = ref('')
const selectedError = ref<ErrorLogRow | null>(null)

const ERROR_LEVELS = ['ERROR', 'FATAL', 'WARNING', 'CONTEXT', 'STATEMENT', 'LOG']
const errorTotalPages = computed(() => Math.max(1, Math.ceil(errorTotal.value / errorLimit.value)))

function toggleLevel(l: string) {
  if (errorLevels.value.includes(l)) {
    errorLevels.value = errorLevels.value.filter(x => x !== l)
  } else {
    errorLevels.value.push(l)
  }
}

async function loadErrorLogs() {
  if (!selectedConnId.value) return
  errorLoading.value = true
  errorNotice.value = ''
  try {
    const { data } = await axios.get<ErrorLogResponse>(
      `/api/connections/${selectedConnId.value}/db-logs/error-logs`,
      {
        params: {
          limit: errorLimit.value,
          page: errorPage.value,
          level: errorLevels.value.length ? errorLevels.value.join(',') : undefined,
          from: errorFrom.value || undefined,
          to: errorTo.value || undefined,
        },
      },
    )
    errorRows.value = data.rows ?? []
    errorTotal.value = data.total ?? 0
    errorSource.value = data.source ?? ''
    errorNotice.value = data.notice ?? ''
  } catch (e: any) {
    errorNotice.value = e?.response?.data?.error || e?.message || 'Failed to load error logs'
    errorRows.value = []
  } finally {
    errorLoading.value = false
  }
}

function errorGoPage(p: number) {
  errorPage.value = p
  loadErrorLogs()
}

function errorApply() {
  errorPage.value = 1
  loadErrorLogs()
}

// ── Audit Log state ──────────────────────────────────────────────────

interface AuditRow {
  id: number
  conn_id: number
  sql: string
  executed_at: string
  duration_ms: number
  rows_affected: number
  status: string
  error?: string
}

const auditRows = ref<AuditRow[]>([])
const auditLoading = ref(false)
const auditNotice = ref('')
const auditPage = ref(1)
const auditLimit = ref(50)
const auditTotal = ref(0)
const auditSearch = ref('')
const selectedAudit = ref<AuditRow | null>(null)
const auditTotalPages = computed(() => Math.max(1, Math.ceil(auditTotal.value / auditLimit.value)))

async function loadAuditLogs() {
  if (!selectedConnId.value) return
  auditLoading.value = true
  auditNotice.value = ''
  try {
    const { data } = await axios.get(`/api/connections/${selectedConnId.value}/history`, {
      params: {
        limit: auditLimit.value,
        offset: (auditPage.value - 1) * auditLimit.value,
        search: auditSearch.value || undefined,
      },
    })
    const rows = Array.isArray(data) ? data : (data.rows ?? data.history ?? [])
    auditRows.value = rows
    auditTotal.value = data.total ?? rows.length
  } catch (e: any) {
    auditNotice.value = e?.response?.data?.error || e?.message || 'Failed to load audit log'
    auditRows.value = []
  } finally {
    auditLoading.value = false
  }
}

function auditGoPage(p: number) {
  auditPage.value = p
  loadAuditLogs()
}

// ── Tab switch ───────────────────────────────────────────────────────

function switchTab(t: Tab) {
  activeTab.value = t
  if (!selectedConnId.value) return
  if (t === 'slow') loadSlowQueries()
  else if (t === 'error') loadErrorLogs()
  else loadAuditLogs()
}

watch(selectedConnId, (id) => {
  if (!id) return
  selectedSlow.value = null
  selectedError.value = null
  selectedAudit.value = null
  if (activeTab.value === 'slow') loadSlowQueries()
  else if (activeTab.value === 'error') loadErrorLogs()
  else loadAuditLogs()
})

onMounted(async () => {
  await fetchConnections()
  if (pgConnections.value.length) {
    selectedConnId.value = pgConnections.value[0].id
  }
})

// ── Helpers ──────────────────────────────────────────────────────────

function fmtMs(ms: number): string {
  if (ms >= 1000) return (ms / 1000).toFixed(2) + 's'
  return ms.toFixed(0) + 'ms'
}

function fmtNum(n: number): string {
  return n.toLocaleString()
}

function severityClass(s: string): string {
  switch (s?.toUpperCase()) {
    case 'FATAL': return 'sev-fatal'
    case 'ERROR': return 'sev-error'
    case 'WARNING': return 'sev-warning'
    case 'CONTEXT': return 'sev-context'
    case 'STATEMENT': return 'sev-stmt'
    default: return 'sev-info'
  }
}

function stmtClass(t: string): string {
  switch (t) {
    case 'SELECT': return 'stmt-select'
    case 'INSERT': return 'stmt-insert'
    case 'UPDATE': return 'stmt-update'
    case 'DELETE': return 'stmt-delete'
    default: return 'stmt-other'
  }
}

function truncate(s: string, n = 120): string {
  if (!s) return ''
  return s.length > n ? s.slice(0, n) + '…' : s
}

function today(): string {
  return new Date().toISOString().slice(0, 10)
}

function yesterday(): string {
  const d = new Date()
  d.setDate(d.getDate() - 1)
  return d.toISOString().slice(0, 10)
}
</script>

<template>
  <div class="dblogs">
    <!-- Header -->
    <div class="dblogs-header">
      <div class="dblogs-header__left">
        <div class="dblogs-header__tag">DATABASE LOGS</div>
        <h1 class="dblogs-header__title">DB Logs</h1>
        <p class="dblogs-header__sub">Slow queries, error events, and SQL audit trail for your PostgreSQL connections.</p>
      </div>
      <div class="dblogs-header__right">
        <select v-model="selectedConnId" class="dblogs-conn-select" @change="() => {}">
          <option :value="null" disabled>Select connection…</option>
          <option v-for="c in pgConnections" :key="c.id" :value="c.id">{{ c.name }}</option>
        </select>
      </div>
    </div>

    <!-- No PG connections -->
    <div v-if="pgConnections.length === 0" class="dblogs-empty">
      <svg width="36" height="36" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5v14c0 1.657 4.03 3 9 3s9-1.343 9-3V5"/><path d="M3 12c0 1.657 4.03 3 9 3s9-1.343 9-3"/></svg>
      <p>No PostgreSQL connections found.</p>
      <p class="dblogs-muted">Create a PostgreSQL connection in Admin → Connections first.</p>
    </div>

    <template v-else>
      <!-- Tabs -->
      <div class="dblogs-tabs">
        <button class="dblogs-tab" :class="{ active: activeTab === 'error' }" @click="switchTab('error')">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
          Error Logs
        </button>
        <button class="dblogs-tab" :class="{ active: activeTab === 'slow' }" @click="switchTab('slow')">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
          Slow Query Logs
        </button>
        <button class="dblogs-tab" :class="{ active: activeTab === 'audit' }" @click="switchTab('audit')">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/></svg>
          SQL Audit Logs
        </button>
      </div>

      <!-- ── ERROR LOGS ───────────────────────────────────────── -->
      <div v-if="activeTab === 'error'" class="dblogs-panel">
        <!-- Filters -->
        <div class="dblogs-toolbar">
          <div class="dblogs-toolbar__group">
            <span class="dblogs-label">Level</span>
            <div class="dblogs-pills">
              <button
                v-for="lv in ERROR_LEVELS" :key="lv"
                class="dblogs-pill" :class="[severityClass(lv), { active: errorLevels.includes(lv) }]"
                @click="toggleLevel(lv)"
              >{{ lv }}</button>
            </div>
          </div>
          <div class="dblogs-toolbar__group">
            <span class="dblogs-label">From</span>
            <input v-model="errorFrom" type="date" class="dblogs-input" />
          </div>
          <div class="dblogs-toolbar__group">
            <span class="dblogs-label">To</span>
            <input v-model="errorTo" type="date" class="dblogs-input" />
          </div>
          <div class="dblogs-toolbar__group">
            <span class="dblogs-label">Per page</span>
            <select v-model="errorLimit" class="dblogs-select-sm">
              <option :value="25">25</option>
              <option :value="50">50</option>
              <option :value="100">100</option>
            </select>
          </div>
          <div class="dblogs-toolbar__spacer" />
          <button class="dblogs-btn" @click="errorApply">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
            Apply
          </button>
          <div v-if="errorSource" class="dblogs-source">source: {{ errorSource }}</div>
        </div>

        <!-- Today / Yesterday shortcuts -->
        <div class="dblogs-shortcuts">
          <button class="dblogs-shortcut" @click="errorFrom = today(); errorTo = today(); errorApply()">Today</button>
          <button class="dblogs-shortcut" @click="errorFrom = yesterday(); errorTo = yesterday(); errorApply()">Yesterday</button>
          <button class="dblogs-shortcut" @click="errorFrom = ''; errorTo = ''; errorApply()">All time</button>
        </div>

        <!-- Notice -->
        <div v-if="errorNotice" class="dblogs-notice">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
          {{ errorNotice }}
        </div>

        <!-- Loading -->
        <div v-else-if="errorLoading" class="dblogs-loading">
          <svg class="spin" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
          Loading error logs…
        </div>

        <!-- Empty -->
        <div v-else-if="!errorLoading && !errorRows.length" class="dblogs-empty-sm">
          No error log entries found for the selected filters.
          <span v-if="!errorSource" class="dblogs-muted">Click Apply to load.</span>
        </div>

        <!-- Table -->
        <template v-else>
          <div class="dblogs-table-wrap">
            <table class="dblogs-table">
              <thead>
                <tr>
                  <th style="width:160px">Time</th>
                  <th style="width:100px">Level</th>
                  <th style="width:100px">Database</th>
                  <th style="width:100px">Username</th>
                  <th>Description</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="(row, i) in errorRows" :key="i"
                  :class="{ selected: selectedError === row }"
                  @click="selectedError = selectedError === row ? null : row"
                >
                  <td class="dblogs-mono">{{ row.log_time }}</td>
                  <td><span class="dblogs-badge" :class="severityClass(row.severity)">{{ row.severity }}</span></td>
                  <td class="dblogs-muted-cell">{{ row.database_name }}</td>
                  <td class="dblogs-muted-cell">{{ row.username }}</td>
                  <td class="dblogs-msg">{{ truncate(row.message, 200) }}</td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Row detail -->
          <div v-if="selectedError" class="dblogs-detail">
            <div class="dblogs-detail__header">
              <span class="dblogs-badge" :class="severityClass(selectedError.severity)">{{ selectedError.severity }}</span>
              <span class="dblogs-detail__time">{{ selectedError.log_time }}</span>
              <button class="dblogs-detail__close" @click="selectedError = null">✕</button>
            </div>
            <div class="dblogs-detail__body">
              <div v-if="selectedError.message" class="dblogs-detail__section">
                <div class="dblogs-detail__section-label">Message</div>
                <pre class="dblogs-detail__pre">{{ selectedError.message }}</pre>
              </div>
              <div v-if="selectedError.detail" class="dblogs-detail__section">
                <div class="dblogs-detail__section-label">Detail</div>
                <pre class="dblogs-detail__pre">{{ selectedError.detail }}</pre>
              </div>
              <div v-if="selectedError.hint" class="dblogs-detail__section">
                <div class="dblogs-detail__section-label">Hint</div>
                <pre class="dblogs-detail__pre dblogs-hint">{{ selectedError.hint }}</pre>
              </div>
              <div v-if="selectedError.query" class="dblogs-detail__section">
                <div class="dblogs-detail__section-label">Query</div>
                <pre class="dblogs-detail__pre dblogs-query">{{ selectedError.query }}</pre>
              </div>
              <div class="dblogs-detail__meta">
                <span v-if="selectedError.database_name"><strong>Database:</strong> {{ selectedError.database_name }}</span>
                <span v-if="selectedError.username"><strong>User:</strong> {{ selectedError.username }}</span>
                <span v-if="selectedError.app_name"><strong>App:</strong> {{ selectedError.app_name }}</span>
                <span v-if="selectedError.remote_host"><strong>Host:</strong> {{ selectedError.remote_host }}</span>
                <span v-if="selectedError.sql_state"><strong>SQL State:</strong> {{ selectedError.sql_state }}</span>
              </div>
            </div>
          </div>

          <!-- Pagination -->
          <div class="dblogs-pager">
            <span class="dblogs-pager__info">Page {{ errorPage }} / {{ errorTotalPages }}</span>
            <button class="dblogs-pager__btn" :disabled="errorPage <= 1" @click="errorGoPage(errorPage - 1)">‹ Prev</button>
            <button class="dblogs-pager__btn" :disabled="errorPage >= errorTotalPages" @click="errorGoPage(errorPage + 1)">Next ›</button>
          </div>
        </template>
      </div>

      <!-- ── SLOW QUERY LOGS ─────────────────────────────────── -->
      <div v-else-if="activeTab === 'slow'" class="dblogs-panel">
        <!-- Filters -->
        <div class="dblogs-toolbar">
          <div class="dblogs-toolbar__group">
            <span class="dblogs-label">Threshold</span>
            <div class="dblogs-threshold">
              <input v-model.number="slowThreshold" type="range" min="0" max="60000" step="100" class="dblogs-slider" />
              <span class="dblogs-threshold__val">{{ fmtMs(slowThreshold) }}</span>
            </div>
          </div>
          <div class="dblogs-toolbar__group">
            <span class="dblogs-label">Database</span>
            <input v-model="slowDbFilter" type="text" placeholder="all" class="dblogs-input dblogs-input--sm" />
          </div>
          <div class="dblogs-toolbar__group">
            <span class="dblogs-label">Username</span>
            <input v-model="slowUserFilter" type="text" placeholder="all" class="dblogs-input dblogs-input--sm" />
          </div>
          <div class="dblogs-toolbar__group">
            <span class="dblogs-label">Type</span>
            <select v-model="slowStmtFilter" class="dblogs-select-sm">
              <option value="">All types</option>
              <option>SELECT</option><option>INSERT</option><option>UPDATE</option>
              <option>DELETE</option><option>CREATE</option><option>ALTER</option><option>DROP</option>
            </select>
          </div>
          <div class="dblogs-toolbar__group">
            <span class="dblogs-label">Per page</span>
            <select v-model="slowLimit" class="dblogs-select-sm">
              <option :value="25">25</option>
              <option :value="50">50</option>
              <option :value="100">100</option>
            </select>
          </div>
          <div class="dblogs-toolbar__spacer" />
          <button class="dblogs-btn" @click="slowApply">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
            Apply
          </button>
          <div v-if="slowSource" class="dblogs-source">source: {{ slowSource }}</div>
        </div>

        <!-- Notice -->
        <div v-if="slowNotice" class="dblogs-notice">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
          {{ slowNotice }}
        </div>

        <!-- Loading -->
        <div v-else-if="slowLoading" class="dblogs-loading">
          <svg class="spin" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
          Loading slow queries…
        </div>

        <!-- Empty -->
        <div v-else-if="!slowLoading && !slowFiltered.length" class="dblogs-empty-sm">
          <template v-if="!slowSource">Click <strong>Apply</strong> to load slow queries.</template>
          <template v-else>No queries exceeded the {{ fmtMs(slowThreshold) }} threshold. Try lowering it.</template>
        </div>

        <!-- Table -->
        <template v-else>
          <div class="dblogs-table-wrap">
            <table class="dblogs-table">
              <thead>
                <tr>
                  <th style="width:90px">Type</th>
                  <th>Execute Statement</th>
                  <th style="width:80px">Calls</th>
                  <th style="width:90px">Avg Time</th>
                  <th style="width:90px">Max Time</th>
                  <th style="width:80px">Rows</th>
                  <th style="width:110px">Database</th>
                  <th style="width:110px">Username</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="(row, i) in slowFiltered" :key="i"
                  :class="{ selected: selectedSlow === row }"
                  @click="selectedSlow = selectedSlow === row ? null : row"
                >
                  <td><span class="dblogs-badge" :class="stmtClass(row.statement_type)">{{ row.statement_type }}</span></td>
                  <td class="dblogs-query-cell">{{ truncate(row.query) }}</td>
                  <td class="dblogs-num">{{ fmtNum(row.calls) }}</td>
                  <td class="dblogs-num" :class="{ 'dblogs-warn': row.avg_ms > 5000, 'dblogs-critical': row.avg_ms > 30000 }">{{ fmtMs(row.avg_ms) }}</td>
                  <td class="dblogs-num" :class="{ 'dblogs-warn': row.max_ms > 5000, 'dblogs-critical': row.max_ms > 30000 }">{{ fmtMs(row.max_ms) }}</td>
                  <td class="dblogs-num">{{ fmtNum(row.rows) }}</td>
                  <td class="dblogs-muted-cell">{{ row.database }}</td>
                  <td class="dblogs-muted-cell">{{ row.username }}</td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Row detail -->
          <div v-if="selectedSlow" class="dblogs-detail">
            <div class="dblogs-detail__header">
              <span class="dblogs-badge" :class="stmtClass(selectedSlow.statement_type)">{{ selectedSlow.statement_type }}</span>
              <span class="dblogs-detail__time">Avg {{ fmtMs(selectedSlow.avg_ms) }} · Max {{ fmtMs(selectedSlow.max_ms) }} · {{ fmtNum(selectedSlow.calls) }} calls</span>
              <button class="dblogs-detail__close" @click="selectedSlow = null">✕</button>
            </div>
            <div class="dblogs-detail__body">
              <div class="dblogs-detail__section">
                <div class="dblogs-detail__section-label">Query</div>
                <pre class="dblogs-detail__pre dblogs-query">{{ selectedSlow.query }}</pre>
              </div>
              <div class="dblogs-detail__meta">
                <span><strong>Database:</strong> {{ selectedSlow.database }}</span>
                <span><strong>User:</strong> {{ selectedSlow.username }}</span>
                <span><strong>Calls:</strong> {{ fmtNum(selectedSlow.calls) }}</span>
                <span><strong>Avg:</strong> {{ fmtMs(selectedSlow.avg_ms) }}</span>
                <span><strong>Min:</strong> {{ fmtMs(selectedSlow.min_ms) }}</span>
                <span><strong>Max:</strong> {{ fmtMs(selectedSlow.max_ms) }}</span>
                <span><strong>Total:</strong> {{ fmtMs(selectedSlow.total_ms) }}</span>
                <span><strong>Rows/call:</strong> {{ selectedSlow.calls > 0 ? (selectedSlow.rows / selectedSlow.calls).toFixed(1) : 0 }}</span>
              </div>
            </div>
          </div>

          <!-- Pagination -->
          <div class="dblogs-pager">
            <span class="dblogs-pager__info">{{ fmtNum(slowTotal) }} results · Page {{ slowPage }} / {{ slowTotalPages }}</span>
            <button class="dblogs-pager__btn" :disabled="slowPage <= 1" @click="slowGoPage(slowPage - 1)">‹ Prev</button>
            <button class="dblogs-pager__btn" :disabled="slowPage >= slowTotalPages" @click="slowGoPage(slowPage + 1)">Next ›</button>
          </div>
        </template>
      </div>

      <!-- ── SQL AUDIT LOGS ──────────────────────────────────── -->
      <div v-else-if="activeTab === 'audit'" class="dblogs-panel">
        <!-- Filters -->
        <div class="dblogs-toolbar">
          <div class="dblogs-toolbar__group">
            <span class="dblogs-label">Search</span>
            <input v-model="auditSearch" type="text" placeholder="Filter by SQL…" class="dblogs-input" @keydown.enter="auditGoPage(1); loadAuditLogs()" />
          </div>
          <div class="dblogs-toolbar__group">
            <span class="dblogs-label">Per page</span>
            <select v-model="auditLimit" class="dblogs-select-sm">
              <option :value="25">25</option>
              <option :value="50">50</option>
              <option :value="100">100</option>
            </select>
          </div>
          <div class="dblogs-toolbar__spacer" />
          <button class="dblogs-btn" @click="auditGoPage(1); loadAuditLogs()">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
            Refresh
          </button>
        </div>

        <div v-if="auditNotice" class="dblogs-notice">{{ auditNotice }}</div>

        <div v-else-if="auditLoading" class="dblogs-loading">
          <svg class="spin" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
          Loading audit log…
        </div>

        <div v-else-if="!auditLoading && !auditRows.length" class="dblogs-empty-sm">
          No audit log entries found.
        </div>

        <template v-else>
          <div class="dblogs-table-wrap">
            <table class="dblogs-table">
              <thead>
                <tr>
                  <th style="width:160px">Executed At</th>
                  <th>SQL Statement</th>
                  <th style="width:90px">Duration</th>
                  <th style="width:80px">Rows</th>
                  <th style="width:80px">Status</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="(row, i) in auditRows" :key="i"
                  :class="{ selected: selectedAudit === row }"
                  @click="selectedAudit = selectedAudit === row ? null : row"
                >
                  <td class="dblogs-mono">{{ row.executed_at }}</td>
                  <td class="dblogs-query-cell">{{ truncate(row.sql) }}</td>
                  <td class="dblogs-num">{{ row.duration_ms != null ? fmtMs(row.duration_ms) : '—' }}</td>
                  <td class="dblogs-num">{{ row.rows_affected != null ? fmtNum(row.rows_affected) : '—' }}</td>
                  <td>
                    <span class="dblogs-badge" :class="row.status === 'error' ? 'sev-error' : 'sev-info'">
                      {{ row.status || 'ok' }}
                    </span>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Row detail -->
          <div v-if="selectedAudit" class="dblogs-detail">
            <div class="dblogs-detail__header">
              <span class="dblogs-badge" :class="selectedAudit.status === 'error' ? 'sev-error' : 'sev-info'">{{ selectedAudit.status || 'ok' }}</span>
              <span class="dblogs-detail__time">{{ selectedAudit.executed_at }}</span>
              <button class="dblogs-detail__close" @click="selectedAudit = null">✕</button>
            </div>
            <div class="dblogs-detail__body">
              <div class="dblogs-detail__section">
                <div class="dblogs-detail__section-label">SQL</div>
                <pre class="dblogs-detail__pre dblogs-query">{{ selectedAudit.sql }}</pre>
              </div>
              <div v-if="selectedAudit.error" class="dblogs-detail__section">
                <div class="dblogs-detail__section-label">Error</div>
                <pre class="dblogs-detail__pre" style="color:var(--red)">{{ selectedAudit.error }}</pre>
              </div>
              <div class="dblogs-detail__meta">
                <span v-if="selectedAudit.duration_ms != null"><strong>Duration:</strong> {{ fmtMs(selectedAudit.duration_ms) }}</span>
                <span v-if="selectedAudit.rows_affected != null"><strong>Rows affected:</strong> {{ fmtNum(selectedAudit.rows_affected) }}</span>
              </div>
            </div>
          </div>

          <!-- Pagination -->
          <div class="dblogs-pager">
            <span class="dblogs-pager__info">{{ fmtNum(auditTotal) }} results · Page {{ auditPage }} / {{ auditTotalPages }}</span>
            <button class="dblogs-pager__btn" :disabled="auditPage <= 1" @click="auditGoPage(auditPage - 1)">‹ Prev</button>
            <button class="dblogs-pager__btn" :disabled="auditPage >= auditTotalPages" @click="auditGoPage(auditPage + 1)">Next ›</button>
          </div>
        </template>
      </div>
    </template>
  </div>
</template>

<style scoped>
.dblogs {
  padding: 24px 28px;
  max-width: 1400px;
  font-family: var(--font);
  color: var(--text);
}

/* Header */
.dblogs-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 20px;
}
.dblogs-header__tag {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: .08em;
  color: var(--text-secondary);
  text-transform: uppercase;
  margin-bottom: 4px;
}
.dblogs-header__title {
  font-size: 22px;
  font-weight: 700;
  margin: 0 0 4px;
  color: var(--text);
}
.dblogs-header__sub {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 0;
}
.dblogs-conn-select {
  font-size: 13px;
  padding: 6px 10px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--surface);
  color: var(--text);
  cursor: pointer;
  min-width: 200px;
}

/* Tabs */
.dblogs-tabs {
  display: flex;
  gap: 2px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 16px;
}
.dblogs-tab {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-secondary);
  background: none;
  border: none;
  border-bottom: 2px solid transparent;
  cursor: pointer;
  transition: color .15s, border-color .15s;
  margin-bottom: -1px;
}
.dblogs-tab:hover { color: var(--text); }
.dblogs-tab.active { color: var(--accent); border-bottom-color: var(--accent); }

/* Toolbar */
.dblogs-toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  padding: 10px 14px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 8px;
  margin-bottom: 10px;
}
.dblogs-toolbar__group {
  display: flex;
  align-items: center;
  gap: 6px;
}
.dblogs-toolbar__spacer { flex: 1; }
.dblogs-label {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: .05em;
  color: var(--text-secondary);
  white-space: nowrap;
}
.dblogs-input {
  font-size: 12.5px;
  padding: 4px 8px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg);
  color: var(--text);
  width: 120px;
}
.dblogs-input--sm { width: 90px; }
.dblogs-select-sm {
  font-size: 12.5px;
  padding: 4px 8px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg);
  color: var(--text);
}

.dblogs-pills {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
}
.dblogs-pill {
  font-size: 11px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 4px;
  border: 1px solid transparent;
  cursor: pointer;
  opacity: .5;
  transition: opacity .15s;
  background: var(--surface);
}
.dblogs-pill.active { opacity: 1; }
.dblogs-pill.sev-fatal  { border-color: #7f1d1d; color: #ef4444; background: rgba(239,68,68,.08); }
.dblogs-pill.sev-error  { border-color: #991b1b; color: #f97316; background: rgba(249,115,22,.08); }
.dblogs-pill.sev-warning{ border-color: #92400e; color: #f59e0b; background: rgba(245,158,11,.08); }
.dblogs-pill.sev-context{ border-color: var(--border); color: var(--text-secondary); }
.dblogs-pill.sev-stmt   { border-color: var(--border); color: var(--accent); background: rgba(99,102,241,.06); }
.dblogs-pill.sev-info   { border-color: var(--border); color: var(--text-secondary); }

.dblogs-threshold {
  display: flex;
  align-items: center;
  gap: 8px;
}
.dblogs-slider { width: 140px; cursor: pointer; accent-color: var(--accent); }
.dblogs-threshold__val { font-size: 12px; font-weight: 600; min-width: 48px; color: var(--accent); }

.dblogs-shortcuts {
  display: flex;
  gap: 6px;
  margin-bottom: 10px;
}
.dblogs-shortcut {
  font-size: 11.5px;
  padding: 3px 10px;
  border-radius: 5px;
  border: 1px solid var(--border);
  background: var(--surface);
  color: var(--text-secondary);
  cursor: pointer;
  transition: background .15s, color .15s;
}
.dblogs-shortcut:hover { background: var(--accent); color: #fff; border-color: var(--accent); }

.dblogs-btn {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 5px 12px;
  font-size: 12.5px;
  font-weight: 600;
  background: var(--accent);
  color: #fff;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  transition: opacity .15s;
}
.dblogs-btn:hover { opacity: .88; }

.dblogs-source {
  font-size: 11px;
  color: var(--text-secondary);
  font-style: italic;
}

/* States */
.dblogs-notice {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  font-size: 13px;
  color: var(--text-secondary);
  background: color-mix(in srgb, var(--accent) 8%, transparent);
  border: 1px solid color-mix(in srgb, var(--accent) 25%, transparent);
  border-radius: 8px;
  padding: 12px 14px;
  margin-bottom: 12px;
}
.dblogs-loading, .dblogs-empty-sm {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--text-secondary);
  padding: 32px 20px;
}
.dblogs-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 60px 20px;
  color: var(--text-secondary);
  text-align: center;
}
.dblogs-empty svg { opacity: .4; }

/* Table */
.dblogs-table-wrap {
  overflow-x: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
  margin-bottom: 0;
}
.dblogs-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12.5px;
}
.dblogs-table thead th {
  padding: 8px 12px;
  text-align: left;
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .05em;
  color: var(--text-secondary);
  background: var(--surface);
  border-bottom: 1px solid var(--border);
  white-space: nowrap;
  position: sticky;
  top: 0;
  z-index: 1;
}
.dblogs-table tbody tr {
  border-bottom: 1px solid color-mix(in srgb, var(--border) 50%, transparent);
  cursor: pointer;
  transition: background .1s;
}
.dblogs-table tbody tr:last-child { border-bottom: none; }
.dblogs-table tbody tr:hover { background: color-mix(in srgb, var(--accent) 5%, transparent); }
.dblogs-table tbody tr.selected { background: color-mix(in srgb, var(--accent) 10%, transparent); }
.dblogs-table td { padding: 7px 12px; vertical-align: top; }

.dblogs-mono { font-family: var(--mono); font-size: 11.5px; white-space: nowrap; }
.dblogs-num { font-family: var(--mono); font-size: 12px; text-align: right; white-space: nowrap; }
.dblogs-muted-cell { color: var(--text-secondary); font-size: 12px; }
.dblogs-msg { max-width: 600px; word-break: break-word; line-height: 1.45; }
.dblogs-query-cell {
  max-width: 480px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--mono);
  font-size: 11.5px;
  color: var(--text-secondary);
}
.dblogs-warn { color: #f59e0b; }
.dblogs-critical { color: #ef4444; }

/* Badges */
.dblogs-badge {
  display: inline-block;
  font-size: 10.5px;
  font-weight: 700;
  padding: 1px 7px;
  border-radius: 4px;
  text-transform: uppercase;
  letter-spacing: .04em;
  white-space: nowrap;
}
.sev-fatal  { background: rgba(239,68,68,.15);  color: #ef4444; }
.sev-error  { background: rgba(249,115,22,.15); color: #f97316; }
.sev-warning{ background: rgba(245,158,11,.15); color: #d97706; }
.sev-context{ background: var(--surface); color: var(--text-secondary); border: 1px solid var(--border); }
.sev-stmt   { background: rgba(99,102,241,.1); color: var(--accent); }
.sev-info   { background: rgba(16,185,129,.1); color: #10b981; }
.stmt-select { background: rgba(16,185,129,.12); color: #10b981; }
.stmt-insert { background: rgba(99,102,241,.12); color: var(--accent); }
.stmt-update { background: rgba(245,158,11,.12); color: #d97706; }
.stmt-delete { background: rgba(239,68,68,.12);  color: #ef4444; }
.stmt-other  { background: var(--surface); color: var(--text-secondary); border: 1px solid var(--border); }

/* Detail panel */
.dblogs-detail {
  border: 1px solid var(--border);
  border-top: none;
  border-radius: 0 0 8px 8px;
  background: var(--surface);
}
.dblogs-detail__header {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
}
.dblogs-detail__time {
  flex: 1;
  font-size: 12px;
  color: var(--text-secondary);
  font-family: var(--mono);
}
.dblogs-detail__close {
  font-size: 14px;
  background: none;
  border: none;
  color: var(--text-secondary);
  cursor: pointer;
  line-height: 1;
  padding: 2px 6px;
  border-radius: 4px;
}
.dblogs-detail__close:hover { background: var(--border); }
.dblogs-detail__body { padding: 14px; display: flex; flex-direction: column; gap: 12px; }
.dblogs-detail__section { display: flex; flex-direction: column; gap: 4px; }
.dblogs-detail__section-label {
  font-size: 10.5px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .06em;
  color: var(--text-secondary);
}
.dblogs-detail__pre {
  margin: 0;
  font-family: var(--mono);
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 10px 12px;
  max-height: 280px;
  overflow-y: auto;
}
.dblogs-hint { color: #10b981; }
.dblogs-query { color: var(--accent); }
.dblogs-detail__meta {
  display: flex;
  flex-wrap: wrap;
  gap: 10px 20px;
  font-size: 12px;
  color: var(--text-secondary);
  padding-top: 4px;
}

/* Pagination */
.dblogs-pager {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  border: 1px solid var(--border);
  border-top: none;
  border-radius: 0 0 8px 8px;
  background: var(--surface);
}
.dblogs-pager__info { font-size: 12px; color: var(--text-secondary); flex: 1; }
.dblogs-pager__btn {
  font-size: 12.5px;
  padding: 4px 12px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg);
  color: var(--text);
  cursor: pointer;
  transition: background .1s;
}
.dblogs-pager__btn:hover:not(:disabled) { background: var(--accent); color: #fff; border-color: var(--accent); }
.dblogs-pager__btn:disabled { opacity: .4; cursor: not-allowed; }

.dblogs-muted { color: var(--text-secondary); font-size: 12.5px; }

/* Spin */
@keyframes spin { to { transform: rotate(360deg); } }
.spin { animation: spin .8s linear infinite; }
</style>
