<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import { formatServerTimestamp, parseServerTimestamp } from '@/utils/datetime'

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

interface FingerprintGroup {
  fingerprint: string
  sample_sql: string
  count: number
  avg_duration_ms: number
  p95_duration_ms: number
  max_duration_ms: number
  error_count: number
  latest_at: string
  categories: string[]
  connections: string[]
}

interface NativeQueryStat {
  conn_id: number
  conn_name: string
  driver: string
  source: string
  fingerprint: string
  sql: string
  calls: number
  total_ms: number
  avg_ms: number
  max_ms: number
  rows: number
  rows_examined: number
  last_seen: string
}

interface NativeQueryNotice {
  conn_id: number
  conn_name: string
  driver: string
  message: string
}

interface NativeResponse {
  stats: NativeQueryStat[]
  notices: NativeQueryNotice[]
}

type FilterMode = 'all' | 'slow' | 'error'
type ViewMode = 'executions' | 'fingerprints'
type SourceMode = 'app' | 'native'

const { connections, fetchConnections } = useConnections()

const sourceMode = ref<SourceMode>('app')
const entries = ref<AuditEntry[]>([])
const nativeStats = ref<NativeQueryStat[]>([])
const nativeNotices = ref<NativeQueryNotice[]>([])
const loading = ref(false)
const filterMode = ref<FilterMode>('slow')
const viewMode = ref<ViewMode>('fingerprints')
const selectedConnId = ref<number | 'all'>('all')
const thresholdMs = ref(1000)
const sinceHours = ref(24)
const search = ref('')
const limit = ref(1000)

function truncateSql(sql: string, max = 220): string {
  const compact = sql.replace(/\s+/g, ' ').trim()
  if (compact.length <= max) return compact
  return compact.slice(0, max - 1) + '…'
}

function normalizeSql(sql: string): string {
  return sql
    .replace(/'([^']|'')*'/g, '?')
    .replace(/\b\d+(?:\.\d+)?\b/g, '?')
    .replace(/\s+/g, ' ')
    .trim()
    .toUpperCase()
}

function categorizeError(error: string): string {
  const msg = error.toLowerCase()
  if (!msg) return 'none'
  if (msg.includes('syntax')) return 'syntax'
  if (msg.includes('timeout') || msg.includes('cancel') || msg.includes('deadline')) return 'timeout'
  if (msg.includes('lock') || msg.includes('deadlock')) return 'locking'
  if (msg.includes('permission') || msg.includes('denied') || msg.includes('forbidden')) return 'permission'
  if (msg.includes('connection') || msg.includes('dial') || msg.includes('network')) return 'connection'
  return 'other'
}

function percentile(values: number[], pct: number): number {
  if (!values.length) return 0
  const sorted = [...values].sort((a, b) => a - b)
  const index = Math.min(sorted.length - 1, Math.max(0, Math.ceil((pct / 100) * sorted.length) - 1))
  return sorted[index]
}

function statusTone(entry: AuditEntry) {
  if (entry.error) return 'error'
  if (entry.duration_ms >= thresholdMs.value) return 'slow'
  return 'ok'
}

async function loadAppData() {
  const params: Record<string, string | number> = {
    event_type: 'query_execution',
    limit: limit.value,
    since_hours: sinceHours.value,
  }
  if (selectedConnId.value !== 'all') params.conn_id = selectedConnId.value
  if (filterMode.value === 'slow') params.min_duration_ms = thresholdMs.value
  if (filterMode.value === 'error') params.has_error = 1
  if (search.value.trim()) params.q = search.value.trim()

  const { data } = await axios.get<AuditEntry[]>('/api/admin/audit', { params })
  entries.value = data ?? []
}

async function loadNativeData() {
  const params: Record<string, string | number> = {
    limit: 100,
  }
  if (selectedConnId.value !== 'all') params.conn_id = selectedConnId.value

  const { data } = await axios.get<NativeResponse>('/api/query-performance/native', { params })
  nativeStats.value = data?.stats ?? []
  nativeNotices.value = data?.notices ?? []
}

async function load() {
  loading.value = true
  try {
    await fetchConnections()
    if (sourceMode.value === 'app') {
      nativeStats.value = []
      nativeNotices.value = []
      await loadAppData()
    } else {
      entries.value = []
      await loadNativeData()
    }
  } finally {
    loading.value = false
  }
}

