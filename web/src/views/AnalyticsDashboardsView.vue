<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import axios from 'axios'
import { useRoute } from 'vue-router'
import { useSavedQueries } from '@/composables/useSavedQueries'
import { useConnections } from '@/composables/useConnections'
import { useToast } from '@/composables/useToast'
import { pendingDashboardBlock } from '@/composables/usePendingDashboardBlock'
import { formatServerTimestamp } from '@/utils/datetime'
import {
  downloadBlob,
  downloadJSON,
  downloadText,
  downloadXLSX,
  sanitizeFileName,
  XLSX_STYLES,
} from '@/utils/export'
import type { XLSXSheet } from '@/utils/export'
import ResultChart from '@/components/ui/ResultChart.vue'

interface Dashboard {
  id: number
  name: string
  description: string
  visibility: 'private' | 'shared' | 'public'
  share_token: string
  default_preset: string
  created_at: string
  updated_at: string
}

interface DashboardViewPreset {
  name: string
  global_filter: string
  params: Record<string, string>
}

interface DashboardAccessEntry {
  user_id: number
  username: string
  access_level: 'viewer' | 'editor'
}

type DashboardChartType = 'table' | 'bar' | 'horizontal-bar' | 'line' | 'area' | 'scatter' | 'pie' | 'donut' | 'kpi'
type RenderableDashboardChartType = Exclude<DashboardChartType, 'table' | 'kpi'>

interface DashboardUserOption {
  id: number
  username: string
  role: string
}

interface DashboardBlock {
  id: number
  dashboard_id: number
  saved_query_id: number
  title: string
  chart_type: DashboardChartType
  x_key: string
  y_key: string
  column_span: 1 | 2 | 3
  row_span: 1 | 2 | 3
  params: DashboardBlockParam[]
  sort_order: number
}

interface DashboardBlockParam {
  name: string
  label: string
  type: 'text' | 'number' | 'date'
  default_value: string
}

interface DashboardDetail extends Dashboard {
  presets: DashboardViewPreset[]
  access: DashboardAccessEntry[]
  blocks: DashboardBlock[]
}

interface RenderBlock extends DashboardBlock {
  connection_id: number
  query_name: string
  description: string
  sql: string
  columns: string[]
  rows: any[][]
  row_count: number
  duration_ms: number
  error: string
}

interface RenderedDashboard {
  id: number
  name: string
  description: string
  params: Array<{ name: string; label: string; type: string; value: string }>
  blocks: RenderBlock[]
}

interface DashboardQueryPreview {
  columns: string[]
  rows: any[][]
  row_count: number
  duration_ms: number
}

interface SemanticFilterOption {
  key: string
  label: string
  options: string[]
  blockIds: number[]
}

const toast = useToast()
const route = useRoute()
const { queries, fetchAll: fetchSavedQueries, save: saveSavedQuery } = useSavedQueries()
const { connections, fetchConnections } = useConnections()

const dashboards = ref<Dashboard[]>([])
const selectedDashboardId = ref<number | null>(null)
const dashboardDetail = ref<DashboardDetail | null>(null)
const rendered = ref<RenderedDashboard | null>(null)
const loading = ref(false)
const rendering = ref(false)
const creating = ref(false)
const savingPreset = ref(false)
const savingAccess = ref(false)
const loadingUsers = ref(false)
const createDashboardOpen = ref(false)

const newDashboardName = ref('')
const newDashboardDescription = ref('')

const blockSourceMode = ref<'sql' | 'saved'>('sql')
const newBlockQueryId = ref<number | null>(null)
const newBlockChartType = ref<DashboardChartType>('table')
const newBlockTitle = ref('')
const newBlockConnId = ref<number | null>(null)
const newBlockSQL = ref('')
const blockPreview = ref<DashboardQueryPreview | null>(null)
const blockPreviewError = ref('')
const blockPreviewing = ref(false)
const savingDraftBlock = ref(false)
const blockPreviewSignature = ref('')
const editingBlockId = ref<number | null>(null)
const editingBlock = ref<DashboardBlock | null>(null)
const draggingBlockId = ref<number | null>(null)
const droppingBlockId = ref<number | null>(null)
const resizingBlockId = ref<number | null>(null)
const dragX = ref(0)
const dragY = ref(0)
const dragGhostTitle = ref('')
const dragGhostType = ref('')
const renamingBlockId = ref<number | null>(null)
const renameDraft = ref('')
const editMode = ref(false)
const globalFilter = ref('')
const dashboardParams = ref<Record<string, string>>({})
const semanticFilters = ref<Record<string, string[]>>({})
const blockTextFilters = ref<Record<number, string>>({})
const blockColumnFilters = ref<Record<number, Record<string, string>>>({})
const presetName = ref('')
const selectedPresetName = ref('')
const dashboardUsers = ref<DashboardUserOption[]>([])
const accessUserId = ref<number | null>(null)
const accessLevel = ref<DashboardAccessEntry['access_level']>('viewer')
const dashboardScreenRef = ref<HTMLElement | null>(null)
const exportMenuOpen = ref<'dashboard' | `block-${number}` | ''>('')
const exportBusy = ref('')

const selectedDashboard = computed(() =>
  dashboards.value.find((item) => item.id === selectedDashboardId.value) ?? null,
)
const sharedToken = computed(() => String(route.params.token ?? '').trim())
const isEmbedView = computed(() => route.name === 'embed-dashboard' || route.name === 'embed-dashboard-block')
const embedBlockId = computed(() => {
  const id = Number(route.params.blockId)
  return Number.isFinite(id) && id > 0 ? id : null
})
const isSharedView = computed(() =>
  (route.name === 'shared-dashboard' || isEmbedView.value) && sharedToken.value !== '',
)
const canEditDashboard = computed(() => !isSharedView.value && !!selectedDashboardId.value)
const shareURL = computed(() => {
  const token = dashboardDetail.value?.share_token?.trim()
  if (!token || typeof window === 'undefined') return ''
  return `${window.location.origin}/shared-dashboards/${token}`
})
const dashboardEmbedURL = computed(() => {
  const token = dashboardDetail.value?.share_token?.trim()
  if (!token || typeof window === 'undefined') return ''
  return `${window.location.origin}/embed/dashboards/${token}`
})
const dashboardEmbedCode = computed(() =>
  dashboardEmbedURL.value
    ? `<iframe src="${dashboardEmbedURL.value}" width="100%" height="720" style="border:0;border-radius:8px;overflow:hidden" loading="lazy" referrerpolicy="no-referrer-when-downgrade"></iframe>`
    : '',
)

const availableQueries = computed(() => [...queries.value].sort((a, b) => a.name.localeCompare(b.name)))
const selectedDraftSavedQuery = computed(() =>
  newBlockQueryId.value != null ? queries.value.find((item) => item.id === newBlockQueryId.value) ?? null : null,
)
const draftBlockSQL = computed(() =>
  blockSourceMode.value === 'saved' ? (selectedDraftSavedQuery.value?.sql ?? '') : newBlockSQL.value,
)
const draftBlockConnId = computed(() =>
  blockSourceMode.value === 'saved' ? (selectedDraftSavedQuery.value?.conn_id ?? null) : newBlockConnId.value,
)
const draftBlockTitle = computed(() => {
  const typed = newBlockTitle.value.trim()
  if (typed) return typed
  if (blockSourceMode.value === 'saved') return selectedDraftSavedQuery.value?.name ?? ''
  return 'Dashboard Query'
})
const draftBlockSignature = computed(() =>
  `${blockSourceMode.value}:${draftBlockConnId.value ?? ''}:${draftBlockSQL.value.trim()}:${newBlockChartType.value}`,
)
const canPreviewBlock = computed(() => !!draftBlockConnId.value && !!draftBlockSQL.value.trim() && !blockPreviewing.value)
const canSavePreviewedBlock = computed(() =>
  !!selectedDashboardId.value &&
  !!draftBlockConnId.value &&
  !!draftBlockSQL.value.trim() &&
  !!blockPreview.value &&
  blockPreviewSignature.value === draftBlockSignature.value &&
  !savingDraftBlock.value,
)
const assignableUsers = computed(() =>
  dashboardUsers.value.filter((user) =>
    !(dashboardDetail.value?.access ?? []).some((entry) => entry.user_id === user.id),
  ),
)
const filteredRenderedBlocks = computed(() => {
  const blocks = embedBlockId.value
    ? (rendered.value?.blocks ?? []).filter((block) => block.id === embedBlockId.value)
    : (rendered.value?.blocks ?? [])
  return blocks.map((block) => {
    const rows = filteredRowsForBlock(block)
    return {
      ...block,
      rows,
      row_count: rows.length,
    }
  })
})
const semanticFilterOptions = computed<SemanticFilterOption[]>(() => {
  const blocks = rendered.value?.blocks ?? []
  const byKey = new Map<string, SemanticFilterOption>()
  for (const block of blocks) {
    for (const [index, column] of block.columns.entries()) {
      if (!isSemanticFilterCandidate(column, block.rows, index)) continue
      const key = normalizeSemanticKey(column)
      const existing = byKey.get(key) ?? {
        key,
        label: prettifySemanticLabel(column),
        options: [],
        blockIds: [],
      }
      const optionSet = new Set(existing.options)
      for (const row of block.rows) {
        const value = normalizeSemanticOptionValue(row[index])
        if (value) optionSet.add(value)
      }
      existing.options = [...optionSet].sort((a, b) => a.localeCompare(b))
      existing.blockIds = [...new Set([...existing.blockIds, block.id])]
      if (!byKey.has(key)) byKey.set(key, existing)
    }
  }
  return [...byKey.values()]
    .filter((item) => item.options.length > 1 && item.options.length <= 20)
    .sort((a, b) => a.label.localeCompare(b.label))
})

const chartTypeOptions = [
  { value: 'table', label: 'Table' },
  { value: 'bar', label: 'Bar' },
  { value: 'horizontal-bar', label: 'Horizontal Bar' },
  { value: 'line', label: 'Line' },
  { value: 'area', label: 'Area' },
  { value: 'scatter', label: 'Scatter' },
  { value: 'pie', label: 'Pie' },
  { value: 'donut', label: 'Donut' },
  { value: 'kpi', label: 'KPI' },
] satisfies Array<{ value: DashboardChartType; label: string }>
const blockSpanOptions = [
  { value: 1 as const, label: 'Compact' },
  { value: 2 as const, label: 'Wide' },
  { value: 3 as const, label: 'Full' },
]

const paramTypeOptions: Array<DashboardBlockParam['type']> = ['text', 'number', 'date']
const blockCards = computed(() => (dashboardDetail.value?.blocks ?? []).slice().sort((a, b) => a.sort_order - b.sort_order || a.id - b.id))
const dateFilterPairs = computed(() => {
  const params = rendered.value?.params ?? []
  const names = new Set(params.map((param) => param.name))
  const pairs: Array<{ start: string; end: string; label: string }> = []
  if (names.has('start_date') && names.has('end_date')) {
    pairs.push({ start: 'start_date', end: 'end_date', label: 'Date Range' })
  }
  if (names.has('date_from') && names.has('date_to')) {
    pairs.push({ start: 'date_from', end: 'date_to', label: 'From / To' })
  }
  return pairs
})
const dayCountParamName = computed(() => {
  const params = rendered.value?.params ?? []
  const match = params.find((param) => ['last_days', 'days', 'lookback_days'].includes(param.name))
  return match?.name ?? ''
})
const resizePreview = ref<Record<number, { column_span: 1 | 2 | 3; row_span: 1 | 2 | 3 }>>({})
const resizeGhostRect = ref<{ left: number; top: number; width: number; height: number } | null>(null)
let resizeBlockSnapshot: DashboardBlock | RenderBlock | null = null
let resizeStartX = 0
let resizeStartY = 0
let resizeStartColumnSpan: 1 | 2 | 3 = 1
let resizeStartRowSpan: 1 | 2 | 3 = 2
let resizeInitialW = 0
let resizeInitialH = 0
let resizeOriginX = 0
let resizeOriginY = 0

async function loadDashboards() {
  if (isSharedView.value) return
  loading.value = true
  try {
    const { data } = await axios.get<Dashboard[]>('/api/analytics-dashboards')
    dashboards.value = data ?? []
    if (!selectedDashboardId.value && dashboards.value.length) {
      selectedDashboardId.value = dashboards.value[0].id
    }
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to load dashboards')
  } finally {
    loading.value = false
  }
}

async function loadDashboardUsers() {
  if (isSharedView.value) return
  loadingUsers.value = true
  try {
    const { data } = await axios.get<DashboardUserOption[]>('/api/analytics-dashboards/users')
    dashboardUsers.value = data ?? []
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to load users')
  } finally {
    loadingUsers.value = false
  }
}

async function loadDashboardDetail(id: number) {
  if (isSharedView.value) return
  try {
    const { data } = await axios.get<DashboardDetail>(`/api/analytics-dashboards/${id}`)
    dashboardDetail.value = data
    resetDashboardStateFromDetail(data)
  } catch (e: any) {
    dashboardDetail.value = null
    rendered.value = null
    globalFilter.value = ''
    dashboardParams.value = {}
    blockTextFilters.value = {}
    blockColumnFilters.value = {}
    selectedPresetName.value = ''
    presetName.value = ''
    toast.error(e?.response?.data?.error || 'Failed to load dashboard')
  }
}

async function renderDashboard(id: number) {
  rendering.value = true
  try {
    const queryParams = Object.fromEntries(
      Object.entries(dashboardParams.value)
        .filter(([, value]) => value != null)
        .map(([key, value]) => [`param_${key}`, value]),
    )
    const url = isSharedView.value
      ? `/api/analytics-dashboards/shared/${sharedToken.value}`
      : `/api/analytics-dashboards/${id}/render`
    const { data } = await axios.get<RenderedDashboard>(url, { params: queryParams })
    rendered.value = data
    const next: Record<string, string> = {}
    for (const param of data.params ?? []) {
      next[param.name] = dashboardParams.value[param.name] ?? param.value ?? ''
    }
    dashboardParams.value = next
  } catch (e: any) {
    rendered.value = null
    blockTextFilters.value = {}
    blockColumnFilters.value = {}
    toast.error(e?.response?.data?.error || 'Failed to render dashboard')
  } finally {
    rendering.value = false
  }
}

async function loadSharedDashboard() {
  if (!isSharedView.value) return
  await renderDashboard(0)
}

async function createDashboard() {
  if (!newDashboardName.value.trim()) return
  creating.value = true
  try {
    const { data } = await axios.post<Dashboard>('/api/analytics-dashboards', {
      name: newDashboardName.value.trim(),
      description: newDashboardDescription.value.trim(),
    })
    newDashboardName.value = ''
    newDashboardDescription.value = ''
    createDashboardOpen.value = false
    await loadDashboards()
    selectedDashboardId.value = data.id
    toast.success('Dashboard created')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to create dashboard')
  } finally {
    creating.value = false
  }
}

async function deleteDashboard(id: number) {
  if (!confirm('Delete this dashboard?')) return
  try {
    await axios.delete(`/api/analytics-dashboards/${id}`)
    if (selectedDashboardId.value === id) {
      selectedDashboardId.value = null
      dashboardDetail.value = null
      rendered.value = null
      globalFilter.value = ''
      dashboardParams.value = {}
      blockTextFilters.value = {}
      blockColumnFilters.value = {}
      selectedPresetName.value = ''
      presetName.value = ''
    }
    await loadDashboards()
    toast.success('Dashboard deleted')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to delete dashboard')
  }
}

