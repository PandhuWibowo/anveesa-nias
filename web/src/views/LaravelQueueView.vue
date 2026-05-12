<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

function debounce<T extends (...args: unknown[]) => void>(fn: T, ms: number): T {
  let timer: ReturnType<typeof setTimeout> | null = null
  return ((...args: unknown[]) => {
    if (timer !== null) clearTimeout(timer)
    timer = setTimeout(() => { timer = null; fn(...args) }, ms)
  }) as T
}
import { useConnections } from '@/composables/useConnections'
import { useLaravelQueue, type LaravelFailedJob, type LaravelHorizonSummary, type LaravelQueueAuditItem, type LaravelQueueFeatureFlags, type LaravelQueueJob, type LaravelQueueQuarantineItem, type LaravelQueueRules, type LaravelQueueSummary } from '@/composables/useLaravelQueue'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'

const props = defineProps<{ activeConnId: number | null }>()
const emit = defineEmits<{ (e: 'set-conn', id: number): void }>()

const { connections, fetchConnections } = useConnections()
const {
  fetchQueues,
  fetchJobs,
  deleteJob,
  requeueJob,
  clearQueue,
  fetchFailedJobs,
  retryFailedJob,
  deleteFailedJob,
  fetchHorizon,
  fetchOpsSettings,
  saveOpsSettings,
  fetchQueueAudit,
  fetchQuarantine,
  quarantineFailedJob,
  releaseQuarantine,
  emitQueueAlerts,
  runLaravelAgent,
} = useLaravelQueue()
const toast = useToast()
const { confirm } = useConfirm()

const selectedDb = ref(0)
const prefix = ref('queues')
const queues = ref<LaravelQueueSummary[]>([])
const selectedQueue = ref('default')
const jobs = ref<LaravelQueueJob[]>([])
const failedJobs = ref<LaravelFailedJob[]>([])
const horizon = ref<LaravelHorizonSummary | null>(null)
const selectedJobId = ref('')
const selectedFailedJobId = ref<number | null>(null)
const activeState = ref<'all' | 'ready' | 'delayed' | 'reserved' | 'failed'>('all')
const detailTab = ref<'summary' | 'main' | 'payload' | 'command' | 'exception'>('summary')
const insightTab = ref<'health' | 'timeline' | 'groups' | 'patterns' | 'quarantine' | 'controls' | 'settings' | 'audit'>('health')
const sidebarCollapsed = ref(false)
const insightsCollapsed = ref(false)
const detailFullscreenOpen = ref(false)
const failedGroupBy = ref<'job' | 'exception' | 'queue' | 'date'>('job')
const search = ref('')
const retryAfter = ref(90)
const autoRefresh = ref(false)
const refreshSeconds = ref(10)
const failedConnId = ref<number | null>(null)
const loadingQueues = ref(false)
const loadingJobs = ref(false)
const loadingFailed = ref(false)
const selectedJobIds = ref(new Set<string>())
const selectedFailedJobIds = ref(new Set<number>())
const profileName = ref('')
const selectedProfile = ref('')
const profiles = ref<QueueProfile[]>([])
const pendingProfile = ref<QueueProfile | null>(null)
const timeline = ref<QueueTimelineSample[]>([])
const editedFailedPayload = ref('')
const sandboxQueue = ref('debug')
const businessFieldsInput = ref('tenant_id,user_id,order_id,invoice_id,email,amount')
const quarantine = ref<LaravelQueueQuarantineItem[]>([])
const queueAudit = ref<LaravelQueueAuditItem[]>([])
const expandedAuditEntryId = ref<number | null>(null)
const opsSettingsLoaded = ref(false)
const savingOpsSettings = ref(false)
const featureFlags = ref<LaravelQueueFeatureFlags>({
  retry: true,
  delete: true,
  clear: true,
  editedReplay: true,
  readOnly: false,
  requireConfirm: true,
})
const queueRules = ref<LaravelQueueRules>({
  readyMax: 100,
  failedMax: 10,
  stuckMax: 0,
  oldestMinutesMax: 30,
  noConsumption: true,
})

interface QueueProfile {
  name: string
  redisConnId: number | null
  redisDb: number
  prefix: string
  queue: string
  failedConnId: number | null
  retryAfter: number
}

interface QueueTimelineSample {
  at: string
  ready: number
  delayed: number
  reserved: number
  failed: number
  stuck: number
}

type JsonLike = null | boolean | number | string | JsonLike[] | { [key: string]: JsonLike }

interface MainDataResult {
  title: string
  data: Record<string, unknown>
  rawCommand?: string
}

const redisDbIndexes = Array.from({ length: 16 }, (_, index) => index)
const redisConnections = computed(() => connections.value.filter((c) => c.driver === 'redis'))
function isNonSqlDriver(driver: string) {
  return driver === 'redis' || driver === 'memcache' || driver === 'kafka' || driver === 's3_aws' || driver === 's3_gcp' || driver === 's3_oss' || driver === 's3_obs'
}

const sqlConnections = computed(() => connections.value.filter((c) => !isNonSqlDriver(c.driver)))
const activeConn = computed(() =>
  props.activeConnId != null ? connections.value.find((c) => c.id === props.activeConnId) ?? null : null,
)
const isRedis = computed(() => activeConn.value?.driver === 'redis')
const isSql = computed(() => activeConn.value != null && !isNonSqlDriver(activeConn.value.driver))
// When in SQL-only mode, the user picks a Redis conn just for the retry-push step
const retryRedisConnId = ref<number | null>(null)
const filteredJobs = computed(() => {
  const query = search.value.trim().toLowerCase()
  return jobs.value.filter((job) => {
    if (activeState.value !== 'all' && job.state !== activeState.value) return false
    if (!query) return true
    return [
      job.uuid,
      job.display_name,
      job.command_name,
      job.job,
      job.raw,
    ].some(value => String(value || '').toLowerCase().includes(query))
  })
})
const selectedJob = computed(() => jobs.value.find(job => job.id === selectedJobId.value) ?? filteredJobs.value[0] ?? null)
const filteredFailedJobs = computed(() => {
  const query = search.value.trim().toLowerCase()
  return failedJobs.value.filter((job) => {
    if (!query) return true
    return [
      job.uuid,
      job.connection,
      job.queue,
      job.raw_payload,
      job.exception,
    ].some(value => String(value || '').toLowerCase().includes(query))
  })
})
const selectedFailedJob = computed(() => failedJobs.value.find(job => job.id === selectedFailedJobId.value) ?? filteredFailedJobs.value[0] ?? null)
const selectedSummary = computed(() => queues.value.find(queue => queue.name === selectedQueue.value) ?? null)
const detailFullscreenTitle = computed(() => {
  if (activeState.value === 'failed' && selectedFailedJob.value) {
    return selectedFailedJob.value.payload?.displayName || selectedFailedJob.value.payload?.job || 'Failed job'
  }
  if (selectedJob.value) {
    return selectedJob.value.display_name || selectedJob.value.command_name || 'Laravel job'
  }
  return 'Job detail'
})
const stuckJobs = computed(() => jobs.value.filter(isStuckJob))
const selectedJobs = computed(() => jobs.value.filter(job => selectedJobIds.value.has(job.id)))
const selectedFailedJobs = computed(() => failedJobs.value.filter(job => selectedFailedJobIds.value.has(job.id)))
const allVisibleJobsSelected = computed(() => filteredJobs.value.length > 0 && filteredJobs.value.every(job => selectedJobIds.value.has(job.id)))
const allVisibleFailedJobsSelected = computed(() => filteredFailedJobs.value.length > 0 && filteredFailedJobs.value.every(job => selectedFailedJobIds.value.has(job.id)))
const oldestJobAge = computed(() => {
  const scored = jobs.value.filter(job => job.score && job.score > 0)
  if (!scored.length) return '-'
  const oldest = Math.min(...scored.map(job => job.score || 0))
  return ageLabel(oldest)
})
const queueTotals = computed(() => queues.value.reduce((total, queue) => ({
  ready: total.ready + queue.ready,
  delayed: total.delayed + queue.delayed,
  reserved: total.reserved + queue.reserved,
}), { ready: 0, delayed: 0, reserved: 0 }))
const businessFields = computed(() => businessFieldsInput.value.split(',').map(field => field.trim()).filter(Boolean))
const oldestJobAgeMinutes = computed(() => {
  const scored = jobs.value.filter(job => job.score && job.score > 0)
  if (!scored.length) return 0
  const oldest = Math.min(...scored.map(job => job.score || 0))
  return Math.floor((Date.now() / 1000 - oldest) / 60)
})
const queueAlerts = computed(() => {
  const alerts: Array<{ level: 'danger' | 'warning'; title: string; detail: string }> = []
  if (queueRules.value.readyMax > 0 && queueTotals.value.ready > queueRules.value.readyMax) {
    alerts.push({ level: 'warning', title: 'Ready threshold exceeded', detail: `${queueTotals.value.ready} ready jobs > ${queueRules.value.readyMax}.` })
  }
  if (queueRules.value.failedMax > 0 && failedJobs.value.length > queueRules.value.failedMax) {
    alerts.push({ level: 'danger', title: 'Failed threshold exceeded', detail: `${failedJobs.value.length} failed jobs > ${queueRules.value.failedMax}.` })
  }
  if (stuckJobs.value.length > queueRules.value.stuckMax) {
    alerts.push({ level: 'danger', title: 'Stuck threshold exceeded', detail: `${stuckJobs.value.length} stuck jobs > ${queueRules.value.stuckMax}.` })
  }
  if (queueRules.value.oldestMinutesMax > 0 && oldestJobAgeMinutes.value > queueRules.value.oldestMinutesMax) {
    alerts.push({ level: 'warning', title: 'Oldest job age exceeded', detail: `${oldestJobAgeMinutes.value} minutes > ${queueRules.value.oldestMinutesMax}.` })
  }
  if (queueRules.value.noConsumption && queueTotals.value.ready > 0 && !queueTotals.value.reserved && !horizon.value?.supervisors) {
    alerts.push({ level: 'warning', title: 'No consumption detected', detail: 'Ready jobs exist, but no reserved jobs or Horizon supervisors were detected.' })
  }
  return alerts
})
const healthIssues = computed(() => {
  const issues: Array<{ level: 'danger' | 'warning' | 'ok'; title: string; detail: string }> = []
  for (const alert of queueAlerts.value) issues.push(alert)
  if (stuckJobs.value.length) {
    issues.push({
      level: 'danger',
      title: 'Reserved jobs past retry_after',
      detail: `${stuckJobs.value.length} job${stuckJobs.value.length === 1 ? '' : 's'} reserved longer than ${retryAfter.value}s.`,
    })
  }
  if (queueTotals.value.ready > 0 && !queueTotals.value.reserved && !horizon.value?.supervisors) {
    issues.push({
      level: 'warning',
      title: 'Queue may not be consuming',
      detail: `${queueTotals.value.ready} ready job${queueTotals.value.ready === 1 ? '' : 's'} and no reserved jobs or Horizon supervisors detected.`,
    })
  }
  if (failedJobs.value.length >= 25) {
    issues.push({
      level: 'warning',
      title: 'Failed jobs are accumulating',
      detail: `${failedJobs.value.length} failed job rows are currently loaded.`,
    })
  }
  if (queueTotals.value.delayed > 0 && stateCount('delayed') > 0) {
    const due = jobs.value.filter(job => job.state === 'delayed' && job.score && job.score <= Math.floor(Date.now() / 1000)).length
    if (due) {
      issues.push({
        level: 'warning',
        title: 'Delayed jobs are due',
        detail: `${due} delayed job${due === 1 ? '' : 's'} appear ready to move back to the queue.`,
      })
    }
  }
  if (horizon.value?.detected && !horizon.value.supervisors) {
    issues.push({
      level: 'warning',
      title: 'Horizon detected without supervisors',
      detail: 'Horizon keys exist, but no active supervisors were found in the sampled data.',
    })
  }
  if (!issues.length) {
    issues.push({
      level: 'ok',
      title: 'No obvious queue issue detected',
      detail: 'Current ready, delayed, reserved, failed, and Horizon signals look normal.',
    })
  }
  return issues
})
const failedGroups = computed(() => {
  const groups = new Map<string, { key: string; count: number; latest: string; sample: LaravelFailedJob }>()
  for (const job of failedJobs.value) {
    const key = failedGroupKey(job)
    const existing = groups.get(key)
    if (existing) {
      existing.count += 1
      if (new Date(job.failed_at).getTime() > new Date(existing.latest).getTime()) existing.latest = job.failed_at
    } else {
      groups.set(key, { key, count: 1, latest: job.failed_at, sample: job })
    }
  }
  return [...groups.values()].sort((a, b) => b.count - a.count || b.latest.localeCompare(a.latest))
})
const failedPatterns = computed(() => {
  const groups = new Map<string, { key: string; count: number; latest: string; sample: LaravelFailedJob; reasons: string[] }>()
  for (const job of failedJobs.value) {
    for (const pattern of patternKeys(job)) {
      const existing = groups.get(pattern.key)
      if (existing) {
        existing.count += 1
        if (new Date(job.failed_at).getTime() > new Date(existing.latest).getTime()) existing.latest = job.failed_at
      } else {
        groups.set(pattern.key, { key: pattern.key, count: 1, latest: job.failed_at, sample: job, reasons: [pattern.type] })
      }
    }
  }
  return [...groups.values()].filter(group => group.count > 1).sort((a, b) => b.count - a.count || b.latest.localeCompare(a.latest)).slice(0, 18)
})
const quarantinedFailedJobIds = computed(() => new Set(quarantine.value.map(item => item.failed_job_id)))
const quarantinedFailedJobs = computed(() => failedJobs.value.filter(job => quarantinedFailedJobIds.value.has(job.id)))
const maxTimelineTotal = computed(() => Math.max(1, ...timeline.value.map(sample => sample.ready + sample.delayed + sample.reserved + sample.failed)))
let refreshTimer: number | null = null

