<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import { useAuth } from '@/composables/useAuth'

defineOptions({ inheritAttrs: false })

const props = defineProps<{ activeConnId?: number | null }>()

const { authReady } = useAuth()

// ── Types ────────────────────────────────────────────────────────────

interface SlowQueryRow {
  query_id: string
  query: string
  statement_type: string
  database: string
  username: string
  calls: number
  avg_ms: number
  min_ms: number
  max_ms: number
  total_ms: number
  rows: number
  // Cloud-provider fields
  exec_time?: string       // raw string e.g. "1.04899 s"
  lock_time?: string
  rows_sent?: string
  rows_examined?: string
  client_ip?: string
  start_time?: string
}

interface SlowQueryResponse {
  rows: SlowQueryRow[]
  total: number
  threshold_ms: number
  source: string
  notice?: string
}

interface ErrorLogRow {
  log_time: string
  severity: string
  message: string
  detail: string
  hint: string
  query: string
  username: string
  database_name: string
  app_name: string
  remote_host: string
  sql_state: string
  // parsed from Huawei content field
  parsed_host?: string
  parsed_user?: string
  parsed_db?: string
  parsed_app?: string
  parsed_pid?: string
  parsed_msg?: string
}

interface ErrorLogResponse {
  rows: ErrorLogRow[]
  total: number
  source: string
  notice?: string
}

// ── Connections ──────────────────────────────────────────────────────

const { connections, fetchConnections } = useConnections()
const pgConnections = computed(() => connections.value.filter(c => c.driver === 'postgres'))
const selectedConnId = ref<number | null>(null)

// ── Tab state ────────────────────────────────────────────────────────

type Tab = 'slow' | 'error' | 'audit'
const activeTab = ref<Tab>('slow')

// ── Cloud Provider Config ─────────────────────────────────────────────

interface CloudConfig {
  id?: number
  conn_id?: number
  name: string
  provider: string
  region: string
  project_id: string
  instance_id: string
  access_key: string
  secret_key: string
  is_active?: boolean
}

const cloudConfigured = ref(false)
const showCloudModal = ref(false)
const cloudSaving = ref(false)
const cloudConfigs = ref<CloudConfig[]>([])
const selectedCloudConfigId = ref<number | null>(null)
function defaultCloudForm(): CloudConfig {
  return {
    id: undefined,
    name: '',
    provider: 'huawei',
    region: '',
    project_id: '',
    instance_id: '',
    access_key: '',
    secret_key: '',
  }
}

const cloudForm = ref<CloudConfig>(defaultCloudForm())
const cloudProvider = computed(() => (cloudForm.value.provider || 'huawei').toLowerCase())
const cloudSupportsAudit = computed(() => cloudConfigured.value && cloudProvider.value === 'huawei')
const cloudProviderLabel = computed(() => cloudProvider.value === 'alibaba' ? 'Alibaba Cloud RDS' : 'Huawei Cloud RDS')
const cloudProviderShort = computed(() => cloudProvider.value === 'alibaba' ? 'Alibaba' : 'Huawei')
const cloudSourceLabel = computed(() => `${cloudProviderLabel.value} API`)
const cloudButtonText = computed(() => cloudConfigured.value ? `${cloudProviderShort.value}: ON` : 'Cloud')
const cloudRegionPlaceholder = computed(() => cloudProvider.value === 'alibaba' ? 'cn-hangzhou' : 'ap-southeast-3')
const cloudInstancePlaceholder = computed(() => cloudProvider.value === 'alibaba' ? 'rm-xxxx' : 'db-rds-xxx...')
const showErrorLevelFilter = computed(() => !cloudConfigured.value || cloudProvider.value !== 'alibaba')
const showSlowTypeFilter = computed(() => !cloudConfigured.value || cloudProvider.value !== 'alibaba')
const slowDefaultRangeLabel = computed(() => '7 days')
const activeCloudConfig = computed(() => cloudConfigs.value.find(c => c.id === selectedCloudConfigId.value) ?? cloudConfigs.value.find(c => c.is_active) ?? null)

function cloudConfigLabel(cfg: CloudConfig): string {
  const provider = (cfg.provider || 'huawei').toLowerCase() === 'alibaba' ? 'Alibaba' : 'Huawei'
  const name = cfg.name?.trim()
  if (name) return name
  const parts = [provider, cfg.instance_id, cfg.region].filter(Boolean)
  return parts.join(' · ') || provider
}

function applyCloudConfigState(data: any) {
  const configs = Array.isArray(data?.configs) ? data.configs : []
  cloudConfigs.value = configs.map((cfg: any): CloudConfig => ({
    id: cfg.id,
    conn_id: cfg.conn_id,
    name: cfg.name ?? '',
    provider: cfg.provider ?? 'huawei',
    region: cfg.region ?? '',
    project_id: cfg.project_id ?? '',
    instance_id: cfg.instance_id ?? '',
    access_key: cfg.access_key ?? '',
    secret_key: '',
    is_active: !!cfg.is_active,
  }))
  cloudConfigured.value = data?.configured ?? cloudConfigs.value.length > 0
  const active = data?.config ?? cloudConfigs.value.find(c => c.is_active) ?? cloudConfigs.value[0]
  if (active) {
    selectedCloudConfigId.value = active.id ?? null
    cloudForm.value = {
      id: active.id,
      conn_id: active.conn_id,
      name: active.name ?? '',
      provider: active.provider ?? 'huawei',
      region: active.region ?? '',
      project_id: active.project_id ?? '',
      instance_id: active.instance_id ?? '',
      access_key: active.access_key ?? '',
      secret_key: '',
      is_active: true,
    }
  } else {
    selectedCloudConfigId.value = null
    cloudForm.value = defaultCloudForm()
  }
}

async function loadCloudConfig() {
  if (!selectedConnId.value) return
  // Wait until the auth state is hydrated so we don't fire before the token is ready
  if (!authReady.value) {
    await new Promise<void>((resolve) => {
      const stop = watch(authReady, (ready) => { if (ready) { stop(); resolve() } })
    })
  }
  try {
    const { data } = await axios.get(`/api/connections/${selectedConnId.value}/cloud-config`)
    applyCloudConfigState(data)
  } catch {
    cloudConfigured.value = false
    cloudConfigs.value = []
    selectedCloudConfigId.value = null
    cloudForm.value = defaultCloudForm()
  }
}

async function saveCloudConfig() {
  if (!selectedConnId.value) return
  cloudSaving.value = true
  try {
    const { data } = await axios.post(`/api/connections/${selectedConnId.value}/cloud-config`, cloudForm.value)
    applyCloudConfigState(data)
    showCloudModal.value = false
    reloadActiveTab()
  } catch (e: any) {
    alert('Failed to save: ' + (e?.response?.data?.error ?? e.message))
  } finally {
    cloudSaving.value = false
  }
}

async function deleteCloudConfig() {
  if (!selectedConnId.value || !confirm('Remove this cloud instance config?')) return
  try {
    const { data } = await axios.delete(`/api/connections/${selectedConnId.value}/cloud-config`, {
      params: cloudForm.value.id ? { id: cloudForm.value.id } : undefined,
    })
    applyCloudConfigState(data)
    reloadActiveTab()
  } catch (e: any) {
    alert('Failed to remove: ' + (e?.response?.data?.error ?? e.message))
  }
}

async function activateCloudConfig() {
  if (!selectedConnId.value || !selectedCloudConfigId.value) return
  try {
    const { data } = await axios.post(`/api/connections/${selectedConnId.value}/cloud-config/active`, {
      id: selectedCloudConfigId.value,
    })
    applyCloudConfigState(data)
    reloadActiveTab()
  } catch (e: any) {
    alert('Failed to switch cloud instance: ' + (e?.response?.data?.error ?? e.message))
  }
}

function openCloudConfigModal() {
  const active = activeCloudConfig.value
  if (active) {
    cloudForm.value = { ...active, secret_key: '' }
  }
  showCloudModal.value = true
}

function newCloudConfig() {
  cloudForm.value = defaultCloudForm()
  showCloudModal.value = true
}

// ── Slow Query state ─────────────────────────────────────────────────

const slowRows = ref<SlowQueryRow[]>([])
const slowTotal = ref(0)
const slowSource = ref('')
const slowNotice = ref('')
const slowLoading = ref(false)
const slowThreshold = ref(1000)
const slowPage = ref(1)
const slowLimit = ref(25)
const slowDbFilter = ref('')
const slowUserFilter = ref('')
const slowStmtFilter = ref('')
const slowFrom = ref('')
const slowTo = ref('')
const selectedSlow = ref<SlowQueryRow | null>(null)

const slowFiltered = computed(() => {
  if (cloudConfigured.value && cloudProvider.value === 'alibaba') return slowRows.value
  if (!slowStmtFilter.value) return slowRows.value
  return slowRows.value.filter(r => r.statement_type === slowStmtFilter.value)
})

const slowTotalPages = computed(() => Math.max(1, Math.ceil(slowTotal.value / slowLimit.value)))

async function loadSlowQueries() {
  if (!selectedConnId.value) return
  slowLoading.value = true
  slowNotice.value = ''
  try {
    if (cloudConfigured.value) {
      const { data } = await axios.get(
        `/api/connections/${selectedConnId.value}/cloud-logs/slow-logs`,
        {
          params: {
            limit: slowLimit.value,
            page: slowPage.value,
            db: slowDbFilter.value || undefined,
            type: cloudProvider.value === 'alibaba' ? undefined : slowStmtFilter.value || undefined,
            from: slowFrom.value ? slowFrom.value + 'T00:00:00+0000' : undefined,
            to: slowTo.value ? slowTo.value + 'T23:59:59+0000' : undefined,
          },
        },
      )
      // Cloud proxy returns { slow_log_list: [...], total_record: N }
      const list = data.slow_log_list ?? []
      slowRows.value = list.map((r: any): SlowQueryRow => ({
        query_id: '',
        query: r.query_sample ?? '',
        statement_type: r.type ?? '',
        database: r.database ?? '',
        username: r.users ?? '',
        calls: parseInt(r.count ?? '1', 10) || 1,
        avg_ms: parseHuaweiDuration(r.time),
        min_ms: 0,
        max_ms: parseHuaweiDuration(r.time),
        total_ms: parseHuaweiDuration(r.time) * (parseInt(r.count ?? '1', 10) || 1),
        rows: parseInt(r.rows_sent ?? '0', 10) || 0,
        exec_time: r.time,
        lock_time: r.lock_time,
        rows_sent: r.rows_sent,
        rows_examined: r.rows_examined,
        client_ip: r.client_ip,
        start_time: r.start_time,
      }))
      slowTotal.value = data.total_record ?? list.length
      slowSource.value = data.source ?? cloudSourceLabel.value
      slowNotice.value = data.notice ?? ''
    } else {
      const { data } = await axios.get<SlowQueryResponse>(
        `/api/connections/${selectedConnId.value}/db-logs/slow-queries`,
        {
          params: {
            threshold_ms: slowThreshold.value,
            limit: slowLimit.value,
            page: slowPage.value,
            db: slowDbFilter.value || undefined,
            user: slowUserFilter.value || undefined,
          },
        },
      )
      slowRows.value = data.rows ?? []
      slowTotal.value = data.total ?? 0
      slowSource.value = data.source ?? ''
      slowNotice.value = data.notice ?? ''
    }
  } catch (e: any) {
    slowNotice.value = e?.response?.data?.error || e?.message || 'Failed to load slow queries'
    slowRows.value = []
  } finally {
    slowLoading.value = false
  }
}

