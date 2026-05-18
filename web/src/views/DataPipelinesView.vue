<script setup lang="ts">
import { ref, computed, onMounted, watch, nextTick } from 'vue'
import { VueFlow, useVueFlow, Position, type Node, type Edge } from '@vue-flow/core'
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import '@vue-flow/controls/dist/style.css'
import { usePipelines, type Pipeline, type PipelineRun, type PipelineRunLog } from '@/composables/usePipelines'
import { useConnections } from '@/composables/useConnections'
import { useToast } from '@/composables/useToast'
type ToastType = 'success' | 'error' | 'info'

// ── State ────────────────────────────────────────────────────────────────────

const { pipelines, loading, fetchPipelines, createPipeline, getPipeline, savePipeline, deletePipeline, triggerRun, fetchRuns, fetchRunStatus, fetchRunLogs } = usePipelines()
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
const newPipelineName = ref('')
const newPipelineDesc = ref('')

// canvas nodes/edges (vue-flow format)
const nodes = ref<Node[]>([])
const edges = ref<Edge[]>([])

// selected node for config panel
const selectedNode = ref<Node | null>(null)

// run history
const runs = ref<PipelineRun[]>([])
const runLogs = ref<PipelineRunLog[]>([])
const selectedRunId = ref<number | null>(null)
const showRunDrawer = ref(false)
const pollingInterval = ref<ReturnType<typeof setInterval> | null>(null)

// vue-flow instance
const { onNodeClick, onConnect, addEdges, setNodes, setEdges } = useVueFlow()

// ── Node type definitions ─────────────────────────────────────────────────────

const NODE_TYPES = [
  { type: 'source_query', label: 'Source: Query', color: '#3b82f6', section: 'Source' },
  { type: 'source_table', label: 'Source: Table', color: '#3b82f6', section: 'Source' },
  { type: 'sink_table', label: 'Sink: Table', color: '#10b981', section: 'Sink' },
  { type: 'sink_object_storage', label: 'Sink: Object Storage', color: '#8b5cf6', section: 'Sink' },
]

// relational connections only
const relationalConnections = computed(() =>
  connections.value.filter(c => ['postgres', 'mysql', 'mariadb', 'mssql', 'sqlite'].includes(c.driver))
)

// object storage connections only
const objectStorageConnections = computed(() =>
  connections.value.filter(c => ['s3_aws', 's3_gcp', 's3_oss', 's3_obs'].includes(c.driver))
)

// ── Canvas helpers ────────────────────────────────────────────────────────────

function nodeLabel(type: string) {
  return NODE_TYPES.find(n => n.type === type)?.label ?? type
}

function nodeColor(type: string) {
  if (type.startsWith('source')) return '#3b82f6'
  if (type === 'sink_object_storage') return '#8b5cf6'
  if (type.startsWith('sink')) return '#10b981'
  return '#6366f1'
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
    style: {
      background: nodeColor(n.node_type),
      color: '#fff',
      border: 'none',
      borderRadius: '8px',
      padding: '10px 16px',
      fontWeight: '600',
      fontSize: '13px',
      minWidth: '160px',
    },
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
    style: { stroke: '#94a3b8', strokeWidth: 2 },
  }))
}

// Convert vue-flow nodes back to pipeline node format for saving
function fromFlowNodes(flowNodes: Node[], flowEdges: Edge[]): { nodes: any[]; edges: any[] } {
  const result = {
    nodes: flowNodes.map((n, idx) => ({
      id: parseInt(n.id) || -(idx + 1),
      pipeline_id: currentPipeline.value?.id,
      node_type: n.data.nodeType,
      connection_id: n.data.connectionId ?? null,
      config: n.data.config ?? {},
      position_x: n.position.x,
      position_y: n.position.y,
      label: n.data.label || n.label,
    })),
    edges: flowEdges.map(e => ({
      source_node_id: parseInt(e.source),
      target_node_id: parseInt(e.target),
    })),
  }
  return result
}

// ── Lifecycle ─────────────────────────────────────────────────────────────────

