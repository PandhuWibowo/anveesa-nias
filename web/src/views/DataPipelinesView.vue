<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import { VueFlow, useVueFlow, Position, type Node, type Edge, type Styles } from '@vue-flow/core'
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import '@vue-flow/controls/dist/style.css'
import { usePipelines, type Pipeline, type PipelineRun, type PipelineRunLog } from '@/composables/usePipelines'
import { useConnections } from '@/composables/useConnections'
import { useToast } from '@/composables/useToast'
type ToastType = 'success' | 'error' | 'info'
type InspectorTab = 'config' | 'runs' | 'schedule' | 'docs'
type DocLanguage = 'id' | 'en'

// ── State ────────────────────────────────────────────────────────────────────

const { pipelines, loading, fetchPipelines, createPipeline, getPipeline, savePipeline, deletePipeline, triggerRun, rerunRun, fetchRuns, fetchRunStatus, fetchRunLogs } = usePipelines()
const { connections, fetchConnections } = useConnections()
const toast = useToast()
function showToast(msg: string, type: ToastType = 'info') {
  toast[type]?.(msg)
}

const view = ref<'list' | 'canvas'>('list')
const currentPipeline = ref<Pipeline | null>(null)
const savingPipeline = ref(false)
const running = ref(false)
const showCreateModal = ref(false)
const showDeleteModal = ref(false)
const pipelineToDelete = ref<Pipeline | null>(null)
const newPipelineName = ref('')
const newPipelineDesc = ref('')
const newPipelineType = ref('replication')
const runBusinessDate = ref(new Date().toISOString().slice(0, 10))
const runParamsText = ref('{\n  "batch_id": "manual"\n}')
const catalogSearch = ref('')
const inspectorTab = ref<InspectorTab>('config')
const docLanguage = ref<DocLanguage>('id')
const showDashboardDocs = ref(false)
const showCatalogPanel = ref(true)
const showBuildGuide = ref(true)
const showInspectorPanel = ref(true)
const inspectorWidth = ref(430)
const inspectorResizing = ref(false)

// canvas nodes/edges (vue-flow format)
const nodes = ref<Node[]>([])
const edges = ref<Edge[]>([])

// selected node for config panel
const selectedNode = ref<Node | null>(null)

// run history
const runs = ref<PipelineRun[]>([])
const runLogs = ref<PipelineRunLog[]>([])
const selectedRunId = ref<number | null>(null)
const pollingInterval = ref<ReturnType<typeof setInterval> | null>(null)

// vue-flow instance
const { onNodeClick, onConnect, addEdges, setNodes, setEdges, screenToFlowCoordinate, nodes: vfNodes, edges: vfEdges } = useVueFlow()

// ── Node type definitions ─────────────────────────────────────────────────────

const NODE_TYPES = [
  {
    type: 'source_query',
    label: 'Read: SQL Query',
    color: 'var(--brand)',
    section: 'Source',
    badge: 'SQL',
    description: 'Custom query extraction',
    description_id: 'Ambil data dari database memakai SQL yang kamu tulis sendiri.',
    description_en: 'Read data from a database using your own SQL.',
  },
  {
    type: 'source_table',
    label: 'Read: Table',
    color: 'var(--brand)',
    section: 'Source',
    badge: 'DB',
    description: 'Table replication source',
    description_id: 'Ambil isi table database tanpa menulis query.',
    description_en: 'Read a database table without writing a query.',
  },
  {
    type: 'transform_sql',
    label: 'Change: SQL',
    color: 'var(--warning)',
    section: 'Transform',
    badge: 'SQL',
    description: 'Idempotent SQL transform',
    description_id: 'Ubah, filter, gabung, atau agregasi data memakai SQL.',
    description_en: 'Filter, join, aggregate, or reshape data with SQL.',
  },
  {
    type: 'external_http',
    label: 'Call: HTTP API',
    color: '#6c9eff',
    section: 'External',
    badge: 'API',
    description: 'Call external services',
    description_id: 'Kirim data ke service lain atau ambil hasil dari API.',
    description_en: 'Send data to another service or fetch results from an API.',
  },
  {
    type: 'sink_table',
    label: 'Save: Table',
    color: 'var(--success)',
    section: 'Sink',
    badge: 'DB',
    description: 'Warehouse table sink',
    description_id: 'Simpan hasil pipeline ke table database atau warehouse.',
    description_en: 'Save pipeline results into a database or warehouse table.',
  },
  {
    type: 'sink_object_storage',
    label: 'Export: Object Storage',
    color: '#8f8ac8',
    section: 'Sink',
    badge: 'S3',
    description: 'Object storage export',
    description_id: 'Export hasil pipeline menjadi file di object storage.',
    description_en: 'Export pipeline results as files in object storage.',
  },
]

const CATALOG_SECTIONS = ['Source', 'Transform', 'External', 'Sink']

const DOC_LANGUAGE_OPTIONS: Array<{ value: DocLanguage; label: string }> = [
  { value: 'id', label: 'ID' },
  { value: 'en', label: 'EN' },
]

const PIPELINE_TYPES = [
  { value: 'replication', label: 'Source DB Replication' },
  { value: 'migration', label: 'Cross-platform DB Migration' },
  { value: 'warehouse_bi', label: 'Warehouse / BI Reporting' },
  { value: 'udp_lakehouse', label: 'UDP / Data Lakehouse' },
  { value: 'ai_ml', label: 'AI / ML Dataset Pipeline' },
  { value: 'custom', label: 'Custom ETL' },
]

const PIPELINE_DOC_STEPS = [
  {
    title: '1. Siapkan connection',
    body: 'Buat koneksi database atau object storage di Connections. Source dan sink akan memakai connection yang sama supaya credential tidak disimpan ulang di pipeline.',
  },
  {
    title: '2. Susun DAG',
    body: 'Drag operator dari catalog ke canvas, lalu hubungkan handle kanan ke handle kiri task berikutnya. Urutan edge menentukan dependency eksekusi di NIAS FlowGrid runtime.',
  },
  {
    title: '3. Konfigurasi task',
    body: 'Klik node untuk memilih connection, query, table, write mode, HTTP URL, atau target object storage. Gunakan transform SQL untuk business logic.',
  },
  {
    title: '4. Run, rerun, atau backdate',
    body: 'Isi business date dan params JSON, lalu Run. Untuk reprocess tanggal lama, ganti business date. Untuk error recovery, buka Runs dan klik Re-run.',
  },
]

const PIPELINE_DOC_SECTIONS = [
  {
    title: 'Untuk apa NIAS FlowGrid?',
    items: [
      'Replikasi source DB ke warehouse atau lakehouse.',
      'Migrasi data antar platform, misalnya PostgreSQL ke MySQL atau object storage.',
      'Membangun data warehouse, BI reporting, dan dataset analitik.',
      'Membuat unified data platform untuk data lake, lakehouse, AI, dan ML.',
      'Menjalankan transformasi idempotent dengan parameter dan return payload.',
    ],
  },
  {
    title: 'Konsep utama',
    items: [
      'DAG adalah graph task yang berjalan searah tanpa cycle.',
      'Source membaca data dari database atau query.',
      'Transform mengubah data dengan SQL dan dapat memakai business date serta params.',
      'Hook HTTP mengirim atau menerima payload dari service eksternal.',
      'Sink menulis hasil ke table atau object storage.',
      'Run history menyimpan status, log per task, row count, error, dan payload.',
    ],
  },
  {
    title: 'Parameter runtime',
    items: [
      'business_date dipakai untuk backdate, partition, dan idempotency.',
      'params JSON dipakai untuk batch_id, tenant, region, atau filter dinamis.',
      'Template yang tersedia: {{business_date}}, {{params.batch_id}}, dan {{payload.Node_Label.rows}}.',
      'Sink table bisa dibuat idempotent dengan write mode replace atau pre-SQL.',
    ],
  },
]

const PIPELINE_USE_CASES = [
  {
    title: 'Daily DB replication',
    goal: 'Copy table transaksi dari OLTP ke warehouse setiap hari.',
    graph: 'Source: Table -> Sink: Table',
    config: 'Source table orders, sink table warehouse_orders, write mode pre_sql.',
    example: "DELETE FROM warehouse_orders WHERE business_date = '{{business_date}}'",
  },
  {
    title: 'Warehouse BI mart',
    goal: 'Bangun table agregasi untuk dashboard sales dan reporting.',
    graph: 'Source: Query -> Transform: SQL -> Sink: Table',
    config: 'Transform SQL menghitung total order, GMV, dan customer aktif per business_date.',
    example: "SELECT '{{business_date}}' AS business_date, COUNT(*) AS total_orders FROM orders",
  },
  {
    title: 'API enrichment',
    goal: 'Ambil data dari DB, panggil service eksternal, lalu simpan hasilnya.',
    graph: 'Source: Query -> Hook: HTTP / API -> Sink: Table',
    config: 'HTTP body mengirim business_date, batch_id, dan row count dari source.',
    example: '{"business_date":"{{business_date}}","batch":"{{params.batch_id}}"}',
  },
  {
    title: 'Lakehouse export',
    goal: 'Export dataset harian ke object storage untuk downstream AI/ML.',
    graph: 'Source: Query -> Transform: SQL -> Sink: Object Storage',
    config: 'Sink object storage format CSV atau SQL, subfolder exports/daily.',
    example: 'exports/daily/business_date={{business_date}}/',
  },
]

const PIPELINE_DOC_STEPS_EN = [
  {
    title: '1. Prepare connections',
    body: 'Create database or object storage connections in Connections. Source and sink tasks reuse those credentials so secrets are not duplicated inside the pipeline.',
  },
  {
    title: '2. Build the DAG',
    body: 'Drag operators from the catalog to the canvas, then connect the right handle to the next task. Edges define execution dependencies in the NIAS FlowGrid runtime.',
  },
  {
    title: '3. Configure tasks',
    body: 'Click a node to set its connection, query, table, write mode, HTTP URL, or object storage target. Use SQL transforms for business logic.',
  },
  {
    title: '4. Run, rerun, or backdate',
    body: 'Set the business date and params JSON, then run the DAG. Change the business date to reprocess older partitions. Open Runs and click Re-run for recovery.',
  },
]

const PIPELINE_DOC_SECTIONS_EN = [
  {
    title: 'What is NIAS FlowGrid for?',
    items: [
      'Replicate source databases to a warehouse or lakehouse.',
      'Migrate data across platforms, for example PostgreSQL to MySQL or object storage.',
      'Build data warehouses, BI reporting marts, and analytics datasets.',
      'Create a unified data platform for data lakes, lakehouses, AI, and ML.',
      'Run idempotent transformations with input parameters and return payloads.',
    ],
  },
  {
    title: 'Core concepts',
    items: [
      'A DAG is a directed graph of tasks with no cycles.',
      'Sources read data from tables or custom queries.',
      'Transforms modify data with SQL and can use business date and params.',
      'HTTP hooks send or receive payloads from external services.',
      'Sinks write results to database tables or object storage.',
      'Run history stores status, task logs, row counts, errors, and payloads.',
    ],
  },
  {
    title: 'Runtime parameters',
    items: [
      'business_date is used for backdating, partitions, and idempotency.',
      'params JSON is used for batch_id, tenant, region, or dynamic filters.',
      'Available templates: {{business_date}}, {{params.batch_id}}, and {{payload.Node_Label.rows}}.',
      'Table sinks can be made idempotent with replace mode or pre-SQL.',
    ],
  },
]