function slowGoPage(p: number) {
  slowPage.value = p
  loadSlowQueries()
}

function slowApply() {
  slowPage.value = 1
  loadSlowQueries()
}

// ── Error Log state ──────────────────────────────────────────────────

const errorRows = ref<ErrorLogRow[]>([])
const errorTotal = ref(0)
const errorSource = ref('')
const errorNotice = ref('')
const errorLoading = ref(false)
const errorPage = ref(1)
const errorLimit = ref(50)
const errorLevels = ref<string[]>([])
const errorFrom = ref('')
const errorTo = ref('')
const selectedError = ref<ErrorLogRow | null>(null)

const ERROR_LEVELS = ['ERROR', 'FATAL', 'WARNING', 'CONTEXT', 'STATEMENT', 'LOG']
const errorTotalPages = computed(() => Math.max(1, Math.ceil(errorTotal.value / errorLimit.value)))

watch(cloudProvider, (provider) => {
  if (provider !== 'alibaba') return
  errorLevels.value = []
  slowStmtFilter.value = ''
  if (errorLimit.value < 30) errorLimit.value = 30
})

function toggleLevel(l: string) {
  if (errorLevels.value.includes(l)) {
    errorLevels.value = errorLevels.value.filter(x => x !== l)
  } else {
    errorLevels.value.push(l)
  }
}

async function loadErrorLogs() {
  if (!selectedConnId.value) return
  errorLoading.value = true
  errorNotice.value = ''
  try {
    if (cloudConfigured.value) {
      if (cloudProvider.value === 'alibaba' && errorLimit.value < 30) errorLimit.value = 30
      const { data } = await axios.get(
        `/api/connections/${selectedConnId.value}/cloud-logs/error-logs`,
        {
          params: {
            limit: errorLimit.value,
            page: errorPage.value,
            level: cloudProvider.value === 'alibaba' ? undefined : errorLevels.value.length ? errorLevels.value[0] : undefined,
            from: errorFrom.value ? errorFrom.value + 'T00:00:00+0000' : undefined,
            to: errorTo.value ? errorTo.value + 'T23:59:59+0000' : undefined,
          },
        },
      )
      // Cloud proxy returns { error_log_list: [...], total_record: N }
      const list = data.error_log_list ?? []
      errorRows.value = list.map((r: any): ErrorLogRow => {
        const parsed = parseHuaweiLogContent(r.content ?? '')
        return {
          log_time: r.time ?? '',
          severity: r.level ?? 'ERROR',
          message: r.content ?? '',
          detail: '', hint: '', query: '',
          username: parsed.user,
          database_name: parsed.db,
          app_name: parsed.app,
          remote_host: parsed.host,
          sql_state: '',
          parsed_host: parsed.host,
          parsed_user: parsed.user,
          parsed_db: parsed.db,
          parsed_app: parsed.app,
          parsed_pid: parsed.pid,
          parsed_msg: parsed.msg,
        }
      })
      errorTotal.value = data.total_record ?? list.length
      errorSource.value = data.source ?? cloudSourceLabel.value
    } else {
      // Fetch from pg_catalog.pg_log / pg_read_file
      const { data } = await axios.get<ErrorLogResponse>(
        `/api/connections/${selectedConnId.value}/db-logs/error-logs`,
        {
          params: {
            limit: errorLimit.value,
            page: errorPage.value,
            level: errorLevels.value.length ? errorLevels.value.join(',') : undefined,
            from: errorFrom.value || undefined,
            to: errorTo.value || undefined,
          },
        },
      )
      errorRows.value = data.rows ?? []
      errorTotal.value = data.total ?? 0
      errorSource.value = data.source ?? ''
      errorNotice.value = data.notice ?? ''
    }
  } catch (e: any) {
    const raw = e?.response?.data
    let msg = e?.message ?? 'Failed to load error logs'
    if (typeof raw === 'string') { try { msg = JSON.parse(raw)?.error ?? raw } catch { msg = raw } }
    else if (raw?.error) msg = raw.error
    errorNotice.value = msg
    errorRows.value = []
  } finally {
    errorLoading.value = false
  }
}

function errorGoPage(p: number) {
  errorPage.value = p
  loadErrorLogs()
}

function errorApply() {
  errorPage.value = 1
  loadErrorLogs()
}

// ── Audit Log state ──────────────────────────────────────────────────

interface AuditRow {
  id: number
  conn_id: number
  sql: string
  executed_at: string
  duration_ms: number
  rows_affected: number
  status: string
  error?: string
}

interface CloudAuditFile {
  id: string
  name: string
  size: number      // KB
  begin_time: string
  end_time: string
  downloading?: boolean
  downloadUrl?: string
}

const auditRows = ref<AuditRow[]>([])
const auditLoading = ref(false)
const auditNotice = ref('')
const auditPage = ref(1)
const auditLimit = ref(50)
const auditTotal = ref(0)
const auditSearch = ref('')
const selectedAudit = ref<AuditRow | null>(null)
const auditTotalPages = computed(() => Math.max(1, Math.ceil(auditTotal.value / auditLimit.value)))

// Cloud audit state
const cloudAuditFiles = ref<CloudAuditFile[]>([])
const cloudAuditTotal = ref(0)
const cloudAuditPage = ref(0)  // offset-based, 0-indexed
const cloudAuditLimit = ref(50)
const cloudAuditFrom = ref('')
const cloudAuditTo = ref('')
const cloudAuditSource = ref('')
const cloudAuditLoading = ref(false)
const cloudAuditNotice = ref('')

async function loadAuditLogs() {
  if (!selectedConnId.value) return
  auditLoading.value = true
  auditNotice.value = ''
  try {
    const { data } = await axios.get(`/api/connections/${selectedConnId.value}/history`, {
      params: {
        limit: auditLimit.value,
        offset: (auditPage.value - 1) * auditLimit.value,
        search: auditSearch.value || undefined,
      },
    })
    const rows = Array.isArray(data) ? data : (data.rows ?? data.history ?? [])
    auditRows.value = rows
    auditTotal.value = data.total ?? rows.length
  } catch (e: any) {
    auditNotice.value = e?.response?.data?.error || e?.message || 'Failed to load audit log'
    auditRows.value = []
  } finally {
    auditLoading.value = false
  }
}

async function loadCloudAuditLogs() {
  if (!selectedConnId.value) return
  cloudAuditLoading.value = true
  cloudAuditNotice.value = ''
  try {
    const { data } = await axios.get(
      `/api/connections/${selectedConnId.value}/cloud-logs/audit-logs`,
      {
        params: {
          limit: cloudAuditLimit.value,
          page: cloudAuditPage.value,
          from: cloudAuditFrom.value ? cloudAuditFrom.value + 'T00:00:00+0000' : undefined,
          to: cloudAuditTo.value ? cloudAuditTo.value + 'T23:59:59+0000' : undefined,
        },
      },
    )
    cloudAuditFiles.value = (data.auditlogs ?? []).map((f: any) => ({ ...f, downloading: false, downloadUrl: '' }))
    cloudAuditTotal.value = data.total_record ?? cloudAuditFiles.value.length
    cloudAuditSource.value = 'Huawei RDS API'
  } catch (e: any) {
    cloudAuditNotice.value = e?.response?.data?.error || e?.message || 'Failed to load audit logs'
    cloudAuditFiles.value = []
  } finally {
    cloudAuditLoading.value = false
  }
}

async function downloadAuditFile(file: CloudAuditFile) {
  file.downloading = true
  file.downloadUrl = ''
  try {
    const { data } = await axios.post(
      `/api/connections/${selectedConnId.value}/cloud-logs/audit-log-links`,
      { ids: [file.id] },
    )
    const link = data.links?.[0]
    if (link) {
      file.downloadUrl = link
      window.open(link, '_blank')
    }
  } catch (e: any) {
    cloudAuditNotice.value = e?.response?.data?.error || e?.message || 'Failed to get download link'
  } finally {
    file.downloading = false
  }
}

function cloudAuditGoPage(offset: number) {
  cloudAuditPage.value = offset
  loadCloudAuditLogs()
}

function cloudAuditApply() {
  cloudAuditPage.value = 0
  loadCloudAuditLogs()
}

function auditGoPage(p: number) {
  auditPage.value = p
  loadAuditLogs()
}

function fmtKB(kb: number): string {
  if (kb >= 1024 * 1024) return (kb / 1024 / 1024).toFixed(1) + ' GB'
  if (kb >= 1024) return (kb / 1024).toFixed(1) + ' MB'
  return kb.toFixed(0) + ' KB'
}

// ── Tab switch ───────────────────────────────────────────────────────

function reloadActiveTab() {
  if (!selectedConnId.value) return
  if (activeTab.value === 'slow') loadSlowQueries()
  else if (activeTab.value === 'error') loadErrorLogs()
  else {
    if (cloudSupportsAudit.value) loadCloudAuditLogs()
    else loadAuditLogs()
  }
}

function switchTab(t: Tab) {
  activeTab.value = t
  reloadActiveTab()
}

watch(selectedConnId, async (id) => {
  if (!id) return
  selectedSlow.value = null
  selectedError.value = null
  selectedAudit.value = null
  await loadCloudConfig()
  reloadActiveTab()
})

function resolveLogsConnectionId(): number | null {
  const active = props.activeConnId != null
    ? pgConnections.value.find(c => c.id === props.activeConnId)
    : null
  return active?.id ?? pgConnections.value[0]?.id ?? null
}

function syncLogsConnection() {
  const nextId = resolveLogsConnectionId()
  if (selectedConnId.value !== nextId) selectedConnId.value = nextId
}

watch(() => props.activeConnId, () => {
  syncLogsConnection()
})

watch(pgConnections, () => {
  const stillAvailable = selectedConnId.value != null && pgConnections.value.some(c => c.id === selectedConnId.value)
  if (!stillAvailable || props.activeConnId != null) syncLogsConnection()
})

onMounted(async () => {
  await fetchConnections()
  syncLogsConnection()
})

// ── Helpers ──────────────────────────────────────────────────────────

function fmtMs(ms: number): string {
  if (ms >= 1000) return (ms / 1000).toFixed(2) + 's'
  return ms.toFixed(0) + 'ms'
}

function fmtNum(n: number): string {
  return n.toLocaleString()
}

function severityClass(s: string): string {
  switch (s?.toUpperCase()) {
    case 'FATAL': return 'sev-fatal'
    case 'ERROR': return 'sev-error'
    case 'WARNING': return 'sev-warning'
    case 'CONTEXT': return 'sev-context'
    case 'STATEMENT': return 'sev-stmt'
    default: return 'sev-info'
  }
}

function stmtClass(t: string): string {
  switch (t) {
    case 'SELECT': return 'stmt-select'
    case 'INSERT': return 'stmt-insert'
    case 'UPDATE': return 'stmt-update'
    case 'DELETE': return 'stmt-delete'
    default: return 'stmt-other'
  }
}