onMounted(async () => {
  loadProfiles()
  if (!connections.value.length) await fetchConnections()
  // Only auto-switch to the sole Redis connection when no SQL conn is active
  if (!isRedis.value && !isSql.value && redisConnections.value.length === 1) {
    emit('set-conn', redisConnections.value[0].id)
    return
  }
  selectedDb.value = Number(activeConn.value?.database || 0)
  if (isRedis.value) {
    failedConnId.value = sqlConnections.value[0]?.id ?? null
    await loadOpsSettings()
    await Promise.all([loadQueues(), loadHorizon(), loadQuarantine(), loadQueueAudit()])
  } else if (isSql.value) {
    failedConnId.value = props.activeConnId
    activeState.value = 'failed'
    await loadOpsSettings()
    await Promise.all([loadFailedJobs(), loadQuarantine(), loadQueueAudit()])
  }
})

onBeforeUnmount(() => stopAutoRefresh())

watch(() => props.activeConnId, async () => {
  const profile = pendingProfile.value
  if (profile && profile.redisConnId === props.activeConnId) {
    selectedDb.value = profile.redisDb
    prefix.value = profile.prefix
    selectedQueue.value = profile.queue
    failedConnId.value = profile.failedConnId
    retryAfter.value = profile.retryAfter
    pendingProfile.value = null
  } else {
    selectedDb.value = Number(activeConn.value?.database || 0)
  }
  selectedJobId.value = ''
  selectedJobIds.value = new Set()
  selectedFailedJobIds.value = new Set()
  queues.value = []
  jobs.value = []
  failedJobs.value = []
  if (isRedis.value) {
    await loadOpsSettings()
    await Promise.all([loadQueues(), loadHorizon(), loadQuarantine(), loadQueueAudit()])
  } else if (isSql.value) {
    failedConnId.value = props.activeConnId
    activeState.value = 'failed'
    await loadOpsSettings()
    await Promise.all([loadFailedJobs(), loadQuarantine(), loadQueueAudit()])
  }
})

watch([autoRefresh, refreshSeconds], () => {
  stopAutoRefresh()
  if (autoRefresh.value) {
    refreshTimer = window.setInterval(refreshCurrentView, Math.max(3, Number(refreshSeconds.value || 10)) * 1000)
  }
})

watch(failedConnId, async () => {
  selectedFailedJobId.value = null
  if (failedConnId.value) await loadFailedJobs()
})

watch([selectedDb, prefix], async () => {
  selectedJobId.value = ''
  selectedJobIds.value = new Set()
  if (isRedis.value) await loadQueues()
})

watch(selectedQueue, async () => {
  selectedJobId.value = ''
  selectedJobIds.value = new Set()
  if (isRedis.value) await loadJobs()
})

watch(selectedFailedJob, (job) => {
  editedFailedPayload.value = job ? formatFailedPayload(job) : ''
}, { immediate: true })

const debouncedPersistOpsSettings = debounce(() => persistOpsSettings(), 600)
watch([featureFlags, queueRules, businessFieldsInput, sandboxQueue], () => debouncedPersistOpsSettings(), { deep: true })

async function loadQueues() {
  if (!activeConn.value || !isRedis.value) return
  loadingQueues.value = true
  try {
    queues.value = await fetchQueues(activeConn.value.id, { db: selectedDb.value, prefix: prefix.value })
    if (!queues.value.some(queue => queue.name === selectedQueue.value)) {
      selectedQueue.value = queues.value[0]?.name ?? 'default'
    }
    await loadJobs()
  } catch (err) {
    toast.error(errorMessage(err, 'Failed to load Laravel queues'))
  } finally {
    loadingQueues.value = false
  }
}

async function loadJobs() {
  if (!activeConn.value || !isRedis.value || !selectedQueue.value) return
  loadingJobs.value = true
  try {
    const data = await fetchJobs(activeConn.value.id, {
      queue: selectedQueue.value,
      db: selectedDb.value,
      prefix: prefix.value,
      limit: 100,
    })
    jobs.value = data.jobs
    selectedJobId.value = filteredJobs.value[0]?.id ?? ''
    selectedJobIds.value = new Set([...selectedJobIds.value].filter(id => jobs.value.some(job => job.id === id)))
    recordTimelineSample()
  } catch (err) {
    toast.error(errorMessage(err, 'Failed to load Laravel queue jobs'))
  } finally {
    loadingJobs.value = false
  }
}

async function loadHorizon() {
  if (!activeConn.value || !isRedis.value) return
  try {
    horizon.value = await fetchHorizon(activeConn.value.id, selectedDb.value)
  } catch {
    horizon.value = null
  }
}

async function loadFailedJobs() {
  if (!failedConnId.value) return
  loadingFailed.value = true
  try {
    failedJobs.value = await fetchFailedJobs(failedConnId.value, 100)
    selectedFailedJobId.value = filteredFailedJobs.value[0]?.id ?? null
    selectedFailedJobIds.value = new Set([...selectedFailedJobIds.value].filter(id => failedJobs.value.some(job => job.id === id)))
    recordTimelineSample()
  } catch (err) {
    toast.error(errorMessage(err, 'Failed to load Laravel failed jobs'))
  } finally {
    loadingFailed.value = false
  }
}

function selectRedisConnection(rawId: string | number) {
  const id = Number(rawId)
  if (Number.isFinite(id)) emit('set-conn', id)
}

function stateCount(state: LaravelQueueJob['state']) {
  return jobs.value.filter(job => job.state === state).length
}

function commandData(job: LaravelQueueJob | null) {
  const data = job?.payload?.data
  return data && typeof data === 'object' ? JSON.stringify(data, null, 2) : ''
}

function failedCommandData(job: LaravelFailedJob | null) {
  const data = job?.payload?.data
  return data && typeof data === 'object' ? JSON.stringify(data, null, 2) : ''
}

function mainDataForJob(job: LaravelQueueJob | null) {
  if (!job) return ''
  return JSON.stringify(extractMainData(job.payload, {
    queue: job.queue,
    uuid: job.uuid,
    attempts: job.attempts,
    state: job.state,
  }), null, 2)
}

function mainDataForFailedJob(job: LaravelFailedJob | null) {
  if (!job) return ''
  return JSON.stringify(extractMainData(job.payload, {
    queue: job.queue,
    uuid: job.uuid,
    failed_at: job.failed_at,
    connection: job.connection,
  }), null, 2)
}

function isStuckJob(job: LaravelQueueJob) {
  if (job.state !== 'reserved' || !job.score) return false
  return job.score < Math.floor(Date.now() / 1000) - Number(retryAfter.value || 0)
}

function ageLabel(score: number) {
  const seconds = Math.max(0, Math.floor(Date.now() / 1000) - score)
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`
  return `${Math.floor(seconds / 86400)}d`
}

function formatDate(raw?: string) {
  if (!raw) return '-'
  const date = new Date(raw)
  return Number.isNaN(date.getTime()) ? raw : date.toLocaleString()
}

function formatAuditDetails(entry: LaravelQueueAuditItem) {
  const details = entry.details && Object.keys(entry.details).length ? entry.details : null
  return JSON.stringify({
    action: entry.action,
    status: entry.status,
    queue: entry.queue || undefined,
    target_queue: entry.target_queue || undefined,
    failed_job_id: entry.failed_job_id || undefined,
    job_uuid: entry.job_uuid || undefined,
    payload_edited: entry.payload_edited || undefined,
    error: entry.error || undefined,
    details: details || undefined,
  }, null, 2)
}

function formatPayload(job: LaravelQueueJob | null) {
  if (!job) return ''
  return JSON.stringify(job.payload ?? safeParse(job.raw) ?? job.raw, null, 2)
}

function formatFailedPayload(job: LaravelFailedJob | null) {
  if (!job) return ''
  return JSON.stringify(job.payload ?? safeParse(job.raw_payload) ?? job.raw_payload, null, 2)
}

function safeParse(raw: string) {
  try { return JSON.parse(raw) } catch { return null }
}

function isValidJson(raw: string) {
  try {
    JSON.parse(raw)
    return true
  } catch {
    return false
  }
}

function extractMainData(payload: Record<string, unknown> | undefined, meta: Record<string, unknown>): MainDataResult {
  if (!payload) return { title: 'No payload found', data: meta }
  const data = isRecord(payload.data) ? payload.data : {}
  const command = typeof data.command === 'string' ? data.command : ''
  const commandName = String(payload.displayName || data.commandName || payload.job || 'Laravel job')
  const main: Record<string, unknown> = {
    job: commandName,
    ...compactRecord(meta),
  }

  const directData = compactRecord(pickReadableFields(data))
  delete directData.command
  delete directData.commandName
  Object.assign(main, directData)

  const decoded = command ? decodeLaravelCommand(command) : {}
  Object.assign(main, compactRecord(decoded))
  const context = pickBusinessContext(main)
  if (Object.keys(context).length) {
    main.business_context = context
  }

  return {
    title: commandName,
    data: main,
    rawCommand: command || undefined,
  }
}

function pickBusinessContext(value: unknown): Record<string, unknown> {
  const result: Record<string, unknown> = {}
  collectBusinessContext(value, result, 0)
  return result
}

function collectBusinessContext(value: unknown, result: Record<string, unknown>, depth: number) {
  if (depth > 5 || !isRecord(value)) return
  for (const [key, raw] of Object.entries(value)) {
    if (businessFields.value.includes(key) && !isEmptyValue(raw)) {
      result[key] = raw
    }
    if (isRecord(raw)) collectBusinessContext(raw, result, depth + 1)
    if (Array.isArray(raw)) {
      for (const item of raw) collectBusinessContext(item, result, depth + 1)
    }
  }
}

function pickReadableFields(value: unknown): Record<string, unknown> {
  if (!isRecord(value)) return {}
  const ignored = new Set([
    'command',
    'commandName',
    'middleware',
    'chained',
    'chainConnection',
    'chainQueue',
    'chainCatchCallbacks',
    'delay',
    'afterCommit',
    'backoff',
    'timeout',
    'tries',
    'maxExceptions',
    'failOnTimeout',
    'retryUntil',
    'uuid',
  ])
  const result: Record<string, unknown> = {}
  for (const [key, raw] of Object.entries(value)) {
    const cleanKey = cleanPhpPropertyName(key)
    if (!cleanKey || ignored.has(cleanKey)) continue
    const normalized = normalizeMainValue(raw, 0)
    if (normalized !== undefined && !isEmptyValue(normalized)) result[cleanKey] = normalized
  }
  return result
}

function decodeLaravelCommand(raw: string): Record<string, unknown> {
  const parsed = parsePhpSerialized(raw)
  const result: Record<string, unknown> = {}
  if (parsed !== undefined) {
    const normalized = normalizeMainValue(parsed, 0)
    if (isRecord(normalized)) Object.assign(result, pickReadableFields(normalized))
    else if (normalized !== undefined) result.command_value = normalized
  }

  const models = extractSerializedModels(raw)
  if (models.length) result.models = models

  if (!Object.keys(result).length) {
    const scalarFields = extractSerializedScalarFields(raw)
    Object.assign(result, scalarFields)
  }
  return result
}

function parsePhpSerialized(raw: string): unknown {
  let index = 0

  function readUntil(char: string) {
    const end = raw.indexOf(char, index)
    if (end < 0) throw new Error('Invalid serialized payload')
    const value = raw.slice(index, end)
    index = end + 1
    return value
  }

  function readValue(): unknown {
    const type = raw[index]
    index += 2
    if (type === 'N') {
      index -= 1
      if (raw[index] !== ';') throw new Error('Invalid null')
      index += 1
      return null
    }
    if (type === 'b') {
      const value = readUntil(';')
      return value === '1'
    }
    if (type === 'i') return Number(readUntil(';'))
    if (type === 'd') return Number(readUntil(';'))
    if (type === 's') {
      const length = Number(readUntil(':'))
      if (raw[index] !== '"') throw new Error('Invalid string')
      index += 1
      const value = raw.slice(index, index + length)
      index += length + 2
      return value
    }
    if (type === 'a') {
      const length = Number(readUntil(':'))
      if (raw[index] !== '{') throw new Error('Invalid array')
      index += 1
      const entries: Array<[unknown, unknown]> = []
      for (let i = 0; i < length; i += 1) entries.push([readValue(), readValue()])
      if (raw[index] !== '}') throw new Error('Invalid array close')
      index += 1
      return entriesToObject(entries)
    }
    if (type === 'O' || type === 'C') {
      const classLength = Number(readUntil(':'))
      if (raw[index] !== '"') throw new Error('Invalid class')
      index += 1
      const className = raw.slice(index, index + classLength)
      index += classLength + 2
      const propCount = Number(readUntil(':'))
      if (raw[index] !== '{') throw new Error('Invalid object')
      index += 1
      const objectValue: Record<string, unknown> = { class: className }
      for (let i = 0; i < propCount; i += 1) {
        const key = cleanPhpPropertyName(String(readValue() ?? ''))
        objectValue[key] = readValue()
      }
      if (raw[index] !== '}') throw new Error('Invalid object close')
      index += 1
      return objectValue
    }
    throw new Error(`Unsupported serialized type ${type}`)
  }

  try {
    return readValue()
  } catch {
    return undefined
  }
}

function entriesToObject(entries: Array<[unknown, unknown]>) {
  const isList = entries.every(([key], index) => Number(key) === index)
  if (isList) return entries.map(([, value]) => value)
  return entries.reduce<Record<string, unknown>>((acc, [key, value]) => {
    acc[cleanPhpPropertyName(String(key))] = value
    return acc
  }, {})
}

function normalizeMainValue(value: unknown, depth: number): JsonLike | undefined {
  if (depth > 4) return undefined
  if (value == null || ['string', 'number', 'boolean'].includes(typeof value)) return value as JsonLike
  if (Array.isArray(value)) {
    const list = value.map(item => normalizeMainValue(item, depth + 1)).filter(item => item !== undefined) as JsonLike[]
    return list.length ? list : undefined
  }
  if (!isRecord(value)) return undefined

  const result: Record<string, JsonLike> = {}
  for (const [rawKey, rawValue] of Object.entries(value)) {
    const key = cleanPhpPropertyName(rawKey)
    if (!key || key.startsWith('__')) continue
    const normalized = normalizeMainValue(rawValue, depth + 1)
    if (normalized !== undefined && !isEmptyValue(normalized)) result[key] = normalized
  }
  return Object.keys(result).length ? result : undefined
}

function extractSerializedModels(raw: string) {
  const models: Array<Record<string, unknown>> = []
  const regex = /ModelIdentifier.+?s:5:"class";s:\d+:"([^"]+)".+?s:2:"id";(?:i:(\d+);|s:\d+:"([^"]+)")/gs
  for (const match of raw.matchAll(regex)) {
    models.push({
      class: match[1],
      id: match[2] || match[3],
    })
  }
  return models
}

function extractSerializedScalarFields(raw: string) {
  const fields: Record<string, unknown> = {}
  const regex = /s:\d+:"\u0000?\*?\u0000?([^"]+)";(?:s:\d+:"([^"]*)"|i:(-?\d+)|b:([01])|d:([-0-9.]+));/g
  for (const match of raw.matchAll(regex)) {
    const key = cleanPhpPropertyName(match[1])
    if (!key || key.length > 80) continue
    if (match[2] != null && match[2] !== '') fields[key] = match[2]
    else if (match[3] != null) fields[key] = Number(match[3])
    else if (match[4] != null) fields[key] = match[4] === '1'
    else if (match[5] != null) fields[key] = Number(match[5])
  }
  return compactRecord(fields)
}

function cleanPhpPropertyName(key: string) {
  return key.replace(/\u0000\*\u0000/g, '').replace(/\u0000[^]*\u0000/g, '').trim()
}

function compactRecord(record: Record<string, unknown>) {
  return Object.entries(record).reduce<Record<string, unknown>>((acc, [key, value]) => {
    if (value !== undefined && !isEmptyValue(value)) acc[key] = value
    return acc
  }, {})
}

function isEmptyValue(value: unknown) {
  if (value == null || value === '') return true
  if (Array.isArray(value)) return value.length === 0
  return isRecord(value) && Object.keys(value).length === 0
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

function failedGroupKey(job: LaravelFailedJob) {
  if (failedGroupBy.value === 'queue') return job.queue || 'default'
  if (failedGroupBy.value === 'date') return (job.failed_at || '').slice(0, 10) || 'unknown date'
  if (failedGroupBy.value === 'exception') {
    const firstLine = job.exception?.split('\n')[0] || 'Unknown exception'
    return firstLine.replace(/^([A-Za-z0-9_\\]+).*$/, '$1') || firstLine
  }
  return String(job.payload?.displayName || job.payload?.job || job.uuid || 'Unknown job')
}

function patternKeys(job: LaravelFailedJob) {
  const main = extractMainData(job.payload, { queue: job.queue, uuid: job.uuid }).data
  const context = isRecord(main.business_context) ? main.business_context : {}
  const modelIds = Array.isArray(main.models) ? main.models : []
  const exception = job.exception?.split('\n')[0] || 'Unknown exception'
  const keys = [
    { type: 'job', key: `Job: ${String(job.payload?.displayName || job.payload?.job || job.uuid || 'Unknown job')}` },
    { type: 'exception', key: `Exception: ${exception.replace(/^([A-Za-z0-9_\\]+).*$/, '$1')}` },
    { type: 'queue', key: `Queue: ${job.queue || 'default'}` },
  ]
  for (const [field, value] of Object.entries(context)) {
    keys.push({ type: field, key: `${field}: ${String(value)}` })
  }
  for (const model of modelIds) {
    if (isRecord(model) && model.class && model.id) keys.push({ type: 'model', key: `Model: ${model.class}#${model.id}` })
  }
  return keys
}