async function updateDashboardDetail(patch: Partial<DashboardDetail>) {
  if (!dashboardDetail.value) return null
  const body = {
    name: patch.name ?? dashboardDetail.value.name,
    description: patch.description ?? dashboardDetail.value.description,
    visibility: patch.visibility ?? dashboardDetail.value.visibility,
    share_token: patch.share_token ?? dashboardDetail.value.share_token,
    presets: patch.presets ?? dashboardDetail.value.presets ?? [],
    access: patch.access ?? dashboardDetail.value.access ?? [],
    default_preset: patch.default_preset ?? dashboardDetail.value.default_preset,
  }
  const { data } = await axios.put<DashboardDetail>(`/api/analytics-dashboards/${dashboardDetail.value.id}`, body)
  dashboardDetail.value = { ...data, blocks: dashboardDetail.value.blocks }
  return data
}

function resetDashboardStateFromDetail(detail: DashboardDetail) {
  const defaultPreset = detail.presets?.find((item) => item.name === detail.default_preset) ?? null
  selectedPresetName.value = defaultPreset?.name ?? ''
  presetName.value = defaultPreset?.name ?? ''
  globalFilter.value = defaultPreset?.global_filter ?? ''
  dashboardParams.value = { ...(defaultPreset?.params ?? {}) }
  semanticFilters.value = {}
  blockTextFilters.value = {}
  blockColumnFilters.value = {}
  resizePreview.value = {}
}

function openCreateDashboard() {
  newDashboardName.value = ''
  newDashboardDescription.value = ''
  createDashboardOpen.value = true
}

async function addBlock() {
  if (!selectedDashboardId.value || !newBlockQueryId.value) return
  try {
    const selectedQuery = queries.value.find((item) => item.id === newBlockQueryId.value)
    const { data } = await axios.post<DashboardBlock>(`/api/analytics-dashboards/${selectedDashboardId.value}/blocks`, {
      saved_query_id: newBlockQueryId.value,
      chart_type: newBlockChartType.value,
      column_span: inferredColumnSpan(newBlockChartType.value),
      row_span: inferredRowSpan(newBlockChartType.value),
      params: selectedQuery ? inferredParamsFromSQL(selectedQuery.sql) : [],
    })
    newBlockQueryId.value = null
    newBlockChartType.value = 'table'
    await refreshSelectedDashboard()
    if (data?.id) {
      const renderedBlock = rendered.value?.blocks?.find((item) => item.id === data.id)
      const dashboardBlock = dashboardDetail.value?.blocks?.find((item) => item.id === data.id)
      if (renderedBlock && dashboardBlock) {
        await autoTuneRenderedBlock(renderedBlock, dashboardBlock, false)
      }
    }
    toast.success('Block added')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to add block')
  }
}

function resetBlockPreview() {
  blockPreview.value = null
  blockPreviewError.value = ''
  blockPreviewSignature.value = ''
}

function resetDraftBlockForm() {
  newBlockQueryId.value = null
  newBlockChartType.value = 'table'
  newBlockTitle.value = ''
  newBlockSQL.value = ''
  resetBlockPreview()
}

async function previewDraftBlock() {
  const connID = draftBlockConnId.value
  const sql = draftBlockSQL.value.trim()
  if (!connID || !sql) return
  blockPreviewing.value = true
  blockPreviewError.value = ''
  blockPreview.value = null
  try {
    const { data } = await axios.post<DashboardQueryPreview>('/api/analytics-dashboards/preview', {
      conn_id: connID,
      sql,
    })
    blockPreview.value = {
      columns: data.columns ?? [],
      rows: data.rows ?? [],
      row_count: data.row_count ?? 0,
      duration_ms: data.duration_ms ?? 0,
    }
    blockPreviewSignature.value = draftBlockSignature.value
  } catch (e: any) {
    blockPreviewError.value = e?.response?.data?.error || 'Failed to preview query'
  } finally {
    blockPreviewing.value = false
  }
}

function previewYKey() {
  const preview = blockPreview.value
  if (!preview?.columns.length) return ''
  const numericColumn = preview.columns.find((column, index) =>
    preview.rows.some((row) => row[index] !== null && row[index] !== '' && !Number.isNaN(Number(row[index]))),
  )
  return numericColumn ?? preview.columns[1] ?? preview.columns[0] ?? ''
}

async function saveDraftBlock() {
  if (!selectedDashboardId.value || !canSavePreviewedBlock.value) return
  savingDraftBlock.value = true
  try {
    let savedQueryID = selectedDraftSavedQuery.value?.id ?? null
    const sql = draftBlockSQL.value.trim()
    const title = draftBlockTitle.value.trim() || 'Dashboard Query'
    if (blockSourceMode.value === 'sql') {
      savedQueryID = await saveSavedQuery(title, sql, 'Created from dashboard builder', draftBlockConnId.value)
      await fetchSavedQueries()
    }
    if (!savedQueryID) {
      toast.error('Select a saved query or write SQL first')
      return
    }
    const columns = blockPreview.value?.columns ?? []
    const { data } = await axios.post<DashboardBlock>(`/api/analytics-dashboards/${selectedDashboardId.value}/blocks`, {
      saved_query_id: savedQueryID,
      title,
      chart_type: newBlockChartType.value,
      column_span: inferredColumnSpan(newBlockChartType.value),
      row_span: inferredRowSpan(newBlockChartType.value),
      x_key: columns[0] ?? '',
      y_key: previewYKey(),
      params: inferredParamsFromSQL(sql),
    })
    await refreshSelectedDashboard()
    if (data?.id) {
      const renderedBlock = rendered.value?.blocks?.find((item) => item.id === data.id)
      const dashboardBlock = dashboardDetail.value?.blocks?.find((item) => item.id === data.id)
      if (renderedBlock && dashboardBlock && newBlockChartType.value === 'table') {
        await autoTuneRenderedBlock(renderedBlock, dashboardBlock, false)
      }
    }
    resetDraftBlockForm()
    toast.success('Block saved')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to save block')
  } finally {
    savingDraftBlock.value = false
  }
}

function startEditBlock(block: DashboardBlock) {
  editingBlockId.value = block.id
  editingBlock.value = {
    ...block,
    params: syncBlockParamsForBlock(block),
  }
}

function cancelEditBlock() {
  editingBlockId.value = null
  editingBlock.value = null
}

async function saveBlock() {
  if (!editingBlock.value) return
  try {
    const payload = {
      ...editingBlock.value,
      params: editingBlock.value.params
        .map((param) => ({
          name: param.name.trim(),
          label: param.label.trim(),
          type: param.type,
          default_value: param.default_value,
        }))
        .filter((param) => param.name),
    }
    await axios.put(`/api/analytics-dashboards/blocks/${editingBlock.value.id}`, payload)
    await refreshSelectedDashboard()
    cancelEditBlock()
    toast.success('Block updated')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to update block')
  }
}

async function updateBlockQuick(block: DashboardBlock, patch: Partial<DashboardBlock>, successMessage = 'Block updated') {
  try {
    await axios.put(`/api/analytics-dashboards/blocks/${block.id}`, {
      ...block,
      ...patch,
    })
    await refreshSelectedDashboard()
    toast.success(successMessage)
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to update block')
  }
}

function startInlineRename(block: DashboardBlock | RenderBlock) {
  renamingBlockId.value = block.id
  renameDraft.value = block.title
}

function cancelInlineRename() {
  renamingBlockId.value = null
  renameDraft.value = ''
}

async function commitInlineRename(block: DashboardBlock | RenderBlock) {
  const title = renameDraft.value.trim()
  if (!title || title === block.title) {
    cancelInlineRename()
    return
  }
  await updateBlockQuick(block as DashboardBlock, { title }, 'Block renamed')
  cancelInlineRename()
}

async function deleteBlock(id: number) {
  if (!confirm('Delete this block?')) return
  try {
    await axios.delete(`/api/analytics-dashboards/blocks/${id}`)
    await refreshSelectedDashboard()
    toast.success('Block deleted')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to delete block')
  }
}

async function refreshSelectedDashboard() {
  if (!selectedDashboardId.value) return
  await loadDashboardDetail(selectedDashboardId.value)
  await renderDashboard(selectedDashboardId.value)
}

function inferredColumnSpan(chartType: DashboardBlock['chart_type']): 1 | 2 | 3 {
  switch (chartType) {
    case 'kpi':
      return 1
    case 'table':
      return 3
    case 'scatter':
    case 'pie':
    case 'donut':
      return 2
    default:
      return 2
  }
}

function inferredRowSpan(chartType: DashboardBlock['chart_type']): 1 | 2 | 3 {
  switch (chartType) {
    case 'kpi':
      return 1
    case 'scatter':
    case 'pie':
    case 'donut':
      return 2
    case 'table':
      return 2
    default:
      return 2
  }
}

function inferredAxisKeys(block: DashboardBlock) {
  const renderedBlock = rendered.value?.blocks?.find((item) => item.id === block.id)
  const columns = renderedBlock?.columns ?? []
  return {
    x_key: block.x_key || columns[0] || '',
    y_key: block.y_key || columns[1] || columns[0] || '',
  }
}

function isNumericColumn(values: any[]) {
  const filtered = values.filter((value) => value !== null && value !== '')
  if (!filtered.length) return false
  return filtered.every((value) => !Number.isNaN(Number(value)))
}

function inferChartTypeFromRendered(block: RenderBlock): DashboardBlock['chart_type'] {
  if (block.error) return 'table'
  if (block.row_count <= 1 && block.columns.length >= 1) {
    const numericColumn = block.columns.find((column, index) =>
      isNumericColumn(block.rows.map((row) => row[index])),
    )
    if (numericColumn) return 'kpi'
  }
  if (block.columns.length >= 2) {
    const firstValues = block.rows.map((row) => row[0])
    const secondValues = block.rows.map((row) => row[1])
    if (!isNumericColumn(firstValues) && isNumericColumn(secondValues)) {
      return block.row_count <= 10 ? 'bar' : 'line'
    }
  }
  return 'table'
}

function isDashboardChartType(chartType: DashboardChartType): chartType is RenderableDashboardChartType {
  return ['bar', 'horizontal-bar', 'line', 'area', 'scatter', 'pie', 'donut'].includes(chartType)
}

function inferColumnSpanFromRendered(block: RenderBlock, chartType: DashboardBlock['chart_type']): 1 | 2 | 3 {
  if (chartType === 'kpi') return 1
  if (chartType === 'table') return block.columns.length > 4 ? 3 : 2
  return block.row_count > 10 ? 3 : 2
}

function inferRowSpanFromRendered(block: RenderBlock, chartType: DashboardBlock['chart_type']): 1 | 2 | 3 {
  if (chartType === 'kpi') return 1
  if (chartType === 'table') return block.row_count > 6 ? 3 : 2
  return block.row_count > 10 ? 3 : 2
}

async function autoTuneRenderedBlock(renderedBlock: RenderBlock, dashboardBlock?: DashboardBlock, showToast = true) {
  const source = dashboardBlock ?? dashboardDetail.value?.blocks?.find((item) => item.id === renderedBlock.id)
  if (!source) return
  const chartType = inferChartTypeFromRendered(renderedBlock)
  const axis = inferredAxisKeys(source)
  await updateBlockQuick(source, {
    chart_type: chartType,
    column_span: inferColumnSpanFromRendered(renderedBlock, chartType),
    row_span: inferRowSpanFromRendered(renderedBlock, chartType),
    x_key: axis.x_key,
    y_key: axis.y_key,
  }, showToast ? 'Block auto-tuned' : 'Block updated')
}

async function applySmartDefaultsToBlock(block: DashboardBlock) {
  const nextSpan = block.column_span || inferredColumnSpan(block.chart_type)
  const axis = inferredAxisKeys(block)
  await updateBlockQuick(block, {
    column_span: nextSpan,
    x_key: axis.x_key,
    y_key: axis.y_key,
  }, 'Block settings updated')
}

function resetDragState() {
  draggingBlockId.value = null
  droppingBlockId.value = null
}

function handleDragHandleMouseDown(block: DashboardBlock | RenderBlock, event: MouseEvent) {
  if (!editMode.value || isSharedView.value) return
  event.preventDefault()
  event.stopPropagation()
  draggingBlockId.value = block.id
  dragGhostTitle.value = block.title
  dragGhostType.value = block.chart_type
  dragX.value = event.clientX
  dragY.value = event.clientY
  window.addEventListener('mousemove', onCardDragMove)
  window.addEventListener('mouseup', onCardDragEnd)
  document.body.style.cursor = 'grabbing'
  document.body.style.userSelect = 'none'
}

function onCardDragMove(event: MouseEvent) {
  dragX.value = event.clientX
  dragY.value = event.clientY
  const el = document.elementFromPoint(event.clientX, event.clientY)
  const cardEl = el?.closest('[data-block-id]') as HTMLElement | null
  const blockId = cardEl ? Number(cardEl.dataset.blockId) : 0
  droppingBlockId.value = (blockId && blockId !== draggingBlockId.value) ? blockId : null
}

async function onCardDragEnd() {
  window.removeEventListener('mousemove', onCardDragMove)
  window.removeEventListener('mouseup', onCardDragEnd)
  document.body.style.cursor = ''
  document.body.style.userSelect = ''
  const targetId = droppingBlockId.value
  if (targetId && draggingBlockId.value) {
    await handleBlockDrop(targetId)
  } else {
    resetDragState()
  }
}

async function handleBlockDrop(targetBlockId: number) {
  const sourceId = draggingBlockId.value
  if (!sourceId || sourceId === targetBlockId || !dashboardDetail.value) {
    resetDragState()
    return
  }
  const ordered = [...blockCards.value]
  const fromIndex = ordered.findIndex((item) => item.id === sourceId)
  const toIndex = ordered.findIndex((item) => item.id === targetBlockId)
  if (fromIndex < 0 || toIndex < 0) {
    resetDragState()
    return
  }
  const [moved] = ordered.splice(fromIndex, 1)
  ordered.splice(toIndex, 0, moved)
  dashboardDetail.value = {
    ...dashboardDetail.value,
    blocks: ordered.map((block, index) => ({ ...block, sort_order: index + 1 })),
  }
  try {
    await Promise.all(
      dashboardDetail.value.blocks.map((block, index) =>
        axios.put(`/api/analytics-dashboards/blocks/${block.id}`, {
          ...block,
          sort_order: index + 1,
        }),
      ),
    )
    await refreshSelectedDashboard()
    toast.success('Block order updated')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to reorder blocks')
  } finally {
    resetDragState()
  }
}

function applyPendingBlock() {
  const pending = pendingDashboardBlock.value
  if (!pending) return
  newBlockQueryId.value = pending.savedQueryId
  pendingDashboardBlock.value = null
  editMode.value = true
}

function cycleBlockWidth(block: DashboardBlock | RenderBlock) {
  const next = block.column_span === 3 ? 2 : block.column_span === 2 ? 1 : 3
  updateBlockQuick(block as DashboardBlock, { column_span: next }, 'Block width updated')
}

function currentBlockSpan(block: DashboardBlock | RenderBlock): 1 | 2 | 3 {
  if (isEmbedView.value && embedBlockId.value) return 3
  return resizePreview.value[block.id]?.column_span ?? block.column_span ?? 1
}

function currentBlockRowSpan(block: DashboardBlock | RenderBlock): 1 | 2 | 3 {
  if (isEmbedView.value && embedBlockId.value) return block.chart_type === 'kpi' ? 1 : Math.max(2, block.row_span ?? 2) as 1 | 2 | 3
  return resizePreview.value[block.id]?.row_span ?? block.row_span ?? inferredRowSpan(block.chart_type)
}

function beginBlockResize(block: DashboardBlock | RenderBlock, event: MouseEvent) {
  if (!editMode.value || isSharedView.value) return
  event.preventDefault()
  event.stopPropagation()
  resizingBlockId.value = block.id
  resizeBlockSnapshot = block
  resizeStartX = event.clientX
  resizeStartY = event.clientY
  resizeStartColumnSpan = currentBlockSpan(block)
  resizeStartRowSpan = currentBlockRowSpan(block)

  const cardEl = (event.currentTarget as HTMLElement)?.closest('[data-block-id]') as HTMLElement | null
  if (cardEl) {
    const rect = cardEl.getBoundingClientRect()
    resizeInitialW = rect.width
    resizeInitialH = rect.height
    resizeOriginX = rect.left
    resizeOriginY = rect.top
    resizeGhostRect.value = { left: rect.left, top: rect.top, width: rect.width, height: rect.height }
  }

  document.body.style.cursor = 'nwse-resize'
  document.body.style.userSelect = 'none'
  window.addEventListener('mousemove', handleBlockResizeMove)
  window.addEventListener('mouseup', finishBlockResize)
}

