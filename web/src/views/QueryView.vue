<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import type { CompletionSource } from '@codemirror/autocomplete'
import axios from 'axios'
import QueryEditor from '@/components/database/QueryEditor.vue'
import VirtualTable from '@/components/database/VirtualTable.vue'
import SchemaTree from '@/components/database/SchemaTree.vue'
import ShortcutsModal from '@/components/ui/ShortcutsModal.vue'
import ExportCodeModal from '@/components/ui/ExportCodeModal.vue'
import ExplainTree from '@/components/database/ExplainTree.vue'
import ColumnProfiler from '@/components/ui/ColumnProfiler.vue'
import ParamPanel from '@/components/ui/ParamPanel.vue'
import SnippetLibrary from '@/components/ui/SnippetLibrary.vue'
import ResultChart from '@/components/ui/ResultChart.vue'
import ResultDiff from '@/components/ui/ResultDiff.vue'
import AIAssistant from '@/components/ui/AIAssistant.vue'
import DatabasePicker from '@/components/ui/DatabasePicker.vue'
import { useQuery, type QueryResult, type HistoryItem } from '@/composables/useQuery'
import { useConnections } from '@/composables/useConnections'
import { useTheme } from '@/composables/useTheme'
import { useSchemaCompletion } from '@/composables/useSchemaCompletion'
import { useSavedQueries } from '@/composables/useSavedQueries'
import { useDatabases } from '@/composables/useDatabases'
import { downloadCSV, downloadJSON } from '@/utils/export'
import { formatSQL } from '@/utils/sqlFormat'

const props = defineProps<{ activeConnId?: number | null }>()

const { connections } = useConnections()
const { fetchHistory, clearHistory } = useQuery()
const { mode } = useTheme()
const { getCompletionSource } = useSchemaCompletion()
const { queries: savedQueries, fetchAll: fetchSaved, save: saveQuery, remove: removeQuery } = useSavedQueries()
const { databases, fetchDatabases } = useDatabases()

const selectedDatabase = ref('')
const showShortcuts = ref(false)
const showExportCode = ref(false)
const showSnippets = ref(false)

// Param substitution
const queryParams = ref<Record<string, string>>({})
const paramPanelRef = ref<InstanceType<typeof ParamPanel> | null>(null)

function buildParamSQL(sql: string): string {
  return sql.replace(/:([a-zA-Z_][a-zA-Z0-9_]*)/g, (_, name) => {
    const val = queryParams.value[name] ?? ''
    if (val === '') return `:${name}`
    return isNaN(Number(val)) || val.trim() === '' ? `'${val.replace(/'/g, "''")}'` : val
  })
}

// Pinned results
interface PinnedResult { id: string; label: string; columns: string[]; rows: unknown[][] }
const pinnedResults = ref<PinnedResult[]>([])
const showPinned = ref(false)

function pinCurrentResult() {
  if (!activeTab.value?.result) return
  const label = activeTab.value.sql.slice(0, 40).replace(/\n/g, ' ').trim()
  pinnedResults.value.unshift({
    id: crypto.randomUUID(),
    label,
    columns: [...activeTab.value.result.columns],
    rows: [...activeTab.value.result.rows] as unknown[][],
  })
  showPinned.value = true
}

function unpin(id: string) {
  pinnedResults.value = pinnedResults.value.filter((p) => p.id !== id)
}

function insertSnippet(sql: string) {
  if (!activeTab.value) return
  activeTab.value.sql = activeTab.value.sql
    ? activeTab.value.sql + '\n\n' + sql
    : sql
}

// ── Active connection ─────────────────────────────────────────────
const activeConn = computed(() =>
  props.activeConnId
    ? connections.value.find((c) => c.id === props.activeConnId)
    : connections.value[0] ?? null,
)
const connId = computed(() => activeConn.value?.id ?? null)

// Transaction state
const txActive = ref(false)

async function txBegin() {
  if (!connId.value) return
  await axios.post(`/api/connections/${connId.value}/transaction/begin`)
  txActive.value = true
}
async function txCommit() {
  if (!connId.value) return
  await axios.post(`/api/connections/${connId.value}/transaction/commit`)
  txActive.value = false
}
async function txRollback() {
  if (!connId.value) return
  await axios.post(`/api/connections/${connId.value}/transaction/rollback`)
  txActive.value = false
}

watch(connId, () => { txActive.value = false })

// ── Tabs ─────────────────────────────────────────────────────────
let tabCounter = 1
interface QueryTab {
  id: string
  name: string
  sql: string
  result: QueryResult | null
  error: string
  running: boolean
}

function makeTab(): QueryTab {
  return {
    id: `tab-${tabCounter++}`,
    name: `Query ${tabCounter - 1}`,
    sql: 'SELECT 1;',
    result: null,
    error: '',
    running: false,
  }
}

const tabs = ref<QueryTab[]>([makeTab()])
const activeTabId = ref(tabs.value[0].id)
const activeTab = computed(() => tabs.value.find((t) => t.id === activeTabId.value) ?? tabs.value[0])

function addTab() {
  const t = makeTab()
  tabs.value.push(t)
  activeTabId.value = t.id
}

function closeTab(id: string) {
  if (tabs.value.length === 1) return
  const idx = tabs.value.findIndex((t) => t.id === id)
  tabs.value.splice(idx, 1)
  if (activeTabId.value === id) {
    activeTabId.value = tabs.value[Math.max(0, idx - 1)].id
  }
}

function tabLabel(tab: QueryTab) {
  const s = tab.sql.trim().replace(/\s+/g, ' ').slice(0, 22)
  return s.length < tab.sql.trim().length ? s + '…' : s || tab.name
}

// ── Run / Explain / Format / Cancel ──────────────────────────────
const abortControllers = new Map<string, AbortController>()

function formatCurrentSQL() {
  if (!activeTab.value) return
  const driver = activeConn.value?.driver ?? 'sql'
  activeTab.value.sql = formatSQL(activeTab.value.sql, driver)
}

function cancelQuery() {
  if (!activeTab.value) return
  const ctrl = abortControllers.get(activeTab.value.id)
  ctrl?.abort()
}

