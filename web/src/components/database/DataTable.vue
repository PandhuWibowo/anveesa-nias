<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { downloadExcel } from '@/utils/export'
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
  addable?: boolean
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
  addable: false,
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

// Column order — tracks user-reordered sequence
const colOrder = ref<string[]>([...props.columns])

// ── Column resize — declared here so the watch below can reference it ──
const COL_MIN_W = 50
const colWidths = ref<Record<string, number>>({})
const isResizing = ref(false)

// Reset visibility + order + widths when columns prop changes
watch(() => props.columns, (newColumns) => {
  visibleColumns.value = new Set(newColumns)
  colOrder.value = [...newColumns]
  const next: Record<string, number> = {}
  for (const col of newColumns) {
    next[col] = colWidths.value[col] ?? Math.max(60, Math.min(260, col.length * 9 + 40))
  }
  colWidths.value = next
}, { immediate: true })

// Ordered, visible columns used by both thead and tbody
const orderedColumns = computed(() =>
  colOrder.value.filter(c => visibleColumns.value.has(c)),
)

// ── Column reorder via pointer events ────────────────────────────
// HTML5 drag-and-drop doesn't work on position:sticky <th> inside
// overflow:auto. We use window-level pointer listeners instead.
const dragSrcCol  = ref<string | null>(null)
const dragOverCol = ref<string | null>(null)
let _dragged = false

let _reorderSrc    = ''
let _reorderStartX = 0
let _reorderStartY = 0
let _reorderActive = false

function _reorderMove(e: PointerEvent) {
  if (!_reorderSrc) return
  if (!_reorderActive) {
    if (Math.abs(e.clientX - _reorderStartX) < 4 && Math.abs(e.clientY - _reorderStartY) < 4) return
    _reorderActive = true
  }
  const el = document.elementFromPoint(e.clientX, e.clientY)
  const th = el?.closest('[data-col]') as HTMLElement | null
  const over = th?.dataset?.col ?? null
  dragOverCol.value = (over && over !== _reorderSrc) ? over : null
}

function _reorderEnd() {
  window.removeEventListener('pointermove', _reorderMove)
  window.removeEventListener('pointerup',   _reorderEnd)
  window.removeEventListener('pointercancel', _reorderEnd)
  if (_reorderActive && dragOverCol.value && dragOverCol.value !== _reorderSrc) {
    _dragged = true
    const order = [...colOrder.value]
    const from  = order.indexOf(_reorderSrc)
    const to    = order.indexOf(dragOverCol.value)
    order.splice(from, 1)
    order.splice(to, 0, _reorderSrc)
    colOrder.value = order
  }
  _reorderSrc    = ''
  _reorderActive = false
  dragSrcCol.value  = null
  dragOverCol.value = null
  setTimeout(() => { _dragged = false }, 0)
}

function onGripPointerDown(col: string, e: PointerEvent) {
  e.preventDefault()
  e.stopPropagation()
  _reorderSrc    = col
  _reorderStartX = e.clientX
  _reorderStartY = e.clientY
  _reorderActive = false
  dragSrcCol.value = col
  window.addEventListener('pointermove',   _reorderMove)
  window.addEventListener('pointerup',     _reorderEnd)
  window.addEventListener('pointercancel', _reorderEnd)
}

// ── Column resize helpers ──────────────────────────────────────────
function colStyle(col: string): Record<string, string> {
  const w = colWidths.value[col] ?? 120
  return { width: `${w}px`, minWidth: `${w}px`, maxWidth: `${w}px` }
}

let _resizeCol = ''
let _resizeStartX = 0
let _resizeStartW = 0
let _resizeMoved = false

// Use Pointer Events + setPointerCapture so the draggable="true" on <th>
// never intercepts the move events during a resize gesture.
function startResize(col: string, e: PointerEvent) {
  e.preventDefault()
  e.stopPropagation()
  ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
  _resizeCol    = col
  _resizeStartX = e.clientX
  _resizeStartW = colWidths.value[col] ?? 120
  _resizeMoved  = false
  isResizing.value = true
}

function onResizeMove(e: PointerEvent) {
  if (!_resizeCol) return
  e.preventDefault()
  const delta = e.clientX - _resizeStartX
  if (Math.abs(delta) > 2) _resizeMoved = true
  colWidths.value = { ...colWidths.value, [_resizeCol]: Math.max(COL_MIN_W, _resizeStartW + delta) }
}

function onResizeEnd(e: PointerEvent) {
  if (!_resizeCol) return
  ;(e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId)
  _resizeCol = ''
  isResizing.value = false
  setTimeout(() => { _resizeMoved = false }, 0)
}

function autoFitCol(col: string) {
  const cIdx = props.columns.indexOf(col)
  const sample = props.rows.slice(0, 100)
  const maxLen = sample.reduce((m, row) => Math.max(m, String(row[cIdx] ?? '').length), col.length)
  colWidths.value = { ...colWidths.value, [col]: Math.max(COL_MIN_W, Math.min(480, maxLen * 8 + 24)) }
}

