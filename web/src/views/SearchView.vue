<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useSearchCache } from '@/composables/useSearchCache'

const props = defineProps<{ activeConnId: number | null }>()
const emit = defineEmits<{ (e: 'set-conn', id: number): void }>()

type SearchIndex = {
  health: string
  status: string
  name: string
  kind: string
  uuid: string
  primary_shards: string
  replica_shards: string
  docs_count: string
  store_size: string
  store_bytes: number
  created_at: string
  backing_count: number
}

type PrettyHit = {
  timestamp: string
  level: string
  title: string
  service: string
  host: string
  container: string
  stream: string
  index: string
  id: string
  tags: Array<{ label: string; value: string }>
  metrics: Array<{ label: string; value: string }>
  raw: any
}

type TimeRange = '15m' | '1h' | '24h' | '7d' | '30d' | 'all' | 'custom'
type SortDirection = 'asc' | 'desc'
type IndexSortField = 'name' | 'docs' | 'size' | 'date' | 'health' | 'kind'

const { connections, fetchConnections } = useConnections()
const toast = useToast()
const { confirm } = useConfirm()
const searchCache = useSearchCache()

const searchConnections = computed(() => connections.value.filter(c => c.driver === 'elasticsearch' || c.driver === 'opensearch'))
const activeConn = computed(() => props.activeConnId != null ? connections.value.find(c => c.id === props.activeConnId) ?? null : null)
const isSearch = computed(() => activeConn.value?.driver === 'elasticsearch' || activeConn.value?.driver === 'opensearch')

const loading = ref(false)
const connected = ref(false)
const latencyMs = ref<number | null>(null)
const clusterInfo = ref<Record<string, any> | null>(null)
const indices = ref<SearchIndex[]>([])
const indexFilter = ref('')
const indexSortField = ref<IndexSortField>('size')
const indexSortDirection = ref<SortDirection>('desc')
const showSystemIndices = ref(true)
const selectedIndex = ref('')
const queryText = ref('{\n  "query": {\n    "match_all": {}\n  }\n}')
const querySize = ref(50)
const queryFrom = ref(0)
const sortField = ref('@timestamp')
const sortDirection = ref<SortDirection>('desc')
const timeRange = ref<TimeRange>('24h')
const customTimeFrom = ref('')
const customTimeTo = ref('')
const searchResult = ref<Record<string, any> | null>(null)
const docId = ref('')
const docBody = ref('{\n  "message": "hello from NIAS",\n  "level": "info"\n}')
const docResult = ref<Record<string, any> | null>(null)
const activeTab = ref<'query' | 'document'>('query')

const filteredIndices = computed(() => {
  const q = indexFilter.value.trim().toLowerCase()
  const filtered = indices.value.filter(i => {
    if (!showSystemIndices.value && i.name.startsWith('.')) return false
    if (!q) return true
    return i.name.toLowerCase().includes(q) || (i.kind || '').toLowerCase().includes(q) || (i.health || i.status || '').toLowerCase().includes(q)
  })
  const dir = indexSortDirection.value === 'asc' ? 1 : -1
  return [...filtered].sort((a, b) => compareIndices(a, b, indexSortField.value) * dir)
})

const hits = computed(() => {
  const raw = searchResult.value?.hits?.hits
  return Array.isArray(raw) ? raw : []
})

const prettyHits = computed(() => hits.value.map(prettifyHit))

const totalHits = computed(() => {
  const total = searchResult.value?.hits?.total
  if (typeof total === 'number') return total
  if (total && typeof total.value === 'number') return total.value
  return hits.value.length
})

onMounted(async () => {
  if (!connections.value.length) await fetchConnections()
  if (!isSearch.value && searchConnections.value.length === 1) {
    emit('set-conn', searchConnections.value[0].id)
    return
  }
  if (isSearch.value) await refresh()
})

watch(() => props.activeConnId, async () => {
  resetWorkspace()
  if (isSearch.value) await refresh()
})

