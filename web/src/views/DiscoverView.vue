<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import { useToast } from '@/composables/useToast'

const props = defineProps<{ activeConnId: number | null }>()
const emit = defineEmits<{ (e: 'set-conn', id: number): void }>()

type TimeRange = '5m' | '15m' | '1h' | '6h' | '24h' | '7d' | '30d' | 'custom'
type AutoRefreshInterval = 0 | 5 | 10 | 30 | 60

interface HistogramBucket { key: number; key_as_string: string; doc_count: number }
interface FieldInfo { name: string; type: string }
interface FieldValue { key: string; doc_count: number }
interface Hit { _index: string; _id: string; _source: Record<string, any>; _score: number }

const { connections, fetchConnections } = useConnections()
const toast = useToast()

const searchConnections = computed(() => connections.value.filter(c => c.driver === 'elasticsearch' || c.driver === 'opensearch'))
const activeConn = computed(() => props.activeConnId != null ? connections.value.find(c => c.id === props.activeConnId) ?? null : null)
const isSearch = computed(() => activeConn.value?.driver === 'elasticsearch' || activeConn.value?.driver === 'opensearch')

// Controls
const indexPattern = ref('')
const searchText = ref('')
const timeRange = ref<TimeRange>('24h')
const customFrom = ref('')
const customTo = ref('')
const autoRefresh = ref<AutoRefreshInterval>(0)
const pageSize = ref(50)
const timestampField = ref('@timestamp')

const QUICK_PATTERNS = [
  { label: 'Filebeat', pattern: 'filebeat-*', fields: ['@timestamp','app_name','environment','message'] },
]

function applyQuickPattern(p: typeof QUICK_PATTERNS[number]) {
  indexPattern.value = p.pattern
  selectedFields.value = [...p.fields]
  run()
}

// State
const loading = ref(false)
const histogram = ref<HistogramBucket[]>([])
const hits = ref<Hit[]>([])
const totalHits = ref(0)
const fields = ref<FieldInfo[]>([])
const selectedFields = ref<string[]>(['@timestamp', 'log.level', 'message'])
const expandedHits = ref<Set<string>>(new Set())
const selectedHit      = ref<{ hit: Hit; key: string } | null>(null)
const showTechnical    = ref(false)
const detailFullscreen = ref(false)

// ── Message beautifier ─────────────────────────────────────
interface MsgToken { type: 'key'|'str'|'num'|'bool'|'null'|'punc'|'plain'; text: string }

function parseJsonTokens(json: string): MsgToken[] {
  const tokens: MsgToken[] = []
  let i = 0
  while (i < json.length) {
    // Skip whitespace (keep it as plain)
    let ws = ''
    while (i < json.length && /\s/.test(json[i])) ws += json[i++]
    if (ws) { tokens.push({ type: 'plain', text: ws }); continue }

    const ch = json[i]
    // String
    if (ch === '"') {
      let s = '"'; i++
      while (i < json.length) {
        if (json[i] === '\\') { s += json[i] + (json[i+1] ?? ''); i += 2 }
        else if (json[i] === '"') { s += '"'; i++; break }
        else { s += json[i++] }
      }
      // Peek ahead: if next non-ws char is ':', it's a key
      let j = i; while (j < json.length && /\s/.test(json[j])) j++
      tokens.push({ type: json[j] === ':' ? 'key' : 'str', text: s })
      continue
    }
    // Number
    const numM = json.slice(i).match(/^-?\d+(\.\d+)?([eE][+-]?\d+)?/)
    if (numM) { tokens.push({ type: 'num', text: numM[0] }); i += numM[0].length; continue }
    // Bool / null
    const kw = ['true','false','null'].find(k => json.startsWith(k, i))
    if (kw) { tokens.push({ type: kw === 'null' ? 'null' : 'bool', text: kw }); i += kw.length; continue }
    // Punctuation
    if ('{}[],:'.includes(ch)) { tokens.push({ type: 'punc', text: ch }); i++; continue }
    // Fallback
    tokens.push({ type: 'plain', text: ch }); i++
  }
  return tokens
}

function beautifyMessage(raw: string): { isJson: boolean; tokens: MsgToken[]; plain: string } {
  if (!raw || raw === '—') return { isJson: false, tokens: [], plain: raw }
  const trimmed = raw.trim()
  // Try JSON pretty-print
  if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
    try {
      const parsed = JSON.parse(trimmed)
      const pretty = JSON.stringify(parsed, null, 2)
      return { isJson: true, tokens: parseJsonTokens(pretty), plain: pretty }
    } catch { /* not valid JSON */ }
  }
  // Laravel-style: [2026-05-12 23:45:01] env.LEVEL: message context
  const laravelRe = /^\[(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\] (\w+)\.(\w+):\s*([\s\S]*)$/
  const lm = trimmed.match(laravelRe)
  if (lm) {
    const [, ts, env, level, body] = lm
    const tokens: MsgToken[] = [
      { type: 'punc',  text: '[' },
      { type: 'num',   text: ts },
      { type: 'punc',  text: '] ' },
      { type: 'str',   text: env },
      { type: 'punc',  text: '.' },
      { type: level.toLowerCase() === 'error' ? 'null' : level.toLowerCase() === 'warning' ? 'bool' : 'key', text: level },
      { type: 'punc',  text: ': ' },
      { type: 'plain', text: body.trim() },
    ]
    return { isJson: false, tokens, plain: trimmed }
  }
  return { isJson: false, tokens: [], plain: trimmed }
}

const detailMsg = computed(() => {
  if (!selectedHit.value) return { isJson: false, tokens: [] as MsgToken[], plain: '' }
  const raw = getPath(selectedHit.value.hit._source, 'message')
    || getPath(selectedHit.value.hit._source, 'msg') || '—'
  return beautifyMessage(raw)
})

// Fields that are Filebeat/ECS infrastructure metadata — useful only rarely.
// Shown only when "Show technical fields" is toggled on.
const TECHNICAL_PREFIXES = [
  'agent.', 'ecs.', 'input.', 'log.file.device_id', 'log.file.inode',
  'log.offset', '_seq_no', '_primary_term',
]
function isTechnical(key: string) {
  return TECHNICAL_PREFIXES.some(p => key === p || key.startsWith(p))
}

// Fields already shown prominently in the header / meta row — skip in field list
const DETAIL_PROMOTED = new Set([
  '@timestamp', 'message', 'msg', 'log.level', 'level',
  'app_name', 'service.name', 'environment', 'host.name',
])

const detailContextFields = computed(() => {
  if (!selectedHit.value) return {} as Record<string, string>
  const all = flatSource(selectedHit.value.hit._source)
  return Object.fromEntries(
    Object.entries(all).filter(([k]) => !DETAIL_PROMOTED.has(k) && !isTechnical(k))
  )
})

const detailTechnicalFields = computed(() => {
  if (!selectedHit.value) return {} as Record<string, string>
  const all = flatSource(selectedHit.value.hit._source)
  return Object.fromEntries(
    Object.entries(all).filter(([k]) => isTechnical(k))
  )
})
const fieldValues = ref<Record<string, FieldValue[]>>({})
const loadingFieldValues = ref<Set<string>>(new Set())
const fieldFilter = ref('')
const refreshTimer = ref<ReturnType<typeof setInterval> | null>(null)
const lastRefreshed = ref<Date | null>(null)
const streamEl = ref<HTMLElement | null>(null)

// ── Filter & Sort ──────────────────────────────────────────
type LevelFilter = 'all' | 'error' | 'warn' | 'info' | 'debug'
type SortOrder   = 'desc' | 'asc'
const levelFilter = ref<LevelFilter>('all')
const envFilter   = ref('')          // '' | 'production' | 'sandbox'
const appFilter   = ref('')          // specific app_name
const sortOrder   = ref<SortOrder>('desc')
const appNames     = ref<string[]>([])
const appSearch    = ref('')
const showControls = ref(true)   // collapse top controls to reclaim space

