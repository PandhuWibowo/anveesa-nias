<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import axios from 'axios'
import SchemaTree from '@/components/database/SchemaTree.vue'
import DataTable from '@/components/database/DataTable.vue'
import VirtualTable from '@/components/database/VirtualTable.vue'
import SQLPanel from '@/components/database/SQLPanel.vue'
import ExplainTree from '@/components/database/ExplainTree.vue'
import ResultChart from '@/components/ui/ResultChart.vue'
import ColumnProfiler from '@/components/ui/ColumnProfiler.vue'
import { useSchema } from '@/composables/useSchema'
import { useForeignKeys } from '@/composables/useForeignKeys'
import { useConnections } from '@/composables/useConnections'
import { useToast } from '@/composables/useToast'
import type { SQLPanelPayload } from '@/components/database/SQLPanel.vue'

type ImportRow = (string | number | null)[]

const props = defineProps<{ connId: number | null; darkMode: boolean; initialSQL?: string | null }>()

const { connections } = useConnections()
const { fetchTableData, fetchTableColumns, columns: schemaColumns, fetchColumns } = useSchema()
const { fetchFKs, isFKColumn } = useForeignKeys()
const toast = useToast()

const activeConn = computed(() =>
  props.connId ? connections.value.find(c => c.id === props.connId) ?? null : null
)

// ── Data browser state ────────────────────────────────────────────
const selected = ref<{ db: string; table: string } | null>(null)
const columns = ref<string[]>([])
const rows = ref<unknown[][]>([])
const totalRows = ref(0)
const page = ref(1)
const pageSize = ref(100)
const sortBy = ref<string | undefined>()
const sortDir = ref<'asc' | 'desc'>('asc')
const loading = ref(false)
const editMode = ref(false)
const pkColumn = ref('')

watch(() => props.connId, () => {
  selected.value = null
  columns.value = []; rows.value = []; totalRows.value = 0
  schemaSelected.value = null
})

async function loadData() {
  if (!selected.value || !props.connId) return
  loading.value = true
  const data = await fetchTableData(props.connId, selected.value.db, selected.value.table, page.value, pageSize.value, sortBy.value, sortDir.value)
  if (data) { columns.value = data.columns ?? []; rows.value = data.rows ?? []; totalRows.value = data.total_rows ?? 0 }
  loading.value = false
}

async function handleSelectTable(payload: { db: string; table: string }) {
  selected.value = payload
  editMode.value = false; pkColumn.value = ''; page.value = 1
  loadData()
  if (props.connId) {
    const cols = await fetchTableColumns(props.connId, payload.db, payload.table)
    fetchFKs(props.connId, payload.db)
    const pk = cols?.find((c: any) => c.is_primary_key)
    pkColumn.value = pk?.name ?? (cols?.[0]?.name ?? '')
  }
}

function handleSort(col: string, dir: 'asc' | 'desc') { sortBy.value = col; sortDir.value = dir; page.value = 1; loadData() }
function handlePageChange(p: number) { page.value = p; loadData() }

async function handleSaveRow(payload: { pkValue: unknown; updates: Record<string, unknown> }) {
  if (!selected.value || !props.connId) return
  try {
    await axios.put(`/api/connections/${props.connId}/schema/${selected.value.db}/tables/${selected.value.table}/rows`, { pk_column: pkColumn.value, pk_value: payload.pkValue, updates: payload.updates })
    toast.success('Row updated'); loadData()
  } catch (e: any) { toast.error(e.response?.data?.error ?? 'Update failed') }
}

async function handleDeleteRow(payload: { pkValue: unknown }) {
  if (!selected.value || !props.connId) return
  if (!confirm('Delete this row?')) return
  try {
    await axios.delete(`/api/connections/${props.connId}/schema/${selected.value.db}/tables/${selected.value.table}/rows`, { data: { pk_column: pkColumn.value, pk_value: payload.pkValue } })
    toast.success('Row deleted'); loadData()
  } catch (e: any) { toast.error(e.response?.data?.error ?? 'Delete failed') }
}

async function handleAddRow(payload: { values: Record<string, unknown> }) {
  if (!selected.value || !props.connId) return
  try {
    await axios.post(`/api/connections/${props.connId}/schema/${selected.value.db}/tables/${selected.value.table}/rows`, { values: payload.values })
    toast.success('Row inserted'); loadData()
  } catch (e: any) { toast.error(e.response?.data?.error ?? 'Insert failed') }
}

// ── Schema tab state ──────────────────────────────────────────────
const schemaSelected = ref<{ db: string; table: string; type: string } | null>(null)
const schemaLoadingCols = ref(false)