function truncate(s: string, n = 120): string {
  if (!s) return ''
  return s.length > n ? s.slice(0, n) + '…' : s
}

function today(): string {
  return new Date().toISOString().slice(0, 10)
}

function yesterday(): string {
  const d = new Date()
  d.setDate(d.getDate() - 1)
  return d.toISOString().slice(0, 10)
}

// Parse Huawei RDS log content:
// Format: "2026-05-13 22:51:14.216 +07:103.66.196.66(52630):user@db:[pid]:[n]:LEVEL: message"
interface ParsedLog {
  host: string; user: string; db: string; app: string; pid: string; msg: string
}
function parseHuaweiLogContent(content: string): ParsedLog {
  const empty: ParsedLog = { host: '', user: '', db: '', app: '', pid: '', msg: content }
  if (!content) return empty
  // Strip leading timestamp prefix: "2026-05-13 22:51:14.216 +07:"
  const afterTs = content.replace(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d+ [+-]\d+:/, '')
  // Now: "103.66.196.66(52630):user@db:[pid]:[n]:LEVEL: message"
  const m = afterTs.match(/^([^(]+)\((\d+)\):([^@]*)@([^:]*):(?:\[\d+\])?\[?\d+\]?:(?:[A-Z]+: )?(.*)/)
  if (!m) return { ...empty, msg: afterTs }
  const [, host, pid, user, db, rest] = m
  // Extract actual message after "LEVEL: " prefix
  const msgMatch = rest.match(/^[A-Z]+: (.+)/)
  const msg = msgMatch ? msgMatch[1] : rest
  return { host: host.trim(), user: user.trim(), db: db.trim(), app: '', pid, msg: msg.trim() }
}

// Parse Huawei duration strings like "1.04899 s", "320.5 ms", "0.00003 s"
function parseHuaweiDuration(s: string): number {
  if (!s) return 0
  const m = s.match(/([\d.]+)\s*(s|ms)/)
  if (!m) return 0
  return m[2] === 's' ? parseFloat(m[1]) * 1000 : parseFloat(m[1])
}

function fmtLogTime(t: string): string {
  if (!t) return ''
  // "2026-05-13T15:51:14.000Z" → "May 13, 15:51:14"
  try {
    const d = new Date(t)
    return d.toLocaleDateString('en-GB', { month: 'short', day: 'numeric' })
      + ' ' + d.toTimeString().slice(0, 8)
  } catch { return t }
}
</script>

<template>
  <div class="dblogs">
    <!-- Header -->
    <div class="dblogs-header">
      <div class="dblogs-header__left">
        <div class="dblogs-header__tag">DATABASE LOGS</div>
        <h1 class="dblogs-header__title">DB Logs</h1>
        <p class="dblogs-header__sub">Slow queries, error events, and SQL audit trail for your PostgreSQL connections.</p>
      </div>
    </div>

    <!-- No PG connections -->
    <div v-if="pgConnections.length === 0" class="dblogs-empty">
      <svg width="36" height="36" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5v14c0 1.657 4.03 3 9 3s9-1.343 9-3V5"/><path d="M3 12c0 1.657 4.03 3 9 3s9-1.343 9-3"/></svg>
      <p>No PostgreSQL connections found.</p>
      <p class="dblogs-muted">Create a PostgreSQL connection in Admin → Connections first.</p>
    </div>

    <template v-else>
      <!-- Tabs -->
      <div class="dblogs-tabs">
        <button class="dblogs-tab" :class="{ active: activeTab === 'error' }" @click="switchTab('error')">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
          Error Logs
        </button>
        <button class="dblogs-tab" :class="{ active: activeTab === 'slow' }" @click="switchTab('slow')">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
          Slow Query Logs
        </button>
        <button class="dblogs-tab" :class="{ active: activeTab === 'audit' }" @click="switchTab('audit')">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/></svg>
          SQL Audit Logs
        </button>
      </div>

      <!-- ── ERROR LOGS ───────────────────────────────────────── -->
      <div v-if="activeTab === 'error'" class="dblogs-panel">

        <!-- Toolbar -->
        <div class="err-toolbar">
          <div class="err-toolbar__left">
            <!-- Level pills -->
            <div v-if="showErrorLevelFilter" class="err-levels">
              <button
                v-for="lv in ERROR_LEVELS" :key="lv"
                class="err-lvl-btn" :class="[`lvl--${lv.toLowerCase()}`, { active: errorLevels.includes(lv) }]"
                @click="toggleLevel(lv); errorApply()"
              >{{ lv }}</button>
            </div>
            <!-- Date shortcuts -->
            <div class="err-datepicker">
              <div class="err-shortcuts">
                <button class="err-shortcut" :class="{ active: errorFrom === today() && errorTo === today() }" @click="errorFrom = today(); errorTo = today(); errorApply()">Today</button>
                <button class="err-shortcut" :class="{ active: errorFrom === yesterday() && errorTo === yesterday() }" @click="errorFrom = yesterday(); errorTo = yesterday(); errorApply()">Yesterday</button>
                <button class="err-shortcut" :class="{ active: !errorFrom && !errorTo }" @click="errorFrom = ''; errorTo = ''; errorApply()">7 days</button>
              </div>
              <div class="err-daterange">
                <input v-model="errorFrom" type="date" class="err-date-input" @change="errorApply()" />
                <span class="err-date-sep">→</span>
                <input v-model="errorTo" type="date" class="err-date-input" @change="errorApply()" />
              </div>
            </div>
          </div>
          <div class="err-toolbar__right">
            <select v-model="errorLimit" class="err-select" @change="errorApply()">
              <option v-if="!(cloudConfigured && cloudProvider === 'alibaba')" :value="25">25 / page</option>
              <option v-if="cloudConfigured && cloudProvider === 'alibaba'" :value="30">30 / page</option>
              <option :value="50">50 / page</option>
              <option :value="100">100 / page</option>
            </select>
            <select
              v-if="cloudConfigured"
              v-model.number="selectedCloudConfigId"
              class="err-select cloud-instance-select"
              @change="activateCloudConfig"
            >
              <option v-for="cfg in cloudConfigs" :key="cfg.id" :value="cfg.id">
                {{ cloudConfigLabel(cfg) }}
              </option>
            </select>
            <button
              class="err-cloud-btn" :class="{ 'err-cloud-btn--on': cloudConfigured }"
              @click="openCloudConfigModal"
              :title="cloudConfigured ? 'Cloud source active — click to reconfigure' : 'Connect cloud provider'"
            >
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 10h-1.26A8 8 0 1 0 9 20h9a5 5 0 0 0 0-10z"/></svg>
              {{ cloudButtonText }}
            </button>
          </div>
        </div>

        <!-- Error notice -->
        <div v-if="errorNotice && !cloudConfigured" class="err-notice err-notice--warn">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
          <span>{{ errorNotice }}</span>
          <button class="err-inline-btn" @click="openCloudConfigModal">
            <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 10h-1.26A8 8 0 1 0 9 20h9a5 5 0 0 0 0-10z"/></svg>
            Connect cloud provider
          </button>
        </div>
        <div v-if="errorNotice && cloudConfigured" class="err-notice err-notice--error">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
          <span>{{ errorNotice }}</span>
        </div>

        <!-- Loading skeleton -->
        <div v-if="errorLoading" class="err-skeleton-wrap">
          <div v-for="i in 8" :key="i" class="err-skeleton-row">
            <div class="err-skel err-skel--time" />
            <div class="err-skel err-skel--badge" />
            <div class="err-skel err-skel--db" />
            <div class="err-skel err-skel--msg" :style="{ width: (40 + (i * 7) % 40) + '%' }" />
          </div>
        </div>

        <!-- Empty state -->
        <div v-else-if="!errorRows.length" class="err-empty">
          <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.2" opacity=".3"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="8" y1="13" x2="16" y2="13"/><line x1="8" y1="17" x2="12" y2="17"/></svg>
          <p>No error log entries found</p>
          <span>Try adjusting the date range or level filters</span>
        </div>

        <!-- Log list -->
        <template v-else>
          <div class="err-list">
            <div
              v-for="(row, i) in errorRows" :key="i"
              class="err-row" :class="[`err-row--${(row.severity || 'info').toLowerCase()}`, { 'err-row--open': selectedError === row }]"
              @click="selectedError = selectedError === row ? null : row"
            >
              <!-- Row summary -->
              <div class="err-row__summary">
                <div class="err-row__left">
                  <span class="err-row__badge" :class="`badge--${(row.severity || 'info').toLowerCase()}`">{{ row.severity }}</span>
                  <span class="err-row__time">{{ fmtLogTime(row.log_time) }}</span>
                  <span v-if="row.parsed_db || row.database_name" class="err-row__db">
                    <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5v14c0 1.657 4.03 3 9 3s9-1.343 9-3V5"/><path d="M3 12c0 1.657 4.03 3 9 3s9-1.343 9-3"/></svg>
                    {{ row.parsed_db || row.database_name }}
                  </span>
                  <span v-if="row.parsed_user || row.username" class="err-row__user">
                    <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>
                    {{ row.parsed_user || row.username }}
                  </span>
                </div>
                <div class="err-row__msg">{{ truncate(row.parsed_msg || row.message, 180) }}</div>
                <svg class="err-row__chevron" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="6 9 12 15 18 9"/></svg>
              </div>

              <!-- Expanded detail -->
              <Transition name="err-expand">
                <div v-if="selectedError === row" class="err-row__detail" @click.stop>
                  <div class="err-detail-grid">
                    <div class="err-detail-section">
                      <div class="err-detail-label">Full Message</div>
                      <pre class="err-detail-pre">{{ row.parsed_msg || row.message }}</pre>
                    </div>
                    <div v-if="row.message !== (row.parsed_msg || row.message)" class="err-detail-section">
                      <div class="err-detail-label">Raw Log</div>
                      <pre class="err-detail-pre err-detail-pre--raw">{{ row.message }}</pre>
                    </div>
                  </div>
                  <div class="err-detail-meta">
                    <div v-if="row.log_time" class="err-meta-chip">
                      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
                      {{ row.log_time }}
                    </div>
                    <div v-if="row.parsed_host || row.remote_host" class="err-meta-chip">
                      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/></svg>
                      {{ row.parsed_host || row.remote_host }}
                    </div>
                    <div v-if="row.parsed_pid" class="err-meta-chip">
                      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3"/><path d="M12 1v4M12 19v4M4.22 4.22l2.83 2.83M16.95 16.95l2.83 2.83M1 12h4M19 12h4M4.22 19.78l2.83-2.83M16.95 7.05l2.83-2.83"/></svg>
                      PID {{ row.parsed_pid }}
                    </div>
                    <div v-if="row.parsed_db || row.database_name" class="err-meta-chip">
                      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5v14c0 1.657 4.03 3 9 3s9-1.343 9-3V5"/></svg>
                      {{ row.parsed_db || row.database_name }}
                    </div>
                    <div v-if="row.parsed_user || row.username" class="err-meta-chip">
                      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>
                      {{ row.parsed_user || row.username }}
                    </div>
                    <div v-if="row.app_name" class="err-meta-chip">
                      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="2" width="20" height="20" rx="5"/><path d="M16 8h-6a2 2 0 1 0 0 4h4a2 2 0 1 1 0 4H8"/><path d="M12 6v2m0 8v2"/></svg>
                      {{ row.app_name }}
                    </div>
                  </div>
                </div>
              </Transition>
            </div>
          </div>

          <!-- Pagination -->
          <div class="err-pager">
            <span class="err-pager__info">
              {{ errorTotal.toLocaleString() }} total · page {{ errorPage }} of {{ errorTotalPages }}
              <span v-if="errorSource" class="err-pager__source">via {{ errorSource }}</span>
            </span>
            <div class="err-pager__btns">
              <button class="err-pager__btn" :disabled="errorPage <= 1" @click="errorGoPage(1)">«</button>
              <button class="err-pager__btn" :disabled="errorPage <= 1" @click="errorGoPage(errorPage - 1)">‹ Prev</button>
              <span class="err-pager__cur">{{ errorPage }}</span>
              <button class="err-pager__btn" :disabled="errorPage >= errorTotalPages" @click="errorGoPage(errorPage + 1)">Next ›</button>
              <button class="err-pager__btn" :disabled="errorPage >= errorTotalPages" @click="errorGoPage(errorTotalPages)">»</button>
            </div>
          </div>
        </template>
      </div>

      <!-- ── SLOW QUERY LOGS ─────────────────────────────────── -->
      <div v-else-if="activeTab === 'slow'" class="dblogs-panel">

        <!-- Toolbar -->
        <div class="err-toolbar">
          <div class="err-toolbar__left">
            <!-- Statement type pills (cloud) / threshold slider (local) -->
            <template v-if="cloudConfigured">
              <div v-if="showSlowTypeFilter" class="err-levels">
                <button
                  v-for="t in ['SELECT','INSERT','UPDATE','DELETE','CREATE']" :key="t"
                  class="err-lvl-btn" :class="[`stmt--${t.toLowerCase()}`, { active: slowStmtFilter === t }]"
                  @click="slowStmtFilter = slowStmtFilter === t ? '' : t; slowApply()"
                >{{ t }}</button>
              </div>
              <div class="dblogs-toolbar__group">
                <span class="dblogs-label">Database</span>
                <input v-model="slowDbFilter" type="text" placeholder="all" class="dblogs-input dblogs-input--sm" @keydown.enter="slowApply" />
              </div>
              <div class="err-datepicker">
                <div class="err-shortcuts">
                  <button class="err-shortcut" :class="{ active: slowFrom === today() && slowTo === today() }" @click="slowFrom = today(); slowTo = today(); slowApply()">Today</button>
                  <button class="err-shortcut" :class="{ active: slowFrom === yesterday() && slowTo === yesterday() }" @click="slowFrom = yesterday(); slowTo = yesterday(); slowApply()">Yesterday</button>
                  <button class="err-shortcut" :class="{ active: !slowFrom && !slowTo }" @click="slowFrom = ''; slowTo = ''; slowApply()">{{ slowDefaultRangeLabel }}</button>
                </div>
                <div class="err-daterange">
                  <input v-model="slowFrom" type="date" class="err-date-input" @change="slowApply()" />
                  <span class="err-date-sep">→</span>
                  <input v-model="slowTo" type="date" class="err-date-input" @change="slowApply()" />
                </div>
              </div>
              <button class="dblogs-btn" @click="slowApply">
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
                Apply
              </button>
            </template>
            <template v-else>
              <div class="dblogs-toolbar__group">
                <span class="dblogs-label">Threshold</span>
                <div class="dblogs-threshold">
                  <input v-model.number="slowThreshold" type="range" min="0" max="60000" step="100" class="dblogs-slider" />
                  <span class="dblogs-threshold__val">{{ fmtMs(slowThreshold) }}</span>
                </div>
              </div>
              <div class="dblogs-toolbar__group">
                <span class="dblogs-label">Database</span>
                <input v-model="slowDbFilter" type="text" placeholder="all" class="dblogs-input dblogs-input--sm" />
              </div>
              <div class="dblogs-toolbar__group">
                <span class="dblogs-label">Type</span>
                <select v-model="slowStmtFilter" class="dblogs-select-sm">
                  <option value="">All</option>
                  <option>SELECT</option><option>INSERT</option><option>UPDATE</option>
                  <option>DELETE</option><option>CREATE</option>
                </select>
              </div>
              <button class="dblogs-btn" @click="slowApply">
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
                Apply
              </button>
            </template>
          </div>
          <div class="err-toolbar__right">
            <select v-model="slowLimit" class="err-select" @change="slowApply()">
              <option :value="25">25 / page</option>
              <option :value="50">50 / page</option>
              <option :value="100">100 / page</option>
            </select>
            <select
              v-if="cloudConfigured"
              v-model.number="selectedCloudConfigId"
              class="err-select cloud-instance-select"
              @change="activateCloudConfig"
            >
              <option v-for="cfg in cloudConfigs" :key="cfg.id" :value="cfg.id">
                {{ cloudConfigLabel(cfg) }}
              </option>
            </select>
            <button
              class="err-cloud-btn" :class="{ 'err-cloud-btn--on': cloudConfigured }"
              @click="openCloudConfigModal"
              :title="cloudConfigured ? 'Cloud source active — click to reconfigure' : 'Connect cloud provider'"
            >
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 10h-1.26A8 8 0 1 0 9 20h9a5 5 0 0 0 0-10z"/></svg>
              {{ cloudButtonText }}
            </button>
          </div>
        </div>

        <!-- Notice -->
        <div v-if="slowNotice" class="err-notice err-notice--warn">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
          <span>{{ slowNotice }}</span>
        </div>

        <!-- Loading skeleton -->
        <div v-if="slowLoading" class="err-skeleton-wrap">
          <div v-for="i in 8" :key="i" class="err-skeleton-row">
            <div class="err-skel err-skel--badge" />
            <div class="err-skel err-skel--time" />
            <div class="err-skel err-skel--db" />
            <div class="err-skel err-skel--msg" :style="{ width: (40 + (i * 7) % 40) + '%' }" />
            <div class="err-skel" style="width:60px;flex-shrink:0" />
          </div>
        </div>

        <!-- Empty -->
        <div v-else-if="!slowRows.length" class="err-empty">
          <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.2" opacity=".3"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
          <p>No slow query logs found</p>
          <span v-if="!cloudConfigured">Try lowering the threshold or click Apply</span>
          <span v-else>Try adjusting the date range or statement type</span>
        </div>

        <!-- List -->
        <template v-else>
          <div class="err-list slow-list">
            <div
              v-for="(row, i) in slowFiltered" :key="i"
              class="err-row slow-row" :class="[`slow-row--${(row.statement_type||'other').toLowerCase()}`, { 'err-row--open': selectedSlow === row }]"
              @click="selectedSlow = selectedSlow === row ? null : row"
            >
              <div class="err-row__summary">
                <div class="err-row__left">
                  <span class="err-row__badge" :class="stmtClass(row.statement_type)">{{ row.statement_type || '—' }}</span>
                  <span class="slow-time" :class="{ 'slow-time--warn': row.avg_ms > 5000, 'slow-time--crit': row.avg_ms > 30000 }">
                    {{ row.exec_time || fmtMs(row.avg_ms) }}
                  </span>
                  <span v-if="row.calls > 1" class="slow-calls">×{{ row.calls }}</span>
                  <span v-if="row.database" class="err-row__db">
                    <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5v14c0 1.657 4.03 3 9 3s9-1.343 9-3V5"/></svg>
                    {{ row.database }}
                  </span>
                  <span v-if="row.username" class="err-row__user">
                    <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>
                    {{ row.username }}
                  </span>
                </div>
                <div class="err-row__msg">{{ truncate(row.query, 180) }}</div>
                <svg class="err-row__chevron" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="6 9 12 15 18 9"/></svg>
              </div>

              <Transition name="err-expand">
                <div v-if="selectedSlow === row" class="err-row__detail" @click.stop>
                  <div class="err-detail-grid">
                    <div class="err-detail-section">
                      <div class="err-detail-label">Query</div>
                      <pre class="err-detail-pre err-detail-pre--query">{{ row.query }}</pre>
                    </div>
                  </div>
                  <div class="err-detail-meta">
                    <div v-if="row.exec_time || row.avg_ms" class="err-meta-chip slow-chip--time">
                      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
                      {{ row.exec_time || fmtMs(row.avg_ms) }}
                    </div>
                    <div v-if="row.lock_time" class="err-meta-chip">
                      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>
                      Lock: {{ row.lock_time }}
                    </div>
                    <div v-if="row.calls > 1" class="err-meta-chip">
                      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="17 1 21 5 17 9"/><path d="M3 11V9a4 4 0 0 1 4-4h14"/><polyline points="7 23 3 19 7 15"/><path d="M21 13v2a4 4 0 0 1-4 4H3"/></svg>
                      {{ row.calls }} executions
                    </div>
                    <div v-if="row.rows_sent" class="err-meta-chip">
                      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/><line x1="8" y1="18" x2="21" y2="18"/><line x1="3" y1="6" x2="3.01" y2="6"/><line x1="3" y1="12" x2="3.01" y2="12"/><line x1="3" y1="18" x2="3.01" y2="18"/></svg>
                      {{ row.rows_sent }} rows sent
                    </div>
                    <div v-if="row.rows_examined" class="err-meta-chip">
                      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
                      {{ row.rows_examined }} rows scanned
                    </div>
                    <div v-if="row.database" class="err-meta-chip">
                      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5v14c0 1.657 4.03 3 9 3s9-1.343 9-3V5"/></svg>
                      {{ row.database }}
                    </div>
                    <div v-if="row.username" class="err-meta-chip">
                      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>
                      {{ row.username }}
                    </div>
                    <div v-if="row.client_ip" class="err-meta-chip">
                      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/></svg>
                      {{ row.client_ip }}
                    </div>
                    <div v-if="row.start_time" class="err-meta-chip">
                      <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="4" width="18" height="18" rx="2"/><line x1="16" y1="2" x2="16" y2="6"/><line x1="8" y1="2" x2="8" y2="6"/><line x1="3" y1="10" x2="21" y2="10"/></svg>
                      {{ row.start_time }}
                    </div>
                  </div>
                </div>
              </Transition>
            </div>
          </div>

          <!-- Pagination -->
          <div class="err-pager">
            <span class="err-pager__info">
              {{ slowTotal.toLocaleString() }} total · page {{ slowPage }} of {{ slowTotalPages }}
              <span v-if="slowSource" class="err-pager__source">via {{ slowSource }}</span>
            </span>
            <div class="err-pager__btns">
              <button class="err-pager__btn" :disabled="slowPage <= 1" @click="slowGoPage(1)">«</button>
              <button class="err-pager__btn" :disabled="slowPage <= 1" @click="slowGoPage(slowPage - 1)">‹ Prev</button>
              <span class="err-pager__cur">{{ slowPage }}</span>
              <button class="err-pager__btn" :disabled="slowPage >= slowTotalPages" @click="slowGoPage(slowPage + 1)">Next ›</button>
              <button class="err-pager__btn" :disabled="slowPage >= slowTotalPages" @click="slowGoPage(slowTotalPages)">»</button>
            </div>
          </div>
        </template>
      </div>

      <!-- ── SQL AUDIT LOGS ──────────────────────────────────── -->
      <div v-else-if="activeTab === 'audit'" class="dblogs-panel">

        <!-- ── Cloud mode: audit log files ── -->
        <template v-if="cloudSupportsAudit">
          <div class="err-toolbar">
            <div class="err-toolbar__left">
              <div class="err-datepicker">
                <div class="err-shortcuts">
                  <button class="err-shortcut" :class="{ active: cloudAuditFrom === today() && cloudAuditTo === today() }" @click="cloudAuditFrom = today(); cloudAuditTo = today(); cloudAuditApply()">Today</button>
                  <button class="err-shortcut" :class="{ active: cloudAuditFrom === yesterday() && cloudAuditTo === yesterday() }" @click="cloudAuditFrom = yesterday(); cloudAuditTo = yesterday(); cloudAuditApply()">Yesterday</button>
                  <button class="err-shortcut" :class="{ active: !cloudAuditFrom && !cloudAuditTo }" @click="cloudAuditFrom = ''; cloudAuditTo = ''; cloudAuditApply()">7 days</button>
                </div>
                <div class="err-daterange">
                  <input v-model="cloudAuditFrom" type="date" class="err-date-input" @change="cloudAuditApply()" />
                  <span class="err-date-sep">→</span>
                  <input v-model="cloudAuditTo" type="date" class="err-date-input" @change="cloudAuditApply()" />
                </div>
              </div>
            </div>
            <div class="err-toolbar__right">
              <select v-model="cloudAuditLimit" class="err-select" @change="cloudAuditApply()">
                <option :value="10">10 / page</option>
                <option :value="25">25 / page</option>
                <option :value="50">50 / page</option>
              </select>
              <select
                v-if="cloudConfigured"
                v-model.number="selectedCloudConfigId"
                class="err-select cloud-instance-select"
                @change="activateCloudConfig"
              >
                <option v-for="cfg in cloudConfigs" :key="cfg.id" :value="cfg.id">
                  {{ cloudConfigLabel(cfg) }}
                </option>
              </select>
              <button class="err-cloud-btn err-cloud-btn--on" @click="openCloudConfigModal">
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 10h-1.26A8 8 0 1 0 9 20h9a5 5 0 0 0 0-10z"/></svg>
                {{ cloudButtonText }}
              </button>
            </div>
          </div>

          <div v-if="cloudAuditNotice" class="err-notice err-notice--error">
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
            <span>{{ cloudAuditNotice }}</span>
          </div>

          <!-- Skeleton -->
          <div v-if="cloudAuditLoading" class="err-skeleton-wrap">
            <div v-for="i in 6" :key="i" class="err-skeleton-row">
              <div class="err-skel" style="width:180px" />
              <div class="err-skel err-skel--msg" :style="{ width: (30 + i * 8) + '%' }" />
              <div class="err-skel" style="width:60px;flex-shrink:0" />
              <div class="err-skel" style="width:80px;flex-shrink:0" />
            </div>
          </div>

          <!-- Empty -->
          <div v-else-if="!cloudAuditFiles.length" class="err-empty">
            <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.2" opacity=".3"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
            <p>No audit log files found</p>
            <span>Adjust the date range or ensure SQL audit is enabled on this instance</span>
          </div>

          <!-- File list -->
          <template v-else>
            <div class="audit-file-list">
              <!-- Header -->
              <div class="audit-file-header">
                <span class="audit-col audit-col--time">Time Range</span>
                <span class="audit-col audit-col--name">File</span>
                <span class="audit-col audit-col--size">Size</span>
                <span class="audit-col audit-col--action"></span>
              </div>
              <div v-for="file in cloudAuditFiles" :key="file.id" class="audit-file-row">
                <div class="audit-col audit-col--time">
                  <div class="audit-time-range">
                    <span class="audit-time-from">{{ fmtLogTime(file.begin_time) }}</span>
                    <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" opacity=".4"><line x1="5" y1="12" x2="19" y2="12"/><polyline points="12 5 19 12 12 19"/></svg>
                    <span class="audit-time-to">{{ fmtLogTime(file.end_time) }}</span>
                  </div>
                </div>
                <div class="audit-col audit-col--name">
                  <span class="audit-filename" :title="file.name">{{ file.name.split('/').pop() }}</span>
                </div>
                <div class="audit-col audit-col--size">
                  <span class="audit-size">{{ fmtKB(file.size) }}</span>
                </div>
                <div class="audit-col audit-col--action">
                  <button
                    class="audit-dl-btn"
                    :disabled="file.downloading"
                    @click="downloadAuditFile(file)"
                    title="Get 5-minute download link"
                  >
                    <svg v-if="!file.downloading" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
                    <svg v-else class="spin" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
                    {{ file.downloading ? 'Getting link…' : 'Download' }}
                  </button>
                </div>
              </div>
            </div>

            <!-- Pagination -->
            <div class="err-pager">
              <span class="err-pager__info">
                {{ cloudAuditTotal.toLocaleString() }} files
                <span class="err-pager__source">via {{ cloudAuditSource }}</span>
              </span>
              <div class="err-pager__btns">
                <button class="err-pager__btn" :disabled="cloudAuditPage === 0" @click="cloudAuditGoPage(0)">«</button>
                <button class="err-pager__btn" :disabled="cloudAuditPage === 0" @click="cloudAuditGoPage(Math.max(0, cloudAuditPage - cloudAuditLimit))">‹ Prev</button>
                <span class="err-pager__cur">{{ Math.floor(cloudAuditPage / cloudAuditLimit) + 1 }}</span>
                <button class="err-pager__btn" :disabled="cloudAuditPage + cloudAuditLimit >= cloudAuditTotal" @click="cloudAuditGoPage(cloudAuditPage + cloudAuditLimit)">Next ›</button>
              </div>
            </div>
          </template>
        </template>

        <!-- ── Local mode: query history ── -->
        <template v-else>
          <div class="err-toolbar">
            <div class="err-toolbar__left">
              <input v-model="auditSearch" type="text" placeholder="Filter by SQL…" class="err-date-input" style="width:200px" @keydown.enter="auditGoPage(1); loadAuditLogs()" />
              <button class="dblogs-btn" @click="auditGoPage(1); loadAuditLogs()">
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
                Refresh
              </button>
            </div>
            <div class="err-toolbar__right">
              <select v-model="auditLimit" class="err-select" @change="auditGoPage(1); loadAuditLogs()">
                <option :value="25">25 / page</option>
                <option :value="50">50 / page</option>
                <option :value="100">100 / page</option>
              </select>
              <select
                v-if="cloudConfigured"
                v-model.number="selectedCloudConfigId"
                class="err-select cloud-instance-select"
                @change="activateCloudConfig"
              >
                <option v-for="cfg in cloudConfigs" :key="cfg.id" :value="cfg.id">
                  {{ cloudConfigLabel(cfg) }}
                </option>
              </select>
              <button
                class="err-cloud-btn" :class="{ 'err-cloud-btn--on': cloudConfigured }"
                @click="openCloudConfigModal"
                :title="cloudConfigured ? 'Cloud source active — click to reconfigure' : 'Connect cloud provider'"
              >
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 10h-1.26A8 8 0 1 0 9 20h9a5 5 0 0 0 0-10z"/></svg>
                {{ cloudButtonText }}
              </button>
            </div>
          </div>

          <div v-if="cloudConfigured && !cloudSupportsAudit" class="err-notice err-notice--warn">
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
            <span>{{ cloudProviderShort }} audit logs are not available in this view. Showing local SQL history.</span>
          </div>

          <div v-if="auditNotice" class="err-notice err-notice--warn">
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
            <span>{{ auditNotice }}</span>
          </div>

          <div v-if="auditLoading" class="err-skeleton-wrap">
            <div v-for="i in 8" :key="i" class="err-skeleton-row">
              <div class="err-skel err-skel--time" />
              <div class="err-skel err-skel--msg" :style="{ width: (40 + (i * 7) % 40) + '%' }" />
              <div class="err-skel" style="width:60px;flex-shrink:0" />
              <div class="err-skel err-skel--badge" />
            </div>
          </div>

          <div v-else-if="!auditRows.length" class="err-empty">
            <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.2" opacity=".3"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="8" y1="13" x2="16" y2="13"/><line x1="8" y1="17" x2="12" y2="17"/></svg>
            <p>No audit log entries found</p>
          </div>

          <template v-else>
            <div class="err-list audit-list">
              <div
                v-for="(row, i) in auditRows" :key="i"
                class="err-row" :class="[row.status === 'error' ? 'err-row--error' : 'err-row--log', { 'err-row--open': selectedAudit === row }]"
                @click="selectedAudit = selectedAudit === row ? null : row"
              >
                <div class="err-row__summary">
                  <div class="err-row__left">
                    <span class="err-row__badge" :class="row.status === 'error' ? 'badge--error' : 'badge--log'">{{ row.status || 'ok' }}</span>
                    <span class="err-row__time">{{ fmtLogTime(row.executed_at) }}</span>
                    <span v-if="row.duration_ms != null" class="slow-time" :class="{ 'slow-time--warn': row.duration_ms > 5000, 'slow-time--crit': row.duration_ms > 30000 }">
                      {{ fmtMs(row.duration_ms) }}
                    </span>
                    <span v-if="row.rows_affected != null" class="slow-calls">{{ row.rows_affected }} rows</span>
                  </div>
                  <div class="err-row__msg">{{ truncate(row.sql, 180) }}</div>
                  <svg class="err-row__chevron" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="6 9 12 15 18 9"/></svg>
                </div>
                <Transition name="err-expand">
                  <div v-if="selectedAudit === row" class="err-row__detail" @click.stop>
                    <div class="err-detail-grid">
                      <div class="err-detail-section">
                        <div class="err-detail-label">SQL</div>
                        <pre class="err-detail-pre err-detail-pre--query">{{ row.sql }}</pre>
                      </div>
                      <div v-if="row.error" class="err-detail-section">
                        <div class="err-detail-label">Error</div>
                        <pre class="err-detail-pre" style="color:#ef4444">{{ row.error }}</pre>
                      </div>
                    </div>
                    <div class="err-detail-meta">
                      <div v-if="row.executed_at" class="err-meta-chip">
                        <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
                        {{ row.executed_at }}
                      </div>
                      <div v-if="row.duration_ms != null" class="err-meta-chip slow-chip--time">
                        <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
                        {{ fmtMs(row.duration_ms) }}
                      </div>
                      <div v-if="row.rows_affected != null" class="err-meta-chip">
                        <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/><line x1="8" y1="18" x2="21" y2="18"/></svg>
                        {{ row.rows_affected }} rows affected
                      </div>
                    </div>
                  </div>
                </Transition>
              </div>
            </div>

            <div class="err-pager">
              <span class="err-pager__info">{{ auditTotal.toLocaleString() }} total · page {{ auditPage }} of {{ auditTotalPages }}</span>
              <div class="err-pager__btns">
                <button class="err-pager__btn" :disabled="auditPage <= 1" @click="auditGoPage(1)">«</button>
                <button class="err-pager__btn" :disabled="auditPage <= 1" @click="auditGoPage(auditPage - 1)">‹ Prev</button>
                <span class="err-pager__cur">{{ auditPage }}</span>
                <button class="err-pager__btn" :disabled="auditPage >= auditTotalPages" @click="auditGoPage(auditPage + 1)">Next ›</button>
                <button class="err-pager__btn" :disabled="auditPage >= auditTotalPages" @click="auditGoPage(auditTotalPages)">»</button>
              </div>
            </div>
          </template>
        </template>
      </div>
    </template>
  </div>

  <!-- ── Cloud Provider Config Modal ─────────────────────────────────── -->
  <Teleport to="body">
    <Transition name="modal-fade">
      <div v-if="showCloudModal" class="cloud-modal-backdrop" @click.self="showCloudModal = false">
        <div class="cloud-modal">
          <div class="cloud-modal__header">
            <div class="cloud-modal__title">
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 10h-1.26A8 8 0 1 0 9 20h9a5 5 0 0 0 0-10z"/></svg>
              Cloud Provider — RDS Log Access
            </div>
            <button class="cloud-modal__close" @click="showCloudModal = false">✕</button>
          </div>

          <div class="cloud-modal__body">
            <p class="cloud-modal__desc">
              Save one or more cloud RDS instances, then switch the active instance from the DB Logs toolbar.
              Credentials are stored on the server and never exposed to the browser.
            </p>

            <div class="cloud-form">
              <div class="cloud-form__row">
                <label>Display Name <span class="cloud-hint">shown in the instance selector</span></label>
                <input v-model="cloudForm.name" class="dblogs-input" placeholder="Production Alibaba RDS" />
              </div>
              <div class="cloud-form__row">
                <label>Provider</label>
                <select v-model="cloudForm.provider" class="dblogs-select">
                  <option value="huawei">Huawei Cloud RDS</option>
                  <option value="alibaba">Alibaba Cloud RDS</option>
                </select>
              </div>
              <div class="cloud-form__row">
                <label>Region <span class="cloud-hint">e.g. ap-southeast-3 / cn-hangzhou</span></label>
                <input v-model="cloudForm.region" class="dblogs-input" :placeholder="cloudRegionPlaceholder" />
              </div>
              <div v-if="cloudProvider === 'huawei'" class="cloud-form__row">
                <label>Project ID <span class="cloud-hint">IAM → My Credentials</span></label>
                <input v-model="cloudForm.project_id" class="dblogs-input" placeholder="0b0e9c5b4f00d5a90f4c..." />
              </div>
              <div class="cloud-form__row">
                <label>Instance ID <span class="cloud-hint">RDS → Instance → Overview</span></label>
                <input v-model="cloudForm.instance_id" class="dblogs-input" :placeholder="cloudInstancePlaceholder" />
              </div>
              <div class="cloud-form__row">
                <label>Access Key (AK) <span class="cloud-hint">IAM → Access Keys</span></label>
                <input v-model="cloudForm.access_key" class="dblogs-input" placeholder="XXXXXXXXXXXXXXXXXXXX" />
              </div>
              <div class="cloud-form__row">
                <label>Secret Key (SK) <span class="cloud-hint">{{ cloudConfigured ? 'Leave blank to keep existing' : 'Shown once when created' }}</span></label>
                <input v-model="cloudForm.secret_key" type="password" class="dblogs-input" placeholder="Enter secret key" />
              </div>
            </div>

            <div class="cloud-form__help">
              <strong>Where to find these:</strong>
              <ol v-if="cloudProvider === 'huawei'">
                <li>Huawei Console → top-right → <strong>My Credentials</strong> → Project List → copy <strong>Project ID</strong></li>
                <li>Same page → <strong>Access Keys</strong> → Add Access Key → download AK/SK</li>
                <li>RDS → your instance → <strong>Basic Information</strong> → copy <strong>Instance ID</strong></li>
                <li>Region code is in the browser URL: <code>console.huaweicloud.com/<strong>ap-southeast-3</strong>/rds</code></li>
              </ol>
              <ol v-else>
                <li>Alibaba Cloud Console → RAM → <strong>AccessKey</strong> → create/copy AK/SK</li>
                <li>ApsaraDB RDS → your instance → <strong>Instance ID</strong> (example: <code>rm-xxxx</code>)</li>
                <li>RegionId is your region code (example: <code>cn-hangzhou</code>, <code>ap-southeast-1</code>)</li>
              </ol>
            </div>
          </div>

          <div class="cloud-modal__footer">
            <button v-if="cloudForm.id" class="dblogs-btn dblogs-btn--danger" @click="deleteCloudConfig">
              Remove Instance
            </button>
            <button v-if="cloudConfigured" class="dblogs-btn dblogs-btn--ghost" @click="newCloudConfig">
              New Instance
            </button>
            <div style="flex:1" />
            <button class="dblogs-btn dblogs-btn--ghost" @click="showCloudModal = false">Cancel</button>
            <button class="dblogs-btn dblogs-btn--primary" :disabled="cloudSaving" @click="saveCloudConfig">
              <svg v-if="cloudSaving" class="spin" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
              {{ cloudSaving ? 'Saving…' : cloudForm.id ? 'Update & Switch' : 'Save & Switch' }}
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.dblogs {
  padding: 24px 28px 48px;
  max-width: 1400px;
  font-family: var(--font);
  color: var(--text);
}

/* Header */
.dblogs-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 20px;
}
.dblogs-header__tag {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: .08em;
  color: var(--text-secondary);
  text-transform: uppercase;
  margin-bottom: 4px;
}
.dblogs-header__title {
  font-size: 22px;
  font-weight: 700;
  margin: 0 0 4px;
  color: var(--text);
}
.dblogs-header__sub {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 0;
}
/* Tabs */
.dblogs-tabs {
  display: flex;
  gap: 2px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 16px;
}
.dblogs-tab {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-secondary);
  background: none;
  border: none;
  border-bottom: 2px solid transparent;
  cursor: pointer;
  transition: color .15s, border-color .15s;
  margin-bottom: -1px;
}
.dblogs-tab:hover { color: var(--text); }
.dblogs-tab.active { color: var(--accent); border-bottom-color: var(--accent); }

