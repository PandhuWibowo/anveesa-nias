<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import CellInspector from '@/components/ui/CellInspector.vue'

interface Props {
  columns: string[]
  rows: unknown[][]
  rowHeight?: number
  loading?: boolean
  showRowNumbers?: boolean
  selectable?: boolean
  connId?: number | null
  tableName?: string
  editable?: boolean
  pkColumn?: string
}

const props = withDefaults(defineProps<Props>(), {
  rowHeight: 28,
  loading: false,
  showRowNumbers: true,
  selectable: false,
  editable: false,
  pkColumn: '',
})

const emit = defineEmits<{
  (e: 'cell-click', payload: { row: number; col: string; value: unknown }): void
  (e: 'bulk-delete', rows: unknown[][]): void
  (e: 'bulk-export', rows: unknown[][], format: 'csv' | 'json'): void
  (e: 'profile-column', column: string): void
  (e: 'save-row', payload: { rowIdx: number; pkValue: unknown; changes: Record<string, unknown> }): void
  (e: 'dirty-change', hasDirty: boolean): void
}>()

// ── Inline editing ────────────────────────────────────────────────────────────

interface EditingCell { rowIdx: number; colIdx: number; value: string }
const editingCell = ref<EditingCell | null>(null)
// keyed by the original row index (not display index after sort)
const dirtyRows = ref<Map<number, Record<string, unknown>>>(new Map())

function realRowIdx(displayIdx: number): number {
  // If sorted, displayRows is a sorted copy; map back to original props.rows index
  if (!sortCol.value) return displayIdx
  const row = displayRows.value[displayIdx]
  return props.rows.indexOf(row)
}

function startEdit(displayIdx: number, colIdx: number, currentVal: unknown) {
  if (!props.editable) return
  editingCell.value = { rowIdx: displayIdx, colIdx, value: String(currentVal ?? '') }
}

function commitEdit() {
  if (!editingCell.value) return
  const { rowIdx, colIdx, value } = editingCell.value
  const col = props.columns[colIdx]
  const origIdx = realRowIdx(rowIdx)
  if (!dirtyRows.value.has(origIdx)) dirtyRows.value.set(origIdx, {})
  dirtyRows.value.get(origIdx)![col] = value
  dirtyRows.value = new Map(dirtyRows.value) // trigger reactivity
  editingCell.value = null
  emit('dirty-change', dirtyRows.value.size > 0)
}

function discardRow(displayIdx: number) {
  const origIdx = realRowIdx(displayIdx)
  dirtyRows.value.delete(origIdx)
  dirtyRows.value = new Map(dirtyRows.value)
  emit('dirty-change', dirtyRows.value.size > 0)
}

function saveRow(displayIdx: number) {
  const origIdx = realRowIdx(displayIdx)
  const row = props.rows[origIdx]
  const pkIdx = props.pkColumn ? props.columns.indexOf(props.pkColumn) : 0
  const pkValue = pkIdx >= 0 ? row[pkIdx] : row[0]
  const changes = dirtyRows.value.get(origIdx) ?? {}
  emit('save-row', { rowIdx: origIdx, pkValue, changes })
  dirtyRows.value.delete(origIdx)
  dirtyRows.value = new Map(dirtyRows.value)
  emit('dirty-change', dirtyRows.value.size > 0)
}

function saveAllRows() {
  for (const [origIdx] of dirtyRows.value) {
    const displayIdx = sortCol.value
      ? displayRows.value.findIndex(r => props.rows.indexOf(r) === origIdx)
      : origIdx
    saveRow(displayIdx >= 0 ? displayIdx : origIdx)
  }
}

function discardAllRows() {
  dirtyRows.value = new Map()
  emit('dirty-change', false)
}

// Display value: prefer dirty edit over original
function displayVal(displayIdx: number, colIdx: number, origVal: unknown): unknown {
  const origIdx = realRowIdx(displayIdx)
  const dirty = dirtyRows.value.get(origIdx)
  if (dirty && props.columns[colIdx] in dirty) return dirty[props.columns[colIdx]]
  return origVal
}