function handleBlockResizeMove(event: MouseEvent) {
  if (!resizingBlockId.value) return
  const dx = event.clientX - resizeStartX
  const dy = event.clientY - resizeStartY

  resizeGhostRect.value = {
    left: resizeOriginX,
    top: resizeOriginY,
    width: Math.max(80, resizeInitialW + dx),
    height: Math.max(60, resizeInitialH + dy),
  }

  const columnStep = Math.round(dx / 80)
  const rowStep = Math.round(dy / 70)
  const nextColumnSpan = Math.max(1, Math.min(3, resizeStartColumnSpan + columnStep)) as 1 | 2 | 3
  const nextRowSpan = Math.max(1, Math.min(3, resizeStartRowSpan + rowStep)) as 1 | 2 | 3
  resizePreview.value = {
    ...resizePreview.value,
    [resizingBlockId.value]: { column_span: nextColumnSpan, row_span: nextRowSpan },
  }
}

async function finishBlockResize() {
  const blockId = resizingBlockId.value
  const snapshot = resizeBlockSnapshot
  window.removeEventListener('mousemove', handleBlockResizeMove)
  window.removeEventListener('mouseup', finishBlockResize)
  document.body.style.cursor = ''
  document.body.style.userSelect = ''
  resizeGhostRect.value = null
  resizingBlockId.value = null
  resizeBlockSnapshot = null
  if (!blockId || !snapshot) return
  const next = resizePreview.value[blockId] ?? {
    column_span: snapshot.column_span,
    row_span: snapshot.row_span ?? inferredRowSpan(snapshot.chart_type),
  }
  resizePreview.value = {}
  if (next.column_span === snapshot.column_span && next.row_span === currentBlockRowSpan(snapshot)) return
  await updateBlockQuick(snapshot as DashboardBlock, next, 'Block size updated')
}

function queryName(savedQueryId: number) {
  return queries.value.find((item) => item.id === savedQueryId)?.name ?? `Query #${savedQueryId}`
}

function applyPresetByName(name: string) {
  const preset = dashboardDetail.value?.presets?.find((item) => item.name === name)
  if (!preset) return
  selectedPresetName.value = preset.name
  presetName.value = preset.name
  globalFilter.value = preset.global_filter ?? ''
  dashboardParams.value = { ...(preset.params ?? {}) }
  semanticFilters.value = {}
  blockTextFilters.value = {}
  blockColumnFilters.value = {}
  if (selectedDashboardId.value) {
    renderDashboard(selectedDashboardId.value)
  }
}

async function saveCurrentPreset() {
  if (!dashboardDetail.value || !presetName.value.trim()) return
  savingPreset.value = true
  try {
    const name = presetName.value.trim()
    const nextPresets = [...(dashboardDetail.value.presets ?? [])]
    const index = nextPresets.findIndex((item) => item.name === name)
    const preset: DashboardViewPreset = {
      name,
      global_filter: globalFilter.value.trim(),
      params: { ...dashboardParams.value },
    }
    if (index >= 0) {
      nextPresets[index] = preset
    } else {
      nextPresets.push(preset)
    }
    const data = await updateDashboardDetail({
      presets: nextPresets,
      default_preset: dashboardDetail.value.default_preset === name ? name : dashboardDetail.value.default_preset,
    })
    if (!data) return
    dashboardDetail.value = { ...data, blocks: dashboardDetail.value.blocks }
    selectedPresetName.value = name
    presetName.value = name
    await loadDashboards()
    toast.success(index >= 0 ? 'Preset updated' : 'Preset saved')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to save preset')
  } finally {
    savingPreset.value = false
  }
}

async function deletePreset(name: string) {
  if (!dashboardDetail.value || !name) return
  if (!confirm(`Delete preset "${name}"?`)) return
  savingPreset.value = true
  try {
    const nextPresets = (dashboardDetail.value.presets ?? []).filter((item) => item.name !== name)
    const nextDefault = dashboardDetail.value.default_preset === name ? '' : dashboardDetail.value.default_preset
    const data = await updateDashboardDetail({
      presets: nextPresets,
      default_preset: nextDefault,
    })
    if (!data) return
    dashboardDetail.value = { ...data, blocks: dashboardDetail.value.blocks }
    if (selectedPresetName.value === name) {
      selectedPresetName.value = ''
      presetName.value = ''
    }
    await loadDashboards()
    toast.success('Preset deleted')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to delete preset')
  } finally {
    savingPreset.value = false
  }
}

async function setDefaultPreset(name: string) {
  if (!dashboardDetail.value) return
  savingPreset.value = true
  try {
    const data = await updateDashboardDetail({
      default_preset: name,
    })
    if (!data) return
    dashboardDetail.value = { ...data, blocks: dashboardDetail.value.blocks }
    await loadDashboards()
    toast.success(name ? 'Default preset updated' : 'Default preset cleared')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to update default preset')
  } finally {
    savingPreset.value = false
  }
}

async function updateDashboardVisibility(visibility: Dashboard['visibility']) {
  if (!dashboardDetail.value) return
  savingAccess.value = true
  try {
    const data = await updateDashboardDetail({ visibility })
    if (!data) return
    dashboardDetail.value = { ...data, blocks: dashboardDetail.value.blocks }
    await loadDashboards()
    toast.success('Dashboard visibility updated')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to update dashboard visibility')
  } finally {
    savingAccess.value = false
  }
}

async function addDashboardAccessEntry() {
  if (!dashboardDetail.value || !accessUserId.value) return
  savingAccess.value = true
  try {
    const selectedUser = dashboardUsers.value.find((item) => item.id === accessUserId.value)
    const data = await updateDashboardDetail({
      access: [
        ...(dashboardDetail.value.access ?? []),
        {
          user_id: accessUserId.value,
          username: selectedUser?.username ?? '',
          access_level: accessLevel.value,
        },
      ],
    })
    if (!data) return
    dashboardDetail.value = { ...data, blocks: dashboardDetail.value.blocks }
    accessUserId.value = null
    accessLevel.value = 'viewer'
    toast.success('Dashboard access added')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to add dashboard access')
  } finally {
    savingAccess.value = false
  }
}

async function removeDashboardAccessEntry(userId: number) {
  if (!dashboardDetail.value) return
  savingAccess.value = true
  try {
    const data = await updateDashboardDetail({
      access: (dashboardDetail.value.access ?? []).filter((entry) => entry.user_id !== userId),
    })
    if (!data) return
    dashboardDetail.value = { ...data, blocks: dashboardDetail.value.blocks }
    toast.success('Dashboard access removed')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to remove dashboard access')
  } finally {
    savingAccess.value = false
  }
}

async function changeDashboardAccessLevel(userId: number, level: DashboardAccessEntry['access_level']) {
  if (!dashboardDetail.value) return
  savingAccess.value = true
  try {
    const data = await updateDashboardDetail({
      access: (dashboardDetail.value.access ?? []).map((entry) =>
        entry.user_id === userId ? { ...entry, access_level: level } : entry,
      ),
    })
    if (!data) return
    dashboardDetail.value = { ...data, blocks: dashboardDetail.value.blocks }
    toast.success('Dashboard access updated')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to update dashboard access')
  } finally {
    savingAccess.value = false
  }
}

function handleVisibilityChange(event: Event) {
  const value = (event.target as HTMLSelectElement | null)?.value as Dashboard['visibility'] | undefined
  if (!value) return
  updateDashboardVisibility(value)
}

function handleDashboardAccessLevelChange(userId: number, event: Event) {
  const value = (event.target as HTMLSelectElement | null)?.value as DashboardAccessEntry['access_level'] | undefined
  if (!value) return
  changeDashboardAccessLevel(userId, value)
}

async function regenerateShareLink() {
  if (!dashboardDetail.value) return
  savingAccess.value = true
  try {
    const data = await updateDashboardDetail({
      visibility: 'public',
      share_token: '',
    })
    if (!data) return
    dashboardDetail.value = { ...data, blocks: dashboardDetail.value.blocks }
    await loadDashboards()
    toast.success('Share link refreshed')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to refresh share link')
  } finally {
    savingAccess.value = false
  }
}

async function copyShareLink() {
  if (!shareURL.value) return
  try {
    await navigator.clipboard.writeText(shareURL.value)
    toast.success('Share link copied')
  } catch {
    toast.error('Failed to copy share link')
  }
}

function blockEmbedURL(block: RenderBlock | DashboardBlock) {
  const token = dashboardDetail.value?.share_token?.trim()
  if (!token || typeof window === 'undefined') return ''
  return `${window.location.origin}/embed/dashboards/${token}/blocks/${block.id}`
}

function blockEmbedCode(block: RenderBlock | DashboardBlock) {
  const url = blockEmbedURL(block)
  if (!url) return ''
  return `<iframe src="${url}" width="100%" height="420" style="border:0;border-radius:8px;overflow:hidden" loading="lazy" referrerpolicy="no-referrer-when-downgrade"></iframe>`
}

async function copyDashboardEmbedCode() {
  if (!dashboardEmbedCode.value) return
  try {
    await navigator.clipboard.writeText(dashboardEmbedCode.value)
    toast.success('Dashboard embed code copied')
  } catch {
    toast.error('Failed to copy embed code')
  }
}

async function copyBlockEmbedCode(block: RenderBlock | DashboardBlock) {
  const code = blockEmbedCode(block)
  if (!code) return
  try {
    await navigator.clipboard.writeText(code)
    toast.success('Chart embed code copied')
  } catch {
    toast.error('Failed to copy embed code')
  }
}

function querySQL(savedQueryId: number) {
  return queries.value.find((item) => item.id === savedQueryId)?.sql ?? ''
}

function blockFilterOptions(block: RenderBlock) {
  const source = rendered.value?.blocks.find((item) => item.id === block.id) ?? block
  return source.columns
    .map((column, index) => {
      const optionSet = new Set<string>()
      for (const row of source.rows) {
        const value = normalizeSemanticOptionValue(row[index])
        if (value) optionSet.add(value)
        if (optionSet.size > 30) break
      }
      const options = [...optionSet].sort((a, b) => a.localeCompare(b))
      if (options.length <= 1 || options.length > 30) return null
      return {
        column,
        label: prettifySemanticLabel(column),
        options,
      }
    })
    .filter((item): item is { column: string; label: string; options: string[] } => item !== null)
}

function filteredRowsForBlock(block: RenderBlock) {
  if (block.error) return block.rows
  const q = (blockTextFilters.value[block.id] ?? '').trim().toLowerCase()
  const columnFilters = Object.entries(blockColumnFilters.value[block.id] ?? {})
    .filter(([, value]) => String(value ?? '').trim() !== '')
  if (!q && !columnFilters.length) return block.rows
  return block.rows.filter((row) => {
    const textMatch = !q || row.some((value) => String(value ?? '').toLowerCase().includes(q))
    if (!textMatch) return false
    return columnFilters.every(([column, expected]) => {
      const index = block.columns.indexOf(column)
      if (index < 0) return true
      return normalizeSemanticOptionValue(row[index]) === expected
    })
  })
}

function setBlockColumnFilter(blockId: number, column: string, value: string) {
  const current = { ...(blockColumnFilters.value[blockId] ?? {}) }
  if (value) {
    current[column] = value
  } else {
    delete current[column]
  }
  blockColumnFilters.value = {
    ...blockColumnFilters.value,
    [blockId]: current,
  }
}

function handleBlockColumnFilter(blockId: number, column: string, event: Event) {
  setBlockColumnFilter(blockId, column, (event.target as HTMLSelectElement)?.value ?? '')
}

function blockHasFilters(block: RenderBlock) {
  return !!(blockTextFilters.value[block.id] ?? '').trim() ||
    Object.values(blockColumnFilters.value[block.id] ?? {}).some((value) => String(value ?? '').trim() !== '')
}

function clearBlockFilters(blockId: number) {
  const nextText = { ...blockTextFilters.value }
  delete nextText[blockId]
  const nextColumns = { ...blockColumnFilters.value }
  delete nextColumns[blockId]
  blockTextFilters.value = nextText
  blockColumnFilters.value = nextColumns
}

const _midnightRe = /^(\d{4}-\d{2}-\d{2})[T ]00:00:00(\.\d+)?(Z|[+-]\d{2}:?\d{2})?$/
const _hasTimestampRe = /^(\d{4}-\d{2}-\d{2})[T ](\d{2}:\d{2}):\d{2}(\.\d+)?(Z|[+-]\d{2}:?\d{2})?$/

function formatCellValue(value: unknown): string {
  if (value === null || value === undefined) return ''
  const s = String(value).trim()
  const midnight = _midnightRe.exec(s)
  if (midnight) return midnight[1]
  const ts = _hasTimestampRe.exec(s)
  if (ts) return `${ts[1]} ${ts[2]}`
  return s
}

const exportOptions = [
  { value: 'pdf', label: 'PDF', detail: 'Printable layout' },
  { value: 'image', label: 'PNG', detail: 'Visual snapshot' },
  { value: 'excel', label: 'Excel', detail: 'Formatted workbook' },
  { value: 'csv', label: 'CSV', detail: 'Flat data' },
  { value: 'sql', label: 'SQL', detail: 'Source queries' },
  { value: 'json', label: 'JSON', detail: 'Structured data' },
] as const

type ExportFormat = typeof exportOptions[number]['value']

