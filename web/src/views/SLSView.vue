<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useConnections } from '@/composables/useConnections'
import { useSLS, type SLSHistogramBucket, type SLSLogRow } from '@/composables/useSLS'
import { useToast } from '@/composables/useToast'
import { useListFilter } from '@/composables/useListFilter'
import ConnectionGate from '@/components/ui/ConnectionGate.vue'

const props = defineProps<{ activeConnId: number | null }>()
const emit = defineEmits<{ (e: 'set-conn', id: number): void }>()

const QUICK_RANGES = [
  { key: '15m', label: '15m', seconds: 15 * 60 },
  { key: '1h', label: '1h', seconds: 60 * 60 },
  { key: '4h', label: '4h', seconds: 4 * 60 * 60 },
  { key: '24h', label: '24h', seconds: 24 * 60 * 60 },
  { key: '7d', label: '7d', seconds: 7 * 24 * 60 * 60 },
]
const PAGE_SIZE = 100

const { connections, fetchConnections } = useConnections()
const sls = useSLS()
const toast = useToast()

const project = ref('')
const logstores = ref<string[]>([])
const { search: logstoreSearch, filtered: filteredLogstores } = useListFilter(logstores, (name, q) => name.toLowerCase().includes(q))
const selectedLogstore = ref('')
const query = ref('')
const range = ref('15m')
const timeWindow = ref<{ from: number; to: number } | null>(null)
const logs = ref<SLSLogRow[]>([])
const buckets = ref<SLSHistogramBucket[]>([])
const total = ref(0)
const progress = ref('')
const tookMs = ref<number | null>(null)
const expanded = ref<Set<number>>(new Set())
const loadingStores = ref(false)
const searching = ref(false)
const loadingMore = ref(false)
const hasMore = ref(false)
const errorText = ref('')

const activeConn = computed(() =>
  props.activeConnId != null ? connections.value.find((c) => c.id === props.activeConnId) ?? null : null,
)
const slsConnections = computed(() => connections.value.filter((c) => c.driver === 'alibaba_sls'))
const isSLS = computed(() => activeConn.value?.driver === 'alibaba_sls')
const bucketMax = computed(() => Math.max(...buckets.value.map((b) => b.count), 1))

onMounted(async () => {
  if (!connections.value.length) await fetchConnections()
  if (!isSLS.value && slsConnections.value.length === 1) {
    emit('set-conn', slsConnections.value[0].id)
    return
  }
  if (isSLS.value) await loadLogstores()
})

watch(() => props.activeConnId, async () => {
  resetWorkspace()
  if (isSLS.value) await loadLogstores()
})

async function loadLogstores() {
  if (!activeConn.value) return
  loadingStores.value = true
  errorText.value = ''
  try {
    const data = await sls.logstores(activeConn.value.id)
    project.value = data.project
    logstores.value = data.logstores ?? []
    if (!selectedLogstore.value && logstores.value.length) await selectLogstore(logstores.value[0])
  } catch (e: any) {
    errorText.value = e?.response?.data?.error ?? 'Failed to list logstores'
  } finally {
    loadingStores.value = false
  }
}

async function selectLogstore(name: string) {
  selectedLogstore.value = name
  await search()
}

async function search() {
  if (!activeConn.value || !selectedLogstore.value) return
  const seconds = QUICK_RANGES.find((r) => r.key === range.value)?.seconds ?? 15 * 60
  const to = Math.floor(Date.now() / 1000)
  timeWindow.value = { from: to - seconds, to }
  searching.value = true
  errorText.value = ''
  expanded.value = new Set()
  const payload = { logstore: selectedLogstore.value, query: query.value, ...timeWindow.value, line: PAGE_SIZE, offset: 0 }
  try {
    const [logsRes, histRes] = await Promise.allSettled([
      sls.logs(activeConn.value.id, payload),
      sls.histogram(activeConn.value.id, payload),
    ])
    if (logsRes.status === 'rejected') throw logsRes.reason
    logs.value = logsRes.value.logs ?? []
    progress.value = logsRes.value.progress
    tookMs.value = logsRes.value.took_ms
    hasMore.value = !logsRes.value.has_sql && logs.value.length === PAGE_SIZE
    if (histRes.status === 'fulfilled') {
      buckets.value = histRes.value.buckets ?? []
      total.value = histRes.value.total
    } else {
      buckets.value = []
      total.value = logs.value.length
    }
  } catch (e: any) {
    logs.value = []
    buckets.value = []
    total.value = 0
    hasMore.value = false
    errorText.value = e?.response?.data?.error ?? 'Search failed'
  } finally {
    searching.value = false
  }
}

