<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'

const props = defineProps<{ activeConnId?: number | null }>()

interface ERColumn { name: string; data_type: string; is_primary_key: boolean; is_nullable: boolean }
interface ERTable  { name: string; type: string; columns: ERColumn[] }
interface FK       { constraint_name: string; table_name: string; column_name: string; ref_table_name: string; ref_column_name: string }
interface ERData   { tables: ERTable[]; foreign_keys: FK[] }
interface LayoutTable extends ERTable { x: number; y: number; width: number; height: number }

const { connections } = useConnections()
const activeConn = computed(() =>
  props.activeConnId
    ? connections.value.find((c) => c.id === props.activeConnId)
    : connections.value[0] ?? null,
)

const selectedDb    = ref('')
const erData        = ref<ERData | null>(null)
const loading       = ref(false)
const error         = ref('')
const selectedTableName = ref('')
const tableFilter   = ref('')
const pathFrom      = ref('')
const pathTo        = ref('')
const compactMode   = ref(true)
const sidepanelOpen = ref(true)

watch(activeConn, (c) => { if (c?.database) selectedDb.value = c.database }, { immediate: true })
watch([() => activeConn.value, selectedDb], ([conn, db]) => { if (conn && db) fetchER(conn.id!, db) }, { immediate: true })
watch(compactMode, () => { computeLayout() })

async function fetchER(connId: number, db: string, refresh = false) {
  loading.value = true; error.value = ''; erData.value = null
  try {
    const enc  = encodeURIComponent(db)
    const path = enc ? `/api/connections/${connId}/er/${enc}` : `/api/connections/${connId}/er`
    const { data } = await axios.get<ERData>(path, { params: refresh ? { refresh: 1 } : undefined })
    erData.value = data
    selectedTableName.value = data.tables[0]?.name ?? ''
    computeLayout()
  } catch (e: unknown) {
    error.value = (e as { response?: { data?: { error?: string } } })?.response?.data?.error ?? 'Failed to load ER diagram'
  } finally { loading.value = false }
}

// ── Layout ────────────────────────────────────────────────────────
const TABLE_W  = 240
const ROW_H    = 24
const HEADER_H = 40
const GAP_X    = 80
const GAP_Y    = 60

const layout = ref<LayoutTable[]>([])

const filteredTableNames = computed(() => {
  if (!erData.value) return []
  const q = tableFilter.value.trim().toLowerCase()
  return q
    ? erData.value.tables.filter((t) => t.name.toLowerCase().includes(q)).map((t) => t.name)
    : erData.value.tables.map((t) => t.name)
})

const visibleLayout = computed(() => layout.value.filter((t) => filteredTableNames.value.includes(t.name)))
const selectedTable = computed(() => erData.value?.tables.find((t) => t.name === selectedTableName.value) ?? null)
const selectedTableRelCount = computed(() => {
  if (!erData.value || !selectedTable.value) return 0
  return erData.value.foreign_keys.filter(
    (fk) => fk.table_name === selectedTable.value!.name || fk.ref_table_name === selectedTable.value!.name,
  ).length
})

const joinPath = computed((): FK[] => {
  if (!erData.value || !pathFrom.value || !pathTo.value || pathFrom.value === pathTo.value) return []
  const edges = erData.value.foreign_keys
  const queue = [pathFrom.value]
  const prev  = new Map<string, { table: string; edge: FK }>()
  const seen  = new Set([pathFrom.value])
  while (queue.length) {
    const cur = queue.shift()!
    if (cur === pathTo.value) break
    for (const edge of edges) {
      const ns: Array<{ next: string; edge: FK }> = []
      if (edge.table_name === cur)     ns.push({ next: edge.ref_table_name, edge })
      if (edge.ref_table_name === cur) ns.push({ next: edge.table_name, edge })
      for (const n of ns) {
        if (!seen.has(n.next)) { seen.add(n.next); prev.set(n.next, { table: cur, edge: n.edge }); queue.push(n.next) }
      }
    }
  }
  if (!prev.has(pathTo.value)) return []
  const path: FK[] = []; let cursor = pathTo.value
  while (cursor !== pathFrom.value) { const item = prev.get(cursor)!; path.unshift(item.edge); cursor = item.table }
  return path
})