// Clear dirty state when rows prop changes (new query result)
watch(() => props.rows, () => {
  dirtyRows.value = new Map()
  editingCell.value = null
})

// Multi-row selection
const selectedIndices = ref(new Set<number>())
const allSelected = computed(() => props.rows.length > 0 && selectedIndices.value.size === props.rows.length)

function toggleSelectAll() {
  if (allSelected.value) {
    selectedIndices.value = new Set()
  } else {
    selectedIndices.value = new Set(props.rows.map((_, i) => i))
  }
}

function toggleRow(i: number) {
  const idx = displayStart.value + i
  const next = new Set(selectedIndices.value)
  if (next.has(idx)) next.delete(idx)
  else next.add(idx)
  selectedIndices.value = next
}

const selectedRows = computed(() =>
  [...selectedIndices.value].map((i) => props.rows[i]).filter(Boolean),
)

function clearSelection() { selectedIndices.value = new Set() }

function bulkDelete() {
  emit('bulk-delete', selectedRows.value)
  clearSelection()
}

function bulkExport(format: 'csv' | 'json') {
  emit('bulk-export', selectedRows.value, format)
  clearSelection()
}

const BUFFER = 15
const scrollEl = ref<HTMLElement>()
const scrollTop = ref(0)
const viewHeight = ref(400)

const inspector = ref({ show: false, column: '', value: undefined as unknown })

const totalHeight = computed(() => props.rows.length * props.rowHeight)

const startIdx = computed(() =>
  Math.max(0, Math.floor(scrollTop.value / props.rowHeight) - BUFFER),
)
const endIdx = computed(() =>
  Math.min(props.rows.length, Math.ceil((scrollTop.value + viewHeight.value) / props.rowHeight) + BUFFER),
)

const paddingTop = computed(() => startIdx.value * props.rowHeight)
const paddingBottom = computed(() =>
  Math.max(0, (props.rows.length - endIdx.value) * props.rowHeight),
)
const visibleRows = computed(() => props.rows.slice(startIdx.value, endIdx.value))

function onScroll(e: Event) {
  scrollTop.value = (e.target as HTMLElement).scrollTop
}

function updateSize() {
  if (scrollEl.value) viewHeight.value = scrollEl.value.clientHeight
}

onMounted(() => {
  updateSize()
  window.addEventListener('resize', updateSize)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', updateSize)
  // pointer capture releases automatically; no manual cleanup needed
})

watch(() => props.rows, () => {
  if (scrollEl.value) scrollEl.value.scrollTop = 0
  scrollTop.value = 0
})

function formatCell(val: unknown): string {
  if (val === null || val === undefined) return 'NULL'
  if (typeof val === 'object') return JSON.stringify(val)
  return String(val)
}

function cellCls(val: unknown): string {
  if (val === null || val === undefined) return 'vt-null'
  if (typeof val === 'number') return 'vt-num'
  if (typeof val === 'boolean') return 'vt-bool'
  return ''
}

function openInspector(rIdx: number, cIdx: number) {
  const val = props.rows[rIdx]?.[cIdx]
  const col = props.columns[cIdx]
  inspector.value = { show: true, column: col, value: val }
  emit('cell-click', { row: rIdx, col, value: val })
}

// Sort
const sortCol = ref('')
const sortDir = ref<'asc' | 'desc'>('asc')

function sortedRows() {
  if (!sortCol.value) return props.rows
  const cIdx = props.columns.indexOf(sortCol.value)
  if (cIdx < 0) return props.rows
  return [...props.rows].sort((a, b) => {
    const av = a[cIdx] as any
    const bv = b[cIdx] as any
    if (av === null) return 1
    if (bv === null) return -1
    const cmp = av < bv ? -1 : av > bv ? 1 : 0
    return sortDir.value === 'asc' ? cmp : -cmp
  })
}

