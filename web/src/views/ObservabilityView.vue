<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import { useToast } from '@/composables/useToast'

const props = defineProps<{ activeConnId: number | null }>()
const emit = defineEmits<{ (e: 'set-conn', id: number): void }>()

type Tab = 'overview' | 'nodes' | 'shards' | 'mapping'
type HealthStatus = 'green' | 'yellow' | 'red' | 'unknown'

interface ClusterHealth {
  cluster_name: string
  status: HealthStatus
  number_of_nodes: number
  number_of_data_nodes: number
  active_primary_shards: number
  active_shards: number
  relocating_shards: number
  initializing_shards: number
  unassigned_shards: number
  delayed_unassigned_shards: number
  number_of_pending_tasks: number
  indices?: Record<string, { status: HealthStatus; number_of_shards: number; number_of_replicas: number; active_primary_shards: number; active_shards: number; unassigned_shards: number }>
}

interface NodeRow {
  name: string
  ip: string
  'heap.percent': string
  'heap.max': string
  'ram.percent': string
  'ram.max': string
  cpu: string
  'disk.used_percent': string
  'disk.avail': string
  'node.role': string
  master: string
  load_1m: string
  uptime: string
  [key: string]: string
}

interface ShardRow {
  index: string
  shard: string
  prirep: string
  state: string
  docs: string
  store: string
  ip: string
  node: string
  [key: string]: string
}

interface MappingField {
  name: string
  type: string
  children?: MappingField[]
}

const { connections, fetchConnections } = useConnections()
const toast = useToast()

const activeTab = ref<Tab>('overview')
const loading = ref(false)

const clusterHealth = ref<ClusterHealth | null>(null)
const nodes = ref<NodeRow[]>([])
const shards = ref<ShardRow[]>([])
const mappingIndex = ref('')
const mappingFields = ref<MappingField[]>([])
const mappingRaw = ref<any>(null)
const shardFilter = ref('')
const mappingSearch = ref('')
const shardStateFilter = ref<'all' | 'STARTED' | 'UNASSIGNED' | 'RELOCATING' | 'INITIALIZING'>('all')
const expandedMappingFields = ref<Set<string>>(new Set())

const searchConnections = computed(() => connections.value.filter(c => c.driver === 'elasticsearch' || c.driver === 'opensearch'))
const activeConn = computed(() => props.activeConnId != null ? connections.value.find(c => c.id === props.activeConnId) ?? null : null)
const isSearch = computed(() => activeConn.value?.driver === 'elasticsearch' || activeConn.value?.driver === 'opensearch')

const filteredShards = computed(() => {
  let list = shards.value
  if (shardFilter.value.trim()) {
    const q = shardFilter.value.trim().toLowerCase()
    list = list.filter(s => s.index?.toLowerCase().includes(q) || s.node?.toLowerCase().includes(q))
  }
  if (shardStateFilter.value !== 'all') {
    list = list.filter(s => (s.state || '').toUpperCase() === shardStateFilter.value)
  }
  return list
})

const filteredMappingFields = computed(() => {
  if (!mappingSearch.value.trim()) return mappingFields.value
  return filterFields(mappingFields.value, mappingSearch.value.trim().toLowerCase())
})

const healthIndicesList = computed(() => {
  if (!clusterHealth.value?.indices) return []
  return Object.entries(clusterHealth.value.indices).map(([name, info]) => ({ name, ...info }))
    .sort((a, b) => healthRank(b.status) - healthRank(a.status))
})

onMounted(async () => {
  if (!connections.value.length) await fetchConnections()
  if (!isSearch.value && searchConnections.value.length === 1) {
    emit('set-conn', searchConnections.value[0].id)
    return
  }
  if (isSearch.value) await loadOverview()
})

watch(() => props.activeConnId, async () => {
  resetAll()
  if (isSearch.value) await loadOverview()
})