const highlightedTableNames = computed(() => {
  const s = new Set<string>()
  if (selectedTableName.value) s.add(selectedTableName.value)
  for (const e of joinPath.value) { s.add(e.table_name); s.add(e.ref_table_name) }
  return s
})
const highlightedArrowKeys = computed(() => new Set(joinPath.value.map((e) => e.constraint_name)))

function tableHeight(t: ERTable) {
  return compactMode.value ? HEADER_H + 40 : HEADER_H + Math.min(t.columns.length, 20) * ROW_H + 10
}

function computeLayout() {
  if (!erData.value) return
  const tables = erData.value.tables
  const COLS   = Math.max(1, Math.ceil(Math.sqrt(tables.length * 1.6)))
  const heights = tables.map((t) => tableHeight(t))
  const rowMaxH: number[] = []
  tables.forEach((_, i) => {
    const row = Math.floor(i / COLS)
    rowMaxH[row] = Math.max(rowMaxH[row] ?? 0, heights[i])
  })
  const rowY: number[] = [40]
  for (let r = 1; r < rowMaxH.length; r++) {
    rowY[r] = rowY[r - 1] + rowMaxH[r - 1] + GAP_Y
  }
  layout.value = tables.map((t, i) => ({
    ...t,
    x:      (i % COLS) * (TABLE_W + GAP_X) + 40,
    y:      rowY[Math.floor(i / COLS)],
    width:  TABLE_W,
    height: heights[i],
  }))
}

// ── World bounds for SVG arrow layer ─────────────────────────────
const worldW = computed(() =>
  visibleLayout.value.length ? Math.max(...visibleLayout.value.map((t) => t.x + t.width)) + 120 : 2000,
)
const worldH = computed(() =>
  visibleLayout.value.length ? Math.max(...visibleLayout.value.map((t) => t.y + t.height)) + 120 : 2000,
)

// ── FK arrows ─────────────────────────────────────────────────────
interface Arrow { path: string; key: string }
const arrows = computed<Arrow[]>(() => {
  if (!erData.value || !visibleLayout.value.length) return []
  const dp = dragPos.value
  const lmap = new Map(visibleLayout.value.map((t) =>
    [t.name, dp && t.name === dp.name ? { ...t, x: dp.x, y: dp.y } : t] as [string, LayoutTable],
  ))
  return erData.value.foreign_keys.flatMap((fk) => {
    const src = lmap.get(fk.table_name)
    const dst = lmap.get(fk.ref_table_name)
    if (!src || !dst || src === dst) return []

    const srcIdx = src.columns.findIndex((c) => c.name === fk.column_name)
    const dstIdx = dst.columns.findIndex((c) => c.name === fk.ref_column_name)
    const srcY = compactMode.value ? src.y + HEADER_H / 2 : src.y + HEADER_H + (Math.max(0, srcIdx) + 0.5) * ROW_H
    const dstY = compactMode.value ? dst.y + HEADER_H / 2 : dst.y + HEADER_H + (Math.max(0, dstIdx) + 0.5) * ROW_H

    let x1: number, x2: number
    if (src.x + src.width <= dst.x)      { x1 = src.x + src.width; x2 = dst.x }
    else if (dst.x + dst.width <= src.x) { x1 = src.x;             x2 = dst.x + dst.width }
    else                                  { x1 = src.x + src.width; x2 = dst.x }

    const dx = Math.max(40, Math.abs(x2 - x1) * 0.45)
    return [{ key: fk.constraint_name, path: `M ${x1} ${srcY} C ${x1 + dx} ${srcY}, ${x2 - dx} ${dstY}, ${x2} ${dstY}` }]
  })
})

const fkByKey = computed(() =>
  new Map(erData.value?.foreign_keys.map((fk) => [fk.constraint_name, fk]) ?? []),
)

// ── Pan / zoom ────────────────────────────────────────────────────
const panX      = ref(0)
const panY      = ref(0)
const scale     = ref(1)
const isPanning = ref(false)
const canvasEl  = ref<HTMLElement>()

let _lastX = 0, _lastY = 0

// Per-table drag — direct DOM to bypass Vue reactivity during move
let _dragging: LayoutTable | null = null
let _dragOffX = 0, _dragOffY = 0, _dragX = 0, _dragY = 0
let _rafId: number | null = null
const _tableEls: Record<string, HTMLElement> = {}

