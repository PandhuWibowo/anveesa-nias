<script setup lang="ts">
import { ref, computed } from 'vue'
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
  (e: 'sort', col: string, dir: 'asc' | 'desc'): void
  (e: 'cell-click', payload: { row: number; col: string; value: unknown }): void
  (e: 'save-row', payload: { pkValue: unknown; updates: Record<string, unknown> }): void
  (e: 'delete-row', payload: { pkValue: unknown }): void
  (e: 'add-row', payload: { values: Record<string, unknown> }): void
}>()

const sortCol = ref<string>('')
const sortDir = ref<'asc' | 'desc'>('asc')

// Cell inspector state
const inspector = ref({ show: false, column: '', value: undefined as unknown, rowIndex: 0 })

// Edit state: Map of rowIndex -> edited cells
const editedRows = ref<Map<number, Record<string, unknown>>>(new Map())
const newRowValues = ref<Record<string, unknown>>({})
const showNewRow = ref(false)

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
  if (!editedRows.value.has(rIdx)) {
    editedRows.value.set(rIdx, {})
  }
  editedRows.value.get(rIdx)![col] = target.value
}

function saveRow(rIdx: number, row: unknown[]) {
  const edits = editedRows.value.get(rIdx)
  if (!edits || !Object.keys(edits).length) return
  const pkIdx = props.columns.indexOf(props.pkColumn)
  const pkValue = pkIdx >= 0 ? row[pkIdx] : null
  emit('save-row', { pkValue, updates: edits })
  editedRows.value.delete(rIdx)
}

function cancelEdit(rIdx: number) {
  editedRows.value.delete(rIdx)
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
</script>

<template>
  <div style="display:flex;flex-direction:column;height:100%;overflow:hidden">
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
      <table class="data-table">
        <thead>
          <tr>
            <th class="col-rownum" v-if="showRowNumbers">#</th>
            <th v-if="editable" class="col-actions" style="width:80px"></th>
            <th
              v-for="col in columns"
              :key="col"
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
              :class="cellClass(val)"
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
            <td v-for="col in columns" :key="col">
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

    <!-- Pagination -->
    <div class="pagination" v-if="totalRows > 0">
      <span class="pagination__info">
        Rows {{ (page - 1) * pageSize + 1 }}–{{ Math.min(page * pageSize, totalRows) }} of {{ totalRows.toLocaleString() }}
      </span>
      <div class="pagination__spacer" />
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
.row-btns { display: flex; gap: 4px; justify-content: center; }
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
.tr-edited td { background: rgba(var(--brand-rgb, 99 102 241), 0.06) !important; }
.tr-new td { background: rgba(74, 222, 128, 0.05) !important; }
.add-row-bar {
  padding: 6px 12px;
  border-top: 1px solid var(--border);
  background: var(--bg-elevated);
}
</style>