async function refresh(force = false) {
  if (!activeConn.value) return
  const id = activeConn.value.id

  const cachedInfo = !force && searchCache.get<any>(id, 'info')
  const cachedIdx  = !force && searchCache.get<SearchIndex[]>(id, 'indices')

  if (cachedInfo && cachedIdx) {
    connected.value = true
    latencyMs.value = cachedInfo.latency_ms ?? null
    clusterInfo.value = cachedInfo.cluster ?? null
    indices.value = cachedIdx
    if (!selectedIndex.value && activeConn.value.database) selectedIndex.value = activeConn.value.database
    if (!selectedIndex.value && cachedIdx.length) selectedIndex.value = cachedIdx[0].name
    return
  }

  loading.value = true
  try {
    const [{ data: info }, { data: idx }] = await Promise.all([
      axios.get(`/api/connections/${id}/search/info`),
      axios.get<SearchIndex[]>(`/api/connections/${id}/search/indices`),
    ])
    connected.value = true
    latencyMs.value = info.latency_ms ?? null
    clusterInfo.value = info.cluster ?? null
    indices.value = idx
    searchCache.set(id, 'info', info, 'info')
    searchCache.set(id, 'indices', idx, 'indices')
    if (!selectedIndex.value && activeConn.value.database) selectedIndex.value = activeConn.value.database
    if (!selectedIndex.value && idx.length) selectedIndex.value = idx[0].name
  } catch (e: any) {
    connected.value = false
    toast.error(e?.response?.data?.error ?? 'Failed to connect search cluster')
  } finally {
    loading.value = false
  }
}

async function runQuery() {
  if (!activeConn.value || !selectedIndex.value.trim()) return
  loading.value = true
  try {
    const parsed = JSON.parse(queryText.value || '{}')
    const body = buildQueryBody(parsed)
    const { data } = await axios.post(`/api/connections/${activeConn.value.id}/search/query`, {
      index: selectedIndex.value.trim(),
      query: body,
      size: Number(querySize.value || 50),
      from: Number(queryFrom.value || 0),
    })
    searchResult.value = data
    activeTab.value = 'query'
  } catch (e: any) {
    toast.error(e instanceof SyntaxError ? 'Query JSON is invalid' : e?.response?.data?.error ?? 'Search failed')
  } finally {
    loading.value = false
  }
}

async function readDocument() {
  if (!activeConn.value || !selectedIndex.value.trim() || !docId.value.trim()) return
  loading.value = true
  try {
    const { data } = await axios.get(`/api/connections/${activeConn.value.id}/search/document`, {
      params: { index: selectedIndex.value.trim(), id: docId.value.trim() },
    })
    docResult.value = data
    docBody.value = JSON.stringify(data._source ?? data, null, 2)
    activeTab.value = 'document'
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to read document')
  } finally {
    loading.value = false
  }
}

async function saveDocument() {
  if (!activeConn.value || !selectedIndex.value.trim()) return
  loading.value = true
  try {
    const document = JSON.parse(docBody.value || '{}')
    const { data } = await axios.post(`/api/connections/${activeConn.value.id}/search/document`, {
      index: selectedIndex.value.trim(),
      id: docId.value.trim(),
      document,
    })
    docResult.value = data
    if (!docId.value && data._id) docId.value = data._id
    toast.success('Document saved')
    await refresh()
  } catch (e: any) {
    toast.error(e instanceof SyntaxError ? 'Document JSON is invalid' : e?.response?.data?.error ?? 'Failed to save document')
  } finally {
    loading.value = false
  }
}

async function deleteDocument() {
  if (!activeConn.value || !selectedIndex.value.trim() || !docId.value.trim()) return
  const ok = await confirm(`Delete document "${docId.value}" from "${selectedIndex.value}"?`, 'Delete Document')
  if (!ok) return
  loading.value = true
  try {
    const { data } = await axios.delete(`/api/connections/${activeConn.value.id}/search/document`, {
      params: { index: selectedIndex.value.trim(), id: docId.value.trim() },
    })
    docResult.value = data
    toast.success('Document deleted')
    await refresh()
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to delete document')
  } finally {
    loading.value = false
  }
}

function selectIndex(name: string) {
  selectedIndex.value = name
}

