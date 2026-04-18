<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import axios from 'axios'
import { useToast } from '@/composables/useToast'
import { useAuth } from '@/composables/useAuth'
import { useDatabases } from '@/composables/useDatabases'

interface ApprovalRequest {
  id: number
  title: string
  description: string
  conn_id: number
  connection: string
  driver: string
  environment: string
  database: string
  statement: string
  status: string
  creator_id: number
  creator_name: string
  reviewer_name?: string
  review_note?: string
  workflow_id: number
  current_step: number
  revision: number
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

interface ConnectionOption {
  id: number
  name: string
  driver: string
  environment?: string
}

interface WorkflowOption {
  id: number
  name: string
  description: string
}

interface DryRunResult {
  ok: boolean
  message: string
  details?: string[]
}

const toast = useToast()
const { user } = useAuth()
const { databases, loading: databasesLoading, fetchDatabases } = useDatabases()

const loading = ref(false)
const detailLoading = ref(false)
const requests = ref<ApprovalRequest[]>([])
const selectedId = ref<number | null>(null)
const selected = ref<ApprovalRequest | null>(null)
const progress = ref<ApprovalProgressResponse | null>(null)
const note = ref('')
const filter = ref<'all' | 'pending_review' | 'approved' | 'rejected' | 'done' | 'failed'>('all')
const createOpen = ref(false)
const createSubmitting = ref(false)
const editingRequestId = ref<number | null>(null)
const workflowsLoading = ref(false)
const dryRunLoading = ref(false)
const connections = ref<ConnectionOption[]>([])
const applicableWorkflows = ref<WorkflowOption[]>([])
const databaseMode = ref<'select' | 'manual'>('select')
const dryRunResult = ref<DryRunResult | null>(null)

const createForm = reactive({
  title: '',
  description: '',
  conn_id: null as number | null,
  database: '',
  statement: '',
  workflow_id: null as number | null,
})

const filteredRequests = computed(() =>
  filter.value === 'all' ? requests.value : requests.value.filter(request => request.status === filter.value)
)

const selectedConnectionOption = computed(() =>
  createForm.conn_id ? connections.value.find(connection => connection.id === createForm.conn_id) ?? null : null
)

const selectedWorkflowOption = computed(() =>
  createForm.workflow_id ? applicableWorkflows.value.find(workflow => workflow.id === createForm.workflow_id) ?? null : null
)

const createReady = computed(() =>
  !!createForm.conn_id &&
  !!createForm.statement.trim() &&
  applicableWorkflows.value.length > 0 &&
  !!createForm.workflow_id
)

const canExecute = computed(() => {
  if (!selected.value || !progress.value) return false
  return !!progress.value.can_execute
})

const canRevise = computed(() => {
  if (!selected.value || !progress.value) return false
  return !!progress.value.can_revise
})

async function syncSelectionWithFilter() {
  const visible = filteredRequests.value
  if (visible.length === 0) {
    selectedId.value = null
    selected.value = null
    progress.value = null
    return
  }

  const stillVisible = selectedId.value != null && visible.some(request => request.id === selectedId.value)
  const nextId = stillVisible ? selectedId.value : visible[0].id
  if (nextId == null) {
    return
  }
  if (selectedId.value !== nextId || selected.value?.id !== nextId) {
    await loadDetail(nextId)
  }
}

function defaultApprovalTitle(sql: string) {
  const firstLine = sql.trim().split('\n')[0]?.trim() || 'Write SQL change'
  return firstLine.slice(0, 80)
}

function resetCreateForm() {
  editingRequestId.value = null
  dryRunResult.value = null
  createForm.title = ''
  createForm.description = ''
  createForm.conn_id = null
  createForm.database = ''
  createForm.statement = ''
  createForm.workflow_id = null
  applicableWorkflows.value = []
  databaseMode.value = 'select'
}

function openCreate() {
  resetCreateForm()
  createOpen.value = true
}

function openRevise() {
  if (!selected.value) return
  dryRunResult.value = null
  editingRequestId.value = selected.value.id
  createForm.title = selected.value.title
  createForm.description = selected.value.description
  createForm.conn_id = selected.value.conn_id
  createForm.database = selected.value.database || ''
  createForm.statement = selected.value.statement
  createForm.workflow_id = selected.value.workflow_id
  createOpen.value = true
}

async function fetchConnections() {
  try {
    const { data } = await axios.get<ConnectionOption[]>('/api/connections')
    connections.value = data || []
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to load connections')
  }
}

async function loadApplicableWorkflows(connID: number | null) {
  applicableWorkflows.value = []
  createForm.workflow_id = null
  if (!connID) return
  workflowsLoading.value = true
  try {
    const { data } = await axios.get<WorkflowOption[]>('/api/workflows/applicable', {
      params: { conn_id: connID },
    })
    applicableWorkflows.value = data || []
    createForm.workflow_id = applicableWorkflows.value[0]?.id ?? null
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to load applicable workflows')
  } finally {
    workflowsLoading.value = false
  }
}

async function loadConnectionDatabases(connID: number | null) {
  createForm.database = ''
  if (!connID) {
    databaseMode.value = 'select'
    return
  }
  await fetchDatabases(connID)
  if (databases.value.length > 0) {
    databaseMode.value = 'select'
    createForm.database = databases.value[0] ?? ''
  } else {
    databaseMode.value = 'manual'
  }
}

async function fetchRequests() {
  loading.value = true
  try {
    const { data } = await axios.get<ApprovalRequest[]>('/api/approval-requests')
    requests.value = data || []
    await syncSelectionWithFilter()
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to load approval requests')
  } finally {
    loading.value = false
  }
}

async function loadDetail(id: number) {
  detailLoading.value = true
  selectedId.value = id
  try {
    const [{ data: requestData }, { data: progressData }] = await Promise.all([
      axios.get<ApprovalRequest>(`/api/approval-requests/${id}`),
      axios.get<ApprovalProgressResponse>(`/api/approval-requests/${id}/approval-progress`),
    ])
    selected.value = requestData
    progress.value = progressData
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to load request detail')
  } finally {
    detailLoading.value = false
  }
}

async function actOnStep(action: 'approved' | 'rejected') {
  if (!selected.value) return
  try {
    await axios.post(`/api/approval-requests/${selected.value.id}/approve-step`, {
      action,
      note: note.value.trim(),
    })
    note.value = ''
    toast.success(action === 'approved' ? 'Approval recorded' : 'Changes requested')
    await fetchRequests()
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to record action')
  }
}

async function executeRequest() {
  if (!selected.value) return
  try {
    await axios.post(`/api/approval-requests/${selected.value.id}/execute`)
    toast.success('Request executed')
    await fetchRequests()
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to execute request')
  }
}

async function submitCreate() {
  if (!createForm.conn_id) {
    toast.error('Connection is required')
    return
  }
  if (!createForm.statement.trim()) {
    toast.error('SQL statement is required')
    return
  }
  if (!createForm.title.trim()) {
    createForm.title = defaultApprovalTitle(createForm.statement)
  }
  if (applicableWorkflows.value.length === 0) {
    toast.error('No active workflow applies to this connection')
    return
  }
  createSubmitting.value = true
  try {
    const payload = {
      title: createForm.title.trim(),
      description: createForm.description.trim(),
      conn_id: createForm.conn_id,
      database: createForm.database.trim(),
      statement: createForm.statement.trim(),
      workflow_id: createForm.workflow_id || 0,
    }
    const { data } = editingRequestId.value
      ? await axios.put<ApprovalRequest>(`/api/approval-requests/${editingRequestId.value}`, payload)
      : await axios.post<ApprovalRequest>('/api/approval-requests', payload)
    createOpen.value = false
    toast.success(editingRequestId.value ? `Approval request #${data.id} resubmitted` : `Approval request #${data.id} created`)
    await fetchRequests()
    if (data.id) {
      await loadDetail(data.id)
    }
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to create approval request')
  } finally {
    createSubmitting.value = false
  }
}

async function runDryRun() {
  if (!createForm.conn_id) {
    toast.error('Connection is required')
    return
  }
  if (!createForm.statement.trim()) {
    toast.error('SQL statement is required')
    return
  }
  dryRunLoading.value = true
  dryRunResult.value = null
  try {
    const { data } = await axios.post(`/api/connections/${createForm.conn_id}/explain`, {
      sql: createForm.statement.trim(),
      database: createForm.database.trim(),
    })
    const details: string[] = []
    if (Array.isArray(data?.raw)) {
      details.push(...data.raw.slice(0, 5).map((row: unknown[]) => row.map(cell => String(cell ?? '')).join(' | ')))
    } else if (data?.format) {
      details.push(`Plan format: ${data.format}`)
    }
    dryRunResult.value = {
      ok: true,
      message: 'Dry run passed. The database accepted the statement for planning/validation.',
      details,
    }
  } catch (error: any) {
    dryRunResult.value = {
      ok: false,
      message: error.response?.data?.error || 'Dry run failed',
    }
  } finally {
    dryRunLoading.value = false
  }
}

async function runSelectedDryRun() {
  if (!selected.value) return
  dryRunLoading.value = true
  dryRunResult.value = null
  try {
    const { data } = await axios.post(`/api/connections/${selected.value.conn_id}/explain`, {
      sql: selected.value.statement.trim(),
      database: selected.value.database?.trim() || '',
    })
    const details: string[] = []
    if (Array.isArray(data?.raw)) {
      details.push(...data.raw.slice(0, 5).map((row: unknown[]) => row.map(cell => String(cell ?? '')).join(' | ')))
    } else if (data?.format) {
      details.push(`Plan format: ${data.format}`)
    }
    dryRunResult.value = {
      ok: true,
      message: 'Dry run passed. The database accepted the statement for planning/validation.',
      details,
    }
  } catch (error: any) {
    dryRunResult.value = {
      ok: false,
      message: error.response?.data?.error || 'Dry run failed',
    }
  } finally {
    dryRunLoading.value = false
  }
}

function statusClass(status: string) {
  return {
    'req-status--pending': status === 'pending_review',
    'req-status--approved': status === 'approved' || status === 'done',
    'req-status--rejected': status === 'rejected' || status === 'failed',
  }
}

watch(() => createForm.conn_id, (connID) => {
  void loadApplicableWorkflows(connID)
  void loadConnectionDatabases(connID)
})

watch(() => createForm.statement, (sql) => {
  dryRunResult.value = null
  if (!createForm.title.trim() && sql.trim()) {
    createForm.title = defaultApprovalTitle(sql)
  }
})

watch(filter, () => {
  void syncSelectionWithFilter()
})

onMounted(async () => {
  await Promise.all([fetchRequests(), fetchConnections()])
})
</script>

<template>
  <div class="req-root">
    <div class="req-scroll">
      <div class="req-header">
        <div class="req-header-copy">
          <div class="req-kicker">Approval Center</div>
          <div class="req-title">Approval Requests</div>
          <div class="req-sub">Track write SQL awaiting approval and execute approved requests.</div>
        </div>
        <div class="req-header-actions">
          <button class="base-btn base-btn--primary base-btn--sm" @click="openCreate">New Request</button>
          <select v-model="filter" class="base-input">
            <option value="all">All statuses</option>
            <option value="pending_review">Pending review</option>
            <option value="approved">Approved</option>
            <option value="rejected">Rejected</option>
            <option value="done">Executed</option>
            <option value="failed">Failed</option>
          </select>
          <button class="base-btn base-btn--ghost base-btn--sm" @click="fetchRequests">Refresh</button>
        </div>
      </div>

