<script setup lang="ts">
import { ref, onMounted } from 'vue'
import axios from 'axios'
import { useRouter } from 'vue-router'
import { useConnections } from '@/composables/useConnections'
import { useToast } from '@/composables/useToast'
import { readableError } from '@/utils/httpError'

interface Schedule {
  id: number; name: string; conn_id: number; sql: string
  dashboard_id: number
  kind: string; ai_prompt: string; created_by: number
  interval_min: number; alert_condition: string; alert_threshold: number
  enabled: boolean; last_run_at: string; next_run_at: string; created_at: string
}
interface AnalyticsDashboard {
  id: number; name: string
}
interface ScheduleRun {
  id: number; schedule_id: number; row_count: number
  summary: string; error: string; alerted: boolean; ran_at: string
}

interface SchedulerStatus {
  running: boolean
  instanceID: string
  lastTick: string
  startedAt: string
  intervalSec: number
}

const { activeConnections: connections } = useConnections()
const toast = useToast()
const router = useRouter()

function goToScheduleSource(s: Schedule) {
  if (s.kind === 'dashboard_report') {
    router.push({ name: 'dashboards' })
  } else {
    router.push({ name: 'data' })
  }
}
const schedules = ref<Schedule[]>([])
const dashboards = ref<AnalyticsDashboard[]>([])
const loading = ref(false)
const showForm = ref(false)
const selectedRuns = ref<ScheduleRun[]>([])
const runsFor = ref<number | null>(null)

const schedulerStatus = ref<SchedulerStatus | null>(null)
const statusLoading = ref(false)

async function fetchSchedulerStatus() {
  statusLoading.value = true
  try {
    const { data } = await axios.get<SchedulerStatus>('/api/scheduler/status')
    schedulerStatus.value = data
  } catch {
    schedulerStatus.value = null
  } finally {
    statusLoading.value = false
  }
}

const form = ref<Partial<Schedule>>({
  name: '', conn_id: 0, dashboard_id: 0, sql: '', kind: 'query', ai_prompt: '', interval_min: 60,
  alert_condition: '', alert_threshold: 0, enabled: true,
})

const kindOpts = [
  { value: 'query', label: 'Query Check' },
  { value: 'ai_summary', label: 'AI Summary' },
  { value: 'dashboard_report', label: 'Dashboard Report' },
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
    const [schedulesResp, dashboardsResp] = await Promise.all([
      axios.get<Schedule[]>('/api/schedules'),
      axios.get<AnalyticsDashboard[]>('/api/analytics-dashboards').catch(() => ({ data: [] as AnalyticsDashboard[] })),
    ])
    schedules.value = schedulesResp.data
    dashboards.value = dashboardsResp.data ?? []
  } finally { loading.value = false }
}

async function save() {
  if (!form.value.name) return
  if (form.value.kind === 'dashboard_report') {
    if (!form.value.dashboard_id) return
  } else if (!form.value.sql || !form.value.conn_id) {
    return
  }
  try {
    if (form.value.id) {
      await axios.put(`/api/schedules/${form.value.id}`, form.value)
    } else {
      await axios.post('/api/schedules', form.value)
    }
    showForm.value = false
    resetForm()
    load()
    toast.success('Schedule saved')
  } catch (e) {
    toast.error(readableError(e, { action: 'Save schedule', fallback: 'Failed to save schedule' }))
  }
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
  form.value = { name: '', conn_id: 0, dashboard_id: 0, sql: '', kind: 'query', ai_prompt: '', interval_min: 60, alert_condition: '', alert_threshold: 0, enabled: true }
}

function connName(id: number) {
  return connections.value.find((c) => c.id === id)?.name ?? `#${id}`
}

function dashboardName(id: number) {
  return dashboards.value.find((d) => d.id === id)?.name ?? `#${id}`
}

onMounted(() => { load(); fetchSchedulerStatus() })
</script>

