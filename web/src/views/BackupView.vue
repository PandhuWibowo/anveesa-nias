<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import axios from 'axios'
import { useConnections, type DbDriver } from '@/composables/useConnections'
import { useDatabases } from '@/composables/useDatabases'
import { useToast } from '@/composables/useToast'
import { useAuth } from '@/composables/useAuth'
import { downloadBlob } from '@/utils/export'
import DriverIcon from '@/components/ui/DriverIcon.vue'

// ── Types ─────────────────────────────────────────────────────────────
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

interface BucketObject {
  key: string
  size: number
  last_modified: string
}

// ── Auth / global ─────────────────────────────────────────────────────
const toast = useToast()
const { connections, fetchConnections } = useConnections()
const { databases, fetchDatabases, loading: databasesLoading } = useDatabases()
const { hasPermission, isAdmin } = useAuth()

const canDirectBackup = computed(() => isAdmin.value || hasPermission('backups.manage'))
const canApprove = computed(() => isAdmin.value || hasPermission('query.approve'))

// ── Tab ───────────────────────────────────────────────────────────────
const activeTab = ref<'bucket' | 'request' | 'direct' | 'restore'>('bucket')

// ── Derived connection lists ──────────────────────────────────────────
const S3_DRIVERS: DbDriver[] = ['s3_aws', 's3_gcp', 's3_oss', 's3_obs']
const DB_DRIVERS: DbDriver[] = ['postgres', 'mysql', 'mariadb', 'mssql', 'sqlite']

const dbConnections = computed(() => connections.value.filter(c => DB_DRIVERS.includes(c.driver)))
const bucketConnections = computed(() => connections.value.filter(c => S3_DRIVERS.includes(c.driver)))

function driverLabel(driver: DbDriver): string {
  const map: Record<string, string> = {
    postgres: 'PostgreSQL', mysql: 'MySQL', mariadb: 'MariaDB', mssql: 'SQL Server',
    sqlite: 'SQLite', s3_aws: 'AWS S3', s3_gcp: 'GCP Storage', s3_oss: 'Alibaba OSS', s3_obs: 'Huawei OBS',
  }
  return map[driver] ?? driver
}

// ── Shared backup options (pgAdmin-style) ────────────────────────────
const backupOpts = reactive({
  sections:          'all'  as 'all' | 'pre-data' | 'data' | 'post-data',
  compress:          true,
  drop_existing:     false,
  if_not_exists:     false,
  column_insert:     true,
  use_transaction:   false,
  disable_fk_checks: true,
  include_indexes:   true,
  include_fks:       true,
  include_views:     false,
  include_sequences: false,
  include_triggers:  false,
  schema:            '',
  include_tables:    '',   // comma-separated; split before sending
  exclude_tables:    '',
})

const showAdvanced = ref(false)

function toBackupOptionsPayload() {
  return {
    sections:          backupOpts.sections,
    compress:          backupOpts.compress,
    drop_existing:     backupOpts.drop_existing,
    if_not_exists:     backupOpts.if_not_exists,
    column_insert:     backupOpts.column_insert,
    use_transaction:   backupOpts.use_transaction,
    disable_fk_checks: backupOpts.disable_fk_checks,
    include_indexes:   backupOpts.include_indexes,
    include_fks:       backupOpts.include_fks,
    include_views:     backupOpts.include_views,
    include_sequences: backupOpts.include_sequences,
    include_triggers:  backupOpts.include_triggers,
    schema:            backupOpts.schema.trim(),
    include_tables:    backupOpts.include_tables.split(',').map(s => s.trim()).filter(Boolean),
    exclude_tables:    backupOpts.exclude_tables.split(',').map(s => s.trim()).filter(Boolean),
  }
}

// ── Backup to Bucket ──────────────────────────────────────────────────
const bucketForm = reactive({
  source_conn_id: null as number | null,
  database: '',
  dest_conn_id: null as number | null,
  prefix: '',
  subfolder: '',
})

const bucketRunning = ref(false)
const bucketCancelled = ref(false)
const bucketAbortController = ref<AbortController | null>(null)
const bucketResult = ref<{ ok: boolean; object_key: string; bucket: string; size_bytes: number; uncompressed_bytes?: number; uploaded_at: string } | null>(null)
const bucketError = ref('')
const bucketErrorDetail = ref<{ message: string; stage: string; status: number | null; time: string; hint: string } | null>(null)
const bucketProgress = ref(0)
const bucketStage = ref('')
const bucketLogOpen = ref(false)
const bucketStageIdx = ref(-1)
const bucketStageTimes = ref<string[]>([])
const bucketHistory = ref<BucketObject[]>([])
const historyLoading = ref(false)

function cancelBucketBackup() {
  bucketAbortController.value?.abort()
  bucketCancelled.value = true
}

const BACKUP_STAGES = [
  { at: 5,  label: 'Connecting to source database' },
  { at: 20, label: 'Generating SQL dump' },
  { at: 55, label: 'Processing tables' },
  { at: 75, label: 'Compressing dump' },
  { at: 90, label: 'Uploading to bucket' },
]