async function handleSchemaSelectTable(payload: { db: string; table: string; type?: string }) {
  schemaSelected.value = { db: payload.db, table: payload.table, type: payload.type ?? 'table' }
  schemaLoadingCols.value = true
  await fetchColumns(props.connId ?? 0, payload.db, payload.table)
  schemaLoadingCols.value = false
}

const schemaColumnRows = computed(() =>
  schemaColumns.value.map((c: any) => [c.name, c.data_type, c.is_nullable ? 'YES' : 'NO', c.is_primary_key ? '✓' : '', c.default_value ?? ''])
)

// ── Import ────────────────────────────────────────────────────────
const importOpen = ref(false)
const importColumns = ref<string[]>([])
const importRows = ref<ImportRow[]>([])
const importError = ref('')
const importLoading = ref(false)
const importDragOver = ref(false)

function openImport() { importColumns.value = []; importRows.value = []; importError.value = ''; importOpen.value = true }
function onImportDrop(e: DragEvent) { importDragOver.value = false; const f = e.dataTransfer?.files[0]; if (f) parseImportFile(f) }
function onImportFilePick(e: Event) { const f = (e.target as HTMLInputElement).files?.[0]; if (f) parseImportFile(f) }
function parseImportFile(file: File) {
  importError.value = ''
  const reader = new FileReader()
  reader.onload = () => {
    try { file.name.endsWith('.json') ? parseJSON(reader.result as string) : parseCSV(reader.result as string) }
    catch (e: any) { importError.value = e.message ?? 'Parse error' }
  }
  reader.readAsText(file)
}
function parseCSV(text: string) {
  const lines = text.trim().split('\n')
  if (lines.length < 2) throw new Error('CSV must have header and at least one row')
  importColumns.value = lines[0].split(',').map(c => c.trim().replace(/^"|"$/g, ''))
  importRows.value = lines.slice(1).map(line => line.split(',').map(cell => { const v = cell.trim().replace(/^"|"$/g, ''); return v === '' ? null : isNaN(Number(v)) ? v : Number(v) }))
}
function parseJSON(text: string) {
  const arr: Record<string, unknown>[] = Array.isArray(JSON.parse(text)) ? JSON.parse(text) : [JSON.parse(text)]
  if (!arr.length) throw new Error('Empty JSON array')
  importColumns.value = Object.keys(arr[0])
  importRows.value = arr.map(row => importColumns.value.map(c => row[c] as any))
}
async function confirmImport() {
  if (!selected.value || !props.connId) return
  importLoading.value = true; importError.value = ''
  try {
    const { data } = await axios.post(`/api/connections/${props.connId}/schema/${selected.value.db}/tables/${selected.value.table}/import`, { columns: importColumns.value, rows: importRows.value, skip_errors: true })
    toast.success(`Imported ${data.inserted} rows`)
    if (data.errors?.length) toast.error(`${data.errors.length} rows skipped`)
    importOpen.value = false; loadData()
  } catch (e: any) { importError.value = e?.response?.data?.error ?? 'Import failed' }
  finally { importLoading.value = false }
}

async function exportCsv() { if (!rows.value.length) return; const { downloadCSV } = await import('@/utils/export'); downloadCSV(columns.value, rows.value, selected.value?.table ?? 'export'); toast.success('CSV exported') }
async function exportJson() { if (!rows.value.length) return; const { downloadJSON } = await import('@/utils/export'); downloadJSON(columns.value, rows.value, selected.value?.table ?? 'export'); toast.success('JSON exported') }

// ── Sub-tab + SQL tabs ────────────────────────────────────────────
type ResultKind = 'query' | 'explain' | 'stream' | 'script' | 'history' | 'saved' | 'error' | 'chart'
interface PinnedResult { id: string; label: string; columns: string[]; rows: unknown[][] }
interface SQLViewTab { id: string; label: string; result: SQLPanelPayload | null; activeResultTab: ResultKind; sqlHeight: number; pinnedResults: PinnedResult[]; diffLeft: string; diffRight: string }

const activeSubTab = ref<string>('data')
const sqlViewTabs = ref<SQLViewTab[]>([])
const sqlPanelRefs = ref<Record<string, InstanceType<typeof SQLPanel>>>({})
let sqlTabCounter = 0

// Sync Schema ↔ Data Browser when switching sub-tabs
watch(activeSubTab, (tab) => {
  if (tab === 'schema' && selected.value) handleSchemaSelectTable({ db: selected.value.db, table: selected.value.table })
  else if (tab === 'data' && schemaSelected.value) handleSelectTable({ db: schemaSelected.value.db, table: schemaSelected.value.table })
})

function openSqlTab(preloadSQL?: string) {
  const id = `sql-${++sqlTabCounter}`
  sqlViewTabs.value.push({ id, label: `SQL ${sqlTabCounter}`, result: null, activeResultTab: 'query', sqlHeight: 260, pinnedResults: [], diffLeft: '', diffRight: '' })
  activeSubTab.value = id
  if (preloadSQL) setTimeout(() => sqlPanelRefs.value[id]?.loadSQL(preloadSQL), 80)
}