// Reactive drag position — lets arrows recompute each rAF without touching layout
const dragPos = ref<{ name: string; x: number; y: number } | null>(null)

// Touch state for pinch-zoom and touch-drag
let _touches: Touch[] = []
let _pinchDist = 0

function setTableRef(el: Element | null, name: string) {
  if (el) _tableEls[name] = el as HTMLElement
  else    delete _tableEls[name]
}

function onTableHeaderDown(e: MouseEvent, t: LayoutTable) {
  e.stopPropagation(); e.preventDefault()
  _dragging  = t
  _dragOffX  = (e.clientX - panX.value) / scale.value - t.x
  _dragOffY  = (e.clientY - panY.value) / scale.value - t.y
  _dragX = t.x; _dragY = t.y
}

function onWheel(e: WheelEvent) {
  e.preventDefault()
  const rect = canvasEl.value!.getBoundingClientRect()
  const mx   = e.clientX - rect.left
  const my   = e.clientY - rect.top
  const factor = e.deltaY < 0 ? 1.12 : 1 / 1.12
  const ns     = Math.min(3, Math.max(0.08, scale.value * factor))
  panX.value   = mx - (mx - panX.value) * (ns / scale.value)
  panY.value   = my - (my - panY.value) * (ns / scale.value)
  scale.value  = ns
}

function onCanvasDown(e: MouseEvent) {
  if ((e.target as Element).closest('.erd-table')) return
  isPanning.value = true
  _lastX = e.clientX; _lastY = e.clientY
  e.preventDefault()
}

function onMousemove(e: MouseEvent) {
  if (_dragging) {
    if (_rafId !== null) return
    const cx = e.clientX, cy = e.clientY
    _rafId = requestAnimationFrame(() => {
      _rafId = null
      if (!_dragging) return
      _dragX = (cx - panX.value) / scale.value - _dragOffX
      _dragY = (cy - panY.value) / scale.value - _dragOffY
      const el = _tableEls[_dragging.name]
      if (el) { el.style.left = `${_dragX}px`; el.style.top = `${_dragY}px` }
      dragPos.value = { name: _dragging.name, x: _dragX, y: _dragY }
    })
    return
  }
  if (!isPanning.value) return
  panX.value += e.clientX - _lastX
  panY.value += e.clientY - _lastY
  _lastX = e.clientX; _lastY = e.clientY
}

function onMouseup() {
  if (_dragging) { _dragging.x = _dragX; _dragging.y = _dragY; _dragging = null; dragPos.value = null }
  isPanning.value = false
}

function resetView() { panX.value = 0; panY.value = 0; scale.value = 1 }
function selectTable(name: string) { selectedTableName.value = name }

const zoomPct = computed(() => `${Math.round(scale.value * 100)}%`)

// ── Touch support ─────────────────────────────────────────────────
function _pinchDistance(a: Touch, b: Touch) {
  return Math.hypot(a.clientX - b.clientX, a.clientY - b.clientY)
}

function onTableHeaderTouchDown(e: TouchEvent, t: LayoutTable) {
  e.stopPropagation()
  if (e.touches.length !== 1) return
  const touch = e.touches[0]
  _dragging = t
  _dragOffX = (touch.clientX - panX.value) / scale.value - t.x
  _dragOffY = (touch.clientY - panY.value) / scale.value - t.y
  _dragX = t.x; _dragY = t.y
}

function onCanvasTouchStart(e: TouchEvent) {
  _touches = Array.from(e.touches)
  if (_touches.length === 2) {
    _pinchDist = _pinchDistance(_touches[0], _touches[1])
    isPanning.value = false
  } else if (_touches.length === 1 && !(_touches[0].target as Element).closest('.erd-table')) {
    isPanning.value = true
    _lastX = _touches[0].clientX; _lastY = _touches[0].clientY
  }
}

