<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import CellInspector from '@/components/ui/CellInspector.vue'

interface Props {
  columns: string[]
  rows: unknown[][]
  rowCount?: number
  loading?: boolean
  page?: number
  pageSize?: number
  totalRows?: number
  showRowNumbers?: boolean
  editable?: boolean
  pkColumn?: string
}

const props = withDefaults(defineProps<Props>(), {
  rowCount: 0,
  loading: false,
  page: 1,
  pageSize: 100,
  totalRows: 0,
  showRowNumbers: true,
  editable: false,
  pkColumn: '',
})

const emit = defineEmits<{
  (e: 'page-change', page: number): void
  (e: 'page-size-change', size: number): void
  (e: 'sort', col: string, dir: 'asc' | 'desc'): void
  (e: 'cell-click', payload: { row: number; col: string; value: unknown }): void
  (e: 'save-row', payload: { pkValue: unknown; updates: Record<string, unknown> }): void
  (e: 'save-all-rows', payload: Array<{ pkValue: unknown; updates: Record<string, unknown> }>): void
  (e: 'delete-row', payload: { pkValue: unknown }): void
  (e: 'add-row', payload: { values: Record<string, unknown> }): void
  (e: 'dirty-change', dirty: boolean): void
}>()

const sortCol = ref<string>('')
const sortDir = ref<'asc' | 'desc'>('asc')

// Column visibility state (all visible by default)
const visibleColumns = ref<Set<string>>(new Set(props.columns))
const showColumnMenu = ref(false)

// Reset all columns to visible when props.columns changes (new table selected)
watch(() => props.columns, (newColumns) => {
  visibleColumns.value = new Set(newColumns)
}, { immediate: true })

const filteredColumns = computed(() => props.columns.filter(c => visibleColumns.value.has(c)))

function toggleColumn(col: string) {
  if (visibleColumns.value.has(col)) {
    visibleColumns.value.delete(col)
  } else {
    visibleColumns.value.add(col)
  }
  visibleColumns.value = new Set(visibleColumns.value)
}

function showAllColumns() {
  visibleColumns.value = new Set(props.columns)
}

function hideAllColumns() {
  visibleColumns.value = new Set()
}

// Cell inspector state
const inspector = ref({ show: false, column: '', value: undefined as unknown, rowIndex: 0 })

// Edit state: Map of rowIndex -> edited cells
const editedRows = ref<Map<number, Record<string, unknown>>>(new Map())
const editHistory = ref<Array<{ rowIndex: number; column: string; previous: unknown; next: unknown }>>([])
const newRowValues = ref<Record<string, unknown>>({})
const showNewRow = ref(false)
const editedRowCount = computed(() => editedRows.value.size)
const hasPendingEdits = computed(() => editedRowCount.value > 0)
const hasDraftChanges = computed(() => hasPendingEdits.value || showNewRow.value)

watch(hasDraftChanges, (dirty) => {
  emit('dirty-change', dirty)
}, { immediate: true })

const totalPages = computed(() =>
  props.totalRows > 0 ? Math.ceil(props.totalRows / props.pageSize) : 1,
)

function handleSort(col: string) {
  if (sortCol.value === col) {
    sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc'
  } else {
    sortCol.value = col
    sortDir.value = 'asc'
  }
  emit('sort', sortCol.value, sortDir.value)
}

function formatCell(val: unknown): string {
  if (val === null || val === undefined) return 'NULL'
  if (typeof val === 'object') return JSON.stringify(val)
  return String(val)
}

function cellClass(val: unknown): string {
  if (val === null || val === undefined) return 'td-null'
  if (typeof val === 'number') return 'td-number'
  if (typeof val === 'boolean') return 'td-bool'
  return ''
}

function handleCellClick(rIdx: number, cIdx: number, val: unknown) {
  const col = props.columns[cIdx]
  emit('cell-click', { row: rIdx, col, value: val })
  if (!props.editable) {
    openInspector(rIdx, col, val)
  }
}

function openInspector(rIdx: number, col: string, val: unknown) {
  inspector.value = { show: true, column: col, value: val, rowIndex: rIdx }
}

function getCellValue(rIdx: number, col: string, originalVal: unknown): unknown {
  return editedRows.value.get(rIdx)?.[col] ?? originalVal
}

function onCellEdit(rIdx: number, col: string, event: Event) {
  const target = event.target as HTMLInputElement
  const currentVal = getCellValue(rIdx, col, props.rows[rIdx]?.[props.columns.indexOf(col)])
  if (currentVal === target.value) return
  if (!editedRows.value.has(rIdx)) {
    editedRows.value.set(rIdx, {})
  }
  editedRows.value.get(rIdx)![col] = target.value
  editHistory.value.push({ rowIndex: rIdx, column: col, previous: currentVal, next: target.value })

  const originalVal = props.rows[rIdx]?.[props.columns.indexOf(col)]
  if (target.value === String(originalVal ?? '')) {
    delete editedRows.value.get(rIdx)![col]
    if (!Object.keys(editedRows.value.get(rIdx) ?? {}).length) {
      editedRows.value.delete(rIdx)
    }
  }
}