function closeSqlTab(id: string) {
  const idx = sqlViewTabs.value.findIndex(t => t.id === id)
  if (idx === -1) return
  sqlViewTabs.value.splice(idx, 1); delete sqlPanelRefs.value[id]
  if (activeSubTab.value === id) activeSubTab.value = sqlViewTabs.value[Math.max(0, idx - 1)]?.id ?? 'data'
}

function onSQLResult(tabId: string, payload: SQLPanelPayload) {
  const tab = sqlViewTabs.value.find(t => t.id === tabId)
  if (tab) { tab.result = payload; tab.activeResultTab = payload.kind as ResultKind }
}
function clearTabResult(tabId: string) { const tab = sqlViewTabs.value.find(t => t.id === tabId); if (tab) tab.result = null }
function pinTabResult(tabId: string) {
  const tab = sqlViewTabs.value.find(t => t.id === tabId)
  if (!tab || !tab.result || tab.result.kind !== 'query') return
  const r = tab.result as any
  tab.pinnedResults.unshift({ id: crypto.randomUUID(), label: r.sql.slice(0, 40).replace(/\n/g, ' ').trim(), columns: [...r.columns], rows: [...r.rows] })
}
function useSQLInTab(tabId: string, sql: string) { sqlPanelRefs.value[tabId]?.loadSQL(sql) }

const resizingTabId = ref<string | null>(null)
const resizeStart = ref({ y: 0, h: 0 })
function onSqlResizeStart(e: MouseEvent, tabId: string) {
  resizingTabId.value = tabId
  const tab = sqlViewTabs.value.find(t => t.id === tabId)
  resizeStart.value = { y: e.clientY, h: tab?.sqlHeight ?? 260 }
  window.addEventListener('mousemove', onSqlResizeMove)
  window.addEventListener('mouseup', onSqlResizeEnd)
}
function onSqlResizeMove(e: MouseEvent) {
  if (!resizingTabId.value) return
  const tab = sqlViewTabs.value.find(t => t.id === resizingTabId.value)
  if (tab) { const delta = resizeStart.value.y - e.clientY; tab.sqlHeight = Math.max(140, Math.min(700, resizeStart.value.h + delta)) }
}
function onSqlResizeEnd() { resizingTabId.value = null; window.removeEventListener('mousemove', onSqlResizeMove); window.removeEventListener('mouseup', onSqlResizeEnd) }
onBeforeUnmount(() => { window.removeEventListener('mousemove', onSqlResizeMove); window.removeEventListener('mouseup', onSqlResizeEnd) })

// Column profiler
const profilerShow = ref(false)

function openSchemaTableInSQL(table: string) { openSqlTab(`SELECT *\nFROM ${table}\nLIMIT 100;`) }

// Open with initial SQL if provided
onMounted(() => { if (props.initialSQL) openSqlTab(props.initialSQL) })

function driverColor(d: string) { return ({ postgres: '#336791', mysql: '#f29111', mariadb: '#c0392b', sqlite: '#7bc8f6', mssql: '#cc2927' } as Record<string,string>)[d] ?? '#555' }
function driverLabel(d: string) { return ({ postgres: 'PG', mysql: 'MY', mariadb: 'MB', sqlite: 'SQ', mssql: 'MS' } as Record<string,string>)[d] ?? '??' }
</script>

