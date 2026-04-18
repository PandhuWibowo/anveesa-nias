<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import type { CompletionSource } from '@codemirror/autocomplete'
import axios from 'axios'
import QueryEditor from '@/components/database/QueryEditor.vue'
import ParamPanel from '@/components/ui/ParamPanel.vue'
import AIAssistant from '@/components/ui/AIAssistant.vue'
import SnippetLibrary from '@/components/ui/SnippetLibrary.vue'
import ShortcutsModal from '@/components/ui/ShortcutsModal.vue'
import ExportCodeModal from '@/components/ui/ExportCodeModal.vue'
import { useQuery, type QueryResult, type HistoryItem } from '@/composables/useQuery'
import { useSavedQueries } from '@/composables/useSavedQueries'
import { useDatabases } from '@/composables/useDatabases'
import { useSchemaCompletion } from '@/composables/useSchemaCompletion'
import { useConnections } from '@/composables/useConnections'
import { formatSQL } from '@/utils/sqlFormat'
import { downloadCSV, downloadJSON } from '@/utils/export'

// ── Result payload type (emitted up to DataView) ──────────────────
export interface ScriptResult {
  index: number; sql: string; columns: string[]; rows: unknown[][]
  row_count: number; affected_rows: number; duration_ms: number; error?: string
}
export type SQLPanelPayload =
  | { kind: 'query'; columns: string[]; rows: unknown[][]; duration_ms: number; row_count: number; affected_rows: number; sql: string }
  | { kind: 'explain'; data: any; sql: string }
  | { kind: 'stream'; columns: string[]; rows: unknown[][]; count: number; duration_ms: number }
  | { kind: 'script'; results: ScriptResult[] }
  | { kind: 'history'; items: HistoryItem[] }
  | { kind: 'saved'; queries: any[] }
  | { kind: 'error'; error: string; sql: string }

const props = defineProps<{
  connId: number | null
  defaultDb?: string
  tableName?: string
  darkMode?: boolean
}>()

const emit = defineEmits<{
  result: [payload: SQLPanelPayload]
  close: []
}>()

const { connections } = useConnections()
const { fetchHistory, clearHistory } = useQuery()
const { getCompletionSource } = useSchemaCompletion()
const { queries: savedQueries, fetchAll: fetchSaved, save: saveQuery, remove: removeQuery } = useSavedQueries()
const { databases, fetchDatabases } = useDatabases()

const activeConn = computed(() =>
  props.connId ? connections.value.find(c => c.id === props.connId) ?? null : null
)
const selectedDatabase = ref(props.defaultDb ?? '')

// ── Tabs ──────────────────────────────────────────────────────────
interface QueryTab {
  id: string; name: string; sql: string
  running: boolean; error: string; notice: string; noticeTone: 'error' | 'success'
}

let tabCounter = 1
function makeTab(sql = 'SELECT 1;'): QueryTab {
  return { id: `sp-tab-${tabCounter++}`, name: `Query ${tabCounter - 1}`, sql, running: false, error: '', notice: '', noticeTone: 'error' }
}

const tabs = ref<QueryTab[]>([makeTab()])
const activeTabId = ref(tabs.value[0].id)
const activeTab = computed(() => tabs.value.find(t => t.id === activeTabId.value) ?? tabs.value[0])

function addTab() {
  const t = makeTab()
  tabs.value.push(t)
  activeTabId.value = t.id
}

function closeTab(id: string) {
  if (tabs.value.length === 1) return
  const idx = tabs.value.findIndex(t => t.id === id)
  tabs.value.splice(idx, 1)
  if (activeTabId.value === id) activeTabId.value = tabs.value[Math.max(0, idx - 1)].id
}

function tabLabel(tab: QueryTab) {
  const s = tab.sql.trim().replace(/\s+/g, ' ').slice(0, 22)
  return s.length < tab.sql.trim().length ? s + '…' : s || tab.name
}

// Auto-fill when table changes
watch(() => props.tableName, (name) => {
  if (name && activeTab.value) {
    const t = /[^a-z0-9_]/i.test(name) ? `"${name}"` : name
    activeTab.value.sql = `SELECT *\nFROM ${t}\nLIMIT 100;`
  }
})

// ── Param substitution ────────────────────────────────────────────
const queryParams = ref<Record<string, string>>({})

