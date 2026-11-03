<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useConnections } from '@/composables/useConnections'
import { useSls, type SlsBucket, type SlsField, type SlsRow } from '@/composables/useSls'
import { useListFilter } from '@/composables/useListFilter'
import { readableError } from '@/utils/httpError'
import ConnectionGate from '@/components/ui/ConnectionGate.vue'
import EmptyState from '@/components/ui/EmptyState.vue'

const props = defineProps<{ activeConnId: number | null }>()
const emit = defineEmits<{ (e: 'set-conn', id: number): void }>()

const { connections, fetchConnections } = useConnections()
const sls = useSls()

const RANGES = [
  { key: '5m', label: 'Last 5 minutes', seconds: 5 * 60 },
  { key: '15m', label: 'Last 15 minutes', seconds: 15 * 60 },
  { key: '1h', label: 'Last hour', seconds: 60 * 60 },
  { key: '4h', label: 'Last 4 hours', seconds: 4 * 60 * 60 },
  { key: '24h', label: 'Last 24 hours', seconds: 24 * 60 * 60 },
  { key: '7d', label: 'Last 7 days', seconds: 7 * 24 * 60 * 60 },
]
const PAGE = 100

// ── Connection ────────────────────────────────────────────────────
const activeConn = computed(() =>
  props.activeConnId != null ? connections.value.find((c) => c.id === props.activeConnId) ?? null : null,
)
const slsConnections = computed(() => connections.value.filter((c) => c.driver === 'sls'))
const isSls = computed(() => activeConn.value?.driver === 'sls')

// ── Logstores ─────────────────────────────────────────────────────
const project = ref('')
const stores = ref<string[]>([])
const storesLoading = ref(false)
const storesError = ref('')
const store = ref('')
const { search: storeFilter, filtered: filteredStores } = useListFilter(stores, (s, q) => s.toLowerCase().includes(q))

// ── Fields ────────────────────────────────────────────────────────
const fields = ref<SlsField[]>([])
const indexed = ref(true)
const columns = ref<string[]>([])
const { search: fieldFilter, filtered: filteredFields } = useListFilter(fields, (f, q) => f.name.toLowerCase().includes(q))

// ── Search ────────────────────────────────────────────────────────
const query = ref('')
const rangeKey = ref('15m')
const zoom = ref<{ from: number; to: number } | null>(null)
const queryInput = ref<HTMLInputElement | null>(null)

const histogram = ref<SlsBucket[]>([])
const total = ref<number | null>(null)
const rows = ref<SlsRow[]>([])
const hasMore = ref(false)
const complete = ref(true)
const elapsedMs = ref<number | null>(null)
const searching = ref(false)
const loadingMore = ref(false)
const searchError = ref('')
const searched = ref(false)
const expanded = ref(new Set<number>())
let lastRange = { from: 0, to: 0 }
let inflight: AbortController | null = null

const isSql = computed(() => query.value.includes('|'))
const maxBucket = computed(() => Math.max(1, ...histogram.value.map((b) => b.count)))
const sqlColumns = computed(() => {
  const keys = new Set<string>()
  for (const r of rows.value) Object.keys(r).forEach((k) => keys.add(k))
  return [...keys]
})

const storageKey = (suffix: string) => `sls:${props.activeConnId}:${suffix}`
function remember(suffix: string, value: string) {
  try { localStorage.setItem(storageKey(suffix), value) } catch { /* storage unavailable */ }
}
function recall(suffix: string): string | null {
  try { return localStorage.getItem(storageKey(suffix)) } catch { return null }
}

onMounted(async () => {
  if (!connections.value.length) await fetchConnections()
  if (!isSls.value && slsConnections.value.length === 1) {
    emit('set-conn', slsConnections.value[0].id)
    return
  }
  if (isSls.value) await loadStores()
})

watch(() => props.activeConnId, async () => {
  reset()
  if (isSls.value) await loadStores()
})

function reset() {
  inflight?.abort()
  project.value = ''
  stores.value = []
  storesError.value = ''
  store.value = ''
  fields.value = []
  columns.value = []
  clearResults()
}