async function deleteIndex(name: string) {
  const ok = await confirm(`Delete index "${name}"? This is irreversible and all data will be lost.`, 'Delete Index')
  if (!ok) return
  loading.value = true
  try {
    await axios.delete(`/api/connections/${activeConn.value!.id}/search/index`, { params: { index: name } })
    toast.success(`Index "${name}" deleted`)
    searchCache.invalidate(activeConn.value!.id, 'indices')
    if (selectedIndex.value === name) selectedIndex.value = ''
    await refresh(true)
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to delete index')
  } finally {
    loading.value = false
  }
}

function compareIndices(a: SearchIndex, b: SearchIndex, field: IndexSortField) {
  switch (field) {
    case 'docs':
      return parseCount(a.docs_count) - parseCount(b.docs_count)
    case 'size':
      return indexBytes(a) - indexBytes(b)
    case 'date':
      return parseDate(a.created_at) - parseDate(b.created_at)
    case 'health':
      return healthRank(a.health || a.status) - healthRank(b.health || b.status)
    case 'kind':
      return (a.kind || '').localeCompare(b.kind || '')
    case 'name':
    default:
      return a.name.localeCompare(b.name)
  }
}

function parseCount(value: string) {
  const parsed = Number(String(value || '').replace(/,/g, ''))
  return Number.isFinite(parsed) ? parsed : 0
}

function indexBytes(index: SearchIndex) {
  if (Number.isFinite(index.store_bytes)) return Number(index.store_bytes)
  return parseCount(index.store_size)
}

function parseDate(value: string) {
  const time = new Date(value).getTime()
  return Number.isFinite(time) ? time : 0
}

function healthRank(value: string) {
  const normalized = (value || '').toLowerCase()
  if (normalized === 'green') return 3
  if (normalized === 'yellow') return 2
  if (normalized === 'red') return 1
  if (normalized === 'open') return 3
  return 0
}

function loadHit(hit: any) {
  selectedIndex.value = hit._index ?? selectedIndex.value
  docId.value = hit._id ?? ''
  docBody.value = JSON.stringify(hit._source ?? {}, null, 2)
  docResult.value = hit
  activeTab.value = 'document'
}

function resetWorkspace() {
  connected.value = false
  latencyMs.value = null
  clusterInfo.value = null
  indices.value = []
  searchResult.value = null
  docResult.value = null
}

function formatJSON(value: any) {
  return JSON.stringify(value, null, 2)
}

function buildQueryBody(parsed: any) {
  const body = parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? structuredClone(parsed) : {}
  applyTimeRange(body)
  applySort(body)
  return body
}

function applySort(body: any) {
  const field = sortField.value.trim()
  if (!field) {
    delete body.sort
    return
  }
  if (field === '_score') {
    body.sort = [{ _score: sortDirection.value }]
    return
  }
  body.sort = [{ [field]: { order: sortDirection.value, unmapped_type: field === '@timestamp' ? 'date' : 'keyword' } }]
}

function applyTimeRange(body: any) {
  const range = timeRangeClause()
  if (!range) return
  const existingQuery = body.query && typeof body.query === 'object' ? body.query : { match_all: {} }
  body.query = {
    bool: {
      must: [existingQuery],
      filter: [{ range: { '@timestamp': range } }],
    },
  }
}

function timeRangeClause() {
  if (timeRange.value === 'all') return null
  if (timeRange.value === 'custom') {
    const range: Record<string, string> = {}
    if (customTimeFrom.value) range.gte = customTimeFrom.value
    if (customTimeTo.value) range.lte = customTimeTo.value
    return Object.keys(range).length ? range : null
  }
  return { gte: `now-${timeRange.value}`, lte: 'now' }
}

