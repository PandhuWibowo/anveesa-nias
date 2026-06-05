<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import axios from 'axios'

interface HealthResult {
  conn_id: number; conn_name: string; driver: string
  status: string; latency_ms: number; error?: string
  pool: { open_conns: number; in_use: number; idle: number; max_open: number }
}

const results = ref<HealthResult[]>([])
const loading = ref(false)
const history = ref<Record<number, number[]>>({})
const autoRefresh = ref(true)
const filter = ref<'all' | 'healthy' | 'issues' | 'na'>('all')
let timer: ReturnType<typeof setInterval>

async function load() {
  loading.value = true
  try {
    const { data } = await axios.get<HealthResult[]>('/api/health')
    results.value = data ?? []
    for (const r of results.value) {
      if (!history.value[r.conn_id]) history.value[r.conn_id] = []
      const h = history.value[r.conn_id]
      h.push(r.status === 'ok' ? r.latency_ms : -1)
      if (h.length > 20) h.shift()
    }
  } finally { loading.value = false }
}

function isNA(r: HealthResult) {
  return !!(r.error && r.error.toLowerCase().includes('does not use sql'))
}

function isIssue(r: HealthResult) {
  return r.status !== 'ok' && !isNA(r)
}

const summary = computed(() => ({
  healthy: results.value.filter(r => r.status === 'ok').length,
  issues:  results.value.filter(r => isIssue(r)).length,
  na:      results.value.filter(r => isNA(r)).length,
}))

const sorted = computed(() => {
  const ok    = results.value.filter(r => r.status === 'ok')
  const error = results.value.filter(r => isIssue(r))
  const na    = results.value.filter(r => isNA(r))
  const all   = [...ok, ...error, ...na]
  if (filter.value === 'healthy') return ok
  if (filter.value === 'issues')  return error
  if (filter.value === 'na')      return na
  return all
})

function sparkPath(samples: number[]): string {
  const ok = samples.filter((v) => v >= 0)
  if (!ok.length) return ''
  const max = Math.max(...ok, 1)
  const w = 80, h = 28
  const pts = samples.map((v, i) => {
    const x = (i / (samples.length - 1)) * w
    const y = v < 0 ? h : h - (v / max) * h * 0.85
    return `${x},${y}`
  })
  return `M ${pts.join(' L ')}`
}

function latencyColor(ms: number) {
  if (ms <= 10)  return '#4ade80'
  if (ms <= 50)  return '#a3e635'
  if (ms <= 150) return '#fbbf24'
  return '#f87171'
}