function isCellEdited(rIdx: number, col: string): boolean {
  return Object.prototype.hasOwnProperty.call(editedRows.value.get(rIdx) ?? {}, col)
}

function rowLabel(rIdx: number): string {
  return editedRows.value.has(rIdx) ? 'Edited' : ''
}

function saveRow(rIdx: number, row: unknown[]) {
  const edits = editedRows.value.get(rIdx)
  if (!edits || !Object.keys(edits).length) return
  const pkIdx = props.columns.indexOf(props.pkColumn)
  const pkValue = pkIdx >= 0 ? row[pkIdx] : null
  emit('save-row', { pkValue, updates: edits })
  editedRows.value.delete(rIdx)
  editHistory.value = editHistory.value.filter((entry) => entry.rowIndex !== rIdx)
}

function saveAllRows() {
  const payload = [...editedRows.value.entries()].map(([rIdx, updates]) => {
    const row = props.rows[rIdx]
    const pkIdx = props.columns.indexOf(props.pkColumn)
    const pkValue = pkIdx >= 0 ? row?.[pkIdx] : null
    return { pkValue, updates }
  }).filter((item) => item.pkValue !== null && Object.keys(item.updates).length > 0)
  if (!payload.length) return
  emit('save-all-rows', payload)
  editedRows.value.clear()
  editHistory.value = []
}

function cancelEdit(rIdx: number) {
  editedRows.value.delete(rIdx)
  editHistory.value = editHistory.value.filter((entry) => entry.rowIndex !== rIdx)
}

function undoLastEdit() {
  const last = editHistory.value.pop()
  if (!last) return
  const rowMap = editedRows.value.get(last.rowIndex) ?? {}
  const originalVal = props.rows[last.rowIndex]?.[props.columns.indexOf(last.column)]

  if (last.previous === originalVal || String(last.previous ?? '') === String(originalVal ?? '')) {
    delete rowMap[last.column]
  } else {
    rowMap[last.column] = last.previous
  }

  if (Object.keys(rowMap).length) {
    editedRows.value.set(last.rowIndex, rowMap)
  } else {
    editedRows.value.delete(last.rowIndex)
  }
}

function clearAllEdits() {
  editedRows.value.clear()
  editHistory.value = []
  showNewRow.value = false
  newRowValues.value = {}
}

function deleteRow(rIdx: number, row: unknown[]) {
  const pkIdx = props.columns.indexOf(props.pkColumn)
  const pkValue = pkIdx >= 0 ? row[pkIdx] : null
  emit('delete-row', { pkValue })
}

function addRow() {
  const values: Record<string, unknown> = {}
  for (const col of props.columns) {
    values[col] = newRowValues.value[col] ?? ''
  }
  emit('add-row', { values })
  newRowValues.value = {}
  showNewRow.value = false
}

function handleKeydown(event: KeyboardEvent) {
  if (!props.editable) return
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'z') {
    event.preventDefault()
    undoLastEdit()
  }
}
</script>

