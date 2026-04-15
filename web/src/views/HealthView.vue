<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import axios from 'axios'

interface HealthResult {
  conn_id: number; conn_name: string; driver: string
  status: string; latency_ms: number; error?: string
  pool: { open_conns: number; in_use: number; idle: number; max_open: number }
}

const results = ref<HealthResult[]>([])
const loading = ref(false)
const history = ref<Record<number, number[]>>({}) // connID → latency samples
const autoRefresh = ref(true)
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

onMounted(() => {
  load()
  timer = setInterval(() => { if (autoRefresh.value) load() }, 5000)
})
onBeforeUnmount(() => clearInterval(timer))

const statusColor = (s: string) =>
  s === 'ok' ? '#4ade80' : s === 'error' ? '#f87171' : '#fbbf24'
</script>

<template>
  <div class="hv-root">
    <div class="hv-scroll">
      <!-- Header -->
      <div class="hv-header">
        <div>
          <div class="hv-title">Connection Health</div>
          <div class="hv-sub">Live latency and pool stats for all connections. Refreshes every 5s.</div>
        </div>
        <div style="flex:1"/>
        <label class="hv-auto-toggle">
          <input type="checkbox" v-model="autoRefresh" />
          Auto-refresh
        </label>
        <button class="base-btn base-btn--ghost base-btn--sm" @click="load" :disabled="loading">
          <svg :class="loading ? 'spin' : ''" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.08-4.43"/></svg>
          Refresh
        </button>
      </div>

      <!-- Cards grid -->
      <div class="hv-grid">
        <div v-if="results.length === 0 && !loading" class="hv-empty">No connections configured.</div>
        <div v-for="r in results" :key="r.conn_id" class="hv-card" :class="`hv-card--${r.status}`">
          <!-- Status dot -->
          <div class="hv-card-head">
            <div class="hv-dot" :style="{ background: statusColor(r.status) }" />
            <div class="hv-conn-name">{{ r.conn_name }}</div>
            <div class="hv-driver-pill">{{ r.driver }}</div>
          </div>

          <!-- Latency + sparkline -->
          <div class="hv-metric-row">
            <div class="hv-metric">
              <div class="hv-metric-val" :style="{ color: r.status === 'ok' ? '#4ade80' : '#f87171' }">
                {{ r.status === 'ok' ? r.latency_ms + 'ms' : '—' }}
              </div>
              <div class="hv-metric-lbl">Latency</div>
            </div>
            <div class="hv-metric">
              <div class="hv-metric-val">{{ r.pool?.open_conns ?? '—' }}</div>
              <div class="hv-metric-lbl">Open</div>
            </div>
            <div class="hv-metric">
              <div class="hv-metric-val">{{ r.pool?.in_use ?? '—' }}</div>
              <div class="hv-metric-lbl">In Use</div>
            </div>
            <div class="hv-metric">
              <div class="hv-metric-val">{{ r.pool?.idle ?? '—' }}</div>
              <div class="hv-metric-lbl">Idle</div>
            </div>
          </div>

          <!-- Sparkline -->
          <div class="hv-spark-wrap" v-if="history[r.conn_id]?.length > 1">
            <svg width="80" height="28" viewBox="0 0 80 28" class="hv-spark">
              <path :d="sparkPath(history[r.conn_id])" fill="none" stroke="#4ade80" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </div>

          <!-- Error -->
          <div v-if="r.error" class="hv-error">{{ r.error }}</div>

          <!-- Pool bar -->
          <div v-if="r.pool?.open_conns > 0" class="hv-pool-bar">
            <div class="hv-pool-used" :style="{ width: (r.pool.in_use / Math.max(r.pool.open_conns, 1) * 100) + '%' }" />
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.hv-root { width:100%; height:100%; display:flex; flex-direction:column; overflow:hidden; }
.hv-scroll { flex:1; min-height:0; overflow-y:auto; padding:24px 28px 40px; display:flex; flex-direction:column; gap:16px; }
.hv-header { display:flex; align-items:flex-start; gap:12px; flex-wrap:wrap; }
.hv-title { font-size:20px; font-weight:700; color:var(--text-primary); }
.hv-sub { font-size:13px; color:var(--text-muted); margin-top:3px; }
.hv-auto-toggle { display:flex; align-items:center; gap:6px; font-size:12px; color:var(--text-muted); cursor:pointer; }
.hv-empty { color:var(--text-muted); text-align:center; padding:40px; }
.hv-grid { display:grid; grid-template-columns:repeat(auto-fill,minmax(240px,1fr)); gap:12px; }
.hv-card { background:var(--bg-elevated); border:1px solid var(--border); border-radius:10px; padding:14px; display:flex; flex-direction:column; gap:10px; }
.hv-card--ok { border-left:3px solid #4ade80; }
.hv-card--error { border-left:3px solid #f87171; }
.hv-card--unknown { border-left:3px solid var(--text-muted); }
.hv-card-head { display:flex; align-items:center; gap:8px; }
.hv-dot { width:8px; height:8px; border-radius:50%; flex-shrink:0; }
.hv-conn-name { font-weight:700; font-size:13px; color:var(--text-primary); flex:1; }
.hv-driver-pill { font-size:10px; padding:1px 6px; border-radius:4px; background:var(--brand-dim); color:var(--brand); font-weight:700; }
.hv-metric-row { display:flex; gap:8px; }
.hv-metric { flex:1; text-align:center; padding:6px 0; background:var(--bg-body); border-radius:6px; }
.hv-metric-val { font-size:16px; font-weight:700; color:var(--text-primary); font-variant-numeric:tabular-nums; }
.hv-metric-lbl { font-size:9.5px; color:var(--text-muted); text-transform:uppercase; letter-spacing:0.4px; }
.hv-spark-wrap { display:flex; justify-content:flex-end; }
.hv-spark { display:block; }
.hv-error { font-size:11.5px; color:#f87171; font-family:var(--mono,monospace); background:rgba(248,113,113,0.08); border-radius:5px; padding:6px 8px; }
.hv-pool-bar { height:4px; background:var(--bg-body); border-radius:2px; overflow:hidden; }
.hv-pool-used { height:100%; background:var(--brand); border-radius:2px; transition:width 0.4s; }
</style>