const PIPELINE_USE_CASES_EN = [
  {
    title: 'Daily DB replication',
    goal: 'Copy transactional tables from OLTP systems into the warehouse every day.',
    graph: 'Source: Table -> Sink: Table',
    config: 'Source table orders, sink table warehouse_orders, write mode pre_sql.',
    example: "DELETE FROM warehouse_orders WHERE business_date = '{{business_date}}'",
  },
  {
    title: 'Warehouse BI mart',
    goal: 'Build aggregated tables for sales dashboards and reporting.',
    graph: 'Source: Query -> Transform: SQL -> Sink: Table',
    config: 'Transform SQL calculates total orders, GMV, and active customers per business_date.',
    example: "SELECT '{{business_date}}' AS business_date, COUNT(*) AS total_orders FROM orders",
  },
  {
    title: 'API enrichment',
    goal: 'Read data from a database, call an external service, then persist the enriched result.',
    graph: 'Source: Query -> Hook: HTTP / API -> Sink: Table',
    config: 'HTTP body sends business_date, batch_id, and the source row count.',
    example: '{"business_date":"{{business_date}}","batch":"{{params.batch_id}}"}',
  },
  {
    title: 'Lakehouse export',
    goal: 'Export daily datasets to object storage for downstream AI/ML workloads.',
    graph: 'Source: Query -> Transform: SQL -> Sink: Object Storage',
    config: 'Object storage sink writes CSV or SQL files under exports/daily.',
    example: 'exports/daily/business_date={{business_date}}/',
  },
]

const PIPELINE_DOC_COPY = {
  id: {
    guideKicker: 'Panduan',
    heroTitle: 'NIAS FlowGrid: connector, DAG, dan runtime data terpadu',
    heroBody: 'NIAS FlowGrid dipakai untuk memindahkan, mentransformasi, menjadwalkan, memonitor, dan me-reprocess data dari source ke warehouse, lakehouse, reporting mart, API, atau dataset AI/ML.',
    quickCards: [
      { title: 'Connector', body: 'Source dan sink memakai connection yang sudah ada.' },
      { title: 'DAG', body: 'Task berjalan sesuai dependency dari kiri ke kanan.' },
      { title: 'Backdate', body: 'Run ulang tanggal lama dengan business date.' },
      { title: 'Monitoring', body: 'Lihat status, rows, error, dan log per run.' },
    ],
    useCaseKicker: 'Use cases',
    useCaseTitle: 'Pola ETL umum',
    docsTitle: 'Dokumentasi NIAS FlowGrid',
    docsSubtitle: 'Cara mendesain, menjalankan, dan memonitor data flow',
    quickStartTitle: 'Mulai cepat',
    operatorReferenceTitle: 'Referensi operator',
    useCaseExamplesTitle: 'Contoh use case',
  },
  en: {
    guideKicker: 'Guide',
    heroTitle: 'NIAS FlowGrid: unified connectors, DAGs, and data runtime',
    heroBody: 'NIAS FlowGrid helps move, transform, schedule, monitor, and reprocess data from sources into warehouses, lakehouses, reporting marts, APIs, or AI/ML datasets.',
    quickCards: [
      { title: 'Connector', body: 'Sources and sinks reuse existing connections.' },
      { title: 'DAG', body: 'Tasks run from left to right based on dependencies.' },
      { title: 'Backdate', body: 'Reprocess older dates with business date.' },
      { title: 'Monitoring', body: 'Review status, rows, errors, and run logs.' },
    ],
    useCaseKicker: 'Use cases',
    useCaseTitle: 'Common ETL patterns',
    docsTitle: 'NIAS FlowGrid Docs',
    docsSubtitle: 'How to design, run, and monitor data flows',
    quickStartTitle: 'Quick start',
    operatorReferenceTitle: 'Operator reference',
    useCaseExamplesTitle: 'Use case examples',
  },
}

const BEGINNER_FLOW_STEPS = {
  id: [
    { title: '1. Ambil data', body: 'Pilih Read block untuk membaca table atau query.' },
    { title: '2. Ubah data', body: 'Pakai Change block kalau perlu filter, join, atau agregasi.' },
    { title: '3. Simpan hasil', body: 'Pakai Save atau Export block untuk output akhir.' },
  ],
  en: [
    { title: '1. Read data', body: 'Pick a Read block to load a table or query.' },
    { title: '2. Change data', body: 'Use a Change block to filter, join, or aggregate.' },
    { title: '3. Save results', body: 'Use Save or Export as the final output.' },
  ],
}

// relational connections only
const relationalConnections = computed(() =>
  connections.value.filter(c => ['postgres', 'mysql', 'mariadb', 'mssql', 'sqlite'].includes(c.driver))
)

// object storage connections only
const objectStorageConnections = computed(() =>
  connections.value.filter(c => ['s3_aws', 's3_gcp', 's3_oss', 's3_obs'].includes(c.driver))
)

const docCopy = computed(() => PIPELINE_DOC_COPY[docLanguage.value])
const pipelineDocSteps = computed(() => docLanguage.value === 'id' ? PIPELINE_DOC_STEPS : PIPELINE_DOC_STEPS_EN)
const pipelineDocSections = computed(() => docLanguage.value === 'id' ? PIPELINE_DOC_SECTIONS : PIPELINE_DOC_SECTIONS_EN)
const pipelineUseCases = computed(() => docLanguage.value === 'id' ? PIPELINE_USE_CASES : PIPELINE_USE_CASES_EN)
const beginnerFlowSteps = computed(() => BEGINNER_FLOW_STEPS[docLanguage.value])
const hasFlowNodes = computed(() => (nodes.value as Node[]).length > 0)

const filteredCatalogSections = computed(() => {
  const q = catalogSearch.value.trim().toLowerCase()
  return CATALOG_SECTIONS
    .map(section => ({
      section,
      items: NODE_TYPES.filter(n =>
        n.section === section &&
        (!q || `${n.label} ${n.description} ${n.description_id} ${n.description_en} ${n.badge}`.toLowerCase().includes(q))
      ),
    }))
    .filter(group => group.items.length > 0)
})

const pipelineSummary = computed(() => {
  const nodeList = nodes.value as Node[]
  return {
    sources: nodeList.filter(n => String(n.data?.nodeType ?? '').startsWith('source')).length,
    transforms: nodeList.filter(n => String(n.data?.nodeType ?? '').startsWith('transform')).length,
    sinks: nodeList.filter(n => String(n.data?.nodeType ?? '').startsWith('sink')).length,
    hooks: nodeList.filter(n => n.data?.nodeType === 'external_http').length,
    edges: (edges.value as Edge[]).length,
  }
})

const latestRun = computed(() => runs.value[0] ?? null)
const selectedRun = computed(() => runs.value.find(r => r.id === selectedRunId.value) ?? latestRun.value)
const selectedNodeMeta = computed(() =>
  selectedNode.value ? NODE_TYPES.find(n => n.type === selectedNode.value?.data.nodeType) : null
)

const pipelineNameModel = computed({
  get: () => currentPipeline.value?.name ?? '',
  set: value => { if (currentPipeline.value) currentPipeline.value.name = value },
})

const pipelineDescriptionModel = computed({
  get: () => currentPipeline.value?.description ?? '',
  set: value => { if (currentPipeline.value) currentPipeline.value.description = value },
})

const pipelineTypeModel = computed({
  get: () => currentPipeline.value?.pipeline_type ?? 'custom',
  set: value => { if (currentPipeline.value) currentPipeline.value.pipeline_type = value },
})

const pipelineStatusModel = computed({
  get: () => currentPipeline.value?.status ?? 'draft',
  set: value => { if (currentPipeline.value) currentPipeline.value.status = value },
})

const pipelineScheduleModel = computed({
  get: () => currentPipeline.value?.schedule ?? '',
  set: value => { if (currentPipeline.value) currentPipeline.value.schedule = value.trim() || null },
})

const pipelineApiEnabledModel = computed({
  get: () => currentPipeline.value?.api_enabled ?? false,
  set: value => { if (currentPipeline.value) currentPipeline.value.api_enabled = value },
})

// ── Canvas helpers ────────────────────────────────────────────────────────────

function nodeLabel(type: string) {
  return NODE_TYPES.find(n => n.type === type)?.label ?? type
}

function operatorDescription(operator: typeof NODE_TYPES[number]) {
  return docLanguage.value === 'id' ? operator.description_id : operator.description_en
}

function flowNodeSize(type: string) {
  if (type === 'source_query' || type === 'transform_sql') return { width: 280, height: 82 }
  if (type === 'external_http' || type === 'sink_object_storage') return { width: 240, height: 74 }
  return { width: 220, height: 68 }
}

function flowNodeStyle(type: string): Styles {
  const size = flowNodeSize(type)
  return {
    background: nodeColor(type),
    color: nodeTextColor(type),
    border: '1px solid rgba(255,255,255,0.14)',
    borderRadius: '6px',
    padding: type === 'source_query' || type === 'transform_sql' ? '14px 18px' : '12px 16px',
    fontWeight: '600',
    fontSize: '13px',
    lineHeight: '1.35',
    minWidth: `${size.width}px`,
    minHeight: `${size.height}px`,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    textAlign: 'center',
    whiteSpace: 'normal',
    boxShadow: 'var(--shadow-sm)',
  }
}

function flowEdgeStyle() {
  return { stroke: 'var(--text-muted)', strokeWidth: 2 }
}

function nodeColor(type: string) {
  if (type.startsWith('source')) return 'var(--brand)'
  if (type.startsWith('transform')) return 'var(--warning)'
  if (type === 'external_http') return '#6c9eff'
  if (type === 'sink_object_storage') return '#8f8ac8'
  if (type.startsWith('sink')) return 'var(--success)'
  return 'var(--brand)'
}

function nodeTextColor(type: string) {
  if (type.startsWith('source') || type.startsWith('sink_table')) return 'var(--brand-fg)'
  if (type.startsWith('transform')) return '#2a1a00'
  return '#fff'
}

function pipelineTypeLabel(type?: string) {
  return PIPELINE_TYPES.find(t => t.value === type)?.label ?? 'Custom ETL'
}

// Convert internal pipeline nodes to vue-flow nodes
function toFlowNodes(pipelineNodes: any[]): Node[] {
  return pipelineNodes.map(n => ({
    id: String(n.id),
    type: 'default',
    position: { x: n.position_x, y: n.position_y },
    label: n.label || nodeLabel(n.node_type),
    data: {
      nodeType: n.node_type,
      connectionId: n.connection_id,
      config: { ...n.config },
      label: n.label || nodeLabel(n.node_type),
    },
    style: flowNodeStyle(n.node_type),
    sourcePosition: Position.Right,
    targetPosition: Position.Left,
  }))
}

function toFlowEdges(pipelineEdges: any[]): Edge[] {
  return pipelineEdges.map(e => ({
    id: `e${e.source_node_id}-${e.target_node_id}`,
    source: String(e.source_node_id),
    target: String(e.target_node_id),
    animated: true,
    style: flowEdgeStyle(),
  }))
}