function onTouchMove(e: TouchEvent) {
  e.preventDefault()
  const ts = Array.from(e.touches)
  if (ts.length === 2) {
    const dist = _pinchDistance(ts[0], ts[1])
    const factor = dist / _pinchDist
    _pinchDist = dist
    const rect = canvasEl.value!.getBoundingClientRect()
    const mx = (ts[0].clientX + ts[1].clientX) / 2 - rect.left
    const my = (ts[0].clientY + ts[1].clientY) / 2 - rect.top
    const ns = Math.min(3, Math.max(0.08, scale.value * factor))
    panX.value = mx - (mx - panX.value) * (ns / scale.value)
    panY.value = my - (my - panY.value) * (ns / scale.value)
    scale.value = ns
  } else if (ts.length === 1 && _dragging) {
    if (_rafId !== null) return
    const cx = ts[0].clientX, cy = ts[0].clientY
    _rafId = requestAnimationFrame(() => {
      _rafId = null
      if (!_dragging) return
      _dragX = (cx - panX.value) / scale.value - _dragOffX
      _dragY = (cy - panY.value) / scale.value - _dragOffY
      const el = _tableEls[_dragging.name]
      if (el) { el.style.left = `${_dragX}px`; el.style.top = `${_dragY}px` }
      dragPos.value = { name: _dragging.name, x: _dragX, y: _dragY }
    })
  } else if (ts.length === 1 && isPanning.value) {
    panX.value += ts[0].clientX - _lastX
    panY.value += ts[0].clientY - _lastY
    _lastX = ts[0].clientX; _lastY = ts[0].clientY
  }
  _touches = ts
}

function onTouchEnd(e: TouchEvent) {
  if (_dragging && e.touches.length < 2) {
    _dragging.x = _dragX; _dragging.y = _dragY; _dragging = null; dragPos.value = null
  }
  if (e.touches.length === 0) isPanning.value = false
  _touches = Array.from(e.touches)
}

// Viewport culling: only render tables inside (viewport + generous pad)
const viewportTables = computed(() => {
  const px = panX.value, py = panY.value, sc = scale.value
  if (!canvasEl.value) return visibleLayout.value
  const { width, height } = canvasEl.value.getBoundingClientRect()
  const pad = 320 / sc
  const l = -px / sc - pad, t = -py / sc - pad
  const r = l + width / sc + pad * 2, b = t + height / sc + pad * 2
  return visibleLayout.value.filter((tbl) =>
    tbl.x < r && tbl.x + tbl.width > l && tbl.y < b && tbl.y + tbl.height > t,
  )
})

// Only draw arrows where at least one endpoint is in viewport (or highlighted)
const visibleArrows = computed(() => {
  const vis = new Set(viewportTables.value.map((t) => t.name))
  return arrows.value.filter((a) => {
    if (highlightedArrowKeys.value.has(a.key)) return true
    const fk = fkByKey.value.get(a.key)
    return fk ? vis.has(fk.table_name) || vis.has(fk.ref_table_name) : false
  })
})

onMounted(() => {
  window.addEventListener('mousemove', onMousemove)
  window.addEventListener('mouseup',   onMouseup)
  window.addEventListener('touchmove', onTouchMove,  { passive: false })
  window.addEventListener('touchend',  onTouchEnd)
  window.addEventListener('touchcancel', onTouchEnd)
})
onBeforeUnmount(() => {
  window.removeEventListener('mousemove', onMousemove)
  window.removeEventListener('mouseup',   onMouseup)
  window.removeEventListener('touchmove', onTouchMove)
  window.removeEventListener('touchend',  onTouchEnd)
  window.removeEventListener('touchcancel', onTouchEnd)
  if (_rafId !== null) cancelAnimationFrame(_rafId)
})
</script>