const displayRows = computed(() => sortedRows())
const totalDisplayHeight = computed(() => displayRows.value.length * props.rowHeight)
const displayStart = computed(() =>
  Math.max(0, Math.floor(scrollTop.value / props.rowHeight) - BUFFER),
)
const displayEnd = computed(() =>
  Math.min(displayRows.value.length, Math.ceil((scrollTop.value + viewHeight.value) / props.rowHeight) + BUFFER),
)
const displayPaddingTop = computed(() => displayStart.value * props.rowHeight)
const displayPaddingBottom = computed(() =>
  Math.max(0, (displayRows.value.length - displayEnd.value) * props.rowHeight),
)
const displayVisible = computed(() => displayRows.value.slice(displayStart.value, displayEnd.value))

function handleSort(col: string) {
  if (_resizeMoved) return   // don't sort after a real resize drag
  if (sortCol.value === col) {
    sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc'
  } else {
    sortCol.value = col
    sortDir.value = 'asc'
  }
  if (scrollEl.value) scrollEl.value.scrollTop = 0
  scrollTop.value = 0
}

// ── Column resize ─────────────────────────────────────────────────
const COL_MIN_W = 50
const COL_DEFAULT_W = 120

const colWidths = ref<Record<string, number>>({})
const isResizing = ref(false)

watch(() => props.columns, (cols) => {
  const next: Record<string, number> = {}
  for (const col of cols) {
    // keep existing width, or init from column name length
    next[col] = colWidths.value[col] ?? Math.max(COL_MIN_W, Math.min(260, col.length * 9 + 40))
  }
  colWidths.value = next
}, { immediate: true })

function colStyle(col: string): Record<string, string> {
  const w = colWidths.value[col] ?? COL_DEFAULT_W
  return { flex: 'none', width: `${w}px`, minWidth: `${w}px`, maxWidth: `${w}px` }
}

let _resizeCol = ''
let _resizeStartX = 0
let _resizeStartW = 0
let _resizeMoved = false