// Convert vue-flow nodes back to pipeline node format for saving
function fromFlowNodes(flowNodes: Node[], flowEdges: Edge[]): { nodes: any[]; edges: any[] } {
  // Build flow-id → temp db-id map so edges reference the same IDs as nodes
  const idMap = new Map<string, number>()
  const nodes = flowNodes.map((n, idx) => {
    const dbId = parseInt(n.id) || -(idx + 1)
    idMap.set(n.id, dbId)
    return {
      id: dbId,
      pipeline_id: currentPipeline.value?.id,
      node_type: n.data.nodeType,
      connection_id: n.data.connectionId ?? null,
      config: n.data.config ?? {},
      position_x: n.position.x,
      position_y: n.position.y,
      label: n.data.label || n.label,
    }
  })
  return {
    nodes,
    edges: flowEdges.map(e => ({
      source_node_id: idMap.get(e.source) ?? parseInt(e.source),
      target_node_id: idMap.get(e.target) ?? parseInt(e.target),
    })),
  }
}

// ── Lifecycle ─────────────────────────────────────────────────────────────────

onMounted(async () => {
  await Promise.all([fetchPipelines(), fetchConnections()])
})

onBeforeUnmount(() => {
  if (pollingInterval.value) {
    clearInterval(pollingInterval.value)
    pollingInterval.value = null
  }
  window.removeEventListener('mousemove', onInspectorResizeMove)
  window.removeEventListener('mouseup', onInspectorResizeEnd)
})

// Keep edges.value in sync with vue-flow's internal edge store.
// vfEdges is the ground truth for what's visually connected in the canvas.
watch(vfEdges, (newEdges) => {
  edges.value = newEdges as Edge[]
}, { deep: true })

// ── Actions ───────────────────────────────────────────────────────────────────

async function openCreateModal() {
  newPipelineName.value = ''
  newPipelineDesc.value = ''
  newPipelineType.value = 'replication'
  showCreateModal.value = true
}

async function confirmCreate() {
  if (!newPipelineName.value.trim()) return
  try {
    const id = await createPipeline(newPipelineName.value.trim(), newPipelineDesc.value, newPipelineType.value)
    showCreateModal.value = false
    await openPipeline(id)
  } catch (e: any) {
    showToast(e.response?.data?.error || 'Failed to create pipeline', 'error')
  }
}

async function openPipeline(id: number) {
  try {
    const p = await getPipeline(id)
    currentPipeline.value = p
    const flowNodes = toFlowNodes(p.nodes ?? [])
    const flowEdges = toFlowEdges(p.edges ?? [])
    setNodes(flowNodes)
    setEdges(flowEdges)
    nodes.value = flowNodes
    edges.value = flowEdges
    selectedNode.value = null
    inspectorTab.value = 'runs'
    runs.value = await fetchRuns(id)
    selectedRunId.value = runs.value[0]?.id ?? null
    runLogs.value = selectedRunId.value ? await fetchRunLogs(id, selectedRunId.value) : []
    view.value = 'canvas'
  } catch (e: any) {
    showToast('Failed to load pipeline', 'error')
  }
}

async function save() {
  if (!currentPipeline.value) return
  savingPipeline.value = true
  try {
    const { nodes: pNodes, edges: pEdges } = fromFlowNodes(nodes.value as unknown as Node[], edges.value as unknown as Edge[])
    // Auto-promote from draft to active once the pipeline has nodes
    const effectiveStatus =
      currentPipeline.value.status === 'draft' && pNodes.length > 0
        ? 'active'
        : currentPipeline.value.status
    if (currentPipeline.value.status === 'draft' && pNodes.length > 0) {
      currentPipeline.value.status = 'active'
    }
    await savePipeline(currentPipeline.value.id, {
      name: currentPipeline.value.name,
      description: currentPipeline.value.description,
      pipeline_type: currentPipeline.value.pipeline_type,
      status: effectiveStatus,
      schedule: currentPipeline.value.schedule,
      api_enabled: currentPipeline.value.api_enabled,
      nodes: pNodes,
      edges: pEdges,
    })
    // reload to get real DB ids
    const p = await getPipeline(currentPipeline.value.id)
    currentPipeline.value = p
    const reloadedNodes = toFlowNodes(p.nodes ?? [])
    const reloadedEdges = toFlowEdges(p.edges ?? [])
    setNodes(reloadedNodes)
    setEdges(reloadedEdges)
    nodes.value = reloadedNodes
    edges.value = reloadedEdges
    showToast('Pipeline saved', 'success')
  } catch (e: any) {
    showToast(e.response?.data?.error || 'Save failed', 'error')
  } finally {
    savingPipeline.value = false
  }
}

async function runPipeline() {
  if (!currentPipeline.value) return
  running.value = true
  try {
    // auto-save first
    await save()
    const runId = await triggerRun(currentPipeline.value.id, {
      business_date: runBusinessDate.value,
      params: parseRunParams(),
    })
    await openRun(runId)
  } catch (e: any) {
    showToast(e.response?.data?.error || e.message || 'Run failed', 'error')
    running.value = false
  }
}

function parseRunParams(): Record<string, any> {
  try {
    const parsed = JSON.parse(runParamsText.value || '{}')
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {}
  } catch {
    throw new Error('Run params must be valid JSON')
  }
}

async function openRun(runId: number) {
  selectedRunId.value = runId
  inspectorTab.value = 'runs'
  if (currentPipeline.value) {
    runs.value = await fetchRuns(currentPipeline.value.id)
    runLogs.value = await fetchRunLogs(currentPipeline.value.id, runId)
  }
  startPolling(runId)
}

async function rerunPipeline(run: PipelineRun) {
  if (!currentPipeline.value) return
  running.value = true
  try {
    const runId = await rerunRun(currentPipeline.value.id, run.id)
    await openRun(runId)
  } catch (e: any) {
    showToast(e.response?.data?.error || 'Re-run failed', 'error')
    running.value = false
  }
}

function startPolling(runId: number) {
  if (pollingInterval.value) clearInterval(pollingInterval.value)
  pollingInterval.value = setInterval(async () => {
    if (!currentPipeline.value) return
    const run = await fetchRunStatus(currentPipeline.value.id, runId)
    const idx = runs.value.findIndex(r => r.id === runId)
    if (idx >= 0) runs.value[idx] = run
    if (selectedRunId.value === runId) {
      runLogs.value = await fetchRunLogs(currentPipeline.value.id, runId)
    }
    if (run.status !== 'running') {
      clearInterval(pollingInterval.value!)
      pollingInterval.value = null
      running.value = false
      if (run.status === 'success') showToast(`Run #${runId} completed — ${run.rows_processed} rows`, 'success')
      else showToast(`Run #${runId} failed: ${run.error_message}`, 'error')
    }
  }, 1500)
}

async function viewRunLogs(run: PipelineRun) {
  selectedRunId.value = run.id
  inspectorTab.value = 'runs'
  if (!currentPipeline.value) return
  runLogs.value = await fetchRunLogs(currentPipeline.value.id, run.id)
}

function handleDelete(p: Pipeline) {
  pipelineToDelete.value = p
  showDeleteModal.value = true
}

async function confirmDelete() {
  if (!pipelineToDelete.value) return
  try {
    await deletePipeline(pipelineToDelete.value.id)
    await fetchPipelines()
  } catch {
    showToast('Delete failed', 'error')
  } finally {
    showDeleteModal.value = false
    pipelineToDelete.value = null
  }
}

function backToList() {
  if (pollingInterval.value) clearInterval(pollingInterval.value)
  view.value = 'list'
  currentPipeline.value = null
  fetchPipelines()
}

// ── Canvas interaction ────────────────────────────────────────────────────────

onNodeClick(({ node }) => {
  selectedNode.value = node
  inspectorTab.value = 'config'
})

onConnect((connection) => {
  const newEdge = {
    ...connection,
    id: `e${connection.source}-${connection.target}-${Date.now()}`,
    animated: true,
    style: flowEdgeStyle(),
  }
  addEdges([newEdge])
  edges.value = [...(edges.value as Edge[]), newEdge]
})

let dragNodeType = ''

function onDragStart(event: DragEvent, type: string) {
  dragNodeType = type
  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('application/x-nias-flow-node', type)
  }
}

function onDragEnd() {
  dragNodeType = ''
}

function onDragOver(event: DragEvent) {
  event.preventDefault()
  if (event.dataTransfer) event.dataTransfer.dropEffect = 'move'
}

function onDrop(event: DragEvent) {
  event.preventDefault()
  const type = dragNodeType || event.dataTransfer?.getData('application/x-nias-flow-node') || ''
  if (!type) return
  dragNodeType = ''
  const position = screenToFlowCoordinate({ x: event.clientX, y: event.clientY })
  const size = flowNodeSize(type)
  const id = `new-${Date.now()}`
  const label = nodeLabel(type)
  const newNode: Node = {
    id,
    type: 'default',
    position: { x: position.x - size.width / 2, y: position.y - size.height / 2 },
    label,
    data: { nodeType: type, connectionId: null, config: {}, label },
    style: flowNodeStyle(type),
    sourcePosition: Position.Right,
    targetPosition: Position.Left,
  };
  (nodes.value as Node[]).push(newNode)
}

function addStarterFlow() {
  const suffix = Date.now()
  const sourceId = `starter-source-${suffix}`
  const transformId = `starter-transform-${suffix}`
  const sinkId = `starter-sink-${suffix}`
  const starterNodes: Node[] = [
    {
      id: sourceId,
      type: 'default',
      position: { x: 90, y: 130 },
      label: docLanguage.value === 'id' ? '1. Ambil table' : '1. Read table',
      data: {
        nodeType: 'source_table',
        connectionId: null,
        label: docLanguage.value === 'id' ? '1. Ambil table' : '1. Read table',
        config: { table: 'orders', limit: 5000 },
      },
      style: flowNodeStyle('source_table'),
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
    },
    {
      id: transformId,
      type: 'default',
      position: { x: 340, y: 130 },
      label: docLanguage.value === 'id' ? '2. Rapikan data' : '2. Prepare data',
      data: {
        nodeType: 'transform_sql',
        connectionId: null,
        label: docLanguage.value === 'id' ? '2. Rapikan data' : '2. Prepare data',
        config: { sql: '' },
      },
      style: flowNodeStyle('transform_sql'),
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
    },
    {
      id: sinkId,
      type: 'default',
      position: { x: 600, y: 130 },
      label: docLanguage.value === 'id' ? '3. Simpan hasil' : '3. Save results',
      data: {
        nodeType: 'sink_table',
        connectionId: null,
        label: docLanguage.value === 'id' ? '3. Simpan hasil' : '3. Save results',
        config: {
          table: '',
          write_mode: 'append',
          pre_sql: '',
        },
      },
      style: flowNodeStyle('sink_table'),
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
    },
  ]

  const starterEdges = [
    { id: `e-${sourceId}-${transformId}`, source: sourceId, target: transformId, animated: true, style: flowEdgeStyle() },
    { id: `e-${transformId}-${sinkId}`, source: transformId, target: sinkId, animated: true, style: flowEdgeStyle() },
  ]
  setNodes(starterNodes)
  setEdges(starterEdges)
  nodes.value = starterNodes
  edges.value = starterEdges
  selectedNode.value = starterNodes[0]
  inspectorTab.value = 'config'
  showToast(docLanguage.value === 'id' ? 'Starter flow ditambahkan' : 'Starter flow added', 'info')
}

function updateSelectedNodeConfig(key: string, value: any) {
  if (!selectedNode.value) return
  selectedNode.value.data = { ...selectedNode.value.data, config: { ...selectedNode.value.data.config, [key]: value } }
  const sid = selectedNode.value.id
  const sdata = selectedNode.value.data;
  (nodes.value as Node[]).forEach((n, i) => {
    if (n.id === sid) (nodes.value as Node[])[i] = { ...n, data: sdata }
  })
}

