<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import { useToast } from '@/composables/useToast'

// ── Shareable link helpers ────────────────────────────────────────
function readURLParams() {
  const p = new URLSearchParams(window.location.search)
  return {
    range: p.get('range') ?? '5m',
    field: p.get('field') ?? '',
    group: p.get('group') ?? '',
    index: p.get('index') ?? '',
    customFrom: p.get('from') ?? '',
    customTo: p.get('to') ?? '',
  }
}
function updateURL() {
  const p = new URLSearchParams()
  if (isCustomRange.value) {
    p.set('from', customFrom.value)
    p.set('to', customTo.value)
  } else {
    p.set('range', timeRange.value)
  }
  p.set('field', activeField.value)
  p.set('group', activeGroup.value)
  p.set('index', selectedIndex.value.value)
  history.replaceState(null, '', `${window.location.pathname}?${p.toString()}`)
}
async function copyShareLink() {
  try {
    updateURL()
    await navigator.clipboard.writeText(window.location.href)
    toast.success('Link copied!')
  } catch { toast.error('Failed to copy') }
}

const props = defineProps<{ activeConnId: number | null }>()
const toast = useToast()
const { connections } = useConnections()

const supportedDrivers = ['elasticsearch', 'opensearch']
const activeConn = computed(() =>
  props.activeConnId != null ? connections.value.find(c => c.id === props.activeConnId) ?? null : null
)
const isSupported = computed(() => !!activeConn.value && supportedDrivers.includes(activeConn.value.driver ?? ''))

// ── Metric presets (mirrors Kibana's metric dropdown) ─────────────
const metricOptions = [
  { label: 'CPU usage', field: 'system.cpu.total.pct', unit: '%', fmt: 'pct' },
  { label: 'Memory usage', field: 'system.memory.actual.used.pct', unit: '%', fmt: 'pct' },
  { label: 'Disk usage', field: 'system.filesystem.used.pct', unit: '%', fmt: 'pct' },
  { label: 'Inbound traffic', field: 'system.network.in.bytes', unit: 'B/s', fmt: 'bytes' },
  { label: 'Outbound traffic', field: 'system.network.out.bytes', unit: 'B/s', fmt: 'bytes' },
  { label: 'Load (1m)', field: 'system.load.1', unit: '', fmt: 'num' },
  { label: 'ES Heap used', field: 'elasticsearch.node.stats.jvm.mem.heap.used.pct', unit: '%', fmt: 'pct' },
  { label: 'ES CPU', field: 'elasticsearch.node.stats.os.cpu.percent', unit: '%', fmt: 'num' },
  { label: 'Custom…', field: '__custom__', unit: '', fmt: 'num' },
]

const groupOptions = [
  { label: 'Hosts', field: 'host.name' },
  { label: 'Pods', field: 'kubernetes.pod.name' },
  { label: 'Containers', field: 'container.name' },
  { label: 'Services', field: 'service.name' },
  { label: 'Custom…', field: '__custom__' },
]

const indexOptions = [
  { label: 'metricbeat-*', value: 'metricbeat-*' },
  { label: 'metrics-* (Elastic Agent)', value: '.ds-metrics-*,metrics-*' },
  { label: 'Both', value: 'metricbeat-*,.ds-metrics-*,metrics-*' },
]

// ── Filter/sort/config state ──────────────────────────────────────
const search = ref('')
const selectedMetric = ref(metricOptions[0])
const customField = ref('')
const customGroup = ref('')
const selectedGroup = ref(groupOptions[0])
const selectedIndex = ref(indexOptions[0])
const timeRange = ref('5m')
const timeRanges = [
  { value: '1m', label: '1m' },
  { value: '5m', label: '5m' },
  { value: '15m', label: '15m' },
  { value: '1h', label: '1h' },
  { value: '6h', label: '6h' },
  { value: '24h', label: '24h' },
]
const sortCol = ref<'name' | 'last_1m' | 'avg' | 'max'>('last_1m')
const sortDir = ref<'asc' | 'desc'>('desc')
const pageSize = ref(25)
const currentPage = ref(1)
const showMetricMenu = ref(false)
const showGroupMenu = ref(false)
const showIndexMenu = ref(false)
const showCustomPicker = ref(false)
const isCustomRange = ref(false)
const customFrom = ref('')
const customTo = ref('')
const viewMode = ref<'table' | 'heatmap'>('table')
const anomalyOnly = ref(false)
const compareMode = ref(false)
const selectedForCompare = ref<string[]>([])
const showAlertsPanel = ref(false)
const showAnnotationForm = ref(false)

function defaultCustomFrom() {
  const d = new Date()
  d.setHours(d.getHours() - 1)
  return d.toISOString().slice(0, 16)
}
function defaultCustomTo() {
  return new Date().toISOString().slice(0, 16)
}
function openCustomPicker() {
  if (!customFrom.value) customFrom.value = defaultCustomFrom()
  if (!customTo.value) customTo.value = defaultCustomTo()
  showCustomPicker.value = true
  showMetricMenu.value = false
  showGroupMenu.value = false
  showIndexMenu.value = false
}
function applyCustomRange() {
  if (!customFrom.value || !customTo.value) return
  isCustomRange.value = true
  showCustomPicker.value = false
  currentPage.value = 1
  loadInventory()
}
function clearCustomRange() {
  isCustomRange.value = false
  showCustomPicker.value = false
  loadInventory()
}

const activeField = computed(() =>
  selectedMetric.value.field === '__custom__' ? customField.value : selectedMetric.value.field
)
const activeGroup = computed(() =>
  selectedGroup.value.field === '__custom__' ? customGroup.value : selectedGroup.value.field
)
const activeUnit = computed(() => selectedMetric.value.unit)
const activeFmt = computed(() => selectedMetric.value.fmt)

// ── Data ──────────────────────────────────────────────────────────
interface HostRow {
  name: string
  last_1m: number | null
  avg: number | null
  max: number | null
  latest_ts: string
}

const rows = ref<HostRow[]>([])
const total = ref(0)
const loading = ref(false)
const lastRefreshed = ref<Date | null>(null)
const autoRefresh = ref(true)
let refreshTimer: ReturnType<typeof setInterval> | null = null

async function loadInventory() {
  if (!props.activeConnId || !isSupported.value) return
  if (!activeField.value || !activeGroup.value) return
  loading.value = true
  try {
    const params: Record<string, string> = {
      index: selectedIndex.value.value,
      field: activeField.value,
      group: activeGroup.value,
    }
    if (isCustomRange.value && customFrom.value && customTo.value) {
      params.from = new Date(customFrom.value).toISOString()
      params.to = new Date(customTo.value).toISOString()
    } else {
      params.range = timeRange.value
    }
    if (search.value) params.q = search.value
    const { data } = await axios.post<{ rows: HostRow[]; total: number }>(
      `/api/connections/${props.activeConnId}/search/metrics-inventory`,
      null,
      { params }
    )
    rows.value = data.rows ?? []
    total.value = data.total ?? 0
    lastRefreshed.value = new Date()
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to load inventory')
    rows.value = []
  } finally {
    loading.value = false
  }
}

// ── Sorted + paginated rows ───────────────────────────────────────
const pinnedRows = computed(() => rows.value.filter(r => pinnedHosts.value.has(r.name)))
const unpinnedRows = computed(() => rows.value.filter(r => !pinnedHosts.value.has(r.name)))

const sortedRows = computed(() => {
  let list = anomalyOnly.value
    ? unpinnedRows.value.filter(r => anomalyHosts.value.has(r.name))
    : [...unpinnedRows.value]
  list.sort((a, b) => {
    let av: any, bv: any
    if (sortCol.value === 'name') { av = a.name; bv = b.name }
    else { av = a[sortCol.value] ?? -Infinity; bv = b[sortCol.value] ?? -Infinity }
    if (av < bv) return sortDir.value === 'asc' ? -1 : 1
    if (av > bv) return sortDir.value === 'asc' ? 1 : -1
    return 0
  })
  return list
})

const totalPages = computed(() => Math.max(1, Math.ceil(sortedRows.value.length / pageSize.value)))
const pagedRows = computed(() => {
  const s = (currentPage.value - 1) * pageSize.value
  return sortedRows.value.slice(s, s + pageSize.value)
})

function setSort(col: typeof sortCol.value) {
  if (sortCol.value === col) sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc'
  else { sortCol.value = col; sortDir.value = 'desc' }
}

function sortIcon(col: typeof sortCol.value) {
  if (sortCol.value !== col) return '↕'
  return sortDir.value === 'asc' ? '↑' : '↓'
}

// ── Format helpers ────────────────────────────────────────────────
function fmtValue(v: number | null, fmt: string, unit: string): string {
  if (v == null) return '—'
  if (fmt === 'pct') return (v * 100 < 1 ? (v * 100).toFixed(2) : (v * 100).toFixed(1)) + '%'
  if (fmt === 'bytes') {
    if (v >= 1e9) return (v / 1e9).toFixed(1) + ' GB/s'
    if (v >= 1e6) return (v / 1e6).toFixed(1) + ' MB/s'
    if (v >= 1e3) return (v / 1e3).toFixed(1) + ' KB/s'
    return v.toFixed(0) + ' B/s'
  }
  if (v >= 1e6) return (v / 1e6).toFixed(2) + 'M' + (unit ? ' ' + unit : '')
  if (v >= 1e3) return (v / 1e3).toFixed(2) + 'K' + (unit ? ' ' + unit : '')
  return (Number.isInteger(v) ? v : v.toFixed(2)) + (unit ? ' ' + unit : '')
}

function pctWidth(v: number | null, fmt: string): number {
  if (v == null) return 0
  if (fmt === 'pct') return Math.min(100, v * 100)
  return 0 // no bar for non-percentage metrics
}

function pctColor(p: number): string {
  if (p >= 90) return '#ef4444'
  if (p >= 75) return '#f59e0b'
  return '#3b82f6'
}

function timeSince(ts: string): string {
  if (!ts) return '—'
  const diff = Math.floor((Date.now() - new Date(ts).getTime()) / 1000)
  if (diff < 60) return `${diff}s ago`
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  return `${Math.floor(diff / 3600)}h ago`
}

// ── Auto-refresh ──────────────────────────────────────────────────
function startAutoRefresh() {
  stopAutoRefresh()
  if (autoRefresh.value) refreshTimer = setInterval(loadInventory, 30_000)
}
function stopAutoRefresh() {
  if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null }
}

// ── Pinned hosts ──────────────────────────────────────────────────
const PINNED_KEY = 'nias:infra-pinned'
const pinnedHosts = ref<Set<string>>(new Set(JSON.parse(localStorage.getItem(PINNED_KEY) ?? '[]')))
function togglePin(name: string) {
  if (pinnedHosts.value.has(name)) pinnedHosts.value.delete(name)
  else pinnedHosts.value.add(name)
  localStorage.setItem(PINNED_KEY, JSON.stringify([...pinnedHosts.value]))
}

// ── Sparklines ────────────────────────────────────────────────────
const sparklines = ref<Record<string, number[]>>({})
async function loadSparklines(hostList: string[]) {
  if (!props.activeConnId || !hostList.length) return
  try {
    const body: Record<string, any> = {
      hosts: hostList,
      field: activeField.value,
      group: activeGroup.value,
      index: selectedIndex.value.value,
    }
    if (isCustomRange.value && customFrom.value && customTo.value) {
      body.from = new Date(customFrom.value).toISOString()
      body.to = new Date(customTo.value).toISOString()
    } else {
      body.range = timeRange.value
    }
    const { data } = await axios.post<{ sparklines: Record<string, number[]> }>(
      `/api/connections/${props.activeConnId}/search/metrics-sparklines`, body
    )
    sparklines.value = data.sparklines ?? {}
  } catch { /* silent */ }
}

// ── Anomaly detection (z-score) ───────────────────────────────────
const anomalyHosts = computed<Set<string>>(() => {
  const vals = rows.value.map(r => r.last_1m).filter((v): v is number => v != null)
  if (vals.length < 3) return new Set()
  const mean = vals.reduce((a, b) => a + b, 0) / vals.length
  const stddev = Math.sqrt(vals.reduce((a, b) => a + (b - mean) ** 2, 0) / vals.length)
  if (stddev === 0) return new Set()
  return new Set(rows.value.filter(r => r.last_1m != null && Math.abs((r.last_1m - mean) / stddev) > 2.5).map(r => r.name))
})

// ── Host comparison ───────────────────────────────────────────────
interface CompareSeriesEntry { ts: number; [host: string]: number }
const compareData = ref<Record<string, ChartResult[]>>({})
const compareLoading = ref(false)
const compareCategory = ref('CPU')
const showComparePanel = ref(false)

