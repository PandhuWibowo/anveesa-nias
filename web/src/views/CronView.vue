<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import axios from 'axios'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useAuth } from '@/composables/useAuth'

interface CronHost {
  id: number
  name: string
  ssh_host: string
}

interface CronJob {
  id: number
  name: string
  command: string
  working_dir: string
  category: string
  cron_expr: string
  timeout_sec: number
  host_ids: number[]
  enabled: boolean
  last_run_at: string
  created_at: string
}

interface CronJobRun {
  id: number
  job_id: number
  host_id: number
  host_name: string
  trigger: string
  status: string
  exit_code: number
  stdout: string
  stderr: string
  started_at: string
  finished_at: string
  duration_ms: number
}

interface JobForm {
  name: string
  command: string
  working_dir: string
  category: string
  cron_expr: string
  timeout_sec: number
  host_ids: number[]
  enabled: boolean
}

const toast = useToast()
const { confirm } = useConfirm()
const { hasAnyPermission } = useAuth()
const canManage = computed(() => hasAnyPermission(['cron.manage']))
const canExec = computed(() => hasAnyPermission(['cron.exec']))

const jobs = ref<CronJob[]>([])
const hosts = ref<CronHost[]>([])
const loading = ref(false)
const selectedId = ref<number | null>(null)
const runs = ref<CronJobRun[]>([])
const runsLoading = ref(false)
const openRun = ref<CronJobRun | null>(null)

const showForm = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)
const form = ref<JobForm>(emptyForm())

const CRON_PRESETS: { label: string; expr: string }[] = [
  { label: 'Every minute', expr: '* * * * *' },
  { label: 'Every 5 minutes', expr: '*/5 * * * *' },
  { label: 'Hourly', expr: '0 * * * *' },
  { label: 'Daily 00:00', expr: '0 0 * * *' },
  { label: 'Weekly (Sun 00:00)', expr: '0 0 * * 0' },
  { label: 'Monthly (1st 00:00)', expr: '0 0 1 * *' },
]

function emptyForm(): JobForm {
  return {
    name: '',
    command: '',
    working_dir: '',
    category: '',
    cron_expr: '*/5 * * * *',
    timeout_sec: 3600,
    host_ids: [],
    enabled: true,
  }
}

const hostName = (id: number) => hosts.value.find((h) => h.id === id)?.name || `#${id}`
const selectedJob = computed(() => jobs.value.find((j) => j.id === selectedId.value) || null)

async function loadAll() {
  loading.value = true
  try {
    const [j, h] = await Promise.all([
      axios.get<CronJob[]>('/api/cron/jobs'),
      axios.get<CronHost[]>('/api/cron/hosts'),
    ])
    jobs.value = j.data || []
    hosts.value = h.data || []
    if (selectedId.value && !jobs.value.some((x) => x.id === selectedId.value)) {
      selectedId.value = null
    }
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to load jobs')
  } finally {
    loading.value = false
  }
}

async function selectJob(j: CronJob) {
  selectedId.value = j.id
  openRun.value = null
  await loadRuns(j.id)
}

async function loadRuns(jobId: number) {
  runsLoading.value = true
  try {
    const { data } = await axios.get<CronJobRun[]>(`/api/cron/jobs/${jobId}/runs`)
    runs.value = data || []
  } catch {
    runs.value = []
  } finally {
    runsLoading.value = false
  }
}

function openNew() {
  editingId.value = null
  form.value = emptyForm()
  showForm.value = true
}

function openEdit(j: CronJob) {
  editingId.value = j.id
  form.value = {
    name: j.name,
    command: j.command,
    working_dir: j.working_dir,
    category: j.category,
    cron_expr: j.cron_expr,
    timeout_sec: j.timeout_sec,
    host_ids: [...j.host_ids],
    enabled: j.enabled,
  }
  showForm.value = true
}

function toggleHost(id: number) {
  const i = form.value.host_ids.indexOf(id)
  if (i >= 0) form.value.host_ids.splice(i, 1)
  else form.value.host_ids.push(id)
}

