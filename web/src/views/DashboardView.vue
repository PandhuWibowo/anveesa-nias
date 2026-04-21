<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import { formatServerTimestamp } from '@/utils/datetime'

const props = defineProps<{ activeConnId?: number | null }>()
const emit = defineEmits<{ (e: 'set-conn', id: number): void }>()

const router = useRouter()
const { connections, fetchConnections } = useConnections()

interface TableStat {
  name: string
  row_count: number
  size_bytes: number
}

interface SlowQueryStat {
  sql: string
  duration_ms: number
  row_count: number
  error: string
  executed_at: string
}

interface SlowQuerySummary {
  threshold_ms: number
  count: number
  avg_duration_ms: number
  max_duration_ms: number
  queries: SlowQueryStat[]
}

interface DashboardData {
  driver: string
  database: string
  version: string
  size_bytes: number
  table_count: number
  view_count: number
  tables: TableStat[]
  slow_queries: SlowQuerySummary
}

interface ConnState {
  loading: boolean
  error: string
  data: DashboardData | null
  expanded: boolean
}

const states = ref<Record<number, ConnState>>({})

function formatBytes(bytes: number): string {
  if (!bytes || bytes === 0) return '—'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  if (bytes < 1024 * 1024 * 1024) return (bytes / 1024 / 1024).toFixed(1) + ' MB'
  return (bytes / 1024 / 1024 / 1024).toFixed(2) + ' GB'
}

function truncateSql(sql: string, max = 120): string {
  const compact = sql.replace(/\s+/g, ' ').trim()
  if (compact.length <= max) return compact
  return compact.slice(0, max - 1) + '…'
}

function formatExecutedAt(value: string): string {
  return formatServerTimestamp(value)
}

const driverColors: Record<string, string> = {
  postgres: '#336791', mysql: '#f29111', mariadb: '#c0392b',
  mssql: '#cc2927',
}
const driverLabels: Record<string, string> = {
  postgres: 'PG', mysql: 'MY', mariadb: 'MB', mssql: 'MS',
}

async function loadOne(id: number) {
  if (!states.value[id]) {
    states.value[id] = { loading: false, error: '', data: null, expanded: false }
  }
  states.value[id].loading = true
  states.value[id].error = ''
  try {
    const { data } = await axios.get<DashboardData>(`/api/connections/${id}/dashboard`)
    states.value[id].data = data
  } catch (e: any) {
    states.value[id].error = e?.response?.data?.error ?? 'Failed to load'
  } finally {
    states.value[id].loading = false
  }
}

async function loadAll() {
  await fetchConnections()
  for (const c of connections.value) loadOne(c.id)
}

function toggleExpand(id: number) {
  if (states.value[id]) states.value[id].expanded = !states.value[id].expanded
}

function openInBrowser(id: number) {
  emit('set-conn', id)
  router.push({ name: 'data' })
}

// Aggregates
const totalTables = computed(() =>
  Object.values(states.value).reduce((s, v) => s + (v.data?.table_count ?? 0), 0)
)
const totalSize = computed(() =>
  Object.values(states.value).reduce((s, v) => s + (v.data?.size_bytes ?? 0), 0)
)
const totalSlowQueries = computed(() =>
  Object.values(states.value).reduce((s, v) => s + (v.data?.slow_queries.count ?? 0), 0)
)
const worstSlowQueryMs = computed(() =>
  Object.values(states.value).reduce((max, v) => Math.max(max, v.data?.slow_queries.max_duration_ms ?? 0), 0)
)
const loadedCount = computed(() =>
  Object.values(states.value).filter(v => v.data).length
)

onMounted(loadAll)
</script>