onMounted(() => {
  load()
  timer = setInterval(() => { if (autoRefresh.value) load() }, 5000)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <div class="page-shell hv-root">
    <div class="page-scroll hv-scroll">
      <div class="page-stack">

        <!-- Hero -->
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Observability</div>
            <div class="page-title">Connection Health</div>
            <div class="page-subtitle">Live latency, pool pressure, and connectivity status for all database endpoints. Refreshes every 5 seconds.</div>
          </div>
          <div class="page-hero__actions">
            <label class="hv-auto-toggle">
              <input type="checkbox" v-model="autoRefresh" />
              Auto-refresh
            </label>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="load" :disabled="loading">
              <svg :class="loading ? 'spin' : ''" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.08-4.43"/></svg>
              Refresh
            </button>
          </div>
        </section>

        <!-- Summary + filter bar -->
        <div class="hv-bar" v-if="results.length">
          <div class="hv-summary">
            <span class="hv-summary-chip hv-summary-chip--ok">
              <span class="hv-dot hv-dot--ok" />
              {{ summary.healthy }} Healthy
            </span>
            <span v-if="summary.issues" class="hv-summary-chip hv-summary-chip--error">
              <span class="hv-dot hv-dot--error" />
              {{ summary.issues }} Issue{{ summary.issues > 1 ? 's' : '' }}
            </span>
            <span v-if="summary.na" class="hv-summary-chip hv-summary-chip--na">
              {{ summary.na }} Not applicable
            </span>
          </div>
          <div class="hv-filters">
            <button class="hv-filter-btn" :class="{ active: filter === 'all' }"     @click="filter = 'all'">All</button>
            <button class="hv-filter-btn" :class="{ active: filter === 'healthy' }" @click="filter = 'healthy'">Healthy</button>
            <button class="hv-filter-btn" :class="{ active: filter === 'issues' }"  @click="filter = 'issues'">
              Issues
              <span v-if="summary.issues" class="hv-filter-badge">{{ summary.issues }}</span>
            </button>
            <button class="hv-filter-btn" :class="{ active: filter === 'na' }"      @click="filter = 'na'">N/A</button>
          </div>
        </div>

        <!-- Empty -->
        <div v-if="results.length === 0 && !loading" class="hv-empty">No connections configured.</div>
        <div v-if="sorted.length === 0 && results.length > 0" class="hv-empty">No connections match this filter.</div>

        <!-- Cards grid -->
        <div class="page-grid page-grid--cards hv-grid">
          <div
            v-for="r in sorted"
            :key="r.conn_id"
            class="page-card hv-card"
            :class="isNA(r) ? 'hv-card--na' : isIssue(r) ? 'hv-card--error' : 'hv-card--ok'"
          >
            <!-- Head -->
            <div class="hv-card-head">
              <span class="hv-status-dot" :class="isNA(r) ? 'hv-status-dot--na' : isIssue(r) ? 'hv-status-dot--error' : 'hv-status-dot--ok'" />
              <span class="hv-conn-name">{{ r.conn_name }}</span>
              <span class="hv-driver-pill">{{ r.driver }}</span>
            </div>

            <!-- N/A state: not a SQL connection -->
            <template v-if="isNA(r)">
              <div class="hv-na-notice">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
                Not a SQL connection — pool metrics unavailable
              </div>
            </template>

            <!-- Error state -->
            <template v-else-if="isIssue(r)">
              <div class="hv-metrics-row">
                <div class="hv-metric">
                  <div class="hv-metric-val hv-metric-val--muted">—</div>
                  <div class="hv-metric-lbl">Latency</div>
                </div>
                <div class="hv-metric">
                  <div class="hv-metric-val hv-metric-val--muted">{{ r.pool?.open_conns ?? '—' }}</div>
                  <div class="hv-metric-lbl">Open</div>
                </div>
                <div class="hv-metric">
                  <div class="hv-metric-val hv-metric-val--muted">{{ r.pool?.in_use ?? '—' }}</div>
                  <div class="hv-metric-lbl">In Use</div>
                </div>
                <div class="hv-metric">
                  <div class="hv-metric-val hv-metric-val--muted">{{ r.pool?.idle ?? '—' }}</div>
                  <div class="hv-metric-lbl">Idle</div>
                </div>
              </div>
              <div class="hv-error-badge">
                <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>
                {{ r.error ?? 'Disconnected' }}
              </div>
            </template>

            <!-- Healthy state -->
            <template v-else>
              <div class="hv-metrics-row">
                <div class="hv-metric">
                  <div class="hv-metric-val" :style="{ color: latencyColor(r.latency_ms) }">{{ r.latency_ms }}ms</div>
                  <div class="hv-metric-lbl">Latency</div>
                </div>
                <div class="hv-metric">
                  <div class="hv-metric-val">{{ r.pool?.open_conns ?? 0 }}</div>
                  <div class="hv-metric-lbl">Open</div>
                </div>
                <div class="hv-metric">
                  <div class="hv-metric-val">{{ r.pool?.in_use ?? 0 }}</div>
                  <div class="hv-metric-lbl">In Use</div>
                </div>
                <div class="hv-metric">
                  <div class="hv-metric-val">{{ r.pool?.idle ?? 0 }}</div>
                  <div class="hv-metric-lbl">Idle</div>
                </div>
              </div>

              <!-- Sparkline -->
              <div class="hv-spark-row" v-if="history[r.conn_id]?.length > 1">
                <svg width="100%" height="28" viewBox="0 0 80 28" preserveAspectRatio="none" class="hv-spark">
                  <path :d="sparkPath(history[r.conn_id])" fill="none" :stroke="latencyColor(r.latency_ms)" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
                </svg>
              </div>

              <!-- Pool pressure bar -->
              <div v-if="(r.pool?.open_conns ?? 0) > 0" class="hv-pool-wrap">
                <div class="hv-pool-bar">
                  <div class="hv-pool-used" :style="{ width: (r.pool.in_use / Math.max(r.pool.open_conns, 1) * 100) + '%' }" />
                </div>
                <span class="hv-pool-label">Pool usage</span>
              </div>
            </template>

          </div>
        </div>

      </div>
    </div>
  </div>
</template>

<style scoped>
.hv-root { background: var(--bg-body); }
.hv-auto-toggle { display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--text-muted); cursor: pointer; }

/* ── Summary + filter bar ───────────────────────────────────────────────────── */
.hv-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
  padding: 0 2px 4px;
}