function buildParamSQL(sql: string): string {
  return sql.replace(/:([a-zA-Z_][a-zA-Z0-9_]*)/g, (_, name) => {
    const val = queryParams.value[name] ?? ''
    if (val === '') return `:${name}`
    return isNaN(Number(val)) || val.trim() === '' ? `'${val.replace(/'/g, "''")}'` : val
  })
}

// ── Schema completion ─────────────────────────────────────────────
const schemaCompletion = ref<CompletionSource | null>(null)

watch(() => props.connId, async (id) => {
  schemaCompletion.value = null
  selectedDatabase.value = props.defaultDb ?? ''
  if (!id) return
  await fetchDatabases(id)
  if (!selectedDatabase.value) selectedDatabase.value = databases.value[0] ?? ''
  const db = selectedDatabase.value || 'public'
  schemaCompletion.value = await getCompletionSource(id, db)
}, { immediate: true })

// ── Transaction controls ──────────────────────────────────────────
const txActive = ref(false)

async function txBegin() {
  if (!props.connId) return
  await axios.post(`/api/connections/${props.connId}/transaction/begin`)
  txActive.value = true
}
async function txCommit() {
  if (!props.connId) return
  await axios.post(`/api/connections/${props.connId}/transaction/commit`)
  txActive.value = false
}
async function txRollback() {
  if (!props.connId) return
  await axios.post(`/api/connections/${props.connId}/transaction/rollback`)
  txActive.value = false
}
watch(() => props.connId, () => { txActive.value = false })

// ── Run / Explain / Format / Cancel ──────────────────────────────
const abortControllers = new Map<string, AbortController>()
const approvalDialogOpen = ref(false)
const approvalSubmitting = ref(false)
const approvalTitle = ref('')
const approvalDescription = ref('')
const approvalWorkflowId = ref<number | null>(null)
const approvalWorkflows = ref<Array<{ id: number; name: string; description: string }>>([])

function formatCurrentSQL() {
  if (!activeTab.value) return
  const driver = activeConn.value?.driver ?? 'sql'
  activeTab.value.sql = formatSQL(activeTab.value.sql, driver)
}

function cancelQuery() {
  if (!activeTab.value) return
  abortControllers.get(activeTab.value.id)?.abort()
}

function defaultApprovalTitle(sql: string) {
  const firstLine = sql.trim().split('\n')[0]?.trim() || 'Write SQL change'
  return firstLine.slice(0, 80)
}

async function createApprovalRequest(sql: string) {
  if (!props.connId || !approvalTitle.value.trim()) return null
  const { data } = await axios.post('/api/approval-requests', {
    title: approvalTitle.value.trim(),
    description: approvalDescription.value.trim(),
    conn_id: props.connId,
    database: selectedDatabase.value || '',
    statement: buildParamSQL(sql),
    workflow_id: approvalWorkflowId.value || 0,
  })
  return data
}

async function handleApprovalRequired(sql: string, responseData: any) {
  approvalWorkflows.value = Array.isArray(responseData?.workflows) ? responseData.workflows : []
  approvalWorkflowId.value = approvalWorkflows.value[0]?.id ?? null
  approvalTitle.value = defaultApprovalTitle(sql)
  approvalDescription.value = ''

  if (approvalWorkflows.value.length <= 1) {
    const data = await createApprovalRequest(sql)
    if (data?.id && activeTab.value) {
      activeTab.value.error = ''
      activeTab.value.notice = `Approval request #${data.id} submitted for review.`
      activeTab.value.noticeTone = 'success'
      approvalDialogOpen.value = false
      emit('result', { kind: 'error', error: activeTab.value.notice, sql })
      return true
    }
  }

  approvalDialogOpen.value = true
  if (activeTab.value) {
    activeTab.value.notice = responseData?.error ?? 'Approval required before executing write SQL.'
    activeTab.value.noticeTone = 'error'
  }
  return false
}

