<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import { useDatabases } from '@/composables/useDatabases'
import { useToast } from '@/composables/useToast'
import { useAuth } from '@/composables/useAuth'
import { downloadBlob } from '@/utils/export'

interface BackupDownloadRequest {
  id: number
  title: string
  description: string
  conn_id: number
  connection: string
  driver: string
  environment: string
  database_name: string
  status: string
  creator_id: number
  creator_name: string
  reviewer_name?: string
  review_note: string
  workflow_id: number
  current_step: number
  revision: number
  execute_error: string
  created_at: string
  updated_at: string
}

interface WorkflowOption {
  id: number
  name: string
  description: string
}

const toast = useToast()
const { connections, fetchConnections } = useConnections()
const { databases, fetchDatabases, loading: databasesLoading } = useDatabases()
const { hasPermission, isAdmin } = useAuth()

const canDirectBackup = computed(() => isAdmin.value || hasPermission('backups.manage'))
const canApprove = computed(() => isAdmin.value || hasPermission('query.approve'))

const activeTab = ref<'request' | 'direct' | 'restore'>('request')
const requestsLoading = ref(false)
const createLoading = ref(false)
const reviewLoading = ref(false)
const requests = ref<BackupDownloadRequest[]>([])
const selectedRequestId = ref<number | null>(null)
const selectedRequest = ref<BackupDownloadRequest | null>(null)
const filter = ref<'all' | 'pending_review' | 'approved' | 'rejected' | 'done' | 'failed'>('all')
const applicableWorkflows = ref<WorkflowOption[]>([])
const workflowsLoading = ref(false)
const reviewNote = ref('')

const requestForm = reactive({
  title: '',
  description: '',
  conn_id: null as number | null,
  database: '',
  workflow_id: null as number | null,
})

const directConnId = ref<number | null>(null)
const directDatabase = ref('')
const restoreConnId = ref<number | null>(null)
const restoreSQL = ref('')
const restoreResult = ref('')
const restoreLoading = ref(false)
const restoreError = ref('')

const filteredRequests = computed(() =>
  filter.value === 'all' ? requests.value : requests.value.filter((item) => item.status === filter.value),
)

async function fetchRequests() {
  requestsLoading.value = true
  try {
    const { data } = await axios.get<BackupDownloadRequest[]>('/api/backup-download-requests')
    requests.value = Array.isArray(data) ? data : []
    if (!selectedRequestId.value || !requests.value.some((item) => item.id === selectedRequestId.value)) {
      selectedRequestId.value = filteredRequests.value[0]?.id ?? requests.value[0]?.id ?? null
    }
    if (selectedRequestId.value) {
      await loadRequest(selectedRequestId.value)
    } else {
      selectedRequest.value = null
    }
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to load backup requests')
  } finally {
    requestsLoading.value = false
  }
}

async function loadRequest(id: number) {
  selectedRequestId.value = id
  try {
    const { data } = await axios.get<BackupDownloadRequest>(`/api/backup-download-requests/${id}`)
    selectedRequest.value = data
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to load backup request')
  }
}

async function loadApplicableWorkflows(connId: number | null) {
  applicableWorkflows.value = []
  requestForm.workflow_id = null
  if (!connId) return
  workflowsLoading.value = true
  try {
    const { data } = await axios.get<WorkflowOption[]>('/api/workflows/applicable', {
      params: { conn_id: connId },
    })
    applicableWorkflows.value = Array.isArray(data) ? data : []
    requestForm.workflow_id = applicableWorkflows.value[0]?.id ?? null
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to load applicable workflows')
  } finally {
    workflowsLoading.value = false
  }
}

async function onRequestConnChange() {
  requestForm.database = ''
  if (requestForm.conn_id) {
    await fetchDatabases(requestForm.conn_id)
    requestForm.database = databases.value[0] ?? ''
    await loadApplicableWorkflows(requestForm.conn_id)
  }
}

async function onDirectConnChange() {
  directDatabase.value = ''
  if (directConnId.value) {
    await fetchDatabases(directConnId.value)
    directDatabase.value = databases.value[0] ?? ''
  }
}

async function onRestoreConnChange() {
  if (restoreConnId.value) {
    await fetchDatabases(restoreConnId.value)
  }
}