      <div class="req-layout">
        <aside class="req-list-wrap">
          <div class="req-list-head">Requests</div>
          <div v-if="loading" class="req-empty">Loading requests…</div>
          <div v-else-if="filteredRequests.length === 0" class="req-empty">
            <div class="req-empty__title">No approval requests yet</div>
            <div class="req-empty__text">Requests appear here after a non-admin user submits write SQL against a connection covered by an active workflow.</div>
            <div class="req-empty__actions">
              <button class="base-btn base-btn--primary base-btn--sm" @click="openCreate">Create Request</button>
            </div>
          </div>
          <div v-else class="req-list">
            <button
              v-for="request in filteredRequests"
              :key="request.id"
              class="req-list__item"
              :class="{ 'req-list__item--active': selectedId === request.id }"
              @click="loadDetail(request.id)"
            >
              <div class="req-list__top">
                <div class="req-list__title">{{ request.title }}</div>
                <span class="req-status" :class="statusClass(request.status)">{{ request.status.replace('_', ' ') }}</span>
              </div>
              <div class="req-list__meta">
                <span class="req-list__chip">{{ request.connection }}</span>
                <span class="req-list__chip">{{ request.creator_name }}</span>
              </div>
              <div class="req-list__sub">{{ new Date(request.created_at).toLocaleString() }}</div>
            </button>
          </div>
        </aside>

