<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import axios from 'axios'
import { useToast } from '@/composables/useToast'
import { useDatabases } from '@/composables/useDatabases'

interface ChangeSet {
  id: number
  title: string
  description: string
  conn_id: number
  connection: string
  driver: string
  environment: string
  database: string
  statement: string
  rollback_sql: string
  impact_summary: string
  status: string
  creator_id: number
  creator_name: string
  reviewer_name?: string
  review_note?: string
  workflow_id: number
  current_step: number
  revision: number
  validation_status: 'pending' | 'passed' | 'failed'
  validation_message: string
  execute_error?: string
  created_at: string
  updated_at: string
}

interface ApprovalAction {
  id: number
  username: string
  action: 'approved' | 'rejected'
  note: string
  created_at: string
}

interface ApprovalProgressStep {
  step: {
    id: number
    step_order: number
    name: string
    required_approvals: number
    approvers: Array<{ approver_name: string; approver_type: string }>
  }
  status: 'pending' | 'approved' | 'rejected' | 'waiting'
  approvals: ApprovalAction[]
}

interface ApprovalProgressResponse {
  workflow_name: string
  current_step: number
  total_steps: number
  progress: ApprovalProgressStep[]
  can_approve: boolean
  can_execute: boolean
  can_revise: boolean
}

interface ValidationResponse {
  ok: boolean
  validation_status: 'pending' | 'passed' | 'failed'
  message: string
  impact_summary: string
}

interface ConnectionOption {
  id: number
  name: string
  environment?: string
}

interface WorkflowOption {
  id: number
  name: string
  description: string
}

const toast = useToast()
const { databases, loading: databasesLoading, fetchDatabases } = useDatabases()

const loading = ref(false)
const detailLoading = ref(false)
const validating = ref(false)
const submitting = ref(false)
const createOpen = ref(false)
const createSubmitting = ref(false)
const selectedId = ref<number | null>(null)
const selected = ref<ChangeSet | null>(null)
const progress = ref<ApprovalProgressResponse | null>(null)
const note = ref('')
const filter = ref<'all' | 'draft' | 'pending_review' | 'approved' | 'rejected' | 'done' | 'failed'>('all')
const changeSets = ref<ChangeSet[]>([])
const connections = ref<ConnectionOption[]>([])
const workflows = ref<WorkflowOption[]>([])
const workflowsLoading = ref(false)
const editingId = ref<number | null>(null)
const databaseMode = ref<'select' | 'manual'>('select')

const form = reactive({
  title: '',
  description: '',
  conn_id: null as number | null,
  database: '',
  statement: '',
  rollback_sql: '',
  impact_summary: '',
  workflow_id: null as number | null,
})

const filteredChangeSets = computed(() =>
  filter.value === 'all' ? changeSets.value : changeSets.value.filter(item => item.status === filter.value)
)

const createReady = computed(() => !!form.conn_id && !!form.title.trim() && !!form.statement.trim())
const canSubmit = computed(() => !!selected.value && (selected.value.status === 'draft' || selected.value.status === 'rejected') && selected.value.validation_status === 'passed')

async function fetchConnections() {
  try {
    const { data } = await axios.get<ConnectionOption[]>('/api/connections')
    connections.value = data || []
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to load connections')
  }
}

async function fetchChangeSets() {
  loading.value = true
  try {
    const { data } = await axios.get<ChangeSet[]>('/api/change-sets')
    changeSets.value = data || []
    await syncSelection()
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to load change sets')
  } finally {
    loading.value = false
  }
}

async function syncSelection() {
  const visible = filteredChangeSets.value
  if (visible.length === 0) {
    selectedId.value = null
    selected.value = null
    progress.value = null
    return
  }
  const nextId = selectedId.value != null && visible.some(item => item.id === selectedId.value) ? selectedId.value : visible[0].id
  if (nextId != null) {
    await loadDetail(nextId)
  }
}

async function loadDetail(id: number) {
  detailLoading.value = true
  selectedId.value = id
  try {
    const [{ data: changeSet }, { data: progressData }] = await Promise.all([
      axios.get<ChangeSet>(`/api/change-sets/${id}`),
      axios.get<ApprovalProgressResponse>(`/api/change-sets/${id}/approval-progress`),
    ])
    selected.value = changeSet
    progress.value = progressData
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to load change set')
  } finally {
    detailLoading.value = false
  }
}