function canAction(action: 'retry' | 'delete' | 'clear' | 'editedReplay') {
  if (featureFlags.value.readOnly) return false
  return featureFlags.value[action]
}

async function confirmAction(action: 'retry' | 'delete' | 'clear' | 'editedReplay', message: string, title: string) {
  if (!canAction(action)) {
    toast.error(featureFlags.value.readOnly ? 'Read-only mode is enabled' : `${title} is disabled by feature flags`)
    return false
  }
  return featureFlags.value.requireConfirm ? confirm(message, title) : true
}

async function loadOpsSettings() {
  if (!activeConn.value) return
  opsSettingsLoaded.value = false
  try {
    const settings = await fetchOpsSettings(activeConn.value.id)
    featureFlags.value = { ...featureFlags.value, ...(settings.featureFlags || {}) }
    queueRules.value = { ...queueRules.value, ...(settings.queueRules || {}) }
    businessFieldsInput.value = settings.businessFieldsInput || businessFieldsInput.value
    sandboxQueue.value = settings.sandboxQueue || sandboxQueue.value
  } catch (err) {
    toast.error(errorMessage(err, 'Failed to load queue operations settings'))
  } finally {
    opsSettingsLoaded.value = true
  }
}

async function persistOpsSettings() {
  if (!activeConn.value || !opsSettingsLoaded.value || savingOpsSettings.value) return
  savingOpsSettings.value = true
  try {
    await saveOpsSettings(activeConn.value.id, {
      featureFlags: featureFlags.value,
      queueRules: queueRules.value,
      businessFieldsInput: businessFieldsInput.value,
      sandboxQueue: sandboxQueue.value,
      environment: 'default',
    })
  } catch (err) {
    toast.error(errorMessage(err, 'Failed to save queue operations settings'))
  } finally {
    savingOpsSettings.value = false
  }
}

async function loadQuarantine() {
  if (!activeConn.value) return
  try {
    quarantine.value = await fetchQuarantine(activeConn.value.id)
  } catch (err) {
    toast.error(errorMessage(err, 'Failed to load queue quarantine'))
  }
}

async function loadQueueAudit() {
  if (!activeConn.value) return
  try {
    queueAudit.value = await fetchQueueAudit(activeConn.value.id, 100)
  } catch {
    queueAudit.value = []
  }
}

async function quarantineSelectedFailed() {
  const results = await Promise.allSettled(selectedFailedJobs.value.map(job => quarantineFailed(job, false)))
  await loadQuarantine()
  const failed = results.filter(r => r.status === 'rejected').length
  if (failed > 0) {
    toast.error(`${failed} job${failed === 1 ? '' : 's'} failed to quarantine`)
  } else {
    toast.success('Selected failed jobs moved to quarantine')
  }
}

async function quarantineFailed(job: LaravelFailedJob | null, notify = true) {
  if (!failedConnId.value || !job) return
  // In SQL mode, use failedConnId as the primary conn; in Redis mode use the Redis conn
  const connId = isRedis.value ? activeConn.value!.id : failedConnId.value
  try {
    await quarantineFailedJob(connId, {
      failed_conn_id: failedConnId.value,
      failed_job_id: job.id,
      uuid: job.uuid,
      queue: job.queue,
      job_name: String(job.payload?.displayName || job.payload?.job || job.uuid || `failed_jobs #${job.id}`),
      payload: job.raw_payload,
      exception: job.exception,
      reason: 'manual review',
    })
    await loadQuarantine()
    await loadQueueAudit()
    if (notify) toast.success('Failed job moved to quarantine')
  } catch (err) {
    toast.error(errorMessage(err, 'Failed to quarantine job'))
  }
}

async function removeFromQuarantine(job: LaravelFailedJob) {
  const connId = isRedis.value ? activeConn.value?.id : failedConnId.value
  if (!connId) return
  const item = quarantine.value.find(entry => entry.failed_job_id === job.id)
  if (!item) return
  try {
    await releaseQuarantine(connId, item.id)
    await loadQuarantine()
  } catch (err) {
    toast.error(errorMessage(err, 'Failed to release quarantine item'))
  }
}

async function emitCurrentAlerts() {
  if (!activeConn.value || queueAlerts.value.length === 0) return
  try {
    await emitQueueAlerts(activeConn.value.id, {
      queue: selectedQueue.value,
      prefix: prefix.value,
      alerts: queueAlerts.value,
    })
    await loadQueueAudit()
    toast.success('Queue alerts sent')
  } catch (err) {
    toast.error(errorMessage(err, 'Failed to send queue alerts'))
  }
}

async function runAgentCommand(command: string) {
  if (!activeConn.value) return
  try {
    await runLaravelAgent(activeConn.value.id, { command, queue: selectedQueue.value })
  } catch (err) {
    toast.error(errorMessage(err, 'Laravel agent command failed'))
  } finally {
    await loadQueueAudit()
  }
}

function recordTimelineSample() {
  const sample: QueueTimelineSample = {
    at: new Date().toLocaleTimeString(),
    ready: queueTotals.value.ready,
    delayed: queueTotals.value.delayed,
    reserved: queueTotals.value.reserved,
    failed: failedJobs.value.length,
    stuck: stuckJobs.value.length,
  }
  const last = timeline.value[timeline.value.length - 1]
  if (last && last.ready === sample.ready && last.delayed === sample.delayed && last.reserved === sample.reserved && last.failed === sample.failed && last.stuck === sample.stuck) {
    timeline.value = [...timeline.value.slice(0, -1), sample]
    return
  }
  timeline.value = [...timeline.value, sample].slice(-24)
}

function timelineWidth(value: number) {
  return `${Math.max(2, Math.round((value / maxTimelineTotal.value) * 100))}%`
}

async function exportVisibleJobs() {
  const payload = {
    exported_at: new Date().toISOString(),
    redis_connection: activeConn.value?.name || null,
    redis_db: selectedDb.value,
    prefix: prefix.value,
    queue: selectedQueue.value,
    state: activeState.value,
    jobs: activeState.value === 'failed' ? filteredFailedJobs.value : filteredJobs.value,
  }
  await exportJson(`laravel-queue-${selectedQueue.value}-${activeState.value}.json`, payload)
}

async function exportSelectedJobs() {
  const payload = {
    exported_at: new Date().toISOString(),
    redis_connection: activeConn.value?.name || null,
    redis_db: selectedDb.value,
    prefix: prefix.value,
    queue: selectedQueue.value,
    state: activeState.value,
    jobs: activeState.value === 'failed' ? selectedFailedJobs.value : selectedJobs.value,
  }
  await exportJson(`laravel-queue-${selectedQueue.value}-selected.json`, payload)
}

async function exportJson(filename: string, payload: unknown) {
  const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename.replace(/[^a-z0-9._-]+/gi, '-').toLowerCase()
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
  toast.success('JSON exported')
}

async function copyPayload(job: LaravelQueueJob | null) {
  if (!job) return
  await navigator.clipboard.writeText(formatPayload(job))
  toast.success('Payload copied')
}

async function copyFailedPayload(job: LaravelFailedJob | null) {
  if (!job) return
  await navigator.clipboard.writeText(formatFailedPayload(job))
  toast.success('Failed job payload copied')
}

async function removeJob(job: LaravelQueueJob | null) {
  if (!activeConn.value || !job) return
  const ok = await confirmAction('delete', `Delete this ${job.state} job from "${job.queue}"?`, 'Delete Queue Job')
  if (!ok) return
  try {
    await deleteJob(activeConn.value.id, { queue: selectedQueue.value, prefix: prefix.value, db: selectedDb.value, state: job.state, raw: job.raw })
    toast.success('Job deleted')
    await loadQueues()
    await loadQueueAudit()
  } catch (err) {
    toast.error(errorMessage(err, 'Failed to delete job'))
  }
}

function toggleJobSelection(job: LaravelQueueJob, checked: boolean) {
  const next = new Set(selectedJobIds.value)
  if (checked) next.add(job.id)
  else next.delete(job.id)
  selectedJobIds.value = next
}

function toggleAllVisibleJobs(checked: boolean) {
  const next = new Set(selectedJobIds.value)
  for (const job of filteredJobs.value) {
    if (checked) next.add(job.id)
    else next.delete(job.id)
  }
  selectedJobIds.value = next
}

function toggleFailedSelection(job: LaravelFailedJob, checked: boolean) {
  const next = new Set(selectedFailedJobIds.value)
  if (checked) next.add(job.id)
  else next.delete(job.id)
  selectedFailedJobIds.value = next
}

function toggleAllVisibleFailed(checked: boolean) {
  const next = new Set(selectedFailedJobIds.value)
  for (const job of filteredFailedJobs.value) {
    if (checked) next.add(job.id)
    else next.delete(job.id)
  }
  selectedFailedJobIds.value = next
}

async function bulkDeleteJobs() {
  if (!activeConn.value || selectedJobs.value.length === 0) return
  const ok = await confirmAction('delete', `Delete ${selectedJobs.value.length} selected queue job${selectedJobs.value.length === 1 ? '' : 's'}?`, 'Delete Selected Jobs')
  if (!ok) return
  try {
    await Promise.all(selectedJobs.value.map(job =>
      deleteJob(activeConn.value!.id, { queue: selectedQueue.value, prefix: prefix.value, db: selectedDb.value, state: job.state, raw: job.raw }),
    ))
    toast.success('Selected jobs deleted')
    selectedJobIds.value = new Set()
    await loadQueues()
    await loadQueueAudit()
  } catch (err) {
    toast.error(errorMessage(err, 'Failed to delete selected jobs'))
  }
}

async function bulkRequeueJobs() {
  if (!activeConn.value || selectedJobs.value.length === 0) return
  if (!canAction('retry')) {
    toast.error('Retry/requeue is disabled by feature flags')
    return
  }
  const movable = selectedJobs.value.filter(job => job.state !== 'ready')
  if (movable.length === 0) {
    toast.error('Only delayed or reserved jobs can be requeued')
    return
  }
  const ok = await confirmAction('retry', `Requeue ${movable.length} delayed/reserved job${movable.length === 1 ? '' : 's'}?`, 'Requeue Selected Jobs')
  if (!ok) return
  try {
    await Promise.all(movable.map(job =>
      requeueJob(activeConn.value!.id, { queue: selectedQueue.value, prefix: prefix.value, db: selectedDb.value, state: job.state, raw: job.raw }),
    ))
    toast.success('Selected jobs requeued')
    selectedJobIds.value = new Set()
    await loadQueues()
    await loadQueueAudit()
  } catch (err) {
    toast.error(errorMessage(err, 'Failed to requeue selected jobs'))
  }
}

async function requeueSelectedJob(job: LaravelQueueJob | null) {
  if (!activeConn.value || !job) return
  const ok = await confirmAction('retry', `Move this ${job.state} job back to the ready queue?`, 'Requeue Job')
  if (!ok) return
  try {
    await requeueJob(activeConn.value.id, { queue: selectedQueue.value, prefix: prefix.value, db: selectedDb.value, state: job.state, raw: job.raw })
    toast.success('Job requeued')
    await loadQueues()
    await loadQueueAudit()
  } catch (err) {
    toast.error(errorMessage(err, 'Failed to requeue job'))
  }
}