<template>
  <div style="display:flex;flex-direction:column;width:100%;height:100%;min-height:0;overflow:hidden">

    <!-- Sub-tab bar -->
    <div class="sp-toolbar">
      <div class="sp-tabs">
        <button class="sp-tab" :class="{ 'sp-tab--active': activeSubTab === 'data' }" @click="activeSubTab = 'data'">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
          Data Browser
        </button>
        <button class="sp-tab" :class="{ 'sp-tab--active': activeSubTab === 'schema' }" @click="activeSubTab = 'schema'">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polygon points="12 2 2 7 12 12 22 7 12 2"/><polyline points="2 17 12 22 22 17"/><polyline points="2 12 12 17 22 12"/></svg>
          Schema
        </button>
        <button
          v-for="tab in sqlViewTabs" :key="tab.id"
          class="sp-tab"
          :class="{ 'sp-tab--active': activeSubTab === tab.id }"
          @click="activeSubTab = tab.id"
        >
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
          {{ tab.label }}
          <span class="sp-tab__close" @click.stop="closeSqlTab(tab.id)">×</span>
        </button>
        <button class="sp-tab-new" @click="openSqlTab()">
          <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
          SQL
        </button>
      </div>
    </div>

    <!-- No connection -->
    <div v-if="!activeConn" class="sp-no-conn">
      <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" style="color:var(--text-muted)"><path d="M18.36 6.64a9 9 0 1 1-12.73 0"/><line x1="12" y1="2" x2="12" y2="12"/></svg>
      <p style="font-size:13px;color:var(--text-muted);margin:0">No connection — pick one from the session tabs above</p>
    </div>

    <!-- DATA BROWSER -->
    <div v-else v-show="activeSubTab === 'data'" style="display:flex;flex:1;min-height:0;overflow:hidden">
      <div style="width:220px;flex-shrink:0;border-right:1px solid var(--border);display:flex;flex-direction:column;background:var(--bg-surface);overflow:hidden">
        <div class="panel-header">
          <span style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{{ activeConn.name }}</span>
          <span class="driver-badge" :style="{ background: driverColor(activeConn.driver) }">{{ driverLabel(activeConn.driver) }}</span>
        </div>
        <SchemaTree :connId="connId" @select-table="handleSelectTable" />
      </div>
      <div style="flex:1;min-width:0;display:flex;flex-direction:column;overflow:hidden">
        <div style="padding:10px 16px;border-bottom:1px solid var(--border);background:var(--bg-surface);display:flex;align-items:center;gap:10px;flex-shrink:0">
          <template v-if="selected">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="color:var(--brand)"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
            <span style="font-size:13px;font-weight:600;color:var(--text-primary)">{{ selected.db }}.{{ selected.table }}</span>
            <span v-if="totalRows" style="font-size:11px;color:var(--text-muted)">{{ totalRows.toLocaleString() }} rows</span>
            <div style="flex:1"/>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="loadData">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.08-4.43"/></svg>
              Refresh
            </button>
            <button class="base-btn base-btn--sm" :class="editMode ? 'base-btn--primary' : 'base-btn--ghost'" @click="editMode = !editMode">{{ editMode ? 'Editing' : 'Edit' }}</button>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="openImport">Import</button>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="exportCsv" :disabled="!rows.length">CSV</button>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="exportJson" :disabled="!rows.length">JSON</button>
            <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!columns.length" @click="profilerShow=true">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="18" y1="20" x2="18" y2="10"/><line x1="12" y1="20" x2="12" y2="4"/><line x1="6" y1="20" x2="6" y2="14"/></svg>
              Profile
            </button>
          </template>
          <span v-else style="font-size:13px;color:var(--text-muted)">Select a table to browse data</span>
        </div>
        <div style="flex:1;min-height:0;overflow:hidden;display:flex;flex-direction:column">
          <DataTable v-if="selected" :columns="columns" :rows="rows" :loading="loading" :page="page" :page-size="pageSize" :total-rows="totalRows" :editable="editMode" :pk-column="pkColumn" @page-change="handlePageChange" @sort="handleSort" @save-row="handleSaveRow" @delete-row="handleDeleteRow" @add-row="handleAddRow" />
          <div v-else class="empty-state">
            <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" style="color:var(--text-muted)"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
            Select a table from the left to browse its data.
          </div>
        </div>
      </div>
    </div>

    <!-- SCHEMA -->
    <div v-if="activeConn" v-show="activeSubTab === 'schema'" style="display:flex;flex:1;min-height:0;overflow:hidden">
      <div style="width:260px;flex-shrink:0;border-right:1px solid var(--border);display:flex;flex-direction:column;background:var(--bg-surface);overflow:hidden">
        <div class="panel-header">
          <span style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{{ activeConn.name }}</span>
          <span class="driver-badge" :style="{ background: driverColor(activeConn.driver) }">{{ driverLabel(activeConn.driver) }}</span>
        </div>
        <SchemaTree :connId="connId" @select-table="handleSchemaSelectTable" />
      </div>
      <div style="flex:1;min-width:0;display:flex;flex-direction:column;overflow:hidden">
        <div style="padding:10px 16px;border-bottom:1px solid var(--border);background:var(--bg-surface);display:flex;align-items:center;gap:10px;flex-shrink:0;min-height:40px">
          <template v-if="schemaSelected">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="color:var(--brand)"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
            <span style="font-size:13px;font-weight:600;color:var(--text-primary)">{{ schemaSelected.db }}.{{ schemaSelected.table }}</span>
            <span class="schema-type-badge">{{ schemaSelected.type }}</span>
            <div style="flex:1"/>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="handleSelectTable({ db: schemaSelected.db, table: schemaSelected.table }); activeSubTab = 'data'">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
              Browse Data
            </button>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="openSchemaTableInSQL(schemaSelected!.table)">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
              Query
            </button>
          </template>
          <span v-else style="font-size:13px;color:var(--text-muted)">Select a table to inspect its schema</span>
        </div>
        <div v-if="schemaSelected" style="padding:6px 16px;border-bottom:1px solid var(--border);background:var(--bg-surface);display:flex;align-items:center;gap:8px;flex-shrink:0">
          <span style="font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.4px;color:var(--text-muted)">Columns</span>
          <span class="schema-type-badge">{{ schemaColumns.length }}</span>
        </div>
        <div v-if="schemaSelected" style="flex:1;overflow:hidden">
          <DataTable :columns="['Name','Type','Nullable','PK','Default']" :rows="schemaColumnRows" :loading="schemaLoadingCols" :show-row-numbers="false" />
        </div>
        <div v-else class="empty-state">
          <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" style="color:var(--text-muted)"><polygon points="12 2 2 7 12 12 22 7 12 2"/><polyline points="2 17 12 22 22 17"/><polyline points="2 12 12 17 22 12"/></svg>
          Select a table from the left to inspect its columns.
        </div>
      </div>
    </div>

    <!-- SQL TABS -->
    <template v-if="activeConn">
      <div v-for="tab in sqlViewTabs" :key="tab.id" v-show="activeSubTab === tab.id" style="display:flex;flex:1;min-height:0;flex-direction:column;overflow:hidden">
        <!-- Result area -->
        <div style="flex:1;min-height:0;overflow:hidden;display:flex;flex-direction:column">
          <div v-if="tab.result" class="res-tabs">
            <button class="res-tab" :class="{'res-tab--active':tab.activeResultTab==='query'}" :disabled="tab.result.kind!=='query'&&tab.result.kind!=='stream'" @click="tab.activeResultTab='query'">
              <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
              Results<span v-if="tab.result.kind==='query'" class="res-tab__badge">{{ (tab.result as any).row_count }}</span>
            </button>
            <button class="res-tab" :class="{'res-tab--active':tab.activeResultTab==='explain'}" :disabled="tab.result.kind!=='explain'" @click="tab.activeResultTab='explain'">Explain</button>
            <button class="res-tab" :class="{'res-tab--active':tab.activeResultTab==='history'}" :disabled="tab.result.kind!=='history'" @click="tab.activeResultTab='history'">
              History<span v-if="tab.result.kind==='history'" class="res-tab__badge">{{ (tab.result as any).items?.length }}</span>
            </button>
            <button class="res-tab" :class="{'res-tab--active':tab.activeResultTab==='saved'}" :disabled="tab.result.kind!=='saved'" @click="tab.activeResultTab='saved'">
              Saved<span v-if="tab.result.kind==='saved'" class="res-tab__badge">{{ (tab.result as any).queries?.length }}</span>
            </button>
            <button class="res-tab" :class="{'res-tab--active':tab.activeResultTab==='script'}" :disabled="tab.result.kind!=='script'" @click="tab.activeResultTab='script'">
              Script<span v-if="tab.result.kind==='script'" class="res-tab__badge">{{ (tab.result as any).results?.length }}</span>
            </button>
            <button class="res-tab" :class="{'res-tab--active':tab.activeResultTab==='chart'}" :disabled="tab.result.kind!=='query'" @click="tab.activeResultTab='chart'">Chart</button>
            <div style="flex:1"/>
            <template v-if="tab.result.kind==='query'">
              <span class="res-meta">{{ (tab.result as any).duration_ms }}ms · {{ (tab.result as any).row_count }} rows</span>
              <button class="base-btn base-btn--ghost base-btn--xs" @click="sqlPanelRefs[tab.id]?.exportCurrentResult('csv',(tab.result as any).columns,(tab.result as any).rows)">CSV</button>
              <button class="base-btn base-btn--ghost base-btn--xs" @click="sqlPanelRefs[tab.id]?.exportCurrentResult('json',(tab.result as any).columns,(tab.result as any).rows)">JSON</button>
              <button class="base-btn base-btn--ghost base-btn--xs" @click="pinTabResult(tab.id)" title="Pin">Pin</button>
            </template>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="clearTabResult(tab.id)">
              <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg> Clear
            </button>
          </div>
          <div v-if="tab.result" style="flex:1;min-height:0;overflow:hidden;display:flex;flex-direction:column">
            <div v-if="tab.result.kind==='error'" class="res-error">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="flex-shrink:0;color:#f87171"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/></svg>
              {{ (tab.result as any).error }}
            </div>
            <template v-else-if="(tab.result.kind==='query'||tab.result.kind==='stream')&&tab.activeResultTab!=='chart'">
              <VirtualTable :columns="(tab.result as any).columns" :rows="(tab.result as any).rows" :selectable="true" />
            </template>
            <template v-else-if="tab.result.kind==='query'&&tab.activeResultTab==='chart'">
              <div style="flex:1;min-height:0;padding:12px;overflow:hidden;display:flex;flex-direction:column">
                <ResultChart :columns="(tab.result as any).columns" :rows="(tab.result as any).rows" />
              </div>
            </template>
            <template v-else-if="tab.result.kind==='explain'">
              <div class="res-explain">
                <div v-if="(tab.result as any).data" class="res-explain__header">
                  <span class="explain-driver">{{ (tab.result as any).data?.driver }}</span>
                  <span class="explain-format">{{ (tab.result as any).data?.format }}</span>
                </div>
                <ExplainTree :result="(tab.result as any).data" />
              </div>
            </template>
            <template v-else-if="tab.result.kind==='history'">
              <div class="res-list-toolbar"><span style="font-size:11px;color:var(--text-muted)">{{ (tab.result as any).items?.length }} entries</span></div>
              <div style="flex:1;overflow-y:auto">
                <div v-for="(item,i) in (tab.result as any).items" :key="i" class="res-list-item" @click="useSQLInTab(tab.id,item.sql)">
                  <div class="res-list-item__sql">{{ item.sql }}</div>
                  <div class="res-list-item__meta"><span :style="{color:item.error?'#f87171':'#4ade80'}">{{ item.error?'✗':'✓' }} {{ item.row_count }} rows</span><span>{{ item.duration_ms }}ms</span></div>
                </div>
                <div v-if="!(tab.result as any).items?.length" class="empty-state" style="font-size:12px">No history yet.</div>
              </div>
            </template>
            <template v-else-if="tab.result.kind==='saved'">
              <div class="res-list-toolbar"><span style="font-size:11px;color:var(--text-muted)">{{ (tab.result as any).queries?.length }} saved</span></div>
              <div style="flex:1;overflow-y:auto">
                <div v-for="q in (tab.result as any).queries" :key="q.id" class="res-list-item" @click="useSQLInTab(tab.id,q.sql)">
                  <div style="font-size:12px;font-weight:600;color:var(--text-primary)">{{ q.name }}</div>
                  <div class="res-list-item__sql">{{ q.sql }}</div>
                </div>
                <div v-if="!(tab.result as any).queries?.length" class="empty-state" style="font-size:12px">No saved queries.</div>
              </div>
            </template>
            <template v-else-if="tab.result.kind==='script'">
              <div style="flex:1;overflow-y:auto;padding:8px">
                <div v-for="sr in (tab.result as any).results" :key="sr.index" class="script-result">
                  <div class="script-result-header" :class="{'is-error':sr.error}">
                    <span class="script-result-num">#{{ sr.index+1 }}</span>
                    <code class="script-result-sql">{{ sr.sql.slice(0,80) }}{{ sr.sql.length>80?'…':'' }}</code>
                    <span class="script-result-meta">{{ sr.duration_ms }}ms</span>
                    <span v-if="sr.error" class="script-result-err">✗ {{ sr.error }}</span>
                    <span v-else-if="sr.columns.length" class="script-result-ok">{{ sr.row_count }} rows</span>
                    <span v-else class="script-result-ok">{{ sr.affected_rows }} affected</span>
                  </div>
                  <VirtualTable v-if="sr.columns.length&&!sr.error" :columns="sr.columns" :rows="sr.rows" style="max-height:200px" />
                </div>
              </div>
            </template>
          </div>
          <div v-else class="empty-state" style="font-size:12.5px">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" style="color:var(--text-muted)"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
            Run a query to see results here.
          </div>
        </div>
        <div class="sql-resize-handle" @mousedown="(e) => onSqlResizeStart(e, tab.id)" :class="{'sql-resize-handle--active':resizingTabId===tab.id}">
          <div class="sql-resize-handle__bar" />
        </div>
        <div class="sql-panel" :style="{ height: tab.sqlHeight + 'px' }">
          <SQLPanel
            :ref="(el: any) => { if (el) sqlPanelRefs[tab.id] = el }"
            :conn-id="connId" :default-db="selected?.db" :table-name="selected?.table" :dark-mode="darkMode"
            @result="(p) => onSQLResult(tab.id, p)"
            @close="closeSqlTab(tab.id)"
          />
        </div>
      </div>
    </template>
  </div>

  <ColumnProfiler :show="profilerShow" :conn-id="connId" :table="selected?.table ?? ''" :column="columns[0] ?? ''" :database="selected?.db" @close="profilerShow=false" />

  <!-- Import modal -->
  <Teleport to="body">
    <div v-if="importOpen" class="imp-overlay" @click.self="importOpen=false">
      <div class="imp-modal">
        <div class="imp-header">
          <span class="imp-title">Import into <strong>{{ selected?.table }}</strong></span>
          <button class="imp-close" @click="importOpen=false">×</button>
        </div>
        <div class="imp-body">
          <div v-if="!importColumns.length" class="imp-drop" :class="{'imp-drop--over':importDragOver}" @dragover.prevent="importDragOver=true" @dragleave="importDragOver=false" @drop.prevent="onImportDrop">
            <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" style="color:var(--brand)"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
            <div style="font-size:14px;font-weight:600;color:var(--text-primary)">Drop CSV or JSON here</div>
            <label class="base-btn base-btn--ghost base-btn--sm" style="cursor:pointer">Browse file<input type="file" accept=".csv,.json" style="display:none" @change="onImportFilePick" /></label>
          </div>
          <div v-else class="imp-preview">
            <div style="font-size:12px;color:var(--text-muted);margin-bottom:8px">{{ importRows.length }} rows · {{ importColumns.length }} columns <button class="base-btn base-btn--ghost base-btn--xs" style="margin-left:8px" @click="importColumns=[];importRows=[]">Change file</button></div>
            <div class="imp-table-wrap">
              <table class="imp-table">
                <thead><tr><th v-for="col in importColumns" :key="col">{{ col }}</th></tr></thead>
                <tbody>
                  <tr v-for="(row,i) in importRows.slice(0,5)" :key="i"><td v-for="(val,j) in row" :key="j">{{ val??'NULL' }}</td></tr>
                  <tr v-if="importRows.length>5"><td :colspan="importColumns.length" style="text-align:center;color:var(--text-muted);font-size:11px">… {{ importRows.length-5 }} more rows</td></tr>
                </tbody>
              </table>
            </div>
          </div>
          <div v-if="importError" class="notice notice--error" style="margin-top:10px">{{ importError }}</div>
        </div>
        <div class="imp-footer">
          <button class="base-btn base-btn--ghost base-btn--sm" @click="importOpen=false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="!importColumns.length||importLoading" @click="confirmImport">{{ importLoading?'Importing…':`Import ${importRows.length} rows` }}</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.sp-toolbar { display:flex;align-items:center;padding:0 12px;border-bottom:1px solid var(--border);background:var(--bg-surface);flex-shrink:0;min-height:36px;overflow:visible;position:relative;z-index:5; }