<template>
  <div style="display:flex;flex-direction:column;height:100%;overflow:hidden" @keydown.capture="handleKeydown">
    <!-- Loading -->
    <div v-if="loading" style="display:flex;align-items:center;gap:8px;padding:16px;color:var(--text-muted);font-size:13px">
      <svg class="spin" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
      Fetching data…
    </div>

    <!-- Empty -->
    <div v-else-if="rows.length === 0 && !showNewRow" class="empty-state">
      No rows returned.
      <button v-if="editable" class="base-btn base-btn--ghost base-btn--xs" style="margin-top:8px" @click="showNewRow=true">
        + Add row
      </button>
    </div>

    <!-- Table -->
    <div v-else class="data-table-wrap">
      <div v-if="editable" class="edit-toolbar" :class="{ 'edit-toolbar--active': hasPendingEdits }">
        <div class="edit-toolbar__status">
          <span class="edit-toolbar__badge">{{ editedRowCount }}</span>
          <span>{{ editedRowCount === 0 ? 'No pending row changes' : `${editedRowCount} row${editedRowCount > 1 ? 's' : ''} pending` }}</span>
        </div>
        <div class="edit-toolbar__actions">
          <span v-if="editHistory.length" class="edit-toolbar__hint">`Ctrl/Cmd+Z` undo</span>
          <button class="base-btn base-btn--ghost base-btn--xs" :disabled="!editHistory.length" @click="undoLastEdit">Undo Last</button>
          <button class="base-btn base-btn--ghost base-btn--xs" :disabled="!hasPendingEdits" @click="clearAllEdits">Discard All</button>
          <button class="base-btn base-btn--primary base-btn--xs" :disabled="!hasPendingEdits" @click="saveAllRows">Save All</button>
        </div>
      </div>
      <table class="data-table">
        <thead>
          <tr>
            <th class="col-rownum" v-if="showRowNumbers">#</th>
            <th v-if="editable" class="col-actions" style="width:80px"></th>
            <th
              v-for="col in columns"
              :key="col"
              v-show="visibleColumns.has(col)"
              :class="{ sorted: sortCol === col }"
              @click="handleSort(col)"
            >
              {{ col }}
              <span class="sort-icon">
                <template v-if="sortCol === col">{{ sortDir === 'asc' ? '↑' : '↓' }}</template>
                <template v-else>↕</template>
              </span>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, rIdx) in rows" :key="rIdx" :class="{ 'tr-edited': editedRows.has(rIdx) }">
            <td class="col-rownum" v-if="showRowNumbers">{{ (page - 1) * pageSize + rIdx + 1 }}</td>
            <td v-if="editable" class="col-actions">
              <div class="row-btns">
                <template v-if="editedRows.has(rIdx)">
                  <span class="row-state-pill">{{ rowLabel(rIdx) }}</span>
                  <button class="rbtn rbtn--save" @click="saveRow(rIdx, row)" title="Save">✓</button>
                  <button class="rbtn rbtn--cancel" @click="cancelEdit(rIdx)" title="Cancel">✕</button>
                </template>
                <template v-else>
                  <button class="rbtn rbtn--delete" @click="deleteRow(rIdx, row)" title="Delete row">🗑</button>
                </template>
              </div>
            </td>
            <td
              v-for="(val, cIdx) in row"
              :key="cIdx"
              v-show="visibleColumns.has(columns[cIdx])"
              :class="cellClass(val)"
              :data-edited="isCellEdited(rIdx, columns[cIdx])"
              @click="handleCellClick(rIdx, cIdx, val)"
            >
              <input
                v-if="editable"
                class="cell-input"
                :value="getCellValue(rIdx, columns[cIdx], val)"
                @input="onCellEdit(rIdx, columns[cIdx], $event)"
                @click.stop
              />
              <template v-else>{{ formatCell(val) }}</template>
            </td>
          </tr>

          <!-- New row -->
          <tr v-if="showNewRow" class="tr-new">
            <td class="col-rownum" v-if="showRowNumbers">*</td>
            <td v-if="editable" class="col-actions">
              <div class="row-btns">
                <button class="rbtn rbtn--save" @click="addRow" title="Insert">✓</button>
                <button class="rbtn rbtn--cancel" @click="showNewRow=false" title="Cancel">✕</button>
              </div>
            </td>
            <td v-for="col in columns" :key="col" v-show="visibleColumns.has(col)">
              <input
                class="cell-input"
                :placeholder="col"
                v-model="newRowValues[col]"
                @click.stop
              />
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Add row toolbar -->
    <div v-if="editable && rows.length > 0 && !showNewRow" class="add-row-bar">
      <button class="base-btn base-btn--ghost base-btn--xs" @click="showNewRow=true">+ Add row</button>
    </div>

    <!-- Pagination & Controls -->
    <div class="pagination" v-if="totalRows > 0">
      <span class="pagination__info">
        Rows {{ (page - 1) * pageSize + 1 }}–{{ Math.min(page * pageSize, totalRows) }} of {{ totalRows.toLocaleString() }}
      </span>
      <div style="display:flex;align-items:center;gap:6px;margin-left:12px">
        <span style="font-size:11px;color:var(--text-muted)">Per page:</span>
        <select class="page-size-select" :value="pageSize" @change="emit('page-size-change', Number(($event.target as HTMLSelectElement).value))">
          <option value="25">25</option>
          <option value="50">50</option>
          <option value="100">100</option>
          <option value="200">200</option>
          <option value="500">500</option>
        </select>
      </div>
      <div class="pagination__spacer" />
      
      <!-- Column visibility toggle -->
      <div class="col-vis-wrapper" @click.stop>
        <button class="base-btn base-btn--ghost base-btn--xs" @click="showColumnMenu = !showColumnMenu">
          <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
          Columns
        </button>
        <div v-if="showColumnMenu" class="col-vis-menu">
          <div class="col-vis-header">
            <span style="font-weight:600;font-size:11px">Column Visibility</span>
            <div style="display:flex;gap:4px">
              <button class="col-vis-btn" @click="showAllColumns">All</button>
              <button class="col-vis-btn" @click="hideAllColumns">None</button>
            </div>
          </div>
          <div class="col-vis-list">
            <label v-for="col in columns" :key="col" class="col-vis-item">
              <input type="checkbox" :checked="visibleColumns.has(col)" @change="toggleColumn(col)" />
              <span>{{ col }}</span>
            </label>
          </div>
        </div>
      </div>
      
      <button class="base-btn base-btn--ghost base-btn--xs" :disabled="page <= 1" @click="emit('page-change', page - 1)">← Prev</button>
      <span style="font-size:12px;color:var(--text-secondary)">{{ page }} / {{ totalPages }}</span>
      <button class="base-btn base-btn--ghost base-btn--xs" :disabled="page >= totalPages" @click="emit('page-change', page + 1)">Next →</button>
    </div>
    <div class="pagination" v-else-if="rows.length > 0">
      <span class="pagination__info">{{ rows.length.toLocaleString() }} rows</span>
    </div>

    <!-- Cell inspector -->
    <CellInspector
      :show="inspector.show"
      :column="inspector.column"
      :value="inspector.value"
      :row-index="inspector.rowIndex"
      @close="inspector.show = false"
    />
  </div>