async function clearSelectedQueue() {
  if (!activeConn.value || !selectedQueue.value) return
  const ok = await confirmAction('clear', `Clear all ready, delayed, reserved, and notify keys for "${selectedQueue.value}"?`, 'Clear Laravel Queue')
  if (!ok) return
  try {
    await clearQueue(activeConn.value.id, { queue: selectedQueue.value, prefix: prefix.value, db: selectedDb.value, state: 'all' })
    toast.success('Queue cleared')
    await loadQueues()
    await loadQueueAudit()
  } catch (err) {
    toast.error(errorMessage(err, 'Failed to clear queue'))
  }
}

async function retrySelectedFailedJob(job: LaravelFailedJob | null, deleteAfter: boolean, queueOverride = '') {
  if (!failedConnId.value || !job) return
  const redisConnId = isRedis.value ? activeConn.value?.id : retryRedisConnId.value
  if (!redisConnId) {
    toast.error('Select a Redis connection to push the retried job to')
    return
  }
  if (deleteAfter && !canAction('delete')) {
    toast.error('Delete after retry is disabled by feature flags')
    return
  }
  const action = detailTab.value === 'payload' ? 'editedReplay' : 'retry'
  const payload = editedFailedPayload.value.trim() || job.raw_payload
  if (!isValidJson(payload)) {
    toast.error('Edited payload must be valid JSON')
    return
  }
  const targetQueue = queueOverride.trim() || job.queue || selectedQueue.value
  const ok = await confirmAction(action, `Retry failed job #${job.id} into "${targetQueue}"?`, 'Retry Failed Job')
  if (!ok) return
  try {
    await retryFailedJob(failedConnId.value, {
      id: job.id,
      redis_conn_id: redisConnId,
      redis_db: selectedDb.value,
      prefix: prefix.value,
      queue: targetQueue,
      payload,
      delete_after: deleteAfter,
      payload_edited: payload !== job.raw_payload,
    })
    toast.success(deleteAfter ? 'Failed job retried and deleted' : 'Failed job retried')
    await loadQueues()
    await loadFailedJobs()
    await loadQueueAudit()
  } catch (err) {
    toast.error(errorMessage(err, 'Failed to retry failed job'))
  }
}

async function removeFailedJob(job: LaravelFailedJob | null) {
  if (!failedConnId.value || !job) return
  const ok = await confirmAction('delete', `Delete failed job #${job.id}?`, 'Delete Failed Job')
  if (!ok) return
  try {
    await deleteFailedJob(failedConnId.value, job.id, activeConn.value?.id)
    toast.success('Failed job deleted')
    await loadFailedJobs()
    await loadQueueAudit()
  } catch (err) {
    toast.error(errorMessage(err, 'Failed to delete failed job'))
  }
}

async function bulkRetryFailed(deleteAfter: boolean) {
  if (!failedConnId.value || selectedFailedJobs.value.length === 0) return
  const redisConnId = isRedis.value ? activeConn.value?.id : retryRedisConnId.value
  if (!redisConnId) {
    toast.error('Select a Redis connection to push the retried jobs to')
    return
  }
  if (deleteAfter && !canAction('delete')) {
    toast.error('Delete after retry is disabled by feature flags')
    return
  }
  const ok = await confirmAction('retry', `Retry ${selectedFailedJobs.value.length} selected failed job${selectedFailedJobs.value.length === 1 ? '' : 's'}?`, 'Retry Selected Failed Jobs')
  if (!ok) return
  try {
    await Promise.all(selectedFailedJobs.value.map(job =>
      retryFailedJob(failedConnId.value!, {
        id: job.id,
        redis_conn_id: redisConnId!,
        redis_db: selectedDb.value,
        prefix: prefix.value,
        queue: job.queue || selectedQueue.value,
        payload: job.raw_payload,
        delete_after: deleteAfter,
        payload_edited: false,
      }),
    ))
    toast.success(deleteAfter ? 'Selected failed jobs retried and deleted' : 'Selected failed jobs retried')
    selectedFailedJobIds.value = new Set()
    await loadQueues()
    await loadFailedJobs()
    await loadQueueAudit()
  } catch (err) {
    toast.error(errorMessage(err, 'Failed to retry selected failed jobs'))
  }
}

async function bulkDeleteFailed() {
  if (!failedConnId.value || selectedFailedJobs.value.length === 0) return
  const ok = await confirmAction('delete', `Delete ${selectedFailedJobs.value.length} selected failed job row${selectedFailedJobs.value.length === 1 ? '' : 's'}?`, 'Delete Selected Failed Jobs')
  if (!ok) return
  try {
    await Promise.all(selectedFailedJobs.value.map(job =>
      deleteFailedJob(failedConnId.value!, job.id, activeConn.value?.id),
    ))
    toast.success('Selected failed jobs deleted')
    selectedFailedJobIds.value = new Set()
    await loadFailedJobs()
    await loadQueueAudit()
  } catch (err) {
    toast.error(errorMessage(err, 'Failed to delete selected failed jobs'))
  }
}

async function refreshCurrentView() {
  await loadQueues()
  await loadHorizon()
  if (activeState.value === 'failed' && failedConnId.value) await loadFailedJobs()
}

function stopAutoRefresh() {
  if (refreshTimer != null) {
    window.clearInterval(refreshTimer)
    refreshTimer = null
  }
}

function loadProfiles() {
  try {
    profiles.value = JSON.parse(localStorage.getItem('laravel_queue_profiles') || '[]')
  } catch {
    profiles.value = []
  }
}

function persistProfiles() {
  localStorage.setItem('laravel_queue_profiles', JSON.stringify(profiles.value))
}

function saveProfile() {
  const name = profileName.value.trim() || `${activeConn.value?.name || 'Redis'} / ${selectedQueue.value}`
  const profile: QueueProfile = {
    name,
    redisConnId: activeConn.value?.id ?? null,
    redisDb: selectedDb.value,
    prefix: prefix.value,
    queue: selectedQueue.value,
    failedConnId: failedConnId.value,
    retryAfter: Number(retryAfter.value || 90),
  }
  const existing = profiles.value.findIndex(item => item.name === name)
  if (existing >= 0) profiles.value[existing] = profile
  else profiles.value.push(profile)
  profiles.value.sort((a, b) => a.name.localeCompare(b.name))
  selectedProfile.value = name
  profileName.value = ''
  persistProfiles()
  toast.success('Queue profile saved')
}

async function applyProfile(name: string) {
  const profile = profiles.value.find(item => item.name === name)
  if (!profile) return
  if (profile.redisConnId && profile.redisConnId !== activeConn.value?.id) {
    pendingProfile.value = profile
    emit('set-conn', profile.redisConnId)
    return
  }
  selectedDb.value = profile.redisDb
  prefix.value = profile.prefix
  selectedQueue.value = profile.queue
  failedConnId.value = profile.failedConnId
  retryAfter.value = profile.retryAfter
  selectedJobIds.value = new Set()
  selectedFailedJobIds.value = new Set()
  await loadQueues()
  if (failedConnId.value) await loadFailedJobs()
}

function deleteProfile() {
  if (!selectedProfile.value) return
  profiles.value = profiles.value.filter(item => item.name !== selectedProfile.value)
  selectedProfile.value = ''
  persistProfiles()
  toast.success('Queue profile deleted')
}

function errorMessage(err: unknown, fallback: string) {
  const message = (err as { response?: { data?: { error?: string } } })?.response?.data?.error
  return message ? `${fallback}: ${message}` : fallback
}
</script>