const filteredEntries = computed(() => {
  const term = search.value.trim().toLowerCase()
  return entries.value.filter((entry) => {
    if (selectedConnId.value !== 'all' && entry.conn_id !== selectedConnId.value) return false
    if (filterMode.value === 'slow' && entry.duration_ms < thresholdMs.value) return false
    if (filterMode.value === 'error' && !entry.error) return false
    if (term) {
      const haystack = [entry.sql, entry.error, entry.username, entry.conn_name].join(' ').toLowerCase()
      if (!haystack.includes(term)) return false
    }
    return true
  })
})

const sortedEntries = computed(() =>
  [...filteredEntries.value].sort((a, b) => parseServerTimestamp(b.executed_at).getTime() - parseServerTimestamp(a.executed_at).getTime())
)

const appDurations = computed(() => filteredEntries.value.map((e) => e.duration_ms).filter((v) => v >= 0))
const appSlowCount = computed(() => filteredEntries.value.filter((e) => e.duration_ms >= thresholdMs.value).length)
const appErrorCount = computed(() => filteredEntries.value.filter((e) => !!e.error).length)
const appWorstDuration = computed(() => appDurations.value.length ? Math.max(...appDurations.value) : 0)
const appAvgDuration = computed(() => appDurations.value.length ? Math.round(appDurations.value.reduce((sum, v) => sum + v, 0) / appDurations.value.length) : 0)
const p50Duration = computed(() => percentile(appDurations.value, 50))
const p95Duration = computed(() => percentile(appDurations.value, 95))
const p99Duration = computed(() => percentile(appDurations.value, 99))

const categoryBreakdown = computed(() => {
  const counts = new Map<string, number>()
  for (const entry of filteredEntries.value) {
    if (!entry.error) continue
    const category = categorizeError(entry.error)
    counts.set(category, (counts.get(category) ?? 0) + 1)
  }
  return [...counts.entries()]
    .map(([category, count]) => ({ category, count }))
    .sort((a, b) => b.count - a.count)
})

const fingerprintGroups = computed<FingerprintGroup[]>(() => {
  const groups = new Map<string, AuditEntry[]>()
  for (const entry of filteredEntries.value) {
    const fp = normalizeSql(entry.sql)
    if (!groups.has(fp)) groups.set(fp, [])
    groups.get(fp)!.push(entry)
  }

  return [...groups.entries()].map(([fingerprint, groupEntries]) => {
    const groupDurations = groupEntries.map((e) => e.duration_ms)
    const categories = [...new Set(groupEntries.map((e) => categorizeError(e.error)).filter((c) => c !== 'none'))]
    const connectionsUsed = [...new Set(groupEntries.map((e) => e.conn_name).filter(Boolean))]
    const latest = [...groupEntries].sort((a, b) => parseServerTimestamp(b.executed_at).getTime() - parseServerTimestamp(a.executed_at).getTime())[0]
    return {
      fingerprint,
      sample_sql: latest?.sql ?? groupEntries[0]?.sql ?? fingerprint,
      count: groupEntries.length,
      avg_duration_ms: Math.round(groupDurations.reduce((sum, v) => sum + v, 0) / Math.max(groupDurations.length, 1)),
      p95_duration_ms: percentile(groupDurations, 95),
      max_duration_ms: groupDurations.length ? Math.max(...groupDurations) : 0,
      error_count: groupEntries.filter((e) => !!e.error).length,
      latest_at: latest?.executed_at ?? '',
      categories,
      connections: connectionsUsed,
    }
  }).sort((a, b) => {
    if (b.max_duration_ms !== a.max_duration_ms) return b.max_duration_ms - a.max_duration_ms
    if (b.count !== a.count) return b.count - a.count
    return parseServerTimestamp(b.latest_at).getTime() - parseServerTimestamp(a.latest_at).getTime()
  })
})

