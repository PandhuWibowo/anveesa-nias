<script setup lang="ts">
import { ref, onBeforeUnmount } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'

interface Watcher {
  id: string
  name: string
  connId: number
  sql: string
  intervalSec: number
  lastValue: string
  lastAt: string
  error: string
  running: boolean
  samples: { value: number; time: number }[]
  timerId?: ReturnType<typeof setInterval>
}

const { connections } = useConnections()
const watchers = ref<Watcher[]>([])
const showForm = ref(false)
const form = ref({ name: '', connId: 0, sql: 'SELECT COUNT(*) FROM ', intervalSec: 10 })

const intervalOpts = [
  { value: 5, label: '5s' }, { value: 10, label: '10s' },
  { value: 30, label: '30s' }, { value: 60, label: '1m' },
  { value: 300, label: '5m' },
]

function addWatcher() {
  if (!form.value.connId || !form.value.sql) return
  const w: Watcher = {
    id: crypto.randomUUID(),
    name: form.value.name || form.value.sql.slice(0, 30),
    connId: form.value.connId,
    sql: form.value.sql,
    intervalSec: form.value.intervalSec,
    lastValue: '—', lastAt: '', error: '',
    running: false, samples: [],
  }
  startWatcher(w)
  watchers.value.push(w)
  showForm.value = false
  form.value = { name: '', connId: 0, sql: 'SELECT COUNT(*) FROM ', intervalSec: 10 }
}

async function poll(w: Watcher) {
  if (w.running) return
  w.running = true
  w.error = ''
  try {
    const { data } = await axios.post(`/api/connections/${w.connId}/query`, { sql: w.sql })
    const row = data.rows?.[0]
    const val = row ? String(Object.values(row)[0] ?? row[0] ?? '?') : '?'
    w.lastValue = val
    w.lastAt = new Date().toLocaleTimeString()
    const num = parseFloat(val)
    if (!isNaN(num)) {
      w.samples.push({ value: num, time: Date.now() })
      if (w.samples.length > 60) w.samples.shift()
    }
  } catch (e: any) {
    w.error = e?.response?.data?.error ?? 'Query failed'
  } finally {
    w.running = false
  }
}

function startWatcher(w: Watcher) {
  poll(w)
  w.timerId = setInterval(() => poll(w), w.intervalSec * 1000)
}

function stopWatcher(w: Watcher) {
  if (w.timerId) clearInterval(w.timerId)
  w.timerId = undefined; w.running = false
}

function removeWatcher(w: Watcher) {
  stopWatcher(w)
  watchers.value = watchers.value.filter((x) => x.id !== w.id)
}

function sparkPath(samples: { value: number }[]): string {
  if (samples.length < 2) return ''
  const vals = samples.map((s) => s.value)
  const min = Math.min(...vals)
  const max = Math.max(...vals, min + 1)
  const W = 160, H = 44
  const pts = samples.map((s, i) => {
    const x = (i / (samples.length - 1)) * W
    const y = H - ((s.value - min) / (max - min)) * H * 0.9
    return `${x.toFixed(1)},${y.toFixed(1)}`
  })
  return `M ${pts.join(' L ')}`
}

onBeforeUnmount(() => watchers.value.forEach(stopWatcher))

function connName(id: number) {
  return connections.value.find((c) => c.id === id)?.name ?? `#${id}`
}
</script>