async function save() {
  if (!form.value.name.trim() || !form.value.command.trim()) {
    toast.error('Name and command are required')
    return
  }
  if (form.value.cron_expr.trim().split(/\s+/).length !== 5) {
    toast.error('Schedule must be a 5-field cron expression')
    return
  }
  saving.value = true
  try {
    const body = { ...form.value, cron_expr: form.value.cron_expr.trim() }
    if (editingId.value) await axios.put(`/api/cron/jobs/${editingId.value}`, body)
    else await axios.post('/api/cron/jobs', body)
    toast.success(editingId.value ? 'Job updated' : 'Job created')
    showForm.value = false
    await loadAll()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to save job')
  } finally {
    saving.value = false
  }
}

async function toggleEnabled(j: CronJob) {
  try {
    await axios.post(`/api/cron/jobs/${j.id}/toggle`, { enabled: !j.enabled })
    j.enabled = !j.enabled
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to toggle job')
  }
}

async function runNow(j: CronJob) {
  if (!j.host_ids.length) {
    toast.error('Job has no target hosts')
    return
  }
  try {
    await axios.post(`/api/cron/jobs/${j.id}/run`, {})
    toast.success(`Triggered on ${j.host_ids.length} host(s)`)
    setTimeout(() => loadRuns(j.id), 1200)
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to run job')
  }
}

async function removeJob(j: CronJob) {
  if (!(await confirm(`Delete job "${j.name}"?`, 'Its run history will also be removed.'))) return
  try {
    await axios.delete(`/api/cron/jobs/${j.id}`)
    toast.success('Job deleted')
    if (selectedId.value === j.id) selectedId.value = null
    await loadAll()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to delete job')
  }
}

function statusClass(s: string) {
  if (s === 'success') return 'crn-badge--ok'
  if (s === 'failed') return 'crn-badge--err'
  return 'crn-badge--run'
}