async function runQuery() {
  if (!connId.value || !activeTab.value) return
  const tab = activeTab.value
  const ctrl = new AbortController()
  abortControllers.set(tab.id, ctrl)
  tab.running = true
  tab.error = ''
  tab.result = null
  try {
    const axios = (await import('axios')).default
      const { data } = await axios.post<QueryResult>(
                      `/api/connections/${connId.value}/query`,
                      { sql: buildParamSQL(tab.sql), database: selectedDatabase.value || undefined },
                      { signal: ctrl.signal },
                    )
    tab.result = data
    axios.post(`/api/connections/${connId.value}/history`, {
      sql: tab.sql, duration_ms: data.duration_ms, row_count: data.row_count,
    }).catch(() => {})
    historyItems.value.unshift({ sql: tab.sql, time: new Date(), connId: connId.value!, duration_ms: data.duration_ms, row_count: data.row_count })
  } catch (e: unknown) {
    const err = e as { code?: string; response?: { data?: { error?: string } } }
    if (err.code === 'ERR_CANCELED') {
      tab.error = 'Query cancelled.'
    } else {
      tab.error = err.response?.data?.error ?? 'Query failed'
      lastQueryError.value = tab.error
    }
  } finally {
    tab.running = false
    abortControllers.delete(tab.id)
  }
}

async function runExplain() {
  if (!connId.value || !activeTab.value) return
  const tab = activeTab.value
  tab.running = true
  tab.error = ''
  tab.result = null
  try {
    const { data } = await (await import('axios')).default.post<QueryResult>(
      `/api/connections/${connId.value}/query`,
      { sql: `EXPLAIN ${tab.sql}`, database: selectedDatabase.value || undefined },
    )
    tab.result = data
  } catch (e: unknown) {
    const msg = (e as { response?: { data?: { error?: string } } })?.response?.data?.error ?? 'Explain failed'
    tab.error = msg
  } finally {
    tab.running = false
  }
}

// Global keyboard shortcuts
function handleGlobalKey(e: KeyboardEvent) {
  if (e.key === '?' && !(e.target instanceof HTMLInputElement) && !(e.target instanceof HTMLTextAreaElement)) {
    showShortcuts.value = !showShortcuts.value
    return
  }
  if (e.key === 'Escape') {
    showShortcuts.value = false
    return
  }
  if ((e.ctrlKey || e.metaKey) && e.key === 't') {
    e.preventDefault()
    addTab()
    return
  }
  if ((e.ctrlKey || e.metaKey) && e.key === 'w') {
    e.preventDefault()
    if (activeTab.value && tabs.value.length > 1) closeTab(activeTab.value.id)
    return
  }
  if ((e.ctrlKey || e.metaKey) && e.shiftKey && e.key === 'F') {
    e.preventDefault()
    formatCurrentSQL()
    return
  }
}

onMounted(() => window.addEventListener('keydown', handleGlobalKey))
onBeforeUnmount(() => window.removeEventListener('keydown', handleGlobalKey))

// ── Schema completion ─────────────────────────────────────────────
const schemaCompletion = ref<CompletionSource | null>(null)

watch(
  () => activeConn.value,
  async (conn) => {
    schemaCompletion.value = null
    selectedDatabase.value = ''
    if (!conn) return
    await fetchDatabases(conn.id)
    selectedDatabase.value = conn.database || (databases.value[0] ?? '')
    const db = selectedDatabase.value || 'public'
    schemaCompletion.value = await getCompletionSource(conn.id, db)
  },
  { immediate: true },
)

watch(selectedDatabase, async (db) => {
  if (!activeConn.value || !db) return
  schemaCompletion.value = await getCompletionSource(activeConn.value.id, db)
})

// ── Schema panel ──────────────────────────────────────────────────
const schemaVisible = ref(true)

function onSchemaSelect(payload: { db: string; table: string }) {
  if (activeTab.value) {
    activeTab.value.sql = `SELECT *\nFROM ${payload.table}\nLIMIT 100;`
  }
}

// ── Multi-statement runner ────────────────────────────────────────
interface ScriptResult {
  index: number; sql: string; columns: string[]; rows: unknown[][]
  row_count: number; affected_rows: number; duration_ms: number; error?: string
}
const scriptResults = ref<ScriptResult[]>([])
const scriptRunning = ref(false)

async function runScript() {
  if (!connId.value || !activeTab.value?.sql) return
  scriptRunning.value = true
  scriptResults.value = []
  activeResultTab.value = 'script'
  try {
    const { data } = await axios.post<ScriptResult[]>(
      `/api/connections/${connId.value}/script`,
      { sql: buildParamSQL(activeTab.value.sql), database: selectedDatabase.value || undefined }
    )
    scriptResults.value = data
  } catch (e: unknown) {
    const msg = (e as { response?: { data?: { error?: string } } })?.response?.data?.error ?? 'Script failed'
    if (activeTab.value) activeTab.value.error = msg
  } finally {
    scriptRunning.value = false
  }
}

// ── AI panel ──────────────────────────────────────────────────────
const showAI = ref(false)
const lastQueryError = ref('')

// ── Diff between pinned results ───────────────────────────────────
const diffLeft = ref<string>('')
const diffRight = ref<string>('')

// ── History / Saved queries / Explain panel ───────────────────────
type ResultTab = 'results' | 'history' | 'saved' | 'explain' | 'chart' | 'script' | 'diff'
const activeResultTab = ref<ResultTab>('results')

// Explain
const explainResult = ref<any>(null)
const explainLoading = ref(false)

// Profiler
const profilerShow = ref(false)
const profilerColumn = ref('')

function openProfiler(col: string) {
  profilerColumn.value = col
  profilerShow.value = true
}

// Streaming
const streamMode = ref(false)
const streamRows = ref<unknown[][]>([])
const streamCols = ref<string[]>([])
const streamLoading = ref(false)
const streamCount = ref(0)
const streamDurationMs = ref(0)
let streamAbort: AbortController | null = null

async function runStreamQuery() {
  if (!connId.value || !activeTab.value?.sql) return
  streamRows.value = []; streamCols.value = []; streamCount.value = 0
  streamLoading.value = true
  const tab = activeTab.value
  tab.running = true
  streamAbort = new AbortController()
  try {
    const resp = await fetch(`/api/connections/${connId.value}/query/stream`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sql: tab.sql, database: selectedDatabase.value || undefined }),
      signal: streamAbort.signal,
    })
    const reader = resp.body!.getReader()
    const dec = new TextDecoder()
    let buf = ''
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buf += dec.decode(value, { stream: true })
      const parts = buf.split('\n\n')
      buf = parts.pop() ?? ''
      for (const part of parts) {
        for (const line of part.split('\n')) {
          if (!line.startsWith('data: ')) continue
          const obj = JSON.parse(line.slice(6))
          if (obj.columns) { streamCols.value = obj.columns }
          else if (obj.row) { streamRows.value.push(obj.row); streamCount.value++ }
          else if (obj.done) { streamDurationMs.value = obj.duration_ms }
        }
      }
    }
  } catch (e: any) {
    if (e?.name !== 'AbortError' && tab) tab.error = 'Stream aborted'
  } finally {
    streamLoading.value = false
    tab.running = false
    streamAbort = null
  }
}