onBeforeUnmount(() => {
  window.removeEventListener('pointermove',   _reorderMove)
  window.removeEventListener('pointerup',     _reorderEnd)
  window.removeEventListener('pointercancel', _reorderEnd)
})

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
  if (_dragged || _resizeMoved) { _dragged = false; return }
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

function handleCellClick(rIdx: number, col: string, row: unknown[]) {
  const val = row[props.columns.indexOf(col)]
  emit('cell-click', { row: rIdx, col, value: val })
  if (!props.editable) {
    openInspector(rIdx, col, val)
  }
}

function openInspector(rIdx: number, col: string, val: unknown) {
  inspector.value = { show: true, column: col, value: val, rowIndex: rIdx }
}

function getCellValue(rIdx: number, col: string, row: unknown[]): unknown {
  return editedRows.value.get(rIdx)?.[col] ?? row[props.columns.indexOf(col)]
}

function onCellEdit(rIdx: number, col: string, event: Event) {
  const target = event.target as HTMLInputElement
  const currentVal = getCellValue(rIdx, col, props.rows[rIdx] ?? [])
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

function startAddRow() {
  if (!props.columns.length) return
  showNewRow.value = true
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
}

watch(() => props.rows, () => {
  editedRows.value.clear()
  editHistory.value = []
  newRowValues.value = {}
  showNewRow.value = false
  emit('dirty-change', false)
})

function exportToExcel() {
  const cols = orderedColumns.value
  const rows = props.rows.map(row =>
    cols.map(col => {
      const val = row[props.columns.indexOf(col)]
      if (typeof val === 'object' && val !== null) return JSON.stringify(val)
      return val
    }),
  )
  const timestamp = new Date().toISOString().slice(0, 19).replace(/[T:]/g, '-')
  downloadExcel(cols, rows, `export-${timestamp}`)
}

function handleKeydown(event: KeyboardEvent) {
  if (!props.editable) return
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'z') {
    event.preventDefault()
    undoLastEdit()
  }
}

defineExpose({ startAddRow })
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
      <button v-if="addable" class="base-btn base-btn--ghost base-btn--xs" style="margin-top:8px" @click="showNewRow=true">
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
      <div v-if="showNewRow" class="insert-toolbar">
        <div class="insert-toolbar__status">
          <span class="insert-toolbar__badge">New</span>
          <span>Fill the row values, then save to insert it.</span>
        </div>
        <div class="insert-toolbar__actions">
          <button class="base-btn base-btn--ghost base-btn--xs" @click="showNewRow=false">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--xs" @click="addRow">Save Row</button>
        </div>
      </div>
      <table class="data-table" :class="{ 'dt-resizing': isResizing }">
        <thead>
          <tr>
            <th class="col-rownum" v-if="showRowNumbers">#</th>
            <th v-if="editable || showNewRow" class="col-actions" style="width:80px;min-width:80px"></th>
            <th
              v-for="col in orderedColumns"
              :key="col"
              :data-col="col"
              :style="colStyle(col)"
              :class="{
                sorted: sortCol === col,
                'th-dragging':  dragSrcCol  === col,
                'th-drag-over': dragOverCol === col && dragSrcCol !== col,
              }"
              @click="handleSort(col)"
            >
              <span
                class="th-grip"
                title="Drag to reorder"
                @pointerdown.stop.prevent="onGripPointerDown(col, $event)"
              >
                <svg width="8" height="14" viewBox="0 0 8 14" fill="currentColor">
                  <circle cx="2" cy="2"  r="1.2"/><circle cx="6" cy="2"  r="1.2"/>
                  <circle cx="2" cy="7"  r="1.2"/><circle cx="6" cy="7"  r="1.2"/>
                  <circle cx="2" cy="12" r="1.2"/><circle cx="6" cy="12" r="1.2"/>
                </svg>
              </span>
              <span class="th-label">
                {{ col }}
                <span class="sort-icon">
                  <template v-if="sortCol === col">{{ sortDir === 'asc' ? '↑' : '↓' }}</template>
                  <template v-else>↕</template>
                </span>
              </span>
              <!-- Resize handle — pointer events bypass the th draggable system -->
              <div
                class="th-resize-handle"
                draggable="false"
                title="Drag to resize · Double-click to auto-fit"
                @pointerdown.stop.prevent="startResize(col, $event)"
                @pointermove.stop="onResizeMove($event)"
                @pointerup.stop="onResizeEnd($event)"
                @pointercancel.stop="onResizeEnd($event)"
                @dragstart.stop.prevent
                @dblclick.stop="autoFitCol(col)"
              />
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, rIdx) in rows" :key="rIdx" :class="{ 'tr-edited': editedRows.has(rIdx) }">
            <td class="col-rownum" v-if="showRowNumbers">{{ (page - 1) * pageSize + rIdx + 1 }}</td>
            <td v-if="editable || showNewRow" class="col-actions">
              <div v-if="editable" class="row-btns">
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
              v-for="col in orderedColumns"
              :key="col"
              :class="cellClass(row[columns.indexOf(col)])"
              :data-edited="isCellEdited(rIdx, col)"
              @click="handleCellClick(rIdx, col, row)"
            >
              <input
                v-if="editable"
                class="cell-input"
                :value="getCellValue(rIdx, col, row)"
                @input="onCellEdit(rIdx, col, $event)"
                @click.stop
              />
              <template v-else>{{ formatCell(row[columns.indexOf(col)]) }}</template>
            </td>
          </tr>

          <!-- New row -->
          <tr v-if="showNewRow" class="tr-new">
            <td class="col-rownum" v-if="showRowNumbers">*</td>
            <td v-if="editable || addable" class="col-actions">
              <span class="row-state-pill row-state-pill--new">New</span>
            </td>
            <td v-for="col in orderedColumns" :key="col">
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
      
      <!-- Export to Excel -->
      <button class="base-btn base-btn--ghost base-btn--xs" title="Export visible columns to Excel" @click="exportToExcel">
        <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/>
          <line x1="12" y1="18" x2="12" y2="12"/><polyline points="9 15 12 18 15 15"/>
        </svg>
        Export
      </button>

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
      <div class="pagination__spacer" />
      <button class="base-btn base-btn--ghost base-btn--xs" title="Export visible columns to Excel" @click="exportToExcel">
        <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/>
          <line x1="12" y1="18" x2="12" y2="12"/><polyline points="9 15 12 18 15 15"/>
        </svg>
        Export
      </button>
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
/* ── Column resize handle ──────────────────────────────── */
.th-resize-handle {
  position: absolute;
  right: -3px;
  top: 0;
  width: 6px;
  height: 100%;
  cursor: col-resize;
  z-index: 3;
  display: flex;
  align-items: center;
  justify-content: center;
}
.th-resize-handle::after {
  content: '';
  display: block;
  width: 2px;
  height: 60%;
  border-radius: 1px;
  background: transparent;
  transition: background 0.12s;
}
th:hover .th-resize-handle::after { background: var(--border); }
.th-resize-handle:hover::after,
.th-resize-handle:active::after { background: var(--brand) !important; }

