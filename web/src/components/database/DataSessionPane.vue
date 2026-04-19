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

const props = defineProps<{ connId: number | null; darkMode: boolean; initialSQL?: string | null; initialDb?: string; initialTable?: string }>()
const emit = defineEmits<{ (e: 'table-selected', db: string, table: string): void }>()

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
  emit('table-selected', payload.db, payload.table)
}

function handleSort(col: string, dir: 'asc' | 'desc') { sortBy.value = col; sortDir.value = dir; page.value = 1; loadData() }
function handlePageChange(p: number) { page.value = p; loadData() }
function handlePageSizeChange(size: number) { pageSize.value = size; page.value = 1; loadData() }

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

// ── Schema Explorer state (full metadata) ────────────────────────
import SchemaExplorerTree from '@/components/database/SchemaExplorerTree.vue'
import { useSchema as useSchemaExplorer } from '@/composables/useSchema'

const { 
  databases, 
  loadingSchema, 
  metadata, 
  objectDetail, 
  fetchSchema: fetchSchemaList, 
  fetchMetadata, 
  fetchObjectDetail 
} = useSchemaExplorer()

const activeDatabase = ref('')
const selectedObjectKey = ref('')
const detailLoading = ref(false)

watch(() => props.connId, async (id) => {
  metadata.value = null
  objectDetail.value = null
  selectedObjectKey.value = ''
  activeDatabase.value = ''
  if (!id) return
  await fetchSchemaList(id)
  activeDatabase.value = databases.value[0]?.name ?? ''
}, { immediate: true })

watch(activeDatabase, async (dbName) => {
  metadata.value = null
  objectDetail.value = null
  selectedObjectKey.value = ''
  if (!props.connId || !dbName) return
  const catalog = await fetchMetadata(props.connId, dbName)
  const firstItem = catalog?.groups.find(group => group.items.length > 0)?.items[0]
  if (firstItem) {
    await selectSchemaObject({ type: firstItem.type, name: firstItem.name })
  }
})

async function selectSchemaObject(payload: { type: string; name: string }) {
  if (!props.connId || !activeDatabase.value) return
  selectedObjectKey.value = `${payload.type}:${payload.name}`
  detailLoading.value = true
  await fetchObjectDetail(props.connId, activeDatabase.value, payload.type, payload.name)
  detailLoading.value = false
}

function rowsForProperties(properties: any[]) {
  return properties.map((property) => [property.label, property.value])
}

const indexRows = computed(() => (objectDetail.value?.indexes ?? []).map((index) => [
  index.name,
  index.table_name,
  index.method,
  index.is_unique ? 'YES' : 'NO',
  index.is_primary ? 'YES' : '',
  (index.columns ?? []).join(', '),
]))

const constraintRows = computed(() => (objectDetail.value?.constraints ?? []).map((constraint) => [
  constraint.name,
  constraint.constraint_type,
  (constraint.columns ?? []).join(', '),
  constraint.referenced_table ?? '',
  constraint.definition,
]))

const triggerRows = computed(() => (objectDetail.value?.triggers ?? []).map((trigger) => [
  trigger.name,
  trigger.table_name,
  trigger.timing,
  trigger.events,
]))

const sequenceRows = computed(() => (objectDetail.value?.sequences ?? []).map((sequence) => [
  sequence.name,
  sequence.start_value,
  sequence.increment_by,
  sequence.cache_size,
  sequence.cycle ? 'YES' : 'NO',
  sequence.owned_by ?? '',
]))

const copiedDDL = ref(false)
async function copyDDL(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    copiedDDL.value = true
    setTimeout(() => { copiedDDL.value = false }, 1500)
  } catch {
    toast.error('Failed to copy')
  }
}

const columnDetailRows = computed(() => (objectDetail.value?.columns ?? []).map((column) => [
  column.name,
  column.data_type,
  column.is_nullable ? 'YES' : 'NO',
  column.is_primary_key ? 'YES' : '',
  column.default_value ?? '',
]))

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