// ── Pagination ──────────────────────────────────────────────
const currentPage = ref(1)
const totalPages  = computed(() => Math.max(1, Math.ceil(totalHits.value / pageSize.value)))
const pageFrom    = computed(() => (currentPage.value - 1) * pageSize.value)
const pageTo      = computed(() => Math.min(currentPage.value * pageSize.value, totalHits.value))

// Visible page window (up to 7 buttons)
const pageWindow = computed(() => {
  const total = totalPages.value
  if (total <= 7) return Array.from({ length: total }, (_, i) => i + 1)
  const cur = currentPage.value
  const pages: (number | '…')[] = [1]
  const lo = Math.max(2, cur - 2)
  const hi = Math.min(total - 1, cur + 2)
  if (lo > 2) pages.push('…')
  for (let p = lo; p <= hi; p++) pages.push(p)
  if (hi < total - 1) pages.push('…')
  pages.push(total)
  return pages
})

// Chips: active filters the user can individually remove
const activeFilters = computed(() => {
  const chips: { label: string; clear: () => void }[] = []
  if (levelFilter.value !== 'all') chips.push({ label: `level: ${levelFilter.value}`, clear: () => { levelFilter.value = 'all'; run() } })
  if (envFilter.value)             chips.push({ label: `env: ${envFilter.value}`,     clear: () => { envFilter.value = '';    run() } })
  if (appFilter.value)             chips.push({ label: `app: ${appFilter.value}`,     clear: () => { appFilter.value = '';   run() } })
  if (searchText.value.trim())     chips.push({ label: `"${searchText.value.trim()}"`,clear: () => { searchText.value = '';  run() } })
  return chips
})

function clearAllFilters() {
  levelFilter.value = 'all'; envFilter.value = ''; appFilter.value = ''; searchText.value = ''
  currentPage.value = 1; run()
}

function goToPage(p: number | '…') {
  if (p === '…' || typeof p !== 'number') return
  currentPage.value = p; run(true)
}

const filteredFields = computed(() => {
  const q = fieldFilter.value.trim().toLowerCase()
  if (!q) return fields.value
  return fields.value.filter(f => f.name.toLowerCase().includes(q) || f.type.toLowerCase().includes(q))
})

const histogramMax = computed(() => Math.max(...histogram.value.map(b => b.doc_count), 1))

const timeRangeLabel: Record<TimeRange, string> = {
  '5m': 'Last 5 min', '15m': 'Last 15 min', '1h': 'Last 1 hour',
  '6h': 'Last 6 hours', '24h': 'Last 24 hours', '7d': 'Last 7 days',
  '30d': 'Last 30 days', 'custom': 'Custom',
}

onMounted(async () => {
  if (!connections.value.length) await fetchConnections()
  if (!isSearch.value && searchConnections.value.length === 1) {
    emit('set-conn', searchConnections.value[0].id)
    return
  }
  if (isSearch.value && activeConn.value?.database) {
    indexPattern.value = activeConn.value.database
    await run()
  }
})

watch(() => props.activeConnId, async () => {
  resetAll()
  if (isSearch.value && activeConn.value?.database) {
    indexPattern.value = activeConn.value.database
    await run()
  }
})

watch(autoRefresh, (interval) => {
  clearTimer()
  if (interval > 0) {
    refreshTimer.value = setInterval(run, interval * 1000)
  }
})

onBeforeUnmount(clearTimer)

function clearTimer() {
  if (refreshTimer.value) { clearInterval(refreshTimer.value); refreshTimer.value = null }
}

function resetAll() {
  histogram.value = []
  hits.value = []
  totalHits.value = 0
  fields.value = []
  fieldValues.value = {}
  expandedHits.value = new Set()
  appNames.value = []
  appFilter.value = ''
}

function buildQuery(): any {
  const clauses: any[] = []
  const timeClause = buildTimeClause()
  if (timeClause) clauses.push({ range: { [timestampField.value]: timeClause } })
  if (searchText.value.trim())
    clauses.push({ query_string: { query: searchText.value.trim(), lenient: true } })
  if (envFilter.value)
    // Try .keyword sub-field, direct keyword field, and analyzed text
    clauses.push({ bool: { should: [
      { term:         { 'environment.keyword': envFilter.value } },
      { term:         { environment: envFilter.value } },
      { match_phrase: { environment: envFilter.value } },
    ], minimum_should_match: 1 } })
  if (appFilter.value)
    clauses.push({ bool: { should: [
      { term:         { 'app_name.keyword': appFilter.value } },
      { term:         { app_name: appFilter.value } },
      { match_phrase: { app_name: appFilter.value } },
    ], minimum_should_match: 1 } })
  if (levelFilter.value !== 'all') {
    // Try both top-level log.level keyword and query_string for JSON-embedded level
    clauses.push({ bool: { should: [
      { term: { 'log.level.keyword': levelFilter.value } },
      { term: { 'log.level': levelFilter.value } },
      { match_phrase: { message: `"log.level":"${levelFilter.value}"` } },
    ], minimum_should_match: 1 } })
  }
  if (!clauses.length) return { match_all: {} }
  return { bool: { filter: clauses } }
}

// Same as buildQuery but without the appFilter clause — used for the app
// aggregation so the dropdown always shows all available apps.
function buildQueryWithoutApp(): any {
  const saved = appFilter.value
  appFilter.value = ''
  const q = buildQuery()
  appFilter.value = saved
  return q
}

function buildTimeClause(): Record<string, string> | null {
  if (timeRange.value === 'custom') {
    const r: Record<string, string> = {}
    if (customFrom.value) r.gte = customFrom.value
    if (customTo.value) r.lte = customTo.value
    return Object.keys(r).length ? r : null
  }
  return { gte: `now-${timeRange.value}`, lte: 'now' }
}

function histogramInterval(): string {
  const map: Record<TimeRange, string> = {
    '5m': '30s', '15m': '1m', '1h': '5m', '6h': '30m',
    '24h': '1h', '7d': '12h', '30d': '1d', 'custom': '1h',
  }
  return map[timeRange.value] ?? '1h'
}

async function run(keepPage = false) {
  if (!activeConn.value || !indexPattern.value.trim()) return
  if (!keepPage) currentPage.value = 1
  loading.value = true
  try {
    const query    = buildQuery()
    // Base query without appFilter — used for the app_name aggregation so the
    // full list of apps is always visible regardless of the active app filter.
    const baseQuery = buildQueryWithoutApp()
    const interval  = histogramInterval()
    const [histResult, hitsResult, appResult] = await Promise.all([
      axios.post(`/api/connections/${activeConn.value.id}/search/aggregate`, {
        index: indexPattern.value.trim(),
        query,
        size: 0,
        aggs: {
          over_time: {
            date_histogram: {
              field: timestampField.value,
              fixed_interval: interval,
              min_doc_count: 0,
              extended_bounds: { min: `now-${timeRange.value === 'custom' ? '24h' : timeRange.value}`, max: 'now' },
            },
          },
        },
      }),
      axios.post(`/api/connections/${activeConn.value.id}/search/aggregate`, {
        index: indexPattern.value.trim(),
        query,
        size: pageSize.value,
        from: pageFrom.value,
        aggs: {},
        sort: [{ [timestampField.value]: { order: sortOrder.value } }],
      }),
      axios.post(`/api/connections/${activeConn.value.id}/search/aggregate`, {
        index: indexPattern.value.trim(),
        query: baseQuery,
        size: 0,
        aggs: {
          apps:      { terms: { field: 'app_name.keyword', size: 50, order: { _count: 'desc' } } },
          apps_plain:{ terms: { field: 'app_name',         size: 50, order: { _count: 'desc' } } },
        },
      }),
    ])
    histogram.value = histResult.data?.aggregations?.over_time?.buckets ?? []
    const rawHits = hitsResult.data?.hits?.hits ?? []
    hits.value = rawHits
    const total = hitsResult.data?.hits?.total
    totalHits.value = typeof total === 'number' ? total : (total?.value ?? rawHits.length)
    lastRefreshed.value = new Date()
    // Populate app list; keep existing list if the index has no app_name field
    const appBuckets: { key: string }[] =
      (appResult.data?.aggregations?.apps?.buckets?.length
        ? appResult.data.aggregations.apps.buckets
        : appResult.data?.aggregations?.apps_plain?.buckets) ?? []
    if (appBuckets.length) appNames.value = appBuckets.map(b => b.key)

    // Scroll stream back to top after every page load
    await nextTick()
    streamEl.value?.scrollTo({ top: 0, behavior: 'smooth' })

    // Load field list from mapping if empty
    if (!fields.value.length) await loadFields()
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Discover query failed')
  } finally {
    loading.value = false
  }
}