function prettifyHit(hit: any): PrettyHit {
  const source = hit?._source ?? {}
  const parsedMessage = parseMessage(source.message)
  const merged = parsedMessage && typeof parsedMessage === 'object'
    ? { ...source, ...parsedMessage }
    : source

  const title = cleanMessage(
    getString(parsedMessage, 'message') ||
    getString(source, 'message') ||
    getString(merged, 'event.action') ||
    '(no message)'
  )

  const tags = [
    { label: 'app', value: getString(merged, 'app_name') || getString(merged, 'service.name') },
    { label: 'agent', value: getString(merged, 'agent.type') },
    { label: 'env', value: getString(merged, 'environment') },
    { label: 'input', value: getString(merged, 'input.type') },
  ].filter(t => t.value)

  const metrics = [
    { label: 'cpu total', value: formatMaybeNumber(getPath(merged, 'monitoring.metrics.beat.cpu.total.time.ms'), 'ms') },
    { label: 'memory', value: formatBytesValue(getPath(merged, 'monitoring.metrics.beat.memstats.rss')) },
    { label: 'events', value: formatMaybeNumber(getPath(merged, 'monitoring.metrics.libbeat.output.events.total')) },
    { label: 'load 1m', value: formatMaybeNumber(getPath(merged, 'monitoring.metrics.system.load.1')) },
  ].filter(m => m.value)

  return {
    timestamp: getString(merged, '@timestamp') || getString(source, '@timestamp'),
    level: (getString(merged, 'log.level') || getString(merged, 'log.level.keyword') || getString(merged, 'level') || 'info').toLowerCase(),
    title,
    service: getString(merged, 'service.name') || getString(merged, 'app_name') || '-',
    host: getString(merged, 'host.name') || '-',
    container: getString(merged, 'container.name') || '-',
    stream: getString(merged, 'stream') || '-',
    index: hit?._index ?? '',
    id: hit?._id ?? '',
    tags,
    metrics,
    raw: source,
  }
}

function parseMessage(value: unknown) {
  if (typeof value !== 'string') return null
  const trimmed = value.trim()
  if (!trimmed.startsWith('{') || !trimmed.endsWith('}')) return null
  try {
    return JSON.parse(trimmed)
  } catch {
    return null
  }
}

function getPath(obj: any, path: string): any {
  if (!obj || !path) return undefined
  const direct = obj[path]
  if (direct !== undefined) return direct
  return path.split('.').reduce((acc, key) => acc && typeof acc === 'object' ? acc[key] : undefined, obj)
}

function getString(obj: any, path: string): string {
  const value = getPath(obj, path)
  if (value === null || value === undefined) return ''
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return ''
}

function cleanMessage(message: string) {
  const parsed = parseMessage(message)
  if (parsed && typeof parsed === 'object') {
    return getString(parsed, 'message') || message
  }
  return message.length > 420 ? message.slice(0, 420) + '...' : message
}

function formatMaybeNumber(value: unknown, suffix = '') {
  if (value === null || value === undefined || value === '') return ''
  const num = Number(value)
  if (!Number.isFinite(num)) return String(value)
  return `${num.toLocaleString()}${suffix}`
}

function formatBytesValue(value: unknown) {
  const num = Number(value)
  if (!Number.isFinite(num) || num <= 0) return ''
  const units = ['B', 'KB', 'MB', 'GB']
  let size = num
  let unit = 0
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024
    unit += 1
  }
  return `${size.toFixed(size >= 10 || unit === 0 ? 0 : 1)} ${units[unit]}`
}