onMounted(async () => {
  await Promise.all([fetchPipelines(), fetchConnections()])
})

// ── Actions ───────────────────────────────────────────────────────────────────

async function openCreateModal() {
  newPipelineName.value = ''
  newPipelineDesc.value = ''
  showCreateModal.value = true
}

async function confirmCreate() {
  if (!newPipelineName.value.trim()) return
  try {
    const id = await createPipeline(newPipelineName.value.trim(), newPipelineDesc.value)
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
    nodes.value = toFlowNodes(p.nodes ?? [])
    edges.value = toFlowEdges(p.edges ?? [])
    selectedNode.value = null
    runs.value = await fetchRuns(id)
    view.value = 'canvas'
  } catch (e: any) {
    showToast('Failed to load pipeline', 'error')
  }
}

async function save() {
  if (!currentPipeline.value) return
  savingPipeline.value = true
  try {
    const { nodes: pNodes, edges: pEdges } = fromFlowNodes(nodes.value as Node[], edges.value as Edge[])
    await savePipeline(currentPipeline.value.id, {
      name: currentPipeline.value.name,
      description: currentPipeline.value.description,
      status: currentPipeline.value.status,
      schedule: currentPipeline.value.schedule,
      nodes: pNodes,
      edges: pEdges,
    })
    // reload to get real DB ids
    const p = await getPipeline(currentPipeline.value.id)
    currentPipeline.value = p
    nodes.value = toFlowNodes(p.nodes ?? [])
    edges.value = toFlowEdges(p.edges ?? [])
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
    const runId = await triggerRun(currentPipeline.value.id)
    selectedRunId.value = runId
    showRunDrawer.value = true
    runs.value = await fetchRuns(currentPipeline.value.id)
    startPolling(runId)
  } catch (e: any) {
    showToast(e.response?.data?.error || 'Run failed', 'error')
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
  if (!currentPipeline.value) return
  runLogs.value = await fetchRunLogs(currentPipeline.value.id, run.id)
  showRunDrawer.value = true
}

async function handleDelete(p: Pipeline) {
  if (!confirm(`Delete pipeline "${p.name}"?`)) return
  try {
    await deletePipeline(p.id)
    await fetchPipelines()
  } catch {
    showToast('Delete failed', 'error')
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
})

onConnect((connection) => {
  addEdges([{
    ...connection,
    id: `e${connection.source}-${connection.target}-${Date.now()}`,
    animated: true,
    style: { stroke: '#94a3b8', strokeWidth: 2 },
  }])
})

let dragNodeType = ''

function onDragStart(type: string) {
  dragNodeType = type
}

function onDrop(event: DragEvent) {
  if (!dragNodeType) return
  const bounds = (event.currentTarget as HTMLElement).getBoundingClientRect()
  const x = event.clientX - bounds.left
  const y = event.clientY - bounds.top
  const type = dragNodeType
  dragNodeType = ''
  const id = `new-${Date.now()}`
  const label = nodeLabel(type)
  const newNode: Node = {
    id,
    type: 'default',
    position: { x, y },
    label,
    data: { nodeType: type, connectionId: null, config: {}, label },
    style: {
      background: nodeColor(type),
      color: '#fff',
      border: 'none',
      borderRadius: '8px',
      padding: '10px 16px',
      fontWeight: '600',
      fontSize: '13px',
      minWidth: '160px',
    },
    sourcePosition: Position.Right,
    targetPosition: Position.Left,
  };
  (nodes.value as Node[]).push(newNode)
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
  nodes.value = (nodes.value as Node[]).filter((n: Node) => n.id !== id) as Node[]
  edges.value = (edges.value as Edge[]).filter((e: Edge) => e.source !== id && e.target !== id) as Edge[]
  selectedNode.value = null
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
        <h1 class="page-title">Data Pipelines</h1>
        <button class="btn-primary" @click="openCreateModal">+ New Pipeline</button>
      </div>

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
    </div>

    <!-- ── Canvas View ─────────────────────────────────────── -->
    <div v-else class="canvas-view">
      <!-- Top bar -->
      <div class="canvas-topbar">
        <button class="btn-ghost" @click="backToList">← Pipelines</button>
        <span class="pipeline-name">{{ currentPipeline?.name }}</span>
        <div class="topbar-actions">
          <button class="btn-secondary" :disabled="savingPipeline" @click="save">
            {{ savingPipeline ? 'Saving…' : 'Save' }}
          </button>
          <button class="btn-primary" :disabled="running" @click="runPipeline">
            {{ running ? '⏳ Running…' : '▶ Run' }}
          </button>
          <button class="btn-ghost" @click="showRunDrawer = !showRunDrawer">
            History ({{ runs.length }})
          </button>
        </div>
      </div>

      <!-- Main area -->
      <div class="canvas-main">
        <!-- Node palette -->
        <div class="node-palette">
          <div class="palette-section" v-for="section in ['Source', 'Sink']" :key="section">
            <div class="palette-label">{{ section }}</div>
            <div
              v-for="nt in NODE_TYPES.filter(n => n.section === section)"
              :key="nt.type"
              class="palette-item"
              :style="{ borderLeftColor: nt.color }"
              draggable="true"
              @dragstart="onDragStart(nt.type)"
            >
              {{ nt.label }}
            </div>
          </div>
          <div class="palette-hint">Drag node to canvas</div>
        </div>

        <!-- Canvas -->
        <div class="flow-container" @dragover.prevent @drop="onDrop">
          <VueFlow
            v-model:nodes="nodes"
            v-model:edges="edges"
            fit-view-on-init
            :default-viewport="{ zoom: 1 }"
            :min-zoom="0.3"
            :max-zoom="2"
          >
            <Background variant="dots" :gap="20" :size="1" />
            <Controls />
          </VueFlow>
        </div>

        <!-- Config panel -->
        <div v-if="selectedNode" class="config-panel">
          <div class="config-header">
            <span>{{ selectedNode.data.nodeType }}</span>
            <button class="btn-icon btn-danger" @click="removeSelectedNode" title="Remove node">✕</button>
          </div>

          <div class="config-field">
            <label>Label</label>
            <input
              :value="selectedNode.data.label"
              @input="updateSelectedNodeField('label', ($event.target as HTMLInputElement).value)"
            />
          </div>

          <!-- Connection selector — relational for source/sink_table nodes -->
          <div v-if="selectedNode.data.nodeType !== 'transform_sql' && selectedNode.data.nodeType !== 'sink_object_storage'" class="config-field">
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
                :value="selectedNode.data.config.sql ?? ''"
                rows="6"
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
            Connect nodes by dragging from a node's right handle to another node's left handle.
          </div>
        </div>

        <div v-else class="config-panel config-empty">
          <p>Click a node to configure it.</p>
          <p class="hint">Drag node types from the palette on the left onto the canvas.</p>
        </div>
      </div>

      <!-- Run history drawer -->
      <div v-if="showRunDrawer" class="run-drawer">
        <div class="drawer-header">
          <span>Run History</span>
          <button class="btn-icon" @click="showRunDrawer = false">✕</button>
        </div>
        <div class="drawer-body">
          <div class="run-split" v-if="selectedRunId && runLogs.length > 0">
            <div class="run-list">
              <div
                v-for="run in runs"
                :key="run.id"
                :class="['run-row', { active: selectedRunId === run.id }]"
                @click="viewRunLogs(run)"
              >
                <span :class="['run-status-dot', runStatusClass(run.status)]"></span>
                <span class="run-id">#{{ run.id }}</span>
                <span class="run-info">{{ formatDate(run.started_at) }}</span>
                <span class="run-rows">{{ run.rows_processed }} rows</span>
              </div>
            </div>
            <div class="run-logs">
              <div v-for="log in runLogs" :key="log.id" class="log-row">
                <span class="log-label">{{ log.node_label || 'executor' }}</span>
                <span class="log-msg">{{ log.message }}</span>
                <span class="log-meta">{{ log.rows_affected }} rows · {{ log.duration_ms }}ms</span>
              </div>
            </div>
          </div>
          <div v-else class="run-list-full">
            <div
              v-for="run in runs"
              :key="run.id"
              class="run-row"
              @click="viewRunLogs(run)"
            >
              <span :class="['run-status-dot', runStatusClass(run.status)]"></span>
              <span class="run-id">#{{ run.id }}</span>
              <span class="run-info">{{ formatDate(run.started_at) }}</span>
              <span class="run-rows">{{ run.rows_processed }} rows</span>
              <span v-if="run.status === 'failed'" class="run-error">{{ run.error_message }}</span>
            </div>
            <div v-if="runs.length === 0" class="empty-state small">No runs yet.</div>
          </div>
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
  background: var(--color-bg, #0f172a);
  color: var(--color-text, #e2e8f0);
  font-family: inherit;
}

/* ── List ──────────────────────────────────────────────────── */
.list-view {
  flex: 1;
  padding: 24px;
  overflow-y: auto;
}

.list-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.page-title {
  font-size: 20px;
  font-weight: 700;
  margin: 0;
}

.pipeline-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 16px;
}

.pipeline-card {
  background: var(--color-surface, #1e293b);
  border: 1px solid var(--color-border, #334155);
  border-radius: 10px;
  padding: 16px;
  cursor: pointer;
  transition: border-color 0.15s;
}

.pipeline-card:hover {
  border-color: #3b82f6;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 6px;
}

.card-name {
  font-weight: 600;
  font-size: 15px;
}

.card-status {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 99px;
  font-weight: 600;
}

.status-draft { background: #334155; color: #94a3b8; }
.status-active { background: #052e16; color: #4ade80; }
.status-paused { background: #431407; color: #fb923c; }

.card-desc {
  font-size: 12px;
  color: #64748b;
  margin-bottom: 8px;
}

.card-meta {
  font-size: 11px;
  color: #475569;
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
  padding: 10px 16px;
  background: var(--color-surface, #1e293b);
  border-bottom: 1px solid var(--color-border, #334155);
  flex-shrink: 0;
}

.pipeline-name {
  font-weight: 600;
  font-size: 15px;
  flex: 1;
}

.topbar-actions {
  display: flex;
  gap: 8px;
}

.canvas-main {
  flex: 1;
  display: flex;
  min-height: 0;
  position: relative;
}

.node-palette {
  width: 160px;
  flex-shrink: 0;
  background: var(--color-surface, #1e293b);
  border-right: 1px solid var(--color-border, #334155);
  padding: 12px 8px;
  overflow-y: auto;
}

.palette-section {
  margin-bottom: 12px;
}

.palette-label {
  font-size: 10px;
  font-weight: 700;
  color: #64748b;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  margin-bottom: 6px;
  padding: 0 4px;
}

.palette-item {
  background: var(--color-bg, #0f172a);
  border: 1px solid var(--color-border, #334155);
  border-left-width: 3px;
  border-radius: 6px;
  padding: 8px 10px;
  font-size: 12px;
  cursor: grab;
  user-select: none;
  margin-bottom: 6px;
  transition: opacity 0.15s;
}

.palette-item:hover {
  opacity: 0.8;
}

.palette-hint {
  font-size: 10px;
  color: #475569;
  margin-top: 12px;
  text-align: center;
}

.flow-container {
  flex: 1;
  min-width: 0;
  height: 100%;
}

/* ── Config panel ──────────────────────────────────────────── */
.config-panel {
  width: 260px;
  flex-shrink: 0;
  background: var(--color-surface, #1e293b);
  border-left: 1px solid var(--color-border, #334155);
  padding: 16px;
  overflow-y: auto;
}

.config-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  color: #475569;
  font-size: 13px;
  text-align: center;
  gap: 8px;
}

.config-empty .hint {
  font-size: 11px;
  color: #334155;
}

.config-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
  font-weight: 600;
  font-size: 13px;
  text-transform: capitalize;
}

.config-field {
  margin-bottom: 12px;
}

.config-field label {
  display: block;
  font-size: 11px;
  font-weight: 600;
  color: #64748b;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  margin-bottom: 4px;
}

.config-field input,
.config-field select,
.config-field textarea {
  width: 100%;
  background: var(--color-bg, #0f172a);
  border: 1px solid var(--color-border, #334155);
  border-radius: 6px;
  padding: 6px 8px;
  color: inherit;
  font-size: 12px;
  font-family: inherit;
  box-sizing: border-box;
}

.config-field textarea {
  resize: vertical;
  font-family: 'JetBrains Mono', monospace;
}

.config-hint {
  font-size: 11px;
  color: #475569;
  margin-top: 16px;
  line-height: 1.5;
}

/* ── Run drawer ────────────────────────────────────────────── */
.run-drawer {
  position: absolute;
  bottom: 0;
  left: 160px;
  right: 260px;
  height: 220px;
  background: var(--color-surface, #1e293b);
  border-top: 1px solid var(--color-border, #334155);
  display: flex;
  flex-direction: column;
  z-index: 10;
}

.drawer-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 14px;
  font-weight: 600;
  font-size: 13px;
  border-bottom: 1px solid var(--color-border, #334155);
  flex-shrink: 0;
}

.drawer-body {
  flex: 1;
  overflow: hidden;
}

.run-split {
  display: flex;
  height: 100%;
}

.run-list {
  width: 300px;
  flex-shrink: 0;
  overflow-y: auto;
  border-right: 1px solid var(--color-border, #334155);
}

.run-list-full {
  overflow-y: auto;
  height: 100%;
}

.run-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 12px;
  cursor: pointer;
  font-size: 12px;
  border-bottom: 1px solid var(--color-border, #1e293b);
}

.run-row:hover,
.run-row.active {
  background: rgba(59, 130, 246, 0.1);
}

.run-status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.status-success { background: #4ade80; }
.status-failed { background: #f87171; }
.status-running { background: #facc15; animation: pulse 1s infinite; }

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.3; }
}

.run-id { font-weight: 600; min-width: 36px; }
.run-info { flex: 1; color: #64748b; }
.run-rows { font-size: 11px; color: #475569; }
.run-error { font-size: 11px; color: #f87171; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.run-logs {
  flex: 1;
  overflow-y: auto;
  padding: 4px 0;
}

.log-row {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 4px 12px;
  font-size: 12px;
  border-bottom: 1px solid rgba(51, 65, 85, 0.5);
}

.log-label {
  font-weight: 600;
  color: #93c5fd;
  min-width: 90px;
  font-size: 11px;
}

.log-msg {
  flex: 1;
  color: #cbd5e1;
}

.log-meta {
  font-size: 10px;
  color: #475569;
  white-space: nowrap;
}

/* ── Buttons ───────────────────────────────────────────────── */
.btn-primary {
  background: #3b82f6;
  color: #fff;
  border: none;
  border-radius: 6px;
  padding: 7px 14px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
}

.btn-primary:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.btn-secondary {
  background: transparent;
  color: #94a3b8;
  border: 1px solid #334155;
  border-radius: 6px;
  padding: 7px 14px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
}

.btn-secondary:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.btn-ghost {
  background: transparent;
  color: #94a3b8;
  border: none;
  border-radius: 6px;
  padding: 6px 10px;
  font-size: 13px;
  cursor: pointer;
}

.btn-ghost:hover {
  background: rgba(255, 255, 255, 0.05);
}

.btn-icon {
  background: transparent;
  color: #64748b;
  border: none;
  cursor: pointer;
  font-size: 14px;
  padding: 2px 6px;
  border-radius: 4px;
}

.btn-icon:hover {
  color: #e2e8f0;
  background: rgba(255, 255, 255, 0.05);
}

.btn-danger:hover {
  color: #f87171;
}

/* ── Shared ────────────────────────────────────────────────── */
.empty-state {
  color: #475569;
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
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}

.modal {
  background: var(--color-surface, #1e293b);
  border: 1px solid var(--color-border, #334155);
  border-radius: 12px;
  padding: 24px;
  min-width: 360px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.modal h2 {
  margin: 0;
  font-size: 16px;
  font-weight: 700;
}

.modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
</style>