<template>
  <div class="page-shell dash-root">
    <div class="page-scroll dash-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Overview</div>
            <div class="page-title">Dashboard</div>
            <div class="page-subtitle">A live, connection-level snapshot of your databases, footprint, and the surfaces your team can browse right now.</div>
          </div>
          <div class="page-hero__actions">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="loadAll">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.08-4.43"/></svg>
              Refresh All
            </button>
          </div>
        </section>

        <div class="dash-agg">
          <div class="page-panel dash-agg-card">
          <div class="dash-agg-card__icon" style="background:var(--brand-dim);color:var(--brand)">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M18.36 6.64a9 9 0 1 1-12.73 0"/><line x1="12" y1="2" x2="12" y2="12"/></svg>
          </div>
          <div>
            <div class="dash-agg-card__label">Connections</div>
            <div class="dash-agg-card__value">{{ connections.length }}</div>
          </div>
        </div>
        <div class="page-panel dash-agg-card">
          <div class="dash-agg-card__icon" style="background:#6366f122;color:#6366f1">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
          </div>
          <div>
            <div class="dash-agg-card__label">Total Tables</div>
            <div class="dash-agg-card__value">{{ totalTables.toLocaleString() }}</div>
          </div>
        </div>
        <div class="page-panel dash-agg-card">
          <div class="dash-agg-card__icon" style="background:#10b98122;color:#10b981">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/></svg>
          </div>
          <div>
            <div class="dash-agg-card__label">Total Size</div>
            <div class="dash-agg-card__value">{{ formatBytes(totalSize) }}</div>
          </div>
        </div>
        <div class="page-panel dash-agg-card">
          <div class="dash-agg-card__icon" style="background:#f59e0b22;color:#f59e0b">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
          </div>
          <div>
            <div class="dash-agg-card__label">Loaded</div>
            <div class="dash-agg-card__value">{{ loadedCount }} / {{ connections.length }}</div>
          </div>
        </div>
        <div class="page-panel dash-agg-card">
          <div class="dash-agg-card__icon" style="background:#ef444422;color:#ef4444">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 9v4"/><path d="M12 17h.01"/><path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/></svg>
          </div>
          <div>
            <div class="dash-agg-card__label">Slow Queries</div>
            <div class="dash-agg-card__value">{{ totalSlowQueries.toLocaleString() }}</div>
          </div>
        </div>
        <div class="page-panel dash-agg-card">
          <div class="dash-agg-card__icon" style="background:#dc262622;color:#dc2626">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 2v6"/><path d="M12 22v-6"/><path d="m4.93 4.93 4.24 4.24"/><path d="m14.83 14.83 4.24 4.24"/><path d="M2 12h6"/><path d="M22 12h-6"/><path d="m4.93 19.07 4.24-4.24"/><path d="m14.83 9.17 4.24-4.24"/></svg>
          </div>
          <div>
            <div class="dash-agg-card__label">Worst Query</div>
            <div class="dash-agg-card__value">{{ worstSlowQueryMs ? `${worstSlowQueryMs} ms` : '—' }}</div>
          </div>
        </div>
        </div>

      <div v-if="connections.length === 0" class="page-panel dash-empty">
        <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round"><path d="M18.36 6.64a9 9 0 1 1-12.73 0"/><line x1="12" y1="2" x2="12" y2="12"/></svg>
        <div class="dash-empty__title">No connections yet</div>
        <div class="dash-empty__sub">Add a connection to get started.</div>
      </div>

      <!-- Connection cards grid -->
      <div v-else class="dash-grid">
        <div
          v-for="conn in connections"
          :key="conn.id"
          class="page-panel dash-conn-card"
          :class="{ 'dash-conn-card--active': conn.id === activeConnId }"
        >
          <!-- Card header -->
          <div class="dash-conn-card__header">
            <div
              class="dash-conn-badge"
              :style="{ background: (driverColors[conn.driver] ?? '#555') + '22', color: driverColors[conn.driver] ?? '#555' }"
            >{{ driverLabels[conn.driver] ?? '??' }}</div>
            <div style="flex:1;min-width:0">
              <div class="dash-conn-card__name">{{ conn.name }}</div>
              <div class="dash-conn-card__host">{{ conn.host }}{{ conn.port ? ':' + conn.port : '' }}</div>
            </div>
            <!-- Loading spinner -->
            <svg v-if="states[conn.id]?.loading" class="spin" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="color:var(--text-muted);flex-shrink:0"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
          </div>

          <!-- Error -->
          <div v-if="states[conn.id]?.error" class="dash-conn-card__error">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
            {{ states[conn.id].error }}
            <button class="dash-retry-btn" @click="loadOne(conn.id)">Retry</button>
          </div>

          <!-- Stats -->
          <template v-else-if="states[conn.id]?.data">
            <div class="dash-conn-card__db">
              <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/></svg>
              {{ states[conn.id].data!.database }}
            </div>
            <div class="dash-conn-stats">
              <div class="dash-conn-stat">
                <div class="dash-conn-stat__val">{{ states[conn.id].data!.table_count.toLocaleString() }}</div>
                <div class="dash-conn-stat__lbl">Tables</div>
              </div>
              <div class="dash-conn-stat">
                <div class="dash-conn-stat__val">{{ states[conn.id].data!.view_count.toLocaleString() }}</div>
                <div class="dash-conn-stat__lbl">Views</div>
              </div>
              <div class="dash-conn-stat">
                <div class="dash-conn-stat__val">{{ formatBytes(states[conn.id].data!.size_bytes) }}</div>
                <div class="dash-conn-stat__lbl">Size</div>
              </div>
            </div>
            <div class="dash-conn-card__version">{{ states[conn.id].data!.version }}</div>

            <div class="dash-perf-block">
              <div class="dash-perf-block__header">
                <span>Slow query monitor</span>
                <span>≥ {{ states[conn.id].data!.slow_queries.threshold_ms }}ms</span>
              </div>
              <div class="dash-perf-stats">
                <div class="dash-perf-stat">
                  <div class="dash-perf-stat__val">{{ states[conn.id].data!.slow_queries.count.toLocaleString() }}</div>
                  <div class="dash-perf-stat__lbl">Count</div>
                </div>
                <div class="dash-perf-stat">
                  <div class="dash-perf-stat__val">{{ states[conn.id].data!.slow_queries.avg_duration_ms }}ms</div>
                  <div class="dash-perf-stat__lbl">Avg</div>
                </div>
                <div class="dash-perf-stat">
                  <div class="dash-perf-stat__val">{{ states[conn.id].data!.slow_queries.max_duration_ms }}ms</div>
                  <div class="dash-perf-stat__lbl">Max</div>
                </div>
              </div>

              <div v-if="states[conn.id].data!.slow_queries.queries.length > 0" class="dash-slow-list">
                <div
                  v-for="(q, idx) in states[conn.id].data!.slow_queries.queries"
                  :key="`${q.executed_at}-${idx}`"
                  class="dash-slow-item"
                >
                  <div class="dash-slow-item__top">
                    <span class="dash-slow-item__duration">{{ q.duration_ms }}ms</span>
                    <span class="dash-slow-item__meta">{{ q.row_count.toLocaleString() }} rows</span>
                    <span class="dash-slow-item__meta">{{ formatExecutedAt(q.executed_at) }}</span>
                  </div>
                  <div class="dash-slow-item__sql">{{ truncateSql(q.sql) }}</div>
                  <div v-if="q.error" class="dash-slow-item__error">{{ q.error }}</div>
                </div>
              </div>
              <div v-else class="dash-slow-empty">
                No slow queries captured yet for this connection.
              </div>
            </div>

            <!-- Top tables (expandable) -->
            <div v-if="states[conn.id].data!.tables.length > 0">
              <button class="dash-expand-btn" @click="toggleExpand(conn.id)">
                <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"
                  :style="{ transform: states[conn.id]?.expanded ? 'rotate(90deg)' : 'none', transition: 'transform 0.2s' }">
                  <polyline points="9 18 15 12 9 6"/>
                </svg>
                Top tables
              </button>
              <div v-if="states[conn.id]?.expanded" class="dash-top-tables">
                <div
                  v-for="t in states[conn.id].data!.tables.slice(0, 8)"
                  :key="t.name"
                  class="dash-top-row"
                >
                  <span class="dash-top-row__name">{{ t.name }}</span>
                  <span class="dash-top-row__rows">{{ t.row_count.toLocaleString() }} rows</span>
                  <span class="dash-top-row__size">{{ formatBytes(t.size_bytes) }}</span>
                  <div class="dash-top-row__bar-wrap">
                    <div
                      class="dash-top-row__bar"
                      :style="{ width: `${states[conn.id].data!.tables[0].size_bytes > 0 ? (t.size_bytes / states[conn.id].data!.tables[0].size_bytes) * 100 : 100}%` }"
                    />
                  </div>
                </div>
              </div>
            </div>
          </template>

          <!-- Skeleton while loading -->
          <div v-else-if="states[conn.id]?.loading" class="dash-skeleton">
            <div class="dash-skel-line" style="width:60%" />
            <div class="dash-skel-line" style="width:40%;height:32px;margin:8px 0" />
            <div class="dash-skel-line" style="width:80%" />
          </div>

          <!-- Actions -->
          <div class="dash-conn-card__actions">
            <button class="base-btn base-btn--primary base-btn--sm" @click="openInBrowser(conn.id)">
              <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
              Browse
            </button>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="loadOne(conn.id)" :disabled="states[conn.id]?.loading">
              <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.08-4.43"/></svg>
              Refresh
            </button>
          </div>
        </div>
      </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.dash-root { background: var(--bg-body); }