// Use Pointer Events + setPointerCapture to bypass any drag interception
function startResize(col: string, e: PointerEvent) {
  e.preventDefault()
  e.stopPropagation()
  ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
  _resizeCol    = col
  _resizeStartX = e.clientX
  _resizeStartW = colWidths.value[col] ?? COL_DEFAULT_W
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

// double-click handle → auto-fit width from visible data
function autoFitCol(col: string) {
  const cIdx = props.columns.indexOf(col)
  const sample = displayRows.value.slice(0, 100)
  const maxLen = sample.reduce((m, row) => Math.max(m, String(row[cIdx] ?? '').length), col.length)
  colWidths.value = { ...colWidths.value, [col]: Math.max(COL_MIN_W, Math.min(480, maxLen * 8 + 24)) }
}
</script>

<template>
  <div class="vt-root" :class="{ 'vt-resizing': isResizing }">
    <!-- Loading -->
    <div v-if="loading" class="vt-loading">
      <svg class="spin" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
      Running query…
    </div>

    <!-- Empty -->
    <div v-else-if="rows.length === 0" class="vt-empty">No rows returned.</div>

    <!-- Table -->
    <template v-else>
      <!-- Edit-mode banner -->
      <div v-if="editable" class="vt-edit-banner">
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
        Edit mode — double-click any cell to edit
        <template v-if="dirtyRows.size > 0">
          · <strong>{{ dirtyRows.size }} unsaved row{{ dirtyRows.size > 1 ? 's' : '' }}</strong>
          <button class="vt-edit-save-all" @click="saveAllRows">Save all</button>
          <button class="vt-edit-discard-all" @click="discardAllRows">Discard all</button>
        </template>
      </div>

      <!-- Bulk action toolbar -->
      <div v-if="selectable && selectedIndices.size > 0" class="vt-bulk-bar">
        <span class="vt-bulk-count">{{ selectedIndices.size }} selected</span>
        <button class="base-btn base-btn--ghost base-btn--sm" @click="bulkExport('csv')">Export CSV</button>
        <button class="base-btn base-btn--ghost base-btn--sm" @click="bulkExport('json')">Export JSON</button>
        <button class="base-btn base-btn--ghost base-btn--sm" style="color:var(--danger)" @click="bulkDelete">Delete</button>
        <button class="base-btn base-btn--ghost base-btn--sm" @click="clearSelection">Clear</button>
      </div>

      <!-- Single scroll container: header sticky inside, body virtual-scrolls below -->
      <div class="vt-scroll-wrap" ref="scrollEl" @scroll="onScroll">
        <!-- Sticky header -->
        <div class="vt-header">
          <div class="vt-row vt-row--head">
            <div v-if="selectable" class="vt-cell vt-cell--check vt-cell--head">
              <input type="checkbox" :checked="allSelected" @change="toggleSelectAll" />
            </div>
            <div v-if="showRowNumbers" class="vt-cell vt-cell--rownum vt-cell--head">#</div>
            <div
              v-for="col in columns"
              :key="col"
              class="vt-cell vt-cell--head"
              :class="{ 'vt-cell--sorted': sortCol === col }"
              :style="colStyle(col)"
              @click="handleSort(col)"
            >
              <span class="vt-head-label">
                {{ col }}
                <span class="vt-sort-icon">
                  <template v-if="sortCol === col">{{ sortDir === 'asc' ? '↑' : '↓' }}</template>
                  <template v-else>↕</template>
                </span>
              </span>
              <span
                v-if="tableName"
                class="vt-profile-btn"
                title="Profile column"
                @click.stop="emit('profile-column', col)"
              >⊞</span>
              <!-- Resize handle — pointer events bypass header drag interception -->
              <div
                class="vt-resize-handle"
                draggable="false"
                title="Drag to resize · Double-click to auto-fit"
                @pointerdown.stop.prevent="startResize(col, $event)"
                @pointermove.stop="onResizeMove($event)"
                @pointerup.stop="onResizeEnd($event)"
                @pointercancel.stop="onResizeEnd($event)"
                @dragstart.stop.prevent
                @dblclick.stop="autoFitCol(col)"
              />
            </div>
          </div>
        </div>

        <!-- Virtual body -->
        <div :style="{ height: totalDisplayHeight + 'px', position: 'relative' }">
          <div :style="{ paddingTop: displayPaddingTop + 'px', paddingBottom: displayPaddingBottom + 'px' }">
            <div
              v-for="(row, i) in displayVisible"
              :key="displayStart + i"
              class="vt-row"
              :class="{
                'vt-row--selected': selectable && selectedIndices.has(displayStart + i),
                'vt-row--dirty': editable && dirtyRows.has(realRowIdx(displayStart + i)),
              }"
              :style="{ height: editable && dirtyRows.has(realRowIdx(displayStart + i)) ? 'auto' : rowHeight + 'px', minHeight: rowHeight + 'px' }"
            >
              <div v-if="selectable" class="vt-cell vt-cell--check" @click.stop>
                <input type="checkbox" :checked="selectedIndices.has(displayStart + i)" @change="toggleRow(i)" />
              </div>
              <div v-if="showRowNumbers" class="vt-cell vt-cell--rownum vt-cell--dim">
                {{ displayStart + i + 1 }}
              </div>
              <div
                v-for="(val, cIdx) in row"
                :key="cIdx"
                class="vt-cell"
                :class="[
                  cellCls(displayVal(displayStart + i, cIdx, val)),
                  editable && dirtyRows.has(realRowIdx(displayStart + i)) && columns[cIdx] in (dirtyRows.get(realRowIdx(displayStart + i)) ?? {}) ? 'vt-cell--edited' : '',
                ]"
                :style="colStyle(columns[cIdx])"
                :title="editable ? 'Double-click to edit' : formatCell(val)"
                @click="editable ? undefined : openInspector(displayStart + i, cIdx)"
                @dblclick="editable ? startEdit(displayStart + i, cIdx, displayVal(displayStart + i, cIdx, val)) : undefined"
              >
                <!-- Editing input -->
                <input
                  v-if="editable && editingCell?.rowIdx === displayStart + i && editingCell?.colIdx === cIdx"
                  class="vt-edit-input"
                  :value="editingCell.value"
                  @input="editingCell!.value = ($event.target as HTMLInputElement).value"
                  @blur="commitEdit"
                  @keydown.enter.prevent="commitEdit"
                  @keydown.escape.prevent="editingCell = null"
                  @click.stop
                  autofocus
                />
                <template v-else>{{ formatCell(displayVal(displayStart + i, cIdx, val)) }}</template>
              </div>
              <!-- Row save/discard actions (shown when row is dirty) -->
              <div
                v-if="editable && dirtyRows.has(realRowIdx(displayStart + i))"
                class="vt-cell vt-row-actions"
                @click.stop
              >
                <button class="vt-row-save" @click="saveRow(displayStart + i)">Save</button>
                <button class="vt-row-discard" @click="discardRow(displayStart + i)">✕</button>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Footer info -->
      <div class="vt-footer">
        <span>{{ rows.length.toLocaleString() }} rows</span>
        <span v-if="selectable && selectedIndices.size > 0"> · <strong>{{ selectedIndices.size }}</strong> selected</span>
        <span v-if="sortCol"> · sorted by <strong>{{ sortCol }}</strong> {{ sortDir }}</span>
        <span v-if="editable && dirtyRows.size > 0" style="color:var(--warning)"> · {{ dirtyRows.size }} unsaved</span>
        <span style="flex:1"/>
        <span style="font-size:10.5px;color:var(--text-muted)">
          <template v-if="editable">Double-click cell to edit · Enter/blur to confirm · Esc to cancel</template>
          <template v-else>Click cell to inspect · Click column to sort<template v-if="selectable"> · Check rows to select</template></template>
        </span>
      </div>
    </template>

    <CellInspector
      :show="inspector.show"
      :column="inspector.column"
      :value="inspector.value"
      @close="inspector.show = false"
    />
  </div>