function resetForm() {
  editingId.value = null
  form.title = ''
  form.description = ''
  form.conn_id = null
  form.database = ''
  form.statement = ''
  form.rollback_sql = ''
  form.impact_summary = ''
  form.workflow_id = null
  workflows.value = []
  databaseMode.value = 'select'
}

function openCreate() {
  resetForm()
  createOpen.value = true
}

function openRevise() {
  if (!selected.value) return
  editingId.value = selected.value.id
  form.title = selected.value.title
  form.description = selected.value.description
  form.conn_id = selected.value.conn_id
  form.database = selected.value.database
  form.statement = selected.value.statement
  form.rollback_sql = selected.value.rollback_sql
  form.impact_summary = selected.value.impact_summary
  form.workflow_id = selected.value.workflow_id || null
  createOpen.value = true
}

async function loadWorkflows(connID: number | null) {
  const currentWorkflowID = form.workflow_id
  workflows.value = []
  if (!connID) return
  workflowsLoading.value = true
  try {
    const { data } = await axios.get<WorkflowOption[]>('/api/workflows/applicable', { params: { conn_id: connID } })
    workflows.value = data || []
    const preferred = workflows.value.find(workflow => workflow.id === currentWorkflowID)
    form.workflow_id = preferred?.id ?? workflows.value[0]?.id ?? null
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to load workflows')
  } finally {
    workflowsLoading.value = false
  }
}

async function loadConnectionDatabases(connID: number | null) {
  const currentDatabase = form.database
  if (!connID) {
    form.database = ''
    databaseMode.value = 'select'
    return
  }
  await fetchDatabases(connID)
  if (databases.value.length > 0) {
    databaseMode.value = 'select'
    form.database = databases.value.includes(currentDatabase) ? currentDatabase : (databases.value[0] ?? '')
  } else {
    databaseMode.value = 'manual'
  }
}

async function saveDraft() {
  if (!createReady.value || !form.conn_id) return
  createSubmitting.value = true
  try {
    const payload = {
      title: form.title.trim(),
      description: form.description.trim(),
      conn_id: form.conn_id,
      database: form.database.trim(),
      statement: form.statement.trim(),
      rollback_sql: form.rollback_sql.trim(),
      impact_summary: form.impact_summary.trim(),
      workflow_id: form.workflow_id || 0,
    }
    const { data } = editingId.value
      ? await axios.put<ChangeSet>(`/api/change-sets/${editingId.value}`, payload)
      : await axios.post<ChangeSet>('/api/change-sets', payload)
    createOpen.value = false
    toast.success(editingId.value ? `Change set #${data.id} updated` : `Change set #${data.id} saved`)
    await fetchChangeSets()
    await loadDetail(data.id)
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to save change set')
  } finally {
    createSubmitting.value = false
  }
}

async function validateSelected() {
  if (!selected.value) return
  validating.value = true
  try {
    const { data } = await axios.post<ValidationResponse>(`/api/change-sets/${selected.value.id}/validate`)
    toast.success(data.ok ? 'Validation passed' : 'Validation failed')
    await fetchChangeSets()
    await loadDetail(selected.value.id)
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Validation failed')
  } finally {
    validating.value = false
  }
}

async function submitSelected() {
  if (!selected.value) return
  submitting.value = true
  try {
    await axios.post(`/api/change-sets/${selected.value.id}/submit`)
    toast.success('Change set submitted for approval')
    await fetchChangeSets()
    await loadDetail(selected.value.id)
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to submit change set')
  } finally {
    submitting.value = false
  }
}

async function executeSelected() {
  if (!selected.value) return
  try {
    await axios.post(`/api/change-sets/${selected.value.id}/execute`)
    toast.success('Change set executed')
    await fetchChangeSets()
    await loadDetail(selected.value.id)
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to execute change set')
  }
}

async function actOnStep(action: 'approved' | 'rejected') {
  if (!selected.value) return
  try {
    await axios.post(`/api/change-sets/${selected.value.id}/approve-step`, {
      action,
      note: note.value.trim(),
    })
    note.value = ''
    toast.success(action === 'approved' ? 'Approval recorded' : 'Changes requested')
    await fetchChangeSets()
    await loadDetail(selected.value.id)
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to record action')
  }
}

function statusClass(status: string) {
  return {
    'cs-status--draft': status === 'draft',
    'cs-status--pending': status === 'pending_review',
    'cs-status--approved': status === 'approved' || status === 'done',
    'cs-status--rejected': status === 'rejected' || status === 'failed',
  }
}