        <section class="req-detail-wrap">
          <div v-if="detailLoading" class="req-empty">Loading request detail…</div>
          <div v-else-if="!selected || !progress" class="req-empty">
            <div class="req-empty__title">Nothing to review yet</div>
            <div class="req-empty__text">Create a request directly here by choosing a connection and pasting the write SQL that needs approval.</div>
            <div class="req-empty__actions">
              <button class="base-btn base-btn--primary base-btn--sm" @click="openCreate">New Request</button>
            </div>
          </div>
          <template v-else>
            <div class="req-hero">
              <div class="req-hero__grid" />
            </div>
            <div class="req-detail-head">
              <div>
                <div class="req-detail-title">{{ selected.title }}</div>
                <div class="req-detail-meta">
                  <span class="req-status" :class="statusClass(selected.status)">{{ selected.status.replace('_', ' ') }}</span>
                  <span>{{ selected.connection }} • {{ selected.environment }}</span>
                  <span v-if="selected.database">{{ selected.database }}</span>
                  <span>Revision {{ selected.revision }}</span>
                </div>
              </div>
              <div class="req-detail-actions">
                <button v-if="canExecute" class="base-btn base-btn--primary base-btn--sm" @click="executeRequest">Execute</button>
                <button v-if="canRevise" class="base-btn base-btn--ghost base-btn--xs" @click="openRevise">Revise</button>
                <button class="base-btn base-btn--ghost base-btn--xs" :disabled="dryRunLoading" @click="runSelectedDryRun">
                  {{ dryRunLoading ? 'Running Dry Run…' : 'Dry Run' }}
                </button>
              </div>
            </div>

            <div v-if="selected.description" class="req-panel req-panel--desc">{{ selected.description }}</div>