function htmlEscape(value: unknown) {
  return String(value ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

function dashboardExportName() {
  return sanitizeFileName(rendered.value?.name || selectedDashboard.value?.name || 'dashboard')
}

function blockExportName(block: RenderBlock) {
  return sanitizeFileName(`${dashboardExportName()}-${block.title || block.query_name || `block-${block.id}`}`)
}

function blockSQL(block: RenderBlock) {
  return String(block.sql || querySQL(block.saved_query_id) || '').trim()
}

function exportFormatLabel(format: ExportFormat) {
  return exportOptions.find((option) => option.value === format)?.label ?? format.toUpperCase()
}

function csvFromBlocks(blocks: RenderBlock[]) {
  const lines: string[] = []
  const escape = (value: unknown) => {
    const text = formatCellValue(value)
    return /[",\n]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text
  }
  const push = (row: unknown[] = []) => lines.push(row.map(escape).join(','))
  const dashboardName = rendered.value?.name ?? selectedDashboard.value?.name ?? 'Dashboard'
  const totalRows = blocks.reduce((sum, block) => sum + (block.error ? 0 : block.row_count), 0)
  const durations = blocks.map((block) => Number(block.duration_ms)).filter((value) => Number.isFinite(value))
  const activeParams = Object.keys(dashboardParams.value).length ? JSON.stringify(dashboardParams.value) : 'None'

  push(['Analytics Dashboard Export'])
  push(['Dashboard', dashboardName])
  push(['Exported At', new Date().toLocaleString()])
  push(['Active Params', activeParams])
  push()
  push(['Summary'])
  push(['Metric', 'Value'])
  push(['Block Count', blocks.length])
  push(['Total Rows', totalRows])
  push(['Chart Blocks', blocks.filter((block) => block.chart_type !== 'table').length])
  push(['Table Blocks', blocks.filter((block) => block.chart_type === 'table').length])
  push(['Error Blocks', blocks.filter((block) => block.error).length])
  push(['Fastest Query Ms', durations.length ? Math.min(...durations) : 0])
  push(['Slowest Query Ms', durations.length ? Math.max(...durations) : 0])
  lines.push('')
  push(['Views'])
  push(['Block ID', 'Title', 'Chart Type', 'Rows', 'Duration Ms', 'Query Name', 'X Key', 'Y Key', 'Connection ID'])
  for (const block of blocks) {
    push([
      block.id,
      block.title || block.query_name || `Block ${block.id}`,
      block.chart_type,
      block.row_count,
      block.duration_ms,
      block.query_name,
      block.x_key || defaultXKey(block),
      block.y_key || defaultYKey(block),
      block.connection_id,
    ])
  }
  lines.push('')

  for (const [index, block] of blocks.entries()) {
    push([`View ${index + 1}`, block.title || block.query_name || `Block ${block.id}`])
    push(['Chart Type', block.chart_type, 'Rows', block.row_count, 'Duration Ms', block.duration_ms, 'Connection ID', block.connection_id])
    push(['X Key', block.x_key || defaultXKey(block), 'Y Key', block.y_key || defaultYKey(block), 'Query Name', block.query_name])
    push(['SQL', blockSQL(block)])
    if (block.error) {
      push(['Error', block.error])
      push()
      continue
    }
    push()
    push(['Data'])
    push(block.columns)
    for (const row of block.rows) {
      push(row as unknown[])
    }
    push()
  }
  return lines.join('\n')
}

const xlCell = (value: unknown, styleId: number) => ({ value, styleId })

function excelChartStyle(type: DashboardChartType) {
  if (type === 'table' || type === 'kpi') return XLSX_STYLES.badgeTeal
  if (type === 'scatter') return XLSX_STYLES.badgeAmber
  if (type === 'pie' || type === 'donut') return XLSX_STYLES.badgePurple
  return XLSX_STYLES.badgeBlue
}

function excelDurationStyle(value: unknown) {
  const ms = Number(value)
  return Number.isFinite(ms) && ms <= 30 ? XLSX_STYLES.metricTeal : XLSX_STYLES.metricAmber
}

function excelDataCell(column: string, value: unknown) {
  const key = column.toLowerCase()
  const numeric = typeof value === 'number' ? value : Number(value)
  if (Number.isFinite(numeric)) {
    if (key.includes('revoked') && numeric > 0) return xlCell(value, XLSX_STYLES.metricRed)
    if (key.includes('expired') && numeric > 0) return xlCell(value, XLSX_STYLES.metricAmber)
    if (key.includes('unique') || key.includes('active') || key.includes('success')) return xlCell(value, XLSX_STYLES.metricTeal)
    if (key.includes('total') || key.includes('count') || key.includes('session') || key.includes('login')) return xlCell(value, XLSX_STYLES.metricBlue)
  }
  return value
}

function excelSheetsFromBlocks(blocks: RenderBlock[], title: string): XLSXSheet[] {
  const exportedAt = new Date().toLocaleString()
  const totalRows = blocks.reduce((sum, block) => sum + (block.error ? 0 : block.row_count), 0)
  const durations = blocks.map((block) => Number(block.duration_ms)).filter((value) => Number.isFinite(value))
  const fastest = durations.length ? Math.min(...durations) : 0
  const slowest = durations.length ? Math.max(...durations) : 0
  const chartBlocks = blocks.filter((block) => block.chart_type !== 'table').length
  const tableBlocks = blocks.filter((block) => block.chart_type === 'table').length
  const errorBlocks = blocks.filter((block) => block.error).length
  const activeParams = Object.keys(dashboardParams.value).length ? JSON.stringify(dashboardParams.value) : 'None'
  const summaryRows: Array<Array<unknown>> = [
    [title, '', '', '', '', ''],
    ['Analytics Dashboard Export', '', '', '', '', ''],
    [xlCell('Exported At', XLSX_STYLES.label), exportedAt, xlCell('Block Count', XLSX_STYLES.label), xlCell(blocks.length, XLSX_STYLES.metricTeal), xlCell('Active Params', XLSX_STYLES.label), activeParams],
    [],
    [xlCell('Total Rows', XLSX_STYLES.label), xlCell(totalRows, XLSX_STYLES.metricBlue), xlCell('Fastest Query', XLSX_STYLES.label), xlCell(`${fastest} ms`, XLSX_STYLES.metricTeal), xlCell('Slowest Query', XLSX_STYLES.label), xlCell(`${slowest} ms`, XLSX_STYLES.metricAmber)],
    [xlCell('Chart Blocks', XLSX_STYLES.label), xlCell(chartBlocks, XLSX_STYLES.badgeBlue), xlCell('Table Blocks', XLSX_STYLES.label), xlCell(tableBlocks, XLSX_STYLES.badgeTeal), xlCell('Error Blocks', XLSX_STYLES.label), xlCell(errorBlocks, errorBlocks ? XLSX_STYLES.metricRed : XLSX_STYLES.metricTeal)],
    [],
    ['Block ID', 'Title', 'Chart Type', 'Rows', 'Duration Ms', 'Query Name'],
    ...blocks.map((block) => [
      block.id,
      block.title,
      xlCell(block.chart_type, excelChartStyle(block.chart_type)),
      xlCell(block.row_count, XLSX_STYLES.metricBlue),
      xlCell(block.duration_ms, excelDurationStyle(block.duration_ms)),
      block.query_name,
    ]),
  ]
  const sheets: XLSXSheet[] = [
    {
      name: 'Summary',
      rows: summaryRows,
      titleRows: [1],
      mutedRows: [2],
      headerRows: [8],
      freezeRow: 8,
      autoFilterRow: 8,
      columnWidths: [12, 34, 16, 14, 14, 34],
    },
  ]
  for (const [index, block] of blocks.entries()) {
    const rows: Array<Array<unknown>> = [
      [block.title || block.query_name || `Block ${block.id}`, '', '', '', '', '', '', ''],
      [
        xlCell('Chart Type', XLSX_STYLES.label),
        xlCell(block.chart_type, excelChartStyle(block.chart_type)),
        xlCell('Rows', XLSX_STYLES.label),
        xlCell(block.row_count, XLSX_STYLES.metricBlue),
        xlCell('Duration Ms', XLSX_STYLES.label),
        xlCell(block.duration_ms, excelDurationStyle(block.duration_ms)),
        xlCell('Connection', XLSX_STYLES.label),
        block.connection_id,
      ],
      [
        xlCell('X Key', XLSX_STYLES.label),
        block.x_key || defaultXKey(block),
        xlCell('Y Key', XLSX_STYLES.label),
        block.y_key || defaultYKey(block),
        xlCell('Query', XLSX_STYLES.label),
        block.query_name,
      ],
      [xlCell('SQL', XLSX_STYLES.label), blockSQL(block)],
      [],
    ]
    let headerRow = 0
    let errorRows: number[] = []
    if (block.error) {
      rows.push(['Error', block.error])
      errorRows = [6]
    } else {
      headerRow = rows.length + 1
      rows.push(block.columns)
      rows.push(...block.rows.map((row) => row.map((value, colIndex) => excelDataCell(block.columns[colIndex] || '', value))))
    }
    const dataColumnWidths = block.columns.map((column, colIndex) => {
      const max = Math.max(
        String(column).length,
        ...block.rows.slice(0, 60).map((row) => String(row[colIndex] ?? '').length),
      )
      return Math.min(42, Math.max(14, max + 2))
    })
    const columnWidths = Array.from(
      { length: Math.max(8, dataColumnWidths.length) },
      (_, colIndex) => {
        const dataWidth = dataColumnWidths[colIndex] ?? 16
        if (colIndex === 1) return Math.max(dataWidth, 30)
        return dataWidth
      },
    )
    sheets.push({
      name: block.title || block.query_name || `Block ${index + 1}`,
      rows,
      titleRows: [1],
      mutedRows: [2, 3, 4],
      errorRows,
      headerRows: headerRow ? [headerRow] : [],
      freezeRow: headerRow || undefined,
      autoFilterRow: headerRow || undefined,
      columnWidths,
    })
  }
  return sheets
}

function dashboardJSONPayload(blocks: RenderBlock[]) {
  return {
    schema_version: 1,
    export_type: 'analytics_dashboard',
    dashboard: {
      id: rendered.value?.id ?? dashboardDetail.value?.id ?? null,
      name: rendered.value?.name ?? dashboardDetail.value?.name ?? '',
      description: rendered.value?.description ?? dashboardDetail.value?.description ?? '',
      visibility: dashboardDetail.value?.visibility ?? '',
      default_preset: dashboardDetail.value?.default_preset ?? '',
      presets: dashboardDetail.value?.presets ?? [],
      exported_at: new Date().toISOString(),
      filters: {
        params: dashboardParams.value,
        blocks: {
          text: blockTextFilters.value,
          columns: blockColumnFilters.value,
        },
      },
    },
    blocks: blocks.map((block) => ({
      id: block.id,
      dashboard_id: block.dashboard_id,
      saved_query_id: block.saved_query_id,
      title: block.title,
      chart_type: block.chart_type,
      x_key: block.x_key,
      y_key: block.y_key,
      column_span: block.column_span,
      row_span: block.row_span,
      sort_order: block.sort_order,
      params: block.params ?? [],
      connection_id: block.connection_id,
      query_name: block.query_name,
      description: block.description,
      sql: blockSQL(block),
      columns: block.columns,
      rows: block.rows.map((row) => {
        const item: Record<string, unknown> = {}
        row.forEach((value, index) => {
          item[block.columns[index] ?? `column_${index + 1}`] = value
        })
        return item
      }),
      row_count: block.row_count,
      duration_ms: block.duration_ms,
      error: block.error,
    })),
  }
}

function dashboardSQL(blocks: RenderBlock[]) {
  return blocks
    .map((block) => {
      const sql = blockSQL(block)
      if (!sql) return ''
      const title = (block.title || block.query_name || `Block ${block.id}`).replace(/\*\//g, '')
      return `/* ${title} */\n${sql.replace(/;\s*$/, '')};`
    })
    .filter(Boolean)
    .join('\n\n')
}

function drawRoundRect(ctx: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, r: number) {
  const radius = Math.min(r, w / 2, h / 2)
  ctx.beginPath()
  ctx.moveTo(x + radius, y)
  ctx.arcTo(x + w, y, x + w, y + h, radius)
  ctx.arcTo(x + w, y + h, x, y + h, radius)
  ctx.arcTo(x, y + h, x, y, radius)
  ctx.arcTo(x, y, x + w, y, radius)
  ctx.closePath()
}

function drawWrappedText(ctx: CanvasRenderingContext2D, text: string, x: number, y: number, maxWidth: number, lineHeight: number, maxLines = 2) {
  const words = String(text || '').split(/\s+/).filter(Boolean)
  const lines: string[] = []
  let line = ''
  for (const word of words) {
    const next = line ? `${line} ${word}` : word
    if (ctx.measureText(next).width <= maxWidth || !line) {
      line = next
      continue
    }
    lines.push(line)
    line = word
    if (lines.length >= maxLines) break
  }
  if (line && lines.length < maxLines) lines.push(line)
  lines.forEach((item, index) => {
    const value = index === maxLines - 1 && words.length > 0 && lines.length === maxLines && words.join(' ') !== lines.join(' ')
      ? item.replace(/\s+\S*$/, '') + '...'
      : item
    ctx.fillText(value, x, y + index * lineHeight)
  })
  return lines.length * lineHeight
}

function compactNumber(value: number) {
  const abs = Math.abs(value)
  if (abs >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(1)}B`
  if (abs >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
  if (abs >= 1_000) return `${(value / 1_000).toFixed(1)}K`
  return Number.isInteger(value) ? String(value) : value.toFixed(2)
}

function drawTruncatedText(ctx: CanvasRenderingContext2D, text: unknown, x: number, y: number, maxWidth: number) {
  const value = String(text ?? '')
  if (ctx.measureText(value).width <= maxWidth) {
    ctx.fillText(value, x, y)
    return
  }
  let out = value
  while (out.length > 1 && ctx.measureText(`${out}...`).width > maxWidth) {
    out = out.slice(0, -1)
  }
  ctx.fillText(`${out}...`, x, y)
}

function drawTablePreview(ctx: CanvasRenderingContext2D, block: RenderBlock, x: number, y: number, w: number, h: number) {
  const cols = block.columns.slice(0, Math.min(5, block.columns.length))
  const rows = block.rows.slice(0, Math.max(1, Math.floor((h - 30) / 28)))
  const colW = w / Math.max(cols.length, 1)
  ctx.fillStyle = '#eef2f7'
  ctx.fillRect(x, y, w, 30)
  ctx.strokeStyle = '#d8dee9'
  ctx.strokeRect(x, y, w, Math.min(h, 30 + rows.length * 28))
  ctx.font = '700 12px Arial'
  ctx.fillStyle = '#111827'
  cols.forEach((col, index) => {
    drawTruncatedText(ctx, col, x + index * colW + 8, y + 20, colW - 14)
  })
  ctx.font = '12px Arial'
  rows.forEach((row, rowIndex) => {
    const rowY = y + 30 + rowIndex * 28
    ctx.fillStyle = rowIndex % 2 ? '#f8fafc' : '#ffffff'
    ctx.fillRect(x, rowY, w, 28)
    ctx.fillStyle = '#334155'
    cols.forEach((_, colIndex) => {
      drawTruncatedText(ctx, formatCellValue(row[colIndex]), x + colIndex * colW + 8, rowY + 18, colW - 14)
    })
  })
}

function inlineExportSVGStyles(source: SVGSVGElement, width: number, height: number) {
  const clone = source.cloneNode(true) as SVGSVGElement
  const sourceNodes = [source, ...Array.from(source.querySelectorAll('*'))] as SVGElement[]
  const cloneNodes = [clone, ...Array.from(clone.querySelectorAll('*'))] as SVGElement[]
  clone.setAttribute('xmlns', 'http://www.w3.org/2000/svg')
  clone.setAttribute('width', String(width))
  clone.setAttribute('height', String(height))
  clone.style.width = `${width}px`
  clone.style.height = `${height}px`
  sourceNodes.forEach((node, index) => {
    const target = cloneNodes[index]
    if (!target) return
    const computed = window.getComputedStyle(node)
    const tag = target.tagName.toLowerCase()
    if (tag === 'text') {
      target.style.fill = computed.fill
      target.style.fontFamily = computed.fontFamily || 'Arial, sans-serif'
      target.style.fontSize = computed.fontSize || '12px'
      target.style.fontWeight = computed.fontWeight
      target.style.letterSpacing = '0'
    }
    if (['path', 'circle', 'rect', 'polygon', 'text'].includes(tag)) {
      const fill = node.getAttribute('fill')
      if (fill !== 'none') target.style.fill = computed.fill
      target.style.opacity = computed.opacity
    }
    if (['line', 'path', 'circle', 'rect', 'polyline', 'polygon'].includes(tag)) {
      const stroke = node.getAttribute('stroke')
      if (stroke && stroke !== 'none') target.style.stroke = computed.stroke
      target.style.strokeWidth = computed.strokeWidth
    }
  })
  return clone
}

async function drawRenderedChartSVG(ctx: CanvasRenderingContext2D, block: RenderBlock, x: number, y: number, w: number, h: number) {
  const element = blockElement(block)
  const source = element?.querySelector('.rc-svg') as SVGSVGElement | null
  if (!source) return false
  const clone = inlineExportSVGStyles(source, Math.max(1, Math.round(w)), Math.max(1, Math.round(h)))
  const serialized = new XMLSerializer().serializeToString(clone)
  const blob = new Blob([serialized], { type: 'image/svg+xml;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  try {
    const image = new Image()
    image.decoding = 'async'
    image.src = url
    await image.decode()
    ctx.drawImage(image, x, y, w, h)
    return true
  } catch {
    return false
  } finally {
    URL.revokeObjectURL(url)
  }
}

function renderedChartSVGMarkup(block: RenderBlock) {
  const element = blockElement(block)
  const source = element?.querySelector('.rc-svg') as SVGSVGElement | null
  if (!source) return ''
  const clone = inlineExportSVGStyles(source, 1040, 320)
  clone.setAttribute('preserveAspectRatio', 'xMidYMid meet')
  return new XMLSerializer().serializeToString(clone)
}

function pdfTableMarkup(block: RenderBlock, limit = 24) {
  const cols = block.columns.slice(0, 8)
  const head = cols.map((column) => `<th>${htmlEscape(column)}</th>`).join('')
  const body = block.rows
    .slice(0, limit)
    .map((row) => `<tr>${cols.map((_, index) => `<td>${htmlEscape(formatCellValue(row[index]))}</td>`).join('')}</tr>`)
    .join('')
  const more = block.rows.length > limit
    ? `<p class="pdf-note">Showing ${limit} of ${block.rows.length} rows.</p>`
    : ''
  return `<table class="pdf-table"><thead><tr>${head}</tr></thead><tbody>${body}</tbody></table>${more}`
}

function pdfBlockMarkup(block: RenderBlock) {
  const title = htmlEscape(block.title || block.query_name || `Block ${block.id}`)
  const meta = htmlEscape(`${block.chart_type} · ${block.row_count} rows · ${block.duration_ms} ms`)
  let body = ''
  if (block.error) {
    body = `<div class="pdf-error">${htmlEscape(block.error)}</div>`
  } else if (block.chart_type === 'kpi') {
    body = `<div class="pdf-kpi">
      <div class="pdf-kpi__value">${htmlEscape(numericSeries(block)[0]?.value ?? block.row_count)}</div>
      <div class="pdf-kpi__label">${htmlEscape(defaultYKey(block) || 'value')}</div>
    </div>`
  } else if (isDashboardChartType(block.chart_type)) {
    const svg = renderedChartSVGMarkup(block)
    body = svg
      ? `<div class="pdf-chart">${svg}</div>`
      : `<div class="pdf-chart-fallback">${pdfTableMarkup(block, 12)}</div>`
  } else {
    body = pdfTableMarkup(block)
  }
  return `<section class="pdf-block">
    <header class="pdf-block__head">
      <h2>${title}</h2>
      <div>${meta}</div>
    </header>
    ${body}
  </section>`
}

function printDashboardPDF(blocks: RenderBlock[], title: string) {
  const win = window.open('', '_blank', 'width=1200,height=900')
  if (!win) throw new Error('popup blocked')
  const blockMarkup = blocks.map((block) => pdfBlockMarkup(block)).join('')
  win.document.write(`<!doctype html>
<html>
<head>
<meta charset="utf-8" />
<title>${htmlEscape(title)}</title>
<style>
@page { size: A4 landscape; margin: 12mm; }
* { box-sizing: border-box; }
body {
  margin: 0;
  background: #f8fafc;
  color: #0f172a;
  font-family: Arial, sans-serif;
  -webkit-print-color-adjust: exact;
  print-color-adjust: exact;
}
.pdf-report { padding: 18px; }
.pdf-report__head {
  display: flex;
  justify-content: space-between;
  gap: 24px;
  align-items: flex-end;
  margin-bottom: 16px;
}
.pdf-report__head h1 { margin: 0; font-size: 26px; line-height: 1.15; }
.pdf-report__meta { color: #64748b; font-size: 12px; text-align: right; }
.pdf-block {
  break-inside: avoid;
  page-break-inside: avoid;
  margin: 0 0 14px;
  padding: 16px;
  border: 1px solid #d8dee9;
  border-radius: 10px;
  background: #ffffff;
}
.pdf-block__head {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: flex-start;
  margin-bottom: 12px;
}
.pdf-block__head h2 { margin: 0; font-size: 17px; line-height: 1.2; }
.pdf-block__head div { color: #64748b; font-size: 12px; white-space: nowrap; }
.pdf-chart { width: 100%; height: 320px; overflow: hidden; }
.pdf-chart svg { width: 100%; height: 100%; display: block; }
.pdf-kpi { min-height: 210px; display: flex; flex-direction: column; justify-content: center; align-items: center; }
.pdf-kpi__value { font-size: 54px; line-height: 1; font-weight: 800; }
.pdf-kpi__label { margin-top: 8px; color: #64748b; font-size: 14px; }
.pdf-table { width: 100%; border-collapse: collapse; font-size: 11px; }
.pdf-table th { background: #eef2f7; color: #111827; font-weight: 700; }
.pdf-table th, .pdf-table td { border: 1px solid #d8dee9; padding: 6px 8px; text-align: left; vertical-align: top; }
.pdf-table tbody tr:nth-child(even) { background: #f8fafc; }
.pdf-note { margin: 8px 0 0; color: #64748b; font-size: 11px; }
.pdf-error { color: #b91c1c; background: #fee2e2; border-radius: 8px; padding: 12px; font-family: monospace; }
</style>
</head>
<body>
<main class="pdf-report">
  <header class="pdf-report__head">
    <h1>${htmlEscape(title)}</h1>
    <div class="pdf-report__meta">Exported ${htmlEscape(new Date().toLocaleString())}<br />${blocks.length} view${blocks.length === 1 ? '' : 's'}</div>
  </header>
  ${blockMarkup}
</main>
</body>
</html>`)
  win.document.close()
  win.focus()
  setTimeout(() => win.print(), 300)
}

function drawChartPreview(ctx: CanvasRenderingContext2D, block: RenderBlock, x: number, y: number, w: number, h: number) {
  const series = numericSeries(block).slice(0, 18)
  if (!series.length) {
    drawTablePreview(ctx, block, x, y, w, h)
    return
  }
  const colors = ['#2563eb', '#059669', '#ea580c', '#7c3aed', '#0891b2', '#dc2626']
  const max = Math.max(1, ...series.map((item) => Math.max(0, item.value)))
  ctx.fillStyle = '#334155'
  ctx.font = '12px Arial'
  drawTruncatedText(ctx, `${defaultXKey(block) || 'label'} / ${defaultYKey(block) || 'value'}`, x, y + 10, w)
  const plotX = x + 58
  const plotY = y + 24
  const plotW = w - 72
  const plotH = h - 64
  ctx.strokeStyle = '#d8dee9'
  ctx.lineWidth = 1
  ctx.font = '11px Arial'
  ctx.fillStyle = '#64748b'
  for (let i = 0; i <= 3; i++) {
    const value = (max / 3) * i
    const gy = plotY + plotH - (value / max) * plotH
    ctx.strokeStyle = i === 0 ? '#94a3b8' : '#e2e8f0'
    ctx.beginPath()
    ctx.moveTo(plotX, gy)
    ctx.lineTo(plotX + plotW, gy)
    ctx.stroke()
    ctx.fillStyle = '#64748b'
    ctx.fillText(compactNumber(value), x, gy + 4)
  }
  ctx.beginPath()
  ctx.moveTo(plotX, plotY)
  ctx.lineTo(plotX, plotY + plotH)
  ctx.lineTo(plotX + plotW, plotY + plotH)
  ctx.stroke()
  if (block.chart_type === 'line' || block.chart_type === 'area' || block.chart_type === 'scatter') {
    ctx.strokeStyle = '#2563eb'
    ctx.fillStyle = 'rgba(37, 99, 235, 0.12)'
    ctx.lineWidth = 3
    ctx.beginPath()
    series.forEach((item, index) => {
      const px = plotX + (series.length === 1 ? plotW / 2 : (index * plotW) / (series.length - 1))
      const py = plotY + plotH - (Math.max(0, item.value) / max) * plotH
      if (index === 0) ctx.moveTo(px, py)
      else ctx.lineTo(px, py)
    })
    ctx.stroke()
    if (block.chart_type === 'area') {
      ctx.lineTo(plotX + plotW, plotY + plotH)
      ctx.lineTo(plotX, plotY + plotH)
      ctx.fill()
    }
    const labelEvery = Math.max(1, Math.ceil(series.length / 6))
    series.forEach((item, index) => {
      const px = plotX + (series.length === 1 ? plotW / 2 : (index * plotW) / (series.length - 1))
      const py = plotY + plotH - (Math.max(0, item.value) / max) * plotH
      ctx.fillStyle = '#2563eb'
      ctx.beginPath()
      ctx.arc(px, py, block.chart_type === 'scatter' ? 5 : 3, 0, Math.PI * 2)
      ctx.fill()
      if (index % labelEvery === 0 || index === series.length - 1) {
        ctx.fillStyle = '#0f172a'
        ctx.font = '11px Arial'
        ctx.textAlign = 'center'
        ctx.fillText(compactNumber(item.value), px, Math.max(plotY + 12, py - 8))
        drawTruncatedText(ctx, item.label, px - 34, plotY + plotH + 20, 68)
        ctx.textAlign = 'left'
      }
    })
    return
  }
  if (block.chart_type === 'pie' || block.chart_type === 'donut') {
    const total = series.reduce((sum, item) => sum + Math.max(0, item.value), 0) || 1
    const cx = x + Math.min(w * 0.34, 260)
    const cy = y + h / 2 + 8
    const r = Math.min(w * 0.26, h * 0.36)
    let angle = -Math.PI / 2
    series.slice(0, 10).forEach((item, index) => {
      const sweep = (Math.max(0, item.value) / total) * Math.PI * 2
      ctx.fillStyle = colors[index % colors.length]
      ctx.beginPath()
      ctx.moveTo(cx, cy)
      ctx.arc(cx, cy, r, angle, angle + sweep)
      ctx.closePath()
      ctx.fill()
      angle += sweep
    })
    if (block.chart_type === 'donut') {
      ctx.fillStyle = '#ffffff'
      ctx.beginPath()
      ctx.arc(cx, cy, r * 0.58, 0, Math.PI * 2)
      ctx.fill()
    }
    ctx.font = '12px Arial'
    series.slice(0, 10).forEach((item, index) => {
      const ly = plotY + index * 20
      const lx = x + Math.min(w * 0.58, 620)
      ctx.fillStyle = colors[index % colors.length]
      ctx.fillRect(lx, ly - 10, 12, 12)
      ctx.fillStyle = '#334155'
      drawTruncatedText(
        ctx,
        `${item.label}: ${compactNumber(item.value)} (${((Math.max(0, item.value) / total) * 100).toFixed(1)}%)`,
        lx + 18,
        ly,
        x + w - lx - 24,
      )
    })
    return
  }
  if (block.chart_type === 'horizontal-bar') {
    const items = series.slice(0, 10)
    const rowH = plotH / Math.max(items.length, 1)
    items.forEach((item, index) => {
      const rowY = plotY + index * rowH
      const valueW = (Math.max(0, item.value) / max) * (plotW - 150)
      ctx.fillStyle = '#334155'
      ctx.font = '11px Arial'
      drawTruncatedText(ctx, item.label, plotX, rowY + rowH * 0.62, 130)
      ctx.fillStyle = colors[index % colors.length]
      ctx.fillRect(plotX + 144, rowY + rowH * 0.2, valueW, Math.max(8, rowH * 0.48))
      ctx.fillStyle = '#0f172a'
      ctx.fillText(compactNumber(item.value), plotX + 150 + valueW, rowY + rowH * 0.62)
    })
    return
  }
  const barGap = 8
  const barW = Math.max(8, (plotW - barGap * (series.length - 1)) / series.length)
  const labelEvery = Math.max(1, Math.ceil(series.length / 8))
  series.forEach((item, index) => {
    const valueH = (Math.max(0, item.value) / max) * plotH
    const bx = plotX + index * (barW + barGap)
    const by = plotY + plotH - valueH
    ctx.fillStyle = colors[index % colors.length]
    ctx.fillRect(bx, by, barW, valueH)
    ctx.fillStyle = '#0f172a'
    ctx.font = '11px Arial'
    ctx.textAlign = 'center'
    ctx.fillText(compactNumber(item.value), bx + barW / 2, Math.max(plotY + 12, by - 6))
    if (index % labelEvery === 0 || index === series.length - 1) {
      drawTruncatedText(ctx, item.label, bx - 16, plotY + plotH + 20, Math.max(44, barW + 32))
    }
    ctx.textAlign = 'left'
  })
}

async function downloadDashboardPNG(blocks: RenderBlock[], name: string, title: string) {
  const width = 1400
  const padding = 44
  const cardGap = 24
  const cardH = 360
  const headerH = 118
  const height = headerH + padding + blocks.length * cardH + Math.max(0, blocks.length - 1) * cardGap + padding
  const scale = Math.min(2, window.devicePixelRatio || 1)
  const canvas = document.createElement('canvas')
  canvas.width = width * scale
  canvas.height = height * scale
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error('canvas unavailable')
  ctx.scale(scale, scale)
  ctx.fillStyle = '#f8fafc'
  ctx.fillRect(0, 0, width, height)
  ctx.fillStyle = '#0f172a'
  ctx.font = '700 32px Arial'
  ctx.fillText(title || 'Dashboard', padding, 58)
  ctx.font = '14px Arial'
  ctx.fillStyle = '#64748b'
  ctx.fillText(`Exported ${new Date().toLocaleString()} · ${blocks.length} view${blocks.length === 1 ? '' : 's'}`, padding, 86)
  let y = headerH
  for (const block of blocks) {
    drawRoundRect(ctx, padding, y, width - padding * 2, cardH, 14)
    ctx.fillStyle = '#ffffff'
    ctx.fill()
    ctx.strokeStyle = '#d8dee9'
    ctx.stroke()
    ctx.fillStyle = '#0f172a'
    ctx.font = '700 20px Arial'
    drawWrappedText(ctx, block.title || block.query_name || `Block ${block.id}`, padding + 24, y + 36, 900, 24, 1)
    ctx.fillStyle = '#64748b'
    ctx.font = '13px Arial'
    ctx.fillText(`${block.chart_type} · ${block.row_count} rows · ${block.duration_ms} ms`, padding + 24, y + 62)
    if (block.error) {
      ctx.fillStyle = '#b91c1c'
      ctx.font = '14px Arial'
      drawWrappedText(ctx, block.error, padding + 24, y + 110, width - padding * 2 - 48, 20, 4)
    } else if (isDashboardChartType(block.chart_type) || block.chart_type === 'kpi') {
      if (block.chart_type === 'kpi') {
        ctx.fillStyle = '#0f172a'
        ctx.font = '700 56px Arial'
        ctx.fillText(String(numericSeries(block)[0]?.value ?? block.row_count), padding + 24, y + 170)
        ctx.fillStyle = '#64748b'
        ctx.font = '16px Arial'
        ctx.fillText(defaultYKey(block) || 'value', padding + 24, y + 202)
      } else {
        const chartX = padding + 24
        const chartY = y + 88
        const chartW = width - padding * 2 - 48
        const chartH = 230
        const drewRenderedChart = await drawRenderedChartSVG(ctx, block, chartX, chartY, chartW, chartH)
        if (!drewRenderedChart) {
          drawChartPreview(ctx, block, chartX, chartY, chartW, chartH)
        }
      }
    } else {
      drawTablePreview(ctx, block, padding + 24, y + 88, width - padding * 2 - 48, 230)
    }
    y += cardH + cardGap
  }
  const blob = await new Promise<Blob>((resolve, reject) => {
    canvas.toBlob((value) => value ? resolve(value) : reject(new Error('PNG export failed')), 'image/png')
  })
  downloadBlob(blob, `${sanitizeFileName(name)}.png`)
}

function blockElement(block: RenderBlock) {
  return document.querySelector(`[data-block-id="${block.id}"]`) as HTMLElement | null
}

function toggleExportMenu(target: 'dashboard' | `block-${number}`) {
  exportMenuOpen.value = exportMenuOpen.value === target ? '' : target
}

async function handleDashboardExport(format: ExportFormat) {
  exportMenuOpen.value = ''
  if (!format || !filteredRenderedBlocks.value.length) return
  const blocks = filteredRenderedBlocks.value
  const name = dashboardExportName()
  exportBusy.value = `dashboard-${format}`
  try {
    switch (format) {
      case 'pdf':
        printDashboardPDF(blocks, rendered.value?.name || selectedDashboard.value?.name || 'Dashboard')
        break
      case 'image':
        await downloadDashboardPNG(blocks, name, rendered.value?.name || selectedDashboard.value?.name || 'Dashboard')
        break
      case 'excel':
        downloadXLSX(excelSheetsFromBlocks(blocks, rendered.value?.name || name), name)
        break
      case 'csv':
        downloadText('\ufeff' + csvFromBlocks(blocks), `${name}.csv`, 'text/csv;charset=utf-8')
        break
      case 'sql':
        downloadText(dashboardSQL(blocks), `${name}.sql`, 'application/sql;charset=utf-8')
        break
      case 'json':
        downloadText(JSON.stringify(dashboardJSONPayload(blocks), null, 2), `${name}.json`, 'application/json;charset=utf-8')
        break
    }
    toast.success(`${exportFormatLabel(format)} export ready`)
  } catch (e: any) {
    toast.error(e?.message || 'Export failed')
  } finally {
    exportBusy.value = ''
  }
}

async function handleBlockExport(block: RenderBlock, format: ExportFormat) {
  exportMenuOpen.value = ''
  if (!format) return
  const name = blockExportName(block)
  exportBusy.value = `block-${block.id}-${format}`
  try {
    switch (format) {
      case 'pdf': {
        printDashboardPDF([block], block.title || block.query_name || name)
        break
      }
      case 'image': {
        await downloadDashboardPNG([block], name, block.title || block.query_name || name)
        break
      }
      case 'excel':
        downloadXLSX(excelSheetsFromBlocks([block], block.title || block.query_name || name), name)
        break
      case 'csv':
        downloadText('\ufeff' + csvFromBlocks([block]), `${name}.csv`, 'text/csv;charset=utf-8')
        break
      case 'sql':
        downloadText(blockSQL(block), `${name}.sql`, 'application/sql;charset=utf-8')
        break
      case 'json':
        downloadJSON(block.columns, block.rows, name)
        break
    }
    toast.success(`${exportFormatLabel(format)} export ready`)
  } catch (e: any) {
    toast.error(e?.message || 'Export failed')
  } finally {
    exportBusy.value = ''
  }
}

function inferredParamType(name: string): DashboardBlockParam['type'] {
  const normalized = name.trim().toLowerCase()
  if (normalized.includes('date') || normalized.includes('time')) return 'date'
  if (normalized.endsWith('_id') || normalized.includes('count') || normalized.includes('limit') || normalized.includes('days')) {
    return 'number'
  }
  return 'text'
}

function inferredParamsFromSQL(sql: string): DashboardBlockParam[] {
  const seen = new Set<string>()
  const out: DashboardBlockParam[] = []
  for (const match of sql.matchAll(/\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}/g)) {
    const name = match[1]?.trim()
    if (!name || seen.has(name)) continue
    seen.add(name)
    out.push({
      name,
      label: name.replace(/_/g, ' ').replace(/\b\w/g, (char: string) => char.toUpperCase()),
      type: inferredParamType(name),
      default_value: '',
    })
  }
  return out
}

function syncBlockParams(params: DashboardBlockParam[], sql: string) {
  const detected = inferredParamsFromSQL(sql)
  const byName = new Map(params.map((param) => [param.name, { ...param }]))
  const merged = detected.map((param) => ({
    ...param,
    ...(byName.get(param.name) ?? {}),
  }))
  const detectedNames = new Set(merged.map((param) => param.name))
  const extras = params
    .filter((param) => param.name && !detectedNames.has(param.name))
    .map((param) => ({ ...param }))
  return [...merged, ...extras]
}

function syncBlockParamsForBlock(block: DashboardBlock) {
  return syncBlockParams(block.params ?? [], querySQL(block.saved_query_id))
}

function refreshEditingBlockParams() {
  if (!editingBlock.value) return
  editingBlock.value.params = syncBlockParams(editingBlock.value.params ?? [], querySQL(editingBlock.value.saved_query_id))
}

function addEditingParam() {
  if (!editingBlock.value) return
  editingBlock.value.params = [
    ...(editingBlock.value.params ?? []),
    { name: '', label: '', type: 'text', default_value: '' },
  ]
}

function removeEditingParam(index: number) {
  if (!editingBlock.value) return
  editingBlock.value.params = editingBlock.value.params.filter((_, idx) => idx !== index)
}

function defaultXKey(block: RenderBlock) {
  return block.x_key || block.columns[0] || ''
}

function defaultYKey(block: RenderBlock) {
  return block.y_key || block.columns[1] || block.columns[0] || ''
}

function blockValue(block: RenderBlock, row: any[], key: string) {
  const index = block.columns.indexOf(key)
  return index >= 0 ? row[index] : null
}

function numericSeries(block: RenderBlock) {
  const xKey = defaultXKey(block)
  const yKey = defaultYKey(block)
  return block.rows
    .map((row) => ({
      label: String(blockValue(block, row, xKey) ?? ''),
      value: Number(blockValue(block, row, yKey) ?? 0),
    }))
    .filter((item) => !Number.isNaN(item.value))
    .slice(0, 12)
}

function maxSeriesValue(block: RenderBlock) {
  return Math.max(1, ...numericSeries(block).map((item) => item.value))
}

function linePoints(block: RenderBlock) {
  const series = numericSeries(block)
  if (!series.length) return ''
  const max = maxSeriesValue(block)
  return series
    .map((item, index) => {
      const x = series.length === 1 ? 16 : 16 + (index * 240) / (series.length - 1)
      const y = 120 - (item.value / max) * 92
      return `${x},${y}`
    })
    .join(' ')
}

function chartTypeForOption(chartType: DashboardChartType): RenderableDashboardChartType {
  return isDashboardChartType(chartType) ? chartType : 'bar'
}

function dashboardChartType(block: RenderBlock): RenderableDashboardChartType {
  return isDashboardChartType(block.chart_type) ? block.chart_type : 'bar'
}

function normalizeSemanticKey(value: string) {
  return String(value ?? '')
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '')
}

function normalizeSemanticOptionValue(value: any) {
  const text = String(value ?? '').trim()
  return text.length ? text : ''
}

function prettifySemanticLabel(column: string) {
  return String(column ?? '')
    .replace(/[_-]+/g, ' ')
    .replace(/\b\w/g, (char: string) => char.toUpperCase())
}

function isSemanticFilterCandidate(column: string, rows: any[][], index: number) {
  const key = normalizeSemanticKey(column)
  if (!key || key.endsWith('_id') || key === 'id') return false
  const values = rows
    .map((row) => normalizeSemanticOptionValue(row[index]))
    .filter(Boolean)
  if (!values.length) return false
  const unique = [...new Set(values)]
  if (unique.length < 2 || unique.length > 20) return false
  return unique.every((value) => Number.isNaN(Number(value)))
}

function rowMatchesSemanticFilter(block: RenderBlock, row: any[], key: string, values: string[]) {
  const index = block.columns.findIndex((column) => normalizeSemanticKey(column) === key)
  if (index < 0) return true
  return values.includes(normalizeSemanticOptionValue(row[index]))
}

function toggleSemanticFilter(key: string, value: string) {
  const current = semanticFilters.value[key] ?? []
  const next = current.includes(value) ? current.filter((v) => v !== value) : [...current, value]
  semanticFilters.value = { ...semanticFilters.value, [key]: next }
}

function updateSemanticFilter(key: string, event: Event) {
  const target = event.target as HTMLSelectElement | null
  if (!target) return
  semanticFilters.value = {
    ...semanticFilters.value,
    [key]: Array.from(target.selectedOptions).map((option) => option.value).filter(Boolean),
  }
}

function isoDateForOffset(daysAgo: number) {
  const date = new Date()
  date.setHours(0, 0, 0, 0)
  date.setDate(date.getDate() - daysAgo)
  return date.toISOString().slice(0, 10)
}

function applyDateRangePreset(startKey: string, endKey: string, days: number) {
  dashboardParams.value = {
    ...dashboardParams.value,
    [startKey]: isoDateForOffset(days - 1),
    [endKey]: isoDateForOffset(0),
  }
}

function applyDayCountPreset(days: number) {
  if (!dayCountParamName.value) return
  dashboardParams.value = {
    ...dashboardParams.value,
    [dayCountParamName.value]: String(days),
  }
}

function clearDashboardFilters() {
  dashboardParams.value = {}
}

function clearSemanticFilters() {
  semanticFilters.value = {}
}

watch(selectedDashboardId, async (id) => {
  if (isSharedView.value) return
  editMode.value = false
  if (!id) return
  await loadDashboardDetail(id)
  await renderDashboard(id)
  applyPendingBlock()
}, { immediate: false })

watch(
  () => editingBlock.value?.saved_query_id,
  () => {
    refreshEditingBlockParams()
  },
)

watch([blockSourceMode, newBlockQueryId, newBlockChartType, newBlockConnId, newBlockSQL], () => {
  resetBlockPreview()
})

watch(connections, (items) => {
  if (!newBlockConnId.value && items.length > 0) {
    newBlockConnId.value = items[0].id
  }
}, { immediate: true })

watch(semanticFilterOptions, (options) => {
  const allowed = new Map(options.map((option) => [option.key, new Set(option.options)]))
  const next: Record<string, string[]> = {}
  for (const [key, values] of Object.entries(semanticFilters.value)) {
    const allowedValues = allowed.get(key)
    if (!allowedValues) continue
    const filtered = values.filter((value) => allowedValues.has(value))
    if (filtered.length) next[key] = filtered
  }
  semanticFilters.value = next
})

onMounted(async () => {
  if (isSharedView.value) {
    await loadSharedDashboard()
    return
  }
  await Promise.all([fetchSavedQueries(), fetchConnections(), loadDashboards(), loadDashboardUsers()])
  if (selectedDashboardId.value) {
    await loadDashboardDetail(selectedDashboardId.value)
    await renderDashboard(selectedDashboardId.value)
  }
  applyPendingBlock()
})
</script>

<template>
  <div class="page-shell adb-full-shell" :class="{ 'adb-full-shell--embed': isEmbedView }">
    <div ref="dashboardScreenRef" class="adb-full">
      <header v-if="!isEmbedView" class="adb-topbar">
        <div class="adb-titlebar">
          <div class="adb-section-title">{{ isSharedView ? (rendered?.name || 'Shared Dashboard') : (selectedDashboard?.name || 'Dashboards') }}</div>
        </div>
        <div v-if="!isSharedView" class="adb-topbar__picker">
          <select v-model="selectedDashboardId" class="base-input adb-dashboard-select">
            <option :value="null">Select dashboard</option>
            <option v-for="dashboard in dashboards" :key="dashboard.id" :value="dashboard.id">
              {{ dashboard.name }}
            </option>
          </select>
          <button class="base-btn base-btn--primary base-btn--sm" @click="openCreateDashboard">New</button>
        </div>
        <div class="adb-topbar__actions">
          <div class="adb-export" data-export-ignore="true">
            <button
              class="base-btn base-btn--ghost base-btn--sm adb-export-trigger"
              :disabled="!filteredRenderedBlocks.length || !!exportBusy"
              @click="toggleExportMenu('dashboard')"
            >
              {{ exportBusy.startsWith('dashboard-') ? 'Exporting...' : 'Export' }}
            </button>
            <div v-if="exportMenuOpen === 'dashboard'" class="adb-export-menu">
              <button
                v-for="option in exportOptions"
                :key="option.value"
                class="adb-export-option"
                :disabled="!!exportBusy"
                @click="handleDashboardExport(option.value)"
              >
                <span class="adb-export-option__label">{{ option.label }}</span>
                <span class="adb-export-option__detail">{{ option.detail }}</span>
              </button>
            </div>
          </div>
          <button
            v-if="canEditDashboard"
            class="base-btn base-btn--ghost base-btn--sm"
            :class="{ 'adb-btn-active': editMode }"
            @click="editMode = !editMode"
          >
            {{ editMode ? 'Done' : 'Edit' }}
          </button>
          <button
            class="base-btn base-btn--ghost base-btn--sm"
            :disabled="isSharedView ? !sharedToken : !selectedDashboardId"
            @click="isSharedView ? loadSharedDashboard() : refreshSelectedDashboard()"
          >
            Refresh
          </button>
        </div>
      </header>

      <section v-if="!isEmbedView && (selectedDashboardId || isSharedView) && rendered?.params?.length" class="adb-filter-strip">
        <label v-for="param in rendered.params" :key="param.name" class="adb-param-field adb-param-field--compact">
          <span>{{ param.label }}</span>
          <input
            v-model="dashboardParams[param.name]"
            class="base-input"
            :type="param.type === 'number' ? 'number' : (param.type === 'date' ? 'date' : 'text')"
            :placeholder="param.name"
          />
        </label>
        <button class="base-btn base-btn--primary base-btn--sm" @click="isSharedView ? loadSharedDashboard() : (selectedDashboardId && renderDashboard(selectedDashboardId))">Apply</button>
        <button class="base-btn base-btn--ghost base-btn--sm" @click="clearDashboardFilters">Clear</button>
      </section>

      <section v-if="!isSharedView && editMode" class="page-panel adb-edit-tray" data-export-ignore="true">
        <div class="adb-edit-tray__builder">
          <div class="adb-source-tabs">
            <button :class="['adb-source-tab', { 'adb-source-tab--active': blockSourceMode === 'sql' }]" @click="blockSourceMode = 'sql'">Direct SQL</button>
            <button :class="['adb-source-tab', { 'adb-source-tab--active': blockSourceMode === 'saved' }]" @click="blockSourceMode = 'saved'">Saved Query</button>
          </div>
          <div class="adb-builder-grid">
            <input v-model="newBlockTitle" class="base-input adb-builder-title" placeholder="Block title" />
            <select v-if="blockSourceMode === 'sql'" v-model.number="newBlockConnId" class="base-input">
              <option :value="null">Select connection…</option>
              <option v-for="conn in connections" :key="conn.id" :value="conn.id">{{ conn.name }}</option>
            </select>
            <select v-else v-model="newBlockQueryId" class="base-input">
              <option :value="null">Select saved query…</option>
              <option v-for="query in availableQueries" :key="query.id" :value="query.id">{{ query.name }}</option>
            </select>
            <select v-model="newBlockChartType" class="base-input adb-edit-tray__type">
              <option v-for="option in chartTypeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
            </select>
            <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!canPreviewBlock" @click="previewDraftBlock">
              {{ blockPreviewing ? 'Previewing...' : 'Preview' }}
            </button>
            <button class="base-btn base-btn--primary base-btn--sm" :disabled="!canSavePreviewedBlock" @click="saveDraftBlock">
              {{ savingDraftBlock ? 'Saving...' : 'Save Block' }}
            </button>
          </div>
          <textarea
            v-if="blockSourceMode === 'sql'"
            v-model="newBlockSQL"
            class="base-input adb-builder-sql"
            spellcheck="false"
            placeholder="select date_trunc('day', created_at) as day, count(*) as total from orders group by 1 order by 1"
          ></textarea>
          <div v-else class="adb-builder-saved-preview">
            {{ selectedDraftSavedQuery?.sql || 'Select a saved query to preview its SQL.' }}
          </div>
          <div v-if="blockPreviewError" class="adb-error">{{ blockPreviewError }}</div>
          <div v-if="blockPreview" class="adb-builder-preview">
            <div class="adb-builder-preview__head">
              <span>{{ blockPreview.row_count }} rows · {{ blockPreview.duration_ms }} ms</span>
              <span v-if="blockPreviewSignature !== draftBlockSignature">Preview is stale</span>
            </div>
            <div v-if="newBlockChartType === 'kpi'" class="adb-kpi-block adb-builder-kpi">
              <div class="adb-kpi">{{ blockPreview.rows[0]?.[blockPreview.columns.indexOf(previewYKey())] ?? blockPreview.row_count }}</div>
              <div class="adb-kpi__label">{{ previewYKey() || 'value' }}</div>
            </div>
            <div v-else-if="isDashboardChartType(newBlockChartType)" class="adb-builder-chart">
              <ResultChart
                :columns="blockPreview.columns"
                :rows="blockPreview.rows"
                :default-chart-type="chartTypeForOption(newBlockChartType)"
                :initial-x-col="blockPreview.columns[0] || ''"
                :initial-y-col="previewYKey()"
                :hide-controls="true"
              />
            </div>
            <div v-else class="adb-table-wrap adb-builder-table">
              <table class="adb-table">
                <thead>
                  <tr>
                    <th v-for="column in blockPreview.columns.slice(0, 8)" :key="column">{{ column }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(row, rowIndex) in blockPreview.rows.slice(0, 8)" :key="rowIndex">
                    <td v-for="(value, colIndex) in row.slice(0, 8)" :key="`${rowIndex}-${colIndex}`">{{ formatCellValue(value) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>
        <div class="adb-edit-tray__sep"></div>
        <template v-if="dashboardDetail">
          <select class="base-input adb-edit-tray__vis" :value="dashboardDetail.visibility" :disabled="savingAccess" @change="handleVisibilityChange">
            <option value="private">Private</option>
            <option value="shared">Shared</option>
            <option value="public">Public Link</option>
          </select>
          <button v-if="dashboardDetail.visibility === 'public'" class="base-btn base-btn--ghost base-btn--sm" :disabled="savingAccess || !shareURL" @click="copyShareLink">Copy Link</button>
          <button v-if="dashboardDetail.visibility === 'public'" class="base-btn base-btn--ghost base-btn--sm" :disabled="savingAccess || !dashboardEmbedCode" @click="copyDashboardEmbedCode">Copy Embed</button>
          <button v-if="selectedDashboard" class="base-btn base-btn--ghost base-btn--sm" style="color:#f87171" @click="deleteDashboard(selectedDashboard.id)">Delete</button>
        </template>
      </section>

      <main class="adb-dashboard-canvas">
        <div v-if="loading" class="page-panel adb-empty adb-empty--canvas">Loading dashboards...</div>
        <div v-else-if="rendering" class="page-panel adb-empty adb-empty--canvas">Rendering dashboard...</div>
        <div v-else-if="!selectedDashboardId && !isSharedView" class="page-panel adb-empty adb-empty--canvas">
          <div>No dashboard selected.</div>
          <button class="base-btn base-btn--primary base-btn--sm" @click="openCreateDashboard">Create Dashboard</button>
        </div>
        <div v-else-if="!filteredRenderedBlocks.length" class="page-panel adb-empty adb-empty--canvas">
          <div>Dashboard blocks will appear here.</div>
          <div v-if="editMode" class="adb-section-sub">Add a saved query from the edit tray to fill the canvas.</div>
        </div>
        <div v-else class="adb-grid" :class="{ 'adb-grid--editing': editMode && !isSharedView }">
          <article
            v-for="block in filteredRenderedBlocks"
            :key="block.id"
            class="page-panel adb-card"
            :class="[
              `adb-card--span-${currentBlockSpan(block)}`,
              `adb-card--row-${currentBlockRowSpan(block)}`,
              {
                'adb-card--dragging': draggingBlockId === block.id,
                'adb-card--drop-target': droppingBlockId === block.id,
                'adb-canvas-card--resizing': resizingBlockId === block.id,
              },
            ]"
            :data-block-id="block.id"
          >
            <div class="adb-card__head">
              <div class="adb-card__head-left">
                <div v-if="renamingBlockId === block.id" class="adb-inline-rename">
                  <input
                    v-model="renameDraft"
                    class="base-input"
                    @keyup.enter="commitInlineRename(block)"
                    @keyup.esc="cancelInlineRename"
                  />
                  <button v-if="!isSharedView" class="base-btn base-btn--primary base-btn--xs" @click="commitInlineRename(block)">Save</button>
                </div>
                <div v-else class="adb-card__title" @dblclick="editMode && !isSharedView && startInlineRename(block)">{{ block.title }}</div>
                <div class="adb-card__meta">{{ block.row_count }} rows · {{ block.duration_ms }}ms</div>
              </div>
              <div v-if="!isEmbedView" class="adb-render-card__actions">
                <span class="adb-chart-badge">{{ block.chart_type }}</span>
                <div class="adb-export adb-export--block" data-export-ignore="true">
                  <button
                    class="base-btn base-btn--ghost base-btn--xs adb-export-trigger"
                    :disabled="!!exportBusy"
                    @click="toggleExportMenu(`block-${block.id}`)"
                  >
                    {{ exportBusy.startsWith(`block-${block.id}-`) ? 'Exporting...' : 'Export' }}
                  </button>
                  <div v-if="exportMenuOpen === `block-${block.id}`" class="adb-export-menu adb-export-menu--block">
                    <button
                      v-for="option in exportOptions"
                      :key="option.value"
                      class="adb-export-option"
                      :disabled="!!exportBusy"
                      @click="handleBlockExport(block, option.value)"
                    >
                      <span class="adb-export-option__label">{{ option.label }}</span>
                      <span class="adb-export-option__detail">{{ option.detail }}</span>
                    </button>
                  </div>
                </div>
                <button
                  v-if="dashboardDetail?.visibility === 'public'"
                  class="base-btn base-btn--ghost base-btn--xs"
                  :disabled="!blockEmbedURL(block)"
                  @click="copyBlockEmbedCode(block)"
                >
                  Embed
                </button>
                <template v-if="editMode && !isSharedView">
                  <span class="adb-drag-handle" title="Drag to reorder" @mousedown.prevent="handleDragHandleMouseDown(block, $event)">⠿</span>
                  <button class="base-btn base-btn--ghost base-btn--xs" @click="autoTuneRenderedBlock(block)">Auto</button>
                  <button class="base-btn base-btn--ghost base-btn--xs" @click="startEditBlock(block)">Edit</button>
                  <button class="base-btn base-btn--ghost base-btn--xs" style="color:#f87171" @click="deleteBlock(block.id)">✕</button>
                </template>
              </div>
            </div>
            <button v-if="editMode && !isSharedView" class="adb-resize-handle adb-resize-handle--render" @mousedown="beginBlockResize(block, $event)" title="Drag to resize width and height"></button>

            <div v-if="!isEmbedView && !block.error" class="adb-view-filters" data-export-ignore="true">
              <input
                v-model="blockTextFilters[block.id]"
                class="base-input adb-view-filter__search"
                placeholder="Filter this view..."
              />
              <select
                v-for="filter in blockFilterOptions(block)"
                :key="filter.column"
                class="base-input adb-view-filter__select"
                :value="blockColumnFilters[block.id]?.[filter.column] ?? ''"
                @change="handleBlockColumnFilter(block.id, filter.column, $event)"
              >
                <option value="">All {{ filter.label }}</option>
                <option v-for="option in filter.options" :key="option" :value="option">{{ formatCellValue(option) }}</option>
              </select>
              <button
                v-if="blockHasFilters(block)"
                class="base-btn base-btn--ghost base-btn--xs"
                @click="clearBlockFilters(block.id)"
              >
                Clear
              </button>
            </div>

            <div v-if="block.error" class="adb-error">{{ block.error }}</div>

            <template v-else-if="block.chart_type === 'kpi'">
              <div class="adb-kpi-block">
                <div class="adb-kpi">
                  {{ numericSeries(block)[0]?.value ?? block.row_count }}
                </div>
                <div class="adb-kpi__label">{{ defaultYKey(block) || 'value' }}</div>
              </div>
            </template>

            <template v-else-if="isDashboardChartType(block.chart_type)">
              <div class="adb-chart-fill">
                <ResultChart
                  :columns="block.columns"
                  :rows="block.rows"
                  :default-chart-type="dashboardChartType(block)"
                  :initial-x-col="defaultXKey(block)"
                  :initial-y-col="defaultYKey(block)"
                  :hide-controls="true"
                />
              </div>
            </template>

            <template v-else>
              <div class="adb-table-wrap">
                <table class="adb-table">
                  <thead>
                    <tr>
                      <th v-for="column in block.columns.slice(0, 8)" :key="column">{{ column }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="(row, rowIndex) in block.rows.slice(0, 10)" :key="rowIndex">
                      <td v-for="(value, valueIndex) in row.slice(0, 8)" :key="valueIndex">{{ formatCellValue(value) }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </template>
          </article>
        </div>
      </main>
    </div>

    <Teleport to="body">
      <div
        v-if="draggingBlockId"
        class="adb-drag-ghost"
        :style="{ left: dragX + 'px', top: dragY + 'px' }"
      >
        <span class="adb-drag-ghost__badge">{{ dragGhostType }}</span>
        <span class="adb-drag-ghost__title">{{ dragGhostTitle }}</span>
      </div>
    </Teleport>

    <Teleport to="body">
      <div
        v-if="resizeGhostRect"
        class="adb-resize-ghost"
        :style="{
          left: resizeGhostRect.left + 'px',
          top: resizeGhostRect.top + 'px',
          width: resizeGhostRect.width + 'px',
          height: resizeGhostRect.height + 'px',
        }"
      >
        <span class="adb-resize-ghost__label">
          {{ resizePreview[resizingBlockId ?? 0]?.column_span ?? resizeStartColumnSpan }}c ×
          {{ resizePreview[resizingBlockId ?? 0]?.row_span ?? resizeStartRowSpan }}r
        </span>
      </div>
    </Teleport>

    <Transition name="modal">
      <div v-if="createDashboardOpen" class="modal-backdrop" @click.self="createDashboardOpen = false">
        <div class="modal modal--md">
          <div class="modal-hd">
            <span class="modal-title">Create Dashboard</span>
            <button class="base-btn base-btn--ghost base-btn--xs" @click="createDashboardOpen = false">×</button>
          </div>
          <div class="modal-bd">
            <input v-model="newDashboardName" class="base-input" placeholder="Dashboard name" @keyup.enter="createDashboard" />
            <textarea v-model="newDashboardDescription" class="base-input adb-textarea" rows="4" placeholder="What is this dashboard for?" />
          </div>
          <div class="modal-ft">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="createDashboardOpen = false">Cancel</button>
            <button class="base-btn base-btn--primary base-btn--sm" :disabled="!newDashboardName.trim() || creating" @click="createDashboard">
              {{ creating ? 'Creating...' : 'Create Dashboard' }}
            </button>
          </div>
        </div>
      </div>
    </Transition>

    <Transition name="modal">
      <div v-if="editingBlock" class="modal-backdrop" @click.self="cancelEditBlock">
        <div class="modal modal--lg adb-block-modal">
          <div class="modal-hd">
            <span class="modal-title">Edit Dashboard Block</span>
            <button class="base-btn base-btn--ghost base-btn--xs" @click="cancelEditBlock">×</button>
          </div>
          <div class="modal-bd">
            <div class="adb-canvas-card__edit-grid">
              <input v-model="editingBlock.title" class="base-input" placeholder="Block title" />
              <select v-model="editingBlock.chart_type" class="base-input">
                <option v-for="option in chartTypeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
              </select>
              <select v-model="editingBlock.column_span" class="base-input">
                <option v-for="option in blockSpanOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
              </select>
              <select v-model="editingBlock.row_span" class="base-input">
                <option :value="1">Short</option>
                <option :value="2">Standard</option>
                <option :value="3">Tall</option>
              </select>
              <template v-if="isDashboardChartType(editingBlock.chart_type)">
                <input v-model="editingBlock.x_key" class="base-input" placeholder="X column" />
                <input v-model="editingBlock.y_key" class="base-input" placeholder="Y column" />
              </template>
              <template v-else-if="editingBlock.chart_type === 'kpi'">
                <input v-model="editingBlock.y_key" class="base-input" placeholder="Metric column" />
              </template>
            </div>
            <div class="adb-param-editor">
              <div class="adb-param-editor__head">
                <div>
                  <div class="adb-section-title">Parameters</div>
                  <div class="adb-section-sub">Configure labels, input types, and defaults for SQL placeholders.</div>
                </div>
                <div class="adb-inline-actions">
                  <button class="base-btn base-btn--ghost base-btn--xs" @click="refreshEditingBlockParams">Sync from SQL</button>
                  <button class="base-btn base-btn--ghost base-btn--xs" @click="addEditingParam">Add Param</button>
                </div>
              </div>
              <div v-if="!editingBlock.params?.length" class="adb-empty">No parameters detected for this query.</div>
              <div v-else class="adb-param-editor__list">
                <div v-for="(param, paramIndex) in editingBlock.params" :key="`${param.name || 'param'}-${paramIndex}`" class="adb-param-row">
                  <input v-model="param.name" class="base-input" placeholder="name" />
                  <input v-model="param.label" class="base-input" placeholder="Label" />
                  <select v-model="param.type" class="base-input">
                    <option v-for="type in paramTypeOptions" :key="type" :value="type">{{ type }}</option>
                  </select>
                  <input
                    v-model="param.default_value"
                    class="base-input"
                    :type="param.type === 'number' ? 'number' : (param.type === 'date' ? 'date' : 'text')"
                    placeholder="Default value"
                  />
                  <button class="base-btn base-btn--ghost base-btn--xs" style="color:#f87171" @click="removeEditingParam(paramIndex)">Remove</button>
                </div>
              </div>
            </div>
          </div>
          <div class="modal-ft">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="cancelEditBlock">Cancel</button>
            <button class="base-btn base-btn--primary base-btn--sm" @click="saveBlock">Save Block</button>
          </div>
        </div>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.adb-layout {
  display: grid;
  grid-template-columns: 320px minmax(0, 1fr);
  gap: 16px;
}

.adb-layout--shared {
  grid-template-columns: minmax(0, 1fr);
}

.adb-sidebar,
.adb-builder {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 18px;
}

.adb-section-title {
  font-size: 15px;
  font-weight: 700;
  color: var(--text-primary);
}

.adb-section-sub,
.adb-dashboard-item__meta,
.adb-block-row__meta,
.adb-empty {
  font-size: 12px;
  color: var(--text-muted);
}

.adb-card__meta,
.adb-kpi__label {
  font-size: 11px;
  color: var(--text-muted);
}

.adb-create,
.adb-dashboard-list,
.adb-quick-add,
.adb-canvas,
.adb-canvas-card__edit {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.adb-quick-add {
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: var(--r);
  background: linear-gradient(180deg, rgba(79, 156, 249, 0.08), rgba(79, 156, 249, 0.02));
}

.adb-quick-add__row,
.adb-canvas-card__main,
.adb-canvas-card__actions,
.adb-canvas-card__edit-grid,
.adb-inline-rename,
.adb-render-card__actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.adb-canvas {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.adb-canvas-card {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: var(--r);
  background: var(--bg-elevated);
  box-shadow: 0 10px 24px rgba(15, 23, 42, 0.04);
}

.adb-canvas-card--span-2 {
  grid-column: span 2;
}

.adb-canvas-card--span-3 {
  grid-column: 1 / -1;
}

.adb-canvas-card--dragging,
.adb-card--dragging {
  opacity: 0.4;
  pointer-events: none;
}

.adb-canvas-card--drop-target,
.adb-card--drop-target {
  border-color: var(--brand);
  box-shadow: 0 0 0 2px rgba(79, 156, 249, 0.25);
  transition: border-color 0.1s, box-shadow 0.1s;
}

:global(.adb-drag-ghost) {
  position: fixed;
  z-index: 9999;
  pointer-events: none;
  transform: translate(-50%, -50%);
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 16px;
  border-radius: 999px;
  background: var(--bg-elevated);
  border: 1.5px solid var(--brand);
  box-shadow: 0 8px 28px rgba(0, 0, 0, 0.22);
  white-space: nowrap;
}

:global(.adb-drag-ghost__badge) {
  font-size: 10px;
  font-weight: 800;
  letter-spacing: .1em;
  text-transform: uppercase;
  padding: 2px 7px;
  border-radius: 999px;
  background: var(--brand-dim);
  color: var(--brand);
}

:global(.adb-drag-ghost__title) {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
}

:global(.adb-resize-ghost) {
  position: fixed;
  z-index: 9998;
  pointer-events: none;
  border: 2px dashed var(--brand);
  border-radius: 10px;
  background: color-mix(in srgb, var(--brand) 7%, transparent);
  display: flex;
  align-items: flex-end;
  justify-content: flex-end;
  padding: 10px;
  transition: width 0.04s linear, height 0.04s linear;
}

:global(.adb-resize-ghost__label) {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.04em;
  color: var(--brand);
  background: var(--brand-dim);
  padding: 2px 9px;
  border-radius: 999px;
}

.adb-canvas-card--resizing,
.adb-card.adb-canvas-card--resizing {
  user-select: none;
  border-color: var(--brand);
  box-shadow: 0 0 0 2px rgba(79, 156, 249, 0.18);
}

.adb-canvas-card__main {
  justify-content: space-between;
}

.adb-canvas-card__actions {
  flex-wrap: wrap;
}

.adb-canvas-card__edit-grid {
  flex-wrap: wrap;
}

.adb-chart-setup {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px;
  border-radius: var(--r);
  background: var(--bg-subtle);
}

.adb-drag-handle {
  color: var(--text-muted);
  cursor: grab;
  letter-spacing: 0.08em;
  user-select: none;
}

.adb-inline-rename {
  flex-wrap: wrap;
}

.adb-inline-rename .base-input {
  min-width: 180px;
}

.adb-render-card__actions {
  flex-wrap: wrap;
  justify-content: flex-end;
}

.adb-view-filters {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  padding: 6px 0 2px;
  border-top: 1px solid color-mix(in srgb, var(--border) 72%, transparent);
}

.adb-view-filter__search {
  flex: 1 1 180px;
  min-width: 150px;
  height: 28px;
  font-size: 12px;
  padding: 3px 8px;
}

.adb-view-filter__select {
  flex: 0 1 150px;
  min-width: 120px;
  height: 28px;
  font-size: 12px;
  padding: 3px 7px;
}

.adb-resize-handle {
  align-self: flex-end;
  width: 22px;
  height: 22px;
  border: 0;
  border-radius: 999px;
  background: var(--brand-dim);
  cursor: ew-resize;
  position: relative;
}

.adb-resize-handle::before {
  content: '↔';
  font-size: 11px;
  line-height: 1;
  color: var(--brand);
}

.adb-resize-handle--render {
  margin-top: auto;
}

.adb-textarea {
  min-height: 84px;
}

.adb-dashboard-item,
.adb-block-row {
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: var(--r);
  background: var(--bg-elevated);
  text-align: left;
}

.adb-dashboard-item--active {
  border-color: var(--brand);
  background: var(--brand-dim);
}

.adb-dashboard-item__title,
.adb-block-row__title {
  font-size: 14px;
  font-weight: 700;
  color: var(--text-primary);
}

.adb-card__title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
  letter-spacing: -0.01em;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.adb-main {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.adb-access {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 18px;
}

.adb-access__bar {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.adb-access__assign,
.adb-access__list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.adb-access__assign-bar,
.adb-access__item {
  display: flex;
  align-items: center;
  gap: 10px;
}

.adb-access__item {
  justify-content: space-between;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: var(--r);
  background: var(--bg-elevated);
}

.adb-access__item-main {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.adb-filterbar {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 16px 18px;
}

.adb-filterbar__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.adb-semantic-filters {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 10px;
}

.adb-semantic-filter {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
  color: var(--text-secondary);
}

.adb-semantic-filter small {
  color: var(--text-muted);
}

.adb-multiselect {
  min-height: 116px;
}

.adb-presets {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 14px 18px;
}

.adb-presets__head,
.adb-presets__bar {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
}

.adb-presets__bar {
  align-items: center;
}

.adb-params {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 14px 18px;
}

.adb-filter-presets,
.adb-filter-preset {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.adb-filter-preset {
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--r);
  background: var(--bg-subtle);
}

.adb-params__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.adb-params__grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 10px;
}

.adb-param-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
  color: var(--text-secondary);
}

.adb-builder__head,
.adb-card__head,
.adb-block-row,
.adb-inline-actions,
.adb-param-editor__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.adb-inline-actions {
  justify-content: flex-end;
}

.adb-param-editor,
.adb-param-editor__list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.adb-param-row {
  display: grid;
  grid-template-columns: minmax(120px, 1fr) minmax(140px, 1.1fr) 110px minmax(140px, 1fr) auto;
  gap: 8px;
  align-items: center;
}

.adb-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 16px;
}

.adb-card--span-2 {
  grid-column: span 2;
}

.adb-card--span-3 {
  grid-column: 1 / -1;
}

.adb-card {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 14px;
  transition: opacity 0.15s, border-color 0.15s, box-shadow 0.15s;
}

.adb-chart-badge {
  padding: 3px 8px;
  border-radius: 999px;
  background: var(--brand-dim);
  color: var(--brand);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: .06em;
  text-transform: uppercase;
  flex-shrink: 0;
}

.adb-bars {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.adb-bar {
  display: grid;
  grid-template-columns: 88px minmax(0, 1fr) 52px;
  gap: 10px;
  align-items: center;
}

.adb-bar__label,
.adb-line-labels {
  font-size: 12px;
}

.adb-table th {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.adb-table td {
  font-size: 12px;
  color: var(--text-primary);
}

.adb-bar__track {
  height: 10px;
  border-radius: 999px;
  background: var(--bg-subtle);
  overflow: hidden;
}

.adb-bar__fill {
  height: 100%;
  border-radius: 999px;
  background: linear-gradient(90deg, var(--brand), #2dd4bf);
}

.adb-linechart {
  width: 100%;
  height: 160px;
  background: linear-gradient(180deg, rgba(79,156,249,.08), rgba(79,156,249,0));
  border-radius: var(--r);
}

.adb-line-labels {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(0, 1fr));
  gap: 6px;
  color: var(--text-muted);
}

.adb-kpi {
  font-size: clamp(28px, 3.5vw, 44px);
  font-weight: 800;
  line-height: 1.1;
  color: var(--text-primary);
  letter-spacing: -0.02em;
}

.adb-table-wrap {
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: 6px;
}

.adb-table {
  width: 100%;
  border-collapse: collapse;
  min-width: 100%;
}

.adb-table thead tr {
  background: var(--bg-elevated);
  position: sticky;
  top: 0;
  z-index: 1;
}

.adb-table th,
.adb-table td {
  padding: 6px 10px;
  border-bottom: 1px solid var(--border);
  text-align: left;
  white-space: nowrap;
}

.adb-table tbody tr:last-child td {
  border-bottom: none;
}

.adb-table tbody tr:nth-child(even) {
  background: var(--bg-subtle, rgba(0,0,0,0.02));
}

.adb-table tbody tr:hover {
  background: var(--bg-elevated);
}

.adb-error {
  padding: 12px;
  border-radius: var(--r);
  background: rgba(239, 68, 68, 0.08);
  color: #ef4444;
  font-size: 12px;
}

.adb-full-shell {
  background:
    radial-gradient(circle at 8% 0%, rgba(92, 184, 165, 0.10), transparent 28%),
    radial-gradient(circle at 92% 10%, rgba(221, 163, 81, 0.10), transparent 24%),
    var(--bg-body);
}

.adb-full-shell--embed {
  min-height: 100vh;
  background: var(--bg-body);
}

.adb-full {
  display: flex;
  flex: 1;
  min-height: 0;
  flex-direction: column;
  gap: 6px;
  padding: 8px;
  overflow: hidden;
}

.adb-full-shell--embed .adb-full {
  padding: 8px;
  gap: 0;
}

.adb-topbar,
.adb-filter-strip,
.adb-semantic-strip {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 10px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: color-mix(in srgb, var(--bg-elevated) 88%, transparent);
  box-shadow: var(--shadow-sm);
  flex-shrink: 0;
}

.adb-topbar {
  flex-wrap: wrap;
}

.adb-titlebar {
  flex-shrink: 0;
}

.adb-topbar__picker,
.adb-topbar__actions {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}

.adb-filter-search {
  flex: 1;
  min-width: 160px;
}

.adb-dashboard-select {
  min-width: 180px;
}

.adb-btn-active {
  border-color: var(--brand);
  color: var(--brand);
  background: var(--brand-dim);
}

.adb-filter-strip,
.adb-semantic-strip {
  flex-wrap: wrap;
}

.adb-param-field--compact {
  min-width: 130px;
}

.adb-semantic-filter {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.adb-semantic-filter__label {
  font-size: 11px;
  color: var(--text-muted);
  white-space: nowrap;
  font-weight: 500;
}

.adb-chip-group {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.adb-chip {
  padding: 3px 10px;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: var(--bg-panel);
  color: var(--text-muted);
  font-size: 11px;
  cursor: pointer;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
  white-space: nowrap;
}

.adb-chip:hover {
  border-color: var(--brand);
  color: var(--text-primary);
}

.adb-chip--active {
  background: var(--brand);
  border-color: var(--brand);
  color: #fff;
}

.adb-edit-tray {
  display: flex;
  align-items: stretch;
  gap: 8px;
  padding: 10px;
  flex-shrink: 0;
  flex-wrap: wrap;
}

.adb-edit-tray__builder {
  display: flex;
  flex-direction: column;
  gap: 8px;
  flex: 1;
  min-width: 360px;
}

.adb-source-tabs {
  display: flex;
  width: fit-content;
  border: 1px solid var(--border);
  border-radius: 6px;
  overflow: hidden;
}

.adb-source-tab {
  border: 0;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  font-size: 12px;
  padding: 4px 10px;
}

.adb-source-tab--active {
  background: var(--accent);
  color: #fff;
}

.adb-builder-grid {
  display: grid;
  grid-template-columns: minmax(150px, 1fr) minmax(180px, 1.2fr) minmax(120px, auto) auto auto;
  gap: 8px;
  align-items: center;
}

.adb-builder-title {
  min-width: 0;
}

.adb-builder-sql,
.adb-builder-saved-preview {
  min-height: 82px;
  width: 100%;
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.5;
}

.adb-builder-saved-preview {
  padding: 8px 10px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-panel);
  color: var(--text-secondary);
  overflow: auto;
  white-space: pre-wrap;
}

.adb-builder-preview {
  display: flex;
  flex-direction: column;
  gap: 8px;
  border-top: 1px solid var(--border);
  padding-top: 8px;
}

.adb-builder-preview__head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  color: var(--text-muted);
  font-size: 12px;
}

.adb-builder-chart {
  height: 220px;
}

.adb-builder-table {
  max-height: 220px;
}

.adb-builder-kpi {
  min-height: 130px;
}

.adb-edit-tray__query {
  flex: 1;
  min-width: 180px;
}

.adb-edit-tray__type {
  min-width: 100px;
}

.adb-edit-tray__vis {
  min-width: 110px;
}

.adb-export {
  position: relative;
  display: inline-flex;
  align-items: center;
}

.adb-export-trigger {
  min-width: 78px;
}

.adb-export-menu {
  position: absolute;
  right: 0;
  top: calc(100% + 6px);
  z-index: 40;
  display: grid;
  grid-template-columns: repeat(2, minmax(118px, 1fr));
  gap: 6px;
  min-width: 260px;
  padding: 8px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-elevated);
  box-shadow: var(--shadow-lg);
}

.adb-export-menu--block {
  grid-template-columns: 1fr;
  min-width: 190px;
}

.adb-export-option {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-panel);
  color: var(--text-primary);
  cursor: pointer;
  padding: 8px 10px;
  text-align: left;
}

.adb-export-option:hover:not(:disabled) {
  border-color: var(--brand);
  background: var(--brand-dim);
}

.adb-export-option:disabled {
  cursor: wait;
  opacity: 0.65;
}

.adb-export-option__label {
  font-size: 12px;
  font-weight: 700;
}

.adb-export-option__detail {
  color: var(--text-muted);
  font-size: 11px;
}

.adb-edit-tray__sep {
  width: 1px;
  min-height: 32px;
  background: var(--border);
  flex-shrink: 0;
  margin: 0 2px;
}

.adb-dashboard-canvas {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 2px;
}

.adb-full-shell--embed .adb-dashboard-canvas {
  padding: 0;
}

.adb-empty--canvas {
  min-height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 12px;
  padding: 32px;
}

.adb-grid {
  align-items: stretch;
  grid-auto-rows: 180px;
}

.adb-grid--editing .adb-card {
  cursor: grab;
}

.adb-card {
  position: relative;
  overflow: hidden;
}

.adb-full-shell--embed .adb-card {
  box-shadow: none;
}

.adb-card--row-2 {
  grid-row: span 2;
}

.adb-card--row-3 {
  grid-row: span 3;
}

.adb-card__head {
  align-items: center;
  gap: 8px;
}

.adb-card__head-left {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.adb-card .adb-table-wrap {
  flex: 1;
  min-height: 0;
}

.adb-chart-fill {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.adb-kpi-block {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
}

.adb-drag-handle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border: 1px solid var(--border);
  border-radius: 6px;
  font-size: 14px;
  color: var(--text-muted);
  cursor: grab;
  user-select: none;
  flex-shrink: 0;
}

.adb-drag-handle:hover {
  border-color: var(--brand);
  color: var(--brand);
  background: var(--brand-dim);
}

.adb-resize-handle {
  position: absolute;
  right: 8px;
  bottom: 8px;
  width: 28px;
  height: 28px;
  border: 1px solid color-mix(in srgb, var(--brand) 48%, var(--border));
  background: color-mix(in srgb, var(--brand-dim) 72%, var(--bg-elevated));
  cursor: nwse-resize;
  z-index: 4;
}

.adb-resize-handle::before {
  content: '';
  position: absolute;
  right: 7px;
  bottom: 7px;
  width: 11px;
  height: 11px;
  border-right: 2px solid var(--brand);
  border-bottom: 2px solid var(--brand);
}

.adb-resize-handle::after {
  content: '';
  position: absolute;
  right: 12px;
  bottom: 12px;
  width: 6px;
  height: 6px;
  border-right: 2px solid var(--brand);
  border-bottom: 2px solid var(--brand);
  opacity: .65;
}

.adb-resize-handle--render {
  margin-top: 0;
}

.adb-block-modal .modal-bd {
  max-height: min(70vh, 720px);
  overflow: auto;
}

@media (max-width: 1100px) {
  .adb-grid,
  .adb-layout {
    grid-template-columns: 1fr;
  }

  .adb-canvas {
    grid-template-columns: 1fr;
  }

  .adb-canvas-card--span-2,
  .adb-canvas-card--span-3,
  .adb-card--span-2,
  .adb-card--span-3,
  .adb-card--row-2,
  .adb-card--row-3 {
    grid-column: auto;
    grid-row: auto;
  }

  .adb-edit-tray {
    grid-template-columns: 1fr;
  }

  .adb-builder-grid {
    grid-template-columns: 1fr 1fr;
  }
}

@media (max-width: 720px) {
  .adb-bar {
    grid-template-columns: 1fr;
  }

  .adb-builder__head,
  .adb-card__head,
  .adb-block-row,
  .adb-param-editor__head,
  .adb-presets__head,
  .adb-presets__bar,
  .adb-access__assign-bar,
  .adb-access__item,
  .adb-quick-add__row,
  .adb-canvas-card__main,
  .adb-canvas-card__actions,
  .adb-canvas-card__edit-grid,
  .adb-inline-rename,
  .adb-render-card__actions {
    flex-direction: column;
    align-items: stretch;
  }

  .adb-param-row {
    grid-template-columns: 1fr;
  }

  .adb-edit-tray__builder {
    min-width: 0;
  }

  .adb-builder-grid {
    grid-template-columns: 1fr;
  }

  .adb-edit-tray__sep {
    width: 100%;
    min-height: 1px;
  }
}
</style>