function clearResults() {
  histogram.value = []
  total.value = null
  rows.value = []
  hasMore.value = false
  complete.value = true
  elapsedMs.value = null
  searchError.value = ''
  searched.value = false
  expanded.value = new Set()
}

async function loadStores() {
  if (!props.activeConnId) return
  storesLoading.value = true
  storesError.value = ''
  try {
    const data = await sls.logstores(props.activeConnId)
    project.value = data.project
    stores.value = data.logstores
    const last = recall('logstore')
    const pick = last && data.logstores.includes(last) ? last : data.logstores[0]
    if (pick) await selectStore(pick)
  } catch (e) {
    storesError.value = readableError(e, 'Could not list logstores')
  } finally {
    storesLoading.value = false
  }
}

async function selectStore(name: string) {
  if (!props.activeConnId) return
  store.value = name
  zoom.value = null
  remember('logstore', name)
  fieldFilter.value = ''
  try {
    columns.value = JSON.parse(recall(`columns:${name}`) || '[]')
  } catch {
    columns.value = []
  }
  clearResults()
  try {
    const info = await sls.fields(props.activeConnId, name)
    fields.value = info.fields
    indexed.value = info.indexed
  } catch {
    fields.value = []
    indexed.value = true
  }
  if (indexed.value) await search()
}

function currentRange() {
  if (zoom.value) return zoom.value
  const r = RANGES.find((x) => x.key === rangeKey.value) ?? RANGES[1]
  const to = Math.floor(Date.now() / 1000)
  return { from: to - r.seconds, to }
}

async function search() {
  if (!props.activeConnId || !store.value) return
  inflight?.abort()
  const ctrl = new AbortController()
  inflight = ctrl
  lastRange = currentRange()
  const params = { logstore: store.value, query: query.value.trim(), ...lastRange }
  searching.value = true
  searchError.value = ''
  expanded.value = new Set()
  try {
    // The histogram only understands the search part of "search | sql".
    const histQuery = params.query.split('|')[0].trim()
    const [hist, page] = await Promise.all([
      sls.histogram(props.activeConnId, { ...params, query: histQuery }, ctrl.signal),
      sls.logs(props.activeConnId, { ...params, line: PAGE }, ctrl.signal),
    ])
    histogram.value = hist.buckets
    total.value = hist.count
    rows.value = page.rows
    hasMore.value = page.has_more && !isSql.value
    complete.value = hist.complete && page.complete
    elapsedMs.value = page.elapsed_ms
    searched.value = true
  } catch (e) {
    if (ctrl.signal.aborted) return
    rows.value = []
    histogram.value = []
    total.value = null
    searchError.value = readableError(e, 'Search failed')
    searched.value = true
  } finally {
    if (inflight === ctrl) {
      inflight = null
      searching.value = false
    }
  }
}

async function loadMore() {
  if (!props.activeConnId || !store.value || loadingMore.value) return
  loadingMore.value = true
  try {
    const page = await sls.logs(props.activeConnId, {
      logstore: store.value, query: query.value.trim(), ...lastRange, offset: rows.value.length, line: PAGE,
    })
    rows.value = rows.value.concat(page.rows)
    hasMore.value = page.has_more
  } catch (e) {
    searchError.value = readableError(e, 'Could not load more logs')
  } finally {
    loadingMore.value = false
  }
}

function pickRange(key: string) {
  rangeKey.value = key
  zoom.value = null
  search()
}

function zoomTo(b: SlsBucket) {
  if (!b.count || b.to - b.from < 2) return
  zoom.value = { from: b.from, to: b.to }
  search()
}

function clearZoom() {
  zoom.value = null
  search()
}

function toggleColumn(name: string) {
  const i = columns.value.indexOf(name)
  if (i >= 0) columns.value.splice(i, 1)
  else columns.value.push(name)
  remember(`columns:${store.value}`, JSON.stringify(columns.value))
}

function toggleRow(i: number) {
  const next = new Set(expanded.value)
  if (next.has(i)) next.delete(i)
  else next.add(i)
  expanded.value = next
}