function updateSelectedNodeField(key: string, value: any) {
  if (!selectedNode.value) return
  selectedNode.value.data = { ...selectedNode.value.data, [key]: value }
  if (key === 'label') {
    selectedNode.value.label = value
  }
  const sid = selectedNode.value.id
  const sdata = selectedNode.value.data
  const isLabel = key === 'label';
  (nodes.value as Node[]).forEach((n, i) => {
    if (n.id === sid) {
      const updated = { ...n, data: sdata } as Node
      if (isLabel) updated.label = value;
      (nodes.value as Node[])[i] = updated
    }
  })
}

function removeSelectedNode() {
  if (!selectedNode.value) return
  const id = selectedNode.value.id
  setNodes((nodes.value as Node[]).filter((n: Node) => n.id !== id))
  setEdges((edges.value as Edge[]).filter((e: Edge) => e.source !== id && e.target !== id))
  selectedNode.value = null
}

function onInspectorResizeStart(event: MouseEvent) {
  event.preventDefault()
  inspectorResizing.value = true
  window.addEventListener('mousemove', onInspectorResizeMove)
  window.addEventListener('mouseup', onInspectorResizeEnd)
}

function onInspectorResizeMove(event: MouseEvent) {
  inspectorWidth.value = Math.min(680, Math.max(360, window.innerWidth - event.clientX))
}

function onInspectorResizeEnd() {
  inspectorResizing.value = false
  window.removeEventListener('mousemove', onInspectorResizeMove)
  window.removeEventListener('mouseup', onInspectorResizeEnd)
}

// ── Format helpers ────────────────────────────────────────────────────────────

function formatDate(d: string | null) {
  if (!d) return '—'
  return new Date(d).toLocaleString()
}

function runStatusClass(status: string) {
  if (status === 'success') return 'status-success'
  if (status === 'failed') return 'status-failed'
  return 'status-running'
}
</script>

