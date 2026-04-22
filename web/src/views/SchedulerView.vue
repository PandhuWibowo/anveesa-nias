<script setup lang="ts">
import { ref, onMounted } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'

interface Schedule {
  id: number; name: string; conn_id: number; sql: string
  kind: string; ai_prompt: string; created_by: number
  interval_min: number; alert_condition: string; alert_threshold: number
  enabled: boolean; last_run_at: string; next_run_at: string; created_at: string
}
interface ScheduleRun {
  id: number; schedule_id: number; row_count: number
  summary: string; error: string; alerted: boolean; ran_at: string
}

const { connections } = useConnections()
const schedules = ref<Schedule[]>([])
const loading = ref(false)
const showForm = ref(false)
const selectedRuns = ref<ScheduleRun[]>([])
const runsFor = ref<number | null>(null)

const form = ref<Partial<Schedule>>({
  name: '', conn_id: 0, sql: '', kind: 'query', ai_prompt: '', interval_min: 60,
  alert_condition: '', alert_threshold: 0, enabled: true,
})

const kindOpts = [
  { value: 'query', label: 'Query Check' },
  { value: 'ai_summary', label: 'AI Summary' },
]

const alertOpts = [
  { value: '', label: 'None' },
  { value: 'row_count_gt', label: 'Row count >' },
  { value: 'row_count_lt', label: 'Row count <' },
  { value: 'row_count_eq', label: 'Row count =' },
]

const intervalOpts = [
  { value: 5, label: '5 min' },
  { value: 15, label: '15 min' },
  { value: 30, label: '30 min' },
  { value: 60, label: '1 hour' },
  { value: 360, label: '6 hours' },
  { value: 1440, label: '24 hours' },
]

async function load() {
  loading.value = true
  try {
    const { data } = await axios.get<Schedule[]>('/api/schedules')
    schedules.value = data
  } finally { loading.value = false }
}

async function save() {
  if (!form.value.name || !form.value.sql || !form.value.conn_id) return
  if (form.value.id) {
    await axios.put(`/api/schedules/${form.value.id}`, form.value)
  } else {
    await axios.post('/api/schedules', form.value)
  }
  showForm.value = false
  resetForm()
  load()
}

async function toggle(s: Schedule) {
  await axios.put(`/api/schedules/${s.id}`, { ...s, enabled: !s.enabled })
  load()
}

async function del(s: Schedule) {
  if (!confirm(`Delete schedule "${s.name}"?`)) return
  await axios.delete(`/api/schedules/${s.id}`)
  load()
}

async function runNow(s: Schedule) {
  await axios.post(`/api/schedules/${s.id}/run`)
  load()
}

async function viewRuns(s: Schedule) {
  runsFor.value = s.id
  const { data } = await axios.get<ScheduleRun[]>(`/api/schedules/${s.id}/runs`)
  selectedRuns.value = data
}

function editSchedule(s: Schedule) {
  form.value = { ...s }
  showForm.value = true
}

function resetForm() {
  form.value = { name: '', conn_id: 0, sql: '', kind: 'query', ai_prompt: '', interval_min: 60, alert_condition: '', alert_threshold: 0, enabled: true }
}

function connName(id: number) {
  return connections.value.find((c) => c.id === id)?.name ?? `#${id}`
}

onMounted(load)
</script>