<template>
  <div class="lq-view" :class="{ 'is-sidebar-collapsed': sidebarCollapsed }">
    <section v-if="!isRedis && !isSql" class="page-panel lq-empty">
      <div class="lq-empty__title">No connection selected</div>
      <div class="lq-empty__hint">Select a Redis connection to monitor queues, or a SQL database connection to browse failed jobs.</div>
      <div v-if="redisConnections.length" class="lq-picker">
        <label class="form-label">Redis Connection (full queue monitor)</label>
        <select class="base-input" @change="selectRedisConnection(($event.target as HTMLSelectElement).value)">
          <option value="">Select Redis connection</option>
          <option v-for="conn in redisConnections" :key="conn.id" :value="conn.id">
            {{ conn.name }} - {{ conn.host }}:{{ conn.port }}
          </option>
        </select>
      </div>
      <div v-if="sqlConnections.length" class="lq-picker" style="margin-top:12px">
        <label class="form-label">SQL Connection (failed jobs only)</label>
        <select class="base-input" @change="selectRedisConnection(($event.target as HTMLSelectElement).value)">
          <option value="">Select SQL connection</option>
          <option v-for="conn in sqlConnections" :key="conn.id" :value="conn.id">
            {{ conn.name }} ({{ conn.driver }})
          </option>
        </select>
      </div>
      <div v-if="!redisConnections.length && !sqlConnections.length" class="lq-muted">Create a connection first.</div>
    </section>

    <template v-else-if="isRedis || isSql">
      <aside class="lq-sidebar">
        <div class="lq-panel-header">
          <span class="lq-sidebar-title">{{ activeConn?.name }}</span>
          <div class="lq-sidebar-head-actions">
            <span class="lq-driver">{{ (({ postgres: 'PG', mysql: 'MY', mariadb: 'MB', mssql: 'MS', redis: 'RD' } as Record<string, string>)[activeConn?.driver ?? ''] ?? (activeConn?.driver ?? 'DB').slice(0,2).toUpperCase()) }}</span>
            <button class="lq-sidebar-toggle" :title="sidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'" @click="sidebarCollapsed = !sidebarCollapsed">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                <rect x="3" y="4" width="18" height="16" rx="2" />
                <path d="M9 4v16" />
                <path d="M6 8h.01M6 12h.01M6 16h.01" />
              </svg>
            </button>
          </div>
        </div>

        <div class="lq-sidebar-rail">
          <button class="lq-sidebar-rail-btn" title="Expand sidebar" @click="sidebarCollapsed = false">
            <span>{{ (({ postgres: 'PG', mysql: 'MY', mariadb: 'MB', mssql: 'MS', redis: 'RD' } as Record<string, string>)[activeConn?.driver ?? ''] ?? (activeConn?.driver ?? 'DB').slice(0,2).toUpperCase()) }}</span>
          </button>
        </div>

        <div class="lq-controls">
          <div class="form-group">
            <label class="form-label">Profile</label>
            <div class="lq-profile-row">
              <select v-model="selectedProfile" class="base-input" @change="applyProfile(selectedProfile)">
                <option value="">Select profile</option>
                <option v-for="profile in profiles" :key="profile.name" :value="profile.name">{{ profile.name }}</option>
              </select>
              <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!selectedProfile" @click="deleteProfile">Delete</button>
            </div>
          </div>
          <div class="lq-profile-row">
            <input v-model="profileName" class="base-input" placeholder="Profile name" @keydown.enter="saveProfile" />
            <button class="base-btn base-btn--primary base-btn--sm" @click="saveProfile">Save</button>
          </div>
          <template v-if="isRedis">
            <div class="form-group">
              <label class="form-label">Redis DB</label>
              <select v-model.number="selectedDb" class="base-input">
                <option v-for="db in redisDbIndexes" :key="db" :value="db">DB {{ db }}</option>
              </select>
            </div>
            <div class="form-group">
              <label class="form-label">Queue Prefix</label>
              <input v-model="prefix" class="base-input" placeholder="queues" @keydown.enter="loadQueues" />
            </div>
            <button class="base-btn base-btn--ghost base-btn--sm" :disabled="loadingQueues" @click="loadQueues">Refresh</button>
          </template>
          <template v-else>
            <div class="form-group">
              <label class="form-label">Redis connection for retry</label>
              <select v-model.number="retryRedisConnId" class="base-input" title="Pick a Redis connection to push retried jobs back into">
                <option :value="null">None (disable retry)</option>
                <option v-for="conn in redisConnections" :key="conn.id" :value="conn.id">{{ conn.name }}</option>
              </select>
            </div>
            <div v-if="retryRedisConnId" class="form-group">
              <label class="form-label">Redis DB</label>
              <select v-model.number="selectedDb" class="base-input">
                <option v-for="db in redisDbIndexes" :key="db" :value="db">DB {{ db }}</option>
              </select>
            </div>
            <div v-if="retryRedisConnId" class="form-group">
              <label class="form-label">Queue prefix</label>
              <input v-model="prefix" class="base-input" placeholder="queues" />
            </div>
          </template>
        </div>

        <div v-if="isRedis" class="lq-queue-list">
          <button
            v-for="queue in queues"
            :key="queue.name"
            class="lq-queue"
            :class="{ 'is-active': selectedQueue === queue.name }"
            @click="selectedQueue = queue.name"
          >
            <span class="lq-queue__name">{{ queue.name }}</span>
            <span class="lq-queue__meta">{{ queue.ready }} ready / {{ queue.delayed }} delayed / {{ queue.reserved }} reserved</span>
          </button>
          <div v-if="!loadingQueues && queues.length === 0" class="lq-muted">No Laravel queues found.</div>
        </div>
        <div v-else class="lq-queue-list">
          <div class="lq-muted" style="padding:10px 12px">Showing <strong>failed_jobs</strong> from <em>{{ activeConn?.name }}</em>.<br>Switch to a Redis connection to monitor live queues.</div>
        </div>
      </aside>

      <main class="lq-main">
        <div class="lq-toolbar">
          <div class="lq-toolbar__info">
            <span class="lq-title">{{ isSql ? 'Laravel Failed Jobs' : 'Laravel Queue' }}</span>
            <span v-if="isRedis" class="lq-meta">{{ prefix }}:{{ selectedQueue }}</span>
            <span v-else class="lq-meta">{{ activeConn?.name }} · failed_jobs</span>
          </div>
          <div class="lq-toolbar__actions">
            <label class="lq-auto">
              <input v-model="autoRefresh" type="checkbox" />
              <span>Auto</span>
            </label>
            <select v-model.number="refreshSeconds" class="base-input lq-refresh-select" :disabled="!autoRefresh">
              <option :value="5">5s</option>
              <option :value="10">10s</option>
              <option :value="30">30s</option>
            </select>
            <template v-if="isRedis">
              <span v-if="selectedSummary" class="lq-chip">{{ selectedSummary.ready }} ready</span>
              <span v-if="selectedSummary" class="lq-chip">{{ selectedSummary.delayed }} delayed</span>
              <span v-if="selectedSummary" class="lq-chip">{{ selectedSummary.reserved }} reserved</span>
              <span class="lq-chip" :class="{ 'lq-chip--danger': stuckJobs.length }">{{ stuckJobs.length }} stuck</span>
              <span v-if="queueAlerts.length" class="lq-chip lq-chip--danger">{{ queueAlerts.length }} alerts</span>
              <span class="lq-chip">oldest {{ oldestJobAge }}</span>
              <button class="base-btn base-btn--ghost base-btn--sm" :disabled="loadingJobs" @click="loadJobs">Refresh Jobs</button>
              <button class="base-btn base-btn--danger base-btn--sm" :disabled="!jobs.length || !canAction('clear')" @click="clearSelectedQueue">Clear Queue</button>
            </template>
            <template v-else>
              <span class="lq-chip" :class="{ 'lq-chip--danger': failedJobs.length > 0 }">{{ failedJobs.length }} failed</span>
              <button class="base-btn base-btn--ghost base-btn--sm" :disabled="loadingFailed" @click="loadFailedJobs">Refresh</button>
            </template>
            <span v-if="featureFlags.readOnly" class="lq-chip lq-chip--danger">read only</span>
          </div>
        </div>

        <div v-if="isRedis" class="lq-healthbar">
          <span class="lq-health">All queues ready <strong>{{ queueTotals.ready }}</strong></span>
          <span class="lq-health">Delayed <strong>{{ queueTotals.delayed }}</strong></span>
          <span class="lq-health">Reserved <strong>{{ queueTotals.reserved }}</strong></span>
          <span class="lq-health" :class="{ 'is-danger': stuckJobs.length }">Stuck <strong>{{ stuckJobs.length }}</strong></span>
          <span class="lq-health">Failed <strong>{{ failedJobs.length }}</strong></span>
          <span v-if="horizon?.detected" class="lq-health">Horizon keys <strong>{{ horizon.key_count }}</strong></span>
          <span v-if="horizon?.detected" class="lq-health">Supervisors <strong>{{ horizon.supervisors }}</strong></span>
          <span v-if="horizon?.detected" class="lq-health">Recent <strong>{{ horizon.recent_jobs }}</strong></span>
          <span v-if="!horizon?.detected" class="lq-health">Horizon <strong>not detected</strong></span>
        </div>

        <section class="lq-insights" :class="{ 'is-collapsed': insightsCollapsed }">
          <div class="lq-insight-tabs">
            <div class="lq-insight-tab-list">
              <button class="lq-insight-tab" :class="{ active: insightTab === 'health' }" @click="insightTab = 'health'; insightsCollapsed = false">Health</button>
              <button class="lq-insight-tab" :class="{ active: insightTab === 'timeline' }" @click="insightTab = 'timeline'; insightsCollapsed = false">Timeline</button>
              <button class="lq-insight-tab" :class="{ active: insightTab === 'groups' }" @click="insightTab = 'groups'; insightsCollapsed = false">Failed Groups</button>
              <button class="lq-insight-tab" :class="{ active: insightTab === 'patterns' }" @click="insightTab = 'patterns'; insightsCollapsed = false">Patterns</button>
              <button class="lq-insight-tab" :class="{ active: insightTab === 'quarantine' }" @click="insightTab = 'quarantine'; insightsCollapsed = false">Quarantine</button>
              <button class="lq-insight-tab" :class="{ active: insightTab === 'controls' }" @click="insightTab = 'controls'; insightsCollapsed = false">Controls</button>
              <button class="lq-insight-tab" :class="{ active: insightTab === 'settings' }" @click="insightTab = 'settings'; insightsCollapsed = false">Settings</button>
              <button class="lq-insight-tab" :class="{ active: insightTab === 'audit' }" @click="insightTab = 'audit'; insightsCollapsed = false; loadQueueAudit()">Audit</button>
            </div>
            <button class="lq-insights-toggle" :title="insightsCollapsed ? 'Show insights' : 'Hide insights'" @click="insightsCollapsed = !insightsCollapsed">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                <path :d="insightsCollapsed ? 'M6 9l6 6 6-6' : 'M18 15l-6-6-6 6'" />
              </svg>
              <span>{{ insightsCollapsed ? 'Show' : 'Hide' }}</span>
            </button>
          </div>

          <Transition name="lq-insights-slide">
            <div v-show="!insightsCollapsed" class="lq-insight-body">
          <div v-if="insightTab === 'health'" class="lq-insight-grid">
            <div v-if="queueAlerts.length" class="lq-insight is-warning">
              <div class="lq-insight__title">Notifications</div>
              <div class="lq-insight__detail">{{ queueAlerts.length }} active rule alert{{ queueAlerts.length === 1 ? '' : 's' }} can be sent to notification rules.</div>
              <button class="base-btn base-btn--primary base-btn--sm" @click="emitCurrentAlerts">Send Alerts</button>
            </div>
            <div v-for="issue in healthIssues" :key="issue.title" class="lq-insight" :class="`is-${issue.level}`">
              <div class="lq-insight__title">{{ issue.title }}</div>
              <div class="lq-insight__detail">{{ issue.detail }}</div>
            </div>
          </div>

          <div v-else-if="insightTab === 'timeline'" class="lq-timeline">
            <div v-if="timeline.length === 0" class="lq-muted">Timeline will populate after refreshes.</div>
            <div v-for="sample in timeline" :key="sample.at" class="lq-timeline-row">
              <span class="lq-timeline-time">{{ sample.at }}</span>
              <div class="lq-timeline-bars">
                <span v-if="sample.ready" class="lq-bar is-ready" :style="{ width: timelineWidth(sample.ready) }">{{ sample.ready }}</span>
                <span v-if="sample.delayed" class="lq-bar is-delayed" :style="{ width: timelineWidth(sample.delayed) }">{{ sample.delayed }}</span>
                <span v-if="sample.reserved" class="lq-bar is-reserved" :style="{ width: timelineWidth(sample.reserved) }">{{ sample.reserved }}</span>
                <span v-if="sample.failed" class="lq-bar is-failed" :style="{ width: timelineWidth(sample.failed) }">{{ sample.failed }}</span>
              </div>
            </div>
          </div>

          <div v-else-if="insightTab === 'groups'" class="lq-groups">
            <div class="lq-groups__toolbar">
              <span>Group by</span>
              <select v-model="failedGroupBy" class="base-input lq-group-select">
                <option value="job">Job</option>
                <option value="exception">Exception</option>
                <option value="queue">Queue</option>
                <option value="date">Date</option>
              </select>
            </div>
            <div v-if="failedGroups.length === 0" class="lq-muted">Load failed jobs to see groups.</div>
            <button
              v-for="group in failedGroups"
              :key="group.key"
              class="lq-group"
              @click="selectedFailedJobId = group.sample.id; activeState = 'failed'"
            >
              <span class="lq-group__name">{{ group.key }}</span>
              <span class="lq-group__meta">{{ group.count }} failed / latest {{ formatDate(group.latest) }}</span>
            </button>
          </div>

          <div v-if="insightTab === 'patterns'" class="lq-groups">
            <div v-if="failedPatterns.length === 0" class="lq-muted">No repeated failed-job patterns detected from loaded rows.</div>
            <button
              v-for="pattern in failedPatterns"
              :key="pattern.key"
              class="lq-group"
              @click="selectedFailedJobId = pattern.sample.id; activeState = 'failed'"
            >
              <span class="lq-group__name">{{ pattern.key }}</span>
              <span class="lq-group__meta">{{ pattern.count }} repeats / latest {{ formatDate(pattern.latest) }}</span>
            </button>
          </div>

          <div v-if="insightTab === 'quarantine'" class="lq-groups">
            <div v-if="quarantine.length === 0" class="lq-muted">No failed jobs in quarantine.</div>
            <button
              v-for="item in quarantine"
              :key="item.id"
              class="lq-group"
              @click="selectedFailedJobId = item.failed_job_id; activeState = 'failed'"
            >
              <span class="lq-group__name">{{ item.job_name || item.uuid || `failed_jobs #${item.failed_job_id}` }}</span>
              <span class="lq-group__meta">{{ item.queue }} / {{ formatDate(item.created_at) }}</span>
            </button>
          </div>

          <div v-if="insightTab === 'controls'" class="lq-ops-grid">
            <div class="lq-ops-card">
              <div class="lq-insight__title">Retry Sandbox</div>
              <div class="lq-insight__detail">Retry selected failed jobs into a non-production queue.</div>
              <input v-model="sandboxQueue" class="base-input" placeholder="debug" />
              <button class="base-btn base-btn--primary base-btn--sm" :disabled="!selectedFailedJob || !canAction('retry')" @click="retrySelectedFailedJob(selectedFailedJob, false, sandboxQueue)">Retry current to sandbox</button>
            </div>
            <div class="lq-ops-card">
              <div class="lq-insight__title">Horizon / Worker Control</div>
              <div class="lq-insight__detail">Laravel command execution is not connected yet. These controls are disabled until a Laravel agent/API is configured.</div>
              <div class="lq-control-buttons">
                <button class="base-btn base-btn--ghost base-btn--sm" @click="runAgentCommand('horizon:pause')">Pause</button>
                <button class="base-btn base-btn--ghost base-btn--sm" @click="runAgentCommand('horizon:continue')">Resume</button>
                <button class="base-btn base-btn--ghost base-btn--sm" @click="runAgentCommand('horizon:terminate')">Terminate</button>
                <button class="base-btn base-btn--ghost base-btn--sm" @click="runAgentCommand('queue:retry')">queue:retry</button>
                <button class="base-btn base-btn--ghost base-btn--sm" @click="runAgentCommand('queue:flush')">queue:flush</button>
              </div>
            </div>
          </div>

          <div v-if="insightTab === 'settings'" class="lq-settings">
            <div class="lq-settings-card">
              <div class="lq-insight__title">Feature Flags</div>
              <label><input v-model="featureFlags.readOnly" type="checkbox" /> Read-only mode</label>
              <label><input v-model="featureFlags.retry" type="checkbox" /> Enable retry actions</label>
              <label><input v-model="featureFlags.delete" type="checkbox" /> Enable delete actions</label>
              <label><input v-model="featureFlags.clear" type="checkbox" /> Enable clear queue</label>
              <label><input v-model="featureFlags.editedReplay" type="checkbox" /> Enable edited payload replay</label>
              <label><input v-model="featureFlags.requireConfirm" type="checkbox" /> Require confirmation</label>
            </div>
            <div class="lq-settings-card">
              <div class="lq-insight__title">Queue Rules</div>
              <label>Ready jobs &gt;<input v-model.number="queueRules.readyMax" class="base-input" type="number" min="0" /></label>
              <label>Failed jobs &gt;<input v-model.number="queueRules.failedMax" class="base-input" type="number" min="0" /></label>
              <label>Stuck jobs &gt;<input v-model.number="queueRules.stuckMax" class="base-input" type="number" min="0" /></label>
              <label>Oldest age minutes &gt;<input v-model.number="queueRules.oldestMinutesMax" class="base-input" type="number" min="0" /></label>
              <label><input v-model="queueRules.noConsumption" type="checkbox" /> Alert when no consumption is detected</label>
            </div>
            <div class="lq-settings-card">
              <div class="lq-insight__title">Business Context Presets</div>
              <div class="lq-insight__detail">Comma-separated fields highlighted in Main Data.</div>
              <textarea v-model="businessFieldsInput" class="base-input lq-small-editor" rows="4" />
            </div>
          </div>

          <div v-if="insightTab === 'audit'" class="lq-audit-list">
            <div v-if="queueAudit.length === 0" class="lq-muted">No Laravel Queue audit entries yet.</div>
            <div v-for="entry in queueAudit" :key="entry.id" class="lq-audit-item" :class="{ 'is-open': expandedAuditEntryId === entry.id }">
              <button class="lq-audit-row" @click="expandedAuditEntryId = expandedAuditEntryId === entry.id ? null : entry.id">
                <span class="lq-audit-caret">{{ expandedAuditEntryId === entry.id ? '-' : '+' }}</span>
                <span class="lq-audit-action">{{ entry.action }}</span>
                <span class="lq-audit-status" :data-status="entry.status">{{ entry.status }}</span>
                <span>{{ entry.queue || entry.target_queue || '-' }}</span>
                <span>{{ entry.failed_job_id || entry.job_uuid || '-' }}</span>
                <span>{{ entry.username || `user #${entry.user_id}` }}</span>
                <span>{{ formatDate(entry.created_at) }}</span>
              </button>
              <Transition name="lq-audit-slide">
                <div v-show="expandedAuditEntryId === entry.id" class="lq-audit-detail">
                  <div v-if="entry.error" class="lq-audit-error">{{ entry.error }}</div>
                  <pre>{{ formatAuditDetails(entry) }}</pre>
                </div>
              </Transition>
            </div>
          </div>
            </div>
          </Transition>
        </section>

        <div class="lq-filterbar">
          <input v-model="search" class="base-input lq-search" placeholder="Search UUID, class, handler, or payload" />
          <template v-if="isRedis">
            <label class="lq-retry-after">
              <span>Retry after</span>
              <input v-model.number="retryAfter" class="base-input" type="number" min="1" />
              <span>seconds</span>
            </label>
            <select v-model.number="failedConnId" class="base-input lq-failed-select" title="SQL connection for failed_jobs">
              <option :value="null">Failed jobs connection</option>
              <option v-for="conn in sqlConnections" :key="conn.id" :value="conn.id">{{ conn.name }}</option>
            </select>
            <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!failedConnId || loadingFailed" @click="loadFailedJobs">Load Failed</button>
          </template>
        </div>

        <div class="lq-tabs">
          <template v-if="isRedis">
            <button class="lq-tab" :class="{ active: activeState === 'all' }" @click="activeState = 'all'">All <span>{{ jobs.length }}</span></button>
            <button class="lq-tab" :class="{ active: activeState === 'ready' }" @click="activeState = 'ready'">Ready <span>{{ stateCount('ready') }}</span></button>
            <button class="lq-tab" :class="{ active: activeState === 'delayed' }" @click="activeState = 'delayed'">Delayed <span>{{ stateCount('delayed') }}</span></button>
            <button class="lq-tab" :class="{ active: activeState === 'reserved' }" @click="activeState = 'reserved'">Reserved <span>{{ stateCount('reserved') }}</span></button>
          </template>
          <button class="lq-tab" :class="{ active: activeState === 'failed' }" @click="activeState = 'failed'; loadFailedJobs()">Failed <span>{{ failedJobs.length }}</span></button>
        </div>

        <div v-if="isRedis && activeState !== 'failed' && selectedJobIds.size" class="lq-bulkbar">
          <span>{{ selectedJobIds.size }} selected</span>
          <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!canAction('retry')" @click="bulkRequeueJobs">Requeue selected</button>
          <button class="base-btn base-btn--danger base-btn--sm" :disabled="!canAction('delete')" @click="bulkDeleteJobs">Delete selected</button>
          <button class="base-btn base-btn--ghost base-btn--sm" @click="exportSelectedJobs">Export selected</button>
        </div>

        <div v-if="activeState === 'failed' && selectedFailedJobIds.size" class="lq-bulkbar">
          <span>{{ selectedFailedJobIds.size }} failed selected</span>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="!canAction('retry') || (isSql && !retryRedisConnId)" @click="bulkRetryFailed(false)">Retry selected</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="!canAction('retry') || !canAction('delete') || (isSql && !retryRedisConnId)" @click="bulkRetryFailed(true)">Retry & delete</button>
          <button class="base-btn base-btn--danger base-btn--sm" :disabled="!canAction('delete')" @click="bulkDeleteFailed">Delete failed</button>
          <button class="base-btn base-btn--ghost base-btn--sm" @click="quarantineSelectedFailed">Quarantine</button>
          <button class="base-btn base-btn--ghost base-btn--sm" @click="exportSelectedJobs">Export selected</button>
        </div>

        <div class="lq-exportbar">
          <button class="base-btn base-btn--ghost base-btn--sm" :disabled="activeState === 'failed' ? !filteredFailedJobs.length : !filteredJobs.length" @click="exportVisibleJobs">
            Export visible JSON
          </button>
        </div>

        <div class="lq-content">
          <section v-if="isRedis && activeState !== 'failed'" class="lq-jobs">
            <table class="lq-table">
              <thead>
                <tr>
                  <th class="lq-select-col">
                    <input type="checkbox" :checked="allVisibleJobsSelected" @change="toggleAllVisibleJobs(($event.target as HTMLInputElement).checked)" />
                  </th>
                  <th>State</th>
                  <th>Job</th>
                  <th>Attempts</th>
                  <th>Available At</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="job in filteredJobs"
                  :key="job.id"
                  :class="{ 'is-active': selectedJob?.id === job.id, 'is-stuck': isStuckJob(job) }"
                  @click="selectedJobId = job.id"
                >
                  <td class="lq-select-col" @click.stop>
                    <input type="checkbox" :checked="selectedJobIds.has(job.id)" @change="toggleJobSelection(job, ($event.target as HTMLInputElement).checked)" />
                  </td>
                  <td>
                    <span class="lq-state" :data-state="job.state">{{ job.state }}</span>
                    <span v-if="isStuckJob(job)" class="lq-state lq-state--stuck">stuck</span>
                  </td>
                  <td>
                    <div class="lq-job-name">{{ job.display_name || job.command_name || job.job || 'Laravel job' }}</div>
                    <div class="lq-job-sub">{{ job.uuid || job.id }}</div>
                  </td>
                  <td>{{ job.attempts }}</td>
                  <td>{{ formatDate(job.available_at) }}</td>
                </tr>
              </tbody>
            </table>
            <div v-if="!loadingJobs && filteredJobs.length === 0" class="lq-empty-inline">No jobs in this state.</div>
          </section>

          <section v-else class="lq-jobs">
            <table class="lq-table">
              <thead>
                <tr>
                  <th class="lq-select-col">
                    <input type="checkbox" :checked="allVisibleFailedJobsSelected" @change="toggleAllVisibleFailed(($event.target as HTMLInputElement).checked)" />
                  </th>
                  <th>ID</th>
                  <th>Queue</th>
                  <th>Job</th>
                  <th>Failed At</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="job in filteredFailedJobs"
                  :key="job.id"
                  :class="{ 'is-active': selectedFailedJob?.id === job.id }"
                  @click="selectedFailedJobId = job.id"
                >
                  <td class="lq-select-col" @click.stop>
                    <input type="checkbox" :checked="selectedFailedJobIds.has(job.id)" @change="toggleFailedSelection(job, ($event.target as HTMLInputElement).checked)" />
                  </td>
                  <td>{{ job.id }}</td>
                  <td>{{ job.queue }}</td>
                  <td>
                    <div class="lq-job-name">{{ job.payload?.displayName || job.payload?.job || job.uuid || 'Failed job' }}</div>
                    <div class="lq-job-sub">{{ job.connection }}</div>
                  </td>
                  <td>{{ formatDate(job.failed_at) }}</td>
                </tr>
              </tbody>
            </table>
            <div v-if="!loadingFailed && filteredFailedJobs.length === 0" class="lq-empty-inline">No failed jobs loaded.</div>
          </section>

          <aside class="lq-detail">
            <template v-if="activeState !== 'failed' && selectedJob">
              <div class="lq-detail__head">
                <div>
                  <div class="lq-detail__title">{{ selectedJob.display_name || selectedJob.command_name || 'Laravel job' }}</div>
                  <div class="lq-detail__sub">{{ selectedJob.uuid || selectedJob.id }}</div>
                </div>
                <span class="lq-state" :data-state="selectedJob.state">{{ selectedJob.state }}</span>
              </div>
              <div class="lq-detail__actions">
                <button class="base-btn base-btn--ghost base-btn--sm" @click="copyPayload(selectedJob)">Copy Payload</button>
                <button v-if="selectedJob.state !== 'ready'" class="base-btn base-btn--primary base-btn--sm" :disabled="!canAction('retry')" @click="requeueSelectedJob(selectedJob)">Requeue</button>
                <button class="base-btn base-btn--danger base-btn--sm" :disabled="!canAction('delete')" @click="removeJob(selectedJob)">Delete</button>
              </div>
              <div class="lq-detail-tabs">
                <button class="lq-detail-tab" :class="{ active: detailTab === 'summary' }" @click="detailTab = 'summary'">Summary</button>
                <button class="lq-detail-tab" :class="{ active: detailTab === 'main' }" @click="detailTab = 'main'">Main Data</button>
                <button class="lq-detail-tab" :class="{ active: detailTab === 'payload' }" @click="detailTab = 'payload'">Payload</button>
                <button class="lq-detail-tab" :class="{ active: detailTab === 'command' }" @click="detailTab = 'command'">Command</button>
                <button class="lq-detail-fullscreen" title="Open current tab fullscreen" @click="detailFullscreenOpen = true">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.1" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                    <path d="M8 3H5a2 2 0 0 0-2 2v3M16 3h3a2 2 0 0 1 2 2v3M8 21H5a2 2 0 0 1-2-2v-3M16 21h3a2 2 0 0 0 2-2v-3" />
                  </svg>
                </button>
              </div>
              <div v-if="detailTab === 'summary'" class="lq-props">
                <span>Queue</span><code>{{ selectedJob.queue }}</code>
                <span>Handler</span><code>{{ selectedJob.job || '-' }}</code>
                <span>Command</span><code>{{ selectedJob.command_name || '-' }}</code>
                <span>Attempts</span><code>{{ selectedJob.attempts }}</code>
                <span>Max Tries</span><code>{{ selectedJob.max_tries || '-' }}</code>
                <span>Timeout</span><code>{{ selectedJob.timeout ? `${selectedJob.timeout}s` : '-' }}</code>
                <span>Backoff</span><code>{{ selectedJob.backoff == null ? '-' : JSON.stringify(selectedJob.backoff) }}</code>
                <span>Available</span><code>{{ formatDate(selectedJob.available_at) }}</code>
                <span>Age</span><code>{{ selectedJob.score ? ageLabel(selectedJob.score) : '-' }}</code>
              </div>
              <pre v-if="detailTab === 'main'" class="lq-payload">{{ mainDataForJob(selectedJob) }}</pre>
              <pre v-if="detailTab === 'payload'" class="lq-payload">{{ formatPayload(selectedJob) }}</pre>
              <pre v-if="detailTab === 'command'" class="lq-payload">{{ commandData(selectedJob) || 'No command data found.' }}</pre>
            </template>
            <template v-else-if="activeState === 'failed' && selectedFailedJob">
              <div class="lq-detail__head">
                <div>
                  <div class="lq-detail__title">{{ selectedFailedJob.payload?.displayName || selectedFailedJob.payload?.job || 'Failed job' }}</div>
                  <div class="lq-detail__sub">{{ selectedFailedJob.uuid || `failed_jobs #${selectedFailedJob.id}` }}</div>
                </div>
                <span class="lq-state lq-state--failed">failed</span>
              </div>
              <div class="lq-detail__actions">
                <button class="base-btn base-btn--ghost base-btn--sm" @click="copyFailedPayload(selectedFailedJob)">Copy Payload</button>
                <button
                  class="base-btn base-btn--primary base-btn--sm"
                  :disabled="!canAction('retry') || (isSql && !retryRedisConnId)"
                  :title="isSql && !retryRedisConnId ? 'Select a Redis connection in the sidebar to enable retry' : undefined"
                  @click="retrySelectedFailedJob(selectedFailedJob, false)"
                >Retry</button>
                <button
                  class="base-btn base-btn--primary base-btn--sm"
                  :disabled="!canAction('retry') || !canAction('delete') || (isSql && !retryRedisConnId)"
                  :title="isSql && !retryRedisConnId ? 'Select a Redis connection in the sidebar to enable retry' : undefined"
                  @click="retrySelectedFailedJob(selectedFailedJob, true)"
                >Retry & Delete</button>
                <button class="base-btn base-btn--ghost base-btn--sm" @click="quarantineFailed(selectedFailedJob)">Quarantine</button>
                <button v-if="quarantinedFailedJobIds.has(selectedFailedJob.id)" class="base-btn base-btn--ghost base-btn--sm" @click="removeFromQuarantine(selectedFailedJob)">Unquarantine</button>
                <button class="base-btn base-btn--danger base-btn--sm" :disabled="!canAction('delete')" @click="removeFailedJob(selectedFailedJob)">Delete Failed</button>
              </div>
              <div class="lq-detail-tabs">
                <button class="lq-detail-tab" :class="{ active: detailTab === 'summary' }" @click="detailTab = 'summary'">Summary</button>
                <button class="lq-detail-tab" :class="{ active: detailTab === 'main' }" @click="detailTab = 'main'">Main Data</button>
                <button class="lq-detail-tab" :class="{ active: detailTab === 'payload' }" @click="detailTab = 'payload'">Payload</button>
                <button class="lq-detail-tab" :class="{ active: detailTab === 'command' }" @click="detailTab = 'command'">Command</button>
                <button class="lq-detail-tab" :class="{ active: detailTab === 'exception' }" @click="detailTab = 'exception'">Exception</button>
                <button class="lq-detail-fullscreen" title="Open current tab fullscreen" @click="detailFullscreenOpen = true">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.1" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                    <path d="M8 3H5a2 2 0 0 0-2 2v3M16 3h3a2 2 0 0 1 2 2v3M8 21H5a2 2 0 0 1-2-2v-3M16 21h3a2 2 0 0 0 2-2v-3" />
                  </svg>
                </button>
              </div>
              <div v-if="detailTab === 'summary'" class="lq-props">
                <span>ID</span><code>{{ selectedFailedJob.id }}</code>
                <span>Connection</span><code>{{ selectedFailedJob.connection }}</code>
                <span>Queue</span><code>{{ selectedFailedJob.queue }}</code>
                <span>Failed At</span><code>{{ formatDate(selectedFailedJob.failed_at) }}</code>
              </div>
              <pre v-if="detailTab === 'main'" class="lq-payload">{{ mainDataForFailedJob(selectedFailedJob) }}</pre>
              <div v-if="detailTab === 'payload'" class="lq-editor-block">
                <textarea v-model="editedFailedPayload" class="base-input lq-payload-editor" rows="14" />
                <div class="lq-editor-actions">
                  <button class="base-btn base-btn--ghost base-btn--sm" @click="editedFailedPayload = formatFailedPayload(selectedFailedJob)">Reset</button>
                  <button class="base-btn base-btn--primary base-btn--sm" :disabled="!canAction('editedReplay') || (isSql && !retryRedisConnId)" @click="retrySelectedFailedJob(selectedFailedJob, false)">Retry Edited</button>
                  <button class="base-btn base-btn--primary base-btn--sm" :disabled="!canAction('editedReplay') || !canAction('delete') || (isSql && !retryRedisConnId)" @click="retrySelectedFailedJob(selectedFailedJob, true)">Retry Edited & Delete</button>
                </div>
              </div>
              <pre v-if="detailTab === 'command'" class="lq-payload">{{ failedCommandData(selectedFailedJob) || 'No command data found.' }}</pre>
              <pre v-if="detailTab === 'exception'" class="lq-exception">{{ selectedFailedJob.exception }}</pre>
            </template>
            <div v-else class="lq-muted">Select a job to inspect the payload.</div>
          </aside>
        </div>
      </main>
    </template>

    <Teleport to="body">
      <div v-if="detailFullscreenOpen" class="lq-fullscreen-overlay" @click.self="detailFullscreenOpen = false">
        <div class="lq-fullscreen-panel">
          <div class="lq-fullscreen-head">
            <div>
              <div class="lq-fullscreen-title">{{ detailFullscreenTitle }}</div>
              <div class="lq-fullscreen-sub">{{ detailTab === 'main' ? 'Main Data' : detailTab === 'payload' ? 'Payload' : detailTab === 'command' ? 'Command' : detailTab === 'exception' ? 'Exception' : 'Summary' }}</div>
            </div>
            <div class="lq-fullscreen-actions">
              <button class="base-btn base-btn--ghost base-btn--sm" @click="detailFullscreenOpen = false">Close</button>
            </div>
          </div>

          <div class="lq-fullscreen-tabs">
            <button class="lq-detail-tab" :class="{ active: detailTab === 'summary' }" @click="detailTab = 'summary'">Summary</button>
            <button class="lq-detail-tab" :class="{ active: detailTab === 'main' }" @click="detailTab = 'main'">Main Data</button>
            <button class="lq-detail-tab" :class="{ active: detailTab === 'payload' }" @click="detailTab = 'payload'">Payload</button>
            <button class="lq-detail-tab" :class="{ active: detailTab === 'command' }" @click="detailTab = 'command'">Command</button>
            <button v-if="activeState === 'failed'" class="lq-detail-tab" :class="{ active: detailTab === 'exception' }" @click="detailTab = 'exception'">Exception</button>
          </div>

          <div class="lq-fullscreen-body">
            <template v-if="activeState !== 'failed' && selectedJob">
              <div v-if="detailTab === 'summary'" class="lq-props lq-props--fullscreen">
                <span>Queue</span><code>{{ selectedJob.queue }}</code>
                <span>Handler</span><code>{{ selectedJob.job || '-' }}</code>
                <span>Command</span><code>{{ selectedJob.command_name || '-' }}</code>
                <span>Attempts</span><code>{{ selectedJob.attempts }}</code>
                <span>Max Tries</span><code>{{ selectedJob.max_tries || '-' }}</code>
                <span>Timeout</span><code>{{ selectedJob.timeout ? `${selectedJob.timeout}s` : '-' }}</code>
                <span>Backoff</span><code>{{ selectedJob.backoff == null ? '-' : JSON.stringify(selectedJob.backoff) }}</code>
                <span>Available</span><code>{{ formatDate(selectedJob.available_at) }}</code>
                <span>Age</span><code>{{ selectedJob.score ? ageLabel(selectedJob.score) : '-' }}</code>
              </div>
              <pre v-if="detailTab === 'main'" class="lq-payload lq-payload--fullscreen">{{ mainDataForJob(selectedJob) }}</pre>
              <pre v-if="detailTab === 'payload'" class="lq-payload lq-payload--fullscreen">{{ formatPayload(selectedJob) }}</pre>
              <pre v-if="detailTab === 'command'" class="lq-payload lq-payload--fullscreen">{{ commandData(selectedJob) || 'No command data found.' }}</pre>
            </template>

            <template v-else-if="activeState === 'failed' && selectedFailedJob">
              <div v-if="detailTab === 'summary'" class="lq-props lq-props--fullscreen">
                <span>ID</span><code>{{ selectedFailedJob.id }}</code>
                <span>Connection</span><code>{{ selectedFailedJob.connection }}</code>
                <span>Queue</span><code>{{ selectedFailedJob.queue }}</code>
                <span>Failed At</span><code>{{ formatDate(selectedFailedJob.failed_at) }}</code>
              </div>
              <pre v-if="detailTab === 'main'" class="lq-payload lq-payload--fullscreen">{{ mainDataForFailedJob(selectedFailedJob) }}</pre>
              <div v-if="detailTab === 'payload'" class="lq-editor-block lq-editor-block--fullscreen">
                <textarea v-model="editedFailedPayload" class="base-input lq-payload-editor lq-payload-editor--fullscreen" />
                <div class="lq-editor-actions lq-editor-actions--fullscreen">
                  <button class="base-btn base-btn--ghost base-btn--sm" @click="editedFailedPayload = formatFailedPayload(selectedFailedJob)">Reset</button>
                  <button class="base-btn base-btn--primary base-btn--sm" :disabled="!canAction('editedReplay') || (isSql && !retryRedisConnId)" @click="retrySelectedFailedJob(selectedFailedJob, false)">Retry Edited</button>
                  <button class="base-btn base-btn--primary base-btn--sm" :disabled="!canAction('editedReplay') || !canAction('delete') || (isSql && !retryRedisConnId)" @click="retrySelectedFailedJob(selectedFailedJob, true)">Retry Edited & Delete</button>
                </div>
              </div>
              <pre v-if="detailTab === 'command'" class="lq-payload lq-payload--fullscreen">{{ failedCommandData(selectedFailedJob) || 'No command data found.' }}</pre>
              <pre v-if="detailTab === 'exception'" class="lq-exception lq-exception--fullscreen">{{ selectedFailedJob.exception }}</pre>
            </template>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.lq-view {
  display: grid;
  grid-template-columns: 260px minmax(0, 1fr);
  height: 100%;
  overflow: hidden;
  background: var(--bg-body);
  border-top: 1px solid var(--border);
  transition: grid-template-columns 0.22s var(--ease);
}