function stopStream() {
  streamAbort?.abort()
}

async function runExplainPlan() {
  if (!connId.value || !activeTab.value?.sql) return
  explainLoading.value = true
  explainResult.value = null
  activeResultTab.value = 'explain'
  try {
    const { data } = await axios.post(`/api/connections/${connId.value}/explain`, { sql: activeTab.value.sql })
    explainResult.value = data
  } catch (e: any) {
    explainResult.value = { error: e?.response?.data?.error ?? 'Explain failed' }
  } finally {
    explainLoading.value = false
  }
}
const historyItems = ref<HistoryItem[]>([])
const loadingHistory = ref(false)

// Save dialog state
const saveDialogOpen = ref(false)
const saveName = ref('')
const saveDesc = ref('')

async function openSaveDialog() {
  saveName.value = activeTab.value?.sql.trim().slice(0, 40) ?? ''
  saveDesc.value = ''
  saveDialogOpen.value = true
}

async function confirmSave() {
  if (!saveName.value.trim() || !activeTab.value?.sql) return
  await saveQuery(saveName.value.trim(), activeTab.value.sql, saveDesc.value, connId.value)
  saveDialogOpen.value = false
  await fetchSaved()
}

function loadSavedQuery(sql: string) {
  if (activeTab.value) {
    activeTab.value.sql = sql
    activeResultTab.value = 'results'
  }
}

watch(connId, () => fetchSaved(), { immediate: true })

async function loadHistory() {
  if (!connId.value) return
  loadingHistory.value = true
  historyItems.value = await fetchHistory(connId.value)
  loadingHistory.value = false
}

async function doClearHistory() {
  if (!connId.value) return
  await clearHistory(connId.value)
  historyItems.value = []
}

watch(connId, (id) => {
  if (id) loadHistory()
}, { immediate: true })

function useHistoryItem(item: HistoryItem) {
  if (activeTab.value) {
    activeTab.value.sql = item.sql
    activeResultTab.value = 'results'
  }
}

// ── Draggable split pane ──────────────────────────────────────────
const editorRatio = ref(0.45)
const splitRef = ref<HTMLElement>()

function onDividerMousedown(e: MouseEvent) {
  e.preventDefault()
  const el = splitRef.value
  if (!el) return
  const startY = e.clientY
  const startRatio = editorRatio.value
  const totalH = el.getBoundingClientRect().height

  function onMove(ev: MouseEvent) {
    const delta = ev.clientY - startY
    const newRatio = Math.min(0.85, Math.max(0.15, startRatio + delta / totalH))
    editorRatio.value = newRatio
  }
  function onUp() {
    window.removeEventListener('mousemove', onMove)
    window.removeEventListener('mouseup', onUp)
  }
  window.addEventListener('mousemove', onMove)
  window.addEventListener('mouseup', onUp)
}

// ── Export ────────────────────────────────────────────────────────
function exportResults(format: 'csv' | 'json') {
  const r = activeTab.value?.result
  if (!r) return
  if (format === 'csv') downloadCSV(r.columns, r.rows as unknown[][], 'query-results')
  else downloadJSON(r.columns, r.rows as unknown[][], 'query-results')
}
</script>

