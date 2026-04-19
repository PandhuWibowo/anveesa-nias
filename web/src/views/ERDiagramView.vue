<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'

const props = defineProps<{ activeConnId?: number | null }>()

interface ERColumn { name: string; data_type: string; is_primary_key: boolean; is_nullable: boolean }
interface ERTable  { name: string; type: string; columns: ERColumn[] }
interface FK       { constraint_name: string; table_name: string; column_name: string; ref_table_name: string; ref_column_name: string }
interface ERData   { tables: ERTable[]; foreign_keys: FK[] }

const { connections } = useConnections()
const activeConn = computed(() =>
  props.activeConnId
    ? connections.value.find((c) => c.id === props.activeConnId)
    : connections.value[0] ?? null,
)

// ── State ─────────────────────────────────────────────────────────
const dbList = computed(() => {
  const names: string[] = []
  connections.value.forEach((c) => { if (c.database && !names.includes(c.database)) names.push(c.database) })
  return names
})

const selectedDb = ref('')
const erData = ref<ERData | null>(null)
const loading = ref(false)
const error = ref('')

watch(activeConn, (c) => {
  if (c?.database) selectedDb.value = c.database
}, { immediate: true })

watch([() => activeConn.value, selectedDb], ([conn, db]) => {
  if (conn && db) fetchER(conn.id!, db)
}, { immediate: true })

async function fetchER(connId: number, db: string) {
  loading.value = true
  error.value = ''
  erData.value = null
  try {
    const encodedDb = encodeURIComponent(db)
    const path = encodedDb ? `/api/connections/${connId}/er/${encodedDb}` : `/api/connections/${connId}/er`
    const { data } = await axios.get<ERData>(path)
    erData.value = data
    computeLayout()
  } catch (e: unknown) {
    error.value = (e as { response?: { data?: { error?: string } } })?.response?.data?.error ?? 'Failed to load ER diagram'
  } finally {
    loading.value = false
  }
}

// ── Layout computation ────────────────────────────────────────────
const TABLE_W = 220
const ROW_H   = 26
const HEADER_H = 36
const GAP_X   = 60
const GAP_Y   = 48

interface LayoutTable extends ERTable {
  x: number; y: number; width: number; height: number
}

const layout = ref<LayoutTable[]>([])

function tableHeight(t: ERTable) {
  return HEADER_H + Math.min(t.columns.length, 20) * ROW_H + 6
}

function computeLayout() {
  if (!erData.value) return
  const tables = erData.value.tables
  const COLS = Math.max(1, Math.ceil(Math.sqrt(tables.length)))
  layout.value = tables.map((t, i) => {
    const col = i % COLS
    const row = Math.floor(i / COLS)
    return {
      ...t,
      x: col * (TABLE_W + GAP_X) + 40,
      y: row * (300 + GAP_Y) + 40,
      width: TABLE_W,
      height: tableHeight(t),
    }
  })
}

// ── SVG dimensions ────────────────────────────────────────────────
const svgW = computed(() => {
  if (!layout.value.length) return 800
  return Math.max(...layout.value.map((t) => t.x + t.width)) + 60
})
const svgH = computed(() => {
  if (!layout.value.length) return 600
  return Math.max(...layout.value.map((t) => t.y + t.height)) + 60
})

// ── FK arrow paths ────────────────────────────────────────────────
interface Arrow { path: string; key: string }
const arrows = computed<Arrow[]>(() => {
  if (!erData.value || !layout.value.length) return []
  const result: Arrow[] = []
  for (const fk of erData.value.foreign_keys) {
    const src = layout.value.find((t) => t.name === fk.table_name)
    const dst = layout.value.find((t) => t.name === fk.ref_table_name)
    if (!src || !dst) continue

    const srcColIdx = src.columns.findIndex((c) => c.name === fk.column_name)
    const dstColIdx = dst.columns.findIndex((c) => c.name === fk.ref_column_name)

    const srcY = src.y + HEADER_H + (srcColIdx + 0.5) * ROW_H
    const dstY = dst.y + HEADER_H + (dstColIdx + 0.5) * ROW_H

    // Exit from right edge of src, enter from left edge of dst (or swap if dst is left)
    let x1: number, x2: number, cp1x: number, cp2x: number
    if (src.x + src.width <= dst.x) {
      x1 = src.x + src.width; x2 = dst.x
    } else if (dst.x + dst.width <= src.x) {
      x1 = src.x; x2 = dst.x + dst.width
    } else {
      x1 = src.x + src.width; x2 = dst.x
    }
    cp1x = x1 + (x2 - x1) * 0.5
    cp2x = cp1x

    result.push({
      key: fk.constraint_name,
      path: `M ${x1} ${srcY} C ${cp1x} ${srcY}, ${cp2x} ${dstY}, ${x2} ${dstY}`,
    })
  }
  return result
})

// ── Pan / zoom ────────────────────────────────────────────────────
const panX = ref(0)
const panY = ref(0)
const scale = ref(1)
const svgEl = ref<SVGSVGElement>()
let isPanning = false
let lastX = 0, lastY = 0