async function runQuery() {
  if (!props.connId || !activeTab.value) return
  const tab = activeTab.value
  const ctrl = new AbortController()
  abortControllers.set(tab.id, ctrl)
  tab.running = true
  tab.error = ''
  tab.notice = ''
  try {
    const { data } = await axios.post<QueryResult>(
      `/api/connections/${props.connId}/query`,
      { sql: buildParamSQL(tab.sql), database: selectedDatabase.value || undefined },
      { signal: ctrl.signal }
    )
    emit('result', {
      kind: 'query',
      columns: data.columns,
      rows: data.rows as unknown[][],
      duration_ms: data.duration_ms,
      row_count: data.row_count,
      affected_rows: data.affected_rows ?? 0,
      sql: tab.sql,
    })
    // Record in history
    axios.post(`/api/connections/${props.connId}/history`, {
      sql: tab.sql, duration_ms: data.duration_ms, row_count: data.row_count,
    }).catch(() => {})
  } catch (e: unknown) {
    const err = e as { code?: string; response?: { data?: { error?: string } } }
    if (err.code !== 'ERR_CANCELED') {
      const responseData = (err as any)?.response?.data
      if (responseData?.approval_required) {
        try {
          await handleApprovalRequired(tab.sql, responseData)
          return
        } catch (submitErr: any) {
          tab.error = submitErr?.response?.data?.error ?? 'Failed to submit approval request'
          tab.notice = ''
          tab.noticeTone = 'error'
          emit('result', { kind: 'error', error: tab.error, sql: tab.sql })
          return
        }
      }
      tab.error = err.response?.data?.error ?? 'Query failed'
      tab.notice = ''
      tab.noticeTone = 'error'
      emit('result', { kind: 'error', error: tab.error, sql: tab.sql })
    }
  } finally {
    tab.running = false
    abortControllers.delete(tab.id)
  }
}

async function submitApprovalRequest() {
  if (!props.connId || !activeTab.value || !approvalTitle.value.trim()) return
  approvalSubmitting.value = true
  try {
    const data = await createApprovalRequest(activeTab.value.sql)
    approvalDialogOpen.value = false
    activeTab.value.error = ''
    activeTab.value.notice = `Approval request #${data.id} submitted for review.`
    activeTab.value.noticeTone = 'success'
  } catch (e: any) {
    activeTab.value.error = e?.response?.data?.error ?? 'Failed to submit approval request'
    activeTab.value.notice = ''
    activeTab.value.noticeTone = 'error'
  } finally {
    approvalSubmitting.value = false
  }
}

// ── Explain ───────────────────────────────────────────────────────
async function runExplainPlan() {
  if (!props.connId || !activeTab.value?.sql) return
  activeTab.value.running = true
  try {
    const { data } = await axios.post(`/api/connections/${props.connId}/explain`, {
      sql: activeTab.value.sql
    })
    emit('result', { kind: 'explain', data, sql: activeTab.value.sql })
  } catch (e: any) {
    emit('result', { kind: 'error', error: e?.response?.data?.error ?? 'Explain failed', sql: activeTab.value.sql })
  } finally {
    activeTab.value.running = false
  }
}

// ── Streaming ─────────────────────────────────────────────────────
const streamMode = ref(false)
let streamAbort: AbortController | null = null

async function runStreamQuery() {
  if (!props.connId || !activeTab.value?.sql) return
  const tab = activeTab.value
  tab.running = true
  const cols: string[] = []
  const rows: unknown[][] = []
  let count = 0
  let durationMs = 0
  streamAbort = new AbortController()
  try {
    const resp = await fetch(`/api/connections/${props.connId}/query/stream`, {
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
          if (obj.columns) { cols.push(...obj.columns) }
          else if (obj.row) { rows.push(obj.row); count++ }
          else if (obj.done) { durationMs = obj.duration_ms }
        }
      }
    }
    emit('result', { kind: 'stream', columns: cols, rows, count, duration_ms: durationMs })
  } catch (e: any) {
    if (e?.name !== 'AbortError') tab.error = 'Stream aborted'
  } finally {
    tab.running = false
    streamAbort = null
  }
}

function stopStream() { streamAbort?.abort() }

// ── Multi-statement script ────────────────────────────────────────
const scriptRunning = ref(false)