            <div class="req-panel">
              <div class="req-panel__title">SQL</div>
              <pre class="req-sql">{{ selected.statement }}</pre>
            </div>

            <div v-if="dryRunResult" class="req-panel">
              <div class="req-panel__title">Dry Run</div>
              <div class="req-dryrun" :class="{ 'req-dryrun--ok': dryRunResult.ok, 'req-dryrun--err': !dryRunResult.ok }">
                <div class="req-dryrun__title">{{ dryRunResult.ok ? 'Dry run result' : 'Dry run error' }}</div>
                <div class="req-dryrun__message">{{ dryRunResult.message }}</div>
                <div v-if="dryRunResult.details?.length" class="req-dryrun__details">
                  <div v-for="(line, index) in dryRunResult.details" :key="index">{{ line }}</div>
                </div>
              </div>
            </div>

            <div class="req-panel">
              <div class="req-panel__title">Workflow</div>
              <div class="req-workflow">{{ progress.workflow_name }}</div>
              <div class="req-steps">
                <div v-for="step in progress.progress" :key="step.step.id" class="req-step" :class="`req-step--${step.status}`">
                  <div class="req-step__rail" />
                  <div class="req-step__header">
                    <span>{{ step.step.step_order }}. {{ step.step.name }}</span>
                    <span>{{ step.status }}</span>
                  </div>
                  <div class="req-step__meta">
                    {{ step.step.required_approvals }} required •
                    {{ step.step.approvers.map(approver => approver.approver_name).join(', ') }}
                  </div>
                  <div v-if="step.approvals.length > 0" class="req-step__actions">
                    <div v-for="approval in step.approvals" :key="approval.id" class="req-step__action">
                      <strong>{{ approval.username }}</strong> {{ approval.action }}
                      <span v-if="approval.note"> · {{ approval.note }}</span>
                    </div>
                  </div>
                </div>
              </div>
            </div>

            <div v-if="progress.can_approve" class="req-panel">
              <div class="req-panel__title">Current Step Action</div>
              <input v-model="note" class="base-input" placeholder="Approval note or requested changes…" />
              <div class="req-approve__actions">
                <button class="base-btn base-btn--primary base-btn--sm" @click="actOnStep('approved')">Approve</button>
                <button class="base-btn base-btn--ghost base-btn--sm" style="color:var(--danger)" @click="actOnStep('rejected')">Request Changes</button>
              </div>
            </div>

            <div v-if="selected.execute_error" class="req-error">{{ selected.execute_error }}</div>
          </template>
        </section>
      </div>
    </div>

    <Teleport to="body">
        <div v-if="createOpen" class="req-overlay" @click.self="createOpen = false">
        <div class="req-dialog">
          <div class="req-dialog__head">
            <div>
              <div class="req-dialog__eyebrow">Approval Flow</div>
              <div class="req-dialog__title">{{ editingRequestId ? 'Revise Approval Request' : 'New Approval Request' }}</div>
            </div>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="createOpen = false">×</button>
          </div>
          <div class="req-dialog__body">
            <div class="req-form-grid">
              <section class="req-form-section">
                <div class="req-section-head">
                  <span class="req-section-step">1</span>
                  <div>
                    <div class="req-section-title">Target</div>
                    <div class="req-section-sub">Choose where this change will run.</div>
                  </div>
                </div>

                <label class="req-label">Connection</label>
                <select v-model="createForm.conn_id" class="base-input">
                  <option :value="null">Select connection…</option>
                  <option v-for="connection in connections" :key="connection.id" :value="connection.id">
                    {{ connection.name }}<template v-if="connection.environment"> ({{ connection.environment }})</template>
                  </option>
                </select>

                <label class="req-label">Database</label>
                <div class="req-db-row">
                  <template v-if="databaseMode === 'select' && databases.length > 0">
                    <select v-model="createForm.database" class="base-input" :disabled="databasesLoading">
                      <option v-for="database in databases" :key="database" :value="database">{{ database }}</option>
                    </select>
                  </template>
                  <template v-else>
                    <input v-model="createForm.database" class="base-input" placeholder="Optional database name…" />
                  </template>
                  <button
                    class="base-btn base-btn--ghost base-btn--sm"
                    type="button"
                    :disabled="!createForm.conn_id || (databaseMode === 'select' && databases.length === 0)"
                    @click="databaseMode = databaseMode === 'select' ? 'manual' : 'select'"
                  >
                    {{ databaseMode === 'select' ? 'Type Name' : 'Pick Existing' }}
                  </button>
                </div>
                <div v-if="databaseMode === 'select' && databasesLoading" class="req-helper">Loading databases…</div>
                <div v-else-if="databaseMode === 'select' && createForm.conn_id && databases.length === 0" class="req-helper">
                  No database list available for this connection. Enter the name manually.
                </div>