async function createRequest() {
  if (!requestForm.conn_id || !requestForm.workflow_id) return
  createLoading.value = true
  try {
    const { data } = await axios.post<BackupDownloadRequest>('/api/backup-download-requests', {
      title: requestForm.title.trim(),
      description: requestForm.description.trim(),
      conn_id: requestForm.conn_id,
      database: requestForm.database,
      workflow_id: requestForm.workflow_id,
    })
    toast.success(`Backup request #${data.id} created`)
    requestForm.title = ''
    requestForm.description = ''
    await fetchRequests()
    await loadRequest(data.id)
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to create backup request')
  } finally {
    createLoading.value = false
  }
}

async function reviewRequest(action: 'approved' | 'rejected') {
  if (!selectedRequest.value) return
  reviewLoading.value = true
  try {
    const { data } = await axios.post<BackupDownloadRequest>(`/api/backup-download-requests/${selectedRequest.value.id}/review`, {
      action,
      note: reviewNote.value,
    })
    toast.success(action === 'approved' ? 'Request approved' : 'Request rejected')
    reviewNote.value = ''
    await fetchRequests()
    await loadRequest(data.id)
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to review backup request')
  } finally {
    reviewLoading.value = false
  }
}

function downloadApprovedRequest() {
  if (!selectedRequest.value) return
  axios.get(`/api/backup-download-requests/${selectedRequest.value.id}/download`, {
    responseType: 'blob',
  }).then((response) => {
    const filename = `backup_request_${selectedRequest.value?.id ?? 'download'}.sql`
    downloadBlob(response.data, filename)
  }).catch((error: any) => {
    toast.error(error.response?.data?.error || 'Failed to download approved backup')
  })
}

function downloadDirectBackup() {
  if (!directConnId.value) return
  axios.get(`/api/connections/${directConnId.value}/backup`, {
    params: { database: directDatabase.value },
    responseType: 'blob',
  }).then((response) => {
    downloadBlob(response.data, `backup_${directDatabase.value || 'db'}.sql`)
  }).catch((error: any) => {
    toast.error(error.response?.data?.error || 'Failed to download backup')
  })
}

async function uploadFile(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  restoreSQL.value = await file.text()
}

async function runRestore() {
  if (!restoreConnId.value || !restoreSQL.value) return
  restoreLoading.value = true
  restoreError.value = ''
  restoreResult.value = ''
  try {
    const { data } = await axios.post(`/api/connections/${restoreConnId.value}/restore`, { sql: restoreSQL.value })
    restoreResult.value = `Executed ${data.executed} statement(s) successfully.`
  } catch (error: any) {
    restoreError.value = error?.response?.data?.error ?? 'Restore failed'
  } finally {
    restoreLoading.value = false
  }
}

function formatDate(value?: string) {
  if (!value) return '—'
  return new Date(value).toLocaleString()
}

watch(filteredRequests, async (items) => {
  if (!items.length) {
    selectedRequestId.value = null
    selectedRequest.value = null
    return
  }
  if (!selectedRequestId.value || !items.some((item) => item.id === selectedRequestId.value)) {
    await loadRequest(items[0].id)
  }
})

onMounted(async () => {
  await fetchConnections()
  await fetchRequests()
})
</script>