<template>
  <div class="page-shell sc-root">
    <div class="page-scroll sc-scroll">
      <div class="page-stack">
      <section class="page-hero">
        <div class="page-hero__content">
          <div class="page-kicker">Automation</div>
          <div class="page-title">Scheduled Queries</div>
          <div class="page-subtitle">Run recurring SQL jobs, monitor thresholds, and inspect execution history without leaving the app.</div>
        </div>
        <div class="page-hero__actions">
          <button class="base-btn base-btn--primary base-btn--sm" @click="resetForm(); showForm=true">+ New Schedule</button>
        </div>
      </section>

      <!-- List -->
      <div class="page-grid sc-list">
        <div v-if="loading" class="sc-empty">
          <svg class="spin" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
        </div>
        <div v-else-if="schedules.length === 0" class="sc-empty">No schedules yet.</div>
        <div v-for="s in schedules" :key="s.id" class="page-card sc-item" :class="{ 'sc-item--off': !s.enabled }">
          <div class="sc-item-head">
            <div class="sc-dot" :class="s.enabled ? 'sc-dot--on' : 'sc-dot--off'" />
            <div class="sc-item-info">
              <span class="sc-item-name">{{ s.name }}</span>
              <span class="sc-item-conn">{{ connName(s.conn_id) }}</span>
              <span class="sc-item-kind">{{ s.kind === 'ai_summary' ? 'AI Summary' : 'Query Check' }}</span>
              <span class="sc-item-interval">every {{ s.interval_min }}m</span>
              <span v-if="s.alert_condition" class="sc-alert-pill">
                🔔 {{ alertOpts.find(a => a.value === s.alert_condition)?.label }} {{ s.alert_threshold }}
              </span>
            </div>
            <div class="sc-item-times">
              <span v-if="s.last_run_at" class="sc-time">Last: {{ new Date(s.last_run_at).toLocaleString() }}</span>
              <span v-if="s.next_run_at" class="sc-time">Next: {{ new Date(s.next_run_at).toLocaleString() }}</span>
            </div>
            <div class="sc-item-actions">
              <button class="base-btn base-btn--ghost base-btn--sm" @click="runNow(s)" title="Run now">▶</button>
              <button class="base-btn base-btn--ghost base-btn--sm" @click="viewRuns(s)" title="View runs">History</button>
              <button class="base-btn base-btn--ghost base-btn--sm" @click="toggle(s)">
                {{ s.enabled ? 'Disable' : 'Enable' }}
              </button>
              <button class="base-btn base-btn--ghost base-btn--sm" @click="editSchedule(s)">Edit</button>
              <button class="base-btn base-btn--ghost base-btn--sm" style="color:var(--danger)" @click="del(s)">Delete</button>
            </div>
          </div>
          <pre class="sc-sql">{{ s.sql }}</pre>
        </div>
      </div>

      <!-- Run history panel -->
      <div v-if="runsFor !== null" class="page-card sc-runs">
        <div class="sc-runs-header">
          <span>Run History</span>
          <button class="cp-close" @click="runsFor=null; selectedRuns=[]">×</button>
        </div>
        <div v-if="selectedRuns.length === 0" class="sc-empty" style="padding:12px">No runs yet.</div>
        <div v-for="run in selectedRuns" :key="run.id" class="sc-run-row" :class="{ 'sc-run--err': run.error, 'sc-run--alerted': run.alerted }">
          <span class="sc-time">{{ new Date(run.ran_at).toLocaleString() }}</span>
          <span class="sc-run-rows">{{ run.row_count }} rows</span>
          <span v-if="run.alerted" class="sc-alert-pill">🔔 Alert triggered</span>
          <span v-if="run.summary" class="sc-run-summary">{{ run.summary }}</span>
          <span v-if="run.error" class="sc-run-err">{{ run.error }}</span>
        </div>
      </div>
      </div>
    </div>

    <!-- Form modal -->
    <Teleport to="body">
      <div v-if="showForm" class="sc-overlay" @click.self="showForm=false">
        <div class="page-modal sc-modal">
          <div class="page-modal__head sc-modal-header">
            <span>{{ form.id ? 'Edit Schedule' : 'New Schedule' }}</span>
            <button class="cp-close" @click="showForm=false">×</button>
          </div>
          <div class="page-modal__body sc-modal-body">
            <div class="form-group">
              <label class="form-label">Name</label>
              <input v-model="form.name" class="base-input" placeholder="Daily row count check" />
            </div>
            <div class="form-group">
              <label class="form-label">Connection</label>
              <select v-model.number="form.conn_id" class="base-input">
                <option :value="0">— select —</option>
                <option v-for="c in connections" :key="c.id" :value="c.id">{{ c.name }}</option>
              </select>
            </div>
            <div class="form-group">
              <label class="form-label">Schedule Type</label>
              <select v-model="form.kind" class="base-input">
                <option v-for="o in kindOpts" :key="o.value" :value="o.value">{{ o.label }}</option>
              </select>
            </div>
            <div class="form-group">
              <label class="form-label">{{ form.kind === 'ai_summary' ? 'Read-only SQL' : 'SQL' }}</label>
              <textarea v-model="form.sql" class="base-input" rows="4" :placeholder="form.kind === 'ai_summary' ? 'SELECT status, COUNT(*) AS total FROM orders GROUP BY status' : 'SELECT COUNT(*) FROM users'" style="font-family:monospace;font-size:12px;resize:vertical" />
            </div>
            <div v-if="form.kind === 'ai_summary'" class="form-group">
              <label class="form-label">AI Summary Prompt</label>
              <textarea v-model="form.ai_prompt" class="base-input" rows="3" placeholder="Summarize the biggest takeaway from this result and call out any unusual changes." style="resize:vertical" />
            </div>
            <div style="display:grid;grid-template-columns:1fr 1fr;gap:10px">
              <div class="form-group">
                <label class="form-label">Interval</label>
                <select v-model.number="form.interval_min" class="base-input">
                  <option v-for="o in intervalOpts" :key="o.value" :value="o.value">{{ o.label }}</option>
                </select>
              </div>
              <div class="form-group">
                <label class="form-label">Alert if</label>
                <select v-model="form.alert_condition" class="base-input">
                  <option v-for="o in alertOpts" :key="o.value" :value="o.value">{{ o.label }}</option>
                </select>
              </div>
            </div>
            <div v-if="form.alert_condition" class="form-group">
              <label class="form-label">Threshold</label>
              <input v-model.number="form.alert_threshold" class="base-input" type="number" placeholder="0" />
            </div>
          </div>
          <div class="page-modal__foot sc-modal-footer">
            <button class="base-btn base-btn--ghost" @click="showForm=false">Cancel</button>
            <button class="base-btn base-btn--primary" @click="save" :disabled="!form.name || !form.sql || !form.conn_id">
              {{ form.id ? 'Save' : 'Create' }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.sc-root { width:100%; height:100%; display:flex; flex-direction:column; overflow:hidden; }
.sc-scroll { flex:1; min-height:0; overflow-y:auto; padding:24px 28px 40px; display:flex; flex-direction:column; gap:16px; }
.sc-header { display:flex; align-items:flex-start; gap:12px; flex-wrap:wrap; }
.sc-title { font-size:20px; font-weight:700; color:var(--text-primary); }
.sc-sub { font-size:13px; color:var(--text-muted); margin-top:3px; }
.sc-list { display:flex; flex-direction:column; gap:8px; }
.sc-empty { display:flex; align-items:center; justify-content:center; padding:32px; color:var(--text-muted); gap:8px; }
.sc-item { background:var(--bg-elevated); border:1px solid var(--border); border-radius:8px; overflow:hidden; }
.sc-item--off { opacity:0.55; }
.sc-item-head { display:flex; align-items:center; gap:10px; padding:12px 14px; flex-wrap:wrap; }
.sc-dot { width:8px; height:8px; border-radius:50%; flex-shrink:0; }
.sc-dot--on { background:#4ade80; box-shadow:0 0 6px #4ade8088; animation:pulse 2s infinite; }
.sc-dot--off { background:var(--text-muted); }
@keyframes pulse { 0%,100%{opacity:1} 50%{opacity:0.4} }
.sc-item-info { display:flex; align-items:center; gap:8px; flex:1; flex-wrap:wrap; }
.sc-item-name { font-weight:700; font-size:13px; color:var(--text-primary); }
.sc-item-conn { font-size:11.5px; color:var(--brand); }
.sc-item-kind { font-size:11px; color:var(--text-secondary); padding:1px 7px; border-radius:999px; border:1px solid var(--border); }
.sc-item-interval { font-size:11px; color:var(--text-muted); }
.sc-alert-pill { padding:1px 7px; border-radius:4px; font-size:10.5px; background:rgba(251,191,36,0.15); color:#fbbf24; }
.sc-item-times { display:flex; flex-direction:column; gap:2px; }
.sc-time { font-size:10.5px; color:var(--text-muted); }
.sc-item-actions { display:flex; gap:4px; flex-wrap:wrap; }
.sc-sql { margin:0; padding:8px 14px; background:var(--bg-body); border-top:1px solid var(--border); font-family:var(--mono,monospace); font-size:11.5px; color:var(--text-secondary); white-space:pre-wrap; word-break:break-all; }
.sc-runs { background:var(--bg-elevated); border:1px solid var(--border); border-radius:8px; overflow:hidden; }
.sc-runs-header { display:flex; align-items:center; justify-content:space-between; padding:10px 14px; border-bottom:1px solid var(--border); font-size:13px; font-weight:700; color:var(--text-primary); }
.sc-run-row { display:flex; align-items:center; gap:12px; padding:8px 14px; border-bottom:1px solid var(--border); font-size:12px; }
.sc-run-row:hover { background:var(--bg-hover); }
.sc-run--err .sc-time { color:var(--danger); }
.sc-run-rows { color:var(--text-muted); }
.sc-run-summary { color:var(--text-primary); flex:1; min-width:220px; }
.sc-run-err { color:var(--danger); font-family:var(--mono,monospace); font-size:11px; flex:1; }
.cp-close { background:transparent; border:none; font-size:20px; color:var(--text-muted); cursor:pointer; padding:0 4px; line-height:1; }
.sc-overlay { position:fixed; inset:0; background:rgba(0,0,0,0.55); display:flex; align-items:center; justify-content:center; z-index:1100; }
.sc-modal { background:var(--bg-elevated); border:1px solid var(--border); border-radius:10px; width:min(520px,94vw); max-height:85vh; display:flex; flex-direction:column; box-shadow:0 24px 64px rgba(0,0,0,0.55); }
.sc-modal-header { display:flex; align-items:center; justify-content:space-between; padding:12px 16px; border-bottom:1px solid var(--border); font-size:14px; font-weight:700; color:var(--text-primary); }
.sc-modal-body { flex:1; min-height:0; overflow-y:auto; padding:16px; display:flex; flex-direction:column; gap:12px; }
.sc-modal-footer { display:flex; justify-content:flex-end; gap:8px; padding:12px 16px; border-top:1px solid var(--border); }
</style>