async function filterBy(key: string, value: unknown, exclude = false) {
  const v = String(value ?? '').replace(/\\/g, '\\\\').replace(/"/g, '\\"')
  const clause = `${exclude ? 'not ' : ''}${key}: "${v}"`
  const [searchPart, ...sqlParts] = query.value.split('|')
  const base = searchPart.trim()
  const next = !base || base === '*' ? clause : `${base} and ${clause}`
  query.value = sqlParts.length ? `${next} | ${sqlParts.join('|').trim()}` : next
  await search()
  await nextTick()
  queryInput.value?.focus()
}

// ── Row helpers ───────────────────────────────────────────────────
const isMeta = (k: string) => k.startsWith('__') // __time__, __source__, __topic__, __tag__:*, __pack_meta__

/** "__tag__:_pod_name_" → "pod_name"; "__source__" → "source". */
function fieldLabel(k: string) {
  return k.replace(/^__tag__:/, '').replace(/^_+|_+$/g, '')
}

function rowEntries(r: SlsRow) {
  return Object.entries(r).filter(([k]) => !isMeta(k))
}

function rowMeta(r: SlsRow) {
  return Object.entries(r).filter(([k]) => isMeta(k) && k !== '__time__' && r[k] !== '' && r[k] != null)
}

function rowSeverity(r: SlsRow): 'error' | 'warn' | '' {
  const v = String(r.level ?? r.severity ?? r.lvl ?? r.log_level ?? '').toLowerCase()
  if (['error', 'err', 'fatal', 'critical', 'crit', 'panic', 'alert', 'emerg'].includes(v)) return 'error'
  if (['warn', 'warning'].includes(v)) return 'warn'
  return ''
}

const pad = (n: number) => String(n).padStart(2, '0')
function formatTime(sec: number, withSeconds = true) {
  const d = new Date(sec * 1000)
  const base = `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
  return withSeconds ? `${base}:${pad(d.getSeconds())}` : base
}
function rowTime(r: SlsRow) {
  const t = Number(r.__time__)
  return Number.isFinite(t) && t > 0 ? formatTime(t) : ''
}

function display(value: unknown) {
  if (value == null) return ''
  return typeof value === 'string' ? value : JSON.stringify(value)
}

/** Pretty-print values that are JSON documents; leave everything else as-is. */
function pretty(value: unknown) {
  const s = display(value)
  const t = s.trim()
  if ((t.startsWith('{') && t.endsWith('}')) || (t.startsWith('[') && t.endsWith(']'))) {
    try { return JSON.stringify(JSON.parse(t), null, 2) } catch { /* not JSON */ }
  }
  return s
}

const rangeLabel = computed(() => {
  if (zoom.value) return `${formatTime(zoom.value.from)} – ${formatTime(zoom.value.to)}`
  return RANGES.find((r) => r.key === rangeKey.value)?.label ?? ''
})

const axisLabels = computed(() => {
  const h = histogram.value
  if (!h.length) return []
  const picks = [...new Set([0, Math.floor(h.length / 2), h.length - 1])]
  const short = h[h.length - 1].to - h[0].from < 10 * 60
  return picks.map((i) => formatTime(h[i].from, short))
})

const numberFmt = new Intl.NumberFormat()
</script>

<template>
  <div class="page-shell sls">
    <ConnectionGate
      v-if="!isSls"
      :drivers="['sls']"
      label="Alibaba Cloud SLS project"
      short="SLS project"
      purpose="to search its logstores"
      icon="logs"
      @select="(id: number) => emit('set-conn', id)"
    />

    <div v-else class="sls-work">
      <!-- ── Logstores & fields ─────────────────────────────── -->
      <aside class="sls-side" aria-label="Logstores">
        <div class="sls-side__project">
          <span class="sls-side__label">Project</span>
          <strong :title="project || activeConn?.database">{{ project || activeConn?.database }}</strong>
          <span class="sls-side__endpoint">{{ activeConn?.host }}</span>
        </div>

        <div class="sls-side__section">
          <div class="sls-side__head">
            <span>Logstores</span>
            <span class="sls-count">{{ stores.length }}</span>
          </div>
          <input v-if="stores.length > 6" v-model="storeFilter" class="base-input sls-side__filter" placeholder="Filter logstores" aria-label="Filter logstores" />
          <div v-if="storesLoading" class="sls-skel" aria-hidden="true"><span v-for="i in 5" :key="i" /></div>
          <p v-else-if="storesError" class="sls-inline-error">{{ storesError }}</p>
          <p v-else-if="!stores.length" class="sls-muted">This project has no logstores yet.</p>
          <nav v-else class="sls-list">
            <button
              v-for="s in filteredStores"
              :key="s"
              type="button"
              class="sls-list__item"
              :class="{ 'is-active': s === store }"
              :aria-current="s === store ? 'true' : undefined"
              :title="s"
              @click="selectStore(s)"
            >{{ s }}</button>
            <p v-if="!filteredStores.length" class="sls-muted">No logstore matches “{{ storeFilter }}”.</p>
          </nav>
        </div>

        <div v-if="fields.length" class="sls-side__section sls-side__section--fields">
          <div class="sls-side__head">
            <span>Fields</span>
            <span class="sls-count">{{ fields.length }}</span>
          </div>
          <p class="sls-hint">Pick fields to show as columns.</p>
          <input v-if="fields.length > 8" v-model="fieldFilter" class="base-input sls-side__filter" placeholder="Filter fields" aria-label="Filter fields" />
          <div class="sls-list">
            <label v-for="f in filteredFields" :key="f.name" class="sls-field" :title="`${f.name} · ${f.type}`">
              <input type="checkbox" :checked="columns.includes(f.name)" @change="toggleColumn(f.name)" />
              <span class="sls-field__name">{{ fieldLabel(f.name) }}</span>
              <span class="sls-field__type">{{ f.name.startsWith('__tag__:') ? 'tag' : f.type }}</span>
            </label>
          </div>
        </div>
      </aside>

      <!-- ── Search & results ───────────────────────────────── -->
      <section class="sls-main" aria-live="polite">
        <form class="sls-bar" role="search" @submit.prevent="search()">
          <label class="sls-bar__query">
            <span class="sr-only">Query</span>
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path d="m20 20-4-4" /></svg>
            <input
              ref="queryInput"
              v-model="query"
              class="sls-bar__input"
              :placeholder="store ? 'level: error and status >= 500 | select count(*) as n' : 'Pick a logstore first'"
              :disabled="!store || !indexed"
              spellcheck="false"
              autocomplete="off"
            />
          </label>
          <select class="base-input sls-bar__range" :value="zoom ? '' : rangeKey" aria-label="Time range" :disabled="!store" @change="pickRange(($event.target as HTMLSelectElement).value)">
            <option v-if="zoom" value="" disabled>Custom range</option>
            <option v-for="r in RANGES" :key="r.key" :value="r.key">{{ r.label }}</option>
          </select>
          <button class="base-btn base-btn--primary sls-bar__run" type="submit" :disabled="!store || !indexed || searching">
            {{ searching ? 'Searching…' : 'Search' }}
          </button>
        </form>

        <EmptyState
          v-if="stores.length && !store"
          icon="logs"
          title="Pick a logstore"
          text="Choose a logstore on the left to search its logs."
        />
        <EmptyState
          v-else-if="store && !indexed"
          icon="search"
          title="This logstore has no index"
          text="SLS can only search logstores with an index. Turn on indexing for this logstore in the Alibaba Cloud console, then search again."
        />

        <template v-else-if="store">
          <div class="sls-meta">
            <span v-if="total !== null" class="sls-meta__count"><strong>{{ numberFmt.format(total) }}</strong> {{ total === 1 ? 'log' : 'logs' }}</span>
            <span v-else-if="searching">Searching {{ store }}…</span>
            <span class="sls-meta__range">{{ rangeLabel }}</span>
            <button v-if="zoom" type="button" class="sls-chip" @click="clearZoom">
              Reset range
              <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" aria-hidden="true"><path d="M6 6l12 12M18 6 6 18" /></svg>
            </button>
            <span v-if="elapsedMs !== null" class="sls-meta__dim">{{ elapsedMs }} ms</span>
            <span v-if="!complete" class="sls-badge sls-badge--warn" title="SLS stopped before scanning every log. Narrow the time range for exact results.">Partial results</span>
          </div>

          <div v-if="histogram.length" class="sls-hist" :class="{ 'is-busy': searching }">
            <div class="sls-hist__bars">
              <button
                v-for="(b, i) in histogram"
                :key="i"
                type="button"
                class="sls-hist__bar"
                :style="{ '--h': `${Math.max(b.count ? 4 : 0, (b.count / maxBucket) * 100)}%` }"
                :disabled="!b.count"
                :aria-label="`${numberFmt.format(b.count)} logs from ${formatTime(b.from)} to ${formatTime(b.to)}. Zoom in.`"
                :title="`${formatTime(b.from)} – ${formatTime(b.to)}\n${numberFmt.format(b.count)} logs · click to zoom`"
                @click="zoomTo(b)"
              />
            </div>
            <div class="sls-hist__axis" aria-hidden="true"><span v-for="l in axisLabels" :key="l">{{ l }}</span></div>
          </div>

          <div v-if="searchError" class="sls-error" role="alert">
            <strong>Search failed.</strong> {{ searchError }}
          </div>

          <div v-else-if="searching && !rows.length" class="sls-skel sls-skel--rows" aria-hidden="true"><span v-for="i in 8" :key="i" /></div>

          <EmptyState
            v-else-if="searched && !rows.length"
            icon="search"
            compact
            title="No logs match"
            :text="zoom ? 'Nothing in this slice. Reset the range or change the query.' : 'Try a longer time range or a broader query.'"
          />

          <!-- SQL analysis results: a plain table -->
          <div v-else-if="isSql && rows.length" class="sls-table-wrap">
            <table class="sls-table">
              <thead><tr><th v-for="c in sqlColumns" :key="c" scope="col">{{ c }}</th></tr></thead>
              <tbody>
                <tr v-for="(r, i) in rows" :key="i">
                  <td v-for="c in sqlColumns" :key="c">{{ display(r[c]) }}</td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Raw logs -->
          <ol v-else-if="rows.length" class="sls-rows" :class="{ 'is-busy': searching }">
            <li v-for="(r, i) in rows" :key="i" class="sls-row" :class="[rowSeverity(r) && `sls-row--${rowSeverity(r)}`, { 'is-open': expanded.has(i) }]">
              <button type="button" class="sls-row__head" :aria-expanded="expanded.has(i)" @click="toggleRow(i)">
                <svg class="sls-row__chev" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m9 6 6 6-6 6" /></svg>
                <time class="sls-row__time">{{ rowTime(r) }}</time>
                <span v-if="columns.length" class="sls-row__cols">
                  <span v-for="c in columns" :key="c" class="sls-kv"><span class="sls-kv__k">{{ fieldLabel(c) }}</span><span class="sls-kv__v">{{ display(r[c]) || '—' }}</span></span>
                </span>
                <span v-else class="sls-row__summary">
                  <span v-for="[k, v] in rowEntries(r)" :key="k" class="sls-kv"><span class="sls-kv__k">{{ k }}</span><span class="sls-kv__v">{{ display(v) }}</span></span>
                </span>
              </button>

              <div v-if="expanded.has(i)" class="sls-detail">
                <dl class="sls-detail__list">
                  <template v-for="[k, v] in rowEntries(r)" :key="k">
                    <dt>{{ k }}</dt>
                    <dd>
                      <pre>{{ pretty(v) }}</pre>
                      <span class="sls-detail__actions">
                        <button type="button" class="base-btn base-btn--ghost base-btn--xs" @click="filterBy(k, v)">Filter</button>
                        <button type="button" class="base-btn base-btn--ghost base-btn--xs" @click="filterBy(k, v, true)">Exclude</button>
                      </span>
                    </dd>
                  </template>
                </dl>
                <dl v-if="rowMeta(r).length" class="sls-detail__list sls-detail__list--meta">
                  <template v-for="[k, v] in rowMeta(r)" :key="k">
                    <dt>{{ fieldLabel(k) }}</dt>
                    <dd><pre>{{ display(v) }}</pre></dd>
                  </template>
                </dl>
              </div>
            </li>
          </ol>

          <div v-if="hasMore && !searchError" class="sls-more">
            <button type="button" class="base-btn base-btn--ghost" :disabled="loadingMore" @click="loadMore">
              {{ loadingMore ? 'Loading…' : `Load ${PAGE} more` }}
            </button>
            <span class="sls-meta__dim">Showing {{ numberFmt.format(rows.length) }}<template v-if="total"> of {{ numberFmt.format(total) }}</template></span>
          </div>
        </template>
      </section>
    </div>
  </div>
</template>

<style scoped>
.sls { display: flex; flex-direction: column; height: 100%; min-height: 0; background: var(--bg); }
.sls-work { flex: 1; min-height: 0; display: grid; grid-template-columns: 248px minmax(0, 1fr); }

/* ── Side ── */
.sls-side { display: flex; flex-direction: column; gap: 18px; min-height: 0; overflow-y: auto; padding: 16px 12px; border-right: 1px solid var(--border); }
.sls-side__project { display: flex; flex-direction: column; gap: 2px; padding: 0 6px; min-width: 0; }
.sls-side__project strong { font-size: 14.5px; font-weight: 600; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sls-side__label { font-size: 12px; color: var(--muted); }
.sls-side__endpoint { font-family: var(--mono); font-size: 11.5px; color: var(--muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sls-side__section { display: flex; flex-direction: column; gap: 6px; min-height: 0; }
.sls-side__head { display: flex; align-items: center; justify-content: space-between; padding: 0 6px; font-size: 12.5px; font-weight: 500; color: var(--muted); }
.sls-side__filter { height: 30px; font-size: 13px; }
.sls-count { font-variant-numeric: tabular-nums; }
.sls-hint { margin: 0; padding: 0 6px; font-size: 12px; color: var(--muted); }
.sls-list { display: flex; flex-direction: column; gap: 1px; }
.sls-list__item { all: unset; box-sizing: border-box; display: block; width: 100%; padding: 6px 8px; border-radius: 8px; font-size: 13.5px; color: var(--text-2); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; cursor: pointer; transition: background var(--dur, 140ms) ease, color var(--dur, 140ms) ease; }
.sls-list__item:hover { background: var(--hover); color: var(--text); }
.sls-list__item.is-active { background: var(--accent-bg); color: var(--accent); font-weight: 500; }
.sls-list__item:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.sls-field { display: flex; align-items: center; gap: 8px; padding: 4px 8px; border-radius: 8px; font-size: 13px; color: var(--text-2); cursor: pointer; }
.sls-field:hover { background: var(--hover); }
.sls-field input { accent-color: var(--accent); margin: 0; }
.sls-field__name { flex: 1; min-width: 0; font-family: var(--mono); font-size: 12.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sls-field__type { font-size: 11.5px; color: var(--muted); }
.sls-muted { margin: 0; padding: 4px 8px; font-size: 12.5px; color: var(--muted); }
.sls-inline-error { margin: 0; padding: 4px 8px; font-size: 12.5px; color: var(--danger); }

/* ── Main ── */
.sls-main { display: flex; flex-direction: column; min-width: 0; min-height: 0; overflow-y: auto; padding: 16px 20px 24px; gap: 12px; }
.sls-main > * { flex-shrink: 0; }
.sls-bar { position: sticky; top: -16px; z-index: 2; display: flex; gap: 8px; margin: -16px -20px 0; padding: 16px 20px 12px; background: var(--bg); }
.sls-bar__query { flex: 1; min-width: 0; display: flex; align-items: center; gap: 8px; height: 36px; padding: 0 12px; border: 1px solid var(--border-2); border-radius: 8px; background: var(--surface); color: var(--muted); transition: border-color var(--dur, 140ms) ease, box-shadow var(--dur, 140ms) ease; }
.sls-bar__query:focus-within { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-ring); }
.sls-bar__input { flex: 1; min-width: 0; height: 100%; border: 0; outline: 0; background: transparent; color: var(--text); font-family: var(--mono); font-size: 13px; }
.sls-bar__input::placeholder { color: var(--muted); }
.sls-bar__range { width: 168px; height: 36px; }
.sls-bar__run { height: 36px; }

.sls-meta { display: flex; align-items: center; flex-wrap: wrap; gap: 6px 12px; font-size: 13px; color: var(--text-2); }
.sls-meta__count strong { color: var(--text); font-weight: 600; font-variant-numeric: tabular-nums; }
.sls-meta__range { color: var(--muted); font-variant-numeric: tabular-nums; }
.sls-meta__dim { color: var(--muted); font-size: 12.5px; font-variant-numeric: tabular-nums; }
.sls-chip { display: inline-flex; align-items: center; gap: 6px; height: 24px; padding: 0 8px; border: 1px solid var(--border-2); border-radius: 6px; background: transparent; color: var(--text-2); font: inherit; font-size: 12px; cursor: pointer; }
.sls-chip:hover { background: var(--hover); color: var(--text); }
.sls-badge { display: inline-flex; align-items: center; height: 22px; padding: 0 8px; border-radius: 6px; font-size: 12px; font-weight: 500; }
.sls-badge--warn { background: var(--warning-bg); color: var(--warning); }

/* Histogram */
.sls-hist { display: flex; flex-direction: column; gap: 4px; transition: opacity var(--dur, 140ms) ease; }
.sls-hist__bars { display: flex; align-items: flex-end; gap: 2px; height: 72px; padding-bottom: 1px; border-bottom: 1px solid var(--border); }
.sls-hist__bar { flex: 1; min-width: 2px; height: var(--h); padding: 0; border: 0; border-radius: 2px 2px 0 0; background: color-mix(in srgb, var(--accent) 55%, transparent); cursor: zoom-in; transition: background var(--dur, 140ms) ease; }
.sls-hist__bar:hover, .sls-hist__bar:focus-visible { background: var(--accent); outline: none; }
.sls-hist__bar:disabled { cursor: default; }
.sls-hist__axis { display: flex; justify-content: space-between; font-size: 11.5px; color: var(--muted); font-variant-numeric: tabular-nums; }
.is-busy { opacity: 0.55; }

.sls-error { padding: 10px 12px; border-radius: 8px; background: var(--danger-bg); color: var(--danger); font-size: 13px; line-height: 1.5; overflow-wrap: anywhere; }

/* Rows */
.sls-rows { list-style: none; margin: 0; padding: 0; border: 1px solid var(--border); border-radius: 12px; overflow: hidden; transition: opacity var(--dur, 140ms) ease; }
.sls-row { position: relative; border-top: 1px solid var(--border); }
.sls-row:first-child { border-top: 0; }
/* A colored edge is allowed here: it marks log severity. */
.sls-row--error::before, .sls-row--warn::before { content: ''; position: absolute; left: 0; top: 0; bottom: 0; width: 3px; }
.sls-row--error::before { background: var(--danger); }
.sls-row--warn::before { background: var(--warning); }
.sls-row__head { all: unset; box-sizing: border-box; display: grid; grid-template-columns: 14px 118px minmax(0, 1fr); align-items: start; gap: 8px; width: 100%; padding: 8px 12px; cursor: pointer; }
.sls-row__head:hover { background: var(--hover); }
.sls-row__head:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
.sls-row.is-open .sls-row__head { background: var(--surface-2); }
.sls-row__chev { margin-top: 3px; color: var(--muted); transition: transform var(--dur, 140ms) ease; }
.sls-row.is-open .sls-row__chev { transform: rotate(90deg); }
.sls-row__time { font-family: var(--mono); font-size: 12.5px; color: var(--text-2); font-variant-numeric: tabular-nums; line-height: 1.55; }
.sls-row__summary { display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden; font-family: var(--mono); font-size: 12.5px; line-height: 1.55; color: var(--text); overflow-wrap: anywhere; }
.sls-row__cols { display: flex; flex-wrap: wrap; gap: 4px 16px; font-family: var(--mono); font-size: 12.5px; line-height: 1.55; min-width: 0; }
.sls-row__cols .sls-kv { min-width: 13ch; max-width: 100%; margin-right: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sls-kv { margin-right: 12px; }
.sls-kv__k { color: var(--muted); }
.sls-kv__k::after { content: ': '; }
.sls-kv__v { color: var(--text); }

.sls-detail { padding: 4px 12px 12px 34px; background: var(--surface-2); }
.sls-detail__list { display: grid; grid-template-columns: minmax(96px, max-content) minmax(0, 1fr); gap: 2px 16px; margin: 0; font-size: 12.5px; }
.sls-detail__list--meta { margin-top: 10px; padding-top: 10px; border-top: 1px solid var(--border); }
.sls-detail__list--meta dt, .sls-detail__list--meta pre { color: var(--muted); }
.sls-detail__list dt { padding: 4px 0; font-family: var(--mono); color: var(--text-2); overflow-wrap: anywhere; }
.sls-detail__list dd { position: relative; display: flex; align-items: flex-start; gap: 8px; margin: 0; padding: 4px 0; min-width: 0; }
.sls-detail__list pre { flex: 1; min-width: 0; margin: 0; font-family: var(--mono); font-size: 12.5px; line-height: 1.55; color: var(--text); white-space: pre-wrap; overflow-wrap: anywhere; }
.sls-detail__actions { display: flex; gap: 4px; opacity: 0; transition: opacity var(--dur, 140ms) ease; }
.sls-detail__list dd:hover .sls-detail__actions, .sls-detail__actions:focus-within { opacity: 1; }

/* SQL table */
.sls-table-wrap { overflow: auto; border: 1px solid var(--border); border-radius: 12px; }
.sls-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.sls-table th { position: sticky; top: 0; padding: 8px 12px; background: var(--surface-2); color: var(--text-2); font-size: 12px; font-weight: 500; text-align: left; white-space: nowrap; border-bottom: 1px solid var(--border); }
.sls-table td { padding: 7px 12px; border-top: 1px solid var(--border); font-family: var(--mono); font-size: 12.5px; color: var(--text); font-variant-numeric: tabular-nums; white-space: nowrap; }
.sls-table tbody tr:hover td { background: var(--hover); }

.sls-more { display: flex; align-items: center; gap: 12px; justify-content: center; padding-top: 4px; }

/* Skeletons */
.sls-skel { display: flex; flex-direction: column; gap: 6px; padding: 4px 6px; }
.sls-skel span { display: block; height: 14px; border-radius: 6px; background: var(--surface-2); animation: sls-pulse 1.2s ease-in-out infinite; }
.sls-skel span:nth-child(odd) { width: 80%; }
.sls-skel--rows span { height: 34px; width: 100%; }
@keyframes sls-pulse { 50% { opacity: 0.5; } }

.sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }

@media (prefers-reduced-motion: reduce) {
  .sls-skel span { animation: none; }
  .sls-row__chev, .sls-hist__bar, .sls-list__item { transition: none; }
}

@media (max-width: 900px) {
  .sls-work { grid-template-columns: 1fr; grid-template-rows: auto minmax(0, 1fr); }
  .sls-side { max-height: 38vh; border-right: 0; border-bottom: 1px solid var(--border); }
  .sls-side__section--fields { display: none; }
  .sls-main { padding: 12px 16px 20px; }
  .sls-bar { flex-wrap: wrap; margin: -12px -16px 0; padding: 12px 16px; top: -12px; }
  .sls-bar__query { flex-basis: 100%; height: 40px; }
  .sls-bar__range { flex: 1; width: auto; height: 40px; }
  .sls-bar__run { height: 40px; }
  .sls-row__head { grid-template-columns: 14px minmax(0, 1fr); }
  .sls-row__time { grid-column: 2; }
  .sls-row__summary, .sls-row__cols { grid-column: 2; }
  .sls-detail { padding-left: 12px; }
  .sls-detail__list { grid-template-columns: 1fr; }
  .sls-detail__list dt { padding-bottom: 0; }
  .sls-detail__actions { opacity: 1; }
}
</style>