async function loadFields() {
  if (!activeConn.value || !indexPattern.value.trim()) return
  try {
    const { data } = await axios.get<FieldInfo[]>(`/api/connections/${activeConn.value.id}/search/fields`, {
      params: { index: indexPattern.value.trim() },
    })
    fields.value = data.sort((a, b) => a.name.localeCompare(b.name))
  } catch {
    // ignore — fields sidebar is optional
  }
}

async function loadFieldValues(fieldName: string) {
  if (!activeConn.value || !indexPattern.value.trim()) return
  if (loadingFieldValues.value.has(fieldName)) return
  loadingFieldValues.value.add(fieldName)
  try {
    const kw = fieldName.includes('.') ? fieldName : fieldName + '.keyword'
    const query = buildQuery()
    const { data } = await axios.post(`/api/connections/${activeConn.value.id}/search/aggregate`, {
      index: indexPattern.value.trim(),
      query,
      size: 0,
      aggs: { top_values: { terms: { field: kw, size: 8 } } },
    })
    const buckets = data?.aggregations?.top_values?.buckets ?? []
    fieldValues.value = { ...fieldValues.value, [fieldName]: buckets }
  } catch {
    // Try without .keyword suffix
    try {
      const query = buildQuery()
      const { data } = await axios.post(`/api/connections/${activeConn.value.id}/search/aggregate`, {
        index: indexPattern.value.trim(),
        query,
        size: 0,
        aggs: { top_values: { terms: { field: fieldName, size: 8 } } },
      })
      const buckets = data?.aggregations?.top_values?.buckets ?? []
      fieldValues.value = { ...fieldValues.value, [fieldName]: buckets }
    } catch { /* field not aggregatable */ }
  } finally {
    loadingFieldValues.value.delete(fieldName)
  }
}

function toggleHit(id: string, hit?: Hit) {
  if (selectedHit.value?.key === id) {
    selectedHit.value = null
    return
  }
  if (hit) { selectedHit.value = { hit, key: id }; showTechnical.value = false }
  expandedHits.value = new Set(id ? [id] : [])
}

function closeDetail() {
  selectedHit.value = null
  expandedHits.value = new Set()
  detailFullscreen.value = false
}

function toggleField(name: string) {
  const idx = selectedFields.value.indexOf(name)
  if (idx >= 0) selectedFields.value.splice(idx, 1)
  else selectedFields.value.push(name)
}

function pinField(name: string) {
  if (!selectedFields.value.includes(name)) selectedFields.value.push(name)
  loadFieldValues(name)
}

function addFilter(field: string, value: string) {
  if (field === 'environment') { envFilter.value = value; run(); return }
  if (field === 'app_name')    { appFilter.value = value; run(); return }
  const clause = `${field}:"${value}"`
  searchText.value = searchText.value ? `${searchText.value} AND ${clause}` : clause
  run()
}

function getFieldValue(source: Record<string, any>, field: string): string {
  const direct = source[field]
  if (direct !== undefined) return formatValue(direct)
  // support dot-notation
  const parts = field.split('.')
  let cur: any = source
  for (const p of parts) {
    if (cur == null || typeof cur !== 'object') return '-'
    cur = cur[p]
  }
  return cur !== undefined ? formatValue(cur) : '-'
}

function formatValue(v: any): string {
  if (v === null || v === undefined) return '-'
  if (typeof v === 'string') return v
  if (typeof v === 'number' || typeof v === 'boolean') return String(v)
  return JSON.stringify(v)
}

function formatTimestamp(v: string) {
  if (!v) return '-'
  const d = new Date(v)
  return isNaN(d.getTime()) ? v : d.toLocaleString()
}

function levelClass(source: Record<string, any>): string {
  const level = parsedLevel(source)
  if (['error', 'fatal', 'critical'].includes(level)) return 'level-error'
  if (['warn', 'warning'].includes(level)) return 'level-warn'
  if (level === 'debug') return 'level-debug'
  if (level === 'trace') return 'level-trace'
  return 'level-info'
}

function getPath(obj: any, path: string): string {
  if (!obj) return ''
  const direct = obj[path]
  if (direct !== undefined) return String(direct)
  return path.split('.').reduce((acc: any, key) => acc && typeof acc === 'object' ? acc[key] : undefined, obj) ?? ''
}

function hitMessage(source: Record<string, any>): string {
  const raw = getPath(source, 'message') || getPath(source, 'msg') || ''
  // If the message is a JSON string (common in filebeat container logs),
  // extract the inner "message" field for a cleaner display.
  if (raw.startsWith('{')) {
    try {
      const parsed = JSON.parse(raw)
      const inner = parsed.message || parsed.msg || parsed.log || raw
      const level = parsed['log.level'] || parsed.level || ''
      return level ? `[${level.toUpperCase()}] ${inner}` : String(inner)
    } catch { /* not JSON */ }
  }
  return raw.length > 300 ? raw.slice(0, 300) + '…' : raw
}

function parsedLevel(source: Record<string, any>): string {
  const direct = getPath(source, 'log.level') || getPath(source, 'level')
  if (direct) return direct.toLowerCase()
  // Try parsing the message JSON for filebeat-style logs
  const raw = getPath(source, 'message') || ''
  if (raw.startsWith('{')) {
    try {
      const p = JSON.parse(raw)
      return (p['log.level'] || p.level || '').toLowerCase()
    } catch { /* not JSON */ }
  }
  return ''
}

function formatBarTime(bucket: HistogramBucket): string {
  const d = new Date(bucket.key)
  return isNaN(d.getTime()) ? bucket.key_as_string : d.toLocaleTimeString()
}

function fieldTypeClass(type: string): string {
  const map: Record<string, string> = {
    text: 'ft-text', keyword: 'ft-keyword', date: 'ft-date',
    long: 'ft-num', integer: 'ft-num', float: 'ft-num', double: 'ft-num',
    boolean: 'ft-bool', object: 'ft-obj', nested: 'ft-obj',
    ip: 'ft-ip', geo_point: 'ft-geo',
  }
  return map[type] ?? 'ft-other'
}

function hitKey(hit: Hit, idx: number): string {
  return `${hit._index}:${hit._id}:${idx}`
}
</script>