                <div class="req-summary-card" :class="{ 'req-summary-card--muted': !selectedConnectionOption }">
                  <div class="req-summary-label">Selected target</div>
                  <div v-if="selectedConnectionOption" class="req-summary-value">
                    {{ selectedConnectionOption.name }}
                    <span v-if="selectedConnectionOption.environment" class="req-summary-pill">{{ selectedConnectionOption.environment }}</span>
                  </div>
                  <div v-else class="req-summary-placeholder">Choose a connection to continue.</div>
                </div>
              </section>

              <section class="req-form-section">
                <div class="req-section-head">
                  <span class="req-section-step">2</span>
                  <div>
                    <div class="req-section-title">Approval Route</div>
                    <div class="req-section-sub">Pick the workflow that should review this change.</div>
                  </div>
                </div>

                <label class="req-label">Workflow</label>
                <select v-model="createForm.workflow_id" class="base-input" :disabled="!createForm.conn_id || workflowsLoading || applicableWorkflows.length === 0">
                  <option :value="null">{{ workflowsLoading ? 'Loading workflows…' : applicableWorkflows.length ? 'Select workflow…' : 'No workflow available' }}</option>
                  <option v-for="workflow in applicableWorkflows" :key="workflow.id" :value="workflow.id">
                    {{ workflow.name }}
                  </option>
                </select>
                <div v-if="createForm.conn_id && !workflowsLoading && applicableWorkflows.length === 0" class="req-helper req-helper--warn">
                  No active workflow applies to this connection for your user.
                </div>

                <div class="req-summary-card" :class="{ 'req-summary-card--muted': !selectedWorkflowOption }">
                  <div class="req-summary-label">Workflow details</div>
                  <div v-if="selectedWorkflowOption" class="req-summary-value">{{ selectedWorkflowOption.name }}</div>
                  <div v-if="selectedWorkflowOption?.description" class="req-summary-text">{{ selectedWorkflowOption.description }}</div>
                  <div v-else-if="selectedWorkflowOption" class="req-summary-text">No description provided.</div>
                  <div v-else class="req-summary-placeholder">Select a connection to load available workflows.</div>
                </div>
              </section>
            </div>

            <section class="req-form-section req-form-section--full">
              <div class="req-section-head">
                <span class="req-section-step">3</span>
                <div>
                  <div class="req-section-title">Change Request</div>
                  <div class="req-section-sub">Describe the change and paste the SQL reviewers will approve.</div>
                </div>
              </div>

              <div class="req-field-row">
                <div>
                  <label class="req-label">Title</label>
                  <input v-model="createForm.title" class="base-input" placeholder="Short request title…" />
                </div>
                <div>
                  <label class="req-label">Description</label>
                  <input v-model="createForm.description" class="base-input" placeholder="Why is this change needed?" />
                </div>
              </div>