</template>

<style scoped>
.col-actions { width: 70px; text-align: center; padding: 0 4px !important; }
.row-btns { display: flex; gap: 4px; justify-content: center; align-items: center; flex-wrap: wrap; }
.edit-toolbar {
  position: sticky;
  top: 0;
  z-index: 3;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-elevated);
}
.edit-toolbar--active {
  background: rgba(var(--brand-rgb, 99 102 241), 0.08);
}
.edit-toolbar__status {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--text-secondary);
}
.edit-toolbar__badge,
.row-state-pill {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 20px;
  height: 20px;
  padding: 0 8px;
  border-radius: 999px;
  background: var(--brand);
  color: white;
  font-size: 10px;
  font-weight: 700;
}
.edit-toolbar__actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
.edit-toolbar__hint {
  font-size: 11px;
  color: var(--text-muted);
}
.rbtn {
  width: 22px; height: 22px;
  border-radius: 4px;
  border: 1px solid var(--border);
  background: transparent;
  cursor: pointer;
  font-size: 11px;
  display: flex; align-items: center; justify-content: center;
  transition: all 0.12s;
}
.rbtn--save { color: #4ade80; }
.rbtn--save:hover { background: rgba(74, 222, 128, 0.15); }
.rbtn--cancel { color: var(--text-muted); }
.rbtn--cancel:hover { background: var(--bg-hover); }
.rbtn--delete { color: #f87171; }
.rbtn--delete:hover { background: rgba(248, 113, 113, 0.15); }
.cell-input {
  width: 100%;
  background: transparent;
  border: none;
  color: var(--text-primary);
  font-size: 12.5px;
  font-family: inherit;
  outline: none;
  padding: 0;
}
.cell-input:focus {
  background: var(--bg-hover);
  padding: 0 4px;
  border-radius: 3px;
}
.tr-edited td { background: rgba(var(--brand-rgb, 99 102 241), 0.08) !important; }
.tr-edited td[data-edited="true"] {
  box-shadow: inset 0 0 0 1px rgba(var(--brand-rgb, 99 102 241), 0.45);
  background: rgba(var(--brand-rgb, 99 102 241), 0.14) !important;
}
.tr-new td { background: rgba(74, 222, 128, 0.05) !important; }
.add-row-bar {
  padding: 6px 12px;
  border-top: 1px solid var(--border);
  background: var(--bg-elevated);
}

/* Page size selector */
.page-size-select {
  padding: 2px 6px;
  border: 1px solid var(--border);
  border-radius: 4px;
  background: var(--bg-surface);
  color: var(--text-primary);
  font-size: 11px;
  cursor: pointer;
  outline: none;
}
.page-size-select:hover {
  background: var(--bg-hover);
}

/* Column visibility dropdown */
.col-vis-wrapper {
  position: relative;
}
.col-vis-menu {
  position: absolute;
  bottom: 100%;
  right: 0;
  margin-bottom: 4px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 6px;
  box-shadow: 0 4px 12px rgba(0,0,0,0.15);
  min-width: 200px;
  max-height: 320px;
  display: flex;
  flex-direction: column;
  z-index: 100;
}
.col-vis-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 10px;
  border-bottom: 1px solid var(--border);
  color: var(--text-secondary);
}
.col-vis-btn {
  padding: 2px 6px;
  font-size: 10px;
  border: 1px solid var(--border);
  border-radius: 3px;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  transition: all 0.12s;
}
.col-vis-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
.col-vis-list {
  overflow-y: auto;
  max-height: 260px;
  padding: 4px;
}
.col-vis-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-radius: 4px;
  font-size: 12px;
  color: var(--text-primary);
  cursor: pointer;
  transition: background 0.12s;
}
.col-vis-item:hover {
  background: var(--bg-hover);
}
.col-vis-item input[type="checkbox"] {
  cursor: pointer;
}
</style>