.hv-summary {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.hv-summary-chip {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  font-weight: 600;
  padding: 3px 10px;
  border-radius: 20px;
}

.hv-summary-chip--ok    { background: rgba(74,222,128,0.1); color: #4ade80; }
.hv-summary-chip--error { background: rgba(248,113,113,0.1); color: #f87171; }
.hv-summary-chip--na    { background: var(--bg-elevated); color: var(--text-muted); }

.hv-dot { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; }
.hv-dot--ok    { background: #4ade80; }
.hv-dot--error { background: #f87171; }

.hv-filters {
  display: flex;
  gap: 4px;
  background: var(--bg-elevated);
  padding: 3px;
  border-radius: 8px;
}

.hv-filter-btn {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 4px 12px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--text-muted);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}

.hv-filter-btn:hover { background: var(--bg-surface); color: var(--text-primary); }
.hv-filter-btn.active { background: var(--bg-surface); color: var(--text-primary); font-weight: 600; box-shadow: 0 1px 3px rgba(0,0,0,0.12); }

.hv-filter-badge {
  background: #f87171;
  color: #fff;
  font-size: 10px;
  font-weight: 700;
  padding: 0 5px;
  border-radius: 10px;
  min-width: 16px;
  text-align: center;
}

/* ── Grid ───────────────────────────────────────────────────────────────────── */
.hv-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 12px; }
.hv-empty { color: var(--text-muted); text-align: center; padding: 48px 0; font-size: 13px; }

/* ── Cards ──────────────────────────────────────────────────────────────────── */
.hv-card {
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  transition: box-shadow 0.15s;
}

.hv-card:hover { box-shadow: 0 2px 12px rgba(0,0,0,0.12); }
.hv-card--ok    { border-left: 3px solid #4ade80; }
.hv-card--error { border-left: 3px solid #f87171; }
.hv-card--na    { border-left: 3px solid var(--border); opacity: 0.75; }

/* ── Card head ──────────────────────────────────────────────────────────────── */
.hv-card-head { display: flex; align-items: center; gap: 8px; min-width: 0; }

.hv-status-dot {
  width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0;
}
.hv-status-dot--ok    { background: #4ade80; box-shadow: 0 0 0 3px rgba(74,222,128,0.2); }
.hv-status-dot--error { background: #f87171; box-shadow: 0 0 0 3px rgba(248,113,113,0.2); }
.hv-status-dot--na    { background: var(--text-muted); }

.hv-conn-name {
  font-weight: 700;
  font-size: 13px;
  color: var(--text-primary);
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.hv-driver-pill {
  flex-shrink: 0;
  font-size: 10px;
  padding: 1px 6px;
  border-radius: 4px;
  background: var(--brand-dim);
  color: var(--brand);
  font-weight: 700;
  text-transform: lowercase;
}

/* ── Metrics ────────────────────────────────────────────────────────────────── */
.hv-metrics-row { display: flex; gap: 6px; }
.hv-metric { flex: 1; text-align: center; padding: 6px 0; background: var(--bg-body); border-radius: 6px; }
.hv-metric-val { font-size: 15px; font-weight: 700; color: var(--text-primary); font-variant-numeric: tabular-nums; }
.hv-metric-val--muted { color: var(--text-muted); }
.hv-metric-lbl { font-size: 9px; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.4px; margin-top: 1px; }

/* ── Sparkline ──────────────────────────────────────────────────────────────── */
.hv-spark-row { height: 28px; }
.hv-spark { display: block; width: 100%; }

/* ── Pool bar ───────────────────────────────────────────────────────────────── */
.hv-pool-wrap { display: flex; align-items: center; gap: 8px; }
.hv-pool-bar { flex: 1; height: 4px; background: var(--bg-body); border-radius: 2px; overflow: hidden; }
.hv-pool-used { height: 100%; background: var(--brand); border-radius: 2px; transition: width 0.4s; }
.hv-pool-label { font-size: 9.5px; color: var(--text-muted); white-space: nowrap; }

/* ── Error badge ────────────────────────────────────────────────────────────── */
.hv-error-badge {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 11.5px;
  font-weight: 500;
  color: #f87171;
  background: rgba(248,113,113,0.08);
  border: 1px solid rgba(248,113,113,0.2);
  border-radius: 6px;
  padding: 5px 8px;
}

/* ── N/A notice ─────────────────────────────────────────────────────────────── */
.hv-na-notice {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11.5px;
  color: var(--text-muted);
  background: var(--bg-body);
  border-radius: 6px;
  padding: 6px 8px;
}

/* ── Responsive ─────────────────────────────────────────────────────────────── */
@media (max-width: 768px) {
  .hv-bar { flex-direction: column; align-items: flex-start; }
  .hv-grid { grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); }
}

@media (max-width: 480px) {
  .hv-grid { grid-template-columns: 1fr; }
  .hv-filters { width: 100%; justify-content: space-between; }
}
</style>