<template>
  <div class="pipelines-shell">
    <!-- ── List View ────────────────────────────────────────── -->
    <div v-if="view === 'list'" class="list-view">
      <div class="list-header">
        <h1 class="page-title">NIAS FlowGrid</h1>
        <button class="btn-primary" @click="openCreateModal">+ New Pipeline</button>
      </div>

      <section class="dashboard-doc-strip">
        <div class="doc-strip-copy">
          <span class="panel-kicker">{{ docCopy.guideKicker }}</span>
          <strong>{{ docCopy.heroTitle }}</strong>
          <p>{{ docCopy.heroBody }}</p>
        </div>
        <div class="doc-strip-actions">
          <div class="doc-language-toggle" aria-label="Documentation language">
            <button
              v-for="option in DOC_LANGUAGE_OPTIONS"
              :key="option.value"
              :class="{ active: docLanguage === option.value }"
              @click="docLanguage = option.value"
            >
              {{ option.label }}
            </button>
          </div>
          <button class="btn-secondary" @click="showDashboardDocs = !showDashboardDocs">
            {{ showDashboardDocs ? 'Hide docs' : 'Show docs' }}
          </button>
        </div>
      </section>

      <div v-if="loading" class="empty-state">Loading…</div>
      <div v-else-if="pipelines.length === 0" class="empty-state">
        No pipelines yet. Create one to get started.
      </div>
      <div v-else class="pipeline-grid">
        <div v-for="p in pipelines" :key="p.id" class="pipeline-card" @click="openPipeline(p.id)">
          <div class="card-header">
            <span class="card-name">{{ p.name }}</span>
            <span :class="['card-status', `status-${p.status}`]">{{ p.status }}</span>
          </div>
          <div class="card-type">{{ pipelineTypeLabel(p.pipeline_type) }}</div>
          <div class="card-desc">{{ p.description || '—' }}</div>
          <div class="card-meta">
            Last run: {{ formatDate(p.last_run_at) }}
          </div>
          <div class="card-actions" @click.stop>
            <button class="btn-icon" @click="openPipeline(p.id)" title="Open">✎</button>
            <button class="btn-icon btn-danger" @click="handleDelete(p)" title="Delete">✕</button>
          </div>
        </div>
      </div>

      <div v-if="showDashboardDocs" class="dashboard-doc-expanded">
        <section class="dashboard-guide">
          <div class="guide-main">
            <div class="guide-heading">
              <span class="panel-kicker">{{ docCopy.guideKicker }}</span>
            </div>
            <h2>{{ docCopy.heroTitle }}</h2>
            <p>{{ docCopy.heroBody }}</p>
          </div>
          <div class="guide-quick-grid">
            <div v-for="item in docCopy.quickCards" :key="item.title" class="guide-card">
              <strong>{{ item.title }}</strong>
              <span>{{ item.body }}</span>
            </div>
          </div>
        </section>

        <section class="doc-grid">
          <article v-for="step in pipelineDocSteps" :key="step.title" class="doc-card">
            <h3>{{ step.title }}</h3>
            <p>{{ step.body }}</p>
          </article>
        </section>

        <section class="usecase-panel">
          <div class="section-head">
            <div>
              <span class="panel-kicker">{{ docCopy.useCaseKicker }}</span>
              <h2>{{ docCopy.useCaseTitle }}</h2>
            </div>
          </div>
          <div class="usecase-grid">
            <article v-for="item in pipelineUseCases" :key="item.title" class="usecase-card">
              <h3>{{ item.title }}</h3>
              <p>{{ item.goal }}</p>
              <div class="doc-flow">{{ item.graph }}</div>
              <span>{{ item.config }}</span>
              <code>{{ item.example }}</code>
            </article>
          </div>
        </section>
      </div>
    </div>

    <!-- ── Canvas View ─────────────────────────────────────── -->
    <div v-else class="canvas-view">
      <!-- Top bar -->
      <div class="canvas-topbar">
        <button class="btn-ghost" @click="backToList">← Pipelines</button>
        <div class="pipeline-title">
          <span class="pipeline-name">{{ currentPipeline?.name }}</span>
          <span class="pipeline-type">{{ pipelineTypeLabel(currentPipeline?.pipeline_type) }}</span>
        </div>
        <div class="pipeline-health">
          <span class="metric-pill"><strong>{{ pipelineSummary.sources }}</strong> sources</span>
          <span class="metric-pill"><strong>{{ pipelineSummary.transforms }}</strong> transforms</span>
          <span class="metric-pill"><strong>{{ pipelineSummary.sinks }}</strong> sinks</span>
          <span class="metric-pill"><strong>{{ pipelineSummary.edges }}</strong> edges</span>
          <span v-if="latestRun" class="metric-pill">
            <span :class="['run-status-dot', runStatusClass(latestRun.status)]"></span>
            #{{ latestRun.id }}
          </span>
        </div>
        <div class="topbar-actions">
          <div class="panel-toggle-group" aria-label="Toggle panels">
            <button :class="{ active: showCatalogPanel }" @click="showCatalogPanel = !showCatalogPanel">Catalog</button>
            <button :class="{ active: showBuildGuide }" @click="showBuildGuide = !showBuildGuide">Guide</button>
            <button :class="{ active: showInspectorPanel }" @click="showInspectorPanel = !showInspectorPanel">Inspector</button>
          </div>
          <button class="btn-secondary" :disabled="savingPipeline" @click="save">
            {{ savingPipeline ? 'Saving…' : 'Save' }}
          </button>
          <button class="btn-primary" :disabled="running" @click="runPipeline">
            {{ running ? '⏳ Running…' : '▶ Run / Backdate' }}
          </button>
          <button class="btn-ghost" @click="inspectorTab = 'runs'">
            History ({{ runs.length }})
          </button>
          <button class="btn-ghost" @click="inspectorTab = 'schedule'">Schedule</button>
          <button class="btn-ghost" @click="inspectorTab = 'docs'">Docs</button>
        </div>
      </div>

      <!-- Main area -->
      <div class="canvas-main">
        <!-- Node palette -->
        <div v-if="showCatalogPanel" class="node-palette">
          <div class="catalog-header">
            <div>
              <div class="panel-kicker">{{ docLanguage === 'id' ? 'Pilih blok' : 'Choose blocks' }}</div>
              <div class="panel-title">{{ docLanguage === 'id' ? 'Blok Pipeline' : 'Pipeline Blocks' }}</div>
            </div>
            <span class="catalog-total">{{ NODE_TYPES.length }}</span>
          </div>
          <input
            v-model="catalogSearch"
            class="catalog-search"
            :placeholder="docLanguage === 'id' ? 'Cari blok' : 'Search blocks'"
            spellcheck="false"
          />
          <div class="catalog-help">
            {{ docLanguage === 'id' ? 'Drag blok ke canvas. Mulai dari Read, lanjut Change, akhiri Save atau Export.' : 'Drag blocks to the canvas. Start with Read, then Change, then Save or Export.' }}
          </div>
          <div class="catalog-stats">
            <span>{{ relationalConnections.length }} DB</span>
            <span>{{ objectStorageConnections.length }} object</span>
          </div>
          <div class="palette-section" v-for="group in filteredCatalogSections" :key="group.section">
            <div class="palette-label">{{ group.section }}</div>
            <div
              v-for="nt in group.items"
              :key="nt.type"
              class="palette-item"
              :style="{ borderLeftColor: nt.color }"
              draggable="true"
              @dragstart="onDragStart($event, nt.type)"
              @dragend="onDragEnd"
            >
              <div class="palette-row">
                <span>{{ nt.label }}</span>
                <span class="node-badge">{{ nt.badge }}</span>
              </div>
              <span class="palette-desc">{{ operatorDescription(nt) }}</span>
            </div>
          </div>
          <div v-if="filteredCatalogSections.length === 0" class="palette-hint">No operators found.</div>
        </div>
        <button
          v-else
          class="panel-restore panel-restore--left"
          @click="showCatalogPanel = true"
        >
          Catalog
        </button>

        <!-- Canvas -->
        <div class="dag-workspace">
          <div class="dag-toolbar">
            <div class="dag-title">
              <span class="panel-kicker">DAG</span>
              <strong>{{ pipelineSummary.sources + pipelineSummary.transforms + pipelineSummary.hooks + pipelineSummary.sinks }} tasks</strong>
            </div>
            <div v-if="showBuildGuide" class="build-guide">
              <span v-for="step in beginnerFlowSteps" :key="step.title">
                <strong>{{ step.title }}</strong>
                <em>{{ step.body }}</em>
              </span>
            </div>
            <button v-else class="panel-restore panel-restore--inline" @click="showBuildGuide = true">
              {{ docLanguage === 'id' ? 'Show guide' : 'Show guide' }}
            </button>
            <div class="run-controls">
              <label>
                Business date
                <input v-model="runBusinessDate" type="date" />
              </label>
              <label>
                Params JSON
                <textarea v-model="runParamsText" rows="2" spellcheck="false" />
              </label>
            </div>
          </div>
          <div class="flow-container" @dragover="onDragOver" @drop="onDrop">
            <VueFlow
              v-model:nodes="nodes"
              v-model:edges="edges"
              fit-view-on-init
              :default-viewport="{ zoom: 1 }"
              :min-zoom="0.3"
              :max-zoom="2"
              :nodes-draggable="true"
              :nodes-connectable="true"
              :auto-pan-on-node-drag="true"
              :auto-pan-on-connect="true"
              :node-drag-threshold="1"
            >
              <Background variant="dots" :gap="20" :size="1" />
              <Controls />
            </VueFlow>
            <div v-if="!hasFlowNodes" class="empty-canvas-guide">
              <strong>{{ docLanguage === 'id' ? 'Belum ada flow' : 'No flow yet' }}</strong>
              <span>
                {{ docLanguage === 'id'
                  ? 'Klik starter flow untuk membuat contoh: ambil data, ubah data, lalu simpan hasil.'
                  : 'Add a starter flow to create an example: read data, change it, then save the result.' }}
              </span>
              <button class="btn-primary" @click.stop="addStarterFlow">
                {{ docLanguage === 'id' ? 'Tambah starter flow' : 'Add starter flow' }}
              </button>
            </div>
          </div>
        </div>

        <!-- Config panel -->
        <div
          v-if="showInspectorPanel"
          class="config-panel inspector-panel"
          :class="{ 'is-resizing': inspectorResizing }"
          :style="{ '--inspector-width': `${inspectorWidth}px` }"
        >
          <div class="inspector-resize-handle" @mousedown="onInspectorResizeStart" />
          <div class="inspector-tabs">
            <button :class="{ active: inspectorTab === 'config' }" @click="inspectorTab = 'config'">Config</button>
            <button :class="{ active: inspectorTab === 'runs' }" @click="inspectorTab = 'runs'">Runs</button>
            <button :class="{ active: inspectorTab === 'schedule' }" @click="inspectorTab = 'schedule'">Schedule</button>
            <button :class="{ active: inspectorTab === 'docs' }" @click="inspectorTab = 'docs'">Docs</button>
          </div>

          <div v-if="inspectorTab === 'config'" class="inspector-body">
            <template v-if="selectedNode">
              <div class="config-header">
                <div>
                  <span>{{ selectedNode.data.nodeType }}</span>
                  <small v-if="selectedNodeMeta">{{ selectedNodeMeta.badge }} · {{ selectedNodeMeta.section }}</small>
                </div>
                <button class="btn-icon btn-danger" @click="removeSelectedNode" title="Remove node">✕</button>
              </div>

              <div class="config-field">
                <label>Label</label>
                <input
                  :value="selectedNode.data.label"
                  @input="updateSelectedNodeField('label', ($event.target as HTMLInputElement).value)"
                />
              </div>

          <!-- Connection selector — relational for source/transform/sink_table nodes -->
          <div v-if="selectedNode.data.nodeType !== 'external_http' && selectedNode.data.nodeType !== 'sink_object_storage'" class="config-field">
            <label>Connection</label>
            <select
              :value="selectedNode.data.connectionId ?? ''"
              @change="updateSelectedNodeField('connectionId', parseInt(($event.target as HTMLSelectElement).value) || null)"
            >
              <option value="">— select —</option>
              <option v-for="c in relationalConnections" :key="c.id" :value="c.id">
                {{ c.name }} ({{ c.driver }})
              </option>
            </select>
          </div>

          <!-- Connection selector — object storage for sink_object_storage -->
          <div v-if="selectedNode.data.nodeType === 'sink_object_storage'" class="config-field">
            <label>Object Storage Connection</label>
            <select
              :value="selectedNode.data.connectionId ?? ''"
              @change="updateSelectedNodeField('connectionId', parseInt(($event.target as HTMLSelectElement).value) || null)"
            >
              <option value="">— select —</option>
              <option v-for="c in objectStorageConnections" :key="c.id" :value="c.id">
                {{ c.name }} ({{ c.driver }})
              </option>
            </select>
          </div>

          <!-- source_query config -->
          <template v-if="selectedNode.data.nodeType === 'source_query'">
            <div class="config-field">
              <label>SQL Query</label>
              <textarea
                class="query-textarea"
                :value="selectedNode.data.config.sql ?? ''"
                rows="12"
                placeholder="SELECT * FROM orders WHERE status = 'pending'"
                @input="updateSelectedNodeConfig('sql', ($event.target as HTMLTextAreaElement).value)"
              />
            </div>
            <div class="config-field">
              <label>Row Limit (optional)</label>
              <input
                type="number"
                :value="selectedNode.data.config.limit ?? ''"
                placeholder="500000"
                @input="updateSelectedNodeConfig('limit', parseInt(($event.target as HTMLInputElement).value) || undefined)"
              />
            </div>
          </template>

          <!-- source_table config -->
          <template v-if="selectedNode.data.nodeType === 'source_table'">
            <div class="config-field">
              <label>Table Name</label>
              <input
                :value="selectedNode.data.config.table ?? ''"
                placeholder="orders"
                @input="updateSelectedNodeConfig('table', ($event.target as HTMLInputElement).value)"
              />
            </div>
            <div class="config-field">
              <label>Schema (optional)</label>
              <input
                :value="selectedNode.data.config.schema ?? ''"
                placeholder="public"
                @input="updateSelectedNodeConfig('schema', ($event.target as HTMLInputElement).value)"
              />
            </div>
            <div class="config-field">
              <label>Row Limit (optional)</label>
              <input
                type="number"
                :value="selectedNode.data.config.limit ?? ''"
                placeholder="500000"
                @input="updateSelectedNodeConfig('limit', parseInt(($event.target as HTMLInputElement).value) || undefined)"
              />
            </div>
          </template>

          <!-- sink_table config -->
          <template v-if="selectedNode.data.nodeType === 'sink_table'">
            <div class="config-field">
              <label>Table Name</label>
              <input
                :value="selectedNode.data.config.table ?? ''"
                placeholder="warehouse_orders"
                @input="updateSelectedNodeConfig('table', ($event.target as HTMLInputElement).value)"
              />
            </div>
            <div class="config-field">
              <label>Schema (optional)</label>
              <input
                :value="selectedNode.data.config.schema ?? ''"
                placeholder="public"
                @input="updateSelectedNodeConfig('schema', ($event.target as HTMLInputElement).value)"
              />
            </div>
            <div class="config-field">
              <label>Write Mode</label>
              <select
                :value="selectedNode.data.config.write_mode ?? 'append'"
                @change="updateSelectedNodeConfig('write_mode', ($event.target as HTMLSelectElement).value)"
              >
                <option value="append">Append</option>
                <option value="replace">Replace table data before insert</option>
                <option value="pre_sql">Run pre-SQL then insert</option>
              </select>
            </div>
            <div class="config-field">
              <label>Pre-SQL for idempotency (optional)</label>
              <textarea
                class="query-textarea query-textarea--compact"
                :value="selectedNode.data.config.pre_sql ?? ''"
                rows="8"
                placeholder="DELETE FROM warehouse_orders WHERE business_date = '{{business_date}}'"
                @input="updateSelectedNodeConfig('pre_sql', ($event.target as HTMLTextAreaElement).value)"
              />
            </div>
          </template>

          <!-- transform_sql config -->
          <template v-if="selectedNode.data.nodeType === 'transform_sql'">
            <div class="config-field">
              <label>Transform SQL</label>
              <textarea
                class="query-textarea"
                :value="selectedNode.data.config.sql ?? ''"
                rows="12"
                placeholder="SELECT '{{business_date}}' AS business_date, COUNT(*) AS total_orders"
                @input="updateSelectedNodeConfig('sql', ($event.target as HTMLTextAreaElement).value)"
              />
            </div>
            <div class="config-hint">
              Templates: <code v-pre>{{business_date}}</code>, <code v-pre>{{params.batch_id}}</code>, <code v-pre>{{payload.Node_Label.rows}}</code>. Empty SQL passes upstream rows through.
            </div>
          </template>

          <!-- external_http config -->
          <template v-if="selectedNode.data.nodeType === 'external_http'">
            <div class="config-field">
              <label>Method</label>
              <select
                :value="selectedNode.data.config.method ?? 'POST'"
                @change="updateSelectedNodeConfig('method', ($event.target as HTMLSelectElement).value)"
              >
                <option value="GET">GET</option>
                <option value="POST">POST</option>
                <option value="PUT">PUT</option>
                <option value="PATCH">PATCH</option>
              </select>
            </div>
            <div class="config-field">
              <label>URL</label>
              <input
                :value="selectedNode.data.config.url ?? ''"
                placeholder="https://example.com/api/jobs/{{params.batch_id}}"
                @input="updateSelectedNodeConfig('url', ($event.target as HTMLInputElement).value)"
              />
            </div>
            <div class="config-field">
              <label>JSON Body (optional)</label>
              <textarea
                class="query-textarea query-textarea--compact"
                :value="selectedNode.data.config.body ?? ''"
                rows="8"
                placeholder='{"business_date":"{{business_date}}","rows":"{{payload.Node_Label.rows}}"}'
                @input="updateSelectedNodeConfig('body', ($event.target as HTMLTextAreaElement).value)"
              />
            </div>
          </template>

          <!-- sink_object_storage config -->
          <template v-if="selectedNode.data.nodeType === 'sink_object_storage'">
            <div class="config-field">
              <label>Format</label>
              <select
                :value="selectedNode.data.config.format ?? 'csv'"
                @change="updateSelectedNodeConfig('format', ($event.target as HTMLSelectElement).value)"
              >
                <option value="csv">CSV</option>
                <option value="sql">SQL (INSERT statements)</option>
              </select>
            </div>
            <div class="config-field">
              <label>Filename Prefix (optional)</label>
              <input
                :value="selectedNode.data.config.filename_prefix ?? ''"
                placeholder="export"
                @input="updateSelectedNodeConfig('filename_prefix', ($event.target as HTMLInputElement).value)"
              />
            </div>
            <div class="config-field">
              <label>Subfolder in Bucket (optional)</label>
              <input
                :value="selectedNode.data.config.subfolder ?? ''"
                placeholder="exports/daily"
                @input="updateSelectedNodeConfig('subfolder', ($event.target as HTMLInputElement).value)"
              />
            </div>
            <div v-if="selectedNode.data.config.format === 'sql'" class="config-field">
              <label>Table Name (for INSERT)</label>
              <input
                :value="selectedNode.data.config.table_name ?? ''"
                placeholder="exported_table"
                @input="updateSelectedNodeConfig('table_name', ($event.target as HTMLInputElement).value)"
              />
            </div>
          </template>

              <div class="config-hint">
                Use <code v-pre>{{business_date}}</code> and <code v-pre>{{params.batch_id}}</code> in SQL, URLs, and payloads.
              </div>

              <button class="btn-remove-node" @click="removeSelectedNode">Remove Node</button>
            </template>

            <div v-else class="config-empty inspector-empty">
              <div class="empty-title">Pipeline Overview</div>
              <div class="overview-grid">
                <span><strong>{{ pipelineSummary.sources }}</strong> sources</span>
                <span><strong>{{ pipelineSummary.transforms }}</strong> transforms</span>
                <span><strong>{{ pipelineSummary.hooks }}</strong> hooks</span>
                <span><strong>{{ pipelineSummary.sinks }}</strong> sinks</span>
              </div>
            </div>
          </div>

          <div v-else-if="inspectorTab === 'runs'" class="inspector-body runs-inspector">
            <div class="runs-header">
              <div>
                <span class="panel-kicker">Runs</span>
                <strong>{{ runs.length }} executions</strong>
              </div>
              <button class="btn-secondary" :disabled="running" @click="runPipeline">
                {{ running ? 'Running…' : 'Run' }}
              </button>
            </div>

            <div v-if="runs.length === 0" class="empty-state small">No runs yet.</div>
            <div v-else class="run-list-full">
              <div
                v-for="run in runs"
                :key="run.id"
                :class="['run-row', { active: selectedRunId === run.id }]"
                @click="viewRunLogs(run)"
              >
                <span :class="['run-status-dot', runStatusClass(run.status)]"></span>
                <span class="run-id">#{{ run.id }}</span>
                <span class="run-info">{{ formatDate(run.started_at) }}</span>
                <span v-if="run.business_date" class="run-business-date">{{ run.business_date }}</span>
                <span class="run-rows">{{ run.rows_processed }} rows</span>
                <button class="btn-mini" :disabled="running" @click.stop="rerunPipeline(run)">Re-run</button>
              </div>
            </div>

            <div v-if="selectedRun" class="selected-run-card">
              <div class="selected-run-head">
                <span :class="['run-status-dot', runStatusClass(selectedRun.status)]"></span>
                <strong>Run #{{ selectedRun.id }}</strong>
                <span>{{ selectedRun.triggered_by }}</span>
              </div>
              <div class="selected-run-meta">
                <span>{{ formatDate(selectedRun.started_at) }}</span>
                <span>{{ selectedRun.rows_processed }} rows</span>
                <span v-if="selectedRun.business_date">{{ selectedRun.business_date }}</span>
              </div>
              <div v-if="selectedRun.error_message" class="run-error">{{ selectedRun.error_message }}</div>
            </div>

            <div class="run-logs">
              <div v-for="log in runLogs" :key="log.id" class="log-row">
                <span class="log-label">{{ log.node_label || 'executor' }}</span>
                <span class="log-msg">{{ log.message }}</span>
                <span class="log-meta">{{ log.rows_affected }} rows · {{ log.duration_ms }}ms</span>
              </div>
              <div v-if="selectedRun && runLogs.length === 0" class="empty-state small">No logs for this run.</div>
            </div>
          </div>

          <div v-else-if="inspectorTab === 'schedule'" class="inspector-body">
            <div class="config-header">
              <div>
                <span>Pipeline Settings</span>
                <small>{{ pipelineTypeLabel(currentPipeline?.pipeline_type) }}</small>
              </div>
            </div>

            <div class="config-field">
              <label>Name</label>
              <input v-model="pipelineNameModel" />
            </div>
            <div class="config-field">
              <label>Description</label>
              <textarea v-model="pipelineDescriptionModel" rows="3" />
            </div>
            <div class="config-field">
              <label>Use Case</label>
              <select v-model="pipelineTypeModel">
                <option v-for="t in PIPELINE_TYPES" :key="t.value" :value="t.value">{{ t.label }}</option>
              </select>
            </div>
            <div class="config-field">
              <label>Status</label>
              <select v-model="pipelineStatusModel">
                <option value="draft">Draft</option>
                <option value="active">Active</option>
                <option value="paused">Paused</option>
              </select>
            </div>
            <div class="config-field">
              <label>Schedule</label>
              <input v-model="pipelineScheduleModel" placeholder="0 2 * * *" />
            </div>
            <label class="toggle-row">
              <input v-model="pipelineApiEnabledModel" type="checkbox" />
              <span>Serve as API</span>
            </label>
            <div v-if="pipelineApiEnabledModel && currentPipeline" class="api-route">
              POST /api/pipelines/{{ currentPipeline.id }}/run
            </div>
            <button class="btn-secondary wide" :disabled="savingPipeline" @click="save">
              {{ savingPipeline ? 'Saving…' : 'Save settings' }}
            </button>
          </div>

          <div v-else class="inspector-body docs-inspector">
            <div class="config-header">
              <div>
                <span>{{ docCopy.docsTitle }}</span>
                <small>{{ docCopy.docsSubtitle }}</small>
              </div>
              <div class="doc-language-toggle compact" aria-label="Documentation language">
                <button
                  v-for="option in DOC_LANGUAGE_OPTIONS"
                  :key="option.value"
                  :class="{ active: docLanguage === option.value }"
                  @click="docLanguage = option.value"
                >
                  {{ option.label }}
                </button>
              </div>
            </div>

            <div class="doc-section">
              <h3>{{ docCopy.quickStartTitle }}</h3>
              <ol>
                <li v-for="step in pipelineDocSteps" :key="step.title">
                  <strong>{{ step.title.replace(/^[0-9]+[.]\s*/, '') }}</strong>
                  <span>{{ step.body }}</span>
                </li>
              </ol>
            </div>

            <details v-for="section in pipelineDocSections" :key="section.title" class="doc-details" open>
              <summary>{{ section.title }}</summary>
              <ul>
                <li v-for="item in section.items" :key="item">{{ item }}</li>
              </ul>
            </details>

            <div class="doc-section">
              <h3>{{ docCopy.operatorReferenceTitle }}</h3>
              <div class="operator-doc" v-for="operator in NODE_TYPES" :key="operator.type">
                <div>
                  <strong>{{ operator.label }}</strong>
                  <span>{{ operatorDescription(operator) }}</span>
                </div>
                <span class="node-badge">{{ operator.badge }}</span>
              </div>
            </div>

            <div class="doc-section">
              <h3>{{ docCopy.useCaseExamplesTitle }}</h3>
              <article v-for="item in pipelineUseCases" :key="item.title" class="doc-usecase">
                <strong>{{ item.title }}</strong>
                <p>{{ item.goal }}</p>
                <div class="doc-flow">{{ item.graph }}</div>
                <span>{{ item.config }}</span>
                <code>{{ item.example }}</code>
              </article>
            </div>
          </div>
        </div>
        <button
          v-else
          class="panel-restore panel-restore--right"
          @click="showInspectorPanel = true"
        >
          Inspector
        </button>
      </div>
    </div>

    <!-- Delete confirm modal -->
    <div v-if="showDeleteModal" class="modal-overlay" @click.self="showDeleteModal = false">
      <div class="modal modal-confirm">
        <h2>Delete Pipeline</h2>
        <p class="modal-confirm-text">Delete pipeline <strong>"{{ pipelineToDelete?.name }}"</strong>? This action cannot be undone.</p>
        <div class="modal-actions">
          <button class="btn-ghost" @click="showDeleteModal = false">Cancel</button>
          <button class="btn-danger-solid" @click="confirmDelete">Delete</button>
        </div>
      </div>
    </div>

    <!-- Create modal -->
    <div v-if="showCreateModal" class="modal-overlay" @click.self="showCreateModal = false">
      <div class="modal">
        <h2>New Pipeline</h2>
        <div class="config-field">
          <label>Name</label>
          <input v-model="newPipelineName" placeholder="My Pipeline" autofocus @keyup.enter="confirmCreate" />
        </div>
        <div class="config-field">
          <label>Description (optional)</label>
          <input v-model="newPipelineDesc" placeholder="What does this pipeline do?" />
        </div>
        <div class="config-field">
          <label>ETL Use Case</label>
          <select v-model="newPipelineType">
            <option v-for="t in PIPELINE_TYPES" :key="t.value" :value="t.value">{{ t.label }}</option>
          </select>
        </div>
        <div class="modal-actions">
          <button class="btn-ghost" @click="showCreateModal = false">Cancel</button>
          <button class="btn-primary" :disabled="!newPipelineName.trim()" @click="confirmCreate">Create</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.pipelines-shell {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--bg-body);
  color: var(--text-primary);
  font-family: inherit;
}