<template>
  <div class="page-shell bv-root">
    <div class="page-scroll bv-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Operations</div>
            <div class="page-title">Backup &amp; Restore</div>
            <div class="page-subtitle">Request database download through approval workflows, or use direct backup and restore when you have elevated backup access.</div>
          </div>
        </section>

        <div class="page-tabs bv-tabs">
          <button class="page-tab bv-tab" :class="{ 'is-active': activeTab === 'request' }" @click="activeTab='request'">Request Download</button>
          <button v-if="canDirectBackup" class="page-tab bv-tab" :class="{ 'is-active': activeTab === 'direct' }" @click="activeTab='direct'">Direct Backup</button>
          <button v-if="canDirectBackup" class="page-tab bv-tab" :class="{ 'is-active': activeTab === 'restore' }" @click="activeTab='restore'">Restore</button>
        </div>

        <section v-if="activeTab === 'request'" class="bv-request-layout">
          <div class="page-card bv-card">
            <div class="page-card__head">
              <div>
                <div class="page-card__title">Create Backup Download Request</div>
                <div class="page-card__sub">Submit a database dump request into the approval workflow instead of downloading directly.</div>
              </div>
            </div>
            <div class="page-card__body bv-card-body">
              <input v-model="requestForm.title" class="base-input" placeholder="Request title" />
              <input v-model="requestForm.description" class="base-input" placeholder="Why do you need this download?" />
              <select class="base-input" v-model.number="requestForm.conn_id" @change="onRequestConnChange">
                <option :value="null">Select connection…</option>
                <option v-for="c in connections" :key="c.id" :value="c.id">{{ c.name }}</option>
              </select>
              <select class="base-input" v-model="requestForm.database" :disabled="!requestForm.conn_id || databasesLoading">
                <option value="">Default database/schema</option>
                <option v-for="db in databases" :key="db" :value="db">{{ db }}</option>
              </select>
              <select class="base-input" v-model.number="requestForm.workflow_id" :disabled="!requestForm.conn_id || workflowsLoading || !applicableWorkflows.length">
                <option :value="null">{{ workflowsLoading ? 'Loading workflows…' : 'Select workflow…' }}</option>
                <option v-for="workflow in applicableWorkflows" :key="workflow.id" :value="workflow.id">{{ workflow.name }}</option>
              </select>
              <div v-if="requestForm.conn_id && !workflowsLoading && !applicableWorkflows.length" class="notice notice--warn">
                No applicable workflow matches this connection.
              </div>
              <button class="base-btn base-btn--primary" :disabled="createLoading || !requestForm.conn_id || !requestForm.workflow_id" @click="createRequest">
                {{ createLoading ? 'Submitting…' : 'Submit Download Request' }}
              </button>
            </div>
          </div>

          <div class="page-card bv-card">
            <div class="page-card__head">
              <div>
                <div class="page-card__title">Backup Requests</div>
                <div class="page-card__sub">Your requests, plus approval work if you can review.</div>
              </div>
              <select class="base-input bv-filter" v-model="filter">
                <option value="all">All statuses</option>
                <option value="pending_review">Pending review</option>
                <option value="approved">Approved</option>
                <option value="rejected">Rejected</option>
                <option value="done">Done</option>
                <option value="failed">Failed</option>
              </select>
            </div>

            <div class="bv-request-grid">
              <div class="bv-request-list">
                <div v-if="requestsLoading" class="bv-empty">Loading requests…</div>
                <div v-else-if="!filteredRequests.length" class="bv-empty">No backup download requests yet.</div>
                <button
                  v-for="item in filteredRequests"
                  :key="item.id"
                  class="bv-request-item"
                  :class="{ 'bv-request-item--active': item.id === selectedRequestId }"
                  @click="loadRequest(item.id)"
                >
                  <div class="bv-request-item__top">
                    <strong>#{{ item.id }}</strong>
                    <span class="bv-status" :data-status="item.status">{{ item.status }}</span>
                  </div>
                  <div class="bv-request-item__title">{{ item.title }}</div>
                  <div class="bv-request-item__meta">{{ item.connection || 'No connection' }} · {{ item.creator_name || 'unknown' }}</div>
                  <div class="bv-request-item__meta">{{ formatDate(item.created_at) }}</div>
                </button>
              </div>

              <div class="bv-request-detail">
                <div v-if="!selectedRequest" class="bv-empty">Select a request to inspect it.</div>
                <div v-else class="bv-detail-card">
                  <div class="bv-detail__head">
                    <div>
                      <div class="bv-detail__title">{{ selectedRequest.title }}</div>
                      <div class="bv-detail__sub">{{ selectedRequest.connection }} · {{ selectedRequest.database_name || 'default database/schema' }}</div>
                    </div>
                    <span class="bv-status" :data-status="selectedRequest.status">{{ selectedRequest.status }}</span>
                  </div>
                  <div class="bv-meta">
                    <span>Requested by {{ selectedRequest.creator_name || 'unknown' }}</span>
                    <span>Reviewer {{ selectedRequest.reviewer_name || '—' }}</span>
                    <span>{{ formatDate(selectedRequest.created_at) }}</span>
                  </div>
                  <div v-if="selectedRequest.description" class="bv-description">{{ selectedRequest.description }}</div>
                  <textarea v-model="reviewNote" class="base-input bv-note" rows="3" placeholder="Review note or rejection reason…" />
                  <div class="bv-actions">
                    <button
                      v-if="canApprove"
                      class="base-btn base-btn--ghost base-btn--sm"
                      :disabled="reviewLoading || selectedRequest.status !== 'pending_review'"
                      @click="reviewRequest('rejected')"
                    >
                      Reject
                    </button>
                    <button
                      v-if="canApprove"
                      class="base-btn base-btn--primary base-btn--sm"
                      :disabled="reviewLoading || selectedRequest.status !== 'pending_review'"
                      @click="reviewRequest('approved')"
                    >
                      Approve
                    </button>
                    <button
                      class="base-btn base-btn--primary base-btn--sm"
                      :disabled="selectedRequest.status !== 'approved' && selectedRequest.status !== 'done'"
                      @click="downloadApprovedRequest"
                    >
                      Download Approved Dump
                    </button>
                  </div>
                  <div v-if="selectedRequest.review_note" class="notice notice--info"><strong>Review note:</strong> {{ selectedRequest.review_note }}</div>
                  <div v-if="selectedRequest.execute_error" class="notice notice--error"><strong>Execution error:</strong> {{ selectedRequest.execute_error }}</div>
                </div>
              </div>
            </div>
          </div>
        </section>

        <div v-if="activeTab === 'direct' && canDirectBackup" class="page-card bv-card">
          <div class="page-card__head">
            <div>
              <div class="page-card__title">Direct SQL Dump</div>
              <div class="page-card__sub">Immediate backup download for users with backup management access.</div>
            </div>
          </div>
          <div class="page-card__body bv-card-body">
            <select class="base-input" v-model.number="directConnId" @change="onDirectConnChange">
              <option :value="null">Select connection…</option>
              <option v-for="c in connections" :key="c.id" :value="c.id">{{ c.name }}</option>
            </select>
            <select class="base-input" v-model="directDatabase" :disabled="!directConnId || databasesLoading">
              <option value="">Default database/schema</option>
              <option v-for="db in databases" :key="db" :value="db">{{ db }}</option>
            </select>
            <button class="base-btn base-btn--primary" :disabled="!directConnId" @click="downloadDirectBackup">Download .sql</button>
          </div>
        </div>

        <div v-if="activeTab === 'restore' && canDirectBackup" class="page-card bv-card">
          <div class="page-card__head">
            <div>
              <div class="page-card__title">Restore From SQL File</div>
              <div class="page-card__sub">High-risk direct restore for elevated backup operators.</div>
            </div>
          </div>
          <div class="page-card__body bv-card-body">
            <select class="base-input" v-model.number="restoreConnId" @change="onRestoreConnChange">
              <option :value="null">Select connection…</option>
              <option v-for="c in connections" :key="c.id" :value="c.id">{{ c.name }}</option>
            </select>
            <div class="bv-drop" @dragover.prevent @drop.prevent="(e) => { const f=e.dataTransfer?.files?.[0]; if(f) f.text().then(t=>restoreSQL=t) }">
              <span>Drop a .sql file here or</span>
              <label class="bv-file-btn">
                Browse
                <input type="file" accept=".sql,.txt" style="display:none" @change="uploadFile" />
              </label>
            </div>
            <div v-if="restoreSQL" class="bv-preview">
              <div class="bv-preview-header">
                <span>{{ restoreSQL.split('\n').length }} lines loaded</span>
                <button class="base-btn base-btn--ghost base-btn--sm" @click="restoreSQL=''">Clear</button>
              </div>
              <pre class="bv-preview-pre">{{ restoreSQL.slice(0, 500) }}{{ restoreSQL.length > 500 ? '\n…' : '' }}</pre>
            </div>
            <div v-if="restoreResult" class="notice notice--ok">{{ restoreResult }}</div>
            <div v-if="restoreError" class="notice notice--error">{{ restoreError }}</div>
            <button class="base-btn base-btn--primary" :disabled="!restoreConnId || !restoreSQL || restoreLoading" @click="runRestore">
              {{ restoreLoading ? 'Restoring…' : 'Run Restore' }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.bv-request-layout {
  display: grid;
  grid-template-columns: 360px minmax(0, 1fr);
  gap: 16px;
}

.bv-card {
  padding: 20px;
}

.bv-card-body {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.bv-filter {
  max-width: 180px;
}

.bv-request-grid {
  display: grid;
  grid-template-columns: 280px minmax(0, 1fr);
  gap: 14px;
}

.bv-request-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.bv-request-item {
  width: 100%;
  text-align: left;
  border: 1px solid var(--border);
  border-radius: 14px;
  background: var(--bg-elevated);
  padding: 12px 14px;
  cursor: pointer;
}

.bv-request-item--active {
  border-color: var(--brand);
  box-shadow: inset 0 0 0 1px var(--brand);
}

.bv-request-item__top,
.bv-detail__head,
.bv-meta,
.bv-actions {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}

.bv-request-item__title,
.bv-detail__title {
  font-size: 13px;
  font-weight: 700;
  color: var(--text-primary);
}

.bv-request-item__meta,
.bv-detail__sub,
.bv-meta {
  font-size: 11px;
  color: var(--text-muted);
}

.bv-detail-card {
  border: 1px solid var(--border);
  border-radius: 16px;
  background: var(--bg-elevated);
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.bv-description {
  font-size: 12px;
  color: var(--text-secondary);
  line-height: 1.6;
}

.bv-note {
  width: 100%;
  min-height: 84px;
}

.bv-status {
  display: inline-flex;
  align-items: center;
  padding: 3px 8px;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 700;
  text-transform: capitalize;
  background: var(--bg-surface);
  color: var(--text-secondary);
}

.bv-status[data-status="approved"],
.bv-status[data-status="done"] {
  background: rgba(34, 197, 94, 0.15);
  color: #16a34a;
}

.bv-status[data-status="pending_review"],
.bv-status[data-status="executing"] {
  background: rgba(245, 158, 11, 0.16);
  color: #d97706;
}

.bv-status[data-status="rejected"],
.bv-status[data-status="failed"] {
  background: rgba(239, 68, 68, 0.14);
  color: #dc2626;
}

.bv-empty {
  padding: 18px;
  border: 1px dashed var(--border);
  border-radius: 14px;
  color: var(--text-muted);
  font-size: 12px;
  text-align: center;
}

.bv-drop {
  border: 2px dashed var(--border);
  border-radius: 8px;
  padding: 28px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  font-size: 13px;
  color: var(--text-muted);
  cursor: pointer;
  transition: border-color 0.15s;
}

.bv-drop:hover {
  border-color: var(--brand);
}

.bv-file-btn {
  padding: 4px 12px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 6px;
  font-size: 12px;
  color: var(--text-secondary);
  cursor: pointer;
}

.bv-preview {
  background: var(--bg-body);
  border: 1px solid var(--border);
  border-radius: 6px;
  overflow: hidden;
}

.bv-preview-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border);
  font-size: 12px;
  color: var(--text-muted);
}

.bv-preview-pre {
  margin: 0;
  padding: 10px 12px;
  font-family: var(--mono, monospace);
  font-size: 11.5px;
  line-height: 1.5;
  color: var(--text-primary);
  white-space: pre-wrap;
  word-break: break-all;
}

.notice {
  border-radius: 10px;
  padding: 10px 14px;
  font-size: 12.5px;
}

.notice--ok {
  background: rgba(74, 222, 128, 0.1);
  border: 1px solid rgba(74, 222, 128, 0.3);
  color: #16a34a;
}

.notice--error {
  background: rgba(239, 68, 68, 0.1);
  border: 1px solid rgba(239, 68, 68, 0.25);
  color: #dc2626;
}

.notice--warn,
.notice--info {
  background: rgba(245, 158, 11, 0.12);
  border: 1px solid rgba(245, 158, 11, 0.22);
  color: #b45309;
}

@media (max-width: 1100px) {
  .bv-request-layout,
  .bv-request-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 720px) {
  .bv-card {
    padding: 16px;
  }
}
</style>