async function runComparison() {
  if (!props.activeConnId || selectedForCompare.value.length < 2) return
  compareLoading.value = true
  showComparePanel.value = true
  compareData.value = {}
  await Promise.all(selectedForCompare.value.map(async host => {
    try {
      const params: Record<string, string> = { host, group: activeGroup.value, index: selectedIndex.value.value }
      if (detailIsCustomRange.value && detailCustomFrom.value && detailCustomTo.value) {
        params.from = new Date(detailCustomFrom.value).toISOString()
        params.to = new Date(detailCustomTo.value).toISOString()
      } else {
        params.range = detailRange.value
      }
      const { data } = await axios.get<HostDetailData>(`/api/connections/${props.activeConnId}/search/metrics-host-detail`, { params })
      compareData.value[host] = data.categories?.find(c => c.name === compareCategory.value)?.charts ?? []
    } catch { /* skip */ }
  }))
  compareLoading.value = false
}
function toggleCompare(name: string) {
  const idx = selectedForCompare.value.indexOf(name)
  if (idx >= 0) selectedForCompare.value.splice(idx, 1)
  else if (selectedForCompare.value.length < 6) selectedForCompare.value.push(name)
}

const COMPARE_COLORS = ['#6366f1', '#22c55e', '#f59e0b', '#ef4444', '#06b6d4', '#a855f7']

// ── Alert rules ───────────────────────────────────────────────────
interface AlertRule { id: number; name: string; metric_field: string; threshold: number; comparison: string; duration_min: number; enabled: boolean }
const alertRules = ref<AlertRule[]>([])
const alertForm = ref({ name: '', metric_field: 'system.cpu.total.pct', threshold: 80, comparison: 'gt', duration_min: 5 })
const alertFormLoading = ref(false)

async function loadAlertRules() {
  if (!props.activeConnId) return
  try {
    const { data } = await axios.get<AlertRule[]>(`/api/connections/${props.activeConnId}/infra-alert-rules`)
    alertRules.value = data ?? []
  } catch { /* silent */ }
}
async function createAlertRule() {
  if (!props.activeConnId || !alertForm.value.name) return
  alertFormLoading.value = true
  try {
    await axios.post(`/api/connections/${props.activeConnId}/infra-alert-rules`, alertForm.value)
    alertForm.value = { name: '', metric_field: 'system.cpu.total.pct', threshold: 80, comparison: 'gt', duration_min: 5 }
    await loadAlertRules()
    toast.success('Alert rule created')
  } catch (e: any) { toast.error(e?.response?.data?.error ?? 'Failed') } finally { alertFormLoading.value = false }
}
async function deleteAlertRule(id: number) {
  if (!props.activeConnId) return
  try {
    await axios.delete(`/api/connections/${props.activeConnId}/infra-alert-rules/${id}`)
    await loadAlertRules()
  } catch { toast.error('Failed to delete') }
}
async function toggleAlertRule(id: number) {
  if (!props.activeConnId) return
  try {
    await axios.patch(`/api/connections/${props.activeConnId}/infra-alert-rules/${id}/toggle`)
    await loadAlertRules()
  } catch { toast.error('Failed to toggle') }
}

// ── Annotations ───────────────────────────────────────────────────
interface Annotation { id: number; title: string; color: string; event_time: string; description: string }
const annotations = ref<Annotation[]>([])
const annotationForm = ref({ title: '', description: '', color: '#6366f1', event_time: new Date().toISOString().slice(0, 16) })
const annotationFormLoading = ref(false)

async function loadAnnotations() {
  if (!props.activeConnId) return
  try {
    const { data } = await axios.get<Annotation[]>(`/api/connections/${props.activeConnId}/infra-annotations`)
    annotations.value = data ?? []
  } catch { /* silent */ }
}
async function createAnnotation() {
  if (!props.activeConnId || !annotationForm.value.title) return
  annotationFormLoading.value = true
  try {
    await axios.post(`/api/connections/${props.activeConnId}/infra-annotations`, {
      ...annotationForm.value,
      event_time: new Date(annotationForm.value.event_time).toISOString(),
    })
    showAnnotationForm.value = false
    await loadAnnotations()
    toast.success('Annotation added')
  } catch (e: any) { toast.error(e?.response?.data?.error ?? 'Failed') } finally { annotationFormLoading.value = false }
}
async function deleteAnnotation(id: number) {
  if (!props.activeConnId) return
  try {
    await axios.delete(`/api/connections/${props.activeConnId}/infra-annotations/${id}`)
    await loadAnnotations()
  } catch { toast.error('Failed to delete') }
}

function annotationXPos(ts: string, buckets: BucketValue[]): number | null {
  if (!buckets.length) return null
  const t = new Date(ts).getTime()
  const first = buckets[0].key as number
  const last = buckets[buckets.length - 1].key as number
  if (t < first || t > last) return null
  const W = CHART_W - CHART_PAD_L - CHART_PAD_R
  return CHART_PAD_L + ((t - first) / Math.max(last - first, 1)) * W
}

// ── Lifecycle ─────────────────────────────────────────────────────
onMounted(async () => {
  // Restore shareable URL params
  const p = readURLParams()
  if (p.customFrom && p.customTo) {
    customFrom.value = p.customFrom; customTo.value = p.customTo; isCustomRange.value = true
  } else if (p.range) {
    timeRange.value = p.range
  }
  if (p.field) {
    const m = metricOptions.find(o => o.field === p.field)
    if (m) selectedMetric.value = m
  }
  if (p.group) {
    const g = groupOptions.find(o => o.field === p.group)
    if (g) selectedGroup.value = g
  }
  if (p.index) {
    const ix = indexOptions.find(o => o.value === p.index)
    if (ix) selectedIndex.value = ix
  }
  if (props.activeConnId && isSupported.value) {
    await loadInventory()
    startAutoRefresh()
    loadAlertRules()
    loadAnnotations()
  }
})
onBeforeUnmount(() => stopAutoRefresh())

watch(() => props.activeConnId, async () => {
  rows.value = []
  stopAutoRefresh()
  if (props.activeConnId && isSupported.value) {
    await loadInventory()
    startAutoRefresh()
    loadAlertRules()
    loadAnnotations()
  }
})
watch(autoRefresh, v => v ? startAutoRefresh() : stopAutoRefresh())
watch([selectedMetric, selectedGroup, selectedIndex, timeRange], () => {
  currentPage.value = 1
  loadInventory()
})
watch(search, () => { currentPage.value = 1; loadInventory() })
watch(rows, newRows => {
  updateURL()
  loadSparklines(newRows.map(r => r.name))
})

// ── Host detail drilldown ─────────────────────────────────────────
interface BucketValue { key: number; key_as_string: string; [field: string]: any }
interface ChartResult {
  category: string
  key: string
  label: string
  fields: { key: string; unit: string }[]
  buckets: BucketValue[]
}
interface CategoryResult {
  name: string
  charts: ChartResult[]
}
interface HostDetailData {
  host: string
  range: string
  interval: string
  categories: CategoryResult[]
}

const detailHost = ref<string | null>(null)
const detailData = ref<HostDetailData | null>(null)
const detailLoading = ref(false)
const detailCategory = ref<string>('CPU')
const detailRange = ref(timeRange.value)
const detailTimeRanges = [
  { value: '15m', label: '15m' },
  { value: '1h', label: '1h' },
  { value: '3h', label: '3h' },
  { value: '6h', label: '6h' },
  { value: '24h', label: '24h' },
  { value: '7d', label: '7d' },
]
const detailIsCustomRange = ref(false)
const detailCustomFrom = ref('')
const detailCustomTo = ref('')
const showDetailCustomPicker = ref(false)

function openDetailCustomPicker() {
  if (!detailCustomFrom.value) detailCustomFrom.value = defaultCustomFrom()
  if (!detailCustomTo.value) detailCustomTo.value = defaultCustomTo()
  showDetailCustomPicker.value = true
}
function applyDetailCustomRange() {
  if (!detailCustomFrom.value || !detailCustomTo.value) return
  detailIsCustomRange.value = true
  showDetailCustomPicker.value = false
  loadHostDetail()
}
function clearDetailCustomRange() {
  detailIsCustomRange.value = false
  showDetailCustomPicker.value = false
  loadHostDetail()
}

const detailCategories = computed(() => detailData.value?.categories.map(c => c.name) ?? [])
const activeCharts = computed(() =>
  detailData.value?.categories.find(c => c.name === detailCategory.value)?.charts ?? []
)

async function openHostDetail(hostName: string) {
  detailHost.value = hostName
  detailData.value = null
  detailCategory.value = 'CPU'
  await loadHostDetail()
}

function closeHostDetail() {
  detailHost.value = null
  detailData.value = null
}

async function loadHostDetail() {
  if (!props.activeConnId || !detailHost.value) return
  detailLoading.value = true
  try {
    const params: Record<string, string> = {
      host: detailHost.value,
      group: activeGroup.value,
      index: selectedIndex.value.value,
    }
    if (detailIsCustomRange.value && detailCustomFrom.value && detailCustomTo.value) {
      params.from = new Date(detailCustomFrom.value).toISOString()
      params.to = new Date(detailCustomTo.value).toISOString()
    } else {
      params.range = detailRange.value
    }
    const { data } = await axios.get<HostDetailData>(
      `/api/connections/${props.activeConnId}/search/metrics-host-detail`,
      { params }
    )
    detailData.value = data
    if (data.categories?.length && !data.categories.find(c => c.name === detailCategory.value)) {
      detailCategory.value = data.categories[0].name
    }
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to load host detail')
  } finally {
    detailLoading.value = false
  }
}

watch(detailRange, () => loadHostDetail())

// ── Detail tabs: processes + correlation ──────────────────────────
const detailTab = ref<'metrics' | 'processes' | 'correlation'>('metrics')
interface ProcessRow { name: string; pid: number; state: string; cpu_pct: number | null; mem_bytes: number | null }
const processData = ref<ProcessRow[]>([])
const processLoading = ref(false)
const processSort = ref<'cpu' | 'mem'>('cpu')

async function loadProcessList() {
  if (!props.activeConnId || !detailHost.value) return
  processLoading.value = true
  try {
    const params: Record<string, string> = {
      host: detailHost.value, group: activeGroup.value,
      index: selectedIndex.value.value, sort: processSort.value,
    }
    if (detailIsCustomRange.value && detailCustomFrom.value && detailCustomTo.value) {
      params.from = new Date(detailCustomFrom.value).toISOString()
      params.to = new Date(detailCustomTo.value).toISOString()
    } else { params.range = detailRange.value }
    const { data } = await axios.get<{ processes: ProcessRow[] }>(
      `/api/connections/${props.activeConnId}/search/metrics-process-list`, { params }
    )
    processData.value = data.processes ?? []
  } catch (e: any) { toast.error('Failed to load process list') } finally { processLoading.value = false }
}

interface CorrelationRow { field: string; label: string; before_avg: number; after_avg: number; pct_change: number }
const correlationData = ref<CorrelationRow[]>([])
const correlationLoading = ref(false)

async function loadCorrelation() {
  if (!props.activeConnId || !detailHost.value) return
  correlationLoading.value = true
  try {
    const params: Record<string, string> = {
      host: detailHost.value, group: activeGroup.value,
      index: selectedIndex.value.value,
      anchor_time: new Date().toISOString(),
    }
    if (detailIsCustomRange.value && detailCustomFrom.value && detailCustomTo.value) {
      params.from = new Date(detailCustomFrom.value).toISOString()
      params.to = new Date(detailCustomTo.value).toISOString()
    } else { params.range = detailRange.value }
    const { data } = await axios.get<{ correlations: CorrelationRow[] }>(
      `/api/connections/${props.activeConnId}/search/metrics-correlation`, { params }
    )
    correlationData.value = data.correlations ?? []
  } catch { toast.error('Failed to load correlations') } finally { correlationLoading.value = false }
}

watch(detailTab, tab => {
  if (tab === 'processes') loadProcessList()
  if (tab === 'correlation') loadCorrelation()
})
watch(processSort, () => loadProcessList())