function fmtDuration(ms: number) {
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

onMounted(loadAll)
</script>

<template>
  <div class="page-shell">
    <div class="page-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Operations</div>
            <div class="page-title">Scheduler</div>
            <div class="page-subtitle">Centrally schedule commands and run them across your servers.</div>
          </div>
          <div class="page-hero__actions">
            <button class="base-btn base-btn--sm" :disabled="loading" @click="loadAll">Refresh</button>
            <button v-if="canManage" class="base-btn base-btn--primary base-btn--sm" @click="openNew">
              + New job
            </button>
          </div>
        </section>

        <div v-if="!hosts.length" class="crn-warn">
          No SSH hosts configured yet — add a server under
          <RouterLink :to="{ name: 'ssh-hosts' }" class="crn-link">SSH Hosts</RouterLink>
          before scheduling jobs.
        </div>

        <div class="crn-layout">
          <!-- Job list -->
          <div class="crn-list page-card">
            <div v-if="loading" class="crn-msg">Loading…</div>
            <div v-else-if="!jobs.length" class="crn-msg">No jobs yet.</div>
            <table v-else class="crn-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Schedule</th>
                  <th>Targets</th>
                  <th>Enabled</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="j in jobs"
                  :key="j.id"
                  :class="{ 'crn-row--sel': j.id === selectedId }"
                  @click="selectJob(j)"
                >
                  <td>
                    <div class="crn-job-name">{{ j.name }}</div>
                    <div v-if="j.category" class="crn-job-cat">{{ j.category }}</div>
                  </td>
                  <td><code class="crn-code">{{ j.cron_expr }}</code></td>
                  <td class="crn-targets">{{ j.host_ids.length }}</td>
                  <td @click.stop>
                    <label class="crn-switch">
                      <input
                        type="checkbox"
                        :checked="j.enabled"
                        :disabled="!canManage"
                        @change="toggleEnabled(j)"
                      />
                      <span />
                    </label>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Detail panel -->
          <div class="crn-detail page-card">
            <div v-if="!selectedJob" class="crn-msg">Select a job to see details and run history.</div>
            <div v-else>
              <div class="crn-detail-head">
                <div>
                  <div class="crn-detail-title">{{ selectedJob.name }}</div>
                  <div class="crn-detail-sub">
                    <code class="crn-code">{{ selectedJob.cron_expr }}</code>
                    · timeout {{ selectedJob.timeout_sec }}s
                  </div>
                </div>
                <div class="crn-detail-actions">
                  <button
                    v-if="canExec"
                    class="base-btn base-btn--sm base-btn--primary"
                    @click="runNow(selectedJob)"
                  >
                    Run now
                  </button>
                  <button v-if="canManage" class="base-btn base-btn--sm" @click="openEdit(selectedJob)">
                    Edit
                  </button>
                  <button v-if="canManage" class="base-btn base-btn--sm crn-danger" @click="removeJob(selectedJob)">
                    Delete
                  </button>
                </div>
              </div>

              <pre class="crn-command">{{ selectedJob.command }}</pre>
              <div v-if="selectedJob.working_dir" class="crn-wd">cwd: {{ selectedJob.working_dir }}</div>

              <div class="crn-hostchips">
                <span v-for="hid in selectedJob.host_ids" :key="hid" class="crn-chip">{{ hostName(hid) }}</span>
                <span v-if="!selectedJob.host_ids.length" class="crn-msg">No target hosts</span>
              </div>

              <div class="crn-runs-head">
                <span>Run history</span>
                <button class="base-btn base-btn--sm" :disabled="runsLoading" @click="loadRuns(selectedJob.id)">
                  Refresh
                </button>
              </div>
              <div v-if="runsLoading" class="crn-msg">Loading runs…</div>
              <div v-else-if="!runs.length" class="crn-msg">No runs yet.</div>
              <table v-else class="crn-table crn-runs">
                <thead>
                  <tr>
                    <th>Started</th>
                    <th>Host</th>
                    <th>Trigger</th>
                    <th>Status</th>
                    <th>Exit</th>
                    <th>Took</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="r in runs" :key="r.id" @click="openRun = r">
                    <td>{{ r.started_at }}</td>
                    <td>{{ r.host_name || hostName(r.host_id) }}</td>
                    <td>{{ r.trigger }}</td>
                    <td><span class="crn-badge" :class="statusClass(r.status)">{{ r.status }}</span></td>
                    <td>{{ r.exit_code }}</td>
                    <td>{{ fmtDuration(r.duration_ms) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Run output -->
    <div v-if="openRun" class="crn-overlay" @click.self="openRun = null">
      <div class="crn-modal crn-modal--wide">
        <div class="crn-modal-head">
          Run #{{ openRun.id }} · {{ openRun.host_name }}
          <span class="crn-badge" :class="statusClass(openRun.status)">{{ openRun.status }}</span>
        </div>
        <div class="crn-out-label">stdout</div>
        <pre class="crn-output">{{ openRun.stdout || '(empty)' }}</pre>
        <div class="crn-out-label">stderr</div>
        <pre class="crn-output crn-output--err">{{ openRun.stderr || '(empty)' }}</pre>
        <div class="crn-modal-actions">
          <div class="crn-spacer" />
          <button class="base-btn base-btn--sm" @click="openRun = null">Close</button>
        </div>
      </div>
    </div>

    <!-- Job editor -->
    <div v-if="showForm" class="crn-overlay" @click.self="showForm = false">
      <div class="crn-modal">
        <div class="crn-modal-head">{{ editingId ? 'Edit job' : 'New job' }}</div>
        <div class="crn-row">
          <div class="crn-field crn-grow">
            <label>Name</label>
            <input v-model="form.name" placeholder="Nightly backup" />
          </div>
          <div class="crn-field">
            <label>Category</label>
            <input v-model="form.category" placeholder="maintenance" />
          </div>
        </div>
        <div class="crn-field">
          <label>Command</label>
          <textarea v-model="form.command" rows="3" placeholder="php artisan backup:run"></textarea>
        </div>
        <div class="crn-field">
          <label>Working directory <span class="crn-hint">(optional)</span></label>
          <input v-model="form.working_dir" placeholder="/var/www/app" />
        </div>
        <div class="crn-row">
          <div class="crn-field crn-grow">
            <label>Schedule (cron)</label>
            <input v-model="form.cron_expr" class="crn-mono" placeholder="*/5 * * * *" />
          </div>
          <div class="crn-field crn-timeout">
            <label>Timeout (s)</label>
            <input v-model.number="form.timeout_sec" type="number" />
          </div>
        </div>
        <div class="crn-presets">
          <button
            v-for="p in CRON_PRESETS"
            :key="p.expr"
            class="crn-preset"
            :class="{ 'crn-preset--on': form.cron_expr === p.expr }"
            @click="form.cron_expr = p.expr"
          >
            {{ p.label }}
          </button>
        </div>
        <div class="crn-field">
          <label>Target hosts</label>
          <div v-if="!hosts.length" class="crn-msg">No hosts available.</div>
          <div v-else class="crn-hostpick">
            <label v-for="h in hosts" :key="h.id" class="crn-hostopt">
              <input
                type="checkbox"
                :checked="form.host_ids.includes(h.id)"
                @change="toggleHost(h.id)"
              />
              {{ h.name }} <span class="crn-hint">({{ h.ssh_host }})</span>
            </label>
          </div>
        </div>
        <label class="crn-enable">
          <input type="checkbox" v-model="form.enabled" /> Enabled
        </label>
        <div class="crn-modal-actions">
          <div class="crn-spacer" />
          <button class="base-btn base-btn--sm" @click="showForm = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="saving" @click="save">
            {{ saving ? 'Saving…' : 'Save' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.crn-warn {
  padding: 0.75rem 1rem;
  background: rgba(234, 179, 8, 0.12);
  border: 1px solid rgba(234, 179, 8, 0.4);
  border-radius: 8px;
  font-size: 0.85rem;
}
.crn-link {
  color: #2563eb;
  font-weight: 600;
  text-decoration: underline;
}
.crn-msg {
  padding: 1.25rem;
  color: var(--text-muted, #64748b);
  text-align: center;
}
.crn-layout {
  display: grid;
  grid-template-columns: minmax(320px, 420px) 1fr;
  gap: 1rem;
  align-items: start;
}
@media (max-width: 900px) {
  .crn-layout {
    grid-template-columns: 1fr;
  }
}
.crn-list,
.crn-detail {
  padding: 0.5rem;
  overflow: hidden;
}
.crn-detail {
  padding: 1rem;
}
.crn-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.85rem;
}
.crn-table th {
  text-align: left;
  padding: 0.5rem;
  font-size: 0.72rem;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--text-muted, #94a3b8);
  border-bottom: 1px solid var(--border, #e2e8f0);
}
.crn-table td {
  padding: 0.5rem;
  border-bottom: 1px solid var(--border, #f1f5f9);
}
.crn-list tbody tr {
  cursor: pointer;
}
.crn-list tbody tr:hover {
  background: var(--surface-2, #f8fafc);
}
.crn-row--sel {
  background: rgba(59, 130, 246, 0.1) !important;
}
.crn-job-name {
  font-weight: 600;
}
.crn-job-cat {
  font-size: 0.72rem;
  color: var(--text-muted, #94a3b8);
}
.crn-code {
  font-family: ui-monospace, monospace;
  font-size: 0.78rem;
  background: var(--surface-2, #f1f5f9);
  padding: 0.1rem 0.35rem;
  border-radius: 5px;
}
.crn-targets {
  text-align: center;
}
.crn-switch {
  position: relative;
  display: inline-block;
  width: 34px;
  height: 18px;
}
.crn-switch input {
  opacity: 0;
  width: 0;
  height: 0;
}
.crn-switch span {
  position: absolute;
  inset: 0;
  background: #cbd5e1;
  border-radius: 999px;
  transition: 0.2s;
}
.crn-switch span::before {
  content: '';
  position: absolute;
  height: 14px;
  width: 14px;
  left: 2px;
  bottom: 2px;
  background: #fff;
  border-radius: 50%;
  transition: 0.2s;
}
.crn-switch input:checked + span {
  background: #22c55e;
}
.crn-switch input:checked + span::before {
  transform: translateX(16px);
}
.crn-detail-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 1rem;
  margin-bottom: 0.75rem;
}
.crn-detail-title {
  font-size: 1.1rem;
  font-weight: 600;
}
.crn-detail-sub {
  font-size: 0.8rem;
  color: var(--text-muted, #64748b);
  margin-top: 0.2rem;
}
.crn-detail-actions {
  display: flex;
  gap: 0.4rem;
  flex-wrap: wrap;
}
.crn-command {
  background: #0f172a;
  color: #e2e8f0;
  padding: 0.75rem;
  border-radius: 8px;
  font-family: ui-monospace, monospace;
  font-size: 0.8rem;
  white-space: pre-wrap;
  word-break: break-all;
  margin: 0.5rem 0;
}
.crn-wd {
  font-size: 0.78rem;
  color: var(--text-muted, #64748b);
  font-family: ui-monospace, monospace;
}
.crn-hostchips {
  display: flex;
  gap: 0.4rem;
  flex-wrap: wrap;
  margin: 0.75rem 0;
}
.crn-chip {
  background: var(--surface-2, #f1f5f9);
  padding: 0.2rem 0.55rem;
  border-radius: 999px;
  font-size: 0.75rem;
}
.crn-runs-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin: 1rem 0 0.4rem;
  font-weight: 600;
  font-size: 0.9rem;
}
.crn-runs tbody tr {
  cursor: pointer;
}
.crn-runs tbody tr:hover {
  background: var(--surface-2, #f8fafc);
}
.crn-badge {
  padding: 0.1rem 0.5rem;
  border-radius: 999px;
  font-size: 0.72rem;
  font-weight: 600;
}
.crn-badge--ok {
  background: rgba(34, 197, 94, 0.15);
  color: #16a34a;
}
.crn-badge--err {
  background: rgba(239, 68, 68, 0.15);
  color: #dc2626;
}
.crn-badge--run {
  background: rgba(59, 130, 246, 0.15);
  color: #2563eb;
}
.crn-danger {
  color: #ef4444;
}
.crn-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
  padding: 1rem;
}
.crn-modal {
  background: var(--surface, #fff);
  border-radius: 12px;
  padding: 1.25rem;
  width: 100%;
  max-width: 520px;
  max-height: 90vh;
  overflow-y: auto;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.25);
}
.crn-modal--wide {
  max-width: 760px;
}
.crn-modal-head {
  font-size: 1.05rem;
  font-weight: 600;
  margin-bottom: 1rem;
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.crn-field {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  margin-bottom: 0.75rem;
}
.crn-field label {
  font-size: 0.8rem;
  font-weight: 500;
}
.crn-hint {
  color: var(--text-muted, #94a3b8);
  font-weight: 400;
}
.crn-field input,
.crn-field textarea {
  padding: 0.5rem 0.6rem;
  border: 1px solid var(--border, #cbd5e1);
  border-radius: 8px;
  background: var(--surface-2, #f8fafc);
  color: inherit;
  font-family: inherit;
}
.crn-field textarea,
.crn-mono {
  font-family: ui-monospace, monospace !important;
  font-size: 0.82rem;
}
.crn-row {
  display: flex;
  gap: 0.6rem;
}
.crn-grow {
  flex: 1;
}
.crn-timeout {
  width: 110px;
}
.crn-presets {
  display: flex;
  gap: 0.35rem;
  flex-wrap: wrap;
  margin-bottom: 0.75rem;
}
.crn-preset {
  font-size: 0.72rem;
  padding: 0.25rem 0.5rem;
  border: 1px solid var(--border, #cbd5e1);
  border-radius: 999px;
  background: transparent;
  cursor: pointer;
  color: inherit;
}
.crn-preset--on {
  background: rgba(59, 130, 246, 0.15);
  border-color: #3b82f6;
}
.crn-hostpick {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  max-height: 160px;
  overflow-y: auto;
  border: 1px solid var(--border, #e2e8f0);
  border-radius: 8px;
  padding: 0.5rem;
}
.crn-hostopt {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  font-size: 0.85rem;
}
.crn-enable {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  font-size: 0.85rem;
  margin-bottom: 0.75rem;
}
.crn-out-label {
  font-size: 0.72rem;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--text-muted, #94a3b8);
  margin: 0.5rem 0 0.25rem;
}
.crn-output {
  background: #0f172a;
  color: #e2e8f0;
  padding: 0.75rem;
  border-radius: 8px;
  font-family: ui-monospace, monospace;
  font-size: 0.78rem;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 240px;
  overflow-y: auto;
  margin: 0;
}
.crn-output--err {
  color: #fca5a5;
}
.crn-modal-actions {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-top: 0.75rem;
}
.crn-spacer {
  flex: 1;
}
</style>