<template>
  <div class="er-root">
    <!-- Toolbar -->
    <div class="er-toolbar">
      <div class="er-toolbar__left">
        <span class="er-toolbar__title">ER Diagram</span>
        <template v-if="activeConn">
          <span class="er-toolbar__conn">
            <span class="er-driver-badge">{{ activeConn.driver.toUpperCase() }}</span>
            {{ activeConn.name }}
          </span>
        </template>
      </div>
      <div class="er-toolbar__right">
        <input v-model="tableFilter" class="er-filter-input" placeholder="Filter tables…" />
        <label class="er-toggle">
          <input v-model="compactMode" type="checkbox" />
          Compact
        </label>
        <button class="base-btn base-btn--ghost base-btn--sm" @click="sidepanelOpen = !sidepanelOpen">
          {{ sidepanelOpen ? 'Hide Panel' : 'Show Panel' }}
        </button>
        <select
          v-if="activeConn"
          v-model="selectedDb"
          class="er-db-select"
          @change="activeConn && fetchER(activeConn.id!, selectedDb)"
        >
          <option :value="activeConn?.database">{{ activeConn?.database }}</option>
        </select>
        <button class="base-btn base-btn--ghost base-btn--sm" @click="resetView">Reset view</button>
        <button
          class="base-btn base-btn--ghost base-btn--sm"
          @click="activeConn && fetchER(activeConn.id!, selectedDb, true)"
        >
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.08-4.43"/></svg>
          Refresh
        </button>
      </div>
    </div>

    <!-- Body -->
    <div class="er-body" :class="{ 'er-body--collapsed': !sidepanelOpen }">
      <!-- Canvas -->
      <div
        ref="canvasEl"
        class="er-canvas"
        :class="{ 'er-canvas--panning': isPanning }"
        @wheel.prevent="onWheel"
        @mousedown="onCanvasDown"
        @touchstart.prevent="onCanvasTouchStart"
      >
        <!-- Loading -->
        <div v-if="loading" class="er-center">
          <svg class="spin" width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="var(--brand)" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
          <span style="color:var(--text-muted);font-size:13px">Loading diagram…</span>
        </div>

        <!-- Error -->
        <div v-else-if="error" class="er-center notice notice--error" style="max-width:460px">{{ error }}</div>

        <!-- No connection -->
        <div v-else-if="!activeConn" class="er-center empty-state">
          Select a connection from the sidebar to view its ER diagram.
        </div>

        <!-- Empty -->
        <div v-else-if="erData && erData.tables.length === 0" class="er-center empty-state">
          No tables found in <strong>{{ selectedDb }}</strong>.
        </div>

        <!-- World (pan+zoom container) -->
        <div
          v-if="erData && erData.tables.length"
          class="er-world"
          :style="{
            transform: `translate(${panX}px, ${panY}px) scale(${scale})`,
            width: `${worldW}px`,
            height: `${worldH}px`,
          }"
        >
          <!-- Arrow SVG (pointer-events: none so it doesn't block table clicks) -->
          <svg
            class="er-arrow-svg"
            :width="worldW"
            :height="worldH"
          >
            <defs>
              <marker id="arr" markerWidth="8" markerHeight="8" refX="7" refY="3" orient="auto">
                <path d="M0,0 L0,6 L8,3 z" fill="var(--brand)" />
              </marker>
              <marker id="arr-dim" markerWidth="8" markerHeight="8" refX="7" refY="3" orient="auto">
                <path d="M0,0 L0,6 L8,3 z" fill="var(--brand)" opacity="0.4" />
              </marker>
            </defs>
            <path
              v-for="a in visibleArrows"
              :key="a.key"
              :d="a.path"
              fill="none"
              stroke="var(--brand)"
              :stroke-width="highlightedArrowKeys.has(a.key) ? 2.5 : 1.5"
              stroke-dasharray="6 3"
              :opacity="highlightedArrowKeys.size ? (highlightedArrowKeys.has(a.key) ? 0.9 : 0.1) : 0.4"
              :marker-end="highlightedArrowKeys.has(a.key) ? 'url(#arr)' : 'url(#arr-dim)'"
            />
          </svg>

          <!-- Table nodes (HTML divs — dramatically faster than SVG for 100+ tables) -->
          <div
            v-for="t in viewportTables"
            :key="t.name"
            :ref="(el) => setTableRef(el as Element | null, t.name)"
            class="erd-table"
            :class="{
              'erd-table--selected': selectedTableName === t.name,
              'erd-table--dimmed':   highlightedTableNames.size > 0 && !highlightedTableNames.has(t.name),
              'erd-table--compact':  compactMode,
            }"
            :style="{ left: `${t.x}px`, top: `${t.y}px`, width: `${t.width}px` }"
            @click.stop="selectTable(t.name)"
          >
            <!-- Header / drag handle -->
            <div
              class="erd-table__header"
              @mousedown.stop="onTableHeaderDown($event, t)"
              @touchstart.stop.prevent="onTableHeaderTouchDown($event, t)"
            >
              <span
                class="erd-table__badge"
                :class="t.type === 'view' ? 'erd-table__badge--view' : 'erd-table__badge--table'"
              >{{ t.type === 'view' ? 'VW' : 'TB' }}</span>
              <span class="erd-table__name" :title="t.name">{{ t.name }}</span>
              <span class="erd-table__count">{{ t.columns.length }}</span>
            </div>

            <!-- Compact body -->
            <div v-if="compactMode" class="erd-table__compact-body">
              <span>{{ t.columns.filter((c) => c.is_primary_key).length }} PK</span>
              <span>{{ t.columns.length }} columns</span>
            </div>

            <!-- Full column list -->
            <template v-else>
              <div
                v-for="(col, ci) in t.columns.slice(0, 20)"
                :key="col.name"
                class="erd-col"
                :class="{ 'erd-col--pk': col.is_primary_key, 'erd-col--alt': ci % 2 === 1 }"
              >
                <span class="erd-col__icon">{{ col.is_primary_key ? '🔑' : '' }}</span>
                <span class="erd-col__name">{{ col.name }}</span>
                <span class="erd-col__type">
                  {{ col.data_type.replace('character varying', 'varchar').replace('timestamp without time zone', 'ts').slice(0, 14) }}
                </span>
              </div>
              <div v-if="t.columns.length > 20" class="erd-col erd-col--more">
                +{{ t.columns.length - 20 }} more columns
              </div>
            </template>
          </div>
        </div>

        <!-- Zoom indicator -->
        <div v-if="erData && erData.tables.length" class="er-zoom-badge">{{ zoomPct }}</div>
      </div>

      <!-- Side panel -->
      <aside v-if="erData && erData.tables.length && sidepanelOpen" class="er-sidepanel">
        <section class="er-panel">
          <div class="er-panel__title">Join Path Finder</div>
          <div class="er-panel__sub">Find the shortest foreign-key path between two tables.</div>
          <select v-model="pathFrom" class="er-sidepanel__select">
            <option value="">From table…</option>
            <option v-for="name in filteredTableNames" :key="`f-${name}`" :value="name">{{ name }}</option>
          </select>
          <select v-model="pathTo" class="er-sidepanel__select">
            <option value="">To table…</option>
            <option v-for="name in filteredTableNames" :key="`t-${name}`" :value="name">{{ name }}</option>
          </select>
          <div v-if="pathFrom && pathTo && !joinPath.length" class="er-sidepanel__empty">No FK path found between these tables.</div>
          <div v-else-if="joinPath.length" class="er-path-list">
            <div v-for="step in joinPath" :key="step.constraint_name" class="er-path-step">
              <strong>{{ step.table_name }}</strong>
              <span>{{ step.column_name }} → {{ step.ref_table_name }}.{{ step.ref_column_name }}</span>
            </div>
          </div>
        </section>

        <section class="er-panel">
          <div class="er-panel__title">Table Details</div>
          <div class="er-panel__sub">Click a table in the diagram to inspect its shape.</div>
          <template v-if="selectedTable">
            <div class="er-detail-head">
              <strong>{{ selectedTable.name }}</strong>
              <span class="er-detail-type">{{ selectedTable.type }}</span>
            </div>
            <div class="er-detail-stats">
              <span>{{ selectedTable.columns.length }} columns</span>
              <span>{{ selectedTableRelCount }} relationships</span>
            </div>
            <div class="er-column-list">
              <div v-for="col in selectedTable.columns" :key="col.name" class="er-column-row">
                <div class="er-column-row__name">
                  <span v-if="col.is_primary_key" class="er-pk">PK</span>
                  <span>{{ col.name }}</span>
                </div>
                <div class="er-column-row__meta">
                  {{ col.data_type }}<span v-if="!col.is_nullable"> · required</span>
                </div>
              </div>
            </div>
          </template>
          <div v-else class="er-sidepanel__empty">Select a table to view details.</div>
        </section>
      </aside>
    </div>

    <!-- Legend -->
    <div v-if="erData && erData.tables.length" class="er-legend">
      <span class="er-legend__item">
        <svg width="24" height="10"><line x1="0" y1="5" x2="24" y2="5" stroke="var(--brand)" stroke-width="1.5" stroke-dasharray="5 2" /></svg>
        Foreign key
      </span>
      <span class="er-legend__item">
        <span style="color:#f2c97d;font-size:12px">🔑</span> Primary key
      </span>
      <span class="er-legend__hint">Drag header to move · Scroll to zoom · Drag canvas to pan</span>
      <span class="er-legend__stat">{{ erData.tables.length }} tables · {{ erData.foreign_keys.length }} FK · {{ viewportTables.length }} visible</span>
    </div>
  </div>