async function loadMore() {
  if (!activeConn.value || !timeWindow.value) return
  loadingMore.value = true
  try {
    const data = await sls.logs(activeConn.value.id, {
      logstore: selectedLogstore.value,
      query: query.value,
      ...timeWindow.value,
      line: PAGE_SIZE,
      offset: logs.value.length,
    })
    logs.value = [...logs.value, ...(data.logs ?? [])]
    hasMore.value = (data.logs ?? []).length === PAGE_SIZE
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to load more logs')
  } finally {
    loadingMore.value = false
  }
}

function resetWorkspace() {
  project.value = ''
  logstores.value = []
  selectedLogstore.value = ''
  logs.value = []
  buckets.value = []
  total.value = 0
  progress.value = ''
  tookMs.value = null
  hasMore.value = false
  errorText.value = ''
  timeWindow.value = null
}

function toggleRow(index: number) {
  const next = new Set(expanded.value)
  if (next.has(index)) next.delete(index)
  else next.add(index)
  expanded.value = next
}

function rowTime(row: SLSLogRow) {
  const seconds = Number(row.__time__)
  if (!Number.isFinite(seconds) || seconds <= 0) return ''
  const d = new Date(seconds * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function rowSummary(row: SLSLogRow) {
  if (row.content) return row.content
  const fields = Object.entries(row).filter(([key]) => !key.startsWith('__'))
  return fields.map(([key, value]) => `${key}=${value}`).join('  ')
}

function rowFields(row: SLSLogRow) {
  return Object.entries(row).sort(([a], [b]) => Number(a.startsWith('__')) - Number(b.startsWith('__')) || a.localeCompare(b))
}

function bucketLabel(bucket?: SLSHistogramBucket) {
  if (!bucket) return ''
  return new Date(bucket.from * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}
</script>

<template>
  <div class="page-shell sls-root">
    <header class="sls-toolbar">
      <div class="sls-title">
        <h1>Log Service</h1>
        <p v-if="isSLS">{{ activeConn?.name }}<template v-if="project"> · {{ project }}</template></p>
      </div>
      <div v-if="isSLS" class="sls-actions">
        <select
          class="base-input sls-select"
          :value="activeConnId ?? ''"
          @change="emit('set-conn', Number(($event.target as HTMLSelectElement).value))"
        >
          <option value="" disabled>Select SLS project</option>
          <option v-for="conn in slsConnections" :key="conn.id" :value="conn.id">{{ conn.name }}</option>
        </select>
        <button class="base-btn base-btn--ghost" :disabled="loadingStores" @click="loadLogstores">Refresh</button>
      </div>
    </header>

    <ConnectionGate v-if="!isSLS" :drivers="['alibaba_sls']" label="Alibaba Cloud SLS" short="SLS" purpose="to search its logstores" icon="logs" @select="(id: number) => emit('set-conn', id)" />

    <main v-else class="sls-body">
      <aside class="sls-side">
        <div class="sls-side-title">Logstores</div>
        <input v-if="logstores.length" v-model="logstoreSearch" class="base-input sls-store-search" type="search" placeholder="Filter logstores…" />
        <button
          v-for="name in filteredLogstores"
          :key="name"
          class="sls-store"
          :class="{ 'sls-store--active': selectedLogstore === name }"
          @click="selectLogstore(name)"
        >
          {{ name }}
        </button>
        <div v-if="loadingStores" class="sls-muted">Loading logstores…</div>
        <div v-else-if="!logstores.length" class="sls-muted">No logstores in this project.</div>
        <div v-else-if="!filteredLogstores.length" class="sls-muted">No logstores match "{{ logstoreSearch }}".</div>
      </aside>

      <section class="sls-main">
        <form class="sls-searchbar" @submit.prevent="search">
          <input
            v-model="query"
            class="base-input sls-query"
            spellcheck="false"
            placeholder="Search statement, e.g. level: error and content: timeout, or * | select count(*) as pv"
          />
          <div class="sls-ranges">
            <button
              v-for="r in QUICK_RANGES"
              :key="r.key"
              type="button"
              class="sls-range"
              :class="{ active: range === r.key }"
              @click="range = r.key"
            >
              {{ r.label }}
            </button>
          </div>
          <button class="base-btn base-btn--primary" type="submit" :disabled="!selectedLogstore || searching">
            {{ searching ? 'Searching…' : 'Search' }}
          </button>
        </form>

        <div v-if="errorText" class="sls-error">{{ errorText }}</div>

        <template v-else-if="selectedLogstore">
          <div class="sls-meta">
            <span><strong>{{ total.toLocaleString() }}</strong> logs in the last {{ range }}</span>
            <span v-if="tookMs !== null">{{ tookMs }} ms</span>
            <span v-if="progress && progress !== 'Complete'" class="sls-incomplete">Results incomplete, try a narrower range</span>
          </div>

          <div v-if="buckets.some((b) => b.count > 0)" class="sls-histo">
            <div class="sls-histo-bars">
              <div v-for="b in buckets" :key="b.from" class="sls-histo-col" :title="`${bucketLabel(b)}: ${b.count.toLocaleString()} logs`">
                <div class="sls-histo-bar" :style="{ height: `${Math.max(2, Math.round((b.count / bucketMax) * 100))}%` }" />
              </div>
            </div>
            <div class="sls-histo-labels">
              <span>{{ bucketLabel(buckets[0]) }}</span>
              <span>{{ bucketLabel(buckets[Math.floor(buckets.length / 2)]) }}</span>
              <span>now</span>
            </div>
          </div>

          <div class="sls-stream">
            <div v-if="!logs.length && !searching" class="sls-muted sls-empty">No logs match this query in the selected range.</div>
            <div v-for="(row, index) in logs" :key="index" class="sls-row" :class="{ 'sls-row--open': expanded.has(index) }">
              <button class="sls-row-head" type="button" @click="toggleRow(index)">
                <span class="sls-row-time">{{ rowTime(row) }}</span>
                <span class="sls-row-text">{{ rowSummary(row) }}</span>
              </button>
              <dl v-if="expanded.has(index)" class="sls-fields">
                <template v-for="[key, value] in rowFields(row)" :key="key">
                  <dt>{{ key }}</dt>
                  <dd>{{ value }}</dd>
                </template>
              </dl>
            </div>
            <button v-if="hasMore" class="base-btn base-btn--ghost sls-more" :disabled="loadingMore" @click="loadMore">
              {{ loadingMore ? 'Loading…' : 'Load more' }}
            </button>
          </div>
        </template>
      </section>
    </main>
  </div>
</template>

<style scoped>
.sls-root { display: flex; flex-direction: column; height: 100%; overflow: hidden; background: var(--bg-body); }
.sls-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 16px 24px; border-bottom: 1px solid var(--border); background: var(--bg-body); min-height: 68px; }
.sls-title h1 { margin: 0; font-size: 20px; font-weight: 600; letter-spacing: -0.01em; color: var(--text-primary); }
.sls-title p { margin: 2px 0 0; font-size: 13px; color: var(--text-muted); }
.sls-actions { display: flex; align-items: center; gap: 8px; }
.sls-select { width: 220px; }
.sls-body { flex: 1; min-height: 0; display: flex; overflow: hidden; }
.sls-side { width: 260px; flex-shrink: 0; padding: 12px; border-right: 1px solid var(--border); background: var(--bg-surface); overflow-y: auto; }
.sls-side-title { font-size: 12px; font-weight: 600; color: var(--text-muted); margin-bottom: 8px; }
.sls-store-search { width: 100%; margin-bottom: 8px; }
.sls-store { width: 100%; border: 0; background: transparent; color: var(--text-secondary); text-align: left; padding: 7px 10px; border-radius: 6px; font-family: var(--mono, monospace); font-size: 12.5px; cursor: pointer; word-break: break-all; }
.sls-store:hover, .sls-store--active { background: var(--bg-elevated); color: var(--text-primary); }
.sls-muted { color: var(--text-muted); font-size: 12px; line-height: 1.45; }
.sls-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 12px; padding: 16px; overflow-y: auto; }
.sls-searchbar { display: flex; align-items: center; gap: 8px; }
.sls-query { flex: 1; min-width: 0; font-family: var(--mono, monospace); }
.sls-ranges { display: flex; border: 1px solid var(--border); border-radius: 6px; overflow: hidden; }
.sls-range { border: 0; background: transparent; color: var(--text-muted); padding: 6px 10px; font-size: 12px; cursor: pointer; }
.sls-range.active { background: var(--accent); color: white; }
.sls-error { padding: 10px 12px; border: 1px solid var(--danger); border-radius: 8px; color: var(--danger); font-size: 13px; }
.sls-meta { display: flex; align-items: center; gap: 14px; font-size: 12px; color: var(--text-muted); }
.sls-meta strong { color: var(--text-primary); }
.sls-incomplete { color: var(--warning); }
.sls-histo { border: 1px solid var(--border); background: var(--bg-elevated); border-radius: 8px; padding: 8px 12px 5px; }
.sls-histo-bars { display: flex; align-items: flex-end; height: 48px; gap: 1.5px; }
.sls-histo-col { flex: 1; height: 100%; display: flex; align-items: flex-end; cursor: crosshair; }
.sls-histo-bar { width: 100%; min-height: 2px; background: color-mix(in srgb, var(--accent) 60%, transparent); border-radius: 2px 2px 0 0; transition: background .15s; }
.sls-histo-col:hover .sls-histo-bar { background: var(--accent); }
.sls-histo-labels { display: flex; justify-content: space-between; margin-top: 3px; }
.sls-histo-labels span { font-size: 10px; color: var(--text-muted); font-family: var(--mono, monospace); }
.sls-stream { display: flex; flex-direction: column; border: 1px solid var(--border); border-radius: 10px; background: var(--bg-elevated); overflow: hidden; }
.sls-empty { padding: 16px; }
.sls-row { border-bottom: 1px solid var(--border); }
.sls-row:last-of-type { border-bottom: 0; }
.sls-row-head { display: flex; gap: 12px; width: 100%; border: 0; background: transparent; padding: 8px 12px; text-align: left; cursor: pointer; font-family: var(--mono, monospace); font-size: 12px; color: var(--text-secondary); }
.sls-row-head:hover, .sls-row--open .sls-row-head { background: var(--bg-surface); }
.sls-row-time { flex-shrink: 0; width: 110px; color: var(--text-muted); }
.sls-row-text { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sls-row--open .sls-row-text { white-space: pre-wrap; word-break: break-all; }
.sls-fields { display: grid; grid-template-columns: minmax(120px, 220px) 1fr; gap: 4px 12px; margin: 0; padding: 8px 12px 12px 134px; font-family: var(--mono, monospace); font-size: 12px; }
.sls-fields dt { color: var(--text-muted); word-break: break-all; }
.sls-fields dd { margin: 0; color: var(--text-primary); white-space: pre-wrap; word-break: break-all; }
.sls-more { margin: 10px auto; }
@media (max-width: 900px) {
  .sls-toolbar, .sls-body, .sls-searchbar { flex-direction: column; align-items: stretch; }
  .sls-side { width: auto; max-height: 200px; border-right: 0; border-bottom: 1px solid var(--border); }
  .sls-fields { padding-left: 12px; grid-template-columns: 1fr; }
  .sls-row-time { width: auto; }
}
</style>