// Open with initial SQL if provided; restore last selected table
onMounted(() => {
  if (props.initialSQL) openSqlTab(props.initialSQL)
  if (props.initialDb && props.initialTable) {
    handleSelectTable({ db: props.initialDb, table: props.initialTable })
  }
})

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
          Browse
        </button>
        <button class="sp-tab" :class="{ 'sp-tab--active': activeSubTab === 'explorer' }" @click="activeSubTab = 'explorer'">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/><path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/></svg>
          Explorer
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
      <div class="sidebar-panel">
        <div class="panel-header">
          <span style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{{ activeConn.name }}</span>
          <span class="driver-badge" :style="{ background: driverColor(activeConn.driver) }">{{ driverLabel(activeConn.driver) }}</span>
        </div>
        <SchemaTree :connId="connId" @select-table="handleSelectTable" />
      </div>
      <div style="flex:1;min-width:0;display:flex;flex-direction:column;overflow:hidden">
        <div class="browse-toolbar">
          <template v-if="selected">
            <div class="browse-toolbar__info">
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="color:var(--brand)"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
              <span class="browse-toolbar__title">{{ selected.db }}.{{ selected.table }}</span>
              <span v-if="totalRows" class="browse-toolbar__meta">{{ totalRows.toLocaleString() }} rows</span>
            </div>
            <div class="browse-toolbar__actions">
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
            </div>
          </template>
          <span v-else class="browse-toolbar__empty">Select a table to browse data</span>
        </div>
        <div style="flex:1;min-height:0;overflow:hidden;display:flex;flex-direction:column">
          <DataTable v-if="selected" :columns="columns" :rows="rows" :loading="loading" :page="page" :page-size="pageSize" :total-rows="totalRows" :editable="editMode" :pk-column="pkColumn" @page-change="handlePageChange" @page-size-change="handlePageSizeChange" @sort="handleSort" @save-row="handleSaveRow" @delete-row="handleDeleteRow" @add-row="handleAddRow" />
          <div v-else class="empty-state">
            <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" style="color:var(--text-muted)"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
            Select a table from the left to browse its data.
          </div>
        </div>
      </div>
    </div>

    <!-- SCHEMA -->
    <div v-if="activeConn" v-show="activeSubTab === 'schema'" style="display:flex;flex:1;min-height:0;overflow:hidden">
      <div class="sidebar-panel">
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

    <!-- EXPLORER (Full Schema Metadata) -->
    <div v-if="activeConn" v-show="activeSubTab === 'explorer'" class="explorer-view">
      <div class="explorer-sidebar">
        <div class="explorer-sidebar__head">
          <select v-model="activeDatabase" class="explorer-db-select" :disabled="!databases.length">
            <option value="" disabled>Select database…</option>
            <option v-for="database in databases" :key="database.name" :value="database.name">{{ database.name }}</option>
          </select>
        </div>
        <div v-if="loadingSchema" class="explorer-empty">Loading…</div>
        <div v-else-if="!activeDatabase" class="explorer-empty">Choose a database</div>
        <SchemaExplorerTree
          v-else
          :catalog="metadata"
          :selected-key="selectedObjectKey"
          @select-object="selectSchemaObject"
        />
      </div>

      <div class="explorer-detail">
        <div v-if="detailLoading" class="explorer-empty">Loading object detail…</div>
        <div v-else-if="!objectDetail" class="explorer-empty">
          <svg width="36" height="36" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" style="opacity:0.3"><path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/><path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/></svg>
          <p>Select an object to view details</p>
        </div>
        <template v-else>
          <div class="explorer-hero">
            <div class="explorer-hero__kicker">{{ objectDetail.type }}</div>
            <div class="explorer-hero__title">{{ objectDetail.name }}</div>
            <div class="explorer-hero__sub">{{ objectDetail.database }}</div>
          </div>

          <div class="explorer-panels">
            <div class="explorer-panel">
              <div class="explorer-panel__header">
                <span>Properties</span>
                <span class="explorer-panel__count">{{ rowsForProperties(objectDetail.properties).length }}</span>
              </div>
              <div class="ex-table-scroll">
                <table class="ex-table">
                  <thead>
                    <tr><th>Property</th><th>Value</th></tr>
                  </thead>
                  <tbody>
                    <tr v-for="(row, i) in rowsForProperties(objectDetail.properties)" :key="i">
                      <td class="ex-table__key">{{ row[0] }}</td>
                      <td>{{ row[1] }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>
            <div v-if="objectDetail.enum_values?.length" class="explorer-panel">
              <div class="explorer-panel__header">
                <span>Enum Values</span>
                <span class="explorer-panel__count">{{ objectDetail.enum_values.length }}</span>
              </div>
              <div class="ex-table-scroll">
                <table class="ex-table">
                  <thead><tr><th>Value</th></tr></thead>
                  <tbody>
                    <tr v-for="(value, i) in objectDetail.enum_values" :key="i">
                      <td><code class="ex-code-inline">{{ value }}</code></td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>
          </div>

          <div class="explorer-panel explorer-panel--code">
            <div class="explorer-panel__header explorer-panel__header--with-action">
              <span>DDL</span>
              <button
                v-if="objectDetail.ddl"
                class="explorer-copy-btn"
                @click="copyDDL(objectDetail.ddl)"
                :title="copiedDDL ? 'Copied!' : 'Copy DDL'"
              >
                <svg v-if="!copiedDDL" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                <svg v-else width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
                {{ copiedDDL ? 'Copied' : 'Copy' }}
              </button>
            </div>
            <pre class="explorer-code">{{ objectDetail.ddl || '-- definition unavailable' }}</pre>
          </div>

          <div v-if="objectDetail.routine" class="explorer-panel">
            <div class="explorer-panel__header">Routine</div>
            <div class="ex-table-scroll">
              <table class="ex-table">
                <thead><tr><th>Field</th><th>Value</th></tr></thead>
                <tbody>
                  <tr><td class="ex-table__key">Type</td><td>{{ objectDetail.routine.routine_type }}</td></tr>
                  <tr><td class="ex-table__key">Identity</td><td>{{ objectDetail.routine.identity }}</td></tr>
                  <tr><td class="ex-table__key">Return Type</td><td>{{ objectDetail.routine.return_type || '—' }}</td></tr>
                </tbody>
              </table>
            </div>
          </div>

          <div v-if="objectDetail.columns?.length" class="explorer-panel">
            <div class="explorer-panel__header">
              <span>Columns</span>
              <span class="explorer-panel__count">{{ objectDetail.columns.length }}</span>
            </div>
            <div class="ex-table-scroll">
              <table class="ex-table ex-table--cols">
                <thead>
                  <tr>
                    <th>Name</th><th>Type</th><th>Nullable</th><th>PK</th><th>Default</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(col, i) in objectDetail.columns" :key="i">
                    <td class="ex-table__key">{{ col.name }}</td>
                    <td><code class="ex-code-inline">{{ col.data_type }}</code></td>
                    <td>
                      <span class="ex-pill" :class="col.is_nullable ? 'ex-pill--muted' : 'ex-pill--solid'">
                        {{ col.is_nullable ? 'YES' : 'NO' }}
                      </span>
                    </td>
                    <td>
                      <span v-if="col.is_primary_key" class="ex-pill ex-pill--brand">PK</span>
                      <span v-else class="ex-table__dim">—</span>
                    </td>
                    <td><code v-if="col.default_value" class="ex-code-inline">{{ col.default_value }}</code><span v-else class="ex-table__dim">—</span></td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <div v-if="objectDetail.indexes?.length" class="explorer-panel">
            <div class="explorer-panel__header">
              <span>Indexes</span>
              <span class="explorer-panel__count">{{ objectDetail.indexes.length }}</span>
            </div>
            <div class="ex-table-scroll">
              <table class="ex-table">
                <thead>
                  <tr>
                    <th>Name</th><th>Table</th><th>Method</th><th>Unique</th><th>Primary</th><th>Columns</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(idx, i) in objectDetail.indexes" :key="i">
                    <td class="ex-table__key">{{ idx.name }}</td>
                    <td>{{ idx.table_name }}</td>
                    <td><code class="ex-code-inline">{{ idx.method }}</code></td>
                    <td><span v-if="idx.is_unique" class="ex-pill ex-pill--solid">YES</span><span v-else class="ex-table__dim">—</span></td>
                    <td><span v-if="idx.is_primary" class="ex-pill ex-pill--brand">PK</span><span v-else class="ex-table__dim">—</span></td>
                    <td><code class="ex-code-inline">{{ (idx.columns ?? []).join(', ') || '—' }}</code></td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <div v-if="objectDetail.constraints?.length" class="explorer-panel">
            <div class="explorer-panel__header">
              <span>Constraints</span>
              <span class="explorer-panel__count">{{ objectDetail.constraints.length }}</span>
            </div>
            <div class="ex-table-scroll">
              <table class="ex-table">
                <thead>
                  <tr>
                    <th>Name</th><th>Type</th><th>Columns</th><th>References</th><th>Definition</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(c, i) in objectDetail.constraints" :key="i">
                    <td class="ex-table__key">{{ c.name }}</td>
                    <td><span class="ex-pill ex-pill--muted">{{ c.constraint_type }}</span></td>
                    <td><code class="ex-code-inline">{{ (c.columns ?? []).join(', ') || '—' }}</code></td>
                    <td>{{ c.referenced_table || '—' }}</td>
                    <td><code class="ex-code-inline ex-code-inline--def">{{ c.definition || '—' }}</code></td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <div v-if="objectDetail.triggers?.length" class="explorer-panel">
            <div class="explorer-panel__header">
              <span>Triggers</span>
              <span class="explorer-panel__count">{{ objectDetail.triggers.length }}</span>
            </div>
            <div class="ex-table-scroll">
              <table class="ex-table">
                <thead><tr><th>Name</th><th>Table</th><th>Timing</th><th>Events</th></tr></thead>
                <tbody>
                  <tr v-for="(t, i) in objectDetail.triggers" :key="i">
                    <td class="ex-table__key">{{ t.name }}</td>
                    <td>{{ t.table_name }}</td>
                    <td><span class="ex-pill ex-pill--muted">{{ t.timing }}</span></td>
                    <td><code class="ex-code-inline">{{ t.events }}</code></td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <div v-if="objectDetail.sequences?.length" class="explorer-panel">
            <div class="explorer-panel__header">
              <span>Sequences</span>
              <span class="explorer-panel__count">{{ objectDetail.sequences.length }}</span>
            </div>
            <div class="ex-table-scroll">
              <table class="ex-table">
                <thead><tr><th>Name</th><th>Start</th><th>Increment</th><th>Cache</th><th>Cycle</th><th>Owned By</th></tr></thead>
                <tbody>
                  <tr v-for="(s, i) in objectDetail.sequences" :key="i">
                    <td class="ex-table__key">{{ s.name }}</td>
                    <td>{{ s.start_value }}</td>
                    <td>{{ s.increment_by }}</td>
                    <td>{{ s.cache_size }}</td>
                    <td><span v-if="s.cycle" class="ex-pill ex-pill--solid">YES</span><span v-else class="ex-table__dim">—</span></td>
                    <td>{{ s.owned_by || '—' }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </template>
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
.sp-toolbar { 
  display: flex;
  align-items: center;
  padding: 8px 20px;
  border-bottom: 1px solid var(--border);
  background: linear-gradient(to bottom, var(--bg-surface), color-mix(in srgb, var(--bg-surface) 98%, transparent));
  flex-shrink: 0;
  min-height: 48px;
  overflow: visible;
  position: relative;
  z-index: 5;
  box-shadow: 0 1px 3px rgba(0,0,0,.03);
}

.sp-tabs { 
  display: flex;
  align-items: stretch;
  flex: 1;
  overflow-x: auto;
  scrollbar-width: none;
  gap: 6px;
}

.sp-tabs::-webkit-scrollbar { display: none; }

.sp-tab { 
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 16px;
  height: 36px;
  border: none;
  background: transparent;
  color: var(--text-muted);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  white-space: nowrap;
  transition: all .2s ease;
  border-radius: 8px;
  flex-shrink: 0;
  position: relative;
  letter-spacing: 0.01em;
}

.sp-tab:hover { 
  color: var(--text-primary);
  background: color-mix(in srgb, var(--bg-elevated) 70%, transparent);
  transform: translateY(-1px);
}

.sp-tab--active { 
  color: var(--text-primary);
  background: var(--bg-body);
  box-shadow: 0 2px 8px rgba(0,0,0,.08), inset 0 0 0 1px var(--border);
}

.sp-tab svg { 
  opacity: 0.65;
  transition: all .2s ease;
}

.sp-tab:hover svg {
  opacity: 0.85;
}

.sp-tab--active svg { 
  opacity: 1;
  color: var(--brand);
}

.sp-tab__close { 
  display: flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  border-radius: 5px;
  font-size: 15px;
  color: var(--text-muted);
  line-height: 1;
  transition: all .15s ease;
}

.sp-tab__close:hover { 
  background: var(--bg-elevated);
  color: var(--text-primary);
  transform: scale(1.1);
}

.sp-tab-new { 
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 0 14px;
  height: 36px;
  border: none;
  background: transparent;
  color: var(--brand);
  font-size: 12.5px;
  font-weight: 700;
  cursor: pointer;
  white-space: nowrap;
  transition: all .2s ease;
  flex-shrink: 0;
  border-radius: 8px;
  letter-spacing: 0.02em;
}

.sp-tab-new:hover { 
  background: var(--brand-dim);
  transform: translateY(-1px);
}

.sp-no-conn { 
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 14px;
}
.panel-header { display:flex;align-items:center;justify-content:space-between;padding:10px 14px;border-bottom:1px solid var(--border);font-size:12px;font-weight:600;color:var(--text-secondary);flex-shrink:0;background:color-mix(in srgb,var(--bg-surface) 95%,transparent); }
.driver-badge { display:inline-flex;align-items:center;justify-content:center;width:24px;height:18px;border-radius:4px;font-size:9px;font-weight:700;color:#fff;letter-spacing:.4px; }
.schema-type-badge { display:inline-flex;align-items:center;padding:2px 8px;border-radius:5px;font-size:10px;font-weight:700;background:var(--bg-elevated);color:var(--text-muted);border:1px solid var(--border);text-transform:uppercase;letter-spacing:.5px; }
.empty-state { display:flex;flex-direction:column;align-items:center;justify-content:center;gap:12px;min-height:240px;text-align:center;color:var(--text-muted);padding:32px;font-size:13px; }
.empty-state svg { opacity:.4; }

/* ── Browse Toolbar (Modern & Spacious) ───────────────────────── */
.browse-toolbar { padding:12px 18px;border-bottom:1px solid var(--border);background:var(--bg-surface);display:flex;align-items:center;justify-content:space-between;gap:16px;flex-shrink:0;min-height:48px; }
.browse-toolbar__info { display:flex;align-items:center;gap:10px;flex:1;min-width:0; }
.browse-toolbar__title { font-size:14px;font-weight:600;color:var(--text-primary);white-space:nowrap;overflow:hidden;text-overflow:ellipsis; }
.browse-toolbar__meta { font-size:11px;color:var(--text-muted);background:var(--bg-elevated);padding:2px 8px;border-radius:4px;font-weight:600; }
.browse-toolbar__actions { display:flex;align-items:center;gap:8px; }
.browse-toolbar__empty { font-size:13px;color:var(--text-muted); }

/* ── Sidebar Panel (Consistent Width & Styling) ───────────────── */
.sidebar-panel { 
  width: 260px;
  flex-shrink: 0;
  border-right: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  background: var(--bg-surface);
  overflow: hidden;
  box-shadow: 2px 0 12px rgba(0,0,0,.03);
}
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

/* ── Explorer View (Modern & Minimalist) ──────────────────────── */
.explorer-view { 
  display: grid;
  grid-template-columns: 340px minmax(0, 1fr);
  flex: 1;
  min-height: 0;
  height: 0; /* force grid children to respect parent height so scroll works */
  overflow: hidden;
  background: var(--bg-body);
}

.explorer-sidebar { 
  border-right: 1px solid var(--border);
  background: var(--bg-surface);
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
  box-shadow: 2px 0 16px rgba(0,0,0,.04);
  height: 100%;
}

.explorer-sidebar__head { 
  padding: 20px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
  background: linear-gradient(to bottom, var(--bg-surface), color-mix(in srgb, var(--bg-surface) 98%, transparent));
}

.explorer-db-select {
  width: 100%;
  font-size: 14px;
  font-weight: 600;
  padding: 12px 16px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-elevated);
  color: var(--text-primary);
  cursor: pointer;
  transition: all .15s ease;
}

.explorer-db-select:hover {
  border-color: color-mix(in srgb, var(--border) 65%, var(--brand) 35%);
  background: var(--bg-body);
}

.explorer-db-select:focus {
  outline: none;
  border-color: var(--brand);
  box-shadow: 0 0 0 4px color-mix(in srgb, var(--brand) 18%, transparent);
}

.explorer-detail { 
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 20px;
  background: var(--bg-body);
}

.explorer-detail::-webkit-scrollbar {
  width: 8px;
}

.explorer-detail::-webkit-scrollbar-track {
  background: transparent;
}

.explorer-detail::-webkit-scrollbar-thumb {
  background: var(--border);
  border-radius: 6px;
  border: 2px solid var(--bg-body);
}

.explorer-detail::-webkit-scrollbar-thumb:hover {
  background: color-mix(in srgb, var(--border) 60%, var(--brand) 40%);
}

.explorer-empty { 
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 14px;
  min-height: 300px;
  text-align: center;
  color: var(--text-muted);
  padding: 40px;
  font-size: 14px;
}

.explorer-empty p {
  font-weight: 500;
  opacity: 0.8;
}

.explorer-hero { 
  padding: 28px 32px;
  border: 1px solid color-mix(in srgb, var(--border) 60%, var(--brand) 40%);
  border-radius: 16px;
  background: 
    radial-gradient(circle at top right, color-mix(in srgb, var(--brand) 8%, transparent), transparent 50%),
    linear-gradient(135deg, 
      color-mix(in srgb, var(--bg-elevated) 95%, var(--brand) 5%),
      var(--bg-elevated)
    );
  box-shadow: 0 4px 16px rgba(0,0,0,.06);
  position: relative;
  overflow: hidden;
  flex-shrink: 0;
}

.explorer-hero::before {
  content: '';
  position: absolute;
  top: 0;
  right: 0;
  width: 200px;
  height: 200px;
  background: radial-gradient(circle, color-mix(in srgb, var(--brand) 10%, transparent), transparent 70%);
  pointer-events: none;
}

.explorer-hero__kicker { 
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 1.2px;
  font-weight: 800;
  color: var(--brand);
  opacity: 0.9;
  position: relative;
  z-index: 1;
}

.explorer-hero__title { 
  font-size: 26px;
  font-weight: 800;
  color: var(--text-primary);
  margin-top: 8px;
  position: relative;
  z-index: 1;
  letter-spacing: -0.02em;
}

.explorer-hero__sub { 
  color: var(--text-muted);
  margin-top: 6px;
  font-size: 14px;
  font-weight: 500;
  position: relative;
  z-index: 1;
}

.explorer-panels { 
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
  gap: 20px;
  flex-shrink: 0;
}

.explorer-panel { 
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--bg-elevated);
  overflow: hidden;
  box-shadow: 0 1px 3px rgba(0,0,0,.03);
  transition: box-shadow .2s ease, border-color .2s ease;
  display: flex;
  flex-direction: column;
  flex-shrink: 0; /* prevent flex-parent from squishing panel content */
}

.explorer-panel:hover {
  box-shadow: 0 4px 16px rgba(0,0,0,.06);
  border-color: color-mix(in srgb, var(--border) 70%, var(--brand) 30%);
}

.explorer-panel__count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 22px;
  height: 18px;
  padding: 0 6px;
  font-size: 10px;
  font-weight: 700;
  color: var(--brand);
  background: color-mix(in srgb, var(--brand) 12%, transparent);
  border-radius: 9px;
  letter-spacing: 0;
  text-transform: none;
}