<template>
  <div class="query-view">
    <!-- ── Tab bar ──────────────────────────────────────────────── -->
    <div class="query-tabbar">
      <div class="query-tabbar__tabs">
        <button
          v-for="tab in tabs"
          :key="tab.id"
          class="qtab"
          :class="{ 'qtab--active': tab.id === activeTabId, 'qtab--running': tab.running }"
          @click="activeTabId = tab.id"
        >
          <span class="qtab__dot" :class="{ 'qtab__dot--err': tab.error, 'qtab__dot--ok': tab.result && !tab.error }" />
          <span class="qtab__label">{{ tabLabel(tab) }}</span>
          <button
            v-if="tabs.length > 1"
            class="qtab__close"
            @click.stop="closeTab(tab.id)"
            title="Close tab"
          >×</button>
        </button>
      </div>
      <button class="qtab-add" @click="addTab" title="New tab">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
      </button>
    </div>

    <!-- ── Toolbar ─────────────────────────────────────────────── -->
    <div class="query-toolbar">
      <div class="query-toolbar__conn">
        <template v-if="activeConn">
          <span class="query-toolbar__conn-driver">{{ activeConn.driver.toUpperCase() }}</span>
          <span class="query-toolbar__conn-name">{{ activeConn.name }}</span>
        </template>
        <span v-else class="query-toolbar__conn-none">No connection selected</span>
      </div>
      <div class="query-toolbar__spacer" />

      <button
        class="base-btn base-btn--ghost base-btn--sm"
        @click="schemaVisible = !schemaVisible"
        :title="schemaVisible ? 'Hide schema' : 'Show schema'"
      >
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="12 2 2 7 12 12 22 7 12 2"/><polyline points="2 17 12 22 22 17"/><polyline points="2 12 12 17 22 12"/></svg>
        Schema
      </button>

      <!-- Database selector -->
      <DatabasePicker
        v-if="databases.length > 1"
        v-model="selectedDatabase"
        :databases="databases"
      />

      <!-- Transaction controls -->
      <template v-if="connId">
        <div v-if="txActive" class="tx-indicator" title="Transaction active">
          <span class="tx-dot" />
          TX
        </div>
        <button v-if="!txActive" class="base-btn base-btn--ghost base-btn--sm" @click="txBegin" title="Begin transaction">BEGIN</button>
        <button v-if="txActive" class="base-btn base-btn--ghost base-btn--sm" style="color:#4ade80" @click="txCommit">COMMIT</button>
        <button v-if="txActive" class="base-btn base-btn--ghost base-btn--sm" style="color:#f87171" @click="txRollback">ROLLBACK</button>
        <div class="vdivider" />
      </template>

      <!-- Format -->
      <button
        class="base-btn base-btn--ghost base-btn--sm"
        :disabled="!activeTab?.sql.trim()"
        @click="formatCurrentSQL"
        title="Format SQL (Ctrl+Shift+F)"
      >
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="21" y1="10" x2="7" y2="10"/><line x1="21" y1="6" x2="3" y2="6"/><line x1="21" y1="14" x2="3" y2="14"/><line x1="21" y1="18" x2="7" y2="18"/></svg>
        Format
      </button>

      <button
        class="base-btn base-btn--ghost base-btn--sm"
        :disabled="!connId || !activeTab?.sql.trim() || activeTab?.running"
        @click="runExplainPlan"
      >
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
        Explain
      </button>

      <!-- Stop button (visible only when running) -->
      <button
        v-if="activeTab?.running"
        class="base-btn base-btn--danger base-btn--sm"
        @click="cancelQuery"
      >
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/></svg>
        Stop
      </button>

      <button
        v-else
        class="base-btn base-btn--primary base-btn--sm"
        :disabled="!connId || !activeTab?.sql.trim()"
        @click="streamMode ? runStreamQuery() : runQuery()"
      >
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polygon points="5 3 19 12 5 21 5 3"/></svg>
        {{ streamMode ? 'Stream' : 'Run' }}
      </button>
      <button
        v-if="streamLoading"
        class="base-btn base-btn--danger base-btn--sm"
        @click="stopStream"
      >
        <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor"><rect x="4" y="4" width="16" height="16" rx="2"/></svg>
        Stop
      </button>

      <!-- Stream toggle -->
      <label class="stream-toggle" title="Stream rows as they arrive">
        <input type="checkbox" v-model="streamMode" />
        <span>Stream</span>
      </label>

      <!-- Snippets -->
      <button class="base-btn base-btn--ghost base-btn--sm" @click="showSnippets=true" title="Snippet library">
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
        Snippets
      </button>

      <!-- Pin result -->
      <button
        class="base-btn base-btn--ghost base-btn--sm"
        :disabled="!activeTab?.result"
        @click="pinCurrentResult"
        title="Pin current result for comparison"
      >
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 10c0 7-9 13-9 13s-9-6-9-13a9 9 0 0 1 18 0z"/><circle cx="12" cy="10" r="3"/></svg>
        Pin
      </button>

      <!-- Export as code -->
      <button class="base-btn base-btn--ghost base-btn--sm" @click="showExportCode=true" :disabled="!activeTab?.sql.trim()" title="Export as code">
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
        Code
      </button>

      <!-- Script runner -->
      <button class="base-btn base-btn--ghost base-btn--sm" @click="runScript" :disabled="!connId || !activeTab?.sql.trim() || scriptRunning" title="Run all statements">
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg>
        Script
      </button>

      <!-- AI assistant -->
      <button class="base-btn base-btn--ghost base-btn--sm" :class="{ 'is-active': showAI }" @click="showAI = !showAI" title="AI query assistant">
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2a10 10 0 1 0 10 10"/><path d="M12 8v4l2 2"/><path d="M18 2l4 4-4 4"/><path d="M22 6H16"/></svg>
        AI
      </button>

      <!-- Shortcuts hint -->
      <button class="base-btn base-btn--ghost base-btn--sm shortcuts-btn" @click="showShortcuts=true" title="Keyboard shortcuts (?)">
        ?
      </button>
    </div>

    <!-- ── Body: schema + editor+results ─────────────────────────── -->
    <div class="query-body" :class="{ 'query-body--with-ai': showAI }">
      <!-- Schema panel -->
      <Transition name="schema-slide">
        <div v-if="schemaVisible" class="query-schema-panel">
          <div class="panel-header">Schema</div>
          <SchemaTree :connId="connId" @select-table="onSchemaSelect" />
        </div>
      </Transition>

      <!-- Editor + Results column (per active tab) -->
      <div class="query-main" ref="splitRef">
        <!-- Editor slot -->
        <div
          class="query-editor-slot"
          :style="{ flex: `${editorRatio} 1 0`, minHeight: '80px' }"
        >
          <template v-for="tab in tabs" :key="tab.id">
            <div v-show="tab.id === activeTabId" style="flex:1;min-height:0;display:flex;flex-direction:column;">
              <QueryEditor
                v-model="tab.sql"
                :dark-mode="mode === 'dark'"
                :schema-completion="schemaCompletion"
                @run="runQuery"
              />
            </div>
          </template>
          <div class="editor-hint">
            <kbd>Cmd/Ctrl</kbd>+<kbd>Enter</kbd> run &nbsp;·&nbsp; <kbd>Cmd/Ctrl</kbd>+<kbd>Space</kbd> suggest &nbsp;·&nbsp; <kbd>Tab</kbd> indent &nbsp;·&nbsp; <kbd>Ctrl</kbd>+<kbd>K</kbd> search schema
          </div>
          <!-- Param panel -->
          <ParamPanel
            ref="paramPanelRef"
            :sql="activeTab?.sql ?? ''"
            @update:params="queryParams = $event"
          />
        </div>

        <!-- Drag divider -->
        <div class="query-resize-handle" @mousedown="onDividerMousedown" />

        <!-- Results slot -->
        <div
          class="query-results-slot"
          :style="{ flex: `${1 - editorRatio} 1 0`, minHeight: '60px' }"
        >
          <!-- Tabs + meta -->
          <div class="query-results-tabs">
            <button
              class="result-tab"
              :class="{ 'is-active': activeResultTab === 'results' }"
              @click="activeResultTab = 'results'"
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
              Results
              <span v-if="activeTab?.result?.row_count" class="result-tab__badge">
                {{ activeTab.result.row_count.toLocaleString() }}
              </span>
            </button>
            <button
              class="result-tab"
              :class="{ 'is-active': activeResultTab === 'history' }"
              @click="activeResultTab = 'history'; loadHistory()"
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="12 8 12 12 14 14"/><path d="M3.05 11a9 9 0 1 0 .5-4"/><polyline points="3 3 3 7 7 7"/></svg>
              History
              <span v-if="historyItems.length" class="result-tab__badge">{{ historyItems.length }}</span>
            </button>
            <button
              class="result-tab"
              :class="{ 'is-active': activeResultTab === 'explain' }"
              @click="activeResultTab = 'explain'"
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
              Explain
            </button>
            <button
              class="result-tab"
              :class="{ 'is-active': activeResultTab === 'saved' }"
              @click="activeResultTab = 'saved'; fetchSaved()"
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/><polyline points="7 3 7 8 15 8"/></svg>
              Saved
              <span v-if="savedQueries.length" class="result-tab__badge">{{ savedQueries.length }}</span>
            </button>
            <button
              class="result-tab"
              :class="{ 'is-active': activeResultTab === 'chart' }"
              @click="activeResultTab = 'chart'"
              :disabled="!activeTab?.result"
              title="Visualize results as chart"
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="20" x2="18" y2="10"/><line x1="12" y1="20" x2="12" y2="4"/><line x1="6" y1="20" x2="6" y2="14"/></svg>
              Chart
            </button>
            <button
              class="result-tab"
              :class="{ 'is-active': activeResultTab === 'script' }"
              @click="activeResultTab = 'script'"
              :disabled="scriptResults.length === 0"
              title="Multi-statement results"
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg>
              Script
              <span v-if="scriptResults.length" class="result-tab__badge">{{ scriptResults.length }}</span>
            </button>
            <button
              class="result-tab"
              :class="{ 'is-active': activeResultTab === 'diff' }"
              @click="activeResultTab = 'diff'"
              :disabled="pinnedResults.length < 2"
              title="Compare two pinned results"
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="5" y1="12" x2="19" y2="12"/><polyline points="12 5 19 12 12 19"/></svg>
              Diff
            </button>
            <div style="flex:1" />
            <button class="base-btn base-btn--ghost base-btn--xs" @click="openSaveDialog" :disabled="!activeTab?.sql.trim()" title="Save current query">
              <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/><polyline points="7 3 7 8 15 8"/></svg>
              Save
            </button>
            <template v-if="activeTab?.result">
              <div class="result-meta">
                <span>{{ activeTab.result.duration_ms }}ms</span>
                <span v-if="activeTab.result.affected_rows">· {{ activeTab.result.affected_rows }} rows affected</span>
              </div>
              <button class="base-btn base-btn--ghost base-btn--xs" @click="exportResults('csv')">CSV</button>
              <button class="base-btn base-btn--ghost base-btn--xs" @click="exportResults('json')">JSON</button>
            </template>
          </div>

          <!-- Content -->
          <div class="query-results-body">
            <div v-if="activeTab?.error" class="notice notice--error" style="margin:10px;flex-shrink:0">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
              {{ activeTab.error }}
            </div>

            <template v-if="activeResultTab === 'results'">
              <!-- Streaming results -->
              <template v-if="streamMode && (streamCols.length || streamLoading)">
                <VirtualTable
                  :columns="streamCols"
                  :rows="streamRows"
                  :loading="streamLoading"
                  :selectable="true"
                />
                <div v-if="!streamLoading && streamCount > 0" class="stream-done-bar">
                  Streamed {{ streamCount.toLocaleString() }} rows in {{ streamDurationMs }}ms
                </div>
              </template>
              <!-- Regular results -->
              <template v-else>
                <VirtualTable
                  v-if="activeTab?.result"
                  :columns="activeTab.result.columns"
                  :rows="(activeTab.result.rows as unknown[][])"
                  :loading="activeTab?.running"
                  :selectable="true"
                  @profile-column="openProfiler"
                />
                <div v-else-if="!activeTab?.running && !activeTab?.error" class="empty-state" style="font-size:12.5px">
                  Run a query to see results here.
                </div>
              </template>
            </template>

            <template v-else-if="activeResultTab === 'explain'">
              <div class="explain-panel">
                <div v-if="explainLoading" class="empty-state" style="font-size:12px">
                  <svg class="spin" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
                  Analyzing…
                </div>
                <template v-else>
                  <div v-if="explainResult" class="explain-header">
                    <span class="explain-driver">{{ explainResult.driver }}</span>
                    <span class="explain-format">{{ explainResult.format }}</span>
                  </div>
                  <ExplainTree :result="explainResult" />
                </template>
              </div>
            </template>

            <template v-else-if="activeResultTab === 'history'">
              <div class="history-toolbar">
                <span style="font-size:11px;color:var(--text-muted)">{{ historyItems.length }} entries</span>
                <button v-if="historyItems.length" class="base-btn base-btn--ghost base-btn--xs" @click="doClearHistory">Clear</button>
              </div>
              <div style="flex:1;overflow-y:auto">
                <div v-if="loadingHistory" class="empty-state" style="font-size:12px">Loading…</div>
                <div
                  v-for="(item, idx) in historyItems"
                  :key="idx"
                  class="history-item"
                  @click="useHistoryItem(item)"
                >
                  <div class="history-item__sql">{{ item.sql }}</div>
                  <div class="history-item__meta">
                    <span :class="item.error ? 'history-item__err' : 'history-item__ok'">
                      {{ item.error ? '✗ Error' : `✓ ${item.row_count} rows` }}
                    </span>
                    <span>{{ item.duration_ms }}ms</span>
                    <span>{{ item.time.toLocaleTimeString() }}</span>
                  </div>
                </div>
                <div v-if="!loadingHistory && historyItems.length === 0" class="empty-state" style="font-size:12px">
                  No query history yet.
                </div>
              </div>
            </template>

            <template v-else-if="activeResultTab === 'saved'">
              <div class="history-toolbar">
                <span style="font-size:11px;color:var(--text-muted)">{{ savedQueries.length }} saved</span>
              </div>
              <div style="flex:1;overflow-y:auto">
                <div
                  v-for="q in savedQueries"
                  :key="q.id"
                  class="history-item"
                  @click="loadSavedQuery(q.sql)"
                >
                  <div style="display:flex;justify-content:space-between;align-items:center;gap:8px">
                    <span style="font-size:12px;font-weight:600;color:var(--text-primary);overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{{ q.name }}</span>
                    <button class="base-btn base-btn--ghost base-btn--xs" style="flex-shrink:0" @click.stop="removeQuery(q.id)">✕</button>
                  </div>
                  <div class="history-item__sql">{{ q.sql }}</div>
                  <div v-if="q.description" class="history-item__meta">{{ q.description }}</div>
                </div>
                <div v-if="savedQueries.length === 0" class="empty-state" style="font-size:12px">
                  No saved queries. Click "Save" to save the current query.
                </div>
              </div>
            </template>

            <!-- Chart tab -->
            <template v-else-if="activeResultTab === 'chart'">
              <div v-if="activeTab?.result" style="flex:1;min-height:0;padding:12px;overflow:hidden;display:flex;flex-direction:column;">
                <ResultChart
                  :columns="activeTab.result.columns"
                  :rows="(activeTab.result.rows as unknown[][])"
                />
              </div>
              <div v-else class="empty-state" style="font-size:12px">Run a query first to chart results.</div>
            </template>

            <!-- Script tab -->
            <template v-else-if="activeResultTab === 'script'">
              <div v-if="scriptRunning" class="empty-state" style="font-size:12px">
                <svg class="spin" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
                Running script…
              </div>
              <div v-else style="flex:1;overflow-y:auto;padding:8px;">
                <div v-for="sr in scriptResults" :key="sr.index" class="script-result">
                  <div class="script-result-header" :class="{ 'is-error': sr.error }">
                    <span class="script-result-num">#{{ sr.index + 1 }}</span>
                    <code class="script-result-sql">{{ sr.sql.slice(0, 80) }}{{ sr.sql.length > 80 ? '…' : '' }}</code>
                    <span class="script-result-meta">{{ sr.duration_ms }}ms</span>
                    <span v-if="sr.error" class="script-result-err">✗ {{ sr.error }}</span>
                    <span v-else-if="sr.columns.length" class="script-result-ok">{{ sr.row_count }} rows</span>
                    <span v-else class="script-result-ok">{{ sr.affected_rows }} affected</span>
                  </div>
                  <VirtualTable v-if="sr.columns.length && !sr.error"
                    :columns="sr.columns"
                    :rows="sr.rows"
                    style="max-height:200px"
                  />
                </div>
                <div v-if="scriptResults.length === 0" class="empty-state" style="font-size:12px">
                  Click Script to run all statements.
                </div>
              </div>
            </template>

            <!-- Diff tab -->
            <template v-else-if="activeResultTab === 'diff'">
              <div v-if="pinnedResults.length >= 2" style="flex:1;min-height:0;display:flex;flex-direction:column;">
                <div class="diff-selectors">
                  <label>Left</label>
                  <select v-model="diffLeft" class="diff-select">
                    <option v-for="p in pinnedResults" :key="p.id" :value="p.id">{{ p.label }}</option>
                  </select>
                  <label>Right</label>
                  <select v-model="diffRight" class="diff-select">
                    <option v-for="p in pinnedResults" :key="p.id" :value="p.id">{{ p.label }}</option>
                  </select>
                </div>
                <ResultDiff
                  v-if="diffLeft && diffRight && diffLeft !== diffRight"
                  :left="{ columns: pinnedResults.find(p=>p.id===diffLeft)!.columns, rows: pinnedResults.find(p=>p.id===diffLeft)!.rows }"
                  :right="{ columns: pinnedResults.find(p=>p.id===diffRight)!.columns, rows: pinnedResults.find(p=>p.id===diffRight)!.rows }"
                  style="flex:1;min-height:0"
                />
                <div v-else class="empty-state" style="font-size:12px">Select two different pinned results to compare.</div>
              </div>
              <div v-else class="empty-state" style="font-size:12px">Pin at least 2 results using the Pin button, then compare them here.</div>
            </template>
          </div>
        </div>
      </div>
    </div>

    <!-- AI assistant panel -->
    <Transition name="ai-slide">
      <div v-if="showAI" class="ai-panel">
        <AIAssistant
          :active-sql="activeTab?.sql"
          :last-error="lastQueryError"
          :connection-info="activeConn ? `${activeConn.driver} ${activeConn.name}` : undefined"
          @insert-sql="sql => { if(activeTab) { activeTab.sql = activeTab.sql ? activeTab.sql + '\n\n' + sql : sql } }"
        />
      </div>
    </Transition>

    <!-- Shortcuts modal -->
    <ShortcutsModal :show="showShortcuts" @close="showShortcuts=false" />

    <!-- Snippet Library -->
    <SnippetLibrary
      :show="showSnippets"
      @close="showSnippets=false"
      @insert="insertSnippet"
    />

    <!-- Pinned results drawer -->
    <Teleport to="body">
      <div v-if="showPinned && pinnedResults.length" class="pinned-drawer">
        <div class="pinned-drawer-header">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 10c0 7-9 13-9 13s-9-6-9-13a9 9 0 0 1 18 0z"/><circle cx="12" cy="10" r="3"/></svg>
          Pinned Results
          <div style="flex:1"/>
          <button class="base-btn base-btn--ghost base-btn--sm" @click="showPinned=false">Hide</button>
        </div>
        <div class="pinned-drawer-body">
          <div v-for="p in pinnedResults" :key="p.id" class="pinned-result">
            <div class="pinned-result-head">
              <span class="pinned-result-label">{{ p.label }}</span>
              <span class="pinned-result-rows">{{ p.rows.length }} rows</span>
              <button class="base-btn base-btn--ghost base-btn--sm" style="color:var(--danger)" @click="unpin(p.id)">×</button>
            </div>
            <VirtualTable
              :columns="p.columns"
              :rows="p.rows"
              :row-height="26"
              :show-row-numbers="false"
              style="height:180px"
            />
          </div>
        </div>
      </div>
    </Teleport>

    <!-- Column profiler -->
    <ColumnProfiler
      :show="profilerShow"
      :conn-id="connId"
      :table="activeTab?.name ?? ''"
      :column="profilerColumn"
      @close="profilerShow=false"
    />

    <!-- Export as code modal -->
    <ExportCodeModal
      :show="showExportCode"
      :sql="activeTab?.sql ?? ''"
      :connection="activeConn ?? null"
      @close="showExportCode=false"
    />

    <!-- Save dialog -->
    <Teleport to="body">
      <div v-if="saveDialogOpen" class="ci-overlay" @click.self="saveDialogOpen=false">
        <div class="save-dialog">
          <div style="font-size:14px;font-weight:600;color:var(--text-primary);margin-bottom:12px">Save Query</div>
          <label class="sdlg-label">Name</label>
          <input class="sdlg-input" v-model="saveName" placeholder="My query name" />
          <label class="sdlg-label" style="margin-top:10px">Description (optional)</label>
          <input class="sdlg-input" v-model="saveDesc" placeholder="What does this query do?" />
          <div style="display:flex;gap:8px;justify-content:flex-end;margin-top:16px">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="saveDialogOpen=false">Cancel</button>
            <button class="base-btn base-btn--primary base-btn--sm" @click="confirmSave" :disabled="!saveName.trim()">Save</button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.query-view {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-height: 0;
  overflow: hidden;
}