<template>
  <div class="wv-root">
    <div class="wv-scroll">
      <!-- Header -->
      <div class="wv-header">
        <div>
          <div class="wv-title">Live Table Watcher</div>
          <div class="wv-sub">Poll any query on an interval and watch values change in real time.</div>
        </div>
        <div style="flex:1"/>
        <button class="base-btn base-btn--primary base-btn--sm" @click="showForm=true">+ Add Watcher</button>
      </div>

      <!-- Watchers grid -->
      <div class="wv-grid">
        <div v-if="watchers.length === 0" class="wv-empty">
          No watchers yet. Add one to monitor a query live.
        </div>
        <div v-for="w in watchers" :key="w.id" class="wv-card">
          <div class="wv-card-head">
            <div class="wv-card-name">{{ w.name }}</div>
            <span class="wv-conn-pill">{{ connName(w.connId) }}</span>
            <div style="flex:1"/>
            <span class="wv-interval-pill">every {{ w.intervalSec }}s</span>
            <div class="wv-dot" :class="w.running ? 'wv-dot--active' : 'wv-dot--idle'" />
            <button class="base-btn base-btn--ghost base-btn--sm" @click="poll(w)" :disabled="w.running">↻</button>
            <button class="base-btn base-btn--ghost base-btn--sm" style="color:var(--danger)" @click="removeWatcher(w)">×</button>
          </div>

          <div class="wv-value-row">
            <div class="wv-value">{{ w.lastValue }}</div>
            <span class="wv-at">{{ w.lastAt }}</span>
          </div>

          <!-- Sparkline -->
          <div class="wv-spark-wrap" v-if="w.samples.length > 1">
            <svg width="160" height="44" viewBox="0 0 160 44" class="wv-spark">
              <defs>
                <linearGradient id="sg" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stop-color="var(--brand)" stop-opacity="0.3"/>
                  <stop offset="100%" stop-color="var(--brand)" stop-opacity="0"/>
                </linearGradient>
              </defs>
              <path :d="sparkPath(w.samples) + ' L 160,44 L 0,44 Z'" fill="url(#sg)" />
              <path :d="sparkPath(w.samples)" fill="none" stroke="var(--brand)" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
            <div class="wv-spark-labels">
              <span>{{ Math.min(...w.samples.map(s=>s.value)) }}</span>
              <span>{{ Math.max(...w.samples.map(s=>s.value)) }}</span>
            </div>
          </div>

          <div v-if="w.error" class="wv-error">{{ w.error }}</div>
          <pre class="wv-sql">{{ w.sql }}</pre>
        </div>
      </div>
    </div>

    <!-- Form modal -->
    <Teleport to="body">
      <div v-if="showForm" class="wv-overlay" @click.self="showForm=false">
        <div class="wv-modal">
          <div class="wv-modal-head">
            <span>New Watcher</span>
            <button class="sl-close" @click="showForm=false">×</button>
          </div>
          <div class="wv-modal-body">
            <div class="form-group">
              <label class="form-label">Label</label>
              <input v-model="form.name" class="base-input" placeholder="Active orders count" />
            </div>
            <div class="form-group">
              <label class="form-label">Connection</label>
              <select v-model.number="form.connId" class="base-input">
                <option :value="0">— select —</option>
                <option v-for="c in connections" :key="c.id" :value="c.id">{{ c.name }}</option>
              </select>
            </div>
            <div class="form-group">
              <label class="form-label">SQL <span class="form-hint" style="display:inline">(should return a single value)</span></label>
              <textarea v-model="form.sql" class="base-input" rows="3" style="font-family:monospace;font-size:12px;resize:vertical" />
            </div>
            <div class="form-group">
              <label class="form-label">Interval</label>
              <select v-model.number="form.intervalSec" class="base-input">
                <option v-for="o in intervalOpts" :key="o.value" :value="o.value">{{ o.label }}</option>
              </select>
            </div>
          </div>
          <div class="wv-modal-foot">
            <button class="base-btn base-btn--ghost" @click="showForm=false">Cancel</button>
            <button class="base-btn base-btn--primary" :disabled="!form.connId || !form.sql" @click="addWatcher">Add</button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.wv-root { width:100%; height:100%; display:flex; flex-direction:column; overflow:hidden; }
.wv-scroll { flex:1; min-height:0; overflow-y:auto; padding:24px 28px 40px; display:flex; flex-direction:column; gap:16px; }
.wv-header { display:flex; align-items:flex-start; gap:12px; flex-wrap:wrap; }
.wv-title { font-size:20px; font-weight:700; color:var(--text-primary); }
.wv-sub { font-size:13px; color:var(--text-muted); margin-top:3px; }
.wv-empty { text-align:center; color:var(--text-muted); padding:40px; font-size:13px; }
.wv-grid { display:grid; grid-template-columns:repeat(auto-fill,minmax(260px,1fr)); gap:12px; }
.wv-card { background:var(--bg-elevated); border:1px solid var(--border); border-radius:10px; padding:14px; display:flex; flex-direction:column; gap:8px; }
.wv-card-head { display:flex; align-items:center; gap:6px; flex-wrap:wrap; }
.wv-card-name { font-weight:700; font-size:13px; color:var(--text-primary); }
.wv-conn-pill { font-size:10px; padding:1px 6px; border-radius:4px; background:var(--brand-dim); color:var(--brand); font-weight:600; }
.wv-interval-pill { font-size:10px; color:var(--text-muted); padding:1px 5px; background:var(--bg-body); border-radius:4px; }
.wv-dot { width:7px; height:7px; border-radius:50%; }
.wv-dot--active { background:#4ade80; animation:pulse 1.2s infinite; }
.wv-dot--idle { background:var(--text-muted); }
@keyframes pulse { 0%,100%{opacity:1} 50%{opacity:0.3} }
.wv-value-row { display:flex; align-items:baseline; gap:10px; }
.wv-value { font-size:32px; font-weight:900; color:var(--text-primary); font-variant-numeric:tabular-nums; line-height:1; }
.wv-at { font-size:11px; color:var(--text-muted); }
.wv-spark-wrap { position:relative; }
.wv-spark { display:block; width:100%; height:44px; }
.wv-spark-labels { display:flex; justify-content:space-between; font-size:9px; color:var(--text-muted); padding:0 2px; }
.wv-error { font-size:11.5px; color:#f87171; padding:5px 8px; background:rgba(248,113,113,0.08); border-radius:5px; }
.wv-sql { margin:0; font-family:var(--mono,monospace); font-size:11px; color:var(--text-muted); white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
.sl-close { background:transparent; border:none; font-size:20px; color:var(--text-muted); cursor:pointer; padding:0 4px; line-height:1; }
.wv-overlay { position:fixed; inset:0; background:rgba(0,0,0,0.55); display:flex; align-items:center; justify-content:center; z-index:1100; }
.wv-modal { background:var(--bg-elevated); border:1px solid var(--border); border-radius:10px; width:min(460px,94vw); display:flex; flex-direction:column; box-shadow:0 24px 64px rgba(0,0,0,0.55); }
.wv-modal-head { display:flex; align-items:center; justify-content:space-between; padding:12px 16px; border-bottom:1px solid var(--border); font-size:14px; font-weight:700; color:var(--text-primary); }
.wv-modal-body { padding:16px; display:flex; flex-direction:column; gap:12px; }
.wv-modal-foot { display:flex; justify-content:flex-end; gap:8px; padding:12px 16px; border-top:1px solid var(--border); }
</style>