async function runScript() {
  if (!props.connId || !activeTab.value?.sql) return
  scriptRunning.value = true
  try {
    const { data } = await axios.post<ScriptResult[]>(
      `/api/connections/${props.connId}/script`,
      { sql: buildParamSQL(activeTab.value.sql), database: selectedDatabase.value || undefined }
    )
    emit('result', { kind: 'script', results: data })
  } catch (e: any) {
    const responseData = e?.response?.data
    if (activeTab.value && responseData?.approval_required) {
      try {
        await handleApprovalRequired(activeTab.value.sql, responseData)
      } catch (submitErr: any) {
        activeTab.value.error = submitErr?.response?.data?.error ?? 'Failed to submit approval request'
      }
    } else if (activeTab.value) {
      activeTab.value.error = e?.response?.data?.error ?? 'Script failed'
    }
  } finally {
    scriptRunning.value = false
  }
}

// ── History ───────────────────────────────────────────────────────
const historyItems = ref<HistoryItem[]>([])

async function showHistory() {
  if (!props.connId) return
  historyItems.value = await fetchHistory(props.connId)
  emit('result', { kind: 'history', items: historyItems.value })
}

async function doClearHistory() {
  if (!props.connId) return
  await clearHistory(props.connId)
  historyItems.value = []
  emit('result', { kind: 'history', items: [] })
}

// ── Saved queries ─────────────────────────────────────────────────
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
  await saveQuery(saveName.value.trim(), activeTab.value.sql, saveDesc.value, props.connId)
  saveDialogOpen.value = false
  await fetchSaved()
}

async function showSaved() {
  await fetchSaved()
  emit('result', { kind: 'saved', queries: savedQueries.value })
}

watch(() => props.connId, id => { if (id) fetchSaved() }, { immediate: true })

// ── Export ────────────────────────────────────────────────────────
function exportCurrentResult(format: 'csv' | 'json', columns: string[], rows: unknown[][]) {
  if (format === 'csv') downloadCSV(columns, rows, 'query-results')
  else downloadJSON(columns, rows, 'query-results')
}

// ── UI toggles ────────────────────────────────────────────────────
const showAI = ref(false)
const showSnippets = ref(false)
const showShortcuts = ref(false)
const showExportCode = ref(false)

function insertSnippet(sql: string) {
  if (!activeTab.value) return
  activeTab.value.sql = activeTab.value.sql ? activeTab.value.sql + '\n\n' + sql : sql
}

// ── Keyboard shortcuts ────────────────────────────────────────────
function handleGlobalKey(e: KeyboardEvent) {
  if (e.key === '?' && !(e.target instanceof HTMLInputElement)) {
    showShortcuts.value = !showShortcuts.value; return
  }
  if (e.key === 'Escape') { showShortcuts.value = false; return }
  if ((e.ctrlKey || e.metaKey) && e.shiftKey && e.key === 'F') {
    e.preventDefault(); formatCurrentSQL(); return
  }
}

onMounted(() => window.addEventListener('keydown', handleGlobalKey))
onBeforeUnmount(() => {
  window.removeEventListener('keydown', handleGlobalKey)
  streamAbort?.abort()
})

// ── Exposed API ───────────────────────────────────────────────────
function loadSQL(sql: string) {
  if (!activeTab.value) return
  activeTab.value.sql = sql
}

defineExpose({ loadSQL, exportCurrentResult })
</script>