/* ── List ──────────────────────────────────────────────────── */
.list-view {
  flex: 1;
  padding: 18px;
  overflow-y: auto;
}

.list-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}

.page-title {
  font-size: 20px;
  font-weight: 700;
  margin: 0;
}

.dashboard-doc-strip {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 14px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--r);
  background: var(--bg-surface);
}

.doc-strip-copy {
  min-width: 0;
}

.doc-strip-copy strong {
  display: block;
  margin-top: 2px;
  color: var(--text-primary);
  font-size: 13px;
}

.doc-strip-copy p {
  max-width: 900px;
  margin-top: 2px;
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.4;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.doc-strip-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.dashboard-doc-expanded {
  margin-top: 16px;
}

.dashboard-guide {
  display: grid;
  grid-template-columns: minmax(0, 1.1fr) minmax(320px, 0.9fr);
  gap: 12px;
  margin-bottom: 12px;
}

.guide-main,
.usecase-panel,
.doc-card,
.usecase-card {
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--r);
}

.guide-main {
  padding: 16px;
}

.guide-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.doc-language-toggle {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 3px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-body);
}

.doc-language-toggle button {
  height: 22px;
  min-width: 30px;
  border: 1px solid transparent;
  border-radius: var(--r-xs);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  font-family: inherit;
  font-size: 11px;
  font-weight: 700;
  line-height: 1;
}

.doc-language-toggle button:hover {
  color: var(--text-primary);
  background: var(--bg-elevated);
}

.doc-language-toggle button.active {
  color: var(--brand);
  background: var(--brand-dim);
  border-color: var(--brand-ring);
}

.doc-language-toggle.compact {
  flex-shrink: 0;
}

.guide-main h2,
.section-head h2 {
  margin: 4px 0 6px;
  color: var(--text-primary);
  font-size: 18px;
  line-height: 1.25;
}

.guide-main p,
.doc-card p,
.usecase-card p,
.doc-usecase p {
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.5;
}

.guide-quick-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}

.guide-card {
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: var(--r);
  background: var(--bg-surface);
}

.guide-card strong,
.doc-card h3,
.usecase-card h3,
.doc-section h3 {
  display: block;
  margin: 0 0 5px;
  color: var(--text-primary);
  font-size: 13px;
}

.guide-card span,
.usecase-card span,
.doc-usecase span {
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.4;
}

.doc-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 12px;
}

.doc-card {
  padding: 13px;
}

.usecase-panel {
  padding: 14px;
  margin-bottom: 16px;
}

.section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}

.usecase-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
}

.usecase-card {
  padding: 12px;
  background: var(--bg-body);
}

.usecase-card code,
.doc-usecase code {
  display: block;
  margin-top: 8px;
  padding: 7px 8px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-elevated);
  color: var(--brand);
  font-family: var(--mono);
  font-size: 11px;
  line-height: 1.4;
  overflow-wrap: anywhere;
}

.doc-flow {
  margin: 8px 0;
  padding: 6px 8px;
  border-radius: var(--r-sm);
  background: var(--brand-dim);
  color: var(--brand);
  font-family: var(--mono);
  font-size: 11px;
  line-height: 1.4;
}

.pipeline-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 12px;
}

.pipeline-card {
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--r);
  padding: 14px;
  cursor: pointer;
  transition:
    background-color var(--dur) var(--ease),
    border-color var(--dur) var(--ease),
    box-shadow var(--dur) var(--ease);
}

.pipeline-card:hover {
  background: var(--bg-elevated);
  border-color: var(--brand);
  box-shadow: var(--shadow-sm);
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 10px;
  margin-bottom: 6px;
}