</template>

<style scoped>
.vt-root {
  display: flex; flex-direction: column;
  height: 100%; width: 100%; overflow: hidden;
  font-size: 12.5px;
}
.vt-loading, .vt-empty {
  flex: 1; display: flex; align-items: center; justify-content: center;
  gap: 8px; color: var(--text-muted);
}
/* Single container handles both axes — header is sticky inside it */
.vt-scroll-wrap {
  flex: 1;
  min-height: 0;
  overflow: auto;       /* scroll both axes here */
}
.vt-header {
  position: sticky;
  top: 0;
  z-index: 2;
  border-bottom: 1px solid var(--border);
  background: var(--bg-elevated);
  /* min-width mirrors the row width so it never shrinks below content */
  min-width: max-content;
}
.vt-footer {
  flex-shrink: 0; display: flex; align-items: center; gap: 8px;
  padding: 5px 12px;
  font-size: 11px; color: var(--text-muted);
  border-top: 1px solid var(--border);
  background: var(--bg-elevated);
}
.vt-row {
  display: flex; align-items: stretch;
  border-bottom: 1px solid var(--border);
  transition: background 0.1s;
  min-width: max-content;
}
.vt-row:hover { background: var(--bg-hover); }
.vt-row--head { cursor: default; }
.vt-row--head:hover { background: transparent; }
.vt-cell {
  flex: 1; min-width: 90px; max-width: 320px;
  padding: 0 10px;
  display: flex; align-items: center;
  overflow: hidden;
  white-space: nowrap; text-overflow: ellipsis;
  color: var(--text-primary);
  cursor: pointer;
  border-right: 1px solid var(--border);
  font-family: var(--mono, monospace);
  font-size: 12px;
  user-select: text;
}
.vt-cell:last-child { border-right: none; }
.vt-cell--check { min-width: 36px; max-width: 36px; flex: none; justify-content: center; cursor: default; }
.vt-row--selected { background: rgba(99,102,241,0.1) !important; }
.vt-bulk-bar {
  flex-shrink: 0; display: flex; align-items: center; gap: 8px;
  padding: 6px 12px; background: rgba(99,102,241,0.1);
  border-bottom: 1px solid rgba(99,102,241,0.3);
}
.vt-bulk-count { font-size: 12px; font-weight: 700; color: var(--brand); }
.vt-profile-btn {
  font-size: 10px; opacity: 0; cursor: pointer; color: var(--brand);
  margin-left: 4px; padding: 1px 3px; border-radius: 3px;
  transition: opacity 0.1s;
}
.vt-cell--head:hover .vt-profile-btn { opacity: 1; }
.vt-cell--head {
  cursor: pointer;
  font-family: inherit;
  font-weight: 600; font-size: 11px;
  text-transform: uppercase; letter-spacing: 0.3px;
  color: var(--text-muted);
  background: var(--bg-elevated);
  justify-content: space-between;
  position: relative;         /* anchor the resize handle */
  user-select: none;
  overflow: visible;          /* let handle bleed outside */
}
.vt-cell--head:hover { background: var(--bg-hover); }
.vt-cell--sorted { color: var(--brand); }