const filteredNativeStats = computed(() => {
  const term = search.value.trim().toLowerCase()
  return nativeStats.value.filter((stat) => {
    if (selectedConnId.value !== 'all' && stat.conn_id !== selectedConnId.value) return false
    if (filterMode.value === 'slow' && stat.avg_ms < thresholdMs.value) return false
    if (term) {
      const haystack = [stat.sql, stat.fingerprint, stat.conn_name, stat.source].join(' ').toLowerCase()
      if (!haystack.includes(term)) return false
    }
    return true
  }).sort((a, b) => {
    if (b.avg_ms !== a.avg_ms) return b.avg_ms - a.avg_ms
    if (b.calls !== a.calls) return b.calls - a.calls
    return parseServerTimestamp(b.last_seen).getTime() - parseServerTimestamp(a.last_seen).getTime()
  })
})

const nativeFingerprintCount = computed(() => filteredNativeStats.value.length)
const nativeCallCount = computed(() => filteredNativeStats.value.reduce((sum, stat) => sum + stat.calls, 0))
const nativeAvgDuration = computed(() => {
  const totalCalls = nativeCallCount.value
  if (!totalCalls) return 0
  const weighted = filteredNativeStats.value.reduce((sum, stat) => sum + (stat.avg_ms * stat.calls), 0)
  return Math.round(weighted / totalCalls)
})
const nativeWorstAvg = computed(() => filteredNativeStats.value.length ? Math.round(Math.max(...filteredNativeStats.value.map((s) => s.avg_ms))) : 0)
const nativeWorstMax = computed(() => filteredNativeStats.value.length ? Math.round(Math.max(...filteredNativeStats.value.map((s) => s.max_ms))) : 0)

onMounted(load)
watch([sourceMode, filterMode, selectedConnId, sinceHours], () => { void load() })
</script>