/* ── Native explorer tables (no DataTable component) ──────────── */
.ex-table-scroll {
  overflow-x: auto;
  overflow-y: auto;
  max-height: 420px;
}

.ex-table-scroll::-webkit-scrollbar {
  width: 8px;
  height: 8px;
}

.ex-table-scroll::-webkit-scrollbar-track {
  background: transparent;
}

.ex-table-scroll::-webkit-scrollbar-thumb {
  background: var(--border);
  border-radius: 6px;
  border: 2px solid var(--bg-elevated);
}

.ex-table-scroll::-webkit-scrollbar-thumb:hover {
  background: color-mix(in srgb, var(--border) 60%, var(--brand) 40%);
}

.ex-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
  background: var(--bg-elevated);
}

.ex-table thead th {
  position: sticky;
  top: 0;
  z-index: 1;
  text-align: left;
  padding: 10px 16px;
  font-size: 10.5px;
  font-weight: 700;
  letter-spacing: 0.6px;
  text-transform: uppercase;
  color: var(--text-muted);
  background: color-mix(in srgb, var(--bg-surface) 60%, var(--bg-elevated));
  border-bottom: 1px solid var(--border);
  white-space: nowrap;
}

.ex-table tbody td {
  padding: 11px 16px;
  border-bottom: 1px solid color-mix(in srgb, var(--border) 50%, transparent);
  color: var(--text-primary);
  vertical-align: top;
  line-height: 1.5;
}