.lq-view.is-sidebar-collapsed {
  grid-template-columns: 48px minmax(0, 1fr);
}

.lq-empty {
  grid-column: 1 / -1;
  margin: 18px;
  padding: 18px;
}

.lq-empty__title,
.lq-title {
  color: var(--text-primary);
  font-size: 14px;
  font-weight: 700;
}

.lq-empty__hint {
  color: var(--text-secondary);
  font-size: 13px;
  margin-top: 6px;
  max-width: 420px;
}

.lq-picker {
  max-width: 360px;
  margin-top: 12px;
}

.lq-sidebar {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  border-right: 1px solid var(--border);
  background: var(--bg-surface);
  overflow: hidden;
  box-shadow: 2px 0 12px rgba(0,0,0,.03);
  transition: border-color 0.18s var(--ease), box-shadow 0.18s var(--ease);
}

.lq-panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  box-sizing: border-box;
  height: 56px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--border);
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 600;
  transition: padding 0.22s var(--ease);
}

.lq-sidebar-title {
  min-width: 0;
  overflow: hidden;
  opacity: 1;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition: opacity 0.16s var(--ease), max-width 0.22s var(--ease);
}

.lq-sidebar-head-actions {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}

.lq-sidebar-toggle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-elevated);
  color: var(--text-muted);
  font-size: 14px;
  line-height: 1;
  cursor: pointer;
  transition: background 0.16s var(--ease), color 0.16s var(--ease), transform 0.22s var(--ease);
}