<template>
  <!-- Toolbar -->
  <div class="sp-header">
    <div class="sp-toolbar">

      <!-- TX controls -->
      <template v-if="connId">
        <div v-if="txActive" class="tx-indicator" title="Transaction active">
          <span class="tx-dot" /> TX
        </div>
        <button v-if="!txActive" class="base-btn base-btn--ghost base-btn--sm" @click="txBegin">BEGIN</button>
        <button v-if="txActive" class="base-btn base-btn--ghost base-btn--sm" style="color:#4ade80" @click="txCommit">COMMIT</button>
        <button v-if="txActive" class="base-btn base-btn--ghost base-btn--sm" style="color:#f87171" @click="txRollback">ROLLBACK</button>
        <div class="sp-divider" />
      </template>

      <!-- Format -->
      <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!activeTab?.sql.trim()" @click="formatCurrentSQL" title="Format SQL (Ctrl+Shift+F)">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="21" y1="10" x2="7" y2="10"/><line x1="21" y1="6" x2="3" y2="6"/><line x1="21" y1="14" x2="3" y2="14"/><line x1="21" y1="18" x2="7" y2="18"/></svg>
        Format
      </button>

      <!-- Explain -->
      <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!connId || !activeTab?.sql.trim() || activeTab?.running" @click="runExplainPlan" title="Explain query plan">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
        Explain
      </button>

      <!-- Script -->
      <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!connId || !activeTab?.sql.trim() || scriptRunning" @click="runScript" title="Run all statements">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg>
        Script
      </button>

      <!-- Snippets -->
      <button class="base-btn base-btn--ghost base-btn--sm" @click="showSnippets = true" title="Snippet library">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
        Snippets
      </button>

      <!-- History -->
      <button class="base-btn base-btn--ghost base-btn--sm" @click="showHistory" title="Query history">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="12 8 12 12 14 14"/><path d="M3.05 11a9 9 0 1 0 .5-4"/><polyline points="3 3 3 7 7 7"/></svg>
        History
      </button>

      <!-- Saved -->
      <button class="base-btn base-btn--ghost base-btn--sm" @click="showSaved" title="Saved queries">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/><polyline points="7 3 7 8 15 8"/></svg>
        Saved
      </button>

      <!-- Save current -->
      <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!activeTab?.sql.trim()" @click="openSaveDialog" title="Save current query">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/></svg>
        Save
      </button>

      <!-- Export as code -->
      <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!activeTab?.sql.trim()" @click="showExportCode = true" title="Export as code">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
        Code
      </button>

      <!-- AI -->
      <button class="base-btn base-btn--ghost base-btn--sm" :class="{ 'is-active': showAI }" @click="showAI = !showAI" title="AI query assistant">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 2a10 10 0 1 0 10 10"/><path d="M12 8v4l2 2"/><path d="M18 2l4 4-4 4"/><path d="M22 6H16"/></svg>
        AI
      </button>

      <div class="sp-divider" />

      <!-- Stream toggle -->
      <label class="sp-stream" title="Stream rows as they arrive">
        <input type="checkbox" v-model="streamMode" />
        <span>Stream</span>
      </label>

      <!-- Stop / Run -->
      <button v-if="activeTab?.running" class="base-btn base-btn--danger base-btn--sm" @click="cancelQuery">
        <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><rect x="3" y="3" width="18" height="18" rx="2"/></svg>
        Stop
      </button>
      <button v-else class="base-btn base-btn--primary base-btn--sm" :disabled="!connId || !activeTab?.sql.trim()" @click="streamMode ? runStreamQuery() : runQuery()">
        <svg width="11" height="11" viewBox="0 0 24 24" fill="currentColor"><polygon points="5 3 19 12 5 21 5 3"/></svg>
        {{ streamMode ? 'Stream' : 'Run' }}
      </button>

      <!-- Shortcuts -->
      <button class="base-btn base-btn--ghost base-btn--sm" @click="showShortcuts = true" title="Keyboard shortcuts (?)">?</button>

      <!-- Close panel -->
      <button class="base-btn base-btn--ghost base-btn--sm" @click="emit('close')" title="Close SQL editor">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
      </button>
    </div>
  </div>

  <!-- Error notice (inline in panel, not in main area) -->
  <div v-if="activeTab?.error || activeTab?.notice" class="sp-error" :class="{ 'sp-error--success': !activeTab?.error && activeTab?.noticeTone === 'success' }">
    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="flex-shrink:0;color:currentColor"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
    {{ activeTab.error || activeTab.notice }}
  </div>

  <!-- CodeMirror editor (one per tab, v-show to preserve state) -->
  <div class="sp-editor">
    <template v-for="tab in tabs" :key="tab.id">
      <div v-show="tab.id === activeTabId" style="flex:1;min-height:0;display:flex;flex-direction:column;">
        <QueryEditor
          v-model="tab.sql"
          :dark-mode="darkMode"
          :schema-completion="schemaCompletion"
          placeholder="Write SQL here… (Ctrl+Enter to run)"
          @run="runQuery"
        />
      </div>
    </template>
    <div class="sp-hint">
      <kbd>Ctrl+Enter</kbd> run &nbsp;·&nbsp; <kbd>Tab</kbd> indent &nbsp;·&nbsp; <kbd>Ctrl+Shift+F</kbd> format
    </div>
    <ParamPanel :sql="activeTab?.sql ?? ''" @update:params="queryParams = $event" />
  </div>

  <!-- Modals -->
  <AIAssistant v-if="showAI" :active-sql="activeTab?.sql" @insert-sql="(sql: string) => { if(activeTab) activeTab.sql = sql; showAI = false }" />
  <SnippetLibrary :show="showSnippets" @insert="insertSnippet" @close="showSnippets = false" />
  <ShortcutsModal :show="showShortcuts" @close="showShortcuts = false" />
  <ExportCodeModal :show="showExportCode" :sql="activeTab?.sql ?? ''" :connection="activeConn" @close="showExportCode = false" />

  <!-- Save dialog -->
  <Teleport to="body">
    <div v-if="saveDialogOpen" class="sp-save-overlay" @click.self="saveDialogOpen = false">
      <div class="sp-save-modal">
        <div class="sp-save-header">
          <span style="font-weight:600;font-size:14px;color:var(--text-primary)">Save Query</span>
          <button class="base-btn base-btn--ghost base-btn--sm" @click="saveDialogOpen = false">×</button>
        </div>
        <div class="sp-save-body">
          <label class="sp-save-label">Name</label>
          <input v-model="saveName" class="base-input" placeholder="Query name…" @keydown.enter="confirmSave" />
          <label class="sp-save-label" style="margin-top:8px">Description (optional)</label>
          <input v-model="saveDesc" class="base-input" placeholder="Brief description…" />
        </div>
        <div class="sp-save-footer">
          <button class="base-btn base-btn--ghost base-btn--sm" @click="saveDialogOpen = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="!saveName.trim()" @click="confirmSave">Save</button>
        </div>
      </div>
    </div>
  </Teleport>

  <Teleport to="body">
    <div v-if="approvalDialogOpen" class="sp-save-overlay" @click.self="approvalDialogOpen = false">
      <div class="sp-save-modal">
        <div class="sp-save-header">
          <span style="font-weight:600;font-size:14px;color:var(--text-primary)">Submit Approval Request</span>
          <button class="base-btn base-btn--ghost base-btn--sm" @click="approvalDialogOpen = false">×</button>
        </div>
        <div class="sp-save-body">
          <label class="sp-save-label">Title</label>
          <input v-model="approvalTitle" class="base-input" placeholder="Short request title…" />
          <label class="sp-save-label" style="margin-top:8px">Description</label>
          <input v-model="approvalDescription" class="base-input" placeholder="Why is this change needed?" />
          <template v-if="approvalWorkflows.length > 1">
            <label class="sp-save-label" style="margin-top:8px">Workflow</label>
            <select v-model="approvalWorkflowId" class="base-input">
              <option v-for="wf in approvalWorkflows" :key="wf.id" :value="wf.id">{{ wf.name }}</option>
            </select>
          </template>
          <div style="margin-top:10px;padding:10px;border:1px solid var(--border);border-radius:10px;background:var(--bg);font-size:11px;color:var(--text-muted)">
            This write query will be submitted for approval instead of executing immediately.
          </div>
        </div>
        <div class="sp-save-footer">
          <button class="base-btn base-btn--ghost base-btn--sm" @click="approvalDialogOpen = false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="approvalSubmitting || !approvalTitle.trim()" @click="submitApprovalRequest">
            {{ approvalSubmitting ? 'Submitting…' : 'Submit' }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
/* ── Header: tabs + toolbar ──────────────────────────────────────── */
.sp-header {
  display: flex;
  align-items: center;
  gap: 0;
  background: var(--bg-elevated);
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
  min-height: 34px;
  overflow: hidden;
}

.sp-tabs {
  display: flex;
  align-items: stretch;
  overflow-x: auto;
  flex-shrink: 0;
  max-width: 50%;
  scrollbar-width: none;
}
.sp-tabs::-webkit-scrollbar { display: none; }

.sp-tab {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 0 10px;
  height: 34px;
  border: none;
  border-right: 1px solid var(--border);
  background: transparent;
  color: var(--text-muted);
  font-size: 11.5px;
  cursor: pointer;
  white-space: nowrap;
  transition: background 0.1s, color 0.1s;
}
.sp-tab:hover { background: var(--bg-surface); color: var(--text-primary); }
.sp-tab--active { background: var(--bg-surface); color: var(--text-primary); border-bottom: 2px solid var(--brand); }
.sp-tab--err .sp-tab__dot { background: #f87171; }
.sp-tab--run .sp-tab__dot { background: var(--brand); animation: pulse 1s infinite; }

.sp-tab__dot {
  width: 6px; height: 6px;
  border-radius: 50%;
  background: var(--text-muted);
  flex-shrink: 0;
  opacity: 0.5;
}
.sp-tab--active .sp-tab__dot { opacity: 1; background: var(--brand); }

.sp-tab__label { max-width: 120px; overflow: hidden; text-overflow: ellipsis; }

.sp-tab__close {
  background: none; border: none; cursor: pointer;
  color: var(--text-muted); font-size: 14px; line-height: 1;
  padding: 0 2px; border-radius: 2px;
  transition: color 0.1s, background 0.1s;
}
.sp-tab__close:hover { color: var(--text-primary); background: var(--bg-elevated); }

.sp-tab-add {
  display: flex; align-items: center; justify-content: center;
  width: 32px; height: 34px;
  border: none; background: transparent; cursor: pointer;
  color: var(--text-muted); border-right: 1px solid var(--border);
  transition: color 0.1s, background 0.1s;
}
.sp-tab-add:hover { color: var(--brand); background: var(--bg-surface); }

.sp-toolbar {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 0 8px;
  flex: 1;
  overflow-x: auto;
  scrollbar-width: none;
}
.sp-toolbar::-webkit-scrollbar { display: none; }

.sp-divider {
  width: 1px; height: 16px;
  background: var(--border);
  flex-shrink: 0;
  margin: 0 4px;
}

.sp-stream {
  display: flex; align-items: center; gap: 4px;
  font-size: 11.5px; color: var(--text-muted); cursor: pointer; user-select: none;
  flex-shrink: 0;
}
.sp-stream input { margin: 0; }

/* ── Error bar ───────────────────────────────────────────────────── */
.sp-error {
  display: flex; align-items: flex-start; gap: 8px;
  padding: 6px 12px; font-size: 12px; color: #f87171;
  background: rgba(248, 113, 113, 0.08);
  border-bottom: 1px solid rgba(248, 113, 113, 0.2);
  flex-shrink: 0; line-height: 1.5;
}
.sp-error--success {
  color: #4ade80;
  background: rgba(74, 222, 128, 0.08);
  border-bottom-color: rgba(74, 222, 128, 0.2);
}

/* ── Editor area ─────────────────────────────────────────────────── */
.sp-editor {
  flex: 1; min-height: 0; display: flex; flex-direction: column; overflow: hidden;
}

.sp-hint {
  display: flex; align-items: center; gap: 4px;
  padding: 3px 12px; font-size: 10.5px; color: var(--text-muted);
  background: var(--bg-elevated); border-top: 1px solid var(--border);
  flex-shrink: 0;
}
.sp-hint kbd {
  background: var(--bg-surface); border: 1px solid var(--border);
  border-radius: 3px; padding: 0px 4px; font-size: 10px;
  font-family: var(--mono, monospace);
}

/* ── TX indicator ────────────────────────────────────────────────── */
.tx-indicator {
  display: flex; align-items: center; gap: 5px;
  padding: 3px 8px; border-radius: 4px;
  font-size: 11px; font-weight: 600;
  color: #4ade80; background: rgba(74, 222, 128, 0.1);
  flex-shrink: 0;
}
.tx-dot { width: 6px; height: 6px; border-radius: 50%; background: #4ade80; }

/* ── is-active (AI button) ───────────────────────────────────────── */
.is-active { color: var(--brand) !important; }

/* ── Save dialog ─────────────────────────────────────────────────── */
.sp-save-overlay {
  position: fixed; inset: 0; z-index: 3000;
  background: rgba(0,0,0,0.5);
  display: flex; align-items: center; justify-content: center;
}
.sp-save-modal {
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 10px; width: 360px; overflow: hidden;
  box-shadow: 0 20px 60px rgba(0,0,0,0.4);
}
.sp-save-header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 12px 16px; border-bottom: 1px solid var(--border);
}
.sp-save-body { padding: 16px; display: flex; flex-direction: column; gap: 4px; }
.sp-save-label { font-size: 11px; color: var(--text-muted); font-weight: 600; margin-bottom: 4px; }
.sp-save-footer {
  display: flex; align-items: center; justify-content: flex-end; gap: 8px;
  padding: 12px 16px; border-top: 1px solid var(--border);
}

@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.3; }
}
</style>