<template>
  <div class="page-shell qp-root">
    <div class="page-scroll qp-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Observability</div>
            <div class="page-title">Query Performance</div>
            <div class="page-subtitle">
              Compare app-captured executions with database-native query statistics. Native mode can reveal slow queries that never went through this app.
            </div>
          </div>
          <div class="page-hero__actions">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="load" :disabled="loading">
              <svg :class="loading ? 'spin' : ''" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.08-4.43"/></svg>
              Refresh
            </button>
          </div>
        </section>

        <div class="qp-stats" v-if="sourceMode === 'app'">
          <div class="page-panel qp-stat"><div class="qp-stat__label">Executions</div><div class="qp-stat__value">{{ filteredEntries.length.toLocaleString() }}</div></div>
          <div class="page-panel qp-stat"><div class="qp-stat__label">Fingerprints</div><div class="qp-stat__value">{{ fingerprintGroups.length.toLocaleString() }}</div></div>
          <div class="page-panel qp-stat"><div class="qp-stat__label">Slow</div><div class="qp-stat__value qp-stat__value--warn">{{ appSlowCount.toLocaleString() }}</div></div>
          <div class="page-panel qp-stat"><div class="qp-stat__label">Errors</div><div class="qp-stat__value qp-stat__value--err">{{ appErrorCount.toLocaleString() }}</div></div>
          <div class="page-panel qp-stat"><div class="qp-stat__label">P95</div><div class="qp-stat__value">{{ p95Duration }}ms</div></div>
          <div class="page-panel qp-stat"><div class="qp-stat__label">P99</div><div class="qp-stat__value">{{ p99Duration }}ms</div></div>
        </div>

        <div class="qp-stats" v-else>
          <div class="page-panel qp-stat"><div class="qp-stat__label">Fingerprints</div><div class="qp-stat__value">{{ nativeFingerprintCount.toLocaleString() }}</div></div>
          <div class="page-panel qp-stat"><div class="qp-stat__label">Calls</div><div class="qp-stat__value">{{ nativeCallCount.toLocaleString() }}</div></div>
          <div class="page-panel qp-stat"><div class="qp-stat__label">Avg</div><div class="qp-stat__value">{{ nativeAvgDuration }}ms</div></div>
          <div class="page-panel qp-stat"><div class="qp-stat__label">Worst Avg</div><div class="qp-stat__value qp-stat__value--warn">{{ nativeWorstAvg }}ms</div></div>
          <div class="page-panel qp-stat"><div class="qp-stat__label">Worst Max</div><div class="qp-stat__value qp-stat__value--warn">{{ nativeWorstMax }}ms</div></div>
          <div class="page-panel qp-stat"><div class="qp-stat__label">Source</div><div class="qp-stat__value">Native</div></div>
        </div>

        <section class="page-panel qp-toolbar">
          <div class="qp-toolbar__grid">
            <label class="qp-field">
              <span>Source</span>
              <select v-model="sourceMode">
                <option value="app">App audit</option>
                <option value="native">Database native</option>
              </select>
            </label>

            <label class="qp-field">
              <span>Mode</span>
              <select v-model="filterMode">
                <option value="slow">Slow queries</option>
                <option value="error" :disabled="sourceMode === 'native'">Error queries</option>
                <option value="all">All query executions</option>
              </select>
            </label>

            <label class="qp-field" v-if="sourceMode === 'app'">
              <span>View</span>
              <select v-model="viewMode">
                <option value="fingerprints">Grouped fingerprints</option>
                <option value="executions">Raw executions</option>
              </select>
            </label>

            <label class="qp-field">
              <span>Connection</span>
              <select v-model="selectedConnId">
                <option value="all">All connections</option>
                <option v-for="conn in connections" :key="conn.id" :value="conn.id">{{ conn.name }}</option>
              </select>
            </label>

            <label class="qp-field" v-if="sourceMode === 'app'">
              <span>Window</span>
              <select v-model.number="sinceHours">
                <option :value="1">Last 1 hour</option>
                <option :value="6">Last 6 hours</option>
                <option :value="24">Last 24 hours</option>
                <option :value="168">Last 7 days</option>
              </select>
            </label>

            <label class="qp-field">
              <span>Slow threshold</span>
              <input v-model.number="thresholdMs" type="number" min="1" step="100" />
            </label>

            <label class="qp-field qp-field--wide">
              <span>Search</span>
              <input v-model="search" type="text" placeholder="Filter by SQL, fingerprint, error, or connection" />
            </label>
          </div>
        </section>

        <div class="qp-insights" v-if="sourceMode === 'app'">
          <section class="page-panel qp-insight-card">
            <div class="qp-section-title">Latency Percentiles</div>
            <div class="qp-metric-strip">
              <div class="qp-metric"><div class="qp-metric__value">{{ appAvgDuration }}ms</div><div class="qp-metric__label">Average</div></div>
              <div class="qp-metric"><div class="qp-metric__value">{{ p50Duration }}ms</div><div class="qp-metric__label">P50</div></div>
              <div class="qp-metric"><div class="qp-metric__value">{{ p95Duration }}ms</div><div class="qp-metric__label">P95</div></div>
              <div class="qp-metric"><div class="qp-metric__value">{{ p99Duration }}ms</div><div class="qp-metric__label">P99</div></div>
              <div class="qp-metric"><div class="qp-metric__value">{{ appWorstDuration ? `${appWorstDuration}ms` : '—' }}</div><div class="qp-metric__label">Worst</div></div>
            </div>
          </section>

          <section class="page-panel qp-insight-card">
            <div class="qp-section-title">Error Categories</div>
            <div v-if="categoryBreakdown.length" class="qp-category-list">
              <div v-for="item in categoryBreakdown" :key="item.category" class="qp-category-row">
                <span class="qp-category-row__name">{{ item.category }}</span>
                <span class="qp-category-row__count">{{ item.count.toLocaleString() }}</span>
              </div>
            </div>
            <div v-else class="qp-empty-inline">No errors in the current slice.</div>
          </section>
        </div>

        <section class="page-panel qp-native-notices" v-if="sourceMode === 'native' && nativeNotices.length">
          <div class="qp-section-title">Native Source Notices</div>
          <div class="qp-notice-list">
            <div v-for="notice in nativeNotices" :key="`${notice.conn_id}-${notice.message}`" class="qp-notice">
              <strong>{{ notice.conn_name }}</strong> · {{ notice.driver }} · {{ notice.message }}
            </div>
          </div>
        </section>

        <section class="qp-list" v-if="sourceMode === 'app'">
          <div v-if="loading" class="page-panel qp-empty">Loading query executions…</div>
          <div v-else-if="viewMode === 'fingerprints' && fingerprintGroups.length === 0" class="page-panel qp-empty">No matching query fingerprints.</div>
          <div v-else-if="viewMode === 'executions' && sortedEntries.length === 0" class="page-panel qp-empty">No matching query executions.</div>

          <template v-if="viewMode === 'fingerprints'">
            <article v-for="group in fingerprintGroups" :key="group.fingerprint" class="page-panel qp-card qp-card--fingerprint">
              <div class="qp-card__top">
                <div class="qp-card__badges">
                  <span class="qp-pill qp-pill--conn">{{ group.connections.join(', ') || 'Unknown connection' }}</span>
                  <span class="qp-pill qp-pill--slow">{{ group.count.toLocaleString() }} runs</span>
                  <span v-if="group.error_count" class="qp-pill qp-pill--err">{{ group.error_count.toLocaleString() }} errors</span>
                  <span v-for="category in group.categories" :key="category" class="qp-pill qp-pill--user">{{ category }}</span>
                </div>
                <div class="qp-card__time">{{ formatServerTimestamp(group.latest_at) }}</div>
              </div>

              <div class="qp-card__sql" :title="group.sample_sql">{{ truncateSql(group.sample_sql) }}</div>
              <div class="qp-card__fingerprint">{{ truncateSql(group.fingerprint, 260) }}</div>
              <div class="qp-card__meta">
                <span>avg {{ group.avg_duration_ms }}ms</span>
                <span>p95 {{ group.p95_duration_ms }}ms</span>
                <span>max {{ group.max_duration_ms }}ms</span>
              </div>
            </article>
          </template>

          <template v-else>
            <article v-for="entry in sortedEntries" :key="entry.id" class="page-panel qp-card" :class="`qp-card--${statusTone(entry)}`">
              <div class="qp-card__top">
                <div class="qp-card__badges">
                  <span class="qp-pill qp-pill--conn">{{ entry.conn_name || 'Unknown connection' }}</span>
                  <span class="qp-pill qp-pill--user">{{ entry.username || 'user' }}</span>
                  <span v-if="entry.error" class="qp-pill qp-pill--err">{{ categorizeError(entry.error) }}</span>
                  <span v-else-if="entry.duration_ms >= thresholdMs" class="qp-pill qp-pill--slow">Slow</span>
                  <span v-else class="qp-pill qp-pill--ok">OK</span>
                </div>
                <div class="qp-card__time">{{ formatServerTimestamp(entry.executed_at) }}</div>
              </div>

              <div class="qp-card__sql" :title="entry.sql">{{ truncateSql(entry.sql) }}</div>
              <div class="qp-card__meta">
                <span>{{ entry.duration_ms }}ms</span>
                <span>{{ entry.row_count.toLocaleString() }} rows</span>
              </div>
              <div v-if="entry.error" class="qp-card__error">{{ entry.error }}</div>
            </article>
          </template>
        </section>

        <section class="qp-list" v-else>
          <div v-if="loading" class="page-panel qp-empty">Loading native query statistics…</div>
          <div v-else-if="filteredNativeStats.length === 0" class="page-panel qp-empty">No native query statistics matched the current filter.</div>

          <article v-for="stat in filteredNativeStats" :key="`${stat.conn_id}-${stat.fingerprint}`" class="page-panel qp-card qp-card--fingerprint">
            <div class="qp-card__top">
              <div class="qp-card__badges">
                <span class="qp-pill qp-pill--conn">{{ stat.conn_name }}</span>
                <span class="qp-pill qp-pill--user">{{ stat.source }}</span>
                <span class="qp-pill qp-pill--slow">{{ stat.calls.toLocaleString() }} calls</span>
              </div>
              <div class="qp-card__time">{{ stat.last_seen ? formatServerTimestamp(stat.last_seen) : 'since reset' }}</div>
            </div>

            <div class="qp-card__sql" :title="stat.sql">{{ truncateSql(stat.sql) }}</div>
            <div class="qp-card__fingerprint">{{ truncateSql(stat.fingerprint, 260) }}</div>
            <div class="qp-card__meta">
              <span>avg {{ Math.round(stat.avg_ms) }}ms</span>
              <span>max {{ Math.round(stat.max_ms) }}ms</span>
              <span>total {{ Math.round(stat.total_ms) }}ms</span>
              <span>{{ stat.rows.toLocaleString() }} rows</span>
              <span v-if="stat.rows_examined">{{ stat.rows_examined.toLocaleString() }} examined</span>
            </div>
          </article>
        </section>
      </div>
    </div>
  </div>