/* ── Tab bar ── */
.query-tabbar {
  display: flex;
  align-items: stretch;
  background: var(--bg-elevated);
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
  height: 36px;
  overflow: hidden;
}
.query-tabbar__tabs {
  display: flex;
  align-items: stretch;
  overflow-x: auto;
  flex: 1;
  scrollbar-width: none;
}
.query-tabbar__tabs::-webkit-scrollbar { display: none; }

.qtab {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 12px 0 10px;
  font-size: 12px;
  font-family: inherit;
  color: var(--text-muted);
  background: transparent;
  border: none;
  border-right: 1px solid var(--border);
  border-bottom: 2px solid transparent;
  cursor: pointer;
  white-space: nowrap;
  transition: color var(--dur), border-color var(--dur), background var(--dur);
  flex-shrink: 0;
  max-width: 200px;
}
.qtab:hover { color: var(--text-secondary); background: var(--bg-hover); }
.qtab--active { color: var(--text-primary); border-bottom-color: var(--brand); background: var(--bg-surface); }
.qtab--running { color: var(--brand); }

.qtab__dot {
  width: 6px; height: 6px;
  border-radius: 50%;
  background: var(--border-2);
  flex-shrink: 0;
  transition: background var(--dur);
}
.qtab__dot--ok  { background: var(--success); }
.qtab__dot--err { background: var(--danger); }