</template>

<style scoped>
/* ── Root layout ─────────────────────────────────────────────────── */
.er-root {
  width: 100%; height: 100%;
  display: flex; flex-direction: column;
  overflow: hidden;
}

.er-body {
  flex: 1; min-height: 0;
  display: grid;
  grid-template-columns: minmax(0, 1fr) 320px;
}

.er-body--collapsed {
  grid-template-columns: minmax(0, 1fr);
}

/* ── Toolbar ─────────────────────────────────────────────────────── */
.er-toolbar {
  display: flex; align-items: center; justify-content: space-between;
  padding: 0 16px; height: 44px; flex-shrink: 0;
  background: var(--bg-surface); border-bottom: 1px solid var(--border);
  gap: 12px;
}
.er-toolbar__left  { display: flex; align-items: center; gap: 12px; }
.er-toolbar__right { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.er-toolbar__title { font-size: 13px; font-weight: 600; color: var(--text-primary); }
.er-toolbar__conn  { display: flex; align-items: center; gap: 6px; font-size: 12.5px; color: var(--text-secondary); }
.er-driver-badge   {
  font-size: 10px; font-weight: 600; text-transform: uppercase;
  background: var(--brand-dim); color: var(--brand);
  padding: 1px 6px; border-radius: 4px;
}
.er-db-select, .er-filter-input {
  background: var(--bg-elevated); color: var(--text-primary);
  border: 1px solid var(--border); border-radius: var(--r);
  padding: 4px 8px; font-size: 12px; font-family: inherit; cursor: pointer;
}
.er-filter-input { width: 160px; }
.er-toggle {
  display: flex; align-items: center; gap: 6px;
  font-size: 12px; color: var(--text-secondary); cursor: pointer;
}

/* ── Canvas ──────────────────────────────────────────────────────── */
.er-canvas {
  position: relative; overflow: hidden;
  background:
    radial-gradient(circle, var(--border) 1px, transparent 1px) 0 0 / 24px 24px,
    var(--bg-body);
  cursor: default;
  touch-action: none;
}

.er-canvas--panning { cursor: grabbing; }

.er-center {
  position: absolute; inset: 0;
  display: flex; flex-direction: column;
  align-items: center; justify-content: center; gap: 12px;
}

/* ── World (pan+zoom layer) ──────────────────────────────────────── */
.er-world {
  position: absolute; top: 0; left: 0;
  transform-origin: 0 0;
  will-change: transform;
}

/* ── Arrow SVG ───────────────────────────────────────────────────── */
.er-arrow-svg {
  position: absolute; top: 0; left: 0;
  pointer-events: none; overflow: visible;
}

/* ── Table nodes (HTML) ──────────────────────────────────────────── */
.erd-table {
  position: absolute;
  background: var(--bg-surface);
  border: 1.5px solid var(--border-2, var(--border));
  border-radius: 10px;
  overflow: hidden;
  box-shadow: 0 2px 8px rgba(0,0,0,0.10), 0 1px 3px rgba(0,0,0,0.06);
  transition: box-shadow 0.15s ease, border-color 0.15s ease, opacity 0.12s ease;
  user-select: none;
  contain: layout style;
}

.erd-table--selected {
  border-color: var(--brand);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--brand) 20%, transparent), 0 4px 20px rgba(0,0,0,0.14);
}