.lq-sidebar-toggle svg {
  width: 15px;
  height: 15px;
}

.lq-sidebar-toggle:hover {
  color: var(--text-primary);
  background: var(--bg-hover);
}

.is-sidebar-collapsed .lq-panel-header {
  justify-content: center;
  padding: 12px 6px;
}

.is-sidebar-collapsed .lq-sidebar-title {
  max-width: 0;
  opacity: 0;
}

.is-sidebar-collapsed .lq-driver {
  width: 0;
  margin: 0;
  opacity: 0;
  overflow: hidden;
}

.lq-sidebar-rail {
  display: flex;
  max-height: 0;
  min-height: 0;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  overflow: hidden;
  padding: 0 6px;
  opacity: 0;
  transform: translateX(-6px);
  transition: max-height 0.22s var(--ease), opacity 0.18s var(--ease), padding 0.22s var(--ease), transform 0.22s var(--ease);
}

.is-sidebar-collapsed .lq-sidebar-rail {
  flex: 1;
  max-height: 360px;
  padding: 12px 6px;
  opacity: 1;
  transform: translateX(0);
}

.lq-sidebar-rail-btn {
  width: 32px;
  height: 32px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-elevated);
  color: var(--text-primary);
  font-size: 10px;
  font-weight: 800;
  cursor: pointer;
}

.lq-sidebar-rail-btn:hover {
  background: var(--bg-hover);
}

.lq-controls,
.lq-queue-list {
  transition: max-height 0.24s var(--ease), opacity 0.18s var(--ease), padding 0.22s var(--ease), transform 0.22s var(--ease), border-color 0.18s var(--ease);
}

.is-sidebar-collapsed .lq-controls,
.is-sidebar-collapsed .lq-queue-list {
  max-height: 0;
  overflow: hidden;
  padding-top: 0;
  padding-bottom: 0;
  border-color: transparent;
  opacity: 0;
  pointer-events: none;
  transform: translateX(-8px);
}

.lq-driver {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 18px;
  border-radius: 4px;
  background: #c6302b;
  color: #fff;
  font-size: 9px;
  font-weight: 700;
}

.lq-controls {
  display: grid;
  gap: 10px;
  max-height: 360px;
  padding: 12px;
  border-bottom: 1px solid var(--border);
}

.lq-profile-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
  align-items: center;
}

.lq-queue-list {
  display: flex;
  flex-direction: column;
  min-height: 0;
  max-height: 100%;
  flex: 1;
  overflow: auto;
  padding: 4px 0;
}

.lq-queue {
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: 8px 12px;
  border: 0;
  background: transparent;
  color: var(--text-primary);
  text-align: left;
  cursor: pointer;
}

.lq-queue:hover,
.lq-queue.is-active {
  background: color-mix(in srgb, var(--brand) 11%, var(--bg-surface));
}

.lq-queue__name {
  font-size: 12.5px;
  font-weight: 700;
}

.lq-queue__meta,
.lq-meta,
.lq-muted,
.lq-detail__sub,
.lq-job-sub {
  color: var(--text-muted);
  font-size: 12px;
}

.lq-main {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
  background: var(--bg-body);
}

.lq-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  box-sizing: border-box;
  height: 56px;
  padding: 12px 18px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-surface);
}

.lq-toolbar__info,
.lq-toolbar__actions {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.lq-chip {
  padding: 2px 8px;
  border: 1px solid var(--border);
  border-radius: 5px;
  background: var(--bg-elevated);
  color: var(--text-muted);
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
}

.lq-chip--danger {
  color: var(--danger);
  background: var(--danger-bg);
  border-color: color-mix(in srgb, var(--danger) 35%, var(--border));
}

.lq-auto {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--text-muted);
  font-size: 12px;
}

.lq-refresh-select {
  width: 68px;
  padding: 5px 8px;
  font-size: 12px;
}

.lq-healthbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  padding: 8px 14px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-surface);
}

.lq-health {
  padding: 3px 8px;
  border: 1px solid var(--border);
  border-radius: 5px;
  background: var(--bg-body);
  color: var(--text-muted);
  font-size: 11px;
}

.lq-health strong {
  color: var(--text-primary);
}

.lq-health.is-danger {
  color: var(--danger);
  background: var(--danger-bg);
}

.lq-insights {
  display: grid;
  gap: 10px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-body);
}

.lq-insights.is-collapsed {
  gap: 0;
  padding-block: 6px;
}

.lq-insight-tabs {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  min-height: 32px;
  border-bottom: 1px solid var(--border);
}

.lq-insights.is-collapsed .lq-insight-tabs {
  border-bottom: 0;
}

.lq-insight-tab-list {
  display: flex;
  align-items: center;
  min-width: 0;
  min-height: 32px;
  overflow-x: auto;
}

.lq-insight-tab {
  height: 32px;
  flex-shrink: 0;
  padding: 0 10px;
  border: 0;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--text-muted);
  font-size: 11.5px;
  cursor: pointer;
}

.lq-insight-tab.active {
  border-bottom-color: var(--brand);
  color: var(--text-primary);
}

.lq-insights-toggle {
  display: inline-flex;
  align-items: center;
  align-self: center;
  gap: 6px;
  height: 26px;
  flex-shrink: 0;
  padding: 0 9px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-elevated);
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 700;
  cursor: pointer;
}

.lq-insights-toggle:hover {
  color: var(--text-primary);
  background: var(--bg-hover);
}

.lq-insights-toggle svg {
  width: 13px;
  height: 13px;
}

.lq-insight-body {
  overflow: hidden;
}

.lq-insights-slide-enter-active,
.lq-insights-slide-leave-active {
  max-height: 520px;
  overflow: hidden;
  opacity: 1;
  transform: translateY(0);
  transition: max-height 0.24s var(--ease), opacity 0.18s var(--ease), transform 0.22s var(--ease);
}

.lq-insights-slide-enter-from,
.lq-insights-slide-leave-to {
  max-height: 0;
  opacity: 0;
  transform: translateY(-6px);
}

.lq-insight-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}

.lq-insight {
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-surface);
}

.lq-insight.is-danger {
  border-color: color-mix(in srgb, var(--danger) 35%, var(--border));
  background: var(--danger-bg);
}

.lq-insight.is-warning {
  border-color: color-mix(in srgb, var(--warning) 35%, var(--border));
  background: var(--warning-bg);
}

.lq-insight.is-ok {
  border-color: color-mix(in srgb, var(--success) 28%, var(--border));
  background: var(--success-bg);
}

.lq-insight__title {
  color: var(--text-primary);
  font-size: 12px;
  font-weight: 700;
}

.lq-insight__detail {
  margin-top: 4px;
  color: var(--text-muted);
  font-size: 11.5px;
  line-height: 1.4;
}

.lq-timeline {
  display: grid;
  gap: 7px;
}

.lq-timeline-row {
  display: grid;
  grid-template-columns: 72px minmax(0, 1fr);
  gap: 10px;
  align-items: center;
}

.lq-timeline-time {
  color: var(--text-muted);
  font-family: var(--mono);
  font-size: 11px;
}

.lq-timeline-bars {
  display: flex;
  gap: 4px;
  min-width: 0;
}

.lq-bar {
  min-width: 20px;
  height: 18px;
  padding: 2px 5px;
  border-radius: 4px;
  color: #fff;
  font-family: var(--mono);
  font-size: 10px;
  line-height: 14px;
}

.lq-bar.is-ready { background: var(--success); }
.lq-bar.is-delayed { background: var(--warning); }
.lq-bar.is-reserved { background: var(--brand); }
.lq-bar.is-failed { background: var(--danger); }

.lq-groups {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
}

.lq-groups__toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  grid-column: 1 / -1;
  color: var(--text-muted);
  font-size: 12px;
}

.lq-group-select {
  max-width: 150px;
  padding: 5px 8px;
  font-size: 12px;
}

.lq-group {
  min-width: 0;
  padding: 8px 10px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-surface);
  text-align: left;
  cursor: pointer;
}

.lq-group:hover {
  border-color: color-mix(in srgb, var(--brand) 36%, var(--border));
  background: color-mix(in srgb, var(--brand) 8%, var(--bg-surface));
}

.lq-group__name {
  display: block;
  overflow: hidden;
  color: var(--text-primary);
  font-family: var(--mono);
  font-size: 12px;
  font-weight: 700;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.lq-group__meta {
  display: block;
  margin-top: 3px;
  color: var(--text-muted);
  font-size: 11px;
}

.lq-ops-grid,
.lq-settings {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}

.lq-ops-card,
.lq-settings-card {
  display: grid;
  align-content: start;
  gap: 8px;
  min-width: 0;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-surface);
}

.lq-settings-card label {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 8px;
  align-items: center;
  color: var(--text-muted);
  font-size: 12px;
}

.lq-settings-card label:has(.base-input) {
  grid-template-columns: minmax(130px, auto) minmax(0, 1fr);
}

.lq-control-buttons {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.lq-small-editor {
  min-height: 76px;
  font-family: var(--mono);
  font-size: 12px;
  resize: vertical;
}

.lq-audit-list {
  display: grid;
  gap: 0;
  max-height: min(46vh, 420px);
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-surface);
}