.qtab__label { overflow: hidden; text-overflow: ellipsis; flex: 1; min-width: 0; }

.qtab__close {
  display: flex; align-items: center; justify-content: center;
  width: 16px; height: 16px;
  border-radius: 3px;
  border: none;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  font-size: 14px;
  line-height: 1;
  flex-shrink: 0;
  padding: 0;
}
.qtab__close:hover { background: var(--bg-hover); color: var(--text-primary); }

.qtab-add {
  display: flex; align-items: center; justify-content: center;
  width: 36px;
  border: none;
  border-left: 1px solid var(--border);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  flex-shrink: 0;
  transition: color var(--dur), background var(--dur);
}
.qtab-add:hover { color: var(--text-primary); background: var(--bg-hover); }

/* ── Toolbar ── */
.query-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 14px;
  height: 42px;
  min-height: 42px;
  background: var(--bg-surface);
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.query-toolbar__conn {
  display: flex; align-items: center; gap: 6px;
  font-size: 12.5px; color: var(--text-secondary);
}
.query-toolbar__conn-name { display: flex; align-items: center; gap: 6px; }
.query-toolbar__conn-driver {
  font-size: 10px; font-weight: 600; text-transform: uppercase;
  background: var(--brand-dim); color: var(--brand);
  padding: 1px 6px; border-radius: 4px;
}
.query-toolbar__conn-none { color: var(--danger); }
.query-toolbar__spacer { flex: 1; }