watch(activeTab, async (tab) => {
  if (!isSearch.value) return
  if (tab === 'overview' && !clusterHealth.value) await loadOverview()
  if (tab === 'nodes' && !nodes.value.length) await loadNodes()
  if (tab === 'shards' && !shards.value.length) await loadShards()
})

async function loadOverview(force = false) {
  if (!activeConn.value) return
  loading.value = true
  try {
    const { data } = await axios.get<ClusterHealth>(`/api/connections/${activeConn.value.id}/search/cluster-health`)
    clusterHealth.value = data
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to load cluster health')
  } finally {
    loading.value = false
  }
}

async function loadNodes() {
  if (!activeConn.value) return
  loading.value = true
  try {
    const { data } = await axios.get<NodeRow[]>(`/api/connections/${activeConn.value.id}/search/nodes`)
    nodes.value = data
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to load nodes')
  } finally {
    loading.value = false
  }
}

async function loadShards() {
  if (!activeConn.value) return
  loading.value = true
  try {
    const { data } = await axios.get<ShardRow[]>(`/api/connections/${activeConn.value.id}/search/shards`)
    shards.value = data
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to load shards')
  } finally {
    loading.value = false
  }
}

async function loadMapping() {
  if (!activeConn.value || !mappingIndex.value.trim()) return
  loading.value = true
  try {
    const { data } = await axios.get(`/api/connections/${activeConn.value.id}/search/mapping`, {
      params: { index: mappingIndex.value.trim() },
    })
    mappingRaw.value = data
    mappingFields.value = parseMappingFields(data, mappingIndex.value.trim())
    expandedMappingFields.value = new Set()
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to load mapping')
  } finally {
    loading.value = false
  }
}

async function refresh() {
  if (!isSearch.value) return
  resetAll()
  await loadOverview()
  if (activeTab.value === 'nodes') await loadNodes()
  if (activeTab.value === 'shards') await loadShards()
}

function resetAll() {
  clusterHealth.value = null
  nodes.value = []
  shards.value = []
  mappingFields.value = []
  mappingRaw.value = null
}

function parseMappingFields(data: any, indexName: string): MappingField[] {
  // Try the index directly, or iterate over returned indices
  const indexData = data[indexName] ?? Object.values(data)[0] as any
  if (!indexData) return []
  const props = indexData?.mappings?.properties ?? {}
  return buildFieldTree(props, '')
}

function buildFieldTree(props: Record<string, any>, prefix: string): MappingField[] {
  return Object.entries(props).map(([key, val]: [string, any]) => {
    const field: MappingField = {
      name: prefix ? `${prefix}.${key}` : key,
      type: val.type ?? (val.properties ? 'object' : 'nested'),
    }
    if (val.properties) {
      field.children = buildFieldTree(val.properties, field.name)
    } else if (val.fields) {
      field.children = buildFieldTree(val.fields, field.name)
    }
    return field
  }).sort((a, b) => a.name.localeCompare(b.name))
}

function filterFields(fields: MappingField[], query: string): MappingField[] {
  const result: MappingField[] = []
  for (const field of fields) {
    if (field.name.toLowerCase().includes(query) || field.type.toLowerCase().includes(query)) {
      result.push(field)
    } else if (field.children) {
      const matched = filterFields(field.children, query)
      if (matched.length) result.push({ ...field, children: matched })
    }
  }
  return result
}

function toggleFieldExpand(name: string) {
  if (expandedMappingFields.value.has(name)) {
    expandedMappingFields.value.delete(name)
  } else {
    expandedMappingFields.value.add(name)
  }
}

function healthRank(status: string): number {
  if (status === 'green') return 3
  if (status === 'yellow') return 2
  if (status === 'red') return 1
  return 0
}

function healthClass(status: string) {
  return `status-${(status || 'unknown').toLowerCase()}`
}