function onWheel(e: WheelEvent) {
  e.preventDefault()
  const factor = e.deltaY < 0 ? 1.1 : 0.9
  scale.value = Math.min(3, Math.max(0.2, scale.value * factor))
}

function onMousedown(e: MouseEvent) {
  if ((e.target as Element).closest('.er-table-node')) return
  isPanning = true
  lastX = e.clientX; lastY = e.clientY
}

function onMousemove(e: MouseEvent) {
  if (!isPanning) return
  panX.value += e.clientX - lastX
  panY.value += e.clientY - lastY
  lastX = e.clientX; lastY = e.clientY
}

function onMouseup() { isPanning = false }

function resetView() { panX.value = 0; panY.value = 0; scale.value = 1 }

onMounted(() => {
  window.addEventListener('mouseup', onMouseup)
})
onBeforeUnmount(() => {
  window.removeEventListener('mouseup', onMouseup)
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
            <span class="query-toolbar__conn-driver">{{ activeConn.driver.toUpperCase() }}</span>
            {{ activeConn.name }}
          </span>
        </template>
      </div>
      <div class="er-toolbar__right">
        <select
          v-if="activeConn"
          v-model="selectedDb"
          class="er-db-select"
          @change="activeConn && fetchER(activeConn.id!, selectedDb)"
        >
          <option :value="activeConn?.database">{{ activeConn?.database }}</option>
        </select>
        <button class="base-btn base-btn--ghost base-btn--sm" @click="resetView">Reset view</button>
        <button class="base-btn base-btn--ghost base-btn--sm" @click="activeConn && fetchER(activeConn.id!, selectedDb)">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.08-4.43"/></svg>
          Refresh
        </button>
      </div>
    </div>

    <!-- Canvas -->
    <div class="er-canvas">
      <!-- Loading / error -->
      <div v-if="loading" class="er-center">
        <svg class="spin" width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="var(--brand)" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
        <span style="color:var(--text-muted);font-size:13px">Loading diagram…</span>
      </div>
      <div v-else-if="error" class="er-center notice notice--error" style="max-width:460px">
        {{ error }}
      </div>
      <div v-else-if="!activeConn" class="er-center empty-state">
        Select a connection from the sidebar to view its ER diagram.
      </div>
      <div v-else-if="erData && erData.tables.length === 0" class="er-center empty-state">
        No tables found in database <strong>{{ selectedDb }}</strong>.
      </div>

      <!-- SVG diagram -->
      <svg
        v-if="erData && erData.tables.length"
        ref="svgEl"
        class="er-svg"
        :width="svgW"
        :height="svgH"
        :style="{
          transform: `translate(${panX}px, ${panY}px) scale(${scale})`,
          transformOrigin: '0 0',
          cursor: isPanning ? 'grabbing' : 'grab',
        }"
        @wheel.prevent="onWheel"
        @mousedown="onMousedown"
        @mousemove="onMousemove"
      >
        <defs>
          <marker id="arrow" markerWidth="8" markerHeight="8" refX="6" refY="3" orient="auto">
            <path d="M0,0 L0,6 L8,3 z" fill="var(--brand)" opacity="0.7" />
          </marker>
        </defs>

        <!-- FK arrows -->
        <g class="er-arrows">
          <path
            v-for="a in arrows"
            :key="a.key"
            :d="a.path"
            fill="none"
            stroke="var(--brand)"
            stroke-width="1.5"
            stroke-dasharray="6 3"
            opacity="0.6"
            marker-end="url(#arrow)"
          />
        </g>

        <!-- Table nodes -->
        <g
          v-for="t in layout"
          :key="t.name"
          :transform="`translate(${t.x}, ${t.y})`"
          class="er-table-node"
        >
          <!-- Shadow -->
          <rect :width="t.width" :height="t.height" rx="6" fill="rgba(0,0,0,0.15)" transform="translate(3,3)" />

          <!-- Card background -->
          <rect :width="t.width" :height="t.height" rx="6"
            fill="var(--bg-surface)" stroke="var(--border-2)" stroke-width="1.5" />

          <!-- Header -->
          <rect :width="t.width" :height="HEADER_H" rx="6" fill="var(--bg-elevated)" />
          <rect x="0" :y="HEADER_H - 4" :width="t.width" height="4" fill="var(--bg-elevated)" />
          <rect x="0" :y="HEADER_H" :width="t.width" height="1" fill="var(--border)" />

          <!-- Table type badge -->
          <rect x="10" y="9" width="20" height="14" rx="3"
            :fill="t.type === 'view' ? 'var(--brand-dim)' : 'rgba(92,184,165,0.15)'" />
          <text x="20" y="20" text-anchor="middle" font-size="8" font-weight="700"
            font-family="Inter, sans-serif" letter-spacing="0.3"
            :fill="t.type === 'view' ? 'var(--brand)' : 'var(--success)'">
            {{ t.type === 'view' ? 'VW' : 'TB' }}
          </text>

          <!-- Table name -->
          <text x="38" y="22" font-size="13" font-weight="600"
            font-family="Inter, sans-serif" fill="var(--text-primary)">
            {{ t.name.length > 20 ? t.name.slice(0, 19) + '…' : t.name }}
          </text>

          <!-- Columns -->
          <g v-for="(col, ci) in t.columns.slice(0, 20)" :key="col.name">
            <rect
              x="0" :y="HEADER_H + ci * ROW_H + 3"
              :width="t.width" :height="ROW_H - 1"
              :fill="ci % 2 === 0 ? 'transparent' : 'rgba(128,128,128,0.04)'"
            />
            <!-- PK icon -->
            <text v-if="col.is_primary_key"
              x="12" :y="HEADER_H + ci * ROW_H + 18"
              font-size="9" fill="#f2c97d" font-family="Inter, sans-serif">🔑</text>
            <!-- Column name -->
            <text
              :x="col.is_primary_key ? 28 : 14"
              :y="HEADER_H + ci * ROW_H + 18"
              font-size="11.5" font-family="JetBrains Mono, monospace"
              :fill="col.is_primary_key ? 'var(--text-primary)' : 'var(--text-secondary)'"
              :font-weight="col.is_primary_key ? '600' : '400'"
            >
              {{ col.name.length > 18 ? col.name.slice(0, 17) + '…' : col.name }}
            </text>
            <!-- Data type -->
            <text
              :x="t.width - 10" :y="HEADER_H + ci * ROW_H + 18"
              font-size="10" font-family="JetBrains Mono, monospace"
              fill="var(--text-muted)" text-anchor="end"
            >
              {{ col.data_type.replace('character varying', 'varchar').replace('timestamp without time zone', 'timestamp').slice(0, 12) }}
            </text>
          </g>

          <!-- "N more" label if truncated -->
          <text
            v-if="t.columns.length > 20"
            x="10" :y="HEADER_H + 20 * ROW_H + 16"
            font-size="10.5" font-family="Inter, sans-serif" fill="var(--text-muted)"
          >
            +{{ t.columns.length - 20 }} more columns…
          </text>
        </g>
      </svg>
    </div>

    <!-- Legend -->
    <div v-if="erData && erData.tables.length" class="er-legend">
      <span class="er-legend__item">
        <svg width="24" height="10"><line x1="0" y1="5" x2="24" y2="5" stroke="var(--brand)" stroke-width="1.5" stroke-dasharray="5 2" /></svg>
        Foreign key
      </span>
      <span class="er-legend__item">
        <span style="color:#f2c97d">🔑</span> Primary key
      </span>
      <span class="er-legend__stat">{{ erData.tables.length }} tables · {{ erData.foreign_keys.length }} FK relationships</span>
      <span class="er-legend__hint">Scroll to zoom · Drag to pan</span>
    </div>
  </div>
</template>

<style scoped>
.er-root {
  width: 100%; height: 100%;
  display: flex; flex-direction: column;
  overflow: hidden;
}

/* Toolbar */
.er-toolbar {
  display: flex; align-items: center; justify-content: space-between;
  padding: 0 16px; height: 44px; flex-shrink: 0;
  background: var(--bg-surface); border-bottom: 1px solid var(--border);
  gap: 12px;
}
.er-toolbar__left  { display: flex; align-items: center; gap: 12px; }
.er-toolbar__right { display: flex; align-items: center; gap: 8px; }
.er-toolbar__title { font-size: 13px; font-weight: 600; color: var(--text-primary); }
.er-toolbar__conn  { display: flex; align-items: center; gap: 6px; font-size: 12.5px; color: var(--text-secondary); }
.query-toolbar__conn-driver {
  font-size: 10px; font-weight: 600; text-transform: uppercase;
  background: var(--brand-dim); color: var(--brand);
  padding: 1px 6px; border-radius: 4px;
}

.er-db-select {
  background: var(--bg-elevated); color: var(--text-primary);
  border: 1px solid var(--border); border-radius: var(--r);
  padding: 3px 8px; font-size: 12px; font-family: inherit;
  cursor: pointer;
}

/* Canvas */
.er-canvas {
  flex: 1; min-height: 0;
  overflow: hidden; position: relative;
  background:
    radial-gradient(circle, var(--border) 1px, transparent 1px) 0 0 / 24px 24px,
    var(--bg-body);
}

.er-center {
  position: absolute; inset: 0;
  display: flex; flex-direction: column;
  align-items: center; justify-content: center; gap: 12px;
}

.er-svg {
  position: absolute; top: 0; left: 0;
  user-select: none;
}

/* Legend */
.er-legend {
  display: flex; align-items: center; gap: 20px;
  padding: 6px 16px; flex-shrink: 0;
  background: var(--bg-surface); border-top: 1px solid var(--border);
  font-size: 11px; color: var(--text-muted);
}
.er-legend__item { display: flex; align-items: center; gap: 6px; }
.er-legend__stat { margin-left: auto; }
.er-legend__hint { font-style: italic; }
</style>