// ── Export ────────────────────────────────────────────────────────
function exportCharts() {
  const svgEls = document.querySelectorAll<SVGSVGElement>('.hd-svg')
  if (!svgEls.length) { toast.error('No charts to export'); return }
  const serializer = new XMLSerializer()
  const parts: string[] = []
  svgEls.forEach((el, i) => {
    const title = el.closest('.hd-chart-card')?.querySelector('.hd-chart-title')?.textContent ?? `Chart ${i + 1}`
    parts.push(`<text style="font:12px sans-serif;fill:#aaa">${title}</text>`)
    parts.push(serializer.serializeToString(el))
  })
  const blob = new Blob([`<svg xmlns="http://www.w3.org/2000/svg">${parts.join('\n')}</svg>`], { type: 'image/svg+xml' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = `infra-${detailHost.value ?? 'export'}-${detailRange.value}.svg`
  a.click()
  URL.revokeObjectURL(a.href)
}

// ── Disk fill forecast ────────────────────────────────────────────
function diskForecast(buckets: BucketValue[], fieldKey: string): number | null {
  const pts = buckets
    .map(b => ({ x: b.key as number, y: b[fieldKey] as number }))
    .filter(p => p.y != null && isFinite(p.y))
  if (pts.length < 3) return null
  const n = pts.length
  const sx = pts.reduce((a, p) => a + p.x, 0)
  const sy = pts.reduce((a, p) => a + p.y, 0)
  const sxy = pts.reduce((a, p) => a + p.x * p.y, 0)
  const sx2 = pts.reduce((a, p) => a + p.x * p.x, 0)
  const slope = (n * sxy - sx * sy) / (n * sx2 - sx * sx)
  const intercept = (sy - slope * sx) / n
  if (slope <= 0) return null
  const timeToFull = (1.0 - intercept) / slope
  const msRemaining = timeToFull - pts[pts.length - 1].x
  return msRemaining > 0 ? msRemaining / (1000 * 60 * 60 * 24) : null
}

// ── Sparkline SVG helper ──────────────────────────────────────────
function sparklinePoints(vals: number[]): string {
  if (!vals.length) return ''
  const w = 60, h = 20
  const min = Math.min(...vals), max = Math.max(...vals)
  const range = max - min || 1
  return vals.map((v, i) => {
    const x = (i / Math.max(vals.length - 1, 1)) * w
    const y = h - ((v - min) / range) * h
    return `${x.toFixed(1)},${y.toFixed(1)}`
  }).join(' ')
}

// ── Heatmap color helper ──────────────────────────────────────────
function heatColor(v: number | null, fmt: string): string {
  if (v == null) return 'rgba(255,255,255,0.04)'
  let pct = fmt === 'pct' ? v * 100 : Math.min(100, v / 10)
  if (pct >= 90) return 'rgba(239,68,68,0.75)'
  if (pct >= 75) return 'rgba(245,158,11,0.65)'
  if (pct >= 50) return 'rgba(251,191,36,0.5)'
  return 'rgba(34,197,94,0.45)'
}

function fmtMem(bytes: number | null): string {
  if (bytes == null) return '—'
  if (bytes >= 1e9) return (bytes / 1e9).toFixed(1) + ' GB'
  if (bytes >= 1e6) return (bytes / 1e6).toFixed(1) + ' MB'
  return (bytes / 1e3).toFixed(0) + ' KB'
}

// ── SVG chart helpers ─────────────────────────────────────────────
const CHART_W = 400
const CHART_H = 80
const CHART_PAD_L = 36
const CHART_PAD_B = 18
const CHART_PAD_T = 8
const CHART_PAD_R = 8

const LINE_COLORS = ['#6366f1', '#22c55e', '#f59e0b', '#ef4444', '#06b6d4', '#a855f7']

function buildChartPaths(chart: ChartResult): { paths: string[]; yLabels: { y: number; label: string }[]; xLabels: { x: number; label: string }[] } {
  const buckets = chart.buckets ?? []
  if (!buckets.length || !chart.fields?.length) return { paths: [], yLabels: [], xLabels: [] }

  const W = CHART_W - CHART_PAD_L - CHART_PAD_R
  const H = CHART_H - CHART_PAD_T - CHART_PAD_B

  // Collect all numeric values across all fields to get global min/max
  let allVals: number[] = []
  for (const f of chart.fields) {
    for (const b of buckets) {
      const v = b[f.key]
      if (v != null && typeof v === 'number' && isFinite(v)) allVals.push(v)
    }
  }
  if (!allVals.length) return { paths: [], yLabels: [], xLabels: [] }

  let minV = Math.min(...allVals)
  let maxV = Math.max(...allVals)
  if (minV === maxV) { minV = 0; maxV = maxV || 1 }
  const range = maxV - minV || 1

  const n = buckets.length

  function xPos(i: number) { return CHART_PAD_L + (i / Math.max(n - 1, 1)) * W }
  function yPos(v: number) { return CHART_PAD_T + H - ((v - minV) / range) * H }

  const paths: string[] = []
  for (const f of chart.fields) {
    const pts = buckets
      .map((b, i) => ({ i, v: b[f.key] }))
      .filter(p => p.v != null && typeof p.v === 'number' && isFinite(p.v))
    if (!pts.length) continue

    // Area path (fill under)
    let areaD = `M ${xPos(pts[0].i)} ${yPos(pts[0].v)}`
    for (let k = 1; k < pts.length; k++) {
      areaD += ` L ${xPos(pts[k].i)} ${yPos(pts[k].v)}`
    }
    const lastX = xPos(pts[pts.length - 1].i)
    const baseY = CHART_PAD_T + H
    areaD += ` L ${lastX} ${baseY} L ${xPos(pts[0].i)} ${baseY} Z`
    paths.push(areaD)
  }

  // Y axis labels (3 ticks)
  const yLabels: { y: number; label: string }[] = []
  for (let t = 0; t <= 2; t++) {
    const v = minV + (range * t) / 2
    yLabels.push({ y: yPos(v), label: fmtAxisVal(v, chart.fields[0]?.unit ?? '') })
  }

  // X axis labels (first + last)
  const xLabels: { x: number; label: string }[] = []
  if (buckets.length > 0) {
    xLabels.push({ x: xPos(0), label: fmtTs(buckets[0].key_as_string) })
    if (buckets.length > 1) {
      xLabels.push({ x: xPos(buckets.length - 1), label: fmtTs(buckets[buckets.length - 1].key_as_string) })
    }
  }

  return { paths, yLabels, xLabels }
}

function fmtAxisVal(v: number, unit: string): string {
  if (unit === '%') return (v * 100).toFixed(0) + '%'
  if (unit === 'B' || unit === 'B/s') {
    if (v >= 1e9) return (v / 1e9).toFixed(1) + 'G'
    if (v >= 1e6) return (v / 1e6).toFixed(1) + 'M'
    if (v >= 1e3) return (v / 1e3).toFixed(1) + 'K'
    return v.toFixed(0)
  }
  if (v >= 1e6) return (v / 1e6).toFixed(1) + 'M'
  if (v >= 1e3) return (v / 1e3).toFixed(1) + 'K'
  return v % 1 === 0 ? v.toFixed(0) : v.toFixed(1)
}

function fmtTs(ts: string): string {
  if (!ts) return ''
  const d = new Date(ts)
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

function chartLatestVal(chart: ChartResult): string {
  if (!chart.buckets?.length || !chart.fields?.length) return '—'
  const last = [...chart.buckets].reverse().find(b => b[chart.fields[0].key] != null)
  if (!last) return '—'
  return fmtAxisVal(last[chart.fields[0].key], chart.fields[0].unit ?? '')
}

// ── Cluster Tab ───────────────────────────────────────────────────
const mainTab = ref<'inventory' | 'cluster'>('inventory')

interface ClusterHealth {
  status: string; cluster_name: string; number_of_nodes: number
  active_shards: number; unassigned_shards: number
}
interface NodeCatRow {
  name: string; cpu: string; 'ram.percent': string; 'heap.percent': string
  'disk.used_percent': string; load_1m: string; uptime: string
  'node.role': string; master: string
}
interface IndexCatRow {
  index: string; 'docs.count': string; 'store.size': string
  'indexing.index_total': string; 'search.query_total': string
  pri: string; rep: string; status: string
}
interface ClusterData {
  health: ClusterHealth | null
  stats: any
  nodes_cat: NodeCatRow[]
  indices_cat: IndexCatRow[]
}

const clusterData = ref<ClusterData | null>(null)
const clusterLoading = ref(false)

async function loadClusterData() {
  if (!props.activeConnId || !isSupported.value) return
  clusterLoading.value = true
  try {
    const { data } = await axios.get<ClusterData>(
      `/api/connections/${props.activeConnId}/search/metrics-cluster`
    )
    clusterData.value = data
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to load cluster data')
  } finally {
    clusterLoading.value = false
  }
}

watch(mainTab, tab => { if (tab === 'cluster') loadClusterData() })
</script>

<template>
  <div class="page-shell inv-root" @click="showMetricMenu = false; showGroupMenu = false; showIndexMenu = false; showCustomPicker = false">
    <div class="page-scroll">
      <div class="page-stack">

        <!-- Hero -->
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Observability · Infrastructure</div>
            <div class="page-title">Inventory</div>
            <div class="page-subtitle">Infrastructure metrics from Metricbeat or Elastic Agent — hosts, pods, containers.</div>
          </div>
        </section>

        <!-- No connection / unsupported -->
        <div v-if="!activeConnId" class="inv-empty">
          <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4.03 3-9 3S3 13.66 3 12"/><path d="M3 5v14c0 1.66 4.03 3 9 3s9-1.34 9-3V5"/></svg>
          <div class="inv-empty-title">No connection selected</div>
          <div class="inv-empty-sub">Select an Elasticsearch or OpenSearch connection from the top navigation bar.</div>
        </div>
        <div v-else-if="!isSupported" class="inv-empty">
          <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
          <div class="inv-empty-title">Not supported for {{ activeConn?.driver }}</div>
          <div class="inv-empty-sub">Infra Metrics requires an Elasticsearch or OpenSearch connection with Metricbeat or Elastic Agent data.</div>
        </div>

        <template v-else>
          <!-- Main tab switcher -->
          <div class="inv-main-tabs">
            <button class="inv-main-tab" :class="{'inv-main-tab--active': mainTab==='inventory'}" @click="mainTab='inventory'">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><line x1="3" y1="9" x2="21" y2="9"/><line x1="3" y1="15" x2="21" y2="15"/><line x1="9" y1="3" x2="9" y2="21"/></svg>
              Inventory
            </button>
            <button class="inv-main-tab" :class="{'inv-main-tab--active': mainTab==='cluster'}" @click="mainTab='cluster'">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4.03 3-9 3S3 13.66 3 12"/><path d="M3 5v14c0 1.66 4.03 3 9 3s9-1.34 9-3V5"/></svg>
              Cluster
            </button>
          </div>

          <!-- ── Inventory tab ──────────────────────────────────── -->
          <div v-if="mainTab==='inventory'" class="inv-tab-content">
          <!-- Toolbar -->
          <div class="inv-toolbar page-panel">
            <!-- Search -->
            <div class="inv-search">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
              <input
                class="inv-search-input"
                v-model="search"
                :placeholder="`Search for infrastructure data… (e.g. ${activeGroup}:host-1)`"
              />
            </div>

            <div class="inv-toolbar-right">
              <!-- Time range -->
              <div class="inv-time-pills">
                <button
                  v-for="tr in timeRanges" :key="tr.value"
                  class="inv-time-pill" :class="{ 'inv-time-pill--active': !isCustomRange && timeRange === tr.value }"
                  @click="isCustomRange = false; timeRange = tr.value"
                >{{ tr.label }}</button>
                <!-- Custom range -->
                <div class="inv-custom-range-wrap" @click.stop>
                  <button
                    class="inv-time-pill inv-time-pill--custom"
                    :class="{ 'inv-time-pill--active': isCustomRange }"
                    @click="isCustomRange ? clearCustomRange() : openCustomPicker()"
                  >
                    <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="18" rx="2"/><line x1="16" y1="2" x2="16" y2="6"/><line x1="8" y1="2" x2="8" y2="6"/><line x1="3" y1="10" x2="21" y2="10"/></svg>
                    {{ isCustomRange ? `${customFrom.slice(0,16).replace('T',' ')} → ${customTo.slice(0,16).replace('T',' ')}` : 'Custom' }}
                  </button>
                  <div v-if="showCustomPicker" class="inv-date-picker">
                    <div class="inv-date-picker-title">Custom time range</div>
                    <label class="inv-date-label">From</label>
                    <input type="datetime-local" class="inv-date-input" v-model="customFrom" />
                    <label class="inv-date-label">To</label>
                    <input type="datetime-local" class="inv-date-input" v-model="customTo" />
                    <div class="inv-date-actions">
                      <button class="base-btn base-btn--ghost base-btn--sm" @click="showCustomPicker = false">Cancel</button>
                      <button class="base-btn base-btn--primary base-btn--sm" @click="applyCustomRange" :disabled="!customFrom || !customTo">Apply</button>
                    </div>
                  </div>
                </div>
              </div>

              <!-- Auto refresh toggle (disabled for custom range) -->
              <button
                class="inv-auto-refresh-btn"
                :class="{ 'inv-auto-refresh-btn--on': autoRefresh && !isCustomRange }"
                :disabled="isCustomRange"
                :title="isCustomRange ? 'Auto-refresh unavailable for custom range' : ''"
                @click="autoRefresh = !autoRefresh"
              >
                <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polygon points="5 3 19 12 5 21 5 3"/></svg>
                Auto-refresh
              </button>

              <!-- View toggle -->
              <div class="inv-view-toggle">
                <button class="inv-view-btn" :class="{ 'inv-view-btn--active': viewMode === 'table' }" @click="viewMode = 'table'" title="Table view">
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><line x1="3" y1="9" x2="21" y2="9"/><line x1="3" y1="15" x2="21" y2="15"/><line x1="9" y1="3" x2="9" y2="21"/></svg>
                </button>
                <button class="inv-view-btn" :class="{ 'inv-view-btn--active': viewMode === 'heatmap' }" @click="viewMode = 'heatmap'" title="Heatmap view">
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="5" height="5"/><rect x="10" y="3" width="5" height="5"/><rect x="17" y="3" width="5" height="5"/><rect x="3" y="10" width="5" height="5"/><rect x="10" y="10" width="5" height="5"/><rect x="17" y="10" width="5" height="5"/><rect x="3" y="17" width="5" height="5"/><rect x="10" y="17" width="5" height="5"/><rect x="17" y="17" width="5" height="5"/></svg>
                </button>
              </div>

              <!-- Anomaly filter -->
              <button class="inv-anomaly-btn" :class="{ 'inv-anomaly-btn--active': anomalyOnly }" @click="anomalyOnly = !anomalyOnly; currentPage = 1" :title="`${anomalyHosts.size} anomaly hosts detected`">
                <span class="inv-anomaly-dot"></span>
                Anomalies {{ anomalyHosts.size > 0 ? `(${anomalyHosts.size})` : '' }}
              </button>

              <!-- Compare mode -->
              <button class="inv-compare-btn" :class="{ 'inv-compare-btn--active': compareMode }" @click="compareMode = !compareMode; selectedForCompare = []">
                <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="20" x2="18" y2="10"/><line x1="12" y1="20" x2="12" y2="4"/><line x1="6" y1="20" x2="6" y2="14"/></svg>
                Compare
              </button>

              <!-- Alert rules bell -->
              <button class="inv-icon-btn" :class="{ 'inv-icon-btn--active': showAlertsPanel }" @click="showAlertsPanel = !showAlertsPanel" title="Alert rules">
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.73 21a2 2 0 0 1-3.46 0"/></svg>
                <span v-if="alertRules.filter(r=>r.enabled).length" class="inv-badge">{{ alertRules.filter(r=>r.enabled).length }}</span>
              </button>

              <!-- Copy link -->
              <button class="inv-icon-btn" @click="copyShareLink" title="Copy shareable link">
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg>
              </button>

              <!-- Manual refresh -->
              <button class="base-btn base-btn--ghost base-btn--sm" @click="loadInventory" :disabled="loading">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.08-4.43"/></svg>
                Refresh
              </button>

              <span v-if="lastRefreshed" class="inv-ts">{{ lastRefreshed.toLocaleTimeString() }}</span>
            </div>
          </div>

          <!-- Filter bar (Show / Metric / Group by) -->
          <div class="inv-filter-bar page-panel">
            <!-- Index (Show) -->
            <span class="inv-filter-label">Show</span>
            <div class="inv-dropdown" @click.stop>
              <button class="inv-dropdown-btn" @click="showIndexMenu = !showIndexMenu; showMetricMenu = false; showGroupMenu = false">
                {{ selectedIndex.label }} <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><polyline points="6 9 12 15 18 9"/></svg>
              </button>
              <div v-if="showIndexMenu" class="inv-menu">
                <button v-for="opt in indexOptions" :key="opt.value" class="inv-menu-item" :class="{ 'inv-menu-item--active': selectedIndex.value === opt.value }" @click="selectedIndex = opt; showIndexMenu = false">{{ opt.label }}</button>
              </div>
            </div>

            <div class="inv-divider"></div>

            <!-- Metric -->
            <span class="inv-filter-label">Metric</span>
            <div class="inv-dropdown" @click.stop>
              <button class="inv-dropdown-btn" @click="showMetricMenu = !showMetricMenu; showIndexMenu = false; showGroupMenu = false">
                {{ selectedMetric.label }} <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><polyline points="6 9 12 15 18 9"/></svg>
              </button>
              <div v-if="showMetricMenu" class="inv-menu">
                <button v-for="opt in metricOptions" :key="opt.field" class="inv-menu-item" :class="{ 'inv-menu-item--active': selectedMetric.field === opt.field }" @click="selectedMetric = opt; showMetricMenu = false">{{ opt.label }}</button>
              </div>
            </div>

            <!-- Custom field input when "Custom…" selected -->
            <template v-if="selectedMetric.field === '__custom__'">
              <input class="inv-custom-input" v-model="customField" placeholder="e.g. system.cpu.user.pct" @change="loadInventory" />
            </template>

            <div class="inv-divider"></div>

            <!-- Group by -->
            <span class="inv-filter-label">Group by</span>
            <div class="inv-dropdown" @click.stop>
              <button class="inv-dropdown-btn" @click="showGroupMenu = !showGroupMenu; showMetricMenu = false; showIndexMenu = false">
                {{ selectedGroup.label }} <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><polyline points="6 9 12 15 18 9"/></svg>
              </button>
              <div v-if="showGroupMenu" class="inv-menu">
                <button v-for="opt in groupOptions" :key="opt.field" class="inv-menu-item" :class="{ 'inv-menu-item--active': selectedGroup.field === opt.field }" @click="selectedGroup = opt; showGroupMenu = false">{{ opt.label }}</button>
              </div>
            </div>

            <!-- Custom group input -->
            <template v-if="selectedGroup.field === '__custom__'">
              <input class="inv-custom-input" v-model="customGroup" placeholder="e.g. kubernetes.node.name" @change="loadInventory" />
            </template>
          </div>

          <!-- Alert rules panel -->
          <div v-if="showAlertsPanel" class="inv-side-panel page-panel">
            <div class="inv-side-panel-header">
              <span class="inv-side-panel-title">Alert Rules</span>
              <button class="hd-close-btn" @click="showAlertsPanel = false"><svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
            </div>
            <!-- Existing rules -->
            <div class="inv-alert-list">
              <div v-if="!alertRules.length" class="inv-side-empty">No alert rules yet.</div>
              <div v-for="rule in alertRules" :key="rule.id" class="inv-alert-row">
                <div class="inv-alert-info">
                  <span class="inv-alert-name">{{ rule.name }}</span>
                  <span class="inv-alert-meta">{{ rule.metric_field }} {{ rule.comparison }} {{ rule.threshold }} for {{ rule.duration_min }}m</span>
                </div>
                <div class="inv-alert-actions">
                  <button class="inv-toggle-btn" :class="{ 'inv-toggle-btn--on': rule.enabled }" @click="toggleAlertRule(rule.id)">{{ rule.enabled ? 'On' : 'Off' }}</button>
                  <button class="inv-icon-btn inv-icon-btn--danger" @click="deleteAlertRule(rule.id)"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14H6L5 6"/><path d="M10 11v6"/><path d="M14 11v6"/><path d="M9 6V4h6v2"/></svg></button>
                </div>
              </div>
            </div>
            <!-- New rule form -->
            <div class="inv-alert-form">
              <div class="inv-form-title">New Rule</div>
              <input class="inv-date-input" v-model="alertForm.name" placeholder="Rule name" />
              <select class="inv-date-input" v-model="alertForm.metric_field">
                <option v-for="m in metricOptions.filter(m=>m.field!=='__custom__')" :key="m.field" :value="m.field">{{ m.label }}</option>
              </select>
              <div class="inv-form-row">
                <select class="inv-date-input inv-date-input--short" v-model="alertForm.comparison">
                  <option value="gt">&gt;</option><option value="lt">&lt;</option>
                  <option value="gte">≥</option><option value="lte">≤</option>
                </select>
                <input class="inv-date-input" type="number" v-model.number="alertForm.threshold" placeholder="Threshold" />
              </div>
              <div class="inv-form-row inv-form-row--inline">
                <input class="inv-date-input inv-date-input--short" type="number" v-model.number="alertForm.duration_min" />
                <span class="inv-form-hint">minutes</span>
              </div>
              <button class="base-btn base-btn--primary base-btn--sm inv-form-submit" @click="createAlertRule" :disabled="alertFormLoading || !alertForm.name">Create Rule</button>
            </div>
          </div>

          <!-- Compare bar when hosts selected -->
          <div v-if="compareMode && selectedForCompare.length > 0" class="inv-compare-bar page-panel">
            <span class="inv-compare-count">{{ selectedForCompare.length }} host{{ selectedForCompare.length > 1 ? 's' : '' }} selected</span>
            <div class="inv-compare-chips">
              <span v-for="h in selectedForCompare" :key="h" class="inv-compare-chip">
                {{ h }}
                <button class="inv-compare-chip-remove" type="button" @click="toggleCompare(h)" title="Remove from comparison">×</button>
              </span>
            </div>
            <button class="base-btn base-btn--primary base-btn--sm" @click="runComparison" :disabled="selectedForCompare.length < 2">Compare</button>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="selectedForCompare = []">Clear</button>
          </div>

          <!-- Heatmap view -->
          <section v-if="viewMode === 'heatmap'" class="page-panel inv-heatmap-panel">
            <div v-if="loading" class="inv-loading-bar"></div>
            <div class="inv-heatmap-grid">
              <div
                v-for="row in sortedRows" :key="row.name"
                class="inv-heatmap-cell"
                :style="{ background: heatColor(row.last_1m, activeFmt) }"
                @click="openHostDetail(row.name)"
                :title="`${row.name}: ${fmtValue(row.last_1m, activeFmt, activeUnit)}`"
              >
                <div class="inv-heatmap-name">{{ row.name }}</div>
                <div class="inv-heatmap-val">{{ fmtValue(row.last_1m, activeFmt, activeUnit) }}</div>
                <span v-if="anomalyHosts.has(row.name)" class="inv-anomaly-badge">!</span>
              </div>
            </div>
          </section>

          <!-- Table view -->
          <section v-else class="page-panel inv-table-panel">
            <!-- Loading bar -->
            <div v-if="loading" class="inv-loading-bar"></div>

            <table class="inv-table">
              <thead>
                <tr>
                  <th v-if="compareMode" class="inv-th inv-th--check"></th>
                  <th class="inv-th inv-th--pin"></th>
                  <th class="inv-th inv-th--name" @click="setSort('name')">
                    Name <span class="inv-sort-icon" :class="{ active: sortCol === 'name' }">{{ sortIcon('name') }}</span>
                  </th>
                  <th class="inv-th inv-th--spark">Trend</th>
                  <th class="inv-th inv-th--metric" @click="setSort('last_1m')">
                    Last 1m <span class="inv-sort-icon" :class="{ active: sortCol === 'last_1m' }">{{ sortIcon('last_1m') }}</span>
                  </th>
                  <th class="inv-th inv-th--metric" @click="setSort('avg')">
                    Avg <span class="inv-sort-icon" :class="{ active: sortCol === 'avg' }">{{ sortIcon('avg') }}</span>
                  </th>
                  <th class="inv-th inv-th--metric" @click="setSort('max')">
                    Max <span class="inv-sort-icon" :class="{ active: sortCol === 'max' }">{{ sortIcon('max') }}</span>
                  </th>
                  <th class="inv-th inv-th--ts">Last seen</th>
                </tr>
              </thead>
              <tbody>
                <!-- Pinned section -->
                <template v-if="pinnedRows.length">
                  <tr class="inv-section-header">
                    <td :colspan="compareMode ? 8 : 7" class="inv-section-label">
                      <svg width="10" height="10" viewBox="0 0 24 24" fill="currentColor"><path d="M17 2H7a2 2 0 0 0-2 2v16l7-3 7 3V4a2 2 0 0 0-2-2z"/></svg>
                      Pinned
                    </td>
                  </tr>
                  <tr v-for="row in pinnedRows" :key="`pin-${row.name}`" class="inv-row inv-row--pinned inv-row--clickable" @click="openHostDetail(row.name)">
                    <td v-if="compareMode" class="inv-td" @click.stop><input type="checkbox" :checked="selectedForCompare.includes(row.name)" @change="toggleCompare(row.name)" /></td>
                    <td class="inv-td inv-td--pin" @click.stop>
                      <button class="inv-pin-btn inv-pin-btn--active" @click="togglePin(row.name)" title="Unpin">
                        <svg width="10" height="10" viewBox="0 0 24 24" fill="currentColor"><path d="M17 2H7a2 2 0 0 0-2 2v16l7-3 7 3V4a2 2 0 0 0-2-2z"/></svg>
                      </button>
                    </td>
                    <td class="inv-td inv-td--name">
                      <span v-if="anomalyHosts.has(row.name)" class="inv-anomaly-dot-inline"></span>
                      <span class="inv-host-link">{{ row.name }}</span>
                    </td>
                    <td class="inv-td inv-td--spark">
                      <svg width="60" height="20" class="inv-sparkline">
                        <polyline v-if="sparklines[row.name]?.length" :points="sparklinePoints(sparklines[row.name])" fill="none" stroke="var(--brand)" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
                        <line v-else x1="0" y1="10" x2="60" y2="10" stroke="rgba(255,255,255,0.1)" stroke-width="1"/>
                      </svg>
                    </td>
                    <td class="inv-td inv-td--metric">
                      <div class="inv-metric-cell">
                        <span class="inv-metric-val">{{ fmtValue(row.last_1m, activeFmt, activeUnit) }}</span>
                        <div v-if="activeFmt === 'pct' && row.last_1m != null" class="inv-mini-bar-wrap">
                          <div class="inv-mini-bar" :style="{ width: pctWidth(row.last_1m, activeFmt) + '%', background: pctColor(pctWidth(row.last_1m, activeFmt)) }"></div>
                        </div>
                      </div>
                    </td>
                    <td class="inv-td inv-td--metric">{{ fmtValue(row.avg, activeFmt, activeUnit) }}</td>
                    <td class="inv-td inv-td--metric">{{ fmtValue(row.max, activeFmt, activeUnit) }}</td>
                    <td class="inv-td inv-td--ts">{{ timeSince(row.latest_ts) }}</td>
                  </tr>
                  <tr class="inv-section-header">
                    <td :colspan="compareMode ? 8 : 7" class="inv-section-label">All hosts</td>
                  </tr>
                </template>

                <tr v-if="loading && pagedRows.length === 0">
                  <td :colspan="compareMode ? 8 : 7" class="inv-empty-cell">
                    <svg class="spin" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
                    Loading…
                  </td>
                </tr>
                <tr v-else-if="pagedRows.length === 0 && !loading">
                  <td :colspan="compareMode ? 8 : 7" class="inv-empty-cell">
                    <div class="inv-no-data">
                      <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
                      No data found for <strong>{{ selectedMetric.label }}</strong> in <code>{{ selectedIndex.value }}</code>.
                    </div>
                  </td>
                </tr>
                <tr v-for="row in pagedRows" :key="row.name" class="inv-row inv-row--clickable" @click="openHostDetail(row.name)">
                  <td v-if="compareMode" class="inv-td" @click.stop><input type="checkbox" :checked="selectedForCompare.includes(row.name)" @change="toggleCompare(row.name)" /></td>
                  <td class="inv-td inv-td--pin" @click.stop>
                    <button class="inv-pin-btn" :class="{ 'inv-pin-btn--active': pinnedHosts.has(row.name) }" @click="togglePin(row.name)" title="Pin host">
                      <svg width="10" height="10" viewBox="0 0 24 24" :fill="pinnedHosts.has(row.name) ? 'currentColor' : 'none'" stroke="currentColor" stroke-width="2"><path d="M17 2H7a2 2 0 0 0-2 2v16l7-3 7 3V4a2 2 0 0 0-2-2z"/></svg>
                    </button>
                  </td>
                  <td class="inv-td inv-td--name">
                    <span v-if="anomalyHosts.has(row.name)" class="inv-anomaly-dot-inline" title="Anomaly detected"></span>
                    <span class="inv-host-link">{{ row.name }}</span>
                  </td>
                  <td class="inv-td inv-td--spark">
                    <svg width="60" height="20" class="inv-sparkline">
                      <polyline v-if="sparklines[row.name]?.length" :points="sparklinePoints(sparklines[row.name])" fill="none" stroke="var(--brand)" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
                      <line v-else x1="0" y1="10" x2="60" y2="10" stroke="rgba(255,255,255,0.1)" stroke-width="1"/>
                    </svg>
                  </td>
                  <td class="inv-td inv-td--metric">
                    <div class="inv-metric-cell">
                      <span class="inv-metric-val">{{ fmtValue(row.last_1m, activeFmt, activeUnit) }}</span>
                      <div v-if="activeFmt === 'pct' && row.last_1m != null" class="inv-mini-bar-wrap">
                        <div class="inv-mini-bar" :style="{ width: pctWidth(row.last_1m, activeFmt) + '%', background: pctColor(pctWidth(row.last_1m, activeFmt)) }"></div>
                      </div>
                    </div>
                  </td>
                  <td class="inv-td inv-td--metric">{{ fmtValue(row.avg, activeFmt, activeUnit) }}</td>
                  <td class="inv-td inv-td--metric">{{ fmtValue(row.max, activeFmt, activeUnit) }}</td>
                  <td class="inv-td inv-td--ts">{{ timeSince(row.latest_ts) }}</td>
                </tr>
              </tbody>
            </table>

            <!-- Pagination footer -->
            <div class="inv-footer">
              <div class="inv-footer-left">
                <span class="inv-count">{{ rows.length }} result{{ rows.length !== 1 ? 's' : '' }}</span>
                <span class="inv-sep">·</span>
                <label class="inv-per-page">
                  Rows per page:
                  <select class="inv-per-page-sel" v-model="pageSize" @change="currentPage = 1">
                    <option :value="10">10</option><option :value="25">25</option>
                    <option :value="50">50</option><option :value="100">100</option>
                  </select>
                </label>
              </div>
              <div class="inv-pagination" v-if="totalPages > 1">
                <button class="inv-page-btn" :disabled="currentPage === 1" @click="currentPage--">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="15 18 9 12 15 6"/></svg>
                </button>
                <span class="inv-page-info">{{ currentPage }} / {{ totalPages }}</span>
                <button class="inv-page-btn" :disabled="currentPage === totalPages" @click="currentPage++">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 18 15 12 9 6"/></svg>
                </button>
              </div>
            </div>
          </section>
          </div><!-- end inventory tab -->

          <!-- ── Cluster tab ───────────────────────────────────── -->
          <div v-else-if="mainTab==='cluster'" class="inv-tab-content">
            <div class="page-panel cl-header-bar">
              <button class="base-btn base-btn--ghost base-btn--sm" @click="loadClusterData" :disabled="clusterLoading">
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.08-4.43"/></svg>
                Refresh
              </button>
            </div>

            <div v-if="clusterLoading && !clusterData" class="inv-loading-bar cl-loading-bar"></div>

            <template v-if="clusterData">
              <!-- Health banner -->
              <section class="page-panel cl-health-banner" :class="`cl-health--${clusterData.health?.status ?? 'unknown'}`">
                <div class="cl-health-status">
                  <span class="cl-health-dot"></span>
                  <span class="cl-health-name">{{ clusterData.health?.cluster_name ?? '—' }}</span>
                  <span class="cl-health-label">{{ (clusterData.health?.status ?? '—').toUpperCase() }}</span>
                </div>
                <div class="cl-health-stats">
                  <div class="cl-stat">
                    <div class="cl-stat-val">{{ clusterData.health?.number_of_nodes ?? '—' }}</div>
                    <div class="cl-stat-label">Nodes</div>
                  </div>
                  <div class="cl-stat">
                    <div class="cl-stat-val">{{ clusterData.health?.active_shards ?? '—' }}</div>
                    <div class="cl-stat-label">Active shards</div>
                  </div>
                  <div class="cl-stat" :class="{ 'cl-stat--warn': (clusterData.health?.unassigned_shards ?? 0) > 0 }">
                    <div class="cl-stat-val">{{ clusterData.health?.unassigned_shards ?? '—' }}</div>
                    <div class="cl-stat-label">Unassigned</div>
                  </div>
                </div>
              </section>

              <!-- Node table -->
              <section class="page-panel">
                <div class="cl-section-title">Nodes</div>
                <div v-if="!clusterData.nodes_cat?.length" class="inv-no-data inv-no-data--panel">No node data.</div>
                <table v-else class="inv-table">
                  <thead>
                    <tr>
                      <th class="inv-th">Name</th>
                      <th class="inv-th inv-th--metric">Role</th>
                      <th class="inv-th inv-th--metric">CPU %</th>
                      <th class="inv-th inv-th--metric">RAM %</th>
                      <th class="inv-th inv-th--metric">Heap %</th>
                      <th class="inv-th inv-th--metric">Disk %</th>
                      <th class="inv-th inv-th--metric">Load 1m</th>
                      <th class="inv-th inv-th--metric">Uptime</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="n in clusterData.nodes_cat" :key="n.name" class="inv-row">
                      <td class="inv-td inv-td--name">
                        <span v-if="n.master === '*'" class="cl-master-star" title="Elected master">★</span>
                        {{ n.name }}
                      </td>
                      <td class="inv-td"><span class="cl-role-badge">{{ n['node.role'] }}</span></td>
                      <td class="inv-td inv-td--metric">
                        <div class="inv-metric-cell">
                          <span class="inv-metric-val">{{ n.cpu ?? '—' }}%</span>
                          <div class="inv-mini-bar-wrap">
                            <div class="inv-mini-bar" :style="{ width: Math.min(parseFloat(n.cpu)||0, 100)+'%', background: pctColor(parseFloat(n.cpu)||0) }"></div>
                          </div>
                        </div>
                      </td>
                      <td class="inv-td inv-td--metric">
                        <div class="inv-metric-cell">
                          <span class="inv-metric-val">{{ n['ram.percent'] ?? '—' }}%</span>
                          <div class="inv-mini-bar-wrap">
                            <div class="inv-mini-bar" :style="{ width: Math.min(parseFloat(n['ram.percent'])||0, 100)+'%', background: pctColor(parseFloat(n['ram.percent'])||0) }"></div>
                          </div>
                        </div>
                      </td>
                      <td class="inv-td inv-td--metric">
                        <div class="inv-metric-cell">
                          <span class="inv-metric-val">{{ n['heap.percent'] ?? '—' }}%</span>
                          <div class="inv-mini-bar-wrap">
                            <div class="inv-mini-bar" :style="{ width: Math.min(parseFloat(n['heap.percent'])||0, 100)+'%', background: pctColor(parseFloat(n['heap.percent'])||0) }"></div>
                          </div>
                        </div>
                      </td>
                      <td class="inv-td inv-td--metric">
                        <div class="inv-metric-cell">
                          <span class="inv-metric-val">{{ n['disk.used_percent'] ?? '—' }}%</span>
                          <div class="inv-mini-bar-wrap">
                            <div class="inv-mini-bar" :style="{ width: Math.min(parseFloat(n['disk.used_percent'])||0, 100)+'%', background: pctColor(parseFloat(n['disk.used_percent'])||0) }"></div>
                          </div>
                        </div>
                      </td>
                      <td class="inv-td inv-td--metric">{{ n.load_1m ?? '—' }}</td>
                      <td class="inv-td inv-td--ts">{{ n.uptime ?? '—' }}</td>
                    </tr>
                  </tbody>
                </table>
              </section>

              <!-- Indices table -->
              <section class="page-panel">
                <div class="cl-section-title">Top Indices by Size</div>
                <div v-if="!clusterData.indices_cat?.length" class="inv-no-data inv-no-data--panel">No index data.</div>
                <table v-else class="inv-table">
                  <thead>
                    <tr>
                      <th class="inv-th">Index</th>
                      <th class="inv-th inv-th--metric">Status</th>
                      <th class="inv-th inv-th--metric">Docs</th>
                      <th class="inv-th inv-th--metric">Size</th>
                      <th class="inv-th inv-th--metric">Pri</th>
                      <th class="inv-th inv-th--metric">Rep</th>
                      <th class="inv-th inv-th--metric">Index ops</th>
                      <th class="inv-th inv-th--metric">Search ops</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="idx in clusterData.indices_cat" :key="idx.index" class="inv-row">
                      <td class="inv-td inv-td--name">{{ idx.index }}</td>
                      <td class="inv-td"><span class="cl-idx-status" :class="`cl-idx-status--${idx.status}`">{{ idx.status }}</span></td>
                      <td class="inv-td inv-td--metric">{{ Number(idx['docs.count']).toLocaleString() }}</td>
                      <td class="inv-td inv-td--metric">{{ idx['store.size'] ?? '—' }}</td>
                      <td class="inv-td inv-td--metric">{{ idx.pri }}</td>
                      <td class="inv-td inv-td--metric">{{ idx.rep }}</td>
                      <td class="inv-td inv-td--metric">{{ Number(idx['indexing.index_total']).toLocaleString() }}</td>
                      <td class="inv-td inv-td--metric">{{ Number(idx['search.query_total']).toLocaleString() }}</td>
                    </tr>
                  </tbody>
                </table>
              </section>
            </template>

            <div v-if="!clusterData && !clusterLoading" class="inv-empty inv-empty--compact">
              <div class="inv-empty-title">No cluster data</div>
              <div class="inv-empty-sub">Click Refresh to load cluster stats.</div>
            </div>
          </div><!-- end cluster tab -->

        </template>

      </div>
    </div>
  </div>

  <!-- Host Detail Overlay -->
  <Teleport to="body">
    <div v-if="detailHost" class="hd-overlay" @click.self="closeHostDetail">
      <div class="hd-panel">

        <!-- Header -->
        <div class="hd-header">
          <button class="hd-back-btn" @click="closeHostDetail">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="15 18 9 12 15 6"/></svg>
            Back
          </button>
          <div class="hd-host-name">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="3" width="20" height="14" rx="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/></svg>
            {{ detailHost }}
          </div>
          <div class="hd-time-pills">
            <button
              v-for="tr in detailTimeRanges" :key="tr.value"
              class="inv-time-pill" :class="{ 'inv-time-pill--active': !detailIsCustomRange && detailRange === tr.value }"
              @click="detailIsCustomRange = false; detailRange = tr.value"
            >{{ tr.label }}</button>
            <div class="inv-custom-range-wrap" @click.stop>
              <button
                class="inv-time-pill inv-time-pill--custom"
                :class="{ 'inv-time-pill--active': detailIsCustomRange }"
                @click="detailIsCustomRange ? clearDetailCustomRange() : openDetailCustomPicker()"
              >
                <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="18" rx="2"/><line x1="16" y1="2" x2="16" y2="6"/><line x1="8" y1="2" x2="8" y2="6"/><line x1="3" y1="10" x2="21" y2="10"/></svg>
                {{ detailIsCustomRange ? `${detailCustomFrom.slice(0,16).replace('T',' ')} → ${detailCustomTo.slice(0,16).replace('T',' ')}` : 'Custom' }}
              </button>
              <div v-if="showDetailCustomPicker" class="inv-date-picker inv-date-picker--left">
                <div class="inv-date-picker-title">Custom time range</div>
                <label class="inv-date-label">From</label>
                <input type="datetime-local" class="inv-date-input" v-model="detailCustomFrom" />
                <label class="inv-date-label">To</label>
                <input type="datetime-local" class="inv-date-input" v-model="detailCustomTo" />
                <div class="inv-date-actions">
                  <button class="base-btn base-btn--ghost base-btn--sm" @click="showDetailCustomPicker = false">Cancel</button>
                  <button class="base-btn base-btn--primary base-btn--sm" @click="applyDetailCustomRange" :disabled="!detailCustomFrom || !detailCustomTo">Apply</button>
                </div>
              </div>
            </div>
          </div>
          <!-- Tabs -->
          <div class="hd-tabs">
            <button class="hd-tab" :class="{'hd-tab--active': detailTab==='metrics'}" @click="detailTab='metrics'">Metrics</button>
            <button class="hd-tab" :class="{'hd-tab--active': detailTab==='processes'}" @click="detailTab='processes'">Processes</button>
            <button class="hd-tab" :class="{'hd-tab--active': detailTab==='correlation'}" @click="detailTab='correlation'">Correlation</button>
          </div>

          <button class="base-btn base-btn--ghost base-btn--sm hd-refresh-btn" @click="loadHostDetail" :disabled="detailLoading">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.08-4.43"/></svg>
            Refresh
          </button>
          <!-- Annotation button -->
          <button class="base-btn base-btn--ghost base-btn--sm" @click="showAnnotationForm = !showAnnotationForm" title="Add annotation">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
            Note
          </button>
          <!-- Export button -->
          <button class="base-btn base-btn--ghost base-btn--sm" @click="exportCharts" title="Export charts">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
            Export
          </button>
          <button class="hd-close-btn" @click="closeHostDetail">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
          </button>
        </div>

        <!-- Annotation form -->
        <div v-if="showAnnotationForm" class="hd-annotation-form">
          <div class="inv-date-picker-title">Add Annotation</div>
          <input class="inv-date-input" v-model="annotationForm.title" placeholder="Title" />
          <input class="inv-date-input" v-model="annotationForm.description" placeholder="Description (optional)" />
          <div class="hd-annotation-row">
            <label class="hd-annotation-label">Color</label>
            <input class="hd-color-input" type="color" v-model="annotationForm.color" />
            <input type="datetime-local" class="inv-date-input hd-annotation-time" v-model="annotationForm.event_time" />
          </div>
          <div class="hd-annotation-actions">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="showAnnotationForm=false">Cancel</button>
            <button class="base-btn base-btn--primary base-btn--sm" @click="createAnnotation" :disabled="annotationFormLoading||!annotationForm.title">Save</button>
          </div>
        </div>

        <div class="hd-body">
          <!-- Sidebar (only for metrics tab) -->
          <div class="hd-sidebar" v-if="detailTab==='metrics'">
            <button
              v-for="cat in detailCategories" :key="cat"
              class="hd-cat-btn" :class="{ 'hd-cat-btn--active': detailCategory === cat }"
              @click="detailCategory = cat"
            >
              <svg v-if="cat === 'CPU'" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="4" width="16" height="16" rx="2"/><rect x="9" y="9" width="6" height="6"/><line x1="9" y1="1" x2="9" y2="4"/><line x1="15" y1="1" x2="15" y2="4"/><line x1="9" y1="20" x2="9" y2="23"/><line x1="15" y1="20" x2="15" y2="23"/><line x1="20" y1="9" x2="23" y2="9"/><line x1="20" y1="14" x2="23" y2="14"/><line x1="1" y1="9" x2="4" y2="9"/><line x1="1" y1="14" x2="4" y2="14"/></svg>
              <svg v-else-if="cat === 'Memory'" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 2L3 6v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V6l-3-4z"/><line x1="3" y1="6" x2="21" y2="6"/><path d="M16 10a4 4 0 0 1-8 0"/></svg>
              <svg v-else-if="cat === 'Network'" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="16" y="16" width="6" height="6" rx="1"/><rect x="2" y="16" width="6" height="6" rx="1"/><rect x="9" y="2" width="6" height="6" rx="1"/><path d="M5 16v-3a1 1 0 0 1 1-1h12a1 1 0 0 1 1 1v3"/><line x1="12" y1="12" x2="12" y2="8"/></svg>
              <svg v-else-if="cat === 'Disk'" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4.03 3-9 3S3 13.66 3 12"/><path d="M3 5v14c0 1.66 4.03 3 9 3s9-1.34 9-3V5"/></svg>
              <svg v-else width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>
              {{ cat }}
            </button>
          </div>

          <!-- Charts grid -->
          <div class="hd-charts">
            <!-- Loading state -->
            <div v-if="detailLoading && !detailData" class="hd-charts-loading">
              <svg class="spin" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
              Loading metrics…
            </div>
            <div v-else-if="!activeCharts.length" class="hd-charts-loading">
              No data for {{ detailCategory }} in selected range.
            </div>
            <div v-else class="hd-charts-grid">
              <div v-for="chart in activeCharts" :key="chart.key" class="hd-chart-card">
                <div class="hd-chart-header">
                  <span class="hd-chart-title">{{ chart.label }}</span>
                  <span class="hd-chart-latest">{{ chartLatestVal(chart) }}</span>
                </div>
                <div class="hd-chart-wrap">
                  <svg
                    :viewBox="`0 0 ${CHART_W} ${CHART_H + CHART_PAD_T + CHART_PAD_B}`"
                    class="hd-svg"
                    preserveAspectRatio="none"
                  >
                    <defs>
                      <linearGradient
                        v-for="(_, fi) in chart.fields"
                        :key="fi"
                        :id="`hd-grad-${chart.key}-${fi}`"
                        x1="0" y1="0" x2="0" y2="1"
                      >
                        <stop offset="0%" :stop-color="LINE_COLORS[fi % LINE_COLORS.length]" stop-opacity="0.3"/>
                        <stop offset="100%" :stop-color="LINE_COLORS[fi % LINE_COLORS.length]" stop-opacity="0.02"/>
                      </linearGradient>
                    </defs>

                    <!-- Grid lines -->
                    <template v-for="tick in buildChartPaths(chart).yLabels" :key="tick.y">
                      <line
                        :x1="CHART_PAD_L" :y1="tick.y"
                        :x2="CHART_W - CHART_PAD_R" :y2="tick.y"
                        stroke="rgba(255,255,255,0.05)" stroke-width="1"
                      />
                      <text
                        :x="CHART_PAD_L - 4" :y="tick.y + 3"
                        text-anchor="end" font-size="8" fill="var(--text-muted)"
                      >{{ tick.label }}</text>
                    </template>

                    <!-- Area fills -->
                    <path
                      v-for="(pathD, fi) in buildChartPaths(chart).paths"
                      :key="`area-${fi}`"
                      :d="pathD"
                      :fill="`url(#hd-grad-${chart.key}-${fi})`"
                      stroke="none"
                    />

                    <!-- Line strokes (re-trace just the top line without close) -->
                    <polyline
                      v-for="(f, fi) in chart.fields"
                      :key="`line-${fi}`"
                      :points="chart.buckets
                        .map((b, i) => {
                          const v = b[f.key]
                          if (v == null || !isFinite(v)) return null
                          const W2 = CHART_W - CHART_PAD_L - CHART_PAD_R
                          const H2 = CHART_H - CHART_PAD_T - CHART_PAD_B
                          const allV = chart.buckets.map(bk => bk[f.key]).filter(x => x != null && isFinite(x))
                          const minV2 = Math.min(...allV); const maxV2 = Math.max(...allV)
                          const rng = maxV2 - minV2 || 1
                          const x = CHART_PAD_L + (i / Math.max(chart.buckets.length - 1, 1)) * W2
                          const y = CHART_PAD_T + H2 - ((v - minV2) / rng) * H2
                          return `${x},${y}`
                        })
                        .filter(Boolean)
                        .join(' ')
                      "
                      :stroke="LINE_COLORS[fi % LINE_COLORS.length]"
                      stroke-width="1.5"
                      fill="none"
                      stroke-linejoin="round"
                      stroke-linecap="round"
                    />

                    <!-- X axis labels -->
                    <template v-for="xl in buildChartPaths(chart).xLabels" :key="xl.label">
                      <text
                        :x="xl.x" :y="CHART_H + CHART_PAD_T + CHART_PAD_B - 2"
                        :text-anchor="xl === buildChartPaths(chart).xLabels[0] ? 'start' : 'end'"
                        font-size="8" fill="var(--text-muted)"
                      >{{ xl.label }}</text>
                    </template>
                  </svg>
                </div>

                <!-- Field legend -->
                <div v-if="chart.fields.length > 1" class="hd-chart-legend">
                  <span v-for="(f, fi) in chart.fields" :key="f.key" class="hd-legend-item">
                    <span class="hd-legend-dot" :style="{ background: LINE_COLORS[fi % LINE_COLORS.length] }"></span>
                    {{ f.key }}
                  </span>
                </div>
              </div>
            </div>
          </div>
        </div>

      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.inv-root { background: var(--bg-body); }

/* Empty state */
.inv-empty {
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  gap: 12px; padding: 80px 24px; color: var(--text-muted); text-align: center;
}
.inv-empty--compact { padding: 40px 16px; }
.inv-empty-title { font-size: 16px; font-weight: 600; color: var(--text-primary); }
.inv-empty-sub { font-size: 13px; max-width: 400px; }

/* Toolbar */
.inv-toolbar {
  display: flex; align-items: center; gap: 12px;
  padding: 12px 14px; flex-wrap: wrap;
}
.inv-search {
  display: flex; align-items: center; gap: 8px;
  background: var(--bg-body); border: 1px solid var(--border);
  border-radius: 6px; padding: 0 11px; flex: 1 1 300px; min-width: 260px;
  min-height: 30px;
  color: var(--text-muted);
}
.inv-search-input {
  border: none; outline: none; background: transparent;
  color: var(--text-primary); font-size: 13px; width: 100%; font-family: inherit;
}
.inv-search-input::placeholder { color: var(--text-muted); }
.inv-toolbar-right { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; justify-content: flex-end; }

/* Time range pills */
.inv-time-pills { display: flex; align-items: center; gap: 2px; flex-wrap: wrap; }
.inv-time-pill {
  min-height: 28px; display: inline-flex; align-items: center; justify-content: center;
  font-size: 11px; padding: 4px 9px; border: 1px solid var(--border);
  border-radius: 4px; background: transparent; color: var(--text-muted);
  cursor: pointer; transition: all 0.1s; font-family: inherit; line-height: 1;
}
.inv-time-pill:hover { border-color: var(--brand); color: var(--text-primary); }
.inv-time-pill--active { border-color: var(--brand); color: var(--brand); background: rgba(99,102,241,0.08); }

/* Auto-refresh button */
.inv-auto-refresh-btn {
  display: flex; align-items: center; gap: 5px;
  min-height: 28px; font-size: 11px; padding: 4px 10px; border: 1px solid var(--border);
  border-radius: 4px; background: transparent; color: var(--text-muted);
  cursor: pointer; transition: all 0.1s; font-family: inherit; line-height: 1;
}
.inv-auto-refresh-btn:hover { border-color: var(--brand); }
.inv-auto-refresh-btn--on { border-color: #22c55e; color: #22c55e; background: rgba(34,197,94,0.08); }
.inv-auto-refresh-btn:disabled { opacity: 0.45; cursor: not-allowed; }
.inv-view-toggle {
  display: inline-flex; align-items: center; gap: 2px;
  height: 28px; padding: 2px; border: 1px solid var(--border);
  border-radius: 6px; background: var(--bg-body);
}
.inv-view-btn,
.inv-icon-btn {
  width: 26px; height: 26px; display: inline-flex; align-items: center; justify-content: center;
  border: 0; border-radius: 4px; background: transparent;
  color: var(--text-muted); cursor: pointer; position: relative;
  font-family: inherit; transition: background 0.12s, color 0.12s;
}
.inv-view-btn:hover,
.inv-icon-btn:hover {
  background: rgba(255,255,255,0.06); color: var(--text-primary);
}
.inv-view-btn--active,
.inv-icon-btn--active {
  background: rgba(99,102,241,0.12); color: var(--brand);
}
.inv-anomaly-btn,
.inv-compare-btn {
  display: inline-flex; align-items: center; gap: 6px;
  min-height: 28px; padding: 4px 10px;
  border: 1px solid var(--border); border-radius: 5px;
  background: transparent; color: var(--text-muted);
  font-size: 11px; font-weight: 600; font-family: inherit;
  cursor: pointer; white-space: nowrap;
  transition: background 0.12s, border-color 0.12s, color 0.12s;
}
.inv-anomaly-btn:hover,
.inv-compare-btn:hover {
  border-color: var(--brand); color: var(--text-primary);
}
.inv-anomaly-btn--active,
.inv-compare-btn--active {
  border-color: var(--brand); background: rgba(99,102,241,0.1); color: var(--brand);
}
.inv-anomaly-dot {
  width: 7px; height: 7px; border-radius: 50%;
  background: #f59e0b; box-shadow: 0 0 0 2px rgba(245,158,11,0.14);
  flex-shrink: 0;
}
.inv-icon-btn--danger { color: #f87171; }
.inv-icon-btn--danger:hover {
  background: rgba(248,113,113,0.12); color: #ef4444;
}
.inv-badge {
  position: absolute; top: -5px; right: -5px;
  min-width: 16px; height: 16px; padding: 0 4px;
  display: inline-flex; align-items: center; justify-content: center;
  border-radius: 999px; background: #ef4444; color: #fff;
  font-size: 9px; font-weight: 700; line-height: 1;
  box-shadow: 0 0 0 2px var(--bg-surface);
}
.inv-ts { font-size: 11px; color: var(--text-muted); }

/* Filter bar */
.inv-filter-bar {
  display: flex; align-items: center; gap: 8px 10px;
  padding: 10px 14px; flex-wrap: wrap;
}
.inv-filter-label { font-size: 12px; color: var(--text-muted); flex-shrink: 0; }
.inv-divider { width: 1px; height: 20px; background: var(--border); flex-shrink: 0; }

/* Dropdown */
.inv-dropdown { position: relative; }
.inv-dropdown-btn {
  display: flex; align-items: center; gap: 5px;
  min-height: 30px; font-size: 12px; padding: 5px 10px; border: 1px solid var(--border);
  border-radius: 5px; background: var(--bg-body); color: var(--text-primary);
  cursor: pointer; font-family: inherit; transition: border-color 0.1s;
}
.inv-dropdown-btn:hover { border-color: var(--brand); }
.inv-menu {
  position: absolute; top: calc(100% + 4px); left: 0; z-index: 200;
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 7px; min-width: 200px; padding: 4px;
  box-shadow: 0 8px 24px rgba(0,0,0,0.3);
}
.inv-menu-item {
  display: block; width: 100%; text-align: left;
  padding: 7px 10px; font-size: 12px; border: none;
  background: transparent; color: var(--text-primary);
  cursor: pointer; border-radius: 4px; font-family: inherit;
  transition: background 0.1s;
}
.inv-menu-item:hover { background: rgba(255,255,255,0.06); }
.inv-menu-item--active { color: var(--brand); background: rgba(99,102,241,0.08); }
.inv-custom-input {
  min-height: 30px; padding: 5px 10px; border: 1px solid var(--brand); border-radius: 5px;
  background: var(--bg-body); color: var(--text-primary);
  font-size: 12px; font-family: var(--mono, monospace); outline: none; width: 220px;
}

/* Table */
.inv-table-panel { overflow: hidden; }
.inv-loading-bar {
  height: 2px; background: linear-gradient(90deg, var(--brand) 0%, transparent 100%);
  background-size: 200%; animation: shimmer 1.2s infinite;
}
@keyframes shimmer { 0%{background-position:200%} 100%{background-position:-200%} }

.inv-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.inv-th {
  padding: 11px 14px; border-bottom: 1px solid var(--border);
  font-size: 11px; font-weight: 700; text-transform: uppercase;
  letter-spacing: 0.08em; color: var(--text-muted); text-align: left;
  background: rgba(255,255,255,0.02); cursor: pointer; user-select: none;
  white-space: nowrap;
}
.inv-th:hover { color: var(--text-primary); }
.inv-th--name { min-width: 220px; }
.inv-th--metric { text-align: right; width: 140px; }
.inv-th--ts { text-align: right; width: 100px; }
.inv-th--check { width: 36px; min-width: 36px; }
.inv-th--pin { width: 34px; min-width: 34px; }
.inv-sort-icon { font-size: 10px; margin-left: 4px; opacity: 0.5; }
.inv-sort-icon.active { opacity: 1; color: var(--brand); }

.inv-row:hover td { background: rgba(255,255,255,0.025); }
.inv-row--pinned td { background: rgba(99,102,241,0.035); }
.inv-td { padding: 11px 14px; border-bottom: 1px solid var(--border); color: var(--text-primary); vertical-align: middle; }
.inv-row:last-child .inv-td { border-bottom: none; }
.inv-td--name { font-weight: 500; }
.inv-td--metric { text-align: right; }
.inv-td--ts { text-align: right; font-size: 11px; color: var(--text-muted); }

.inv-host-icon { display: inline-flex; color: var(--text-muted); margin-right: 8px; vertical-align: middle; }

.inv-metric-cell { display: flex; align-items: center; justify-content: flex-end; gap: 8px; }
.inv-metric-val { font-variant-numeric: tabular-nums; min-width: 50px; text-align: right; }
.inv-mini-bar-wrap { width: 60px; height: 4px; background: rgba(255,255,255,0.08); border-radius: 2px; overflow: hidden; flex-shrink: 0; }
.inv-mini-bar { height: 100%; border-radius: 2px; transition: width 0.3s; }

.inv-empty-cell { text-align: center; padding: 48px; color: var(--text-muted); }
.inv-no-data { display: flex; flex-direction: column; align-items: center; gap: 10px; font-size: 13px; }
.inv-no-data--panel { padding: 20px; }
.inv-no-data code { font-family: var(--mono, monospace); font-size: 11px; background: rgba(255,255,255,0.05); padding: 1px 6px; border-radius: 3px; }

/* Row actions and table affordances */
.inv-th--spark { width: 92px; text-align: center; }
.inv-td--pin { width: 34px; padding-left: 6px; padding-right: 4px; }
.inv-td--spark { text-align: center; }
.inv-sparkline { display: inline-block; vertical-align: middle; overflow: visible; }
.inv-pin-btn {
  width: 24px; height: 24px; display: inline-flex; align-items: center; justify-content: center;
  border: 1px solid transparent; border-radius: 5px; background: transparent;
  color: var(--text-muted); cursor: pointer; transition: background 0.12s, border-color 0.12s, color 0.12s;
}
.inv-pin-btn:hover { border-color: var(--brand); color: var(--brand); background: rgba(99,102,241,0.08); }
.inv-pin-btn--active { color: var(--brand); background: rgba(99,102,241,0.12); }
.inv-section-header td {
  background: rgba(255,255,255,0.025);
  border-bottom: 1px solid var(--border);
}
.inv-section-label {
  padding: 8px 16px; color: var(--text-muted);
  font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.06em;
}
.inv-section-label svg { margin-right: 6px; vertical-align: -1px; color: var(--brand); }
.inv-anomaly-dot-inline {
  display: inline-block; width: 7px; height: 7px; margin-right: 7px;
  border-radius: 50%; background: #f59e0b; box-shadow: 0 0 0 2px rgba(245,158,11,0.14);
  vertical-align: middle;
}

/* Footer */
.inv-footer {
  display: flex; align-items: center; justify-content: space-between;
  padding: 10px 14px; border-top: 1px solid var(--border);
  flex-wrap: wrap; gap: 8px;
}
.inv-footer-left { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--text-muted); }
.inv-count { color: var(--text-primary); font-weight: 600; }
.inv-sep { opacity: 0.4; }
.inv-per-page { display: flex; align-items: center; gap: 5px; }
.inv-per-page-sel { border: 1px solid var(--border); border-radius: 4px; background: var(--bg-body); color: var(--text-primary); font-size: 12px; padding: 2px 4px; outline: none; }
.inv-pagination { display: flex; align-items: center; gap: 6px; }
.inv-page-btn {
  width: 28px; height: 28px; display: flex; align-items: center; justify-content: center;
  border: 1px solid var(--border); border-radius: 5px; background: transparent;
  color: var(--text-muted); cursor: pointer; transition: all 0.1s;
}
.inv-page-btn:hover:not(:disabled) { border-color: var(--brand); color: var(--brand); }
.inv-page-btn:disabled { opacity: 0.3; cursor: default; }
.inv-page-info { font-size: 12px; color: var(--text-muted); }

/* Compare bar */
.inv-compare-bar {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 14px; flex-wrap: wrap;
}
.inv-compare-count {
  font-size: 12px; font-weight: 600; color: var(--text-primary);
}
.inv-compare-chips {
  display: flex; align-items: center; gap: 6px; flex: 1;
  min-width: 180px; flex-wrap: wrap;
}
.inv-compare-chip {
  display: inline-flex; align-items: center; gap: 6px;
  max-width: 220px; padding: 4px 6px 4px 9px;
  border: 1px solid rgba(99,102,241,0.35); border-radius: 999px;
  background: rgba(99,102,241,0.08); color: var(--text-primary);
  font-size: 12px; font-weight: 500;
}
.inv-compare-chip-remove {
  width: 18px; height: 18px; display: inline-flex; align-items: center; justify-content: center;
  border: 0; border-radius: 50%; background: rgba(255,255,255,0.08);
  color: var(--text-muted); cursor: pointer; font: inherit; line-height: 1;
  transition: background 0.12s, color 0.12s;
}
.inv-compare-chip-remove:hover,
.inv-compare-chip-remove:focus-visible {
  background: rgba(239,68,68,0.14); color: #ef4444; outline: none;
}

/* Alert rules panel */
.inv-side-panel {
  position: relative;
  padding: 0;
  overflow: hidden;
}
.inv-side-panel-header {
  position: relative;
  display: flex; align-items: center; justify-content: space-between;
  min-height: 48px; padding: 12px 48px 12px 16px; border-bottom: 1px solid var(--border);
}
.inv-side-panel .hd-close-btn { right: 12px; }
.inv-side-panel-title {
  font-size: 12px; font-weight: 700; color: var(--text-primary);
  letter-spacing: 0.03em; text-transform: uppercase;
}
.inv-alert-list {
  display: flex; flex-direction: column; gap: 8px;
  padding: 12px 14px; max-height: 260px; overflow-y: auto;
}
.inv-side-empty {
  padding: 14px; border: 1px dashed var(--border); border-radius: 6px;
  color: var(--text-muted); font-size: 12px; text-align: center;
}
.inv-alert-row {
  display: flex; align-items: center; justify-content: space-between; gap: 12px;
  padding: 10px; border: 1px solid var(--border); border-radius: 7px;
  background: var(--bg-body);
}
.inv-alert-info { min-width: 0; display: flex; flex-direction: column; gap: 3px; }
.inv-alert-name {
  color: var(--text-primary); font-size: 12px; font-weight: 700;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.inv-alert-meta {
  color: var(--text-muted); font-size: 10.5px; font-family: var(--mono, monospace);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.inv-alert-actions { display: flex; align-items: center; gap: 6px; flex-shrink: 0; }
.inv-toggle-btn {
  min-width: 42px; height: 24px; padding: 0 9px;
  border: 1px solid var(--border); border-radius: 999px;
  background: transparent; color: var(--text-muted);
  font-size: 10px; font-weight: 700; font-family: inherit;
  cursor: pointer; transition: background 0.12s, border-color 0.12s, color 0.12s;
}
.inv-toggle-btn:hover { border-color: var(--brand); color: var(--text-primary); }
.inv-toggle-btn--on {
  border-color: #22c55e; background: rgba(34,197,94,0.1); color: #22c55e;
}
.inv-alert-form {
  display: flex; flex-direction: column; gap: 8px;
  padding: 12px 14px 14px; border-top: 1px solid var(--border);
  background: rgba(255,255,255,0.015);
}
.inv-form-title {
  font-size: 11px; font-weight: 700; color: var(--text-primary);
  text-transform: uppercase; letter-spacing: 0.04em;
}
.inv-form-row {
  display: flex; align-items: center; gap: 8px;
}
.inv-form-row--inline { margin-top: -1px; }
.inv-form-hint {
  font-size: 11px; color: var(--text-muted);
}
.inv-form-submit { width: 100%; margin-top: 2px; }

@keyframes spin { to { transform: rotate(360deg); } }
.spin { animation: spin 0.8s linear infinite; }

/* Custom range picker */
.inv-custom-range-wrap { position: relative; }
.inv-time-pill--custom {
  display: flex; align-items: center; gap: 4px; max-width: 280px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.inv-date-picker {
  position: absolute; top: calc(100% + 6px); right: 0; z-index: 300;
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 8px; padding: 12px; min-width: 260px;
  box-shadow: 0 8px 32px rgba(0,0,0,0.4);
  display: flex; flex-direction: column; gap: 6px;
}
.inv-date-picker--left { right: auto; left: 0; }
.inv-date-picker-title {
  font-size: 12px; font-weight: 700; color: var(--text-primary);
  margin-bottom: 4px;
}
.inv-date-label {
  font-size: 11px; color: var(--text-muted); font-weight: 600;
  text-transform: uppercase; letter-spacing: 0.06em;
}
.inv-date-input {
  min-height: 30px; padding: 6px 10px; border: 1px solid var(--border); border-radius: 5px;
  background: var(--bg-body); color: var(--text-primary);
  font-size: 12px; font-family: var(--mono, monospace); outline: none; width: 100%;
  box-sizing: border-box; cursor: pointer;
}
.inv-date-input--short {
  width: 72px; min-width: 72px; flex: 0 0 72px;
}
.inv-date-input:focus { border-color: var(--brand); }
.inv-date-actions {
  display: flex; justify-content: flex-end; gap: 6px; margin-top: 6px;
}

/* Clickable row */
.inv-row--clickable { cursor: pointer; }
.inv-row--clickable:hover td { background: rgba(99,102,241,0.06); }
.inv-host-link { color: var(--brand); text-decoration: underline; text-decoration-style: dotted; }

/* Heatmap */
.inv-heatmap-panel { overflow: hidden; padding: 12px; }
.inv-heatmap-grid {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 10px;
}
.inv-heatmap-cell {
  position: relative; min-height: 72px; padding: 10px;
  border: 1px solid rgba(255,255,255,0.08); border-radius: 7px;
  cursor: pointer; overflow: hidden; transition: transform 0.12s, border-color 0.12s;
}
.inv-heatmap-cell:hover {
  transform: translateY(-1px); border-color: var(--brand);
}
.inv-heatmap-name {
  color: #fff; font-size: 12px; font-weight: 700;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  text-shadow: 0 1px 2px rgba(0,0,0,0.35);
}
.inv-heatmap-val {
  margin-top: 8px; color: #fff; font-size: 18px; font-weight: 800;
  font-variant-numeric: tabular-nums; text-shadow: 0 1px 2px rgba(0,0,0,0.35);
}
.inv-anomaly-badge {
  position: absolute; right: 8px; top: 8px;
  width: 18px; height: 18px; display: inline-flex; align-items: center; justify-content: center;
  border-radius: 50%; background: #f59e0b; color: #111827;
  font-size: 11px; font-weight: 900;
}

/* Host Detail Overlay */
.hd-overlay {
  position: fixed; inset: 0; z-index: 500;
  background: rgba(0,0,0,0.55); backdrop-filter: blur(2px);
  display: flex; align-items: stretch; justify-content: flex-end;
}
.hd-panel {
  width: min(900px, 100vw); height: 100vh; overflow: hidden;
  background: var(--bg-surface); border-left: 1px solid var(--border);
  display: flex; flex-direction: column;
  box-shadow: -8px 0 40px rgba(0,0,0,0.4);
}

/* Header */
.hd-header {
  position: relative;
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  padding: 10px 52px 10px 14px; border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.hd-back-btn {
  display: flex; align-items: center; gap: 5px;
  min-height: 30px; font-size: 12px; padding: 5px 10px; border: 1px solid var(--border);
  border-radius: 5px; background: transparent; color: var(--text-muted);
  cursor: pointer; font-family: inherit; transition: all 0.1s; flex-shrink: 0;
}
.hd-back-btn:hover { border-color: var(--brand); color: var(--brand); }
.hd-host-name {
  display: flex; align-items: center; gap: 6px;
  font-size: 14px; font-weight: 700; color: var(--text-primary);
  flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.hd-time-pills { display: flex; gap: 2px; flex-shrink: 0; flex-wrap: wrap; }
.hd-refresh-btn { flex-shrink: 0; }
.hd-tabs {
  display: flex; gap: 2px; flex-shrink: 0;
  background: rgba(255,255,255,0.04); border: 1px solid var(--border);
  border-radius: 7px; padding: 2px;
}
.hd-tab {
  padding: 4px 12px; border-radius: 5px; border: none; background: transparent;
  font-size: 12px; font-weight: 500; color: var(--text-muted);
  cursor: pointer; font-family: inherit; transition: all 0.15s; white-space: nowrap;
}
.hd-tab:hover { color: var(--text-primary); background: rgba(255,255,255,0.06); }
.hd-tab--active { background: var(--brand); color: #fff; }
.hd-close-btn {
  position: absolute; right: 16px; top: 50%; transform: translateY(-50%);
  width: 28px; height: 28px; display: flex; align-items: center; justify-content: center;
  border: 1px solid var(--border); border-radius: 5px; background: transparent;
  color: var(--text-muted); cursor: pointer; transition: all 0.1s;
}
.hd-close-btn:hover { border-color: #ef4444; color: #ef4444; }

.hd-annotation-form {
  display: flex; flex-direction: column; gap: 8px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-elevated);
  box-shadow: 0 6px 18px rgba(0,0,0,0.18);
}
.hd-annotation-row {
  display: flex; align-items: center; gap: 8px;
}
.hd-annotation-label {
  font-size: 11px; color: var(--text-muted); flex-shrink: 0;
}
.hd-color-input {
  width: 40px; height: 30px; padding: 0;
  border: 1px solid var(--border); border-radius: 5px;
  background: transparent; cursor: pointer;
}
.hd-annotation-time { flex: 1; min-width: 180px; }
.hd-annotation-actions {
  display: flex; gap: 8px; justify-content: flex-end;
}

/* Body */
.hd-body {
  display: flex; flex: 1; overflow: hidden;
}

/* Sidebar */
.hd-sidebar {
  width: 130px; flex-shrink: 0; border-right: 1px solid var(--border);
  padding: 12px 8px; display: flex; flex-direction: column; gap: 4px;
  overflow-y: auto;
}
.hd-cat-btn {
  display: flex; align-items: center; gap: 8px;
  width: 100%; text-align: left; padding: 8px 10px;
  font-size: 12px; font-weight: 500; border: none; border-radius: 6px;
  background: transparent; color: var(--text-muted); cursor: pointer;
  font-family: inherit; transition: all 0.15s;
}
.hd-cat-btn:hover { background: rgba(255,255,255,0.05); color: var(--text-primary); }
.hd-cat-btn--active { background: rgba(99,102,241,0.12); color: var(--brand); }

/* Charts area */
.hd-charts { flex: 1; overflow-y: auto; padding: 14px; }
.hd-charts-loading {
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  gap: 10px; height: 200px; color: var(--text-muted); font-size: 13px;
}

.hd-charts-grid {
  display: grid; grid-template-columns: 1fr 1fr; gap: 12px;
}
@media (max-width: 640px) {
  .hd-charts-grid { grid-template-columns: 1fr; }
}

.hd-chart-card {
  background: var(--bg-body); border: 1px solid var(--border);
  border-radius: 8px; padding: 12px; overflow: hidden;
}
.hd-chart-header {
  display: flex; justify-content: space-between; align-items: center;
  margin-bottom: 8px;
}
.hd-chart-title { font-size: 12px; font-weight: 600; color: var(--text-primary); }
.hd-chart-latest {
  font-size: 13px; font-weight: 700; color: var(--brand);
  font-variant-numeric: tabular-nums;
}
.hd-chart-wrap { width: 100%; }
.hd-svg { width: 100%; height: auto; display: block; overflow: visible; }

.hd-chart-legend {
  display: flex; flex-wrap: wrap; gap: 8px; margin-top: 6px;
}
.hd-legend-item {
  display: flex; align-items: center; gap: 4px;
  font-size: 10px; color: var(--text-muted);
}
.hd-legend-dot {
  width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0;
}

.inv-tab-content { display: contents; }

/* ── Main tab switcher ────────────────────────────────────────── */
.inv-main-tabs {
  display: flex; gap: 2px; padding: 0 4px;
}
.inv-main-tab {
  display: flex; align-items: center; gap: 6px;
  padding: 8px 14px; border-radius: 8px 8px 0 0;
  border: none; border-bottom: 2px solid transparent;
  background: transparent; font-size: 12px; font-weight: 500;
  color: var(--text-muted); cursor: pointer; font-family: inherit; transition: all 0.15s;
}
.inv-main-tab:hover { color: var(--text-primary); background: rgba(255,255,255,0.04); }
.inv-main-tab--active { color: var(--brand); border-bottom-color: var(--brand); }

/* ── Cluster tab ─────────────────────────────────────────────── */
.cl-header-bar { display: flex; align-items: center; gap: 8px; padding: 8px 12px; }
.cl-loading-bar { margin: 0 0 8px; }

.cl-health-banner {
  display: flex; align-items: center; gap: 24px; flex-wrap: wrap;
  padding: 14px 18px; border-left: 3px solid transparent;
}
.cl-health--green { border-left-color: #22c55e; }
.cl-health--yellow { border-left-color: #f59e0b; }
.cl-health--red { border-left-color: #ef4444; }
.cl-health--unknown { border-left-color: var(--border); }

.cl-health-status { display: flex; align-items: center; gap: 10px; flex: 1; }
.cl-health-dot {
  width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0; background: var(--text-muted);
}
.cl-health--green .cl-health-dot { background: #22c55e; box-shadow: 0 0 6px #22c55e88; }
.cl-health--yellow .cl-health-dot { background: #f59e0b; box-shadow: 0 0 6px #f59e0b88; }
.cl-health--red .cl-health-dot { background: #ef4444; box-shadow: 0 0 6px #ef444488; }
.cl-health-name { font-size: 14px; font-weight: 600; color: var(--text-primary); }
.cl-health-label { font-size: 11px; font-weight: 700; letter-spacing: 0.05em; color: var(--text-muted); }
.cl-health--green .cl-health-label { color: #22c55e; }
.cl-health--yellow .cl-health-label { color: #f59e0b; }
.cl-health--red .cl-health-label { color: #ef4444; }

.cl-health-stats { display: flex; gap: 20px; }
.cl-stat { text-align: center; }
.cl-stat-val { font-size: 18px; font-weight: 700; color: var(--text-primary); font-variant-numeric: tabular-nums; }
.cl-stat-label { font-size: 10px; color: var(--text-muted); margin-top: 2px; }
.cl-stat--warn .cl-stat-val { color: #f59e0b; }

.cl-section-title {
  font-size: 11px; font-weight: 600; color: var(--text-muted);
  letter-spacing: 0.06em; text-transform: uppercase;
  margin-bottom: 10px; padding-bottom: 8px; border-bottom: 1px solid var(--border);
}

.cl-master-star { color: #f59e0b; margin-right: 4px; font-size: 11px; }

.cl-role-badge {
  display: inline-flex; align-items: center;
  padding: 2px 7px; border-radius: 4px; font-size: 10px; font-weight: 600;
  background: rgba(99,102,241,0.1); color: var(--brand); letter-spacing: 0.03em;
  font-family: monospace;
}

.cl-idx-status {
  display: inline-flex; padding: 2px 7px; border-radius: 4px;
  font-size: 10px; font-weight: 600;
}
.cl-idx-status--green { background: rgba(34,197,94,0.1); color: #22c55e; }
.cl-idx-status--yellow { background: rgba(245,158,11,0.1); color: #f59e0b; }
.cl-idx-status--red { background: rgba(239,68,68,0.1); color: #ef4444; }
.cl-idx-status--close { background: rgba(100,100,100,0.1); color: var(--text-muted); }
</style>