/* ── Body ── */
.query-body { display: flex; flex: 1; min-height: 0; overflow: hidden; }

/* Schema panel */
.query-schema-panel {
  width: 230px; min-width: 160px; flex-shrink: 0;
  display: flex; flex-direction: column; overflow: hidden;
  border-right: 1px solid var(--border);
  background: var(--bg-surface);
}
.panel-header {
  padding: 8px 12px; border-bottom: 1px solid var(--border);
  font-size: 10.5px; font-weight: 600; text-transform: uppercase;
  letter-spacing: 0.6px; color: var(--text-muted); flex-shrink: 0;
}

/* Main: editor + results stacked vertically */
.query-main {
  flex: 1; min-width: 0;
  display: flex; flex-direction: column;
  overflow: hidden; position: relative;
}

/* Editor slot */
.query-editor-slot {
  position: relative;
  display: flex; flex-direction: column; overflow: hidden;
  background: var(--bg-body);
}
.editor-hint {
  position: absolute; bottom: 8px; right: 12px;
  font-size: 10.5px; color: var(--text-muted);
  pointer-events: none;
  display: flex; align-items: center; gap: 4px; opacity: 0.6;
}
.editor-hint kbd {
  padding: 1px 5px; border-radius: 3px;
  border: 1px solid var(--border-2);
  background: var(--bg-elevated);
  font-size: 10px; font-family: inherit;
}

/* Drag handle */
.query-resize-handle {
  height: 5px; flex-shrink: 0;
  background: var(--border); cursor: row-resize;
  position: relative; z-index: 1; transition: background 0.15s;
}
.query-resize-handle:hover, .query-resize-handle:active { background: var(--brand); }
.query-resize-handle::after { content: ''; position: absolute; inset: -4px 0; }

/* Results slot */
.query-results-slot {
  display: flex; flex-direction: column; overflow: hidden;
  background: var(--bg-body); border-top: 1px solid var(--border);
}
.query-results-tabs {
  display: flex; align-items: center; height: 34px; min-height: 34px;
  background: var(--bg-surface); border-bottom: 1px solid var(--border);
  padding: 0 8px; flex-shrink: 0; gap: 2px;
}
.result-tab__badge {
  font-size: 10px; background: var(--brand-dim); color: var(--brand);
  padding: 1px 5px; border-radius: 9px; margin-left: 4px; font-weight: 600;
}
.result-meta {
  display: flex; align-items: center; gap: 8px;
  font-size: 11px; color: var(--text-muted); padding: 0 6px;
}
.query-results-body {
  flex: 1; min-height: 0; overflow: hidden;
  display: flex; flex-direction: column;
}

