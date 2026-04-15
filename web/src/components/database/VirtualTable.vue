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
}

const props = withDefaults(defineProps<Props>(), {
  rowHeight: 28,
  loading: false,
  showRowNumbers: true,
  selectable: false,
})

const emit = defineEmits<{
  (e: 'cell-click', payload: { row: number; col: string; value: unknown }): void
  (e: 'bulk-delete', rows: unknown[][]): void
  (e: 'bulk-export', rows: unknown[][], format: 'csv' | 'json'): void
  (e: 'profile-column', column: string): void
}>()

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
onBeforeUnmount(() => window.removeEventListener('resize', updateSize))

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
  if (sortCol.value === col) {
    sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc'
  } else {
    sortCol.value = col
    sortDir.value = 'asc'
  }
  if (scrollEl.value) scrollEl.value.scrollTop = 0
  scrollTop.value = 0
}
</script>

<template>
  <div class="vt-root">
    <!-- Loading -->
    <div v-if="loading" class="vt-loading">
      <svg class="spin" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
      Running query…
    </div>

    <!-- Empty -->
    <div v-else-if="rows.length === 0" class="vt-empty">No rows returned.</div>

    <!-- Table -->
    <template v-else>
      <!-- Bulk action toolbar -->
      <div v-if="selectable && selectedIndices.size > 0" class="vt-bulk-bar">
        <span class="vt-bulk-count">{{ selectedIndices.size }} selected</span>
        <button class="base-btn base-btn--ghost base-btn--sm" @click="bulkExport('csv')">Export CSV</button>
        <button class="base-btn base-btn--ghost base-btn--sm" @click="bulkExport('json')">Export JSON</button>
        <button class="base-btn base-btn--ghost base-btn--sm" style="color:var(--danger)" @click="bulkDelete">Delete</button>
        <button class="base-btn base-btn--ghost base-btn--sm" @click="clearSelection">Clear</button>
      </div>

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
            @click="handleSort(col)"
          >
            {{ col }}
            <span class="vt-sort-icon">
              <template v-if="sortCol === col">{{ sortDir === 'asc' ? '↑' : '↓' }}</template>
              <template v-else>↕</template>
            </span>
            <span
              v-if="tableName"
              class="vt-profile-btn"
              title="Profile column"
              @click.stop="emit('profile-column', col)"
            >⊞</span>
          </div>
        </div>
      </div>

      <!-- Virtual body -->
      <div class="vt-body" ref="scrollEl" @scroll="onScroll">
        <div :style="{ height: totalDisplayHeight + 'px', position: 'relative' }">
          <div :style="{ paddingTop: displayPaddingTop + 'px', paddingBottom: displayPaddingBottom + 'px' }">
            <div
              v-for="(row, i) in displayVisible"
              :key="displayStart + i"
              class="vt-row"
              :class="{ 'vt-row--selected': selectable && selectedIndices.has(displayStart + i) }"
              :style="{ height: rowHeight + 'px' }"
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
                :class="cellCls(val)"
                :title="formatCell(val)"
                @click="openInspector(displayStart + i, cIdx)"
              >
                {{ formatCell(val) }}
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
        <span style="flex:1"/>
        <span style="font-size:10.5px;color:var(--text-muted)">Click cell to inspect · Click column to sort<template v-if="selectable"> · Check rows to select</template></span>
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
.vt-header {
  flex-shrink: 0;
  overflow: hidden;
  border-bottom: 1px solid var(--border);
  background: var(--bg-elevated);
}
.vt-body {
  flex: 1; min-height: 0; overflow-y: auto; overflow-x: auto;
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
}
.vt-cell--head:hover { background: var(--bg-hover); }
.vt-cell--sorted { color: var(--brand); }
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
</style>