.card-name {
  font-weight: 600;
  font-size: 15px;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.card-status {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 99px;
  font-weight: 600;
  border: 1px solid transparent;
  flex-shrink: 0;
  text-transform: capitalize;
}

.status-draft {
  background: var(--bg-elevated);
  color: var(--text-muted);
  border-color: var(--border);
}
.status-active {
  background: var(--success-bg);
  color: var(--success);
}
.status-paused {
  background: var(--warning-bg);
  color: var(--warning);
}

.card-desc {
  font-size: 12px;
  color: var(--text-secondary);
  margin-bottom: 8px;
  min-height: 18px;
}

.card-type {
  font-size: 11px;
  color: var(--brand);
  margin-bottom: 6px;
  font-weight: 600;
}

.card-meta {
  font-size: 11px;
  color: var(--text-muted);
}

.card-actions {
  display: flex;
  gap: 8px;
  margin-top: 12px;
  justify-content: flex-end;
}

/* ── Canvas ────────────────────────────────────────────────── */
.canvas-view {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.canvas-topbar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 14px;
  background: var(--bg-surface);
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}

.pipeline-name {
  font-weight: 600;
  font-size: 15px;
}

.pipeline-title {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1 1 180px;
  min-width: 0;
}

.pipeline-type {
  font-size: 11px;
  color: var(--text-muted);
}

.pipeline-health {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.metric-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 24px;
  padding: 0 8px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--bg-body);
  color: var(--text-muted);
  font-size: 11px;
  white-space: nowrap;
}

.metric-pill strong {
  color: var(--text-primary);
  font-weight: 700;
}

.run-controls {
  display: flex;
  align-items: stretch;
  gap: 8px;
}

.run-controls label {
  display: flex;
  flex-direction: column;
  gap: 3px;
  font-size: 10px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.4px;
}

.run-controls input,
.run-controls textarea {
  background: var(--bg-body);
  border: 1px solid var(--border-2);
  border-radius: var(--r-sm);
  color: var(--text-primary);
  font-size: 11px;
  padding: 5px 7px;
  min-width: 130px;
  transition:
    border-color var(--dur) var(--ease),
    box-shadow var(--dur) var(--ease);
}

.run-controls input:focus,
.run-controls textarea:focus {
  outline: none;
  border-color: var(--brand);
  box-shadow: 0 0 0 3px var(--brand-ring);
}

.run-controls textarea {
  width: 190px;
  resize: vertical;
  font-family: var(--mono);
}

.topbar-actions {
  display: flex;
  gap: 8px;
  align-items: center;
}

.panel-toggle-group {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 3px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-body);
}

.panel-toggle-group button {
  height: 24px;
  padding: 0 8px;
  border: 1px solid transparent;
  border-radius: var(--r-xs);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  font-family: inherit;
  font-size: 11px;
  font-weight: 700;
}

.panel-toggle-group button:hover {
  color: var(--text-primary);
  background: var(--bg-elevated);
}

.panel-toggle-group button.active {
  color: var(--brand);
  background: var(--brand-dim);
  border-color: var(--brand-ring);
}

.canvas-main {
  flex: 1;
  display: flex;
  min-height: 0;
  min-width: 0;
  position: relative;
}

.node-palette {
  width: 260px;
  flex-shrink: 0;
  background: var(--bg-surface);
  border-right: 1px solid var(--border);
  padding: 12px;
  overflow-y: auto;
}

.panel-restore {
  flex-shrink: 0;
  border: 1px solid var(--border);
  background: var(--bg-surface);
  color: var(--text-secondary);
  cursor: pointer;
  font-family: inherit;
  font-size: 11px;
  font-weight: 700;
  transition:
    background-color var(--dur) var(--ease),
    border-color var(--dur) var(--ease),
    color var(--dur) var(--ease);
}

.panel-restore:hover {
  border-color: var(--brand);
  color: var(--brand);
  background: var(--brand-dim);
}

.panel-restore--left,
.panel-restore--right {
  width: 34px;
  writing-mode: vertical-rl;
  text-orientation: mixed;
  letter-spacing: 0.4px;
}

.panel-restore--left {
  border-width: 0 1px 0 0;
}

.panel-restore--right {
  border-width: 0 0 0 1px;
}

.panel-restore--inline {
  height: 32px;
  padding: 0 10px;
  border-radius: var(--r-sm);
  white-space: nowrap;
}

.catalog-header,
.runs-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 10px;
}

.panel-kicker {
  display: block;
  color: var(--text-muted);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.6px;
  line-height: 1.1;
  text-transform: uppercase;
}

.panel-title {
  color: var(--text-primary);
  font-size: 13px;
  font-weight: 700;
  line-height: 1.3;
}

.catalog-total {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 26px;
  height: 24px;
  border-radius: 999px;
  background: var(--brand-dim);
  color: var(--brand);
  font-size: 11px;
  font-weight: 700;
}

.catalog-search {
  width: 100%;
  height: 30px;
  margin-bottom: 8px;
  border: 1px solid var(--border-2);
  border-radius: var(--r-sm);
  background: var(--bg-body);
  color: var(--text-primary);
  font-size: 12px;
  padding: 0 9px;
}

.catalog-search:focus {
  outline: none;
  border-color: var(--brand);
  box-shadow: 0 0 0 3px var(--brand-ring);
}

.catalog-help {
  margin-bottom: 10px;
  padding: 8px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--brand-dim);
  color: var(--text-secondary);
  font-size: 11px;
  line-height: 1.4;
}

.catalog-stats {
  display: flex;
  gap: 6px;
  margin-bottom: 12px;
}

.catalog-stats span {
  flex: 1;
  padding: 5px 7px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-body);
  color: var(--text-muted);
  font-size: 11px;
  text-align: center;
}

.palette-section {
  margin-bottom: 14px;
}

.palette-label {
  font-size: 10px;
  font-weight: 700;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.6px;
  margin-bottom: 6px;
  padding: 0 4px;
}

.palette-item {
  background: var(--bg-body);
  border: 1px solid var(--border);
  border-left-width: 3px;
  border-radius: var(--r-sm);
  padding: 8px 9px;
  font-size: 12px;
  color: var(--text-secondary);
  cursor: grab;
  user-select: none;
  margin-bottom: 6px;
  transition:
    background-color var(--dur) var(--ease),
    border-color var(--dur) var(--ease),
    color var(--dur) var(--ease);
}

.palette-item:hover {
  background: var(--bg-elevated);
  border-color: var(--border-2);
  color: var(--text-primary);
}

.palette-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-width: 0;
}

.palette-row > span:first-child {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.palette-desc {
  display: block;
  margin-top: 3px;
  color: var(--text-muted);
  font-size: 11px;
  line-height: 1.25;
}

.node-badge {
  flex-shrink: 0;
  padding: 1px 5px;
  border-radius: 999px;
  background: var(--bg-elevated);
  color: var(--text-muted);
  font-size: 10px;
  font-weight: 700;
}

.palette-hint {
  font-size: 10px;
  color: var(--text-muted);
  margin-top: 12px;
  text-align: center;
}

.dag-workspace {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.dag-toolbar {
  min-height: 58px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 12px;
  background: var(--bg-surface);
  border-bottom: 1px solid var(--border);
}

.dag-title {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 90px;
}

.dag-title strong {
  color: var(--text-primary);
  font-size: 13px;
}

.build-guide {
  flex: 1;
  min-width: 260px;
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 6px;
}

.build-guide span {
  padding: 7px 8px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-body);
}

.build-guide strong,
.build-guide em {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.build-guide strong {
  color: var(--text-primary);
  font-size: 11px;
  font-style: normal;
}

.build-guide em {
  margin-top: 2px;
  color: var(--text-muted);
  font-size: 10px;
  font-style: normal;
}

.flow-container {
  position: relative;
  flex: 1;
  min-width: 0;
  min-height: 0;
  background: var(--bg-body);
}

.empty-canvas-guide {
  position: absolute;
  top: 18px;
  left: 18px;
  z-index: 5;
  width: min(340px, calc(100% - 36px));
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: var(--r);
  background: var(--bg-surface);
  box-shadow: var(--shadow-md);
}

.empty-canvas-guide strong,
.empty-canvas-guide span {
  display: block;
}

.empty-canvas-guide strong {
  color: var(--text-primary);
  font-size: 14px;
}

.empty-canvas-guide span {
  margin: 5px 0 12px;
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.45;
}

.flow-container :deep(.vue-flow) {
  background: var(--bg-body);
}

.flow-container :deep(.vue-flow__background) {
  color: var(--border-2);
}

.flow-container :deep(.vue-flow__node-default) {
  border: none;
  box-shadow: var(--shadow-sm);
  cursor: grab;
  max-width: 320px;
  overflow-wrap: anywhere;
}

.flow-container :deep(.vue-flow__node-default.dragging) {
  cursor: grabbing;
  box-shadow: var(--shadow-md);
}

.flow-container :deep(.vue-flow__node-default.selected) {
  box-shadow:
    0 0 0 2px var(--brand-ring),
    var(--shadow-md);
}

.flow-container :deep(.vue-flow__handle) {
  width: 8px;
  height: 8px;
  border: 2px solid var(--bg-body);
  background: var(--brand);
}

.flow-container :deep(.vue-flow__edge-path) {
  stroke: var(--text-muted);
}

.flow-container :deep(.vue-flow__controls) {
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  box-shadow: var(--shadow-sm);
}

.flow-container :deep(.vue-flow__controls-button) {
  background: var(--bg-surface);
  border-bottom: 1px solid var(--border);
  color: var(--text-secondary);
  fill: currentColor;
}

.flow-container :deep(.vue-flow__controls-button:hover) {
  background: var(--bg-elevated);
  color: var(--text-primary);
}

/* ── Config panel ──────────────────────────────────────────── */
.config-panel {
  width: var(--inspector-width, 430px);
  min-width: 360px;
  max-width: 680px;
  flex-shrink: 0;
  background: var(--bg-surface);
  border-left: 1px solid var(--border);
  padding: 0;
  overflow-y: auto;
}

.inspector-panel {
  position: relative;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.inspector-panel.is-resizing,
.inspector-panel.is-resizing * {
  cursor: col-resize;
  user-select: none;
}

.inspector-resize-handle {
  position: absolute;
  top: 0;
  bottom: 0;
  left: -4px;
  z-index: 4;
  width: 8px;
  cursor: col-resize;
}

.inspector-resize-handle::before {
  content: "";
  position: absolute;
  top: 12px;
  bottom: 12px;
  left: 3px;
  width: 2px;
  border-radius: 999px;
  background: transparent;
  transition: background-color var(--dur) var(--ease);
}

.inspector-resize-handle:hover::before,
.inspector-panel.is-resizing .inspector-resize-handle::before {
  background: var(--brand);
}

.inspector-tabs {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 4px;
  padding: 10px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-surface);
  flex-shrink: 0;
}

.inspector-tabs button {
  height: 28px;
  border: 1px solid transparent;
  border-radius: var(--r-sm);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font-family: inherit;
  font-size: 12px;
  font-weight: 600;
}

.inspector-tabs button:hover {
  background: var(--bg-elevated);
  color: var(--text-primary);
}

.inspector-tabs button.active {
  background: var(--brand-dim);
  border-color: var(--brand-ring);
  color: var(--brand);
}

.inspector-body {
  flex: 1;
  min-height: 0;
  padding: 14px;
  overflow-y: auto;
}

.config-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  font-size: 13px;
  text-align: center;
  gap: 8px;
}

.config-empty .hint {
  font-size: 11px;
  color: var(--text-muted);
}

.inspector-empty {
  min-height: 220px;
}

.empty-title {
  color: var(--text-primary);
  font-weight: 700;
}

.overview-grid {
  width: 100%;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}

.overview-grid span {
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-body);
  color: var(--text-muted);
  font-size: 12px;
}

.overview-grid strong {
  display: block;
  color: var(--text-primary);
  font-size: 18px;
}