.lq-audit-item {
  border-bottom: 1px solid var(--border);
}

.lq-audit-item:last-child {
  border-bottom: 0;
}

.lq-audit-row {
  display: grid;
  grid-template-columns: 24px 120px 80px minmax(120px, 1fr) 110px 120px 150px;
  gap: 10px;
  align-items: center;
  width: 100%;
  padding: 7px 10px;
  border: 0;
  background: transparent;
  color: var(--text-muted);
  font-size: 11.5px;
  text-align: left;
  cursor: pointer;
}

.lq-audit-row:hover,
.lq-audit-item.is-open .lq-audit-row {
  background: var(--bg-hover);
}

.lq-audit-caret {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  border: 1px solid var(--border);
  border-radius: 4px;
  color: var(--text-primary);
  font-family: var(--mono);
  font-size: 12px;
  font-weight: 800;
}

.lq-audit-action {
  color: var(--text-primary);
  font-family: var(--mono);
  font-weight: 700;
}

.lq-audit-status {
  font-weight: 700;
  text-transform: uppercase;
}

.lq-audit-status[data-status="failed"],
.lq-audit-status[data-status="blocked"] {
  color: var(--danger);
}

.lq-audit-status[data-status="success"] {
  color: var(--success);
}

.lq-audit-detail {
  padding: 0 10px 10px 44px;
}

.lq-audit-slide-enter-active,
.lq-audit-slide-leave-active {
  max-height: 280px;
  overflow: hidden;
  opacity: 1;
  transition: max-height 0.22s var(--ease), opacity 0.16s var(--ease), padding 0.22s var(--ease);
}

.lq-audit-slide-enter-from,
.lq-audit-slide-leave-to {
  max-height: 0;
  opacity: 0;
  padding-top: 0;
  padding-bottom: 0;
}

.lq-audit-detail pre {
  max-height: 220px;
  margin: 0;
  overflow: auto;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-body);
  color: var(--text-primary);
  font-family: var(--mono);
  font-size: 11.5px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
}

.lq-audit-error {
  margin-bottom: 8px;
  color: var(--danger);
  font-size: 12px;
}

.lq-filterbar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-surface);
}

.lq-search {
  max-width: 460px;
}

.lq-retry-after {
  display: flex;
  align-items: center;
  gap: 7px;
  color: var(--text-muted);
  font-size: 12px;
}

.lq-retry-after input {
  width: 78px;
  padding: 6px 8px;
}

.lq-failed-select {
  max-width: 240px;
}

.lq-tabs {
  display: flex;
  min-height: 32px;
  padding: 0 4px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-elevated);
}

.lq-bulkbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  min-height: 38px;
  padding: 7px 14px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-surface);
  color: var(--text-muted);
  font-size: 12px;
}

.lq-exportbar {
  display: flex;
  justify-content: flex-end;
  padding: 7px 14px 0;
}

.lq-tab {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 10px;
  border: 0;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--text-muted);
  font-size: 11.5px;
  cursor: pointer;
}

.lq-tab.active {
  border-bottom-color: var(--brand);
  background: var(--bg-surface);
  color: var(--text-primary);
}

.lq-tab span {
  color: var(--brand);
  font-weight: 700;
}

.lq-content {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(320px, 420px);
  gap: 14px;
  min-height: 0;
  flex: 1;
  padding: 14px;
  overflow: hidden;
}

.lq-jobs,
.lq-detail {
  min-height: 0;
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-surface);
}

.lq-table {
  width: 100%;
  border-collapse: collapse;
  font-family: var(--mono);
  font-size: 12.5px;
}

.lq-select-col {
  width: 34px;
  min-width: 34px;
  text-align: center;
}

.lq-select-col input {
  display: inline-block;
  margin: 0;
}

.lq-table th {
  position: sticky;
  top: 0;
  z-index: 2;
  padding: 7px 12px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-elevated);
  color: var(--text-muted);
  font-family: 'Inter', sans-serif;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.4px;
  text-align: left;
  text-transform: uppercase;
}

.lq-table td {
  padding: 8px 12px;
  border-bottom: 1px solid var(--border);
  color: var(--text-primary);
  vertical-align: middle;
}

.lq-table tbody tr {
  cursor: pointer;
}

.lq-table tbody tr:hover td,
.lq-table tbody tr.is-active td {
  background: color-mix(in srgb, var(--brand) 9%, var(--bg-surface));
}

.lq-table tbody tr.is-stuck td {
  background: color-mix(in srgb, var(--danger) 7%, var(--bg-surface));
}

.lq-state {
  display: inline-flex;
  padding: 2px 7px;
  border-radius: 5px;
  background: var(--bg-elevated);
  color: var(--text-muted);
  font-size: 10px;
  font-weight: 800;
  text-transform: uppercase;
}

.lq-state[data-state="ready"] { color: var(--success); background: var(--success-bg); }
.lq-state[data-state="delayed"] { color: var(--warning); background: var(--warning-bg); }
.lq-state[data-state="reserved"] { color: var(--brand); background: var(--brand-dim); }

.lq-state--stuck {
  margin-left: 6px;
  color: var(--danger);
  background: var(--danger-bg);
}

.lq-state--failed {
  color: var(--danger);
  background: var(--danger-bg);
}

.lq-job-name {
  max-width: 420px;
  overflow: hidden;
  color: var(--text-primary);
  font-family: var(--mono);
  font-size: 12.5px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.lq-empty-inline {
  padding: 24px;
  color: var(--text-muted);
  font-size: 13px;
}

.lq-detail {
  padding: 14px;
}

.lq-detail__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border);
}

.lq-detail__actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 0;
  border-bottom: 1px solid var(--border);
}

.lq-detail-tabs {
  display: flex;
  gap: 0;
  margin: 12px 0;
  border-bottom: 1px solid var(--border);
}

.lq-detail-tab {
  height: 30px;
  padding: 0 10px;
  border: 0;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--text-muted);
  font-size: 11.5px;
  cursor: pointer;
}

.lq-detail-tab.active {
  border-bottom-color: var(--brand);
  color: var(--text-primary);
}

.lq-detail-fullscreen {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  margin-left: auto;
  border: 0;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}

.lq-detail-fullscreen:hover {
  color: var(--text-primary);
  background: var(--bg-hover);
}

.lq-detail-fullscreen svg {
  width: 15px;
  height: 15px;
}

.lq-detail__title {
  color: var(--text-primary);
  font-size: 13px;
  font-weight: 700;
}

.lq-props {
  display: grid;
  grid-template-columns: 86px minmax(0, 1fr);
  gap: 8px 10px;
  padding: 12px 0;
  border-bottom: 1px solid var(--border);
  font-size: 12px;
}

.lq-props span {
  color: var(--text-muted);
}

.lq-props code {
  overflow: hidden;
  color: var(--text-primary);
  font-family: var(--mono);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.lq-payload {
  margin: 12px 0 0;
  min-height: 260px;
  overflow: auto;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-body);
  color: var(--text-primary);
  font-family: var(--mono);
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
}

.lq-exception {
  margin: 12px 0 0;
  max-height: 240px;
  overflow: auto;
  padding: 12px;
  border: 1px solid color-mix(in srgb, var(--danger) 28%, var(--border));
  border-radius: var(--r-sm);
  background: var(--danger-bg);
  color: var(--danger);
  font-family: var(--mono);
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
}

.lq-editor-block {
  display: grid;
  gap: 10px;
  margin-top: 12px;
}

.lq-payload-editor {
  min-height: 260px;
  overflow: auto;
  padding: 12px;
  background: var(--bg-body);
  color: var(--text-primary);
  font-family: var(--mono);
  font-size: 12px;
  line-height: 1.5;
  resize: vertical;
}

.lq-editor-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
}

.lq-editor-actions--fullscreen {
  flex: 0 0 auto;
  align-items: center;
  justify-content: flex-end;
}

.lq-editor-actions--fullscreen .base-btn {
  width: auto;
  min-width: 0;
  flex: 0 0 auto;
  white-space: nowrap;
}

.lq-fullscreen-overlay {
  position: fixed;
  inset: 0;
  z-index: 1000;
  display: flex;
  align-items: stretch;
  justify-content: center;
  padding: 24px;
  background: rgba(0, 0, 0, 0.62);
  backdrop-filter: blur(2px);
}

.lq-fullscreen-panel {
  display: flex;
  flex-direction: column;
  width: min(1180px, 96vw);
  min-height: 0;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-elevated);
  box-shadow: 0 24px 72px rgba(0, 0, 0, 0.5);
}

.lq-fullscreen-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 14px 18px;
  border-bottom: 1px solid var(--border);
}

.lq-fullscreen-title {
  color: var(--text-primary);
  font-size: 15px;
  font-weight: 700;
}

.lq-fullscreen-sub {
  margin-top: 3px;
  color: var(--text-muted);
  font-size: 12px;
}

.lq-fullscreen-actions {
  display: flex;
  gap: 8px;
}

.lq-fullscreen-tabs {
  display: flex;
  flex-shrink: 0;
  padding: 0 12px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-surface);
}

.lq-fullscreen-body {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 16px;
}

.lq-props--fullscreen {
  max-width: 900px;
  margin: 0;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  padding: 14px;
}

.lq-payload--fullscreen,
.lq-exception--fullscreen {
  min-height: 100%;
  max-height: none;
  margin: 0;
}

.lq-editor-block--fullscreen {
  min-height: 100%;
  height: auto;
  margin: 0;
  align-content: start;
}

.lq-payload-editor--fullscreen {
  min-height: min(62vh, 620px);
  flex: 0 0 auto;
  resize: none;
}

@media (max-width: 1000px) {
  .lq-view {
    display: flex;
    flex-direction: column;
    overflow: auto;
  }

  .lq-content {
    grid-template-columns: 1fr;
  }

  .lq-view.is-sidebar-collapsed {
    grid-template-columns: 1fr;
  }

  .lq-insight-grid,
  .lq-groups,
  .lq-ops-grid,
  .lq-settings,
  .lq-audit-row {
    grid-template-columns: 1fr;
  }

  .lq-sidebar {
    flex: 0 0 auto;
    min-height: 0;
    max-height: min(48vh, 420px);
    border-right: 0;
    border-bottom: 1px solid var(--border);
  }

  .lq-view.is-sidebar-collapsed .lq-sidebar {
    min-height: 48px;
  }

  .lq-view.is-sidebar-collapsed .lq-sidebar-rail {
    display: none;
  }

  .lq-panel-header,
  .lq-toolbar {
    height: auto;
    min-height: 56px;
  }

  .lq-main {
    overflow: visible;
  }

  .lq-toolbar__info,
  .lq-toolbar__actions,
  .lq-healthbar,
  .lq-bulkbar {
    flex-wrap: wrap;
  }

  .lq-toolbar,
  .lq-filterbar {
    align-items: stretch;
    flex-direction: column;
  }

  .lq-search,
  .lq-failed-select {
    max-width: none;
    width: 100%;
  }

  .lq-tabs,
  .lq-detail-tabs,
  .lq-fullscreen-tabs {
    overflow-x: auto;
    scrollbar-width: thin;
  }

  .lq-tab,
  .lq-detail-tab {
    flex-shrink: 0;
  }

  .lq-content {
    gap: 10px;
    padding: 10px;
    overflow: visible;
  }

  .lq-jobs,
  .lq-detail {
    max-height: none;
    overflow: auto;
  }

  .lq-table {
    min-width: 720px;
  }

  .lq-insight-tabs {
    align-items: stretch;
    flex-direction: column;
  }

  .lq-insight-tab-list {
    width: 100%;
    scrollbar-width: thin;
  }

  .lq-insights-toggle {
    justify-content: center;
  }

  .lq-audit-list {
    max-height: 360px;
  }

  .lq-audit-row {
    grid-template-columns: 24px minmax(120px, 1fr);
    align-items: start;
  }

  .lq-audit-row span:nth-child(n + 4) {
    grid-column: 2;
  }

  .lq-audit-detail {
    padding-left: 34px;
  }

  .lq-fullscreen-overlay {
    padding: 10px;
  }

  .lq-fullscreen-head {
    align-items: stretch;
    flex-direction: column;
  }

  .lq-fullscreen-tabs {
    overflow-x: auto;
  }

  .lq-fullscreen-panel {
    width: 100%;
  }

  .lq-fullscreen-body {
    padding: 12px;
  }

  .lq-props--fullscreen {
    max-width: none;
  }
}

@media (max-width: 640px) {
  .lq-panel-header,
  .lq-toolbar,
  .lq-healthbar,
  .lq-insights,
  .lq-filterbar,
  .lq-bulkbar,
  .lq-exportbar {
    padding-left: 10px;
    padding-right: 10px;
  }

  .lq-sidebar {
    max-height: 42vh;
  }

  .lq-profile-row,
  .lq-settings-card label,
  .lq-settings-card label:has(.base-input),
  .lq-retry-after {
    grid-template-columns: 1fr;
  }

  .lq-retry-after {
    display: grid;
    align-items: stretch;
  }

  .lq-retry-after input {
    width: 100%;
  }

  .lq-content {
    padding: 8px;
  }

  .lq-detail {
    padding: 10px;
  }

  .lq-detail__head,
  .lq-detail__actions,
  .lq-editor-actions,
  .lq-editor-actions--fullscreen {
    align-items: stretch;
    flex-direction: column;
  }

  .lq-detail__actions .base-btn,
  .lq-editor-actions .base-btn,
  .lq-fullscreen-actions .base-btn {
    width: 100%;
    justify-content: center;
  }

  .lq-props {
    grid-template-columns: 1fr;
  }

  .lq-props span {
    margin-top: 6px;
  }

  .lq-props span:first-child {
    margin-top: 0;
  }

  .lq-props code {
    white-space: normal;
    word-break: break-word;
  }

  .lq-payload,
  .lq-exception,
  .lq-payload-editor {
    min-height: 180px;
    font-size: 11.5px;
  }

  .lq-fullscreen-overlay {
    padding: 0;
  }

  .lq-fullscreen-panel {
    width: 100vw;
    height: 100dvh;
    border: 0;
    border-radius: 0;
  }

  .lq-fullscreen-head {
    padding: 12px;
  }

  .lq-fullscreen-title {
    font-size: 14px;
  }

  .lq-payload-editor--fullscreen {
    min-height: 52vh;
  }
}
</style>