.erd-table--dimmed { opacity: 0.25; }

.erd-table:hover:not(.erd-table--dimmed) {
  box-shadow: 0 4px 16px rgba(0,0,0,0.14), 0 2px 6px rgba(0,0,0,0.08);
  border-color: var(--brand);
}

/* ── Table header ────────────────────────────────────────────────── */
.erd-table__header {
  display: flex; align-items: center; gap: 7px;
  padding: 0 10px; height: 40px;
  background: var(--bg-elevated);
  border-bottom: 1px solid var(--border);
  cursor: grab;
}

.erd-table__header:active { cursor: grabbing; }

.erd-table__badge {
  flex-shrink: 0;
  font-size: 9px; font-weight: 700; letter-spacing: 0.04em; text-transform: uppercase;
  padding: 2px 5px; border-radius: 4px;
}
.erd-table__badge--table { background: rgba(92,184,165,0.15); color: var(--success, #5cb8a5); }
.erd-table__badge--view  { background: var(--brand-dim); color: var(--brand); }

.erd-table__name {
  flex: 1; min-width: 0;
  font-size: 13px; font-weight: 600; color: var(--text-primary);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}

.erd-table__count {
  flex-shrink: 0;
  font-size: 11px; color: var(--text-muted);
  background: var(--bg-body);
  padding: 1px 6px; border-radius: 10px;
}

/* ── Compact body ────────────────────────────────────────────────── */
.erd-table__compact-body {
  display: flex; align-items: center; justify-content: space-between;
  padding: 10px 12px;
  font-size: 12px; color: var(--text-muted);
}

/* ── Column rows ─────────────────────────────────────────────────── */
.erd-col {
  display: flex; align-items: center; gap: 5px;
  padding: 0 10px; height: 24px;
  font-size: 11px; font-family: 'JetBrains Mono', monospace;
}

.erd-col--alt { background: rgba(128,128,128,0.04); }
.erd-col--pk  { background: rgba(242,201,125,0.07) !important; }

.erd-col__icon { width: 14px; font-size: 9px; flex-shrink: 0; line-height: 1; }
.erd-col__name { flex: 1; min-width: 0; color: var(--text-secondary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.erd-col--pk .erd-col__name { color: var(--text-primary); font-weight: 600; }
.erd-col__type { flex-shrink: 0; font-size: 10px; color: var(--text-muted); }

.erd-col--more {
  justify-content: center; font-size: 11px; color: var(--text-muted);
  height: 28px; border-top: 1px solid var(--border);
}

/* ── Zoom badge ──────────────────────────────────────────────────── */
.er-zoom-badge {
  position: absolute; bottom: 12px; left: 12px;
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 6px; padding: 3px 8px;
  font-size: 11px; color: var(--text-muted); font-variant-numeric: tabular-nums;
  pointer-events: none;
}

/* ── Side panel ──────────────────────────────────────────────────── */
.er-sidepanel {
  border-left: 1px solid var(--border);
  background: var(--bg-surface);
  padding: 14px;
  overflow-y: auto;
  display: grid;
  gap: 14px;
  align-content: start;
}

.er-panel {
  border: 1px solid var(--border); border-radius: 14px;
  background: rgba(255,255,255,0.02);
  padding: 14px; display: grid; gap: 10px;
}
.er-panel__title { font-size: 14px; font-weight: 700; color: var(--text-primary); }
.er-panel__sub, .er-sidepanel__empty { font-size: 12px; color: var(--text-muted); }

.er-sidepanel__select {
  background: var(--bg-elevated); color: var(--text-primary);
  border: 1px solid var(--border); border-radius: 10px;
  padding: 8px 10px; font-size: 12px; font-family: inherit; width: 100%;
}

.er-path-list, .er-column-list { display: grid; gap: 6px; }

.er-path-step, .er-column-row {
  border: 1px solid var(--border); border-radius: 8px;
  padding: 8px 12px; background: rgba(255,255,255,0.02);
  display: grid; gap: 3px;
}
.er-path-step strong, .er-detail-head strong { font-size: 13px; color: var(--text-primary); }
.er-path-step span, .er-column-row__meta, .er-detail-stats { font-size: 12px; color: var(--text-muted); }
.er-detail-head, .er-column-row__name { display: flex; align-items: center; gap: 8px; }

.er-detail-type, .er-pk {
  font-size: 10px; font-weight: 700; letter-spacing: 0.04em; text-transform: uppercase;
  border-radius: 999px; padding: 2px 7px;
  background: var(--brand-dim); color: var(--brand);
}

/* ── Legend ──────────────────────────────────────────────────────── */
.er-legend {
  display: flex; align-items: center; gap: 16px; flex-wrap: wrap;
  padding: 6px 16px; flex-shrink: 0;
  background: var(--bg-surface); border-top: 1px solid var(--border);
  font-size: 11px; color: var(--text-muted);
}
.er-legend__item { display: flex; align-items: center; gap: 6px; }
.er-legend__stat { margin-left: auto; font-variant-numeric: tabular-nums; }
.er-legend__hint { font-style: italic; }

.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }

@media (max-width: 1100px) {
  .er-body { grid-template-columns: 1fr; }
  .er-sidepanel { border-left: none; border-top: 1px solid var(--border); max-height: 280px; }
}
</style>