.sp-tabs { display:flex;align-items:stretch;flex:1;overflow-x:auto;scrollbar-width:none;gap:0; }
.sp-tabs::-webkit-scrollbar { display:none; }
.sp-tab { display:flex;align-items:center;gap:6px;padding:0 12px;height:36px;border:none;border-right:1px solid var(--border);background:transparent;color:var(--text-muted);font-size:12px;font-weight:500;cursor:pointer;white-space:nowrap;transition:color .12s,background .12s;border-bottom:2px solid transparent;flex-shrink:0; }
.sp-tab:hover { color:var(--text-primary);background:var(--bg-elevated); }
.sp-tab--active { color:var(--text-primary);background:var(--bg-body);border-bottom-color:var(--brand); }
.sp-tab__close { display:flex;align-items:center;justify-content:center;width:16px;height:16px;border-radius:3px;font-size:14px;color:var(--text-muted);line-height:1;transition:background .1s,color .1s; }
.sp-tab__close:hover { background:var(--bg-elevated);color:var(--text-primary); }
.sp-tab-new { display:flex;align-items:center;gap:5px;padding:0 10px;height:36px;border:none;border-right:1px solid var(--border);background:transparent;color:var(--brand);font-size:12px;font-weight:600;cursor:pointer;white-space:nowrap;transition:background .12s;flex-shrink:0; }
.sp-tab-new:hover { background:var(--brand-dim); }
.sp-no-conn { flex:1;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:12px; }
.panel-header { display:flex;align-items:center;justify-content:space-between;padding:8px 12px;border-bottom:1px solid var(--border);font-size:12px;font-weight:600;color:var(--text-secondary);flex-shrink:0; }
.driver-badge { display:inline-flex;align-items:center;justify-content:center;width:22px;height:16px;border-radius:3px;font-size:9px;font-weight:700;color:#fff;letter-spacing:.3px; }
.schema-type-badge { display:inline-flex;align-items:center;padding:1px 6px;border-radius:4px;font-size:10px;font-weight:600;background:var(--bg-elevated);color:var(--text-muted);border:1px solid var(--border);text-transform:uppercase;letter-spacing:.3px; }
.sql-resize-handle { flex-shrink:0;height:6px;cursor:ns-resize;background:var(--bg-surface);border-top:1px solid var(--border);display:flex;align-items:center;justify-content:center;user-select:none;transition:background .15s; }
.sql-resize-handle:hover,.sql-resize-handle--active { background:var(--brand-dim); }
.sql-resize-handle__bar { width:32px;height:3px;border-radius:2px;background:var(--border); }
.sql-resize-handle:hover .sql-resize-handle__bar,.sql-resize-handle--active .sql-resize-handle__bar { background:var(--brand); }
.sql-panel { flex-shrink:0;display:flex;flex-direction:column;background:var(--bg-surface);overflow:hidden; }
.res-tabs { display:flex;align-items:center;gap:0;background:var(--bg-elevated);border-bottom:1px solid var(--border);flex-shrink:0;min-height:32px;padding:0 4px;overflow-x:auto;scrollbar-width:none; }
.res-tabs::-webkit-scrollbar { display:none; }
.res-tab { display:flex;align-items:center;gap:5px;padding:0 10px;height:32px;border:none;border-radius:0;background:transparent;color:var(--text-muted);font-size:11.5px;cursor:pointer;white-space:nowrap;transition:color .1s,background .1s;border-bottom:2px solid transparent; }
.res-tab:hover:not(:disabled) { color:var(--text-primary);background:var(--bg-surface); }
.res-tab--active { color:var(--text-primary);border-bottom-color:var(--brand); }
.res-tab:disabled { opacity:.35;cursor:default; }
.res-tab__badge { background:var(--brand-dim);color:var(--brand);border-radius:10px;padding:0 5px;font-size:10px;font-weight:700; }
.res-meta { font-size:11px;color:var(--text-muted);padding:0 4px; }
.res-error { display:flex;align-items:flex-start;gap:8px;padding:12px;font-size:12px;color:#f87171;background:rgba(248,113,113,.06);border-bottom:1px solid rgba(248,113,113,.15);line-height:1.5;flex-shrink:0; }
.res-explain { flex:1;min-height:0;display:flex;flex-direction:column;overflow:hidden; }
.res-explain__header { display:flex;gap:8px;padding:6px 12px;border-bottom:1px solid var(--border);background:var(--bg-surface);flex-shrink:0; }
.explain-driver,.explain-format { font-size:11px;padding:2px 6px;border-radius:4px;background:var(--bg-elevated);color:var(--text-muted);font-weight:600; }
.res-list-toolbar { display:flex;align-items:center;justify-content:space-between;padding:6px 12px;border-bottom:1px solid var(--border);background:var(--bg-surface);flex-shrink:0; }
.res-list-item { padding:8px 12px;border-bottom:1px solid var(--border);cursor:pointer;transition:background .1s; }
.res-list-item:hover { background:var(--bg-elevated); }
.res-list-item__sql { font-size:12px;color:var(--text-primary);font-family:var(--mono,monospace);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:100%; }
.res-list-item__meta { display:flex;gap:10px;margin-top:3px;font-size:11px;color:var(--text-muted); }
.script-result { margin-bottom:8px;border:1px solid var(--border);border-radius:6px;overflow:hidden; }
.script-result-header { display:flex;align-items:center;gap:8px;padding:6px 10px;background:var(--bg-surface);font-size:11.5px; }
.script-result-header.is-error { background:rgba(248,113,113,.08); }
.script-result-num { font-weight:700;color:var(--text-muted); }
.script-result-sql { flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:11px;color:var(--text-primary); }
.script-result-meta { color:var(--text-muted); }
.script-result-ok { color:#4ade80; }
.script-result-err { color:#f87171; }
.imp-overlay { position:fixed;inset:0;background:rgba(0,0,0,.55);display:flex;align-items:center;justify-content:center;z-index:1000; }
.imp-modal { background:var(--bg-elevated);border:1px solid var(--border);border-radius:10px;width:min(720px,94vw);max-height:80vh;display:flex;flex-direction:column;box-shadow:0 24px 64px rgba(0,0,0,.5); }
.imp-header { display:flex;align-items:center;justify-content:space-between;padding:14px 20px;border-bottom:1px solid var(--border); }
.imp-title { font-size:14px;color:var(--text-secondary); }
.imp-close { background:transparent;border:none;font-size:20px;color:var(--text-muted);cursor:pointer;padding:0 4px;line-height:1; }
.imp-body { flex:1;min-height:0;overflow-y:auto;padding:20px; }
.imp-footer { padding:12px 20px;border-top:1px solid var(--border);display:flex;gap:8px;justify-content:flex-end; }
.imp-drop { border:2px dashed var(--border);border-radius:10px;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:10px;padding:40px 20px;text-align:center;transition:border-color .15s,background .15s;cursor:default; }
.imp-drop--over { border-color:var(--brand);background:var(--brand-dim); }
.imp-table-wrap { max-height:280px;overflow:auto;border:1px solid var(--border);border-radius:6px; }
.imp-table { width:100%;border-collapse:collapse;font-size:12px; }
.imp-table th { padding:6px 12px;background:var(--bg-surface);border-bottom:1px solid var(--border);font-weight:600;text-align:left;color:var(--text-muted);white-space:nowrap; }
.imp-table td { padding:5px 12px;border-bottom:1px solid var(--border);color:var(--text-primary);font-family:var(--mono,monospace);white-space:nowrap; }
@keyframes spin { to { transform:rotate(360deg); } }
</style>