/* Aggregate cards */
.dash-agg {
  display: flex; gap: 14px; margin-bottom: 28px; flex-wrap: wrap;
}
.dash-agg-card {
  flex: 1; min-width: 160px;
  display: flex; align-items: center; gap: 14px;
  padding: 16px 20px;
}
.dash-agg-card__icon {
  width: 38px; height: 38px; border-radius: 8px; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
}
.dash-agg-card__label {
  font-size: 11px; font-weight: 600; text-transform: uppercase;
  letter-spacing: 0.5px; color: var(--text-muted); margin-bottom: 2px;
}
.dash-agg-card__value {
  font-size: 20px; font-weight: 700; color: var(--text-primary);
}

/* Connection cards grid */
.dash-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 16px;
}
.dash-conn-card {
  padding: 18px 20px;
  display: flex; flex-direction: column; gap: 12px;
  transition: border-color var(--dur), transform var(--dur), box-shadow var(--dur);
}
.dash-conn-card:hover { border-color: var(--brand-dim); transform: translateY(-1px); box-shadow: var(--shadow-sm); }
.dash-conn-card--active { border-color: var(--brand); }

.dash-conn-card__header {
  display: flex; align-items: center; gap: 12px;
}
.dash-conn-badge {
  width: 36px; height: 36px; border-radius: 8px;
  display: flex; align-items: center; justify-content: center;
  font-size: 11px; font-weight: 700; flex-shrink: 0;
}
.dash-conn-card__name {
  font-size: 14px; font-weight: 600; color: var(--text-primary);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.dash-conn-card__host {
  font-size: 11px; color: var(--text-muted); margin-top: 1px;
  font-family: var(--mono, monospace);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}

.dash-conn-card__db {
  display: flex; align-items: center; gap: 6px;
  font-size: 12px; color: var(--text-secondary); font-family: var(--mono, monospace);
}

.dash-conn-stats {
  display: flex; gap: 0;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: 8px; overflow: hidden;
}
.dash-conn-stat {
  flex: 1; padding: 10px 14px;
  border-right: 1px solid var(--border);
  text-align: center;
}
.dash-conn-stat:last-child { border-right: none; }
.dash-conn-stat__val {
  font-size: 16px; font-weight: 700; color: var(--text-primary);
  font-variant-numeric: tabular-nums;
}
.dash-conn-stat__lbl {
  font-size: 10px; font-weight: 600; text-transform: uppercase;
  letter-spacing: 0.4px; color: var(--text-muted); margin-top: 2px;
}

.dash-conn-card__version {
  font-size: 11px; color: var(--text-muted);
  font-family: var(--mono, monospace);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}

.dash-perf-block {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: linear-gradient(180deg, rgba(239, 68, 68, 0.06), rgba(239, 68, 68, 0.01));
}
.dash-perf-block__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.4px;
  color: var(--text-muted);
}
.dash-perf-stats {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
}
.dash-perf-stat {
  padding: 10px;
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.45);
  border: 1px solid var(--border);
}
.dash-perf-stat__val {
  font-size: 15px;
  font-weight: 700;
  color: var(--text-primary);
  font-variant-numeric: tabular-nums;
}
.dash-perf-stat__lbl {
  margin-top: 2px;
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.35px;
  color: var(--text-muted);
}
.dash-slow-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.dash-slow-item {
  padding: 10px;
  border-radius: 8px;
  background: var(--bg-panel);
  border: 1px solid var(--border);
}
.dash-slow-item__top {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 6px;
  font-size: 11px;
  color: var(--text-muted);
}
.dash-slow-item__duration {
  color: #dc2626;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}
.dash-slow-item__meta {
  font-variant-numeric: tabular-nums;
}
.dash-slow-item__sql {
  font-size: 12px;
  line-height: 1.5;
  color: var(--text-primary);
  font-family: var(--mono, monospace);
  word-break: break-word;
}
.dash-slow-item__error {
  margin-top: 6px;
  font-size: 11px;
  color: var(--error, #f87171);
}
.dash-slow-empty {
  font-size: 12px;
  color: var(--text-muted);
}

/* Expand top tables */
.dash-expand-btn {
  display: flex; align-items: center; gap: 5px;
  font-size: 11px; font-weight: 600; color: var(--text-muted);
  text-transform: uppercase; letter-spacing: 0.4px;
  background: none; border: none; cursor: pointer; padding: 0;
  transition: color var(--dur);
}
.dash-expand-btn:hover { color: var(--text-primary); }

.dash-top-tables {
  margin-top: 8px;
  display: flex; flex-direction: column; gap: 4px;
}
.dash-top-row {
  display: grid;
  grid-template-columns: 1fr auto auto 80px;
  gap: 8px; align-items: center;
  font-size: 12px;
}
.dash-top-row__name {
  color: var(--text-primary); font-family: var(--mono, monospace);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.dash-top-row__rows { color: var(--text-muted); white-space: nowrap; font-variant-numeric: tabular-nums; }
.dash-top-row__size { color: var(--text-muted); white-space: nowrap; }
.dash-top-row__bar-wrap {
  height: 4px; background: var(--bg-hover); border-radius: 2px; overflow: hidden;
}
.dash-top-row__bar {
  height: 100%; background: var(--brand); border-radius: 2px;
  min-width: 2px; transition: width 0.3s;
}

/* Actions */
.dash-conn-card__actions {
  display: flex; gap: 8px; margin-top: 4px;
}

/* Error */
.dash-conn-card__error {
  font-size: 12px; color: var(--error, #f87171);
  display: flex; align-items: center; gap: 6px;
}
.dash-retry-btn {
  font-size: 11px; color: var(--brand); background: none;
  border: none; cursor: pointer; text-decoration: underline; padding: 0;
}

/* Skeleton */
.dash-skeleton { display: flex; flex-direction: column; gap: 6px; }
.dash-skel-line {
  height: 14px; border-radius: 4px;
  background: var(--bg-hover);
  animation: skel-pulse 1.4s ease-in-out infinite;
}
@keyframes skel-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}

/* Empty */
.dash-empty {
  display: flex; flex-direction: column; align-items: center;
  justify-content: center; gap: 10px;
  padding: 60px 40px; color: var(--text-muted); text-align: center;
}
.dash-empty__title { font-size: 16px; font-weight: 600; color: var(--text-secondary); }
.dash-empty__sub { font-size: 13px; max-width: 300px; line-height: 1.6; }
</style>