function validationClass(status: ChangeSet['validation_status']) {
  return {
    'cs-validation--pending': status === 'pending',
    'cs-validation--passed': status === 'passed',
    'cs-validation--failed': status === 'failed',
  }
}

watch(() => form.conn_id, (connID) => {
  void loadWorkflows(connID)
  void loadConnectionDatabases(connID)
})

watch(filter, () => {
  void syncSelection()
})

onMounted(async () => {
  await Promise.all([fetchConnections(), fetchChangeSets()])
})
</script>

<template>
  <div class="cs-root">
    <div class="cs-scroll">
      <div class="cs-header">
        <div>
          <div class="cs-kicker">Operations</div>
          <div class="cs-title">Change Sets</div>
          <div class="cs-sub">Package a database change, validate it, route it through approvals, then execute it with a full record.</div>
        </div>
        <div class="cs-header-actions">
          <button class="base-btn base-btn--primary base-btn--sm" @click="openCreate">New Change Set</button>
          <select v-model="filter" class="base-input">
            <option value="all">All statuses</option>
            <option value="draft">Draft</option>
            <option value="pending_review">Pending review</option>
            <option value="approved">Approved</option>
            <option value="rejected">Rejected</option>
            <option value="done">Executed</option>
            <option value="failed">Failed</option>
          </select>
        </div>
      </div>

      <div class="cs-layout">
        <aside class="cs-list-panel">
          <div class="cs-list-head">Change Sets</div>
          <div v-if="loading" class="cs-empty">Loading change sets…</div>
          <div v-else-if="filteredChangeSets.length === 0" class="cs-empty">
            <div class="cs-empty__title">No change sets yet</div>
            <div class="cs-empty__text">Start with a draft, validate the target, then submit it into the existing approval workflow.</div>
            <button class="base-btn base-btn--primary base-btn--sm" @click="openCreate">Create Draft</button>
          </div>
          <div v-else class="cs-list">
            <button
              v-for="item in filteredChangeSets"
              :key="item.id"
              class="cs-list__item"
              :class="{ 'cs-list__item--active': selectedId === item.id }"
              @click="loadDetail(item.id)"
            >
              <div class="cs-list__top">
                <div class="cs-list__title">{{ item.title }}</div>
                <span class="cs-status" :class="statusClass(item.status)">{{ item.status.replace('_', ' ') }}</span>
              </div>
              <div class="cs-list__meta">
                <span>{{ item.connection }}</span>
                <span>{{ item.creator_name }}</span>
              </div>
              <div class="cs-list__sub">{{ item.impact_summary || 'No impact summary yet' }}</div>
            </button>
          </div>
        </aside>

        <section class="cs-detail-panel">
          <div v-if="detailLoading" class="cs-empty">Loading change set…</div>
          <div v-else-if="!selected || !progress" class="cs-empty">
            <div class="cs-empty__title">Select a change set</div>
            <div class="cs-empty__text">Drafts, validations, approvals, and execution history show here.</div>
          </div>
          <template v-else>
            <div class="cs-hero">
              <div>
                <div class="cs-detail-title">{{ selected.title }}</div>
                <div class="cs-detail-meta">
                  <span class="cs-status" :class="statusClass(selected.status)">{{ selected.status.replace('_', ' ') }}</span>
                  <span>{{ selected.connection }} • {{ selected.environment }}</span>
                  <span v-if="selected.database">{{ selected.database }}</span>
                  <span>Revision {{ selected.revision }}</span>
                </div>
              </div>
              <div class="cs-hero-actions">
                <button class="base-btn base-btn--ghost base-btn--sm" :disabled="validating" @click="validateSelected">
                  {{ validating ? 'Validating…' : 'Validate' }}
                </button>
                <button v-if="canSubmit" class="base-btn base-btn--primary base-btn--sm" :disabled="submitting" @click="submitSelected">
                  {{ submitting ? 'Submitting…' : 'Submit for Approval' }}
                </button>
                <button v-if="progress.can_execute" class="base-btn base-btn--primary base-btn--sm" @click="executeSelected">Execute</button>
                <button v-if="progress.can_revise" class="base-btn base-btn--ghost base-btn--sm" @click="openRevise">Revise</button>
              </div>
            </div>

            <div v-if="selected.description" class="cs-panel">{{ selected.description }}</div>

            <div class="cs-grid">
              <div class="cs-panel">
                <div class="cs-panel__title">Impact</div>
                <div class="cs-impact">{{ selected.impact_summary || 'No impact summary yet.' }}</div>
              </div>
              <div class="cs-panel">
                <div class="cs-panel__title">Validation</div>
                <div class="cs-validation" :class="validationClass(selected.validation_status)">
                  <strong>{{ selected.validation_status }}</strong>
                  <span>{{ selected.validation_message || 'Validation has not been run yet.' }}</span>
                </div>
              </div>
            </div>

            <div class="cs-panel">
              <div class="cs-panel__title">Planned SQL</div>
              <pre class="cs-code">{{ selected.statement }}</pre>
            </div>

            <div v-if="selected.rollback_sql" class="cs-panel">
              <div class="cs-panel__title">Rollback SQL</div>
              <pre class="cs-code">{{ selected.rollback_sql }}</pre>
            </div>

            <div class="cs-panel">
              <div class="cs-panel__title">Workflow</div>
              <div v-if="progress.workflow_name" class="cs-workflow-name">{{ progress.workflow_name }}</div>
              <div v-else class="cs-workflow-name cs-workflow-name--muted">No workflow attached yet. Validate and submit to route this draft.</div>
              <div v-if="progress.progress?.length" class="cs-steps">
                <div v-for="step in progress.progress" :key="step.step.id" class="cs-step" :class="`cs-step--${step.status}`">
                  <div class="cs-step__header">
                    <span>{{ step.step.step_order }}. {{ step.step.name }}</span>
                    <span>{{ step.status }}</span>
                  </div>
                  <div class="cs-step__meta">
                    {{ step.step.required_approvals }} required • {{ step.step.approvers.map(a => a.approver_name).join(', ') }}
                  </div>
                  <div v-if="step.approvals.length" class="cs-step__actions">
                    <div v-for="approval in step.approvals" :key="approval.id">{{ approval.username }} {{ approval.action }}<span v-if="approval.note"> · {{ approval.note }}</span></div>
                  </div>
                </div>
              </div>
            </div>

            <div v-if="progress.can_approve" class="cs-panel">
              <div class="cs-panel__title">Current Step Action</div>
              <input v-model="note" class="base-input" placeholder="Approval note or requested changes…" />
              <div class="cs-approve-actions">
                <button class="base-btn base-btn--primary base-btn--sm" @click="actOnStep('approved')">Approve</button>
                <button class="base-btn base-btn--ghost base-btn--sm" style="color:var(--danger)" @click="actOnStep('rejected')">Request Changes</button>
              </div>
            </div>

            <div v-if="selected.review_note" class="cs-note">Reviewer note: {{ selected.review_note }}</div>
            <div v-if="selected.execute_error" class="cs-error">{{ selected.execute_error }}</div>
          </template>
        </section>
      </div>
    </div>

    <Teleport to="body">
      <div v-if="createOpen" class="cs-overlay" @click.self="createOpen = false">
        <div class="cs-dialog">
          <div class="cs-dialog__head">
            <div>
              <div class="cs-kicker">Draft</div>
              <div class="cs-dialog__title">{{ editingId ? 'Revise Change Set' : 'New Change Set' }}</div>
            </div>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="createOpen = false">×</button>
          </div>
          <div class="cs-dialog__body">
            <label class="cs-label">Connection</label>
            <select v-model="form.conn_id" class="base-input">
              <option :value="null">Select connection…</option>
              <option v-for="connection in connections" :key="connection.id" :value="connection.id">
                {{ connection.name }}<template v-if="connection.environment"> ({{ connection.environment }})</template>
              </option>
            </select>

            <label class="cs-label">Database</label>
            <div class="cs-db-row">
              <template v-if="databaseMode === 'select' && databases.length > 0">
                <select v-model="form.database" class="base-input" :disabled="databasesLoading">
                  <option v-for="database in databases" :key="database" :value="database">{{ database }}</option>
                </select>
              </template>
              <template v-else>
                <input v-model="form.database" class="base-input" placeholder="Optional database name…" />
              </template>
              <button class="base-btn base-btn--ghost base-btn--sm" type="button" @click="databaseMode = databaseMode === 'select' ? 'manual' : 'select'">
                {{ databaseMode === 'select' ? 'Type Name' : 'Pick Existing' }}
              </button>
            </div>

            <label class="cs-label">Workflow</label>
            <select v-model="form.workflow_id" class="base-input" :disabled="!form.conn_id || workflowsLoading || workflows.length === 0">
              <option :value="null">{{ workflowsLoading ? 'Loading workflows…' : workflows.length ? 'Select workflow…' : 'No workflow available yet' }}</option>
              <option v-for="workflow in workflows" :key="workflow.id" :value="workflow.id">{{ workflow.name }}</option>
            </select>

            <div class="cs-field-grid">
              <div>
                <label class="cs-label">Title</label>
                <input v-model="form.title" class="base-input" placeholder="Add nullable email index" />
              </div>
              <div>
                <label class="cs-label">Impact Summary</label>
                <input v-model="form.impact_summary" class="base-input" placeholder="Optional manual summary…" />
              </div>
            </div>

            <label class="cs-label">Description</label>
            <input v-model="form.description" class="base-input" placeholder="Why is this change needed?" />

            <label class="cs-label">SQL</label>
            <textarea v-model="form.statement" class="cs-textarea" placeholder="ALTER TABLE users ADD COLUMN email TEXT;" />

            <label class="cs-label">Rollback SQL</label>
            <textarea v-model="form.rollback_sql" class="cs-textarea cs-textarea--sm" placeholder="ALTER TABLE users DROP COLUMN email;" />
          </div>
          <div class="cs-dialog__foot">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="createOpen = false">Cancel</button>
            <button class="base-btn base-btn--primary base-btn--sm" :disabled="createSubmitting || !createReady" @click="saveDraft">
              {{ createSubmitting ? 'Saving…' : 'Save Draft' }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.cs-root { width: 100%; height: 100%; display: flex; flex-direction: column; overflow: hidden; }
.cs-scroll { flex: 1; min-height: 0; overflow-y: auto; padding: 28px 32px 40px; }
.cs-header {
  display: flex;
  justify-content: space-between;
  gap: 18px;
  margin-bottom: 24px;
  padding: 22px;
  border: 1px solid color-mix(in srgb, var(--border) 76%, #0f766e 24%);
  border-radius: 16px;
  background:
    radial-gradient(circle at top left, rgba(15, 118, 110, 0.14), transparent 34%),
    linear-gradient(145deg, color-mix(in srgb, var(--bg-elevated) 90%, #ecfeff 10%), var(--bg-elevated));
}
.cs-kicker { font-size: 11px; font-weight: 800; letter-spacing: 0.08em; text-transform: uppercase; color: #0f766e; }
.cs-title { font-size: 24px; font-weight: 800; color: var(--text-primary); }
.cs-sub { margin-top: 6px; max-width: 720px; color: var(--text-secondary); line-height: 1.6; }
.cs-header-actions { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.cs-layout { display: grid; grid-template-columns: 320px minmax(0, 1fr); gap: 18px; min-height: 640px; }
.cs-list-panel, .cs-detail-panel { border: 1px solid var(--border); background: var(--bg-surface); border-radius: 16px; min-height: 0; }
.cs-list-panel { padding: 14px; }
.cs-detail-panel { padding: 18px; }
.cs-list-head { font-size: 12px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.08em; color: var(--text-muted); margin-bottom: 12px; }
.cs-list { display: flex; flex-direction: column; gap: 10px; }
.cs-list__item {
  width: 100%;
  text-align: left;
  padding: 14px;
  border-radius: 14px;
  border: 1px solid var(--border);
  background: color-mix(in srgb, var(--bg-elevated) 78%, transparent);
}
.cs-list__item--active { border-color: color-mix(in srgb, var(--brand) 58%, var(--border)); box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--brand) 22%, transparent); }
.cs-list__top, .cs-detail-meta, .cs-hero, .cs-dialog__head, .cs-dialog__foot, .cs-field-grid, .cs-db-row, .cs-grid, .cs-approve-actions { display: flex; gap: 10px; }
.cs-list__top, .cs-hero, .cs-dialog__head, .cs-dialog__foot { justify-content: space-between; align-items: flex-start; }
.cs-list__title, .cs-detail-title { font-weight: 700; color: var(--text-primary); }
.cs-list__meta, .cs-list__sub, .cs-detail-meta, .cs-workflow-name--muted { color: var(--text-secondary); }
.cs-list__meta, .cs-detail-meta { font-size: 12px; flex-wrap: wrap; }
.cs-list__sub { margin-top: 8px; font-size: 12px; line-height: 1.5; }
.cs-empty { min-height: 300px; display: grid; place-items: center; text-align: center; color: var(--text-secondary); gap: 10px; }
.cs-empty__title { font-size: 18px; font-weight: 700; color: var(--text-primary); }
.cs-empty__text { max-width: 340px; line-height: 1.6; }
.cs-status, .cs-validation {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  border-radius: 999px;
  padding: 6px 10px;
  font-size: 12px;
  font-weight: 700;
}
.cs-status--draft { background: rgba(148, 163, 184, 0.14); color: #64748b; }
.cs-status--pending { background: rgba(245, 158, 11, 0.16); color: #d97706; }
.cs-status--approved { background: rgba(16, 185, 129, 0.16); color: #059669; }
.cs-status--rejected { background: rgba(239, 68, 68, 0.14); color: #dc2626; }
.cs-panel { border: 1px solid var(--border); border-radius: 14px; padding: 14px; background: color-mix(in srgb, var(--bg-elevated) 84%, transparent); }
.cs-panel__title { margin-bottom: 10px; font-size: 12px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.08em; color: var(--text-muted); }
.cs-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; margin: 14px 0; }
.cs-code {
  margin: 0;
  padding: 14px;
  border-radius: 12px;
  background: color-mix(in srgb, var(--bg-app) 92%, #021b1a 8%);
  color: var(--text-primary);
  white-space: pre-wrap;
  overflow-x: auto;
}
.cs-impact { line-height: 1.6; color: var(--text-primary); }
.cs-validation { flex-direction: column; align-items: flex-start; border-radius: 14px; }
.cs-validation--pending { background: rgba(148, 163, 184, 0.14); color: #64748b; }
.cs-validation--passed { background: rgba(16, 185, 129, 0.14); color: #059669; }
.cs-validation--failed { background: rgba(239, 68, 68, 0.14); color: #dc2626; }
.cs-hero-actions, .cs-approve-actions { flex-wrap: wrap; }
.cs-workflow-name { color: var(--text-primary); font-weight: 600; margin-bottom: 12px; }
.cs-steps { display: flex; flex-direction: column; gap: 10px; }
.cs-step { border: 1px solid var(--border); border-radius: 12px; padding: 12px; }
.cs-step--approved { border-color: rgba(16, 185, 129, 0.28); }
.cs-step--rejected { border-color: rgba(239, 68, 68, 0.28); }
.cs-step__header { display: flex; justify-content: space-between; gap: 12px; font-weight: 600; color: var(--text-primary); }
.cs-step__meta, .cs-step__actions { margin-top: 6px; color: var(--text-secondary); font-size: 12px; line-height: 1.5; }
.cs-note, .cs-error { margin-top: 12px; padding: 12px 14px; border-radius: 12px; }
.cs-note { background: rgba(245, 158, 11, 0.14); color: #b45309; }
.cs-error { background: rgba(239, 68, 68, 0.14); color: #dc2626; }
.cs-overlay { position: fixed; inset: 0; background: rgba(15, 23, 42, 0.48); display: grid; place-items: center; padding: 24px; z-index: 80; }
.cs-dialog {
  width: min(880px, 100%);
  max-height: calc(100vh - 48px);
  overflow: auto;
  border-radius: 18px;
  border: 1px solid var(--border);
  background: var(--bg-surface);
  box-shadow: var(--shadow-lg);
}
.cs-dialog__head, .cs-dialog__foot { padding: 18px 20px; border-bottom: 1px solid var(--border); }
.cs-dialog__foot { border-top: 1px solid var(--border); border-bottom: 0; align-items: center; }
.cs-dialog__body { padding: 20px; display: flex; flex-direction: column; gap: 12px; }
.cs-dialog__title { font-size: 20px; font-weight: 800; color: var(--text-primary); }
.cs-label { font-size: 12px; font-weight: 700; color: var(--text-secondary); }
.cs-field-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.cs-textarea { min-height: 160px; resize: vertical; border-radius: 12px; border: 1px solid var(--border); background: var(--bg-elevated); color: var(--text-primary); padding: 12px 14px; }
.cs-textarea--sm { min-height: 104px; }

@media (max-width: 960px) {
  .cs-layout { grid-template-columns: 1fr; }
  .cs-grid, .cs-field-grid { grid-template-columns: 1fr; }
  .cs-header, .cs-hero { flex-direction: column; }
}
</style>