/* History */
.history-toolbar {
  display: flex; align-items: center; justify-content: space-between;
  padding: 6px 14px; flex-shrink: 0;
  border-bottom: 1px solid var(--border);
}
.history-item {
  padding: 8px 14px; border-bottom: 1px solid var(--border);
  cursor: pointer; transition: background var(--dur);
}
.history-item:hover { background: var(--bg-hover); }
.history-item__sql {
  font-family: var(--mono); font-size: 11.5px;
  color: var(--text-secondary);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.history-item__meta {
  display: flex; gap: 10px; margin-top: 3px;
  font-size: 10.5px; color: var(--text-muted);
}
.history-item__ok { color: var(--success); }
.history-item__err { color: var(--danger); }

/* Schema slide transition */
.schema-slide-enter-active, .schema-slide-leave-active { transition: width 0.2s var(--ease), opacity 0.18s; }
.schema-slide-enter-from, .schema-slide-leave-to { width: 0 !important; opacity: 0; }

/* Pinned results drawer */
.pinned-drawer {
  position: fixed; bottom: 30px; right: 0; width: 480px; max-height: 480px;
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-right: none; border-radius: 10px 0 0 10px;
  box-shadow: -8px 8px 32px rgba(0,0,0,0.35);
  display: flex; flex-direction: column; z-index: 900;
}
.pinned-drawer-header {
  display: flex; align-items: center; gap: 8px;
  padding: 10px 14px; border-bottom: 1px solid var(--border);
  font-size: 13px; font-weight: 700; color: var(--text-primary);
  background: var(--bg-surface); border-radius: 10px 0 0 0; flex-shrink: 0;
}
.pinned-drawer-body { flex: 1; min-height: 0; overflow-y: auto; padding: 10px; display: flex; flex-direction: column; gap: 10px; }
.pinned-result { border: 1px solid var(--border); border-radius: 7px; overflow: hidden; }
.pinned-result-head {
  display: flex; align-items: center; gap: 8px;
  padding: 6px 10px; background: var(--bg-surface);
  border-bottom: 1px solid var(--border);
}
.pinned-result-label { font-size: 11.5px; color: var(--text-muted); font-family: var(--mono, monospace); flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pinned-result-rows { font-size: 11px; color: var(--text-muted); flex-shrink: 0; }

/* Stream toggle */
.stream-toggle {
  display: flex; align-items: center; gap: 5px;
  font-size: 11.5px; color: var(--text-muted); cursor: pointer;
  padding: 3px 8px; border-radius: 5px; border: 1px solid var(--border);
  user-select: none;
}
.stream-toggle input { accent-color: var(--brand); cursor: pointer; }
.stream-toggle:has(input:checked) { color: var(--brand); border-color: var(--brand); background: rgba(99,102,241,0.1); }
.stream-done-bar {
  flex-shrink: 0; padding: 5px 12px;
  font-size: 11px; color: var(--text-muted);
  border-top: 1px solid var(--border); background: var(--bg-elevated);
}

/* Transaction indicator */
.tx-indicator {
  display: flex; align-items: center; gap: 5px;
  padding: 3px 8px; border-radius: 5px;
  background: rgba(251,191,36,0.15); border: 1px solid rgba(251,191,36,0.3);
  font-size: 11px; font-weight: 700; color: #fbbf24;
}
.tx-dot {
  width: 6px; height: 6px; border-radius: 50%;
  background: #fbbf24; animation: pulse 1.2s infinite;
}
@keyframes pulse { 0%,100% { opacity:1 } 50% { opacity:0.3 } }

/* Vertical divider */
.vdivider { width: 1px; height: 18px; background: var(--border); margin: 0 2px; }

/* Explain panel */
.explain-panel { flex: 1; min-height: 0; overflow: auto; padding: 0; display: flex; flex-direction: column; }
.explain-header {
  display: flex; gap: 8px; align-items: center;
  padding: 8px 14px; border-bottom: 1px solid var(--border);
  background: var(--bg-surface); flex-shrink: 0;
}
.explain-driver {
  font-size: 10px; font-weight: 700; text-transform: uppercase;
  padding: 1px 6px; border-radius: 4px;
  background: var(--brand-dim); color: var(--brand);
}
.explain-format { font-size: 11px; color: var(--text-muted); }
.explain-pre {
  flex: 1; margin: 0; padding: 16px;
  font-family: "JetBrains Mono", monospace; font-size: 11.5px; line-height: 1.6;
  color: #4ade80; white-space: pre-wrap; word-break: break-all; overflow: auto;
}
.explain-rows { flex: 1; overflow: auto; padding: 8px 0; }
.explain-row {
  display: flex; gap: 16px; padding: 4px 14px;
  border-bottom: 1px solid var(--border); align-items: flex-start;
}
.explain-row:hover { background: var(--bg-hover); }
.explain-cell { font-family: var(--mono, monospace); font-size: 12px; color: var(--text-primary); }

/* AI panel */
.ai-panel {
  width: 340px; min-width: 280px; flex-shrink: 0;
  border-left: 1px solid var(--border);
  display: flex; flex-direction: column; overflow: hidden;
}
.query-body--with-ai { gap: 0; }
.ai-slide-enter-active, .ai-slide-leave-active { transition: width 0.22s var(--ease), opacity 0.2s; overflow: hidden; }
.ai-slide-enter-from, .ai-slide-leave-to { width: 0 !important; opacity: 0; }

/* Script results */
.script-result { border: 1px solid var(--border); border-radius: 6px; overflow: hidden; margin-bottom: 8px; }
.script-result-header {
  display: flex; align-items: center; gap: 10px;
  padding: 6px 12px; background: var(--bg-surface);
  border-bottom: 1px solid var(--border); flex-wrap: wrap;
}
.script-result-header.is-error { background: rgba(249,127,79,0.08); }
.script-result-num { font-size: 11px; font-weight: 700; color: var(--text-muted); }
.script-result-sql { font-size: 11px; font-family: var(--mono, monospace); color: var(--text-secondary); flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.script-result-meta { font-size: 11px; color: var(--text-muted); }
.script-result-ok { font-size: 11px; color: var(--success); }
.script-result-err { font-size: 11px; color: var(--danger); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 300px; }

/* Diff selectors */
.diff-selectors {
  display: flex; align-items: center; gap: 10px;
  padding: 8px 14px; background: var(--bg-surface);
  border-bottom: 1px solid var(--border); flex-shrink: 0;
}
.diff-selectors label { font-size: 12px; color: var(--text-muted); font-weight: 600; }
.diff-select {
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 5px; color: var(--text-primary); font-size: 12px;
  padding: 3px 8px; cursor: pointer;
}

/* DB selector */
.db-select {
  padding: 3px 8px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 5px;
  color: var(--text-primary);
  font-size: 12px;
  font-family: inherit;
  cursor: pointer;
  outline: none;
  max-width: 160px;
  transition: border-color 0.15s;
}
.db-select:focus { border-color: var(--brand); }

/* Shortcuts hint button */
.shortcuts-btn {
  width: 26px; height: 26px; padding: 0;
  display: flex; align-items: center; justify-content: center;
  font-size: 13px; font-weight: 600;
  border-radius: 50% !important;
}

/* Save dialog */
.ci-overlay {
  position: fixed; inset: 0;
  background: rgba(0, 0, 0, 0.55);
  display: flex; align-items: center; justify-content: center;
  z-index: 1000;
}
.save-dialog {
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 8px;
  width: min(420px, 90vw);
  padding: 20px 24px;
  box-shadow: 0 20px 60px rgba(0,0,0,0.5);
}
.sdlg-label {
  display: block;
  font-size: 11px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.4px;
  margin-bottom: 5px;
}
.sdlg-input {
  width: 100%;
  padding: 7px 10px;
  background: var(--bg-body);
  border: 1px solid var(--border);
  border-radius: 5px;
  color: var(--text-primary);
  font-size: 13px;
  font-family: inherit;
  box-sizing: border-box;
  outline: none;
  transition: border-color 0.15s;
}
.sdlg-input:focus { border-color: var(--brand); }
</style>