/* Toolbar */
.dblogs-toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  padding: 10px 14px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 8px;
  margin-bottom: 10px;
}
.dblogs-toolbar__group {
  display: flex;
  align-items: center;
  gap: 6px;
}
.dblogs-toolbar__spacer { flex: 1; }
.dblogs-label {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: .05em;
  color: var(--text-secondary);
  white-space: nowrap;
}
.dblogs-input {
  font-size: 12.5px;
  padding: 4px 8px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg);
  color: var(--text);
  width: 120px;
}
.dblogs-input--sm { width: 90px; }
.dblogs-select-sm {
  font-size: 12.5px;
  padding: 4px 8px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg);
  color: var(--text);
}

.dblogs-pills {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
}
.dblogs-pill {
  font-size: 11px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 4px;
  border: 1px solid transparent;
  cursor: pointer;
  opacity: .5;
  transition: opacity .15s;
  background: var(--surface);
}
.dblogs-pill.active { opacity: 1; }
.dblogs-pill.sev-fatal  { border-color: #7f1d1d; color: #ef4444; background: rgba(239,68,68,.08); }
.dblogs-pill.sev-error  { border-color: #991b1b; color: #f97316; background: rgba(249,115,22,.08); }
.dblogs-pill.sev-warning{ border-color: #92400e; color: #f59e0b; background: rgba(245,158,11,.08); }
.dblogs-pill.sev-context{ border-color: var(--border); color: var(--text-secondary); }
.dblogs-pill.sev-stmt   { border-color: var(--border); color: var(--accent); background: rgba(99,102,241,.06); }
.dblogs-pill.sev-info   { border-color: var(--border); color: var(--text-secondary); }

.dblogs-threshold {
  display: flex;
  align-items: center;
  gap: 8px;
}
.dblogs-slider { width: 140px; cursor: pointer; accent-color: var(--accent); }
.dblogs-threshold__val { font-size: 12px; font-weight: 600; min-width: 48px; color: var(--accent); }

.dblogs-shortcuts {
  display: flex;
  gap: 6px;
  margin-bottom: 10px;
}
.dblogs-shortcut {
  font-size: 11.5px;
  padding: 3px 10px;
  border-radius: 5px;
  border: 1px solid var(--border);
  background: var(--surface);
  color: var(--text-secondary);
  cursor: pointer;
  transition: background .15s, color .15s;
}
.dblogs-shortcut:hover { background: var(--accent); color: #fff; border-color: var(--accent); }

.dblogs-btn {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 5px 12px;
  font-size: 12.5px;
  font-weight: 600;
  background: var(--accent);
  color: #fff;
  border: 1px solid transparent;
  border-radius: 6px;
  cursor: pointer;
  transition: opacity .15s;
}
.dblogs-btn:hover { opacity: .88; }

.dblogs-source {
  font-size: 11px;
  color: var(--text-secondary);
  font-style: italic;
}

/* States */
.dblogs-notice {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  font-size: 13px;
  color: var(--text-secondary);
  background: color-mix(in srgb, var(--accent) 8%, transparent);
  border: 1px solid color-mix(in srgb, var(--accent) 25%, transparent);
  border-radius: 8px;
  padding: 12px 14px;
  margin-bottom: 12px;
}
.dblogs-loading, .dblogs-empty-sm {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--text-secondary);
  padding: 32px 20px;
}
.dblogs-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 60px 20px;
  color: var(--text-secondary);
  text-align: center;
}
.dblogs-empty svg { opacity: .4; }

/* Table */
.dblogs-table-wrap {
  overflow-x: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
  margin-bottom: 0;
}
.dblogs-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12.5px;
}
.dblogs-table thead th {
  padding: 8px 12px;
  text-align: left;
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .05em;
  color: var(--text-secondary);
  background: var(--surface);
  border-bottom: 1px solid var(--border);
  white-space: nowrap;
  position: sticky;
  top: 0;
  z-index: 1;
}
.dblogs-table tbody tr {
  border-bottom: 1px solid color-mix(in srgb, var(--border) 50%, transparent);
  cursor: pointer;
  transition: background .1s;
}
.dblogs-table tbody tr:last-child { border-bottom: none; }
.dblogs-table tbody tr:hover { background: color-mix(in srgb, var(--accent) 5%, transparent); }
.dblogs-table tbody tr.selected { background: color-mix(in srgb, var(--accent) 10%, transparent); }
.dblogs-table td { padding: 7px 12px; vertical-align: top; }

.dblogs-mono { font-family: var(--mono); font-size: 11.5px; white-space: nowrap; }
.dblogs-num { font-family: var(--mono); font-size: 12px; text-align: right; white-space: nowrap; }
.dblogs-muted-cell { color: var(--text-secondary); font-size: 12px; }
.dblogs-msg { max-width: 600px; word-break: break-word; line-height: 1.45; }
.dblogs-query-cell {
  max-width: 480px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--mono);
  font-size: 11.5px;
  color: var(--text-secondary);
}
.dblogs-warn { color: #f59e0b; }
.dblogs-critical { color: #ef4444; }

/* Badges */
.dblogs-badge {
  display: inline-block;
  font-size: 10.5px;
  font-weight: 700;
  padding: 1px 7px;
  border-radius: 4px;
  text-transform: uppercase;
  letter-spacing: .04em;
  white-space: nowrap;
}
.sev-fatal  { background: rgba(239,68,68,.15);  color: #ef4444; }
.sev-error  { background: rgba(249,115,22,.15); color: #f97316; }
.sev-warning{ background: rgba(245,158,11,.15); color: #d97706; }
.sev-context{ background: var(--surface); color: var(--text-secondary); border: 1px solid var(--border); }
.sev-stmt   { background: rgba(99,102,241,.1); color: var(--accent); }
.sev-info   { background: rgba(16,185,129,.1); color: #10b981; }
.stmt-select { background: rgba(16,185,129,.12); color: #10b981; }
.stmt-insert { background: rgba(99,102,241,.12); color: var(--accent); }
.stmt-update { background: rgba(245,158,11,.12); color: #d97706; }
.stmt-delete { background: rgba(239,68,68,.12);  color: #ef4444; }
.stmt-other  { background: var(--surface); color: var(--text-secondary); border: 1px solid var(--border); }

/* Detail panel */
.dblogs-detail {
  border: 1px solid var(--border);
  border-top: none;
  border-radius: 0 0 8px 8px;
  background: var(--surface);
}
.dblogs-detail__header {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
}
.dblogs-detail__time {
  flex: 1;
  font-size: 12px;
  color: var(--text-secondary);
  font-family: var(--mono);
}
.dblogs-detail__close {
  font-size: 14px;
  background: none;
  border: none;
  color: var(--text-secondary);
  cursor: pointer;
  line-height: 1;
  padding: 2px 6px;
  border-radius: 4px;
}
.dblogs-detail__close:hover { background: var(--border); }
.dblogs-detail__body { padding: 14px; display: flex; flex-direction: column; gap: 12px; }
.dblogs-detail__section { display: flex; flex-direction: column; gap: 4px; }
.dblogs-detail__section-label {
  font-size: 10.5px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .06em;
  color: var(--text-secondary);
}
.dblogs-detail__pre {
  margin: 0;
  font-family: var(--mono);
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 10px 12px;
  max-height: 280px;
  overflow-y: auto;
}
.dblogs-hint { color: #10b981; }
.dblogs-query { color: var(--accent); }
.dblogs-detail__meta {
  display: flex;
  flex-wrap: wrap;
  gap: 10px 20px;
  font-size: 12px;
  color: var(--text-secondary);
  padding-top: 4px;
}

/* Pagination */
.dblogs-pager {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  border: 1px solid var(--border);
  border-top: none;
  border-radius: 0 0 8px 8px;
  background: var(--surface);
}
.dblogs-pager__info { font-size: 12px; color: var(--text-secondary); flex: 1; }
.dblogs-pager__btn {
  font-size: 12.5px;
  padding: 4px 12px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg);
  color: var(--text);
  cursor: pointer;
  transition: background .1s;
}
.dblogs-pager__btn:hover:not(:disabled) { background: var(--accent); color: #fff; border-color: var(--accent); }
.dblogs-pager__btn:disabled { opacity: .4; cursor: not-allowed; }

.dblogs-muted { color: var(--text-secondary); font-size: 12.5px; }

/* Spin */
@keyframes spin { to { transform: rotate(360deg); } }
.spin { animation: spin .8s linear infinite; }

/* Cloud button variant */
.dblogs-btn--outline { background: transparent; color: var(--text-secondary); border: 1px solid var(--border); }
.dblogs-btn--outline:hover { background: var(--bg); color: var(--text-primary); border-color: var(--text-secondary); }
.dblogs-btn--cloud { background: #16a34a; color: #fff; border-color: #16a34a; }
.dblogs-btn--cloud:hover { background: #15803d; }
.dblogs-btn--primary { background: var(--accent); color: #fff; border-color: var(--accent); }
.dblogs-btn--primary:hover { opacity: .9; }
.dblogs-btn--ghost { background: var(--bg-surface); color: var(--text-secondary); border-color: var(--border); }
.dblogs-btn--ghost:hover { background: var(--bg-body); color: var(--text-primary); }
.dblogs-btn--danger { background: transparent; color: #ef4444; border-color: #ef4444; }
.dblogs-btn--danger:hover { background: rgba(239,68,68,.08); }

/* Cloud Modal */
.cloud-modal-backdrop {
  position: fixed; inset: 0; z-index: 9000;
  background: rgba(0,0,0,.45);
  display: flex; align-items: center; justify-content: center;
}
.cloud-modal {
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: 12px;
  width: 520px; max-width: 95vw;
  max-height: 90vh;
  display: flex; flex-direction: column;
  box-shadow: 0 20px 60px rgba(0,0,0,.2);
}
.cloud-modal__header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 16px 20px;
  border-bottom: 1px solid var(--border);
}
.cloud-modal__title {
  display: flex; align-items: center; gap: 8px;
  font-size: 15px; font-weight: 700; color: var(--text-primary);
}
.cloud-modal__close {
  background: none; border: none; cursor: pointer;
  color: var(--text-secondary); font-size: 16px; padding: 4px 8px;
  border-radius: 4px;
}
.cloud-modal__close:hover { background: var(--bg-body); color: var(--text-primary); }
.cloud-modal__body {
  padding: 20px; overflow-y: auto; flex: 1;
  display: flex; flex-direction: column; gap: 16px;
}
.cloud-modal__desc {
  font-size: 13px; color: var(--text-secondary); line-height: 1.6; margin: 0;
}
.cloud-modal__footer {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  padding: 14px 20px;
  border-top: 1px solid var(--border);
}
.cloud-form { display: flex; flex-direction: column; gap: 12px; }
.cloud-form__row {
  display: grid; grid-template-columns: 180px 1fr; align-items: start; gap: 10px;
}
.cloud-form__row label {
  font-size: 13px; font-weight: 600; color: var(--text-primary);
  display: flex; flex-direction: column; padding-top: 7px;
}
.cloud-hint { font-size: 11px; font-weight: 400; color: var(--text-secondary); margin-top: 2px; }
.cloud-form__row input,
.cloud-form__row select {
  width: 100%;
  background: var(--bg-body);
  border: 1px solid var(--border);
  color: var(--text-primary);
  border-radius: 6px;
  padding: 7px 10px;
  font-size: 13px;
  outline: none;
  box-sizing: border-box;
}
.cloud-form__row input:focus,
.cloud-form__row select:focus {
  border-color: var(--accent);
  box-shadow: 0 0 0 2px rgba(99,102,241,.15);
}
.cloud-form__row input::placeholder { color: var(--text-secondary); opacity: .6; }
.cloud-form__help {
  background: var(--bg-body); border: 1px solid var(--border);
  border-radius: 8px; padding: 14px 16px;
  font-size: 12.5px; color: var(--text-secondary); line-height: 1.7;
}
.cloud-form__help strong { color: var(--text-primary); }
.cloud-form__help ol { margin: 8px 0 0 16px; padding: 0; }
.cloud-form__help code {
  background: var(--bg-elevated); border: 1px solid var(--border);
  color: var(--accent); padding: 1px 5px; border-radius: 3px;
  font-family: var(--mono); font-size: 11.5px;
}

/* Modal transition */
.modal-fade-enter-active, .modal-fade-leave-active { transition: opacity .18s ease; }
.modal-fade-enter-from, .modal-fade-leave-to { opacity: 0; }

/* Slow query extras */
.slow-list { max-height: calc(100vh - 360px); overflow-y: auto; overflow-x: hidden; }
.slow-row--select  { border-left: 3px solid rgba(16,185,129,.5); }
.slow-row--insert  { border-left: 3px solid rgba(99,102,241,.5); }
.slow-row--update  { border-left: 3px solid rgba(245,158,11,.5); }
.slow-row--delete  { border-left: 3px solid rgba(239,68,68,.5); }
.slow-row--create  { border-left: 3px solid rgba(139,92,246,.5); }
.slow-row--other   { border-left: 3px solid rgba(148,163,184,.3); }

.slow-time {
  font-size: 12px; font-weight: 700; font-family: var(--mono);
  color: #10b981; white-space: nowrap;
}
.slow-time--warn { color: #d97706; }
.slow-time--crit { color: #ef4444; }

.slow-calls {
  font-size: 11px; font-weight: 600; padding: 1px 6px;
  border-radius: 10px; background: var(--surface);
  border: 1px solid var(--border); color: var(--text-secondary);
  white-space: nowrap;
}

.slow-chip--time { color: var(--accent); border-color: rgba(99,102,241,.3); background: rgba(99,102,241,.06); }
.err-detail-pre--query { color: var(--accent); }

/* Audit list (local) */
.audit-list { max-height: calc(100vh - 360px); overflow-y: auto; overflow-x: hidden; }

/* Audit file list (cloud) */
.audit-file-list {
  border: 1px solid var(--border);
  border-radius: 10px 10px 0 0;
  overflow: hidden;
  max-height: calc(100vh - 360px);
  overflow-y: auto;
}
.audit-file-header {
  display: flex; align-items: center; gap: 12px;
  padding: 8px 16px;
  background: var(--surface);
  border-bottom: 1px solid var(--border);
  position: sticky; top: 0; z-index: 1;
}
.audit-file-row {
  display: flex; align-items: center; gap: 12px;
  padding: 11px 16px;
  border-bottom: 1px solid color-mix(in srgb, var(--border) 50%, transparent);
  transition: background .1s;
}
.audit-file-row:last-child { border-bottom: none; }
.audit-file-row:hover { background: color-mix(in srgb, var(--accent) 4%, transparent); }

.audit-col { display: flex; align-items: center; }
.audit-col--time  { width: 260px; flex-shrink: 0; }
.audit-col--name  { flex: 1; min-width: 0; }
.audit-col--size  { width: 80px; flex-shrink: 0; justify-content: flex-end; }
.audit-col--action { width: 110px; flex-shrink: 0; justify-content: flex-end; }

.audit-file-header .audit-col {
  font-size: 10.5px; font-weight: 700; letter-spacing: .06em;
  text-transform: uppercase; color: var(--text-secondary);
}

.audit-time-range { display: flex; align-items: center; gap: 6px; }
.audit-time-from, .audit-time-to {
  font-size: 12px; font-family: var(--mono); color: var(--text);
}
.audit-filename {
  font-size: 11.5px; font-family: var(--mono); color: var(--text-secondary);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.audit-size {
  font-size: 12px; font-family: var(--mono);
  color: var(--text-secondary); white-space: nowrap;
}
.audit-dl-btn {
  display: flex; align-items: center; gap: 5px;
  font-size: 12px; font-weight: 600; padding: 5px 12px;
  border-radius: 6px; border: 1px solid var(--accent);
  background: transparent; color: var(--accent);
  cursor: pointer; transition: all .15s; white-space: nowrap;
}
.audit-dl-btn:hover:not(:disabled) { background: var(--accent); color: #fff; }
.audit-dl-btn:disabled { opacity: .5; cursor: not-allowed; }

/* ── Error Log Redesign ──────────────────────────────────────────── */

.err-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  padding: 10px 14px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 10px;
  margin-bottom: 12px;
}
.err-toolbar__left { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; flex: 1; }
.err-toolbar__right { display: flex; align-items: center; justify-content: flex-end; gap: 8px; flex-wrap: wrap; }

.err-levels { display: flex; gap: 4px; }
.err-lvl-btn {
  font-size: 10.5px; font-weight: 700; letter-spacing: .04em;
  padding: 3px 9px; border-radius: 20px;
  border: 1px solid transparent; cursor: pointer;
  opacity: .4; transition: opacity .15s, transform .1s;
  background: var(--surface); color: var(--text-secondary);
}
.err-lvl-btn:hover { opacity: .75; }
.err-lvl-btn.active { opacity: 1; transform: none; }
.lvl--error.active   { background: rgba(239,68,68,.12);  color: #ef4444; border-color: rgba(239,68,68,.3); }
.lvl--fatal.active   { background: rgba(220,38,38,.12);  color: #dc2626; border-color: rgba(220,38,38,.3); }
.lvl--warning.active { background: rgba(245,158,11,.12); color: #d97706; border-color: rgba(245,158,11,.3); }
.lvl--context.active { background: var(--bg); color: var(--text-secondary); border-color: var(--border); }
.lvl--statement.active { background: rgba(99,102,241,.1); color: var(--accent); border-color: rgba(99,102,241,.3); }
.lvl--log.active     { background: rgba(16,185,129,.1); color: #10b981; border-color: rgba(16,185,129,.3); }
.stmt--select.active { background: rgba(16,185,129,.12); color: #10b981; border-color: rgba(16,185,129,.3); }
.stmt--insert.active { background: rgba(99,102,241,.12); color: var(--accent); border-color: rgba(99,102,241,.3); }
.stmt--update.active { background: rgba(245,158,11,.12); color: #d97706; border-color: rgba(245,158,11,.3); }
.stmt--delete.active { background: rgba(239,68,68,.12);  color: #ef4444; border-color: rgba(239,68,68,.3); }
.stmt--create.active { background: rgba(139,92,246,.12); color: #8b5cf6; border-color: rgba(139,92,246,.3); }

.err-datepicker { display: flex; align-items: center; gap: 8px; }
.err-shortcuts { display: flex; gap: 3px; }
.err-shortcut {
  font-size: 11px; font-weight: 500; padding: 3px 10px;
  border-radius: 5px; border: 1px solid var(--border);
  background: var(--bg); color: var(--text-secondary);
  cursor: pointer; transition: all .15s;
}
.err-shortcut:hover, .err-shortcut.active {
  background: var(--accent); color: #fff; border-color: var(--accent);
}
.err-daterange { display: flex; align-items: center; gap: 5px; }
.err-date-input {
  font-size: 12px; padding: 4px 8px;
  border-radius: 6px; border: 1px solid var(--border);
  background: var(--bg); color: var(--text); width: 120px;
}
.err-date-sep { font-size: 12px; color: var(--text-secondary); }

.err-select {
  font-size: 12px; padding: 5px 8px;
  border-radius: 6px; border: 1px solid var(--border);
  background: var(--bg); color: var(--text); cursor: pointer;
}
.cloud-instance-select {
  max-width: 260px;
  min-width: 180px;
}
.err-cloud-btn {
  display: flex; align-items: center; gap: 5px;
  font-size: 12px; font-weight: 600; padding: 5px 12px;
  border-radius: 6px; border: 1px solid var(--border);
  background: var(--bg); color: var(--text-secondary);
  cursor: pointer; transition: all .15s; white-space: nowrap;
}
.err-cloud-btn:hover { border-color: var(--accent); color: var(--accent); }
.err-cloud-btn--on {
  background: rgba(22,163,74,.1); color: #16a34a;
  border-color: rgba(22,163,74,.35);
}
.err-cloud-btn--on:hover { background: rgba(22,163,74,.18); }

/* Notices */
.err-notice {
  display: flex; align-items: center; gap: 8px;
  font-size: 12.5px; padding: 10px 14px;
  border-radius: 8px; margin-bottom: 10px;
  border: 1px solid;
}
.err-notice--warn {
  background: rgba(245,158,11,.07);
  border-color: rgba(245,158,11,.25);
  color: #d97706;
}
.err-notice--error {
  background: rgba(239,68,68,.07);
  border-color: rgba(239,68,68,.25);
  color: #ef4444;
}
.err-notice span { flex: 1; }
.err-inline-btn {
  display: flex; align-items: center; gap: 5px;
  font-size: 11.5px; font-weight: 600; padding: 4px 10px;
  border-radius: 5px; border: 1px solid currentColor;
  background: transparent; color: inherit; cursor: pointer;
  white-space: nowrap; transition: background .15s;
}
.err-inline-btn:hover { background: rgba(255,255,255,.08); }

/* Skeleton */
.err-skeleton-wrap { display: flex; flex-direction: column; gap: 1px; }
.err-skeleton-row {
  display: flex; align-items: center; gap: 12px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border);
}
.err-skel {
  height: 10px; border-radius: 5px;
  background: linear-gradient(90deg, var(--border) 25%, color-mix(in srgb, var(--border) 50%, transparent) 50%, var(--border) 75%);
  background-size: 200% 100%;
  animation: shimmer 1.4s infinite;
}
.err-skel--time  { width: 80px; flex-shrink: 0; }
.err-skel--badge { width: 52px; height: 18px; border-radius: 10px; flex-shrink: 0; }
.err-skel--db    { width: 90px; flex-shrink: 0; }
.err-skel--msg   { flex: 1; }
@keyframes shimmer { to { background-position: -200% 0; } }

/* Empty */
.err-empty {
  display: flex; flex-direction: column; align-items: center;
  gap: 6px; padding: 60px 20px; color: var(--text-secondary); text-align: center;
}
.err-empty p { font-size: 14px; font-weight: 600; margin: 0; color: var(--text); }
.err-empty span { font-size: 12.5px; }

/* Log list */
.err-list {
  border: 1px solid var(--border);
  border-radius: 10px 10px 0 0;
  overflow-y: auto;
  overflow-x: hidden;
  max-height: calc(100vh - 360px);
  margin-bottom: 0;
}
.err-row {
  border-bottom: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
  cursor: pointer;
  transition: background .1s;
}
.err-row:last-child { border-bottom: none; }
.err-row:hover { background: color-mix(in srgb, var(--accent) 4%, transparent); }
.err-row--open { background: color-mix(in srgb, var(--accent) 5%, transparent); }

/* Left accent bar by severity */
.err-row--error   { border-left: 3px solid rgba(239,68,68,.5); }
.err-row--fatal   { border-left: 3px solid rgba(220,38,38,.7); }
.err-row--warning { border-left: 3px solid rgba(245,158,11,.5); }
.err-row--statement { border-left: 3px solid rgba(99,102,241,.4); }
.err-row--context { border-left: 3px solid rgba(148,163,184,.3); }
.err-row--log     { border-left: 3px solid rgba(16,185,129,.4); }
.err-row--info    { border-left: 3px solid rgba(16,185,129,.4); }

.err-row__summary {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 14px 10px 12px;
}
.err-row__left { display: flex; align-items: center; gap: 7px; flex-shrink: 0; }
.err-row__msg {
  flex: 1; font-size: 12.5px; font-family: var(--mono);
  color: var(--text); white-space: nowrap;
  overflow: hidden; text-overflow: ellipsis; min-width: 0;
}
.err-row__chevron {
  flex-shrink: 0; color: var(--text-secondary);
  transition: transform .2s; opacity: .5;
}
.err-row--open .err-row__chevron { transform: rotate(180deg); opacity: 1; }

.err-row__badge {
  display: inline-flex; align-items: center;
  font-size: 10px; font-weight: 800; letter-spacing: .06em;
  padding: 2px 7px; border-radius: 20px; white-space: nowrap;
  text-transform: uppercase;
}
.badge--error     { background: rgba(239,68,68,.12);  color: #ef4444; }
.badge--fatal     { background: rgba(220,38,38,.15);  color: #dc2626; }
.badge--warning   { background: rgba(245,158,11,.12); color: #d97706; }
.badge--statement { background: rgba(99,102,241,.1);  color: var(--accent); }
.badge--context   { background: var(--surface); color: var(--text-secondary); border: 1px solid var(--border); }
.badge--log       { background: rgba(16,185,129,.1);  color: #10b981; }
.badge--info      { background: rgba(16,185,129,.1);  color: #10b981; }
.badge--panic     { background: rgba(220,38,38,.2);   color: #dc2626; }
.badge--note      { background: var(--surface); color: var(--text-secondary); border: 1px solid var(--border); }

.err-row__time {
  font-size: 11.5px; font-family: var(--mono);
  color: var(--text-secondary); white-space: nowrap;
}
.err-row__db, .err-row__user {
  display: flex; align-items: center; gap: 3px;
  font-size: 11px; color: var(--text-secondary);
  white-space: nowrap;
}
.err-row__db svg, .err-row__user svg { opacity: .6; }

/* Expanded detail */
.err-row__detail {
  padding: 0 14px 14px 14px;
  border-top: 1px dashed color-mix(in srgb, var(--border) 70%, transparent);
}
.err-expand-enter-active { transition: all .2s ease; }
.err-expand-leave-active { transition: all .15s ease; }
.err-expand-enter-from, .err-expand-leave-to { opacity: 0; transform: translateY(-4px); }

.err-detail-grid { display: flex; flex-direction: column; gap: 10px; padding-top: 12px; }
.err-detail-section { display: flex; flex-direction: column; gap: 4px; }
.err-detail-label {
  font-size: 10px; font-weight: 700; letter-spacing: .08em;
  text-transform: uppercase; color: var(--text-secondary);
}
.err-detail-pre {
  margin: 0; font-family: var(--mono); font-size: 12px; line-height: 1.6;
  white-space: pre-wrap; word-break: break-all;
  background: var(--bg); border: 1px solid var(--border);
  border-radius: 6px; padding: 10px 12px;
  max-height: 200px; overflow-y: auto; color: var(--text);
}
.err-detail-pre--raw { color: var(--text-secondary); font-size: 11px; }

.err-detail-meta {
  display: flex; flex-wrap: wrap; gap: 6px; padding-top: 10px;
}
.err-meta-chip {
  display: inline-flex; align-items: center; gap: 5px;
  font-size: 11px; padding: 3px 9px; border-radius: 20px;
  background: var(--surface); border: 1px solid var(--border);
  color: var(--text-secondary);
}
.err-meta-chip svg { opacity: .6; }

/* Pagination */
.err-pager {
  display: flex; align-items: center; justify-content: space-between;
  padding: 10px 14px;
  border: 1px solid var(--border); border-top: none;
  border-radius: 0 0 10px 10px;
  background: var(--surface);
}
.err-pager__info {
  font-size: 12px; color: var(--text-secondary); display: flex; align-items: center; gap: 8px;
}
.err-pager__source {
  font-size: 11px; opacity: .7; font-style: italic;
}
.err-pager__btns { display: flex; align-items: center; gap: 4px; }
.err-pager__btn {
  font-size: 12.5px; padding: 4px 10px;
  border-radius: 6px; border: 1px solid var(--border);
  background: var(--bg); color: var(--text);
  cursor: pointer; transition: all .12s;
}
.err-pager__btn:hover:not(:disabled) { background: var(--accent); color: #fff; border-color: var(--accent); }
.err-pager__btn:disabled { opacity: .35; cursor: not-allowed; }
.err-pager__cur {
  font-size: 12.5px; font-weight: 700; padding: 4px 10px;
  border-radius: 6px; background: var(--accent);
  color: #fff; min-width: 32px; text-align: center;
}
</style>