function shardClass(state: string) {
  const s = (state || '').toUpperCase()
  if (s === 'STARTED') return 'shard-started'
  if (s === 'UNASSIGNED') return 'shard-unassigned'
  if (s === 'RELOCATING') return 'shard-relocating'
  if (s === 'INITIALIZING') return 'shard-initializing'
  return ''
}

function nodeRoleLabel(role: string) {
  const r = (role || '').toLowerCase()
  const labels: string[] = []
  if (r.includes('m')) labels.push('master')
  if (r.includes('d')) labels.push('data')
  if (r.includes('i')) labels.push('ingest')
  if (r.includes('c')) labels.push('coordinating')
  return labels.join(', ') || role || '-'
}

function percent(value: string | undefined): number {
  const n = Number(value)
  return Number.isFinite(n) ? Math.round(n) : 0
}

function pctClass(pct: number) {
  if (pct >= 90) return 'pct-crit'
  if (pct >= 75) return 'pct-warn'
  return 'pct-ok'
}
</script>

<template>
  <div class="obs-root page-shell">
    <header class="obs-topbar">
      <div class="obs-title">
        <span class="obs-logo">{{ activeConn?.driver === 'opensearch' ? 'OS' : 'ES' }}</span>
        <div>
          <h1>Observability</h1>
          <p>{{ activeConn ? activeConn.name : 'No Elasticsearch or OpenSearch connection selected' }}</p>
        </div>
      </div>
      <div class="obs-actions">
        <select class="base-input obs-select" :value="activeConnId ?? ''" @change="emit('set-conn', Number(($event.target as HTMLSelectElement).value))">
          <option value="" disabled>Select search cluster</option>
          <option v-for="conn in searchConnections" :key="conn.id" :value="conn.id">{{ conn.name }}</option>
        </select>
        <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!isSearch || loading" @click="refresh">Refresh</button>
      </div>
    </header>

    <section v-if="!isSearch" class="obs-empty">
      <h2>Select a search connection</h2>
      <p>Connect to an Elasticsearch or OpenSearch cluster to monitor cluster health, node stats, shards, and index mappings.</p>
    </section>

    <template v-else>
      <!-- Tabs -->
      <div class="obs-tabs">
        <button :class="{ active: activeTab === 'overview' }" @click="activeTab = 'overview'">Cluster Health</button>
        <button :class="{ active: activeTab === 'nodes' }" @click="activeTab = 'nodes'">Nodes</button>
        <button :class="{ active: activeTab === 'shards' }" @click="activeTab = 'shards'">Shards</button>
        <button :class="{ active: activeTab === 'mapping' }" @click="activeTab = 'mapping'">Mapping</button>
      </div>

      <!-- ── Cluster Health ────────────────────────────────────── -->
      <div v-if="activeTab === 'overview'" class="obs-panel">
        <div v-if="!clusterHealth && !loading" class="obs-loading">Loading cluster health…</div>
        <div v-else-if="clusterHealth">
          <!-- Top summary cards -->
          <div class="obs-stat-grid">
            <div class="obs-stat" :class="healthClass(clusterHealth.status)">
              <span>Status</span>
              <strong>{{ clusterHealth.status?.toUpperCase() ?? '-' }}</strong>
            </div>
            <div class="obs-stat">
              <span>Cluster</span>
              <strong>{{ clusterHealth.cluster_name ?? '-' }}</strong>
            </div>
            <div class="obs-stat">
              <span>Nodes</span>
              <strong>{{ clusterHealth.number_of_nodes }}</strong>
            </div>
            <div class="obs-stat">
              <span>Data Nodes</span>
              <strong>{{ clusterHealth.number_of_data_nodes }}</strong>
            </div>
            <div class="obs-stat">
              <span>Active Shards</span>
              <strong>{{ clusterHealth.active_shards }}</strong>
            </div>
            <div class="obs-stat">
              <span>Primary Shards</span>
              <strong>{{ clusterHealth.active_primary_shards }}</strong>
            </div>
            <div class="obs-stat" :class="clusterHealth.unassigned_shards > 0 ? 'status-red' : ''">
              <span>Unassigned</span>
              <strong>{{ clusterHealth.unassigned_shards }}</strong>
            </div>
            <div class="obs-stat" :class="clusterHealth.relocating_shards > 0 ? 'status-yellow' : ''">
              <span>Relocating</span>
              <strong>{{ clusterHealth.relocating_shards }}</strong>
            </div>
            <div class="obs-stat" :class="clusterHealth.initializing_shards > 0 ? 'status-yellow' : ''">
              <span>Initializing</span>
              <strong>{{ clusterHealth.initializing_shards }}</strong>
            </div>
            <div class="obs-stat" :class="clusterHealth.number_of_pending_tasks > 0 ? 'status-yellow' : ''">
              <span>Pending Tasks</span>
              <strong>{{ clusterHealth.number_of_pending_tasks }}</strong>
            </div>
          </div>

          <!-- Per-index health table -->
          <div v-if="healthIndicesList.length" class="obs-section">
            <div class="obs-section-title">Index Health</div>
            <div class="obs-table-wrap">
              <table class="obs-table">
                <thead>
                  <tr>
                    <th>Index</th>
                    <th>Status</th>
                    <th>Shards</th>
                    <th>Replicas</th>
                    <th>Active Primary</th>
                    <th>Active</th>
                    <th>Unassigned</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="idx in healthIndicesList" :key="idx.name">
                    <td class="obs-mono">{{ idx.name }}</td>
                    <td><span class="obs-badge" :class="healthClass(idx.status)">{{ idx.status }}</span></td>
                    <td>{{ idx.number_of_shards }}</td>
                    <td>{{ idx.number_of_replicas }}</td>
                    <td>{{ idx.active_primary_shards }}</td>
                    <td>{{ idx.active_shards }}</td>
                    <td :class="idx.unassigned_shards > 0 ? 'obs-danger' : ''">{{ idx.unassigned_shards }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </div>

      <!-- ── Nodes ─────────────────────────────────────────────── -->
      <div v-if="activeTab === 'nodes'" class="obs-panel">
        <div v-if="!nodes.length && !loading" class="obs-loading">
          <button class="base-btn base-btn--primary base-btn--sm" @click="loadNodes">Load Nodes</button>
        </div>
        <div v-else-if="nodes.length" class="obs-table-wrap">
          <table class="obs-table">
            <thead>
              <tr>
                <th>Name</th>
                <th>IP</th>
                <th>Role</th>
                <th>Master</th>
                <th>Heap %</th>
                <th>Heap Max</th>
                <th>RAM %</th>
                <th>RAM Max</th>
                <th>CPU %</th>
                <th>Disk Used %</th>
                <th>Disk Avail</th>
                <th>Load 1m</th>
                <th>Uptime</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="node in nodes" :key="node.name + node.ip">
                <td class="obs-bold">{{ node.name || '-' }}</td>
                <td class="obs-mono">{{ node.ip || '-' }}</td>
                <td><span class="obs-role">{{ nodeRoleLabel(node['node.role']) }}</span></td>
                <td class="obs-center">{{ node.master === '*' ? '★' : '-' }}</td>
                <td>
                  <div class="obs-pct-bar">
                    <div class="obs-pct-fill" :class="pctClass(percent(node['heap.percent']))" :style="{ width: `${percent(node['heap.percent'])}%` }" />
                    <span>{{ node['heap.percent'] ?? '-' }}%</span>
                  </div>
                </td>
                <td>{{ node['heap.max'] || '-' }}</td>
                <td>
                  <div class="obs-pct-bar">
                    <div class="obs-pct-fill" :class="pctClass(percent(node['ram.percent']))" :style="{ width: `${percent(node['ram.percent'])}%` }" />
                    <span>{{ node['ram.percent'] ?? '-' }}%</span>
                  </div>
                </td>
                <td>{{ node['ram.max'] || '-' }}</td>
                <td>
                  <div class="obs-pct-bar">
                    <div class="obs-pct-fill" :class="pctClass(percent(node.cpu))" :style="{ width: `${percent(node.cpu)}%` }" />
                    <span>{{ node.cpu ?? '-' }}%</span>
                  </div>
                </td>
                <td>
                  <div class="obs-pct-bar">
                    <div class="obs-pct-fill" :class="pctClass(percent(node['disk.used_percent']))" :style="{ width: `${percent(node['disk.used_percent'])}%` }" />
                    <span>{{ node['disk.used_percent'] ?? '-' }}%</span>
                  </div>
                </td>
                <td>{{ node['disk.avail'] || '-' }}</td>
                <td>{{ node.load_1m || '-' }}</td>
                <td>{{ node.uptime || '-' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- ── Shards ─────────────────────────────────────────────── -->
      <div v-if="activeTab === 'shards'" class="obs-panel">
        <div v-if="!shards.length && !loading" class="obs-loading">
          <button class="base-btn base-btn--primary base-btn--sm" @click="loadShards">Load Shards</button>
        </div>
        <template v-else-if="shards.length">
          <div class="obs-filter-bar">
            <input v-model="shardFilter" class="base-input obs-filter-input" placeholder="Filter by index or node…" />
            <select v-model="shardStateFilter" class="base-input">
              <option value="all">All states</option>
              <option value="STARTED">Started</option>
              <option value="UNASSIGNED">Unassigned</option>
              <option value="RELOCATING">Relocating</option>
              <option value="INITIALIZING">Initializing</option>
            </select>
            <span class="obs-muted">{{ filteredShards.length }} / {{ shards.length }} shards</span>
          </div>
          <div class="obs-table-wrap">
            <table class="obs-table">
              <thead>
                <tr>
                  <th>Index</th>
                  <th>Shard</th>
                  <th>Type</th>
                  <th>State</th>
                  <th>Docs</th>
                  <th>Size</th>
                  <th>Node IP</th>
                  <th>Node</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(shard, i) in filteredShards" :key="`${shard.index}:${shard.shard}:${shard.prirep}:${i}`">
                  <td class="obs-mono">{{ shard.index || '-' }}</td>
                  <td class="obs-center">{{ shard.shard ?? '-' }}</td>
                  <td class="obs-center">
                    <span class="obs-badge" :class="shard.prirep === 'p' ? 'obs-primary' : 'obs-replica'">
                      {{ shard.prirep === 'p' ? 'P' : 'R' }}
                    </span>
                  </td>
                  <td><span class="obs-badge" :class="shardClass(shard.state)">{{ shard.state || '-' }}</span></td>
                  <td>{{ shard.docs || '-' }}</td>
                  <td>{{ shard.store || '-' }}</td>
                  <td class="obs-mono">{{ shard.ip || '-' }}</td>
                  <td>{{ shard.node || 'UNASSIGNED' }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </template>
      </div>

      <!-- ── Mapping ─────────────────────────────────────────────── -->
      <div v-if="activeTab === 'mapping'" class="obs-panel">
        <div class="obs-mapping-bar">
          <input v-model="mappingIndex" class="base-input obs-mapping-input" placeholder="Index name, e.g. logs-*" @keydown.enter="loadMapping" />
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="!mappingIndex.trim() || loading" @click="loadMapping">Load Mapping</button>
          <input v-if="mappingFields.length" v-model="mappingSearch" class="base-input obs-filter-input" placeholder="Search fields…" />
        </div>

        <div v-if="!mappingFields.length && !loading" class="obs-empty-hint">
          Enter an index name and click Load Mapping to explore field types.
        </div>

        <div v-else-if="mappingFields.length" class="obs-mapping-tree">
          <div class="obs-mapping-header">
            <span>Field</span>
            <span>Type</span>
          </div>
          <MappingNode
            v-for="field in filteredMappingFields"
            :key="field.name"
            :field="field"
            :expanded="expandedMappingFields"
            @toggle="toggleFieldExpand"
          />
        </div>
      </div>
    </template>
  </div>

  <!-- Mapping tree component defined inline via defineComponent trick — use recursive template instead -->
</template>

<!-- Recursive mapping node: we define it as a separate component in this SFC via a named script -->
<script lang="ts">
import { defineComponent, h, PropType } from 'vue'

interface MappingFieldDef {
  name: string
  type: string
  children?: MappingFieldDef[]
}

// Explicit annotation avoids TS self-referential inference issues for recursive component.
const MappingNode: any = defineComponent({
  name: 'MappingNode',
  props: {
    field: { type: Object as PropType<MappingFieldDef>, required: true },
    depth: { type: Number, default: 0 },
    expanded: { type: Object as PropType<Set<string>>, required: true },
  },
  emits: ['toggle'],
  setup(props, { emit }) {
    return (): any => {
      const f = props.field
      const hasChildren = f.children && f.children.length > 0
      const isExpanded = props.expanded.has(f.name)
      const indent = props.depth * 18

      const row = h('div', {
        class: ['obs-field-row', hasChildren ? 'obs-field-has-children' : ''],
        style: { paddingLeft: `${indent + 10}px` },
        onClick: hasChildren ? () => emit('toggle', f.name) : undefined,
      }, [
        hasChildren
          ? h('span', { class: 'obs-field-expander' }, isExpanded ? '▾' : '▸')
          : h('span', { class: 'obs-field-leaf' }, '·'),
        h('span', { class: 'obs-field-name' }, f.name.split('.').pop() ?? f.name),
        h('span', { class: ['obs-field-type', `ftype-${f.type}`] }, f.type),
      ])

      const children: any[] = hasChildren && isExpanded
        ? f.children!.map(child =>
          h(MappingNode as any, {
            key: child.name,
            field: child,
            depth: props.depth + 1,
            expanded: props.expanded,
            onToggle: (name: string) => emit('toggle', name),
          }),
        )
        : []

      return h('div', [row, ...children])
    }
  },
})

export { MappingNode }
</script>

<style scoped>
.obs-root { background: var(--bg-body); padding: 18px; gap: 14px; }
.obs-topbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.obs-title { display: flex; align-items: center; gap: 12px; }
.obs-title h1 { margin: 0; font-size: 20px; color: var(--text-primary); }
.obs-title p { margin: 2px 0 0; font-size: 12px; color: var(--text-muted); }
.obs-logo { width: 38px; height: 38px; border-radius: 8px; background: #00bfb3; color: #fff; display: grid; place-items: center; font-weight: 800; font-size: 12px; flex-shrink: 0; }
.obs-actions { display: flex; align-items: center; gap: 8px; }
.obs-select { width: 240px; }
.obs-empty { border: 1px solid var(--border); background: var(--bg-elevated); border-radius: 8px; padding: 36px; text-align: center; color: var(--text-muted); }
.obs-empty h2 { margin: 0 0 6px; color: var(--text-primary); font-size: 16px; }

/* Tabs */
.obs-tabs { display: flex; border-bottom: 1px solid var(--border); gap: 0; }
.obs-tabs button { border: none; border-bottom: 2px solid transparent; background: transparent; color: var(--text-muted); padding: 9px 18px; cursor: pointer; font-size: 13px; font-weight: 600; transition: color 0.15s, border-color 0.15s; }
.obs-tabs button:hover { color: var(--text-primary); }
.obs-tabs button.active { color: #00bfb3; border-bottom-color: #00bfb3; }

.obs-panel { display: flex; flex-direction: column; gap: 14px; flex: 1; min-height: 0; }
.obs-loading { text-align: center; padding: 48px; color: var(--text-muted); font-size: 13px; display: flex; align-items: center; justify-content: center; gap: 12px; }

/* Stat grid */
.obs-stat-grid { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 10px; }
.obs-stat { border: 1px solid var(--border); background: var(--bg-elevated); border-radius: 8px; padding: 12px 14px; display: flex; flex-direction: column; gap: 5px; }
.obs-stat span { color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; font-weight: 600; }
.obs-stat strong { color: var(--text-primary); font-size: 18px; font-weight: 700; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.status-green strong, .status-green { border-color: color-mix(in srgb, var(--success) 40%, var(--border)); }
.status-green strong { color: var(--success); }
.status-yellow strong, .status-yellow { border-color: color-mix(in srgb, var(--warning) 40%, var(--border)); }
.status-yellow strong { color: var(--warning); }
.status-red strong, .status-red { border-color: color-mix(in srgb, var(--danger) 40%, var(--border)); }
.status-red strong { color: var(--danger); }

/* Section */
.obs-section { display: flex; flex-direction: column; gap: 8px; }
.obs-section-title { font-size: 13px; font-weight: 700; color: var(--text-primary); }

/* Table */
.obs-table-wrap { overflow: auto; border: 1px solid var(--border); border-radius: 8px; }
.obs-table { width: 100%; border-collapse: collapse; font-size: 12px; }
.obs-table th { background: var(--bg-elevated); color: var(--text-muted); font-weight: 700; font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; padding: 8px 12px; text-align: left; white-space: nowrap; border-bottom: 1px solid var(--border); }
.obs-table td { padding: 7px 12px; border-bottom: 1px solid var(--border); color: var(--text-primary); vertical-align: middle; }
.obs-table tr:last-child td { border-bottom: none; }
.obs-table tbody tr:hover td { background: var(--bg-elevated); }
.obs-mono { font-family: var(--mono); font-size: 11.5px; }
.obs-bold { font-weight: 600; }
.obs-center { text-align: center; }
.obs-danger { color: var(--danger); font-weight: 700; }

/* Badge */
.obs-badge { border-radius: 4px; padding: 2px 7px; font-size: 10.5px; font-weight: 700; text-transform: uppercase; display: inline-block; }
.status-green.obs-badge { background: color-mix(in srgb, var(--success) 16%, transparent); color: var(--success); border: 1px solid color-mix(in srgb, var(--success) 30%, transparent); }
.status-yellow.obs-badge { background: color-mix(in srgb, var(--warning) 16%, transparent); color: var(--warning); border: 1px solid color-mix(in srgb, var(--warning) 30%, transparent); }
.status-red.obs-badge { background: color-mix(in srgb, var(--danger) 16%, transparent); color: var(--danger); border: 1px solid color-mix(in srgb, var(--danger) 30%, transparent); }
.status-unknown.obs-badge { background: var(--bg-elevated); color: var(--text-muted); border: 1px solid var(--border); }
.shard-started { background: color-mix(in srgb, var(--success) 14%, transparent); color: var(--success); border: 1px solid color-mix(in srgb, var(--success) 28%, transparent); }
.shard-unassigned { background: color-mix(in srgb, var(--danger) 14%, transparent); color: var(--danger); border: 1px solid color-mix(in srgb, var(--danger) 28%, transparent); }
.shard-relocating { background: color-mix(in srgb, var(--warning) 14%, transparent); color: var(--warning); border: 1px solid color-mix(in srgb, var(--warning) 28%, transparent); }
.shard-initializing { background: color-mix(in srgb, #7c6ff7 14%, transparent); color: #7c6ff7; border: 1px solid color-mix(in srgb, #7c6ff7 28%, transparent); }
.obs-primary { background: color-mix(in srgb, #00bfb3 14%, transparent); color: #00bfb3; border: 1px solid color-mix(in srgb, #00bfb3 28%, transparent); }
.obs-replica { background: var(--bg-elevated); color: var(--text-muted); border: 1px solid var(--border); }

/* Node percent bars */
.obs-pct-bar { display: flex; align-items: center; gap: 7px; min-width: 80px; }
.obs-pct-fill { height: 6px; border-radius: 3px; min-width: 2px; transition: width 0.3s; }
.pct-ok { background: var(--success); }
.pct-warn { background: var(--warning); }
.pct-crit { background: var(--danger); }
.obs-pct-bar span { font-size: 11.5px; font-family: var(--mono); color: var(--text-muted); white-space: nowrap; }

/* Node role */
.obs-role { font-size: 10.5px; background: var(--bg-elevated); border: 1px solid var(--border); border-radius: 4px; padding: 2px 6px; color: var(--text-muted); }

/* Filter bar */
.obs-filter-bar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.obs-filter-input { flex: 1; min-width: 180px; height: 34px; }
.obs-muted { color: var(--text-muted); font-size: 11.5px; }

/* Mapping */
.obs-mapping-bar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.obs-mapping-input { flex: 1; min-width: 200px; max-width: 340px; height: 34px; }
.obs-empty-hint { text-align: center; padding: 48px; color: var(--text-muted); font-size: 13px; }
.obs-mapping-tree { border: 1px solid var(--border); border-radius: 8px; overflow: auto; background: var(--bg-elevated); }
.obs-mapping-header { display: grid; grid-template-columns: 1fr 120px; padding: 8px 10px; border-bottom: 1px solid var(--border); font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); }

@media (max-width: 900px) {
  .obs-stat-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .obs-topbar, .obs-actions { flex-direction: column; align-items: stretch; }
  .obs-select { width: 100%; }
}
</style>

<style>
/* Mapping field rows — unscoped so the dynamic component can use them */
.obs-field-row { display: grid; grid-template-columns: 1fr 120px; align-items: center; padding: 5px 10px; border-bottom: 1px solid var(--border); font-size: 12px; gap: 8px; }
.obs-field-row:last-child { border-bottom: none; }
.obs-field-row.obs-field-has-children { cursor: pointer; }
.obs-field-row.obs-field-has-children:hover { background: color-mix(in srgb, var(--text-muted) 5%, transparent); }
.obs-field-expander { width: 14px; display: inline-block; color: var(--text-muted); font-size: 10px; }
.obs-field-leaf { width: 14px; display: inline-block; color: var(--text-muted); font-size: 8px; }
.obs-field-name { color: var(--text-primary); font-family: var(--mono); font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.obs-field-type { font-size: 11px; font-weight: 700; text-transform: uppercase; padding: 2px 7px; border-radius: 4px; text-align: center; }
.ftype-text { background: color-mix(in srgb, #6366f1 14%, transparent); color: #6366f1; }
.ftype-keyword { background: color-mix(in srgb, #f59e0b 14%, transparent); color: #d97706; }
.ftype-date { background: color-mix(in srgb, #06b6d4 14%, transparent); color: #0891b2; }
.ftype-long, .ftype-integer, .ftype-short, .ftype-byte { background: color-mix(in srgb, #10b981 14%, transparent); color: #059669; }
.ftype-float, .ftype-double, .ftype-half_float, .ftype-scaled_float { background: color-mix(in srgb, #8b5cf6 14%, transparent); color: #7c3aed; }
.ftype-boolean { background: color-mix(in srgb, #f43f5e 14%, transparent); color: #e11d48; }
.ftype-object, .ftype-nested { background: color-mix(in srgb, #00bfb3 14%, transparent); color: #00a69c; }
.ftype-geo_point, .ftype-geo_shape { background: color-mix(in srgb, #f97316 14%, transparent); color: #ea580c; }
.ftype-ip { background: color-mix(in srgb, #64748b 14%, transparent); color: #475569; }
</style>