.config-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
  font-weight: 600;
  font-size: 13px;
  text-transform: capitalize;
  color: var(--text-primary);
}

.config-header > div {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.config-header small {
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 500;
  text-transform: none;
}

.config-field {
  margin-bottom: 12px;
}

.config-field label {
  display: block;
  font-size: 11px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin-bottom: 4px;
}

.config-field input,
.config-field select,
.config-field textarea {
  width: 100%;
  background: var(--bg-body);
  border: 1px solid var(--border-2);
  border-radius: var(--r-sm);
  padding: 6px 8px;
  color: var(--text-primary);
  font-size: 12px;
  font-family: inherit;
  box-sizing: border-box;
  transition:
    border-color var(--dur) var(--ease),
    box-shadow var(--dur) var(--ease);
}

.config-field input::placeholder,
.config-field textarea::placeholder {
  color: var(--text-muted);
}

.config-field input:focus,
.config-field select:focus,
.config-field textarea:focus {
  outline: none;
  border-color: var(--brand);
  box-shadow: 0 0 0 3px var(--brand-ring);
}

.config-field textarea {
  resize: vertical;
  font-family: var(--mono);
}

.config-field textarea.query-textarea {
  min-height: 240px;
  max-height: 62vh;
  line-height: 1.5;
  resize: vertical;
  tab-size: 2;
}

.config-field textarea.query-textarea--compact {
  min-height: 150px;
}

.config-hint {
  font-size: 11px;
  color: var(--text-muted);
  margin-top: 16px;
  line-height: 1.5;
}

.config-hint code {
  padding: 1px 4px;
  border: 1px solid var(--border);
  border-radius: var(--r-xs);
  background: var(--brand-dim);
  color: var(--brand);
  font-family: var(--mono);
  font-size: 10px;
}

.btn-remove-node {
  margin-top: 20px;
  width: 100%;
  padding: 8px 0;
  background: transparent;
  border: 1px solid var(--danger);
  color: var(--danger);
  border-radius: var(--r);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition:
    background-color var(--dur) var(--ease),
    border-color var(--dur) var(--ease),
    color var(--dur) var(--ease);
}
.btn-remove-node:hover {
  background: var(--danger-bg);
  border-color: var(--danger);
  color: var(--danger);
}

.run-list-full {
  overflow-y: auto;
  max-height: 240px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
}

.run-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  cursor: pointer;
  font-size: 12px;
  border-bottom: 1px solid var(--border);
  transition:
    background-color var(--dur) var(--ease),
    color var(--dur) var(--ease);
}

.run-row:last-child {
  border-bottom: 0;
}

.run-row:hover,
.run-row.active {
  background: var(--brand-dim);
}

.run-status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.status-success { background: var(--success); }
.status-failed { background: var(--danger); }
.status-running { background: var(--warning); animation: pulse 1s infinite; }

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.3; }
}

.run-id { font-weight: 600; min-width: 36px; }
.run-info {
  flex: 1;
  min-width: 0;
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.run-rows { font-size: 11px; color: var(--text-muted); }
.run-business-date {
  font-size: 11px;
  color: var(--brand);
  white-space: nowrap;
}
.run-error {
  font-size: 11px;
  color: var(--danger);
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.run-logs {
  margin-top: 12px;
  overflow-y: auto;
  padding: 4px 0;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-body);
}

.log-row {
  display: grid;
  grid-template-columns: 86px minmax(0, 1fr);
  gap: 8px;
  padding: 7px 10px;
  font-size: 12px;
  border-bottom: 1px solid var(--border);
}

.log-row:last-child {
  border-bottom: 0;
}

.log-label {
  font-weight: 600;
  color: var(--brand);
  min-width: 90px;
  font-size: 11px;
}

.log-msg {
  color: var(--text-secondary);
  min-width: 0;
  overflow-wrap: anywhere;
}

.log-meta {
  grid-column: 2;
  font-size: 10px;
  color: var(--text-muted);
  white-space: nowrap;
}

.selected-run-card {
  margin-top: 12px;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-body);
}

.selected-run-head,
.selected-run-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.selected-run-head {
  color: var(--text-primary);
  font-size: 12px;
}

.selected-run-head span:last-child,
.selected-run-meta {
  color: var(--text-muted);
  font-size: 11px;
}

.selected-run-meta {
  flex-wrap: wrap;
  margin-top: 5px;
}

.toggle-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 4px 0 12px;
  color: var(--text-secondary);
  font-size: 13px;
  cursor: pointer;
}

.toggle-row input {
  accent-color: var(--brand);
}

.api-route {
  margin-bottom: 12px;
  padding: 8px 9px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--brand-dim);
  color: var(--brand);
  font-family: var(--mono);
  font-size: 11px;
  overflow-wrap: anywhere;
}

.docs-inspector {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.doc-section,
.doc-details,
.doc-usecase,
.operator-doc {
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-body);
}

.doc-section,
.doc-details {
  padding: 11px;
}

.doc-section ol,
.doc-details ul {
  margin: 0;
  padding-left: 18px;
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.5;
}

.doc-section li + li,
.doc-details li + li {
  margin-top: 7px;
}

.doc-section li span {
  display: block;
  color: var(--text-muted);
}

.doc-details summary {
  cursor: pointer;
  color: var(--text-primary);
  font-size: 13px;
  font-weight: 700;
}

.doc-details ul {
  margin-top: 9px;
}

.operator-doc {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
  padding: 9px;
}

.operator-doc + .operator-doc,
.doc-usecase + .doc-usecase {
  margin-top: 8px;
}

.operator-doc strong,
.doc-usecase strong {
  display: block;
  color: var(--text-primary);
  font-size: 12px;
}

.operator-doc span:not(.node-badge) {
  color: var(--text-muted);
  font-size: 11px;
  line-height: 1.35;
}

.doc-usecase {
  padding: 10px;
}

/* ── Buttons ───────────────────────────────────────────────── */
.btn-primary,
.btn-secondary,
.btn-ghost,
.btn-mini {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  border: 1px solid transparent;
  border-radius: var(--r);
  font-family: inherit;
  font-weight: 500;
  line-height: 1;
  white-space: nowrap;
  cursor: pointer;
  transition:
    background-color var(--dur) var(--ease),
    border-color var(--dur) var(--ease),
    color var(--dur) var(--ease),
    opacity var(--dur) var(--ease);
}

.btn-primary {
  background: var(--brand);
  color: var(--brand-fg);
  border-color: var(--brand);
  padding: 7px 14px;
  font-size: 13px;
}

.btn-primary:hover:not(:disabled) {
  background: var(--brand-h);
  border-color: var(--brand-h);
}

.btn-secondary {
  background: transparent;
  color: var(--text-secondary);
  border-color: var(--border-2);
  padding: 7px 14px;
  font-size: 13px;
}

.btn-secondary:hover:not(:disabled) {
  background: var(--bg-elevated);
  color: var(--text-primary);
}

.btn-ghost {
  background: transparent;
  color: var(--text-secondary);
  border-color: transparent;
  padding: 6px 10px;
  font-size: 13px;
}

.btn-ghost:hover {
  background: var(--bg-elevated);
  color: var(--text-primary);
}

.btn-primary:disabled,
.btn-secondary:disabled,
.btn-ghost:disabled,
.btn-mini:disabled {
  opacity: 0.45;
  pointer-events: none;
}

.btn-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  background: transparent;
  color: var(--text-muted);
  border: 1px solid transparent;
  cursor: pointer;
  font-size: 14px;
  line-height: 1;
  padding: 0;
  border-radius: var(--r-sm);
  transition:
    background-color var(--dur) var(--ease),
    border-color var(--dur) var(--ease),
    color var(--dur) var(--ease);
}

.btn-icon:hover {
  color: var(--text-primary);
  background: var(--bg-elevated);
}

.btn-danger:hover {
  color: var(--danger);
  background: var(--danger-bg);
}

.btn-danger-solid {
  padding: 7px 18px;
  border-radius: var(--r);
  border: none;
  background: var(--danger);
  color: #fff;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: opacity var(--dur) var(--ease);
}

.btn-danger-solid:hover {
  opacity: 0.85;
}

.modal-confirm {
  min-width: 320px;
  max-width: 420px;
}

.modal-confirm-text {
  margin: 0;
  font-size: 14px;
  color: var(--text-secondary);
  line-height: 1.5;
}

.btn-mini {
  border-color: var(--border-2);
  background: transparent;
  color: var(--text-secondary);
  border-radius: var(--r-sm);
  padding: 2px 7px;
  font-size: 11px;
}

.btn-mini:hover:not(:disabled) {
  border-color: var(--brand);
  color: var(--text-primary);
  background: var(--brand-dim);
}

.wide {
  width: 100%;
}

/* ── Shared ────────────────────────────────────────────────── */
.empty-state {
  color: var(--text-muted);
  font-size: 14px;
  padding: 40px;
  text-align: center;
}

.empty-state.small {
  padding: 16px;
  font-size: 12px;
}

/* ── Modal ─────────────────────────────────────────────────── */
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.56);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}

.modal {
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--r);
  padding: 20px;
  min-width: 360px;
  display: flex;
  flex-direction: column;
  gap: 14px;
  box-shadow: var(--shadow-lg);
}

.modal h2 {
  margin: 0;
  font-size: 16px;
  font-weight: 700;
  color: var(--text-primary);
}

.modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}

@media (max-width: 1100px) {
  .dashboard-guide {
    grid-template-columns: 1fr;
  }

  .doc-grid,
  .usecase-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .canvas-topbar {
    align-items: flex-start;
    flex-wrap: wrap;
  }

  .pipeline-title {
    flex: 1 1 220px;
  }

  .run-controls {
    order: 3;
    width: 100%;
    flex-wrap: wrap;
  }

  .dag-toolbar {
    align-items: flex-start;
    flex-wrap: wrap;
  }

  .build-guide {
    order: 3;
    width: 100%;
    flex-basis: 100%;
  }

  .topbar-actions {
    margin-left: auto;
    flex-wrap: wrap;
  }
}

@media (max-width: 900px) {
  .list-view {
    padding: 12px;
  }

  .list-header {
    align-items: flex-start;
    flex-direction: column;
  }

  .dashboard-doc-strip {
    align-items: flex-start;
    flex-direction: column;
  }

  .doc-strip-copy p {
    white-space: normal;
  }

  .doc-strip-actions {
    width: 100%;
    justify-content: space-between;
  }

  .guide-quick-grid,
  .doc-grid,
  .usecase-grid,
  .build-guide {
    grid-template-columns: 1fr;
  }

  .canvas-main {
    flex-direction: column;
  }

  .node-palette {
    width: 100%;
    max-height: 150px;
    border-right: 0;
    border-bottom: 1px solid var(--border);
    display: flex;
    gap: 8px;
  }

  .panel-restore--left,
  .panel-restore--right {
    width: 100%;
    height: 30px;
    writing-mode: horizontal-tb;
    border-width: 0 0 1px;
  }

  .panel-restore--right {
    border-width: 1px 0 0;
  }

  .palette-section {
    min-width: 150px;
  }

  .flow-container {
    min-height: 420px;
  }

  .config-panel {
    width: 100%;
    min-width: 0;
    max-width: none;
    max-height: 280px;
    border-left: 0;
    border-top: 1px solid var(--border);
  }

  .inspector-resize-handle {
    display: none;
  }

}
</style>