<template>
  <div class="page-shell sc-root">
    <div class="page-scroll sc-scroll">
      <div class="page-stack">
      <section class="page-hero">
        <div class="page-hero__content">
          <div class="page-kicker">Automation</div>
          <div class="page-title">Scheduled Jobs</div>
          <div class="page-subtitle">Run recurring SQL checks, AI summaries, and dashboard report deliveries without leaving the app.</div>
        </div>
        <div class="page-hero__actions">
          <button class="base-btn base-btn--primary base-btn--sm" @click="resetForm(); showForm=true">+ New Schedule</button>
        </div>
      </section>

      <!-- Scheduler status -->
      <div class="page-card sc-status-card">
        <div class="sc-status-row">
          <div class="sc-status-meta">
            <div class="sc-status-title">Background Scheduler</div>
            <div class="sc-status-desc">The server process that automatically runs your scheduled jobs — SQL checks, AI summaries, dashboard reports, and pipeline cron triggers — on their configured intervals.</div>
          </div>
          <div class="sc-status-right">
            <template v-if="statusLoading">
              <svg class="spin sc-status-spin" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
              <span class="sc-status-badge">Checking…</span>
            </template>
            <template v-else-if="schedulerStatus">
              <div class="sc-status-indicator">
                <div class="sc-dot" :class="schedulerStatus.running ? 'sc-dot--on' : 'sc-dot--err'" />
                <span class="sc-status-badge" :class="schedulerStatus.running ? 'sc-badge--ok' : 'sc-badge--err'">
                  {{ schedulerStatus.running ? 'Running' : 'Stopped' }}
                </span>
              </div>
              <div class="sc-status-details">
                <div class="sc-status-detail-row">
                  <span class="sc-status-key">Runs every</span>
                  <span class="sc-status-val">{{ schedulerStatus.intervalSec }}s</span>
                </div>
                <div class="sc-status-detail-row">
                  <span class="sc-status-key">Last tick</span>
                  <span class="sc-status-val">{{ schedulerStatus.lastTick ? new Date(schedulerStatus.lastTick).toLocaleString() : 'Not yet (no tick since startup)' }}</span>
                </div>
                <div class="sc-status-detail-row">
                  <span class="sc-status-key">Started at</span>
                  <span class="sc-status-val">{{ new Date(schedulerStatus.startedAt).toLocaleString() }}</span>
                </div>
                <div class="sc-status-detail-row">
                  <span class="sc-status-key">Instance</span>
                  <span class="sc-status-instance">{{ schedulerStatus.instanceID }}</span>
                </div>
              </div>
            </template>
            <template v-else>
              <div class="sc-status-indicator">
                <div class="sc-dot sc-dot--err" />
                <span class="sc-status-badge sc-badge--err">Unreachable</span>
              </div>
              <div class="sc-status-row-sub">
                <span class="sc-status-key">Could not connect to the scheduler endpoint.</span>
              </div>
            </template>
            <button class="base-btn base-btn--ghost base-btn--sm sc-status-refresh" :disabled="statusLoading" @click="fetchSchedulerStatus">
              {{ statusLoading ? '…' : 'Refresh' }}
            </button>
          </div>
        </div>
      </div>

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
              <span class="sc-item-conn">{{ s.kind === 'dashboard_report' ? dashboardName(s.dashboard_id) : connName(s.conn_id) }}</span>
              <span class="sc-item-kind">{{ s.kind === 'ai_summary' ? 'AI Summary' : (s.kind === 'dashboard_report' ? 'Dashboard Report' : 'Query Check') }}</span>
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
              <button class="base-btn base-btn--ghost base-btn--sm sc-goto-btn" @click="goToScheduleSource(s)" :title="s.kind === 'dashboard_report' ? 'Go to Dashboard' : 'Go to Query'">
                {{ s.kind === 'dashboard_report' ? '↗ Dashboard' : '↗ Query' }}
              </button>
              <button class="base-btn base-btn--ghost base-btn--sm" style="color:var(--danger)" @click="del(s)">Delete</button>
            </div>
          </div>
          <pre v-if="s.kind !== 'dashboard_report'" class="sc-sql">{{ s.sql }}</pre>
          <div v-else class="sc-dashboard-target">Dashboard: {{ dashboardName(s.dashboard_id) }}</div>
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
              <label class="form-label">{{ form.kind === 'dashboard_report' ? 'Dashboard' : 'Connection' }}</label>
              <select v-if="form.kind === 'dashboard_report'" v-model.number="form.dashboard_id" class="base-input">
                <option :value="0">— select dashboard —</option>
                <option v-for="d in dashboards" :key="d.id" :value="d.id">{{ d.name }}</option>
              </select>
              <select v-else v-model.number="form.conn_id" class="base-input">
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
            <div v-if="form.kind !== 'dashboard_report'" class="form-group">
              <label class="form-label">{{ form.kind === 'ai_summary' ? 'Read-only SQL' : 'SQL' }}</label>
              <textarea v-model="form.sql" class="base-input" rows="4" :placeholder="form.kind === 'ai_summary' ? 'SELECT status, COUNT(*) AS total FROM orders GROUP BY status' : 'SELECT COUNT(*) FROM users'" style="font-family:monospace;font-size:12px;resize:vertical" />
            </div>
            <div v-else class="form-group">
              <label class="form-label">Delivery Summary</label>
              <div class="sc-helper">This schedule renders the selected dashboard, records a run summary, and emits notification events like <code>dashboard.report</code> and <code>dashboard.report.failed</code>.</div>
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
            <button class="base-btn base-btn--primary" @click="save" :disabled="!form.name || (form.kind === 'dashboard_report' ? !form.dashboard_id : (!form.sql || !form.conn_id))">
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
.sc-goto-btn { color:var(--brand); }
.sc-sql { margin:0; padding:8px 14px; background:var(--bg-body); border-top:1px solid var(--border); font-family:var(--mono,monospace); font-size:11.5px; color:var(--text-secondary); white-space:pre-wrap; word-break:break-all; }
.sc-dashboard-target { padding:10px 14px; border-top:1px solid var(--border); background:var(--bg-body); font-size:12px; color:var(--text-secondary); }
.sc-helper { padding:10px 12px; border:1px solid var(--border); border-radius:8px; background:var(--bg-body); font-size:12px; line-height:1.6; color:var(--text-secondary); }
.sc-runs { background:var(--bg-elevated); border:1px solid var(--border); border-radius:8px; overflow:hidden; }
.sc-runs-header { display:flex; align-items:center; justify-content:space-between; padding:10px 14px; border-bottom:1px solid var(--border); font-size:13px; font-weight:700; color:var(--text-primary); }
.sc-run-row { display:flex; align-items:center; gap:12px; padding:8px 14px; border-bottom:1px solid var(--border); font-size:12px; }
.sc-run-row:hover { background:var(--bg-hover); }
.sc-run--err .sc-time { color:var(--danger); }
.sc-run-rows { color:var(--text-muted); }
.sc-run-summary { color:var(--text-primary); flex:1; min-width:220px; }
.sc-run-err { color:var(--danger); font-family:var(--mono,monospace); font-size:11px; flex:1; }
.cp-close { background:transparent; border:none; font-size:20px; color:var(--text-muted); cursor:pointer; padding:0 4px; line-height:1; }
.sc-status-card { background:var(--bg-elevated); border:1px solid var(--border); border-radius:8px; padding:14px 18px; }
.sc-status-row { display:flex; align-items:flex-start; gap:32px; }
.sc-status-meta { min-width:0; max-width:400px; }
.sc-status-title { font-size:13px; font-weight:700; color:var(--text-primary); margin-bottom:4px; }
.sc-status-desc { font-size:12px; color:var(--text-muted); line-height:1.55; }
.sc-status-right { display:flex; flex-direction:column; align-items:flex-start; gap:6px; flex-shrink:0; }
.sc-status-indicator { display:flex; align-items:center; gap:7px; }
.sc-status-badge { font-size:12px; font-weight:600; }
.sc-badge--ok { color:#4ade80; }
.sc-badge--err { color:var(--danger,#f87171); }
.sc-status-row-sub { display:flex; align-items:center; gap:6px; }
.sc-status-details { display:flex; flex-direction:column; gap:4px; margin-top:4px; }
.sc-status-detail-row { display:flex; align-items:center; gap:8px; }
.sc-status-key { font-size:11px; color:var(--text-muted); min-width:68px; flex-shrink:0; }
.sc-status-val { font-size:11px; color:var(--text-secondary); }
.sc-status-instance { font-size:10.5px; color:var(--text-muted); font-family:var(--mono,monospace); background:var(--bg-body); padding:2px 8px; border-radius:4px; border:1px solid var(--border); max-width:220px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.sc-status-refresh { align-self:flex-start; margin-top:2px; }
.sc-status-spin { color:var(--text-muted); }
.sc-dot--err { background:var(--danger,#f87171); box-shadow:0 0 6px rgba(248,113,113,0.5); }
.spin { animation:rotate 1s linear infinite; }
@keyframes rotate { to { transform:rotate(360deg) } }
.sc-overlay { position:fixed; inset:0; background:rgba(0,0,0,0.55); display:flex; align-items:center; justify-content:center; z-index:1100; }
.sc-modal { background:var(--bg-elevated); border:1px solid var(--border); border-radius:10px; width:min(520px,94vw); max-height:85vh; display:flex; flex-direction:column; box-shadow:0 24px 64px rgba(0,0,0,0.55); }
.sc-modal-header { display:flex; align-items:center; justify-content:space-between; padding:12px 16px; border-bottom:1px solid var(--border); font-size:14px; font-weight:700; color:var(--text-primary); }
.sc-modal-body { flex:1; min-height:0; overflow-y:auto; padding:16px; display:flex; flex-direction:column; gap:12px; }
.sc-modal-footer { display:flex; justify-content:flex-end; gap:8px; padding:12px 16px; border-top:1px solid var(--border); }
</style>