function nowTime() {
  return new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

function stageFromError(msg: string): string {
  if (msg.includes('source connection') || msg.includes('connection error')) return 'Database connection'
  if (msg.includes('backup generation') || msg.includes('dump')) return 'SQL dump generation'
  if (msg.includes('compress')) return 'Compression'
  if (msg.includes('upload')) return 'Upload to bucket'
  if (msg.includes('permission')) return 'Authorization'
  if (msg.includes('bucket connection') || msg.includes('secret key') || msg.includes('object storage')) return 'Bucket configuration'
  return 'Backup operation'
}

function hintFromError(msg: string): string {
  if (msg.includes('permission denied')) return 'Check that your account has the required permissions on this connection.'
  if (msg.includes('source connection') || msg.includes('connection error')) return 'Verify the source connection credentials and network access.'
  if (msg.includes('secret key') || msg.includes('decrypt')) return 'The bucket connection credentials may be corrupted — re-enter them.'
  if (msg.includes('object storage')) return 'The destination connection must be an S3-compatible storage provider.'
  if (msg.includes('upload')) return 'Check bucket name, access key, and secret key. Ensure the bucket exists.'
  if (msg.includes('compress')) return 'Unexpected error during gzip compression. Try disabling compression.'
  return 'Check the server logs for more detail.'
}

function copyErrorToClipboard() {
  if (!bucketErrorDetail.value) return
  const d = bucketErrorDetail.value
  const text = `Backup Error\nStage: ${d.stage}\nStatus: ${d.status ?? 'N/A'}\nTime: ${d.time}\nMessage: ${d.message}`
  navigator.clipboard.writeText(text)
}

async function onSourceConnChange() {
  bucketForm.database = ''
  if (bucketForm.source_conn_id) {
    await fetchDatabases(bucketForm.source_conn_id)
    bucketForm.database = databases.value[0] ?? ''
  }
}

async function runBucketBackup() {
  if (!bucketForm.source_conn_id || !bucketForm.dest_conn_id) {
    toast.error('Select a source database and a destination bucket')
    return
  }
  bucketRunning.value = true
  bucketCancelled.value = false
  bucketResult.value = null
  bucketError.value = ''
  bucketErrorDetail.value = null
  bucketProgress.value = 0
  bucketStageIdx.value = 0
  bucketStageTimes.value = [nowTime()]
  bucketStage.value = BACKUP_STAGES[0].label + '…'

  const controller = new AbortController()
  bucketAbortController.value = controller

  // Advance through simulated stages while the request is in flight
  let stageIdx = 0
  const progressTimer = setInterval(() => {
    if (stageIdx < BACKUP_STAGES.length - 1) {
      stageIdx++
      bucketProgress.value = BACKUP_STAGES[stageIdx].at
      bucketStage.value = BACKUP_STAGES[stageIdx].label + '…'
      bucketStageIdx.value = stageIdx
      bucketStageTimes.value[stageIdx] = nowTime()
    }
  }, 1800)

  try {
    const { data } = await axios.post('/api/backup/to-bucket', {
      source_conn_id: bucketForm.source_conn_id,
      database: bucketForm.database,
      dest_conn_id: bucketForm.dest_conn_id,
      prefix: bucketForm.prefix || 'backup',
      subfolder: bucketForm.subfolder,
      options: toBackupOptionsPayload(),
    }, { signal: controller.signal })

    clearInterval(progressTimer)
    bucketProgress.value = 100
    bucketStageIdx.value = BACKUP_STAGES.length
    bucketStage.value = 'Upload complete!'
    bucketResult.value = data
    toast.success(`Backup uploaded → ${data.object_key}`)
    loadBucketHistory()
  } catch (err: any) {
    clearInterval(progressTimer)

    // Cancelled by user — reset silently
    if (axios.isCancel(err) || err?.name === 'CanceledError' || bucketCancelled.value) {
      bucketProgress.value = 0
      bucketStage.value = ''
      bucketStageIdx.value = -1
      toast.info('Backup cancelled')
      return
    }

    const message = err?.response?.data?.error ?? 'Backup to bucket failed'
    bucketError.value = message
    bucketErrorDetail.value = {
      message,
      stage: stageFromError(message),
      status: err?.response?.status ?? null,
      time: new Date().toLocaleString(),
      hint: hintFromError(message),
    }
    bucketProgress.value = 0
    bucketStage.value = ''
  } finally {
    bucketRunning.value = false
    bucketAbortController.value = null
  }
}

async function loadBucketHistory() {
  if (!bucketForm.dest_conn_id) return
  historyLoading.value = true
  try {
    const { data } = await axios.get('/api/backup/bucket-list', {
      params: {
        dest_conn_id: bucketForm.dest_conn_id,
        subfolder: bucketForm.subfolder || '',
      },
    })
    bucketHistory.value = (data.objects ?? []).filter((o: BucketObject) => o.key.endsWith('.sql') || o.key.endsWith('.sql.gz'))
      .sort((a: BucketObject, b: BucketObject) => b.last_modified.localeCompare(a.last_modified))
  } catch {
    bucketHistory.value = []
  } finally {
    historyLoading.value = false
  }
}

watch(() => bucketForm.dest_conn_id, () => {
  bucketHistory.value = []
  if (bucketForm.dest_conn_id) loadBucketHistory()
})

function formatBytes(b: number): string {
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`
  return `${(b / 1024 / 1024).toFixed(2)} MB`
}

function formatDate(value?: string) {
  if (!value) return '—'
  return new Date(value).toLocaleString()
}

function formatStatus(status: string): string {
  return status.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase())
}

function copyKey(key: string) {
  navigator.clipboard.writeText(key).then(() => toast.success('Key copied'))
}

// ── Request Download (existing) ───────────────────────────────────────
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

const filteredRequests = computed(() =>
  filter.value === 'all' ? requests.value : requests.value.filter(i => i.status === filter.value),
)

async function fetchRequests() {
  requestsLoading.value = true
  try {
    const { data } = await axios.get<BackupDownloadRequest[]>('/api/backup-download-requests')
    requests.value = Array.isArray(data) ? data : []
    if (!selectedRequestId.value || !requests.value.some(i => i.id === selectedRequestId.value)) {
      selectedRequestId.value = filteredRequests.value[0]?.id ?? requests.value[0]?.id ?? null
    }
    if (selectedRequestId.value) await loadRequest(selectedRequestId.value)
    else selectedRequest.value = null
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
    toast.error(error.response?.data?.error || 'Failed to load request')
  }
}

async function loadApplicableWorkflows(connId: number | null) {
  applicableWorkflows.value = []
  requestForm.workflow_id = null
  if (!connId) return
  workflowsLoading.value = true
  try {
    const { data } = await axios.get<WorkflowOption[]>('/api/workflows/applicable', { params: { conn_id: connId } })
    applicableWorkflows.value = Array.isArray(data) ? data : []
    requestForm.workflow_id = applicableWorkflows.value[0]?.id ?? null
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to load workflows')
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
    toast.error(error.response?.data?.error || 'Failed to create request')
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
    toast.error(error.response?.data?.error || 'Failed to review request')
  } finally {
    reviewLoading.value = false
  }
}

function downloadApprovedRequest() {
  if (!selectedRequest.value) return
  axios.get(`/api/backup-download-requests/${selectedRequest.value.id}/download`, { responseType: 'blob' })
    .then(response => downloadBlob(response.data, `backup_request_${selectedRequest.value?.id ?? 'download'}.sql`))
    .catch((error: any) => toast.error(error.response?.data?.error || 'Failed to download'))
}

// ── Direct Backup (existing) ──────────────────────────────────────────
const directConnId = ref<number | null>(null)
const directDatabase = ref('')

async function onDirectConnChange() {
  directDatabase.value = ''
  if (directConnId.value) {
    await fetchDatabases(directConnId.value)
    directDatabase.value = databases.value[0] ?? ''
  }
}

function downloadDirectBackup() {
  if (!directConnId.value) return
  const opts = toBackupOptionsPayload()
  axios.get(`/api/connections/${directConnId.value}/backup`, {
    params: {
      database:          directDatabase.value,
      sections:          opts.sections,
      compress:          opts.compress ? '1' : '0',
      drop_existing:     opts.drop_existing ? '1' : '0',
      if_not_exists:     opts.if_not_exists ? '1' : '0',
      column_insert:     opts.column_insert ? '1' : '0',
      use_transaction:   opts.use_transaction ? '1' : '0',
      disable_fk_checks: opts.disable_fk_checks ? '1' : '0',
      include_indexes:   opts.include_indexes ? '1' : '0',
      include_fks:       opts.include_fks ? '1' : '0',
      include_views:     opts.include_views ? '1' : '0',
      include_sequences: opts.include_sequences ? '1' : '0',
      include_triggers:  opts.include_triggers ? '1' : '0',
      schema:            opts.schema,
      include_tables:    (opts.include_tables as string[]).join(','),
      exclude_tables:    (opts.exclude_tables as string[]).join(','),
    },
    responseType: 'blob',
  }).then(response => downloadBlob(response.data, `backup_${directDatabase.value || 'db'}${backupOpts.compress ? '.sql.gz' : '.sql'}`))
    .catch((error: any) => toast.error(error.response?.data?.error || 'Failed to download backup'))
}

// ── Restore (existing) ────────────────────────────────────────────────
const restoreConnId = ref<number | null>(null)
const restoreSQL = ref('')
const restoreResult = ref('')
const restoreLoading = ref(false)
const restoreError = ref('')

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

watch(filteredRequests, async (items) => {
  if (!items.length) { selectedRequestId.value = null; selectedRequest.value = null; return }
  if (!selectedRequestId.value || !items.some(i => i.id === selectedRequestId.value)) {
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
    <div class="page-scroll">
      <div class="page-stack">

        <!-- Hero -->
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Operations</div>
            <div class="page-title">Backup &amp; Restore</div>
            <div class="page-subtitle">Dump your databases to a bucket with custom options, request downloads through approval workflows, or restore from a SQL file.</div>
          </div>
        </section>

        <!-- Tabs -->
        <div class="page-tabs bv-tabs">
          <button class="page-tab" :class="{ 'is-active': activeTab === 'bucket' }" @click="activeTab = 'bucket'">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
            Backup to Bucket
          </button>
          <button class="page-tab" :class="{ 'is-active': activeTab === 'request' }" @click="activeTab = 'request'">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
            Request Download
          </button>
          <button v-if="canDirectBackup" class="page-tab" :class="{ 'is-active': activeTab === 'direct' }" @click="activeTab = 'direct'">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
            Direct Download
          </button>
          <button v-if="canDirectBackup" class="page-tab" :class="{ 'is-active': activeTab === 'restore' }" @click="activeTab = 'restore'">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="1 4 1 10 7 10"/><path d="M3.51 15a9 9 0 1 0 .49-3.14"/></svg>
            Restore
          </button>
        </div>

        <!-- ══ BACKUP TO BUCKET ══════════════════════════════════════════ -->
        <div v-if="activeTab === 'bucket'" class="bv-bucket-layout">

          <!-- Config card -->
          <div class="page-card bv-card">
            <div class="page-card__head">
              <div>
                <div class="page-card__title">Configure Backup</div>
                <div class="page-card__sub">Choose the source database, destination bucket, and how the backup should be named.</div>
              </div>
            </div>
            <div class="page-card__body bv-card-body">

              <!-- Source -->
              <div class="bv-section-label">
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4.03 3-9 3S3 13.66 3 12"/><path d="M3 5v14c0 1.66 4.03 3 9 3s9-1.34 9-3V5"/></svg>
                Source Database
              </div>

              <div v-if="dbConnections.length === 0" class="bv-empty-hint">
                No database connections found. <a href="/connections" style="color:var(--brand)">Add one →</a>
              </div>
              <template v-else>
                <div class="bv-conn-grid">
                  <button
                    v-for="c in dbConnections"
                    :key="c.id"
                    class="bv-conn-card"
                    :class="{ 'is-active': bucketForm.source_conn_id === c.id }"
                    @click="bucketForm.source_conn_id = c.id; onSourceConnChange()"
                  >
                    <div class="bv-conn-card__badge" :class="`conn-badge--${c.driver}`">
                      <DriverIcon :driver="c.driver" :size="14" />
                    </div>
                    <div class="bv-conn-card__info">
                      <span class="bv-conn-card__name">{{ c.name }}</span>
                      <span class="bv-conn-card__driver">{{ driverLabel(c.driver) }}</span>
                    </div>
                    <svg v-if="bucketForm.source_conn_id === c.id" class="bv-conn-card__check" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                  </button>
                </div>

                <div v-if="bucketForm.source_conn_id" class="form-group">
                  <label class="form-label">Database / Schema</label>
                  <select class="base-input" v-model="bucketForm.database" :disabled="databasesLoading">
                    <option value="">Default (connection default)</option>
                    <option v-for="db in databases" :key="db" :value="db">{{ db }}</option>
                  </select>
                </div>
              </template>

              <!-- Destination -->
              <div class="bv-section-label" style="margin-top:4px">
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
                Destination Bucket
              </div>

              <div v-if="bucketConnections.length === 0" class="bv-empty-hint">
                No object storage connection found. Go to <a href="/connections" style="color:var(--brand)">Connections → Add connection</a> and select your provider (Huawei OBS, AWS S3, GCP Storage, or Alibaba OSS), then enter your endpoint, access key, secret key, and bucket name.
              </div>
              <div v-else class="bv-conn-grid">
                <button
                  v-for="c in bucketConnections"
                  :key="c.id"
                  class="bv-conn-card"
                  :class="{ 'is-active': bucketForm.dest_conn_id === c.id }"
                  @click="bucketForm.dest_conn_id = c.id"
                >
                  <div class="bv-conn-card__badge" :class="`conn-badge--${c.driver}`">
                    <DriverIcon :driver="c.driver" :size="14" />
                  </div>
                  <div class="bv-conn-card__info">
                    <span class="bv-conn-card__name">{{ c.name }}</span>
                    <span class="bv-conn-card__driver">{{ c.database || driverLabel(c.driver) }}</span>
                  </div>
                  <svg v-if="bucketForm.dest_conn_id === c.id" class="bv-conn-card__check" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                </button>
              </div>

              <!-- File info -->
              <div class="bv-section-label" style="margin-top:4px">
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
                File Settings
              </div>

              <div class="form-row">
                <div class="form-group">
                  <label class="form-label">
                    Filename Prefix
                    <span class="bv-hint-inline">e.g. "myapp" → myapp_dbname_20250101.sql</span>
                  </label>
                  <input v-model="bucketForm.prefix" class="base-input" placeholder="backup" />
                </div>
                <div class="form-group">
                  <label class="form-label">
                    Subfolder in Bucket
                    <span class="bv-hint-inline">optional path prefix</span>
                  </label>
                  <input v-model="bucketForm.subfolder" class="base-input" placeholder="backups/production" />
                </div>
              </div>

              <!-- pgAdmin-style Backup Options panel -->
              <div class="bv-opts-header" @click="showAdvanced = !showAdvanced">
                <div class="bv-section-label" style="border:none;padding:0;margin:0">
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.07 4.93A10 10 0 0 0 5 5.07M4.93 19.07A10 10 0 0 0 19 18.93"/></svg>
                  Backup Options
                </div>
                <div class="bv-opts-header__right">
                  <span class="bv-opts-summary">{{ backupOpts.sections === 'all' ? 'Full dump' : backupOpts.sections }}</span>
                  <svg :style="{ transform: showAdvanced ? 'rotate(180deg)' : 'none', transition: 'transform 0.2s' }" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"/></svg>
                </div>
              </div>

              <div v-if="showAdvanced" class="bv-opts-panel">

                <!-- Output -->
                <div class="bv-opts-group">
                  <div class="bv-opts-group__label">
                    <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
                    Output
                  </div>
                  <div class="bv-toggle-list">
                    <label class="bv-toggle-row">
                      <div>
                        <span class="bv-toggle-row__name">Compress output (gzip)</span>
                        <span class="bv-toggle-row__desc">Upload as .sql.gz — smaller file, faster transfer, compatible with your existing backups</span>
                      </div>
                      <div class="bv-switch" :class="{ 'is-on': backupOpts.compress }" @click="backupOpts.compress = !backupOpts.compress"><div class="bv-switch__knob"/></div>
                    </label>
                  </div>
                </div>

                <!-- Sections -->
                <div class="bv-opts-group">
                  <div class="bv-opts-group__label">
                    <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><line x1="3" y1="9" x2="21" y2="9"/><line x1="3" y1="15" x2="21" y2="15"/></svg>
                    Sections
                  </div>
                  <div class="bv-sections-grid">
                    <label v-for="s in [
                      { v: 'all',       label: 'All',        sub: 'Schema + data + constraints' },
                      { v: 'pre-data',  label: 'Pre-data',   sub: 'CREATE TABLE, sequences' },
                      { v: 'data',      label: 'Data',       sub: 'INSERT statements only' },
                      { v: 'post-data', label: 'Post-data',  sub: 'Indexes, FK constraints' },
                    ]" :key="s.v" class="bv-section-card" :class="{ 'is-active': backupOpts.sections === s.v }">
                      <input type="radio" v-model="backupOpts.sections" :value="s.v" style="display:none" />
                      <span class="bv-section-card__label">{{ s.label }}</span>
                      <span class="bv-section-card__sub">{{ s.sub }}</span>
                    </label>
                  </div>
                </div>

                <!-- DDL options -->
                <div class="bv-opts-group" v-if="backupOpts.sections !== 'data'">
                  <div class="bv-opts-group__label">
                    <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
                    DDL Options
                  </div>
                  <div class="bv-toggle-list">
                    <label class="bv-toggle-row">
                      <div>
                        <span class="bv-toggle-row__name">DROP before CREATE</span>
                        <span class="bv-toggle-row__desc">Emit DROP TABLE IF EXISTS before each CREATE TABLE</span>
                      </div>
                      <div class="bv-switch" :class="{ 'is-on': backupOpts.drop_existing }" @click="backupOpts.drop_existing = !backupOpts.drop_existing"><div class="bv-switch__knob"/></div>
                    </label>
                    <label class="bv-toggle-row">
                      <div>
                        <span class="bv-toggle-row__name">IF NOT EXISTS</span>
                        <span class="bv-toggle-row__desc">Use CREATE TABLE IF NOT EXISTS syntax</span>
                      </div>
                      <div class="bv-switch" :class="{ 'is-on': backupOpts.if_not_exists }" @click="backupOpts.if_not_exists = !backupOpts.if_not_exists"><div class="bv-switch__knob"/></div>
                    </label>
                  </div>
                </div>

                <!-- Data options -->
                <div class="bv-opts-group" v-if="backupOpts.sections !== 'pre-data' && backupOpts.sections !== 'post-data'">
                  <div class="bv-opts-group__label">
                    <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/><line x1="8" y1="18" x2="21" y2="18"/><line x1="3" y1="6" x2="3.01" y2="6"/><line x1="3" y1="12" x2="3.01" y2="12"/><line x1="3" y1="18" x2="3.01" y2="18"/></svg>
                    Data Options
                  </div>
                  <div class="bv-toggle-list">
                    <label class="bv-toggle-row">
                      <div>
                        <span class="bv-toggle-row__name">Column-based INSERT</span>
                        <span class="bv-toggle-row__desc">INSERT INTO t (col1, col2) VALUES … instead of INSERT INTO t VALUES …</span>
                      </div>
                      <div class="bv-switch" :class="{ 'is-on': backupOpts.column_insert }" @click="backupOpts.column_insert = !backupOpts.column_insert"><div class="bv-switch__knob"/></div>
                    </label>
                    <label class="bv-toggle-row">
                      <div>
                        <span class="bv-toggle-row__name">Disable FK checks</span>
                        <span class="bv-toggle-row__desc">Wrap dump in SET FOREIGN_KEY_CHECKS=0 … =1 for safe restore</span>
                      </div>
                      <div class="bv-switch" :class="{ 'is-on': backupOpts.disable_fk_checks }" @click="backupOpts.disable_fk_checks = !backupOpts.disable_fk_checks"><div class="bv-switch__knob"/></div>
                    </label>
                    <label class="bv-toggle-row">
                      <div>
                        <span class="bv-toggle-row__name">Transaction per table</span>
                        <span class="bv-toggle-row__desc">Wrap each table's INSERTs in BEGIN / COMMIT</span>
                      </div>
                      <div class="bv-switch" :class="{ 'is-on': backupOpts.use_transaction }" @click="backupOpts.use_transaction = !backupOpts.use_transaction"><div class="bv-switch__knob"/></div>
                    </label>
                  </div>
                </div>

                <!-- Include objects -->
                <div class="bv-opts-group" v-if="backupOpts.sections !== 'data'">
                  <div class="bv-opts-group__label">
                    <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 11 12 14 22 4"/><path d="M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11"/></svg>
                    Include Objects
                  </div>
                  <div class="bv-check-grid">
                    <label class="bv-check-pill" :class="{ 'is-on': backupOpts.include_indexes }" @click="backupOpts.include_indexes = !backupOpts.include_indexes">
                      <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                      Indexes
                    </label>
                    <label class="bv-check-pill" :class="{ 'is-on': backupOpts.include_fks }" @click="backupOpts.include_fks = !backupOpts.include_fks">
                      <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                      Foreign Keys
                    </label>
                    <label class="bv-check-pill" :class="{ 'is-on': backupOpts.include_views }" @click="backupOpts.include_views = !backupOpts.include_views">
                      <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                      Views
                    </label>
                    <label class="bv-check-pill" :class="{ 'is-on': backupOpts.include_sequences }" @click="backupOpts.include_sequences = !backupOpts.include_sequences">
                      <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                      Sequences <span class="bv-badge-sm">PG</span>
                    </label>
                    <label class="bv-check-pill" :class="{ 'is-on': backupOpts.include_triggers }" @click="backupOpts.include_triggers = !backupOpts.include_triggers">
                      <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                      Triggers
                    </label>
                  </div>
                </div>

                <!-- Table filter -->
                <div class="bv-opts-group">
                  <div class="bv-opts-group__label">
                    <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="22 3 2 3 10 12.46 10 19 14 21 14 12.46 22 3"/></svg>
                    Table Filter
                  </div>
                  <div class="form-row">
                    <div class="form-group">
                      <label class="form-label">Include only tables <span class="bv-hint-inline">comma-separated, empty = all</span></label>
                      <input v-model="backupOpts.include_tables" class="base-input base-input--sm" placeholder="users, orders, products" />
                    </div>
                    <div class="form-group">
                      <label class="form-label">Exclude tables</label>
                      <input v-model="backupOpts.exclude_tables" class="base-input base-input--sm" placeholder="logs, temp_cache" />
                    </div>
                  </div>
                  <div class="form-group" style="margin-top:0">
                    <label class="form-label">Schema name <span class="bv-hint-inline">default: public (PG) / dbo (MSSQL)</span></label>
                    <input v-model="backupOpts.schema" class="base-input base-input--sm" placeholder="public" style="max-width:220px" />
                  </div>
                </div>

              </div>

              <!-- Preview filename -->
              <div v-if="bucketForm.source_conn_id && bucketForm.dest_conn_id" class="bv-preview-name">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
                <span>
                  Will upload to:
                  <strong>{{ bucketConnections.find(c => c.id === bucketForm.dest_conn_id)?.database }}/{{ bucketForm.subfolder ? bucketForm.subfolder + '/' : '' }}{{ bucketForm.prefix || 'backup' }}_{{ bucketForm.database || 'db' }}_<em>YYYYMMDD_HHmmss</em>{{ backupOpts.compress ? '.sql.gz' : '.sql' }}</strong>
                </span>
              </div>

              <!-- Action -->
              <div class="bv-run-row">
                <button
                  class="base-btn base-btn--primary"
                  :disabled="bucketRunning || !bucketForm.source_conn_id || !bucketForm.dest_conn_id"
                  @click="runBucketBackup"
                >
                  <svg v-if="bucketRunning" class="spin" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
                  <svg v-else width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
                  {{ bucketRunning ? 'Running backup…' : 'Run Backup Now' }}
                </button>

                <!-- Cancel — only shown while backup is running -->
                <button
                  v-if="bucketRunning"
                  class="base-btn bv-cancel-btn"
                  @click="cancelBucketBackup"
                >
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                  Cancel
                </button>

                <!-- Refresh history — hidden while backup is running -->
                <button
                  v-if="bucketForm.dest_conn_id && !bucketRunning"
                  class="base-btn base-btn--ghost"
                  :disabled="historyLoading"
                  @click="loadBucketHistory"
                >
                  <svg v-if="historyLoading" class="spin" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
                  <svg v-else width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="1 4 1 10 7 10"/><path d="M3.51 15a9 9 0 1 0 .49-3.14"/></svg>
                  Refresh history
                </button>
              </div>

              <!-- Progress bar (shown while running) -->
              <div v-if="bucketRunning" class="bv-progress-wrap">
                <div class="bv-progress-bar">
                  <div class="bv-progress-bar__fill" :style="{ width: bucketProgress + '%' }"></div>
                </div>
                <div class="bv-progress-meta">
                  <span class="bv-progress-stage">
                    <svg class="spin" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
                    {{ bucketStage }}
                  </span>
                  <div class="bv-progress-right">
                    <span class="bv-progress-pct">{{ bucketProgress }}%</span>
                    <button class="bv-log-toggle" @click="bucketLogOpen = !bucketLogOpen">
                      <svg :style="{ transform: bucketLogOpen ? 'rotate(180deg)' : 'rotate(0deg)', transition: 'transform 0.2s' }" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"/></svg>
                      {{ bucketLogOpen ? 'Hide details' : 'Show details' }}
                    </button>
                  </div>
                </div>

                <!-- Collapsible step log -->
                <div v-if="bucketLogOpen" class="bv-log">
                  <div
                    v-for="(stage, i) in BACKUP_STAGES"
                    :key="i"
                    class="bv-log-row"
                    :class="{
                      'bv-log-row--done':    i < bucketStageIdx,
                      'bv-log-row--active':  i === bucketStageIdx,
                      'bv-log-row--pending': i > bucketStageIdx,
                    }"
                  >
                    <!-- Status icon -->
                    <span class="bv-log-icon">
                      <!-- done -->
                      <svg v-if="i < bucketStageIdx" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                      <!-- active -->
                      <svg v-else-if="i === bucketStageIdx" class="spin" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
                      <!-- pending -->
                      <svg v-else width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/></svg>
                    </span>
                    <span class="bv-log-label">{{ stage.label }}</span>
                    <span class="bv-log-time">{{ bucketStageTimes[i] ?? '' }}</span>
                  </div>
                </div>
              </div>

              <!-- Result card -->
              <div v-if="bucketResult" class="bv-result-card">
                <div class="bv-result-card__head">
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                  Backup uploaded successfully
                </div>
                <div class="bv-result-card__rows">
                  <div class="bv-result-row">
                    <span class="bv-result-row__label">Object key</span>
                    <code class="bv-result-row__val">{{ bucketResult.object_key }}</code>
                  </div>
                  <div class="bv-result-row">
                    <span class="bv-result-row__label">Bucket</span>
                    <code class="bv-result-row__val">{{ bucketResult.bucket }}</code>
                  </div>
                  <div class="bv-result-row">
                    <span class="bv-result-row__label">Size</span>
                    <span class="bv-result-row__val bv-result-size">
                      {{ formatBytes(bucketResult.size_bytes) }}
                      <template v-if="bucketResult.uncompressed_bytes && bucketResult.uncompressed_bytes > bucketResult.size_bytes">
                        <span class="bv-result-size__orig">from {{ formatBytes(bucketResult.uncompressed_bytes) }}</span>
                        <span class="bv-result-size__ratio">
                          {{ Math.round((1 - bucketResult.size_bytes / bucketResult.uncompressed_bytes) * 100) }}% smaller
                        </span>
                      </template>
                    </span>
                  </div>
                  <div class="bv-result-row">
                    <span class="bv-result-row__label">Uploaded at</span>
                    <span class="bv-result-row__val">{{ formatDate(bucketResult.uploaded_at) }}</span>
                  </div>
                </div>
              </div>

              <!-- Detailed error card -->
              <div v-if="bucketErrorDetail" class="bv-error-card">
                <div class="bv-error-card__head">
                  <div class="bv-error-card__head-left">
                    <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
                    Backup failed
                    <span v-if="bucketErrorDetail.status" class="bv-error-badge">HTTP {{ bucketErrorDetail.status }}</span>
                  </div>
                  <button class="bv-error-copy" title="Copy error details" @click="copyErrorToClipboard">
                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                    Copy
                  </button>
                </div>
                <div class="bv-error-card__rows">
                  <div class="bv-error-row bv-error-row--message">
                    <span class="bv-error-row__label">Error</span>
                    <span class="bv-error-row__val">{{ bucketErrorDetail.message }}</span>
                  </div>
                  <div class="bv-error-row">
                    <span class="bv-error-row__label">Failed at</span>
                    <span class="bv-error-row__val">{{ bucketErrorDetail.stage }}</span>
                  </div>
                  <div class="bv-error-row">
                    <span class="bv-error-row__label">Time</span>
                    <span class="bv-error-row__val">{{ bucketErrorDetail.time }}</span>
                  </div>
                  <div class="bv-error-row bv-error-row--hint">
                    <span class="bv-error-row__label">Hint</span>
                    <span class="bv-error-row__val">{{ bucketErrorDetail.hint }}</span>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <!-- Bucket history sidebar -->
          <div class="page-card bv-card bv-history-card">
            <div class="page-card__head">
              <div>
                <div class="page-card__title">Bucket History</div>
                <div class="page-card__sub">
                  {{ bucketForm.dest_conn_id ? 'Recent .sql files in this bucket' : 'Select a bucket to see history' }}
                </div>
              </div>
              <span v-if="bucketHistory.length" class="bv-count-badge">{{ bucketHistory.length }}</span>
            </div>
            <div class="page-card__body">
              <div v-if="!bucketForm.dest_conn_id" class="bv-empty">
                <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
                Select a destination bucket to view its backup history.
              </div>
              <div v-else-if="historyLoading" class="bv-empty">
                <svg class="spin" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
                Loading…
              </div>
              <div v-else-if="!bucketHistory.length" class="bv-empty">
                No backup files found in this bucket{{ bucketForm.subfolder ? ' under ' + bucketForm.subfolder : '' }}.
              </div>
              <div v-else class="bv-history-list">
                <div v-for="obj in bucketHistory" :key="obj.key" class="bv-history-item">
                  <div class="bv-history-item__icon">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
                  </div>
                  <div class="bv-history-item__body">
                    <div class="bv-history-item__key">{{ obj.key.split('/').pop() }}</div>
                    <div class="bv-history-item__meta">
                      <span>{{ formatBytes(obj.size) }}</span>
                      <span>·</span>
                      <span>{{ formatDate(obj.last_modified) }}</span>
                    </div>
                  </div>
                  <button class="bv-copy-btn" title="Copy object key" @click.stop="copyKey(obj.key)">
                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- ══ REQUEST DOWNLOAD ══════════════════════════════════════════ -->
        <section v-if="activeTab === 'request'" class="bv-request-layout">
          <div class="page-card bv-card">
            <div class="page-card__head">
              <div>
                <div class="page-card__title">Create Backup Download Request</div>
                <div class="page-card__sub">Submit a database dump request into the approval workflow.</div>
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
                <option v-for="wf in applicableWorkflows" :key="wf.id" :value="wf.id">{{ wf.name }}</option>
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
                <button v-for="item in filteredRequests" :key="item.id" class="bv-request-item" :class="{ 'bv-request-item--active': item.id === selectedRequestId }" @click="loadRequest(item.id)">
                  <div class="bv-request-item__top">
                    <strong>#{{ item.id }}</strong>
                    <span class="bv-status" :data-status="item.status">{{ formatStatus(item.status) }}</span>
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
                      <div class="bv-detail__sub">{{ selectedRequest.connection }} · {{ selectedRequest.database_name || 'default' }}</div>
                    </div>
                    <span class="bv-status" :data-status="selectedRequest.status">{{ formatStatus(selectedRequest.status) }}</span>
                  </div>
                  <div class="bv-meta">
                    <span>Requested by {{ selectedRequest.creator_name || 'unknown' }}</span>
                    <span>Reviewer {{ selectedRequest.reviewer_name || '—' }}</span>
                    <span>{{ formatDate(selectedRequest.created_at) }}</span>
                  </div>
                  <div v-if="selectedRequest.description" class="bv-description">{{ selectedRequest.description }}</div>
                  <textarea v-model="reviewNote" class="base-input bv-note" rows="3" placeholder="Review note or rejection reason…" />
                  <div class="bv-actions">
                    <button v-if="canApprove" class="base-btn base-btn--ghost base-btn--sm" :disabled="reviewLoading || selectedRequest.status !== 'pending_review'" @click="reviewRequest('rejected')">Reject</button>
                    <button v-if="canApprove" class="base-btn base-btn--primary base-btn--sm" :disabled="reviewLoading || selectedRequest.status !== 'pending_review'" @click="reviewRequest('approved')">Approve</button>
                    <button class="base-btn base-btn--primary base-btn--sm" :disabled="selectedRequest.status !== 'approved' && selectedRequest.status !== 'done'" @click="downloadApprovedRequest">Download Dump</button>
                  </div>
                  <div v-if="selectedRequest.review_note" class="notice notice--info"><strong>Review note:</strong> {{ selectedRequest.review_note }}</div>
                  <div v-if="selectedRequest.execute_error" class="notice notice--error"><strong>Error:</strong> {{ selectedRequest.execute_error }}</div>
                </div>
              </div>
            </div>
          </div>
        </section>

        <!-- ══ DIRECT DOWNLOAD ══════════════════════════════════════════ -->
        <div v-if="activeTab === 'direct' && canDirectBackup" class="page-card bv-card">
          <div class="page-card__head">
            <div>
              <div class="page-card__title">Direct SQL Dump</div>
              <div class="page-card__sub">Immediate backup download. Backup options above apply here too.</div>
            </div>
          </div>
          <div class="page-card__body bv-card-body">
            <select class="base-input" v-model.number="directConnId" @change="onDirectConnChange">
              <option :value="null">Select connection…</option>
              <option v-for="c in dbConnections" :key="c.id" :value="c.id">{{ c.name }}</option>
            </select>
            <select class="base-input" v-model="directDatabase" :disabled="!directConnId || databasesLoading">
              <option value="">Default database/schema</option>
              <option v-for="db in databases" :key="db" :value="db">{{ db }}</option>
            </select>

            <!-- reuse shared opts panel -->
            <div class="bv-opts-header" @click="showAdvanced = !showAdvanced">
              <div class="bv-section-label" style="border:none;padding:0;margin:0">
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.07 4.93A10 10 0 0 0 5 5.07M4.93 19.07A10 10 0 0 0 19 18.93"/></svg>
                Backup Options
              </div>
              <div class="bv-opts-header__right">
                <span class="bv-opts-summary">{{ backupOpts.sections === 'all' ? 'Full dump' : backupOpts.sections }}</span>
                <svg :style="{ transform: showAdvanced ? 'rotate(180deg)' : 'none', transition: 'transform 0.2s' }" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"/></svg>
              </div>
            </div>

            <div v-if="showAdvanced" class="bv-opts-panel">
              <div class="bv-opts-group">
                <div class="bv-opts-group__label">
                  <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
                  Output
                </div>
                <div class="bv-toggle-list">
                  <label class="bv-toggle-row">
                    <div>
                      <span class="bv-toggle-row__name">Compress output (gzip)</span>
                      <span class="bv-toggle-row__desc">Download as .sql.gz instead of plain .sql</span>
                    </div>
                    <div class="bv-switch" :class="{ 'is-on': backupOpts.compress }" @click="backupOpts.compress = !backupOpts.compress"><div class="bv-switch__knob"/></div>
                  </label>
                </div>
              </div>
              <div class="bv-opts-group">
                <div class="bv-opts-group__label">Sections</div>
                <div class="bv-sections-grid">
                  <label v-for="s in [
                    { v: 'all', label: 'All', sub: 'Schema + data + constraints' },
                    { v: 'pre-data', label: 'Pre-data', sub: 'CREATE TABLE, sequences' },
                    { v: 'data', label: 'Data', sub: 'INSERT statements only' },
                    { v: 'post-data', label: 'Post-data', sub: 'Indexes, FK constraints' },
                  ]" :key="s.v" class="bv-section-card" :class="{ 'is-active': backupOpts.sections === s.v }">
                    <input type="radio" v-model="backupOpts.sections" :value="s.v" style="display:none" />
                    <span class="bv-section-card__label">{{ s.label }}</span>
                    <span class="bv-section-card__sub">{{ s.sub }}</span>
                  </label>
                </div>
              </div>
            </div>

            <button class="base-btn base-btn--primary" :disabled="!directConnId" @click="downloadDirectBackup">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
              Download {{ backupOpts.compress ? '.sql.gz' : '.sql' }}
            </button>
          </div>
        </div>

        <!-- ══ RESTORE ══════════════════════════════════════════════════ -->
        <div v-if="activeTab === 'restore' && canDirectBackup" class="page-card bv-card">
          <div class="page-card__head">
            <div>
              <div class="page-card__title">Restore From SQL File</div>
              <div class="page-card__sub">High-risk direct restore for elevated backup operators.</div>
            </div>
          </div>
          <div class="page-card__body bv-card-body">
            <select class="base-input" v-model.number="restoreConnId">
              <option :value="null">Select connection…</option>
              <option v-for="c in dbConnections" :key="c.id" :value="c.id">{{ c.name }}</option>
            </select>
            <div class="bv-drop" @dragover.prevent @drop.prevent="(e) => { const f = e.dataTransfer?.files?.[0]; if (f) f.text().then(t => restoreSQL = t) }">
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
              <span>Drop a .sql file here or</span>
              <label class="bv-file-btn">Browse <input type="file" accept=".sql,.txt" style="display:none" @change="uploadFile" /></label>
            </div>
            <div v-if="restoreSQL" class="bv-preview">
              <div class="bv-preview-header">
                <span>{{ restoreSQL.split('\n').length }} lines loaded</span>
                <button class="base-btn base-btn--ghost base-btn--sm" @click="restoreSQL = ''">Clear</button>
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
/* ── Layout ── */
.bv-tabs { margin-bottom: 2px; }

.bv-bucket-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 340px;
  gap: 16px;
  align-items: flex-start;
}

.bv-request-layout {
  display: grid;
  grid-template-columns: 360px minmax(0, 1fr);
  gap: 16px;
}

.bv-card { padding: 20px; }

.bv-card-body {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.bv-filter { max-width: 180px; }

/* ── Section label ── */
.bv-section-label {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 11.5px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--text-muted);
  padding-bottom: 2px;
  border-bottom: 1px solid var(--border);
}

/* ── Connection picker grid ── */
.bv-conn-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
  gap: 8px;
}

.bv-conn-card {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 10px 12px;
  border: 1.5px solid var(--border);
  border-radius: 10px;
  background: var(--bg-body);
  cursor: pointer;
  transition: border-color 0.13s, background 0.13s, box-shadow 0.13s;
  text-align: left;
}

.bv-conn-card:hover {
  border-color: var(--brand);
  background: color-mix(in srgb, var(--brand) 5%, var(--bg-elevated));
}

.bv-conn-card.is-active {
  border-color: var(--brand);
  background: color-mix(in srgb, var(--brand) 10%, var(--bg-elevated));
  box-shadow: 0 0 0 3px var(--brand-ring);
}

.bv-conn-card__badge {
  width: 28px;
  height: 28px;
  border-radius: 7px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.bv-conn-card__info {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1;
  min-width: 0;
}

.bv-conn-card__name {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-primary);
  word-break: break-word;
  line-height: 1.35;
}

.bv-conn-card.is-active .bv-conn-card__name { color: var(--brand); }

.bv-conn-card__driver {
  font-size: 10.5px;
  color: var(--text-muted);
  word-break: break-word;
  line-height: 1.3;
}

.bv-conn-card__check {
  color: var(--brand);
  flex-shrink: 0;
}

/* ── Backup options panel ── */
.bv-opts-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  border: 1.5px solid var(--border);
  border-radius: 10px;
  background: var(--bg-body);
  cursor: pointer;
  transition: border-color 0.15s, background 0.15s;
  user-select: none;
}

.bv-opts-header:hover {
  border-color: var(--brand);
  background: color-mix(in srgb, var(--brand) 4%, var(--bg-elevated));
}

.bv-opts-header__right {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--text-muted);
}

.bv-opts-summary {
  font-size: 11px;
  font-weight: 500;
  color: var(--text-muted);
  text-transform: capitalize;
}

.bv-opts-panel {
  border: 1.5px solid var(--border);
  border-radius: 10px;
  background: var(--bg-body);
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.bv-opts-group {
  padding: 14px 16px;
  border-bottom: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.bv-opts-group:last-child { border-bottom: none; }

.bv-opts-group__label {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.07em;
  color: var(--text-muted);
}

/* Sections radio grid */
.bv-sections-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 7px;
}

.bv-section-card {
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: 9px 11px;
  border: 1.5px solid var(--border);
  border-radius: 8px;
  background: var(--bg-elevated);
  cursor: pointer;
  transition: border-color 0.12s, background 0.12s, box-shadow 0.12s;
}

.bv-section-card:hover { border-color: var(--brand); }

.bv-section-card.is-active {
  border-color: var(--brand);
  background: color-mix(in srgb, var(--brand) 10%, var(--bg-elevated));
  box-shadow: 0 0 0 3px var(--brand-ring);
}

.bv-section-card__label {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-primary);
}

.bv-section-card.is-active .bv-section-card__label { color: var(--brand); }

.bv-section-card__sub {
  font-size: 10px;
  color: var(--text-muted);
  line-height: 1.4;
}

/* Toggle rows */
.bv-toggle-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.bv-toggle-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 4px;
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.1s;
}

.bv-toggle-row:hover { background: color-mix(in srgb, var(--brand) 4%, transparent); }

.bv-toggle-row__name {
  display: block;
  font-size: 12.5px;
  font-weight: 500;
  color: var(--text-primary);
}

.bv-toggle-row__desc {
  display: block;
  font-size: 11px;
  color: var(--text-muted);
  margin-top: 1px;
}

/* Switch toggle */
.bv-switch {
  position: relative;
  width: 34px;
  height: 18px;
  border-radius: 999px;
  background: var(--border);
  flex-shrink: 0;
  cursor: pointer;
  transition: background 0.2s;
}

.bv-switch.is-on { background: var(--brand); }

.bv-switch__knob {
  position: absolute;
  top: 2px;
  left: 2px;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: white;
  transition: transform 0.2s;
  box-shadow: 0 1px 3px rgba(0,0,0,0.25);
}

.bv-switch.is-on .bv-switch__knob { transform: translateX(16px); }

/* Check pills */
.bv-check-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
}

.bv-check-pill {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 5px 11px;
  border: 1.5px solid var(--border);
  border-radius: 999px;
  background: var(--bg-elevated);
  font-size: 12px;
  font-weight: 500;
  color: var(--text-secondary);
  cursor: pointer;
  transition: border-color 0.12s, background 0.12s, color 0.12s;
}

.bv-check-pill svg { opacity: 0; transition: opacity 0.12s; }

.bv-check-pill:hover { border-color: var(--brand); }

.bv-check-pill.is-on {
  border-color: var(--brand);
  background: color-mix(in srgb, var(--brand) 12%, var(--bg-elevated));
  color: var(--brand);
}

.bv-check-pill.is-on svg { opacity: 1; }

.bv-badge-sm {
  font-size: 9px;
  font-weight: 700;
  padding: 1px 4px;
  border-radius: 4px;
  background: var(--bg-surface);
  color: var(--text-muted);
  letter-spacing: 0.04em;
}

/* ── Filename preview ── */
.bv-preview-name {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 10px 12px;
  background: var(--brand-dim);
  border: 1px solid color-mix(in srgb, var(--brand) 20%, transparent);
  border-radius: 8px;
  font-size: 12px;
  color: var(--brand);
  line-height: 1.5;
}

.bv-preview-name em { color: var(--text-muted); font-style: normal; }

/* ── Run row ── */
.bv-run-row {
  display: flex;
  gap: 10px;
  align-items: center;
}

.bv-cancel-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 14px;
  height: 34px;
  border-radius: 8px;
  border: 1px solid color-mix(in srgb, var(--error, #e05252) 40%, transparent);
  background: color-mix(in srgb, var(--error, #e05252) 8%, transparent);
  color: var(--error, #e05252);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.15s, border-color 0.15s;
}
.bv-cancel-btn:hover {
  background: color-mix(in srgb, var(--error, #e05252) 16%, transparent);
  border-color: color-mix(in srgb, var(--error, #e05252) 60%, transparent);
}

/* ── Result card ── */
.bv-result-card {
  border: 1px solid var(--success);
  border-radius: 10px;
  background: color-mix(in srgb, var(--success) 8%, transparent);
  overflow: hidden;
}

.bv-result-card__head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  font-size: 12.5px;
  font-weight: 600;
  color: var(--success);
  border-bottom: 1px solid color-mix(in srgb, var(--success) 20%, transparent);
}

.bv-result-card__rows {
  display: flex;
  flex-direction: column;
  gap: 0;
}

.bv-result-row {
  display: flex;
  align-items: baseline;
  gap: 10px;
  padding: 8px 14px;
  border-bottom: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
  font-size: 12px;
}

.bv-result-row:last-child { border-bottom: none; }

.bv-result-row__label {
  min-width: 90px;
  font-weight: 600;
  color: var(--text-muted);
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  flex-shrink: 0;
}

.bv-result-row__val {
  color: var(--text-primary);
  font-family: var(--mono);
  font-size: 11.5px;
  word-break: break-all;
}

.bv-result-size {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.bv-result-size__orig {
  font-size: 11px;
  color: var(--text-muted);
  font-family: var(--mono);
}

.bv-result-size__ratio {
  padding: 1px 7px;
  border-radius: 99px;
  background: color-mix(in srgb, var(--success) 15%, transparent);
  color: var(--success);
  font-size: 10.5px;
  font-weight: 600;
  font-family: var(--sans-serif, inherit);
  letter-spacing: 0.02em;
}

/* ── Progress bar ── */
.bv-progress-wrap {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.bv-progress-bar {
  height: 6px;
  border-radius: 99px;
  background: color-mix(in srgb, var(--brand) 15%, transparent);
  overflow: hidden;
}

.bv-progress-bar__fill {
  height: 100%;
  border-radius: 99px;
  background: var(--brand);
  transition: width 0.8s cubic-bezier(0.4, 0, 0.2, 1);
}

.bv-progress-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.bv-progress-stage {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 11.5px;
  color: var(--text-muted);
}

.bv-progress-right {
  display: flex;
  align-items: center;
  gap: 10px;
}

.bv-progress-pct {
  font-size: 11px;
  font-weight: 600;
  color: var(--brand);
  font-family: var(--mono);
}

.bv-log-toggle {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  border-radius: 6px;
  border: 1px solid color-mix(in srgb, var(--border) 70%, transparent);
  background: transparent;
  font-size: 11px;
  color: var(--text-muted);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
  white-space: nowrap;
}
.bv-log-toggle:hover {
  background: color-mix(in srgb, var(--border) 40%, transparent);
  color: var(--text-primary);
}

/* ── Step log ── */
.bv-log {
  margin-top: 8px;
  border: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
  border-radius: 8px;
  overflow: hidden;
  background: color-mix(in srgb, var(--bg-secondary, #1a1a1a) 60%, transparent);
}

.bv-log-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  border-bottom: 1px solid color-mix(in srgb, var(--border) 40%, transparent);
  font-size: 12px;
  transition: background 0.15s;
}
.bv-log-row:last-child { border-bottom: none; }

.bv-log-row--done   { color: var(--text-muted); }
.bv-log-row--active { color: var(--text-primary); background: color-mix(in srgb, var(--brand) 6%, transparent); }
.bv-log-row--pending { color: color-mix(in srgb, var(--text-muted) 50%, transparent); }

.bv-log-icon {
  display: flex;
  align-items: center;
  flex-shrink: 0;
}
.bv-log-row--done   .bv-log-icon { color: var(--success, #4caf8a); }
.bv-log-row--active .bv-log-icon { color: var(--brand); }
.bv-log-row--pending .bv-log-icon { color: color-mix(in srgb, var(--text-muted) 40%, transparent); }

.bv-log-label {
  flex: 1;
  font-size: 12px;
}
.bv-log-row--done .bv-log-label {
  text-decoration: line-through;
  text-decoration-color: color-mix(in srgb, var(--text-muted) 40%, transparent);
}

.bv-log-time {
  font-family: var(--mono);
  font-size: 10.5px;
  color: var(--text-muted);
  opacity: 0.7;
  min-width: 70px;
  text-align: right;
}

/* ── Error card ── */
.bv-error-card {
  border: 1px solid color-mix(in srgb, var(--error, #e05252) 35%, transparent);
  border-radius: 10px;
  background: color-mix(in srgb, var(--error, #e05252) 6%, transparent);
  overflow: hidden;
}

.bv-error-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  border-bottom: 1px solid color-mix(in srgb, var(--error, #e05252) 20%, transparent);
}

.bv-error-card__head-left {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 12.5px;
  font-weight: 600;
  color: var(--error, #e05252);
}

.bv-error-badge {
  padding: 1px 7px;
  border-radius: 99px;
  background: color-mix(in srgb, var(--error, #e05252) 15%, transparent);
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.03em;
}

.bv-error-copy {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 3px 10px;
  border-radius: 6px;
  border: 1px solid color-mix(in srgb, var(--border) 80%, transparent);
  background: transparent;
  font-size: 11px;
  color: var(--text-muted);
  cursor: pointer;
  transition: all 0.15s;
}
.bv-error-copy:hover {
  background: color-mix(in srgb, var(--border) 40%, transparent);
  color: var(--text-primary);
}

.bv-error-card__rows {
  display: flex;
  flex-direction: column;
}

.bv-error-row {
  display: flex;
  align-items: baseline;
  gap: 10px;
  padding: 8px 14px;
  border-bottom: 1px solid color-mix(in srgb, var(--border) 50%, transparent);
  font-size: 12px;
}
.bv-error-row:last-child { border-bottom: none; }

.bv-error-row__label {
  min-width: 75px;
  flex-shrink: 0;
  font-size: 10.5px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-muted);
}

.bv-error-row__val {
  color: var(--text-primary);
  font-size: 12px;
  line-height: 1.5;
  word-break: break-word;
}

.bv-error-row--message .bv-error-row__val {
  color: var(--error, #e05252);
  font-family: var(--mono);
  font-size: 11.5px;
}

.bv-error-row--hint .bv-error-row__val {
  color: var(--text-muted);
  font-style: italic;
}

/* ── History ── */
.bv-history-card { min-height: 320px; }

.bv-count-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 24px;
  height: 20px;
  padding: 0 7px;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 700;
  background: var(--brand-dim);
  color: var(--brand);
}

.bv-history-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 0 4px;
}

.bv-history-item {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 9px 10px;
  border-radius: 8px;
  transition: background 0.1s;
}

.bv-history-item:hover { background: var(--bg-body); }

.bv-history-item__icon {
  width: 28px;
  height: 28px;
  border-radius: 6px;
  background: var(--bg-body);
  border: 1px solid var(--border);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  flex-shrink: 0;
}

.bv-history-item__body { flex: 1; min-width: 0; }

.bv-copy-btn {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.15s, background 0.15s, color 0.15s;
}

.bv-history-item:hover .bv-copy-btn { opacity: 1; }

.bv-copy-btn:hover {
  background: var(--bg-elevated);
  color: var(--brand);
  border-color: var(--brand);
}

.bv-history-item__key {
  font-size: 11.5px;
  font-weight: 500;
  color: var(--text-primary);
  font-family: var(--mono);
  word-break: break-all;
  line-height: 1.4;
}

.bv-history-item__meta {
  display: flex;
  gap: 5px;
  align-items: center;
  font-size: 11px;
  color: var(--text-muted);
  margin-top: 2px;
}

/* ── Hints ── */
.bv-hint-inline {
  font-size: 10.5px;
  font-weight: 400;
  color: var(--text-muted);
  margin-left: 4px;
}

.bv-empty-hint {
  font-size: 12.5px;
  color: var(--text-muted);
  padding: 12px 0;
}

/* ── Request section (unchanged styles) ── */
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
  transition: border-color 0.13s;
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
.bv-detail__title { font-size: 13px; font-weight: 700; color: var(--text-primary); }

.bv-request-item__meta,
.bv-detail__sub,
.bv-meta { font-size: 11px; color: var(--text-muted); }

.bv-detail-card {
  border: 1px solid var(--border);
  border-radius: 16px;
  background: var(--bg-elevated);
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.bv-description { font-size: 12px; color: var(--text-secondary); line-height: 1.6; }
.bv-note { width: 100%; min-height: 84px; }

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
.bv-status[data-status="done"] { background: rgba(34, 197, 94, 0.15); color: #16a34a; }

.bv-status[data-status="pending_review"],
.bv-status[data-status="executing"] { background: rgba(245, 158, 11, 0.16); color: #d97706; }

.bv-status[data-status="rejected"],
.bv-status[data-status="failed"] { background: rgba(239, 68, 68, 0.14); color: #dc2626; }

.bv-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 32px 20px;
  color: var(--text-muted);
  font-size: 12.5px;
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

.bv-drop:hover { border-color: var(--brand); }

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

.notice { border-radius: 10px; padding: 10px 14px; font-size: 12.5px; display: flex; align-items: center; gap: 8px; }
.notice--ok { background: rgba(74, 222, 128, 0.1); border: 1px solid rgba(74, 222, 128, 0.3); color: #16a34a; }
.notice--error { background: rgba(239, 68, 68, 0.1); border: 1px solid rgba(239, 68, 68, 0.25); color: #dc2626; }
.notice--warn, .notice--info { background: rgba(245, 158, 11, 0.12); border: 1px solid rgba(245, 158, 11, 0.22); color: #b45309; }

/* ── Responsive ── */
@media (max-width: 1100px) {
  .bv-request-layout,
  .bv-request-grid { grid-template-columns: 1fr; }
}

@media (max-width: 900px) {
  .bv-bucket-layout { grid-template-columns: 1fr; }
}

@media (max-width: 720px) {
  .bv-card { padding: 16px; }

  /* Connection grid: horizontal scroll strip so cards never get squished */
  .bv-conn-grid {
    display: flex;
    overflow-x: auto;
    -webkit-overflow-scrolling: touch;
    scroll-snap-type: x mandatory;
    scrollbar-width: none;
    padding-bottom: 4px;
    /* keep consistent gap */
    gap: 8px;
  }
  .bv-conn-grid::-webkit-scrollbar { display: none; }
  .bv-conn-grid .bv-conn-card {
    flex-shrink: 0;
    width: 175px;
    scroll-snap-align: start;
  }

  /* Sections: same horizontal scroll so labels never wrap awkwardly */
  .bv-sections-grid {
    display: flex;
    overflow-x: auto;
    -webkit-overflow-scrolling: touch;
    scroll-snap-type: x mandatory;
    scrollbar-width: none;
    padding-bottom: 4px;
  }
  .bv-sections-grid::-webkit-scrollbar { display: none; }
  .bv-sections-grid .bv-section-card {
    flex-shrink: 0;
    min-width: 120px;
    scroll-snap-align: start;
  }
}

@media (max-width: 480px) {
  .bv-run-row { flex-direction: column; align-items: stretch; }
  .bv-conn-grid .bv-conn-card { width: 160px; }
}
</style>