.ex-table tbody tr:last-child td {
  border-bottom: none;
}

.ex-table tbody tr {
  transition: background .12s ease;
}

.ex-table tbody tr:hover {
  background: color-mix(in srgb, var(--brand) 4%, transparent);
}

.ex-table__key {
  font-weight: 600;
  color: var(--text-primary);
  white-space: nowrap;
}

.ex-table__dim {
  color: var(--text-muted);
  opacity: 0.5;
}

.ex-code-inline {
  display: inline-block;
  padding: 2px 8px;
  font-family: var(--mono, 'SF Mono', 'Monaco', 'Cascadia Code', monospace);
  font-size: 12px;
  background: color-mix(in srgb, var(--bg-app) 60%, var(--bg-surface));
  color: var(--text-primary);
  border-radius: 5px;
  border: 1px solid color-mix(in srgb, var(--border) 50%, transparent);
}

.ex-code-inline--def {
  white-space: pre-wrap;
  word-break: break-word;
  max-width: 360px;
}

.ex-pill {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 2px 9px;
  font-size: 10.5px;
  font-weight: 700;
  letter-spacing: 0.4px;
  border-radius: 6px;
  text-transform: uppercase;
}

.ex-pill--solid {
  background: color-mix(in srgb, var(--text-muted) 18%, transparent);
  color: var(--text-primary);
}