function formatTimestamp(value: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function formatIndexSize(index: SearchIndex) {
  return formatBytesValue(indexBytes(index)) || '0 B'
}

function formatIndexDate(value: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleDateString()
}

function indexKindLabel(index: SearchIndex) {
  return index.kind === 'data_stream' ? 'Data stream' : 'Index'
}
</script>

<template>
  <div class="page-shell search-root">
    <header class="search-topbar">
      <div class="search-title">
        <span class="search-logo">{{ activeConn?.driver === 'opensearch' ? 'OS' : 'ES' }}</span>
        <div>
          <h1>Search Browser</h1>
          <p>{{ activeConn ? activeConn.name : 'No Elasticsearch or OpenSearch connection selected' }}</p>
        </div>
      </div>
      <div class="search-actions">
        <select class="base-input search-select" :value="activeConnId ?? ''" @change="emit('set-conn', Number(($event.target as HTMLSelectElement).value))">
          <option value="" disabled>Select search cluster</option>
          <option v-for="conn in searchConnections" :key="conn.id" :value="conn.id">{{ conn.name }}</option>
        </select>
        <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!isSearch || loading" @click="refresh(true)">Refresh</button>
      </div>
    </header>

    <section v-if="!isSearch" class="search-empty">
      <h2>Select a search connection</h2>
      <p>Create or choose an Elasticsearch/OpenSearch connection to inspect indices, run queries, and manage documents.</p>
    </section>

    <template v-else>
      <section class="search-metrics">
        <div class="search-metric" :class="{ ok: connected }">
          <span>Status</span>
          <strong>{{ connected ? 'Connected' : 'Offline' }}</strong>
        </div>
        <div class="search-metric">
          <span>Cluster</span>
          <strong>{{ clusterInfo?.cluster_name ?? '-' }}</strong>
        </div>
        <div class="search-metric">
          <span>Version</span>
          <strong>{{ clusterInfo?.version?.number ?? '-' }}</strong>
        </div>
        <div class="search-metric">
          <span>Latency</span>
          <strong>{{ latencyMs === null ? '-' : `${latencyMs}ms` }}</strong>
        </div>
      </section>

      <section class="search-workspace">
        <aside class="search-indices">
          <div class="search-panel-head">
            <div>
              <div class="search-panel-title">Indices</div>
              <div class="search-muted">{{ indices.length }} total</div>
            </div>
          </div>
          <input v-model="indexFilter" class="base-input search-input" placeholder="Filter indices" />
          <div class="search-index-controls">
            <label>
              <span>Sort</span>
              <select v-model="indexSortField" class="base-input">
                <option value="size">Size</option>
                <option value="docs">Docs</option>
                <option value="date">Date</option>
                <option value="name">Name</option>
                <option value="health">Health</option>
                <option value="kind">Type</option>
              </select>
            </label>
            <label>
              <span>Order</span>
              <select v-model="indexSortDirection" class="base-input">
                <option value="desc">Desc</option>
                <option value="asc">Asc</option>
              </select>
            </label>
          </div>
          <label class="search-toggle">
            <input v-model="showSystemIndices" type="checkbox" />
            <span>Show system / hidden indices</span>
          </label>
          <div class="search-index-list">
            <div
              v-for="idx in filteredIndices"
              :key="idx.name"
              class="search-index-row"
              :class="{ active: selectedIndex === idx.name }"
              @click="selectIndex(idx.name)"
            >
              <span>
                <strong>{{ idx.name }}</strong>
                <small>{{ indexKindLabel(idx) }} · {{ idx.docs_count || 0 }} docs · {{ formatIndexSize(idx) }}</small>
                <small>Created {{ formatIndexDate(idx.created_at) }} · shards {{ idx.primary_shards || 0 }}/{{ idx.replica_shards || 0 }}<template v-if="idx.backing_count"> · {{ idx.backing_count }} backing</template></small>
              </span>
              <div class="search-index-row__actions" @click.stop>
                <em :class="`health-${idx.health || 'unknown'}`">{{ idx.health || idx.status || '-' }}</em>
                <button class="search-index-delete" title="Delete index" :disabled="loading" @click="deleteIndex(idx.name)">✕</button>
              </div>
            </div>
            <div v-if="!filteredIndices.length" class="search-muted search-pad">No indices found.</div>
          </div>
        </aside>

        <main class="search-main">
          <div class="search-toolbar">
            <input v-model="selectedIndex" class="base-input" placeholder="index or pattern, e.g. logs-*" />
            <div class="search-tabs">
              <button :class="{ active: activeTab === 'query' }" @click="activeTab = 'query'">Query</button>
              <button :class="{ active: activeTab === 'document' }" @click="activeTab = 'document'">Document</button>
            </div>
          </div>

          <section v-if="activeTab === 'query'" class="search-grid">
            <div class="search-editor">
              <div class="search-panel-head">
                <div class="search-panel-title">Query DSL</div>
                <button class="base-btn base-btn--primary base-btn--sm" :disabled="loading || !selectedIndex" @click="runQuery">Run</button>
              </div>
              <div class="search-query-controls">
                <label>
                  <span>Time</span>
                  <select v-model="timeRange" class="base-input">
                    <option value="15m">Last 15m</option>
                    <option value="1h">Last 1h</option>
                    <option value="24h">Last 24h</option>
                    <option value="7d">Last 7d</option>
                    <option value="30d">Last 30d</option>
                    <option value="all">All time</option>
                    <option value="custom">Custom</option>
                  </select>
                </label>
                <label v-if="timeRange === 'custom'">
                  <span>From</span>
                  <input v-model="customTimeFrom" class="base-input" placeholder="2026-05-12T00:00:00Z" />
                </label>
                <label v-if="timeRange === 'custom'">
                  <span>To</span>
                  <input v-model="customTimeTo" class="base-input" placeholder="now" />
                </label>
                <label>
                  <span>Sort</span>
                  <select v-model="sortField" class="base-input">
                    <option value="@timestamp">Date</option>
                    <option value="_score">Score</option>
                    <option value="log.offset">Log offset</option>
                    <option value="agent.name.keyword">Agent</option>
                    <option value="host.name.keyword">Host</option>
                    <option value="container.name.keyword">Container</option>
                  </select>
                </label>
                <label>
                  <span>Order</span>
                  <select v-model="sortDirection" class="base-input">
                    <option value="desc">Desc</option>
                    <option value="asc">Asc</option>
                  </select>
                </label>
                <label>
                  <span>Size</span>
                  <select v-model.number="querySize" class="base-input">
                    <option :value="25">25</option>
                    <option :value="50">50</option>
                    <option :value="100">100</option>
                    <option :value="250">250</option>
                    <option :value="500">500</option>
                  </select>
                </label>
                <label>
                  <span>From</span>
                  <input v-model.number="queryFrom" class="base-input search-num" type="number" min="0" />
                </label>
              </div>
              <textarea v-model="queryText" class="base-input search-textarea" spellcheck="false" />
            </div>

            <div class="search-results">
              <div class="search-panel-head">
                <div>
                  <div class="search-panel-title">Results</div>
                  <div class="search-muted">{{ totalHits }} hit{{ totalHits === 1 ? '' : 's' }}</div>
                </div>
              </div>
              <div v-if="prettyHits.length" class="search-hit-list">
                <div v-for="(event, idx) in prettyHits" :key="`${event.index}:${event.id}`" class="search-hit" role="button" tabindex="0" @click="loadHit(hits[idx])" @keydown.enter="loadHit(hits[idx])">
                  <div class="search-hit__head">
                    <span class="search-hit__level" :class="`level-${event.level}`">{{ event.level }}</span>
                    <span class="search-hit__time">{{ formatTimestamp(event.timestamp) }}</span>
                    <span class="search-hit__id">{{ event.index }} / {{ event.id }}</span>
                  </div>
                  <div class="search-hit__message">{{ event.title }}</div>
                  <div class="search-hit__meta">
                    <span>{{ event.service }}</span>
                    <span>{{ event.host }}</span>
                    <span>{{ event.container }}</span>
                    <span>{{ event.stream }}</span>
                  </div>
                  <div v-if="event.tags.length" class="search-hit__tags">
                    <span v-for="tag in event.tags" :key="`${event.id}:${tag.label}`">{{ tag.label }}: {{ tag.value }}</span>
                  </div>
                  <div v-if="event.metrics.length" class="search-hit__metrics">
                    <span v-for="metric in event.metrics" :key="`${event.id}:${metric.label}`">
                      <small>{{ metric.label }}</small>
                      <strong>{{ metric.value }}</strong>
                    </span>
                  </div>
                  <details class="search-hit__raw" @click.stop>
                    <summary>Raw source</summary>
                    <pre>{{ formatJSON(event.raw) }}</pre>
                  </details>
                </div>
              </div>
              <pre v-else class="search-json">{{ searchResult ? formatJSON(searchResult) : 'Run a query to see results.' }}</pre>
            </div>
          </section>

          <section v-else class="search-grid">
            <div class="search-editor">
              <div class="search-panel-head">
                <div class="search-panel-title">Document</div>
                <div class="search-inline">
                  <input v-model="docId" class="base-input search-doc-id" placeholder="document id" />
                  <button class="base-btn base-btn--ghost base-btn--sm" :disabled="loading || !docId || !selectedIndex" @click="readDocument">Read</button>
                  <button class="base-btn base-btn--primary base-btn--sm" :disabled="loading || !selectedIndex" @click="saveDocument">Save</button>
                  <button class="base-btn base-btn--danger base-btn--sm" :disabled="loading || !docId || !selectedIndex" @click="deleteDocument">Delete</button>
                </div>
              </div>
              <textarea v-model="docBody" class="base-input search-textarea" spellcheck="false" />
            </div>

            <div class="search-results">
              <div class="search-panel-head">
                <div class="search-panel-title">Response</div>
              </div>
              <pre class="search-json">{{ docResult ? formatJSON(docResult) : 'Document API response appears here.' }}</pre>
            </div>
          </section>
        </main>
      </section>
    </template>
  </div>
</template>

<style scoped>
.search-root { background: var(--bg-body); padding: 18px; gap: 14px; }
.search-topbar, .search-workspace, .search-metrics { display: flex; gap: 12px; }
.search-topbar { align-items: center; justify-content: space-between; }
.search-title { display: flex; align-items: center; gap: 12px; }
.search-title h1 { margin: 0; font-size: 20px; color: var(--text-primary); }
.search-title p { margin: 2px 0 0; font-size: 12px; color: var(--text-muted); }
.search-logo { width: 38px; height: 38px; border-radius: 8px; background: #00bfb3; color: #fff; display: grid; place-items: center; font-weight: 800; font-size: 12px; }
.search-actions { display: flex; align-items: center; gap: 8px; }
.search-select { width: 240px; }
.search-empty { border: 1px solid var(--border); background: var(--bg-elevated); border-radius: 8px; padding: 36px; text-align: center; color: var(--text-muted); }
.search-empty h2 { margin: 0 0 6px; color: var(--text-primary); font-size: 16px; }
.search-metrics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); }
.search-metric { border: 1px solid var(--border); background: var(--bg-elevated); border-radius: 8px; padding: 12px; display: flex; flex-direction: column; gap: 5px; }
.search-metric span, .search-muted { color: var(--text-muted); font-size: 11px; }
.search-metric strong { color: var(--text-primary); font-size: 15px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.search-metric.ok strong { color: var(--success); }
.search-workspace { min-height: 0; flex: 1; }
.search-indices, .search-main, .search-editor, .search-results { border: 1px solid var(--border); background: var(--bg-elevated); border-radius: 8px; min-width: 0; }
.search-indices { width: 320px; padding: 12px; display: flex; flex-direction: column; gap: 10px; }
.search-main { flex: 1; padding: 12px; display: flex; flex-direction: column; gap: 12px; }
.search-panel-head, .search-toolbar, .search-inline { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.search-panel-title { color: var(--text-primary); font-weight: 700; font-size: 13px; }
.search-query-controls { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px; }
.search-query-controls label { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.search-query-controls label span { color: var(--text-muted); font-size: 10.5px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.03em; }
.search-query-controls .base-input { height: 32px; min-width: 0; }
.search-input { height: 32px; }
.search-index-controls { display: grid; grid-template-columns: 1fr 92px; gap: 8px; }
.search-index-controls label { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.search-index-controls label span { color: var(--text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.03em; }
.search-index-controls .base-input { height: 32px; min-width: 0; }
.search-toggle { display: flex; align-items: center; gap: 7px; color: var(--text-muted); font-size: 11px; }
.search-toggle input { accent-color: #00bfb3; }
.search-index-list { overflow: auto; display: flex; flex-direction: column; gap: 5px; }
.search-index-row { text-align: left; border: 1px solid var(--border); background: var(--bg-body); color: var(--text-primary); border-radius: 6px; padding: 8px; display: flex; align-items: flex-start; justify-content: space-between; gap: 8px; cursor: pointer; }
.search-index-row.active, .search-index-row:hover { border-color: #00bfb3; }
.search-index-row span { min-width: 0; display: flex; flex-direction: column; gap: 3px; }
.search-index-row strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }
.search-index-row small { color: var(--text-muted); font-size: 10.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.search-index-row em { font-style: normal; font-size: 10px; font-weight: 700; text-transform: uppercase; }
.search-index-row__actions { display: flex; align-items: center; gap: 6px; flex-shrink: 0; }
.search-index-delete { border: 0; background: transparent; color: var(--text-muted); cursor: pointer; font-size: 11px; padding: 2px 4px; border-radius: 4px; line-height: 1; opacity: 0; transition: opacity 0.15s, color 0.15s, background 0.15s; }
.search-index-row:hover .search-index-delete { opacity: 1; }
.search-index-delete:hover { color: var(--danger); background: color-mix(in srgb, var(--danger) 12%, transparent); }
.health-green { color: var(--success); }
.health-yellow { color: var(--warning); }
.health-red { color: var(--danger); }
.search-tabs { display: flex; border: 1px solid var(--border); border-radius: 7px; overflow: hidden; flex-shrink: 0; }
.search-tabs button { border: 0; background: var(--bg-body); color: var(--text-muted); padding: 7px 12px; cursor: pointer; }
.search-tabs button.active { background: #00bfb3; color: #fff; }
.search-grid { display: grid; grid-template-columns: minmax(280px, 0.9fr) minmax(320px, 1.1fr); gap: 12px; min-height: 0; flex: 1; }
.search-editor, .search-results { padding: 12px; display: flex; flex-direction: column; gap: 10px; }
.search-textarea { flex: 1; min-height: 420px; resize: none; font-family: var(--mono); font-size: 12px; line-height: 1.6; }
.search-num { width: 72px; }
.search-doc-id { width: 180px; }
.search-json, .search-hit pre { margin: 0; white-space: pre-wrap; word-break: break-word; font-family: var(--mono); font-size: 11.5px; line-height: 1.55; color: var(--text-secondary); }
.search-results { overflow: hidden; }
.search-json { overflow: auto; background: var(--bg-body); border: 1px solid var(--border); border-radius: 6px; padding: 10px; flex: 1; }
.search-hit-list { overflow: auto; display: flex; flex-direction: column; gap: 8px; }
.search-hit { text-align: left; border: 1px solid var(--border); background: var(--bg-body); color: var(--text-primary); border-radius: 7px; padding: 10px; cursor: pointer; display: flex; flex-direction: column; gap: 8px; }
.search-hit:hover { border-color: #00bfb3; }
.search-hit__head, .search-hit__meta, .search-hit__tags, .search-hit__metrics { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.search-hit__level { font-size: 10px; font-weight: 800; text-transform: uppercase; border-radius: 4px; padding: 2px 6px; background: var(--bg-elevated); color: var(--text-muted); border: 1px solid var(--border); }
.level-error, .level-fatal, .level-critical { color: var(--danger); background: color-mix(in srgb, var(--danger) 12%, transparent); border-color: color-mix(in srgb, var(--danger) 28%, transparent); }
.level-warn, .level-warning { color: var(--warning); background: color-mix(in srgb, var(--warning) 12%, transparent); border-color: color-mix(in srgb, var(--warning) 28%, transparent); }
.level-info { color: #00a69c; background: rgba(0, 191, 179, 0.12); border-color: rgba(0, 191, 179, 0.28); }
.search-hit__time, .search-hit__id { color: var(--text-muted); font-size: 10.5px; font-family: var(--mono); }
.search-hit__id { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 100%; }
.search-hit__message { color: var(--text-primary); font-size: 13px; line-height: 1.55; }
.search-hit__meta span, .search-hit__tags span { color: var(--text-muted); font-size: 10.5px; background: var(--bg-elevated); border: 1px solid var(--border); border-radius: 4px; padding: 2px 6px; max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.search-hit__metrics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); }
.search-hit__metrics span { border: 1px solid var(--border); background: var(--bg-elevated); border-radius: 6px; padding: 6px; display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.search-hit__metrics small { color: var(--text-muted); font-size: 10px; }
.search-hit__metrics strong { color: var(--text-primary); font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.search-hit__raw { border-top: 1px solid var(--border); padding-top: 6px; }
.search-hit__raw summary { color: var(--text-muted); cursor: pointer; font-size: 11px; }
.search-hit__raw pre { margin-top: 8px; max-height: 260px; overflow: auto; background: var(--bg-elevated); border: 1px solid var(--border); border-radius: 6px; padding: 8px; }
.search-pad { padding: 12px; }
@media (max-width: 980px) {
  .search-topbar, .search-actions, .search-workspace, .search-panel-head { flex-direction: column; align-items: stretch; }
  .search-metrics, .search-grid { grid-template-columns: 1fr; }
  .search-query-controls { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .search-hit__metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .search-indices { width: auto; }
  .search-select { width: 100%; }
}
</style>