<template>
  <div class="disc-root">

    <!-- ── Toolbar ─────────────────────────────────────────── -->
    <div class="disc-toolbar">
      <div class="disc-toolbar-left">
        <select class="base-input disc-conn-sel" :value="activeConnId ?? ''" @change="emit('set-conn', Number(($event.target as HTMLSelectElement).value))">
          <option value="" disabled>Cluster…</option>
          <option v-for="c in searchConnections" :key="c.id" :value="c.id">{{ c.name }}</option>
        </select>
        <div class="disc-quick-row">
          <button v-for="p in QUICK_PATTERNS" :key="p.pattern"
            class="disc-qp" :class="{ active: indexPattern === p.pattern }"
            @click="applyQuickPattern(p)">{{ p.label }}</button>
        </div>
      </div>
      <div class="disc-toolbar-right">
        <select v-model.number="autoRefresh" class="base-input disc-rf-sel" title="Auto-refresh">
          <option :value="0">Off</option>
          <option :value="5">5s</option>
          <option :value="10">10s</option>
          <option :value="30">30s</option>
          <option :value="60">1m</option>
        </select>
        <span v-if="autoRefresh > 0" class="disc-live" title="Live" />
      </div>
    </div>

    <!-- ── Search bar ──────────────────────────────────────── -->
    <div class="disc-searchbar">
      <input v-model="indexPattern" class="base-input disc-idx-input"
        placeholder="Index pattern — e.g. filebeat-*, logs-*"
        @keydown.enter="run" />
      <div class="disc-search-wrap">
        <input v-model="searchText" class="base-input disc-search-input"
          placeholder='Filter logs — e.g. app_name:"boss" AND environment:"production"'
          @keydown.enter="run" />
      </div>
      <select v-model="timeRange" class="base-input disc-time-sel">
        <option v-for="(label, val) in timeRangeLabel" :key="val" :value="val">{{ label }}</option>
      </select>
      <template v-if="timeRange === 'custom'">
        <input v-model="customFrom" class="base-input disc-custom-dt" placeholder="from" />
        <input v-model="customTo"   class="base-input disc-custom-dt" placeholder="to" />
      </template>
      <button class="base-btn base-btn--primary disc-run-btn"
        :disabled="!indexPattern.trim() || loading" @click="run">
        {{ loading ? '…' : 'Search' }}
      </button>
    </div>

    <section v-if="!isSearch" class="disc-placeholder">
      <p>Select an Elasticsearch / OpenSearch connection to start exploring logs.</p>
    </section>

    <template v-else>

      <!-- ── Compact control bar (always visible) ────────── -->
      <div class="disc-ctrlbar">
        <!-- Left: hits count + range + chips -->
        <div class="disc-ctrlbar-left">
          <span class="disc-hits-count">
            <strong>{{ totalHits.toLocaleString() }}</strong>
          </span>
          <span v-if="totalHits > 0" class="disc-strip-muted disc-range">
            {{ (pageFrom + 1).toLocaleString() }}–{{ pageTo.toLocaleString() }}
          </span>
          <span v-if="lastRefreshed" class="disc-strip-muted disc-updated">
            {{ lastRefreshed.toLocaleTimeString() }}
          </span>
          <!-- Active chips inline -->
          <template v-if="activeFilters.length">
            <span class="disc-sep-dot">·</span>
            <span v-for="chip in activeFilters" :key="chip.label" class="disc-chip-sm">
              {{ chip.label }}<button class="disc-chip-rm" @click="chip.clear()">×</button>
            </span>
            <button class="disc-clear-sm" @click="clearAllFilters">Clear all</button>
          </template>
        </div>
        <!-- Right: controls toggle + rows -->
        <div class="disc-ctrlbar-right">
          <select v-model.number="pageSize" class="base-input disc-rows-sel"
            @change="currentPage = 1; run()">
            <option :value="25">25</option>
            <option :value="50">50</option>
            <option :value="100">100</option>
            <option :value="250">250</option>
          </select>
          <button class="disc-toggle-ctrl" :class="{ active: showControls }"
            :title="showControls ? 'Hide filters' : 'Show filters'"
            @click="showControls = !showControls">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2">
              <line x1="4" y1="6"  x2="20" y2="6"/><line x1="8" y1="12" x2="20" y2="12"/>
              <line x1="12" y1="18" x2="20" y2="18"/>
              <circle cx="4"  cy="6"  r="2" fill="currentColor" stroke="none"/>
              <circle cx="8"  cy="12" r="2" fill="currentColor" stroke="none"/>
              <circle cx="12" cy="18" r="2" fill="currentColor" stroke="none"/>
            </svg>
            Filters{{ activeFilters.length ? ` (${activeFilters.length})` : '' }}
          </button>
        </div>
      </div>

      <!-- ── Collapsible: filters + histogram ───────────── -->
      <Transition name="ctrl-collapse">
        <div v-if="showControls" class="disc-controls-panel">

          <!-- Filter row -->
          <div class="disc-filterbar">
            <div class="disc-filter-group">
              <span class="disc-filter-label">Level</span>
              <div class="disc-btn-group">
                <button v-for="lv in (['all','error','warn','info','debug'] as const)" :key="lv"
                  class="disc-fg-btn"
                  :class="{ active: levelFilter === lv, [`lvl-${lv}`]: lv !== 'all' }"
                  @click="levelFilter = lv; run()">
                  {{ lv === 'all' ? 'All' : lv.toUpperCase() }}
                </button>
              </div>
            </div>

            <div v-if="appNames.length" class="disc-filter-group">
              <span class="disc-filter-label">App</span>
              <div class="disc-app-picker" :class="{ 'has-value': appFilter }">
                <select class="disc-app-select" :value="appFilter"
                  @change="appFilter = ($event.target as HTMLSelectElement).value; run()">
                  <option value="">All apps</option>
                  <option v-for="app in appNames" :key="app" :value="app">{{ app }}</option>
                </select>
                <button v-if="appFilter" class="disc-app-clear" title="Clear"
                  @click.stop="appFilter = ''; run()">×</button>
              </div>
            </div>

            <div class="disc-filter-group">
              <span class="disc-filter-label">Sort</span>
              <div class="disc-btn-group">
                <button class="disc-fg-btn" :class="{ active: sortOrder === 'desc' }" @click="sortOrder = 'desc'; run()">Newest</button>
                <button class="disc-fg-btn" :class="{ active: sortOrder === 'asc' }"  @click="sortOrder = 'asc';  run()">Oldest</button>
              </div>
            </div>
          </div>

          <!-- Histogram -->
          <div v-if="histogram.length" class="disc-histo">
            <div class="disc-histo-bars">
              <div v-for="b in histogram" :key="b.key"
                class="disc-histo-col"
                :title="`${formatBarTime(b)}: ${b.doc_count.toLocaleString()} events`">
                <div class="disc-histo-bar"
                  :style="{ height: `${Math.max(2, Math.round((b.doc_count / histogramMax) * 100))}%` }" />
              </div>
            </div>
            <div class="disc-histo-labels">
              <span>{{ formatBarTime(histogram[0]) }}</span>
              <span>{{ formatBarTime(histogram[Math.floor(histogram.length / 2)]) }}</span>
              <span>now</span>
            </div>
          </div>

        </div>
      </Transition>

      <!-- ── Log stream ──────────────────────────────────── -->
      <div v-if="!hits.length && !loading" class="disc-placeholder">
        <p>No results — try a broader time range or different index pattern.</p>
      </div>

      <div v-else ref="streamEl" class="disc-stream">
        <div
          v-for="(hit, idx) in hits"
          :key="hitKey(hit, idx)"
          class="disc-entry"
          :class="[levelClass(hit._source), { 'disc-entry-open': selectedHit?.key === hitKey(hit, idx) }]"
          @click="toggleHit(hitKey(hit, idx), hit)"
        >
          <div class="disc-line">
            <div class="disc-level-bar" />
            <time class="disc-ts">{{ formatTimestamp(getPath(hit._source, '@timestamp')) }}</time>
            <span class="disc-level-badge" :class="'badge-' + levelClass(hit._source).replace('level-','')">
              {{ (parsedLevel(hit._source) || 'info').toUpperCase().slice(0, 4) }}
            </span>
            <span v-if="getPath(hit._source, 'app_name') || getPath(hit._source, 'service.name')"
              class="disc-service"
              @click.stop="addFilter(getPath(hit._source, 'app_name') ? 'app_name' : 'service.name', getPath(hit._source, 'app_name') || getPath(hit._source, 'service.name'))">
              {{ getPath(hit._source, 'app_name') || getPath(hit._source, 'service.name') }}
            </span>
            <span v-if="getPath(hit._source, 'environment')"
              class="disc-env-tag"
              :class="getPath(hit._source, 'environment') === 'production' ? 'env-prod' : 'env-sbx'"
              @click.stop="addFilter('environment', getPath(hit._source, 'environment'))">
              {{ getPath(hit._source, 'environment') }}
            </span>
            <span class="disc-msg">{{ hitMessage(hit._source) }}</span>
            <span class="disc-open-hint">View details ›</span>
          </div>
        </div>
      </div>

      <!-- ── Pagination ──────────────────────────────────── -->
      <div v-if="totalPages > 1" class="disc-pagination">
        <button class="disc-pg-btn disc-pg-nav"
          :disabled="currentPage === 1"
          @click="goToPage(currentPage - 1)">‹ Prev</button>

        <template v-for="p in pageWindow" :key="String(p)">
          <span v-if="p === '…'" class="disc-pg-ellipsis">…</span>
          <button v-else class="disc-pg-btn"
            :class="{ active: p === currentPage }"
            @click="goToPage(p)">{{ p }}</button>
        </template>

        <button class="disc-pg-btn disc-pg-nav"
          :disabled="currentPage === totalPages"
          @click="goToPage(currentPage + 1)">Next ›</button>

        <span class="disc-pg-info">Page {{ currentPage }} of {{ totalPages.toLocaleString() }}</span>
      </div>

    </template>
  </div>

  <!-- ── Log Detail Slide-over ──────────────────────────────── -->
  <Teleport to="body">
    <Transition name="detail-slide">
      <div v-if="selectedHit" class="detail-overlay"
        :class="{ 'detail-overlay-fs': detailFullscreen }"
        @click.self="closeDetail">
        <div class="detail-panel" :class="{ 'detail-panel-fs': detailFullscreen }">

          <!-- Header -->
          <div class="detail-header">
            <div class="detail-header-left">
              <span class="detail-level-dot"
                :class="'dot-' + levelClass(selectedHit.hit._source).replace('level-','')"/>
              <span class="detail-level-text"
                :class="'badge-' + levelClass(selectedHit.hit._source).replace('level-','')">
                {{ (parsedLevel(selectedHit.hit._source) || 'info').toUpperCase() }}
              </span>
              <time class="detail-ts">{{ formatTimestamp(getPath(selectedHit.hit._source, '@timestamp')) }}</time>
            </div>
            <div class="detail-header-right">
              <span class="detail-index-pill">{{ selectedHit.hit._index }}</span>
              <button class="detail-icon-btn" :title="detailFullscreen ? 'Exit fullscreen' : 'Fullscreen'"
                @click="detailFullscreen = !detailFullscreen">
                <svg v-if="!detailFullscreen" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <polyline points="15 3 21 3 21 9"/><polyline points="9 21 3 21 3 15"/>
                  <line x1="21" y1="3" x2="14" y2="10"/><line x1="3" y1="21" x2="10" y2="14"/>
                </svg>
                <svg v-else width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <polyline points="4 14 10 14 10 20"/><polyline points="20 10 14 10 14 4"/>
                  <line x1="10" y1="14" x2="3" y2="21"/><line x1="21" y1="3" x2="14" y2="10"/>
                </svg>
              </button>
              <button class="detail-close-btn" @click="closeDetail">✕ Close</button>
            </div>
          </div>

          <!-- Context chips -->
          <div class="detail-meta-row">
            <div v-if="getPath(selectedHit.hit._source, 'app_name') || getPath(selectedHit.hit._source, 'service.name')"
              class="detail-meta-chip chip-app"
              title="Click to filter by app"
              @click="addFilter(getPath(selectedHit.hit._source, 'app_name') ? 'app_name' : 'service.name', getPath(selectedHit.hit._source, 'app_name') || getPath(selectedHit.hit._source, 'service.name')); closeDetail()">
              <span class="chip-label">app</span>
              {{ getPath(selectedHit.hit._source, 'app_name') || getPath(selectedHit.hit._source, 'service.name') }}
            </div>
            <div v-if="getPath(selectedHit.hit._source, 'environment')"
              class="detail-meta-chip"
              :class="getPath(selectedHit.hit._source, 'environment') === 'production' ? 'chip-prod' : 'chip-sbx'"
              title="Click to filter by environment"
              @click="addFilter('environment', getPath(selectedHit.hit._source, 'environment')); closeDetail()">
              <span class="chip-label">env</span>
              {{ getPath(selectedHit.hit._source, 'environment') }}
            </div>
            <div v-if="getPath(selectedHit.hit._source, 'host.name')"
              class="detail-meta-chip chip-host">
              <span class="chip-label">host</span>
              {{ getPath(selectedHit.hit._source, 'host.name') }}
            </div>
            <div v-if="getPath(selectedHit.hit._source, 'log.file.path')"
              class="detail-meta-chip chip-path">
              <span class="chip-label">file</span>
              {{ getPath(selectedHit.hit._source, 'log.file.path') }}
            </div>
            <div v-if="getPath(selectedHit.hit._source, 'http.request.method')"
              class="detail-meta-chip chip-http">
              <span class="chip-label">http</span>
              {{ getPath(selectedHit.hit._source, 'http.request.method') }}
              {{ getPath(selectedHit.hit._source, 'url.path') || getPath(selectedHit.hit._source, 'http.request.referrer') }}
              <span v-if="getPath(selectedHit.hit._source, 'http.response.status_code')"
                :class="Number(getPath(selectedHit.hit._source, 'http.response.status_code')) >= 500 ? 'chip-status-err' : Number(getPath(selectedHit.hit._source, 'http.response.status_code')) >= 400 ? 'chip-status-warn' : 'chip-status-ok'">
                {{ getPath(selectedHit.hit._source, 'http.response.status_code') }}
              </span>
            </div>
          </div>

          <!-- Message block -->
          <div class="detail-message-block" :class="{ 'detail-message-block-fs': detailFullscreen }">
            <div class="detail-message-label-row">
              <span class="detail-message-label">Message</span>
              <span v-if="detailMsg.isJson" class="detail-msg-badge">JSON</span>
            </div>
            <pre class="detail-message-body" :class="{ 'detail-message-body-fs': detailFullscreen }"><template v-if="detailMsg.tokens.length"><span
                v-for="(tok, i) in detailMsg.tokens" :key="i"
                :class="'msg-tok-' + tok.type">{{ tok.text }}</span></template><template v-else>{{ detailMsg.plain }}</template></pre>
          </div>

          <!-- Context fields -->
          <div class="detail-fields-header">
            <span>Context fields</span>
            <span class="detail-fields-hint">Click <strong>＋</strong> to filter</span>
          </div>
          <div class="detail-fields-body">
            <div v-for="(val, key) in detailContextFields"
              :key="String(key)"
              class="detail-field-row">
              <span class="detail-field-key">{{ key }}</span>
              <span class="detail-field-val">{{ val }}</span>
              <button class="detail-field-filter-btn"
                title="Filter for this value"
                @click="addFilter(String(key), String(val)); closeDetail()">＋</button>
            </div>

            <!-- Technical fields toggle -->
            <div class="detail-technical-toggle">
              <button class="detail-toggle-btn" @click="showTechnical = !showTechnical">
                {{ showTechnical ? '▾ Hide' : '▸ Show' }} technical fields
                <span class="detail-toggle-count">({{ Object.keys(detailTechnicalFields).length }})</span>
              </button>
            </div>

            <template v-if="showTechnical">
              <div class="detail-section-divider">Technical / Filebeat metadata</div>
              <div v-for="(val, key) in detailTechnicalFields"
                :key="String(key)"
                class="detail-field-row detail-field-row-dim">
                <span class="detail-field-key">{{ key }}</span>
                <span class="detail-field-val">{{ val }}</span>
                <button class="detail-field-filter-btn"
                  title="Filter for this value"
                  @click="addFilter(String(key), String(val)); closeDetail()">＋</button>
              </div>
            </template>
          </div>

        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script lang="ts">