</template>

<style scoped>
.qp-root { background: var(--bg-body); }
.qp-stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
}
.qp-stat { padding: 14px 16px; }
.qp-stat__label {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.4px;
  color: var(--text-muted);
}
.qp-stat__value {
  margin-top: 6px;
  font-size: 24px;
  font-weight: 700;
  color: var(--text-primary);
  font-variant-numeric: tabular-nums;
}
.qp-stat__value--warn { color: #d97706; }
.qp-stat__value--err { color: #dc2626; }
.qp-toolbar { padding: 16px; }
.qp-toolbar__grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
}
.qp-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.qp-field--wide { grid-column: span 2; }
.qp-field span,
.qp-section-title {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.35px;
  color: var(--text-muted);
}
.qp-field select,
.qp-field input {
  height: 38px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--bg-panel);
  color: var(--text-primary);
  padding: 0 12px;
  font-size: 13px;
}
.qp-insights {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(0, 1fr);
  gap: 12px;
}
.qp-insight-card,
.qp-native-notices { padding: 16px; }
.qp-metric-strip {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 10px;
  margin-top: 12px;
}
.qp-metric {
  padding: 10px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--bg-panel);
}
.qp-metric__value {
  font-size: 18px;
  font-weight: 700;
  color: var(--text-primary);
  font-variant-numeric: tabular-nums;
}
.qp-metric__label {
  margin-top: 3px;
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.3px;
  color: var(--text-muted);
}
.qp-category-list,
.qp-notice-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 12px;
}
.qp-category-row,
.qp-notice {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 9px 10px;
  border-radius: 8px;
  background: var(--bg-panel);
  border: 1px solid var(--border);
}
.qp-notice {
  justify-content: flex-start;
  color: var(--text-secondary);
  font-size: 12px;
}
.qp-category-row__name {
  font-size: 12px;
  color: var(--text-primary);
  text-transform: capitalize;
}
.qp-category-row__count {
  font-size: 12px;
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}
.qp-empty-inline {
  margin-top: 12px;
  color: var(--text-muted);
  font-size: 12px;
}
.qp-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.qp-card {
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  border-left: 3px solid transparent;
}
.qp-card--slow { border-left-color: #f59e0b; }
.qp-card--error { border-left-color: #ef4444; }
.qp-card--ok { border-left-color: #10b981; }
.qp-card--fingerprint { border-left-color: var(--brand); }
.qp-card__top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}
.qp-card__badges {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.qp-pill {
  display: inline-flex;
  align-items: center;
  border-radius: 999px;
  padding: 4px 8px;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.2px;
}
.qp-pill--conn { background: var(--brand-dim); color: var(--brand); }
.qp-pill--user { background: var(--bg-hover); color: var(--text-secondary); }
.qp-pill--slow { background: rgba(245, 158, 11, 0.14); color: #b45309; }
.qp-pill--err { background: rgba(239, 68, 68, 0.14); color: #b91c1c; }
.qp-pill--ok { background: rgba(16, 185, 129, 0.14); color: #047857; }
.qp-card__time,
.qp-card__meta {
  font-size: 12px;
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}
.qp-card__meta {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}
.qp-card__sql,
.qp-card__fingerprint {
  font-size: 13px;
  line-height: 1.6;
  color: var(--text-primary);
  font-family: var(--mono, monospace);
  word-break: break-word;
}
.qp-card__fingerprint {
  color: var(--text-muted);
  font-size: 11px;
}
.qp-card__error {
  padding: 10px 12px;
  border-radius: 8px;
  background: rgba(239, 68, 68, 0.08);
  color: #b91c1c;
  font-size: 12px;
  line-height: 1.5;
}
.qp-empty {
  padding: 28px;
  text-align: center;
  color: var(--text-muted);
}
@media (max-width: 980px) {
  .qp-insights { grid-template-columns: 1fr; }
  .qp-metric-strip { grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); }
}
@media (max-width: 840px) {
  .qp-field--wide { grid-column: span 1; }
  .qp-card__top {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