              <label class="req-label">SQL</label>
              <textarea v-model="createForm.statement" class="req-textarea" placeholder="UPDATE table_name SET column = 'value' WHERE id = 1;" />
              <div class="req-sql-actions">
                <div class="req-helper">Write the exact SQL you want approved. This request will not execute immediately.</div>
                <button class="base-btn base-btn--ghost base-btn--sm" :disabled="dryRunLoading || !createForm.conn_id || !createForm.statement.trim()" @click="runDryRun">
                  {{ dryRunLoading ? 'Running Dry Run…' : 'Dry Run' }}
                </button>
              </div>
              <div v-if="dryRunResult" class="req-dryrun" :class="{ 'req-dryrun--ok': dryRunResult.ok, 'req-dryrun--err': !dryRunResult.ok }">
                <div class="req-dryrun__title">{{ dryRunResult.ok ? 'Dry run result' : 'Dry run error' }}</div>
                <div class="req-dryrun__message">{{ dryRunResult.message }}</div>
                <div v-if="dryRunResult.details?.length" class="req-dryrun__details">
                  <div v-for="(line, index) in dryRunResult.details" :key="index">{{ line }}</div>
                </div>
              </div>
              <div v-if="editingRequestId && selected?.review_note" class="req-helper req-helper--warn">Reviewer feedback: {{ selected.review_note }}</div>
            </section>
          </div>
          <div class="req-dialog__foot">
            <div class="req-footer-note">
              <span class="req-footer-dot" :class="{ 'req-footer-dot--ready': createReady }" />
              {{ createReady ? 'Ready to submit for approval' : 'Complete the target, workflow, and SQL fields' }}
            </div>
            <div class="req-footer-actions">
              <button class="base-btn base-btn--ghost base-btn--sm" @click="createOpen = false">Cancel</button>
              <button class="base-btn base-btn--primary base-btn--sm" :disabled="createSubmitting || !createReady" @click="submitCreate">
              {{ createSubmitting ? (editingRequestId ? 'Resubmitting…' : 'Creating…') : (editingRequestId ? 'Resubmit Request' : 'Create Request') }}
              </button>
            </div>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.req-root { width: 100%; height: 100%; display: flex; flex-direction: column; overflow: hidden; }
.req-scroll { flex: 1; min-height: 0; overflow-y: auto; padding: 28px 32px 40px; }
.req-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 24px;
  padding: 20px 22px;
  border: 1px solid color-mix(in srgb, var(--border) 72%, #f59e0b 28%);
  border-radius: 14px;
  background:
    radial-gradient(circle at top left, rgba(245, 158, 11, 0.16), transparent 34%),
    linear-gradient(135deg, color-mix(in srgb, var(--bg-elevated) 86%, #f8fafc 14%), var(--bg-elevated));
  position: relative;
  overflow: hidden;
}
.req-header::after {
  content: '';
  position: absolute;
  inset: auto -80px -90px auto;
  width: 220px;
  height: 220px;
  background: radial-gradient(circle, rgba(16, 185, 129, 0.08), transparent 68%);
  pointer-events: none;
}
.req-header-copy { position: relative; z-index: 1; max-width: 560px; }
.req-kicker {
  font-size: 11px;
  font-weight: 800;
  letter-spacing: 0.6px;
  text-transform: uppercase;
  color: #f59e0b;
  margin-bottom: 6px;
}
.req-title { font-size: 24px; font-weight: 800; color: var(--text-primary); letter-spacing: -0.02em; }
.req-sub { font-size: 13px; color: var(--text-muted); margin-top: 6px; line-height: 1.6; }
.req-header-actions { min-width: 220px; display: flex; gap: 8px; flex-wrap: wrap; }
.req-header-actions .base-input { min-width: 180px; }
.req-layout { display: grid; grid-template-columns: 320px 1fr; gap: 18px; min-height: 0; }
.req-list-wrap, .req-detail-wrap, .req-panel, .req-panel--desc {
  background: linear-gradient(180deg, color-mix(in srgb, var(--bg-elevated) 90%, #ffffff 10%), var(--bg-elevated));
  border: 1px solid var(--border);
  border-radius: 12px;
  box-shadow: 0 8px 30px rgba(15, 23, 42, 0.08);
}
.req-list-wrap { overflow: hidden; }
.req-list-head { padding: 10px 14px; border-bottom: 1px solid var(--border); background: var(--bg-surface); font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.4px; color: var(--text-muted); }
.req-list { min-height: 420px; }
.req-list__item {
  width: 100%;
  border: 0;
  border-bottom: 1px solid var(--border);
  background: linear-gradient(180deg, transparent, transparent);
  text-align: left;
  padding: 14px 16px;
  cursor: pointer;
  display: flex;
  flex-direction: column;
  gap: 8px;
  transition: background 0.16s ease, transform 0.16s ease;
}
.req-list__item:hover {
  background: linear-gradient(135deg, color-mix(in srgb, var(--bg-hover) 70%, #f59e0b 30%), var(--bg-hover));
  transform: translateY(-1px);
}
.req-list__item--active {
  background: linear-gradient(135deg, color-mix(in srgb, var(--bg-hover) 55%, #f59e0b 45%), var(--bg-hover));
  box-shadow: inset 3px 0 0 #f59e0b;
}
.req-list__top { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.req-list__title { font-size: 13px; font-weight: 700; color: var(--text-primary); line-height: 1.4; }
.req-list__meta, .req-list__sub { font-size: 12px; color: var(--text-muted); display: flex; gap: 8px; flex-wrap: wrap; }
.req-list__chip {
  display: inline-flex;
  align-items: center;
  padding: 3px 9px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--bg-body) 76%, #ffffff 24%);
  border: 1px solid color-mix(in srgb, var(--border) 82%, #f59e0b 18%);
}
.req-detail-wrap { min-height: 420px; padding: 16px; display: flex; flex-direction: column; gap: 16px; position: relative; overflow: hidden; }
.req-hero {
  height: 92px;
  border-radius: 12px;
  background:
    radial-gradient(circle at top left, rgba(245, 158, 11, 0.26), transparent 34%),
    radial-gradient(circle at 80% 10%, rgba(34, 197, 94, 0.12), transparent 28%),
    linear-gradient(135deg, #111827, #1f2937 55%, #0f172a);
  border: 1px solid rgba(255,255,255,0.08);
  position: relative;
  overflow: hidden;
}
.req-hero__grid {
  position: absolute;
  inset: 0;
  background-image:
    linear-gradient(rgba(255,255,255,0.06) 1px, transparent 1px),
    linear-gradient(90deg, rgba(255,255,255,0.06) 1px, transparent 1px);
  background-size: 22px 22px;
  mask-image: linear-gradient(180deg, rgba(255,255,255,0.75), transparent);
}
.req-detail-head { display: flex; justify-content: space-between; gap: 16px; margin-top: -54px; padding: 0 10px; position: relative; z-index: 1; }
.req-detail-head > div:first-child {
  background: color-mix(in srgb, var(--bg-elevated) 88%, #ffffff 12%);
  border: 1px solid color-mix(in srgb, var(--border) 80%, #f59e0b 20%);
  border-radius: 12px;
  padding: 14px 16px;
  backdrop-filter: blur(12px);
  box-shadow: 0 16px 30px rgba(15, 23, 42, 0.16);
}
.req-detail-title { font-size: 20px; font-weight: 800; color: var(--text-primary); margin-bottom: 6px; letter-spacing: -0.02em; }
.req-detail-meta { display: flex; flex-wrap: wrap; gap: 8px; font-size: 12px; color: var(--text-muted); }
.req-detail-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-self: flex-start;
  padding: 10px;
  border: 1px solid color-mix(in srgb, var(--border) 80%, #f59e0b 20%);
  border-radius: 12px;
  background: color-mix(in srgb, var(--bg-elevated) 88%, #ffffff 12%);
  backdrop-filter: blur(12px);
  box-shadow: 0 16px 30px rgba(15, 23, 42, 0.16);
}
.req-panel, .req-panel--desc { padding: 14px 16px; display: flex; flex-direction: column; gap: 10px; border-radius: 12px; }
.req-panel__title { font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.4px; color: var(--text-muted); }
.req-panel--desc { font-size: 13px; color: var(--text-secondary); }
.req-sql {
  margin: 0;
  padding: 14px;
  border-radius: 10px;
  background: linear-gradient(180deg, #111827, #0f172a);
  border: 1px solid rgba(255,255,255,0.08);
  color: #dbe4f0;
  overflow: auto;
  font-size: 12px;
  line-height: 1.5;
}
.req-workflow { font-size: 14px; font-weight: 700; color: var(--text-primary); }
.req-steps { display: flex; flex-direction: column; gap: 10px; }
.req-step {
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 12px 12px 12px 16px;
  background: linear-gradient(180deg, color-mix(in srgb, var(--bg-body) 86%, #ffffff 14%), var(--bg-body));
  position: relative;
  overflow: hidden;
}
.req-step__rail {
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 4px;
  background: color-mix(in srgb, var(--border) 55%, #94a3b8 45%);
}
.req-step__header { display: flex; justify-content: space-between; gap: 10px; font-size: 13px; font-weight: 700; color: var(--text-primary); text-transform: capitalize; }
.req-step__meta, .req-step__action { font-size: 12px; color: var(--text-muted); margin-top: 6px; }
.req-approve__actions { display: flex; gap: 8px; margin-top: 10px; }
.req-status {
  display: inline-flex;
  align-items: center;
  padding: 4px 10px;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: color-mix(in srgb, var(--bg-body) 82%, #ffffff 18%);
  color: var(--text-secondary);
  text-transform: capitalize;
  font-size: 11px;
  font-weight: 700;
}
.req-status--pending {
  color: #b45309;
  border-color: rgba(245, 158, 11, 0.35);
  background: rgba(245, 158, 11, 0.12);
}
.req-status--approved {
  color: #15803d;
  border-color: rgba(34, 197, 94, 0.3);
  background: rgba(34, 197, 94, 0.12);
}
.req-status--rejected {
  color: #b91c1c;
  border-color: rgba(248, 113, 113, 0.3);
  background: rgba(248, 113, 113, 0.12);
}
.req-step--approved { border-color: rgba(34, 197, 94, 0.25); }
.req-step--approved .req-step__rail { background: linear-gradient(180deg, #22c55e, #16a34a); }
.req-step--rejected { border-color: rgba(248, 113, 113, 0.25); }
.req-step--rejected .req-step__rail { background: linear-gradient(180deg, #fb7185, #ef4444); }
.req-step--pending .req-step__rail { background: linear-gradient(180deg, #f59e0b, #d97706); }
.req-step--waiting { opacity: 0.75; }
.req-error { padding: 12px; border-radius: 8px; background: rgba(248, 113, 113, 0.08); color: #f87171; font-size: 12px; border: 1px solid rgba(248, 113, 113, 0.18); }
.req-empty { padding: 28px; color: var(--text-muted); }
.req-empty__title { font-size: 13px; font-weight: 600; color: var(--text-primary); margin-bottom: 6px; }
.req-empty__text { font-size: 12px; line-height: 1.5; }
.req-empty__actions { margin-top: 12px; }
.req-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.55); display: flex; align-items: center; justify-content: center; z-index: 1000; }
.req-dialog {
  background:
    radial-gradient(circle at top left, rgba(245, 158, 11, 0.12), transparent 28%),
    linear-gradient(180deg, color-mix(in srgb, var(--bg-elevated) 92%, #ffffff 8%), var(--bg-elevated));
  border: 1px solid color-mix(in srgb, var(--border) 82%, #f59e0b 18%);
  border-radius: 14px;
  width: min(700px, 92vw);
  max-height: 90vh;
  overflow: auto;
  box-shadow: 0 24px 80px rgba(0,0,0,0.48);
}
.req-dialog__head, .req-dialog__foot { display: flex; align-items: center; justify-content: space-between; padding: 16px 20px; border-bottom: 1px solid var(--border); }
.req-dialog__foot { border-bottom: none; border-top: 1px solid var(--border); justify-content: flex-end; gap: 8px; }
.req-dialog__eyebrow { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.5px; color: var(--brand); margin-bottom: 4px; }
.req-dialog__title { font-size: 15px; color: var(--text-primary); font-weight: 600; }
.req-dialog__body { padding: 20px; display: flex; flex-direction: column; gap: 16px; }
.req-form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.req-form-section {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: linear-gradient(180deg, color-mix(in srgb, var(--bg-body) 90%, #ffffff 10%), var(--bg-body));
}
.req-form-section--full {
  background:
    linear-gradient(180deg, color-mix(in srgb, var(--bg-elevated) 88%, #ffffff 12%), var(--bg-elevated));
}
.req-section-head { display: flex; align-items: flex-start; gap: 10px; margin-bottom: 2px; }
.req-section-step {
  width: 22px;
  height: 22px;
  border-radius: 999px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: var(--brand-dim);
  color: var(--brand);
  font-size: 11px;
  font-weight: 700;
  flex-shrink: 0;
}
.req-section-title { font-size: 13px; font-weight: 600; color: var(--text-primary); }
.req-section-sub { font-size: 12px; color: var(--text-muted); margin-top: 2px; line-height: 1.4; }
.req-label { display: block; font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.4px; color: var(--text-muted); margin-bottom: 4px; }
.req-helper { font-size: 12px; color: var(--text-muted); }
.req-helper--warn { color: #f59e0b; }
.req-db-row { display: grid; grid-template-columns: 1fr auto; gap: 8px; align-items: start; }
.req-summary-card {
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: linear-gradient(180deg, color-mix(in srgb, var(--bg-elevated) 92%, #ffffff 8%), var(--bg-elevated));
}
.req-summary-card--muted { opacity: 0.7; }
.req-summary-label { font-size: 10.5px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.4px; color: var(--text-muted); margin-bottom: 6px; }
.req-summary-value { font-size: 13px; font-weight: 600; color: var(--text-primary); display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.req-summary-text { font-size: 12px; color: var(--text-muted); line-height: 1.5; margin-top: 4px; }
.req-summary-placeholder { font-size: 12px; color: var(--text-muted); }
.req-summary-pill {
  display: inline-flex;
  align-items: center;
  padding: 2px 8px;
  border-radius: 999px;
  background: var(--bg-body);
  border: 1px solid var(--border);
  font-size: 10.5px;
  color: var(--text-muted);
  text-transform: capitalize;
}
.req-field-row { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.req-sql-actions { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.req-dryrun {
  padding: 12px;
  border-radius: 10px;
  border: 1px solid var(--border);
  background: linear-gradient(180deg, color-mix(in srgb, var(--bg-elevated) 92%, #ffffff 8%), var(--bg-elevated));
}
.req-dryrun--ok {
  border-color: rgba(34, 197, 94, 0.28);
  background: rgba(34, 197, 94, 0.06);
}
.req-dryrun--err {
  border-color: rgba(248, 113, 113, 0.28);
  background: rgba(248, 113, 113, 0.06);
}
.req-dryrun__title { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.4px; color: var(--text-muted); margin-bottom: 6px; }
.req-dryrun__message { font-size: 12px; color: var(--text-primary); line-height: 1.5; }
.req-dryrun__details {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 11px;
  color: var(--text-muted);
  font-family: var(--mono, monospace);
}
.req-textarea {
  min-height: 180px;
  width: 100%;
  padding: 10px 12px;
  background: var(--bg-body);
  border: 1px solid var(--border);
  border-radius: 6px;
  color: var(--text-primary);
  font-size: 12px;
  line-height: 1.5;
  font-family: var(--mono, monospace);
  resize: vertical;
  box-sizing: border-box;
  outline: none;
}
.req-textarea:focus { border-color: var(--brand); }
.req-footer-note { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--text-muted); margin-right: auto; }
.req-footer-dot {
  width: 8px;
  height: 8px;
  border-radius: 999px;
  background: #f59e0b;
  flex-shrink: 0;
}
.req-footer-dot--ready { background: #22c55e; }
.req-footer-actions { display: flex; gap: 8px; }

@media (max-width: 960px) {
  .req-scroll { padding: 18px 16px 32px; }
  .req-layout { grid-template-columns: 1fr; }
  .req-header { flex-direction: column; }
  .req-header-actions { width: 100%; min-width: 0; }
  .req-form-grid, .req-field-row { grid-template-columns: 1fr; }
  .req-sql-actions { flex-direction: column; align-items: stretch; }
  .req-dialog__foot { flex-direction: column; align-items: stretch; }
  .req-footer-note { margin-right: 0; }
  .req-footer-actions { width: 100%; justify-content: flex-end; }
}
</style>