/* Resize handle — 6 px zone on the right edge of each header cell */
.vt-resize-handle {
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
.vt-resize-handle::after {
  content: '';
  display: block;
  width: 2px;
  height: 60%;
  border-radius: 1px;
  background: transparent;
  transition: background 0.12s;
}
.vt-cell--head:hover .vt-resize-handle::after { background: var(--border); }
.vt-resize-handle:hover::after,
.vt-resize-handle:active::after { background: var(--brand) !important; }

/* Suppress text selection and pointer cursor while actively resizing */
.vt-resizing { cursor: col-resize !important; }
.vt-resizing * { user-select: none !important; cursor: col-resize !important; }

/* Label inside header keeps flex layout tidy */
.vt-head-label {
  display: flex;
  align-items: center;
  gap: 3px;
  overflow: hidden;
  flex: 1;
  min-width: 0;
}
.vt-cell--rownum {
  min-width: 44px; max-width: 44px;
  flex: none; font-variant-numeric: tabular-nums;
  justify-content: flex-end;
  color: var(--text-muted);
}
.vt-cell--dim { color: var(--text-muted); }
.vt-null { color: var(--text-muted); font-style: italic; }
.vt-num { color: #60a5fa; justify-content: flex-end; }
.vt-bool { color: #c084fc; }
.vt-sort-icon { font-size: 10px; margin-left: 4px; opacity: 0.6; }

/* Edit mode */
.vt-edit-banner {
  display: flex; align-items: center; gap: 8px; flex-shrink: 0;
  padding: 5px 12px; font-size: 12px; font-weight: 500;
  background: color-mix(in srgb, var(--accent) 8%, transparent);
  border-bottom: 1px solid color-mix(in srgb, var(--accent) 30%, transparent);
  color: var(--accent);
}
.vt-edit-save-all {
  padding: 2px 10px; font-size: 11.5px; font-weight: 600;
  background: var(--accent); color: #fff; border: none;
  border-radius: 5px; cursor: pointer;
}
.vt-edit-discard-all {
  padding: 2px 10px; font-size: 11.5px; font-weight: 600;
  background: none; color: var(--text-secondary);
  border: 1px solid var(--border); border-radius: 5px; cursor: pointer;
}
.vt-row--dirty { background: color-mix(in srgb, #f59e0b 6%, transparent) !important; }
.vt-row--dirty:hover { background: color-mix(in srgb, #f59e0b 10%, transparent) !important; }
.vt-cell--edited {
  background: color-mix(in srgb, #f59e0b 12%, transparent);
  color: #d97706 !important;
  font-weight: 600;
}
.vt-edit-input {
  width: 100%; height: 100%; border: none; outline: none;
  background: var(--bg);
  color: var(--text);
  font-size: inherit; font-family: inherit;
  padding: 0 4px;
  border-radius: 3px;
  box-shadow: 0 0 0 2px var(--accent);
}
.vt-row-actions {
  display: flex; align-items: center; gap: 4px;
  flex: none; width: auto; min-width: auto; max-width: none;
  padding: 0 8px;
}
.vt-row-save {
  padding: 2px 8px; font-size: 11px; font-weight: 700;
  background: var(--accent); color: #fff; border: none;
  border-radius: 4px; cursor: pointer; white-space: nowrap;
}
.vt-row-discard {
  padding: 2px 6px; font-size: 11px;
  background: none; color: var(--text-secondary);
  border: 1px solid var(--border); border-radius: 4px; cursor: pointer;
}
</style>