function flatSource(source: Record<string, any>, prefix = ''): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [key, val] of Object.entries(source)) {
    const fullKey = prefix ? `${prefix}.${key}` : key
    if (val !== null && typeof val === 'object' && !Array.isArray(val)) {
      Object.assign(out, flatSource(val, fullKey))
    } else {
      out[fullKey] = val === null ? 'null' : Array.isArray(val) ? JSON.stringify(val) : String(val)
    }
  }
  return out
}
export { flatSource }
</script>

<style scoped>
/* ── Root ─────────────────────────────────────────────────── */
.disc-root {
  display: flex; flex-direction: column; gap: 10px;
  padding: 16px; background: var(--bg-body);
  height: 100%; overflow: hidden;
  box-sizing: border-box;
}

/* Stream is the only scrollable region — toolbar stays pinned */
.disc-stream {
  flex: 1; min-height: 0; overflow-y: auto;
  -webkit-overflow-scrolling: touch;
}

/* ── Toolbar ──────────────────────────────────────────────── */
.disc-toolbar { display:flex; align-items:center; justify-content:space-between; gap:12px; flex-wrap:wrap; }
.disc-toolbar-left  { display:flex; align-items:center; gap:8px; flex-wrap:wrap; }
.disc-toolbar-right { display:flex; align-items:center; gap:8px; }
.disc-conn-sel { width:190px; }
.disc-rf-sel   { width:66px; }
.disc-live { width:7px; height:7px; border-radius:50%; background:var(--success); box-shadow:0 0 5px var(--success); animation:live-pulse 2s infinite; }
@keyframes live-pulse { 0%,100%{opacity:1}50%{opacity:.3} }