/* Suppress text selection while resizing */
.dt-resizing { cursor: col-resize !important; }
.dt-resizing * { user-select: none !important; cursor: col-resize !important; }

/* Label span keeps text + sort icon tidy with overflow clipping */
.th-label {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  overflow: hidden;
  flex: 1;
  min-width: 0;
  white-space: nowrap;
}

/* ── Drag-and-drop column reorder ─────────────────────── */
.th-grip {
  display: inline-flex;
  align-items: center;
  margin-right: 5px;
  color: var(--text-muted);
  opacity: 0;
  cursor: grab;
  vertical-align: middle;
  transition: opacity 0.12s;
  flex-shrink: 0;
  touch-action: none;
  user-select: none;
}
th:hover .th-grip { opacity: 0.7; }
.th-dragging  .th-grip { opacity: 1; cursor: grabbing; }
.th-dragging {
  opacity: 0.35;
  cursor: grabbing;
}
.th-drag-over {
  border-left: 2px solid var(--brand) !important;
  background: rgba(var(--brand-rgb, 99 102 241), 0.1) !important;
}

.col-actions { width: 80px; min-width: 80px; text-align: center; padding: 0 6px !important; }
.row-btns { display: flex; gap: 4px; justify-content: center; align-items: center; flex-wrap: wrap; }
.row-btns--insert { gap: 6px; flex-wrap: nowrap; }
.edit-toolbar,
.insert-toolbar {
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
.insert-toolbar {
  background: color-mix(in srgb, var(--success) 7%, var(--bg-elevated));
  border-bottom-color: color-mix(in srgb, var(--success) 24%, var(--border));
}
.edit-toolbar--active {
  background: rgba(var(--brand-rgb, 99 102 241), 0.08);
}
.edit-toolbar__status,
.insert-toolbar__status {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--text-secondary);
}
.edit-toolbar__badge,
.insert-toolbar__badge,
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
.insert-toolbar__badge,
.row-state-pill--new {
  background: var(--success);
}
.edit-toolbar__actions,
.insert-toolbar__actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
.edit-toolbar__hint {
  font-size: 11px;
  color: var(--text-muted);
}
.rbtn {
  width: 26px; height: 26px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: transparent;
  cursor: pointer;
  color: var(--text-secondary);
  display: flex; align-items: center; justify-content: center;
  padding: 0;
  transition: background .12s, border-color .12s, color .12s, box-shadow .12s;
}
.rbtn--save { color: var(--success); border-color: color-mix(in srgb, var(--success) 35%, var(--border)); }
.rbtn--save:hover { background: var(--success-bg); border-color: var(--success); }
.rbtn--primary { background: var(--success); border-color: var(--success); color: white; box-shadow: 0 1px 4px rgba(34, 197, 94, .25); }
.rbtn--primary:hover { background: color-mix(in srgb, var(--success) 88%, black); color: white; }
.rbtn--cancel { color: var(--text-muted); }
.rbtn--cancel:hover { background: var(--bg-hover); color: var(--text-primary); }
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