.ex-pill--muted {
  background: color-mix(in srgb, var(--text-muted) 10%, transparent);
  color: var(--text-muted);
}

.ex-pill--brand {
  background: color-mix(in srgb, var(--brand) 18%, transparent);
  color: var(--brand);
}

.explorer-panel__header { 
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 18px;
  border-bottom: 1px solid var(--border);
  font-size: 10.5px;
  font-weight: 800;
  letter-spacing: 0.8px;
  text-transform: uppercase;
  color: var(--text-muted);
  background: linear-gradient(to bottom, 
    color-mix(in srgb, var(--bg-surface) 70%, transparent),
    transparent
  );
}

.explorer-panel__header--with-action {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 12px 8px 18px;
}

.explorer-copy-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 6px;
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.4px;
  text-transform: uppercase;
  cursor: pointer;
  transition: all .15s ease;
}

.explorer-copy-btn:hover {
  background: var(--bg-surface);
  color: var(--brand);
  border-color: color-mix(in srgb, var(--border) 60%, var(--brand) 40%);
}

.explorer-panel--code {
  display: flex;
  flex-direction: column;
}

.explorer-code { 
  margin: 0;
  padding: 18px 20px;
  white-space: pre;
  overflow: auto;
  max-height: 480px;
  color: var(--text-primary);
  background: color-mix(in srgb, var(--bg-app) 97%, var(--bg-surface) 3%);
  font-size: 12.5px;
  line-height: 1.6;
  font-family: var(--mono, 'SF Mono', 'Monaco', 'Cascadia Code', monospace);
  border-radius: 0;
  tab-size: 2;
}

.explorer-code::-webkit-scrollbar {
  width: 10px;
  height: 10px;
}

.explorer-code::-webkit-scrollbar-track {
  background: color-mix(in srgb, var(--bg-app) 90%, transparent);
}

.explorer-code::-webkit-scrollbar-thumb {
  background: var(--border);
  border-radius: 6px;
  border: 2px solid transparent;
  background-clip: content-box;
}

.explorer-code::-webkit-scrollbar-thumb:hover {
  background: color-mix(in srgb, var(--border) 60%, var(--brand) 40%);
  background-clip: content-box;
}

.explorer-code::-webkit-scrollbar-corner {
  background: transparent;
}

@keyframes spin { to { transform:rotate(360deg); } }
@media (max-width: 960px) {
  .explorer-view { grid-template-columns:1fr; }
  .explorer-panels { grid-template-columns:1fr; }
}
</style>