/* Quick patterns */
.disc-quick-row { display:flex; gap:4px; flex-wrap:wrap; }
.disc-qp { border:1px solid var(--border); background:transparent; color:var(--text-muted); padding:3px 11px; border-radius:16px; cursor:pointer; font-size:11.5px; font-weight:600; transition:all .15s; white-space:nowrap; }
.disc-qp:hover { border-color:var(--text-muted); color:var(--text-primary); }
.disc-qp.active { background:var(--text-primary); border-color:var(--text-primary); color:var(--bg-body); }

/* ── Search bar ───────────────────────────────────────────── */
.disc-searchbar { display:flex; align-items:center; gap:8px; flex-wrap:wrap; }
.disc-idx-input  { width:240px; font-family:var(--mono); font-size:12px; }
.disc-search-wrap { flex:1; min-width:220px; }
.disc-search-input { width:100%; }
.disc-time-sel  { width:140px; }
.disc-custom-dt { width:160px; font-size:12px; }
.disc-run-btn   { white-space:nowrap; flex-shrink:0; }

/* ── Compact control bar ──────────────────────────────────── */
.disc-ctrlbar {
  display: flex; align-items: center; justify-content: space-between;
  gap: 8px; flex-shrink: 0; min-height: 28px;
}
.disc-ctrlbar-left  { display:flex; align-items:center; gap:6px; flex-wrap:wrap; flex:1; min-width:0; }
.disc-ctrlbar-right { display:flex; align-items:center; gap:6px; flex-shrink:0; }
.disc-hits-count { color:var(--text-primary); font-size:13px; font-weight:700; white-space:nowrap; }
.disc-hits-count strong { font-weight:800; }
.disc-strip-muted { color:var(--text-muted); font-size:11.5px; }
.disc-range  { font-family:var(--mono); }
.disc-updated::before { content:'↻ '; }
.disc-sep-dot { color:var(--border); }
/* Small inline chips */
.disc-chip-sm {
  display:inline-flex; align-items:center; gap:3px;
  background:color-mix(in srgb,#00bfb3 10%,transparent);
  border:1px solid color-mix(in srgb,#00bfb3 30%,transparent);
  color:#00bfb3; font-size:11px; padding:1px 5px 1px 7px;
  border-radius:999px; font-family:var(--mono); white-space:nowrap;
}
.disc-clear-sm {
  background:none; border:none; cursor:pointer; color:var(--text-muted);
  font-size:11px; padding:0 2px; text-decoration:underline;
}
.disc-clear-sm:hover { color:var(--text-primary); }
.disc-rows-sel { width:58px; height:26px; font-size:11px; }
.disc-toggle-ctrl {
  display:flex; align-items:center; gap:5px;
  border:1px solid var(--border); background:transparent;
  color:var(--text-muted); font-size:11.5px; padding:3px 10px;
  border-radius:6px; cursor:pointer; transition:all .12s; white-space:nowrap;
}
.disc-toggle-ctrl:hover { color:var(--text-primary); border-color:var(--text-muted); }
.disc-toggle-ctrl.active { border-color:#00bfb3; color:#00bfb3; }
/* Collapse transition */
.ctrl-collapse-enter-active { transition: max-height .2s ease, opacity .15s ease; }
.ctrl-collapse-leave-active { transition: max-height .18s ease, opacity .12s ease; }
.ctrl-collapse-enter-from, .ctrl-collapse-leave-to { max-height: 0; opacity: 0; overflow:hidden; }
.ctrl-collapse-enter-to, .ctrl-collapse-leave-from { max-height: 200px; opacity: 1; }
.disc-controls-panel { display:flex; flex-direction:column; gap:6px; overflow:hidden; }

/* ── Filter & Sort bar ────────────────────────────────────── */
.disc-filterbar { display:flex; align-items:center; gap:10px; flex-wrap:wrap; padding:2px 0; }
.disc-filter-group { display:flex; align-items:center; gap:6px; }
.disc-filter-label { font-size:11px; font-weight:600; color:var(--text-muted); text-transform:uppercase; letter-spacing:.04em; white-space:nowrap; }
.disc-btn-group { display:flex; border:1px solid var(--border); border-radius:6px; overflow:hidden; }
.disc-fg-btn {
  padding:3px 10px; font-size:11.5px; font-weight:500; background:transparent;
  border:none; color:var(--text-secondary); cursor:pointer; transition:background .12s,color .12s;
  white-space:nowrap;
}
.disc-fg-btn:hover { background:var(--bg-elevated); color:var(--text-primary); }
.disc-fg-btn.active { background:var(--bg-elevated); color:#00bfb3; font-weight:700; }
.disc-fg-btn.lvl-error.active { color:#f87171; }
.disc-fg-btn.lvl-warn.active  { color:#fbbf24; }
.disc-fg-btn.lvl-info.active  { color:#60a5fa; }
.disc-fg-btn.lvl-debug.active { color:#a78bfa; }
/* active filter chips */
.disc-chips { display:flex; align-items:center; gap:6px; flex-wrap:wrap; }
.disc-chip {
  display:inline-flex; align-items:center; gap:4px;
  background:color-mix(in srgb,#00bfb3 12%,transparent);
  border:1px solid color-mix(in srgb,#00bfb3 35%,transparent);
  color:#00bfb3; font-size:11.5px; padding:2px 6px 2px 8px;
  border-radius:999px; font-family:var(--mono);
}
.disc-chip-rm {
  background:none; border:none; cursor:pointer; color:#00bfb3;
  font-size:14px; line-height:1; padding:0 2px; opacity:.7;
}
.disc-chip-rm:hover { opacity:1; }
.disc-clear-all {
  background:none; border:1px solid var(--border); border-radius:6px;
  color:var(--text-muted); font-size:11px; padding:2px 8px; cursor:pointer;
}
.disc-clear-all:hover { color:var(--text-primary); border-color:var(--text-muted); }
/* App picker */
.disc-app-picker { position:relative; display:flex; align-items:center; }
.disc-app-select {
  appearance:none; -webkit-appearance:none;
  background:var(--bg-elevated); border:1px solid var(--border); border-radius:6px;
  color:var(--text-primary); font-size:12px; padding:3px 26px 3px 8px;
  cursor:pointer; height:26px; max-width:180px;
}
.disc-app-select:focus { outline:none; border-color:#00bfb3; }
.has-value .disc-app-select { border-color:#00bfb3; color:#00bfb3; font-weight:600; }
.disc-app-clear {
  position:absolute; right:6px; top:50%; transform:translateY(-50%);
  background:none; border:none; cursor:pointer; color:#00bfb3;
  font-size:14px; line-height:1; padding:0; opacity:.8;
}
.disc-app-clear:hover { opacity:1; }

/* ── Histogram ────────────────────────────────────────────── */
.disc-histo { border:1px solid var(--border); background:var(--bg-elevated); border-radius:8px; padding:8px 12px 5px; }
.disc-histo-bars { display:flex; align-items:flex-end; height:48px; gap:1.5px; }
.disc-histo-col  { flex:1; height:100%; display:flex; align-items:flex-end; cursor:crosshair; }
.disc-histo-bar  { width:100%; min-height:2px; background:color-mix(in srgb,#00bfb3 60%,transparent); border-radius:2px 2px 0 0; transition:background .15s; }
.disc-histo-col:hover .disc-histo-bar { background:#00bfb3; }
.disc-histo-labels { display:flex; justify-content:space-between; margin-top:3px; }
.disc-histo-labels span { font-size:10px; color:var(--text-muted); font-family:var(--mono); }

/* ── Log stream ───────────────────────────────────────────── */
.disc-stream {
  display:flex; flex-direction:column;
  border:1px solid var(--border); border-radius:10px;
  background:var(--bg-elevated);
}

.disc-entry { border-bottom:1px solid var(--border); cursor:pointer; transition:background .1s; }
.disc-entry:last-child { border-bottom:none; }
.disc-entry:hover { background:color-mix(in srgb,var(--text-muted) 4%,transparent); }
.disc-entry-open { background:color-mix(in srgb,#00bfb3 4%,var(--bg-elevated)) !important; }

/* Collapsed log line */
.disc-line {
  display:flex; align-items:baseline; gap:0;
  padding:0; min-height:36px; font-size:12.5px;
  position:relative;
}

/* Colored left accent by level */
.disc-level-bar { width:3px; flex-shrink:0; align-self:stretch; border-radius:0; }
.level-error .disc-level-bar { background:var(--danger); }
.level-warn  .disc-level-bar { background:var(--warning); }
.level-info  .disc-level-bar { background:#00bfb3; }
.level-debug .disc-level-bar { background:#6366f1; }
.level-trace .disc-level-bar { background:var(--border); }

/* Timestamp */
.disc-ts {
  flex-shrink:0; font-family:var(--mono); font-size:11px;
  color:var(--text-muted); padding:0 10px 0 10px;
  line-height:36px; white-space:nowrap;
}

/* Level badge */
.disc-level-badge {
  flex-shrink:0; font-size:10px; font-weight:800;
  padding:2px 6px; border-radius:3px; line-height:1;
  margin-top:10px; margin-right:8px; letter-spacing:.03em;
}
.badge-error { background:color-mix(in srgb,var(--danger)  18%,transparent); color:var(--danger); }
.badge-warn  { background:color-mix(in srgb,var(--warning) 18%,transparent); color:var(--warning); }
.badge-info  { background:color-mix(in srgb,#00bfb3 14%,transparent);         color:#00a69c; }
.badge-debu  { background:color-mix(in srgb,#6366f1 14%,transparent);         color:#6366f1; }
.badge-trac  { background:var(--bg-body); color:var(--text-muted); }

/* Service name */
.disc-service {
  flex-shrink:0; font-size:11.5px; font-weight:700;
  color:var(--text-primary); font-family:var(--mono);
  padding:0 8px 0 0; line-height:36px; white-space:nowrap;
  cursor:pointer; transition:color .12s;
}
.disc-service:hover { color:#00bfb3; text-decoration:underline; }

/* Environment tag */
.disc-env-tag {
  flex-shrink:0; font-size:9.5px; font-weight:700;
  padding:1px 6px; border-radius:3px; margin-right:8px;
  line-height:1; margin-top:10px; letter-spacing:.02em;
}
.env-prod { background:color-mix(in srgb,var(--danger) 12%,transparent); color:var(--danger); border:1px solid color-mix(in srgb,var(--danger) 25%,transparent); }
.env-sbx  { background:color-mix(in srgb,#6366f1 12%,transparent); color:#6366f1; border:1px solid color-mix(in srgb,#6366f1 25%,transparent); }

/* Message text */
.disc-msg {
  flex:1; min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;
  color:var(--text-primary); font-size:12.5px; line-height:36px; padding-right:8px;
}

/* Expand arrow */
.disc-arrow { flex-shrink:0; font-size:9px; color:var(--text-muted); padding-right:12px; line-height:36px; }

/* ── Detail panel ─────────────────────────────────────────── */
/* ── "View details" hint on hover ─────────────────────────── */
.disc-open-hint {
  flex-shrink: 0; font-size: 11px; color: var(--text-muted);
  opacity: 0; transition: opacity .15s; white-space: nowrap; margin-left: auto;
}
.disc-entry:hover .disc-open-hint { opacity: 1; }

/* ── Detail slide-over (Teleported to body) ───────────────── */
.detail-overlay {
  position: fixed; inset: 0; z-index: 9000;
  background: rgba(0,0,0,.45);
  display: flex; justify-content: flex-end;
}
.detail-panel {
  width: min(820px, 95vw); height: 100%;
  background: var(--bg-body); border-left: 1px solid var(--border);
  display: flex; flex-direction: column;
  overflow: hidden;
  box-shadow: -8px 0 40px rgba(0,0,0,.35);
  transition: width .2s cubic-bezier(.22,1,.36,1);
}
.detail-overlay-fs { background: rgba(0,0,0,.65); }
.detail-panel-fs   { width: 100vw; border-left: none; }

/* slide-in transition */
.detail-slide-enter-active { transition: transform .22s cubic-bezier(.22,1,.36,1); }
.detail-slide-leave-active { transition: transform .18s ease-in; }
.detail-slide-enter-from, .detail-slide-leave-to { transform: translateX(100%); }

/* Header */
.detail-header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 14px 20px; border-bottom: 1px solid var(--border);
  gap: 12px; flex-shrink: 0;
}
.detail-header-left  { display:flex; align-items:center; gap:10px; }
.detail-header-right { display:flex; align-items:center; gap:10px; }
.detail-level-dot {
  width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0;
}
.dot-error { background:#f87171; box-shadow:0 0 6px #f871714d; }
.dot-warn  { background:#fbbf24; }
.dot-info  { background:#60a5fa; }
.dot-debug { background:#a78bfa; }
.dot-      { background:var(--text-muted); }
.detail-level-text {
  font-size: 11px; font-weight: 700; letter-spacing: .06em;
  padding: 2px 8px; border-radius: 4px;
}
.detail-ts { font-family:var(--mono); font-size: 13px; color:var(--text-secondary); }
.detail-index-pill {
  font-size: 11px; color:var(--text-muted); background:var(--bg-elevated);
  border: 1px solid var(--border); border-radius: 4px; padding: 2px 8px;
  font-family: var(--mono); white-space: nowrap; overflow: hidden;
  max-width: 240px; text-overflow: ellipsis;
}
.detail-close-btn {
  border: 1px solid var(--border); background: transparent;
  color: var(--text-secondary); font-size: 12px; padding: 4px 14px;
  border-radius: 6px; cursor: pointer; transition: all .12s; white-space: nowrap;
}
.detail-close-btn:hover { background:var(--bg-elevated); color:var(--text-primary); }
.detail-icon-btn {
  width: 28px; height: 28px; display:flex; align-items:center; justify-content:center;
  border: 1px solid var(--border); background: transparent; border-radius: 6px;
  color: var(--text-secondary); cursor: pointer; transition: all .12s; flex-shrink: 0;
}
.detail-icon-btn:hover { background:var(--bg-elevated); color:var(--text-primary); }

/* Meta chips */
.detail-meta-row {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  padding: 10px 20px; border-bottom: 1px solid var(--border); flex-shrink: 0;
}
.detail-meta-chip {
  display: inline-flex; align-items: center; gap: 6px;
  border: 1px solid var(--border); border-radius: 999px;
  padding: 3px 12px 3px 8px; font-size: 12.5px; cursor: pointer;
  transition: border-color .12s, background .12s;
}
.detail-meta-chip:hover { border-color: #00bfb3; }
.chip-label { font-size: 10px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .06em; opacity: .55; }
.chip-app  { color: #60a5fa; }
.chip-prod { color: #f87171; }
.chip-sbx  { color: #00bfb3; }
.chip-host { color: var(--text-secondary); cursor: default; }
.chip-path { color: var(--text-muted); font-family: var(--mono); font-size: 11.5px; cursor: default; }
.chip-http { color: #a78bfa; gap: 8px; }
.chip-status-ok   { color: #4ade80; font-weight: 700; }
.chip-status-warn { color: #fbbf24; font-weight: 700; }
.chip-status-err  { color: #f87171; font-weight: 700; }

/* Message block */
.detail-message-block {
  padding: 14px 20px; border-bottom: 1px solid var(--border); flex-shrink: 0;
}
.detail-message-block-fs {
  flex: 1; display: flex; flex-direction: column; min-height: 0;
}
.detail-message-label-row {
  display: flex; align-items: center; gap: 8px; margin-bottom: 8px;
}
.detail-message-label {
  font-size: 10.5px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .07em; color: var(--text-muted);
}
.detail-msg-badge {
  font-size: 10px; font-weight: 700; letter-spacing: .06em;
  background: color-mix(in srgb,#00bfb3 15%,transparent);
  border: 1px solid color-mix(in srgb,#00bfb3 40%,transparent);
  color: #00bfb3; padding: 1px 6px; border-radius: 4px;
}
.detail-message-body {
  font-family: var(--mono); font-size: 13px; line-height: 1.75;
  color: var(--text-primary); white-space: pre-wrap; word-break: break-all;
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 6px; padding: 14px 16px; margin: 0;
  max-height: 220px; overflow-y: auto;
}
.detail-message-body-fs {
  flex: 1; max-height: none; font-size: 14px; line-height: 1.8;
  padding: 18px 20px; border-radius: 8px;
}
/* JSON / message token colors */
.msg-tok-key   { color: #60a5fa; }
.msg-tok-str   { color: #4ade80; }
.msg-tok-num   { color: #fb923c; }
.msg-tok-bool  { color: #fbbf24; }
.msg-tok-null  { color: #f87171; }
.msg-tok-punc  { color: var(--text-muted); }
.msg-tok-plain { color: var(--text-primary); }

/* All fields table */
.detail-fields-header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 10px 20px 6px; flex-shrink: 0;
  font-size: 11px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .07em; color: var(--text-muted);
}
.detail-fields-hint { font-weight: 400; text-transform: none; letter-spacing: 0; }
.detail-fields-body {
  flex: 1; overflow-y: auto; padding: 0 12px 16px;
}
.detail-field-row {
  display: grid; grid-template-columns: 220px 1fr 32px;
  align-items: baseline; gap: 8px;
  padding: 7px 8px; border-radius: 5px;
  transition: background .1s;
}
.detail-field-row:hover { background: color-mix(in srgb,var(--text-muted) 7%,transparent); }
.detail-field-key {
  font-family: var(--mono); font-size: 12px; font-weight: 600;
  color: #60a5fa; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.detail-field-val {
  font-family: var(--mono); font-size: 12.5px; color: var(--text-primary);
  word-break: break-all; line-height: 1.5;
}
.detail-field-filter-btn {
  justify-self: center; border: 1px solid var(--border); background: transparent;
  color: var(--text-muted); cursor: pointer; font-size: 14px;
  width: 24px; height: 24px; border-radius: 4px; line-height: 1;
  opacity: 0; transition: opacity .12s, color .12s, border-color .12s;
}
.detail-field-row:hover .detail-field-filter-btn { opacity: 1; }
.detail-field-filter-btn:hover { color: #00bfb3; border-color: #00bfb3; }
.detail-field-row-dim .detail-field-key { color: var(--text-muted); opacity: .7; }
.detail-field-row-dim .detail-field-val { color: var(--text-muted); }
.detail-technical-toggle { padding: 10px 0 4px; }
.detail-toggle-btn {
  background: none; border: 1px dashed var(--border); border-radius: 6px;
  color: var(--text-muted); font-size: 12px; padding: 4px 12px;
  cursor: pointer; transition: all .12s;
}
.detail-toggle-btn:hover { color: var(--text-primary); border-color: var(--text-muted); }
.detail-toggle-count { opacity: .6; margin-left: 4px; }
.detail-section-divider {
  font-size: 10px; font-weight: 700; text-transform: uppercase; letter-spacing: .07em;
  color: var(--text-muted); padding: 8px 0 4px;
  border-top: 1px dashed var(--border); margin-top: 4px;
}

/* ── Placeholder ──────────────────────────────────────────── */
.disc-placeholder { border:1px dashed var(--border); border-radius:8px; padding:28px; text-align:center; color:var(--text-muted); font-size:13px; }

/* ── Responsive ───────────────────────────────────────────── */
@media(max-width:860px) {
  .disc-searchbar { flex-direction:column; align-items:stretch; }
  .disc-idx-input,.disc-time-sel { width:100%; }
  .detail-panel { width: 100vw; }
  .detail-field-row { grid-template-columns: 160px 1fr 28px; }
}
@media(max-width:600px) {
  .disc-toolbar { flex-direction:column; align-items:stretch; }
  .disc-conn-sel { width:100%; }
}

/* ── Pagination ───────────────────────────────────────────── */
.disc-pagination {
  display: flex; align-items: center; justify-content: center;
  gap: 4px; padding: 10px 0 4px; flex-shrink: 0; flex-wrap: wrap;
}
.disc-pg-btn {
  min-width: 32px; height: 28px; padding: 0 8px;
  border: 1px solid var(--border); border-radius: 6px;
  background: var(--bg-elevated); color: var(--text-secondary);
  font-size: 12.5px; cursor: pointer; transition: all .12s;
}
.disc-pg-btn:hover:not(:disabled) { border-color:#00bfb3; color:#00bfb3; }
.disc-pg-btn.active { background:#00bfb3; border-color:#00bfb3; color:#fff; font-weight:700; }
.disc-pg-btn:disabled { opacity:.35; cursor:not-allowed; }
.disc-pg-nav { padding: 0 12px; font-size: 13px; }
.disc-pg-ellipsis { color: var(--text-muted); font-size: 13px; padding: 0 4px; line-height: 28px; }
.disc-pg-info { font-size: 11.5px; color: var(--text-muted); margin-left: 8px; white-space: nowrap; }
</style>
