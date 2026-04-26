<template>
  <div class="rc-root">
    <div v-if="!hideControls" class="rc-controls">
      <div class="rc-ctrl-group">
        <label>Chart</label>
        <div class="rc-tabs">
          <button v-for="option in chartTypeOptions" :key="option.value"
            :class="['rc-tab', { active: chartType === option.value }]"
            @click="chartType = option.value">{{ option.label }}</button>
        </div>
      </div>
      <div class="rc-ctrl-group">
        <label>X Axis</label>
        <select v-model="xCol" class="rc-select">
          <option v-for="c in columns" :key="c" :value="c">{{ c }}</option>
        </select>
      </div>
      <div class="rc-ctrl-group" v-if="!isPartitionChart">
        <label>Y Axis</label>
        <select v-model="yCol" class="rc-select">
          <option v-for="c in numericColumns" :key="c" :value="c">{{ c }}</option>
        </select>
      </div>
      <div class="rc-ctrl-group" v-if="isPartitionChart">
        <label>Value</label>
        <select v-model="yCol" class="rc-select">
          <option v-for="c in numericColumns" :key="c" :value="c">{{ c }}</option>
        </select>
      </div>
    </div>

    <div class="rc-canvas" ref="canvasRef">
      <svg v-if="chartType === 'bar'" :key="animationKey" width="100%" height="100%" :viewBox="`0 0 ${svgW} ${svgH}`" class="rc-svg">
        <!-- Y grid -->
        <line v-for="(yt, i) in yTicks" :key="i"
          :x1="padL" :x2="svgW - padR" :y1="yScale(yt)" :y2="yScale(yt)"
          stroke="var(--border)" stroke-dasharray="3,3" />
        <text v-for="(yt, i) in yTicks" :key="'yt'+i"
          :x="padL - 6" :y="yScale(yt) + 4" text-anchor="end" class="rc-axis-label">
          {{ fmtNum(yt) }}
        </text>
        <!-- Bars -->
        <rect v-for="(d, i) in chartData" :key="i"
          :x="barX(i)" :y="Math.min(yScale(d.y), yScale(0))"
          :width="barW" :height="Math.abs(yScale(0) - yScale(d.y))"
          rx="3"
          :fill="barColors[i % barColors.length]"
          class="rc-bar rc-bar-v"
          :style="{ '--rc-delay': `${i * 24}ms` }">
          <title>{{ d.x }}: {{ fmtNum(d.y) }}</title>
        </rect>
        <!-- X labels -->
        <text v-for="(d, i) in chartData" :key="'xl'+i"
          :x="barX(i) + barW / 2" :y="svgH - padB + 14"
          text-anchor="middle" class="rc-axis-label">
          {{ truncate(fmtAxisLabel(d.x), 10) }}
        </text>
        <!-- Axes -->
        <line :x1="padL" :x2="padL" :y1="padT" :y2="svgH - padB" stroke="var(--border)" />
        <line :x1="padL" :x2="svgW - padR" :y1="svgH - padB" :y2="svgH - padB" stroke="var(--border)" />
      </svg>

      <svg v-else-if="chartType === 'horizontal-bar'" :key="animationKey" width="100%" height="100%" :viewBox="`0 0 ${svgW} ${svgH}`" class="rc-svg">
        <line v-for="(xt, i) in xValueTicks" :key="i"
          :x1="xValueScale(xt)" :x2="xValueScale(xt)" :y1="padT" :y2="svgH - padB"
          stroke="var(--border)" stroke-dasharray="3,3" />
        <text v-for="(xt, i) in xValueTicks" :key="'xt'+i"
          :x="xValueScale(xt)" :y="svgH - padB + 14" text-anchor="middle" class="rc-axis-label">
          {{ fmtNum(xt) }}
        </text>
        <rect v-for="(d, i) in horizontalData" :key="i"
          :x="Math.min(xValueScale(0), xValueScale(d.y))"
          :y="horizontalY(i)"
          :width="Math.abs(xValueScale(d.y) - xValueScale(0))"
          :height="horizontalBarH"
          rx="3"
          :fill="barColors[i % barColors.length]"
          class="rc-bar rc-bar-h"
          :style="{ '--rc-delay': `${i * 28}ms` }">
          <title>{{ d.x }}: {{ fmtNum(d.y) }}</title>
        </rect>
        <text v-for="(d, i) in horizontalData" :key="'hl'+i"
          :x="padL - 8" :y="horizontalY(i) + horizontalBarH / 2 + 4"
          text-anchor="end" class="rc-axis-label">
          {{ truncate(fmtAxisLabel(d.x), 12) }}
        </text>
        <line :x1="xValueScale(0)" :x2="xValueScale(0)" :y1="padT" :y2="svgH - padB" stroke="var(--border)" />
        <line :x1="padL" :x2="svgW - padR" :y1="svgH - padB" :y2="svgH - padB" stroke="var(--border)" />
      </svg>

      <svg v-else-if="chartType === 'line' || chartType === 'area'" :key="animationKey" width="100%" height="100%" :viewBox="`0 0 ${svgW} ${svgH}`" class="rc-svg">
        <line v-for="(yt, i) in yTicks" :key="i"
          :x1="padL" :x2="svgW - padR" :y1="yScale(yt)" :y2="yScale(yt)"
          stroke="var(--border)" stroke-dasharray="3,3" />
        <text v-for="(yt, i) in yTicks" :key="'yt'+i"
          :x="padL - 6" :y="yScale(yt) + 4" text-anchor="end" class="rc-axis-label">
          {{ fmtNum(yt) }}
        </text>
        <polygon v-if="chartType === 'area'" :points="areaPoints" fill="#4f9cf9" fill-opacity="0.18" class="rc-area-fill" />
        <polyline :points="linePoints" fill="none" stroke="#4f9cf9" stroke-width="2" stroke-linejoin="round" pathLength="1" class="rc-line-path" />
        <circle v-for="(d, i) in chartData" :key="i"
          :cx="lineX(i)" :cy="yScale(d.y)" r="3" fill="#4f9cf9"
          class="rc-point"
          :style="{ '--rc-delay': `${220 + i * 18}ms` }">
          <title>{{ d.x }}: {{ fmtNum(d.y) }}</title>
        </circle>
        <text v-for="(d, i) in chartData.filter((_,ii) => ii % Math.max(1, Math.floor(chartData.length/10)) === 0)" :key="'xl'+i"
          :x="lineX(chartData.indexOf(d))" :y="svgH - padB + 14"
          text-anchor="middle" class="rc-axis-label">
          {{ truncate(fmtAxisLabel(d.x), 8) }}
        </text>
        <line :x1="padL" :x2="padL" :y1="padT" :y2="svgH - padB" stroke="var(--border)" />
        <line :x1="padL" :x2="svgW - padR" :y1="svgH - padB" :y2="svgH - padB" stroke="var(--border)" />
      </svg>

      <svg v-else-if="chartType === 'scatter'" :key="animationKey" width="100%" height="100%" :viewBox="`0 0 ${svgW} ${svgH}`" class="rc-svg">
        <line v-for="(yt, i) in yTicks" :key="i"
          :x1="padL" :x2="svgW - padR" :y1="yScale(yt)" :y2="yScale(yt)"
          stroke="var(--border)" stroke-dasharray="3,3" />
        <text v-for="(yt, i) in yTicks" :key="'yt'+i"
          :x="padL - 6" :y="yScale(yt) + 4" text-anchor="end" class="rc-axis-label">
          {{ fmtNum(yt) }}
        </text>
        <line v-for="(xt, i) in scatterXTicks" :key="'sx'+i"
          :x1="scatterXScale(xt)" :x2="scatterXScale(xt)" :y1="padT" :y2="svgH - padB"
          stroke="var(--border)" stroke-dasharray="3,3" />
        <text v-for="(xt, i) in scatterXTicks" :key="'sxl'+i"
          :x="scatterXScale(xt)" :y="svgH - padB + 14" text-anchor="middle" class="rc-axis-label">
          {{ scatterUsesNumericX ? fmtNum(xt) : truncate(fmtAxisLabel(chartData[Math.round(xt)]?.x), 8) }}
        </text>
        <circle v-for="(d, i) in chartData" :key="i"
          :cx="scatterX(i, d.x)" :cy="yScale(d.y)" r="4"
          :fill="barColors[i % barColors.length]" fill-opacity="0.85"
          class="rc-point"
          :style="{ '--rc-delay': `${i * 18}ms` }">
          <title>{{ d.x }}: {{ fmtNum(d.y) }}</title>
        </circle>
        <line :x1="padL" :x2="padL" :y1="padT" :y2="svgH - padB" stroke="var(--border)" />
        <line :x1="padL" :x2="svgW - padR" :y1="svgH - padB" :y2="svgH - padB" stroke="var(--border)" />
      </svg>

      <svg v-else-if="chartType === 'pie' || chartType === 'donut'" :key="animationKey" width="100%" height="100%" :viewBox="`0 0 ${svgW} ${svgH}`" preserveAspectRatio="xMidYMid meet" class="rc-svg">
        <g :transform="`translate(${pieCenter.x},${pieCenter.y})`">
          <path v-for="(s, i) in pieSlices" :key="i"
            :d="s.path"
            :fill="pieColors[i % pieColors.length]"
            :opacity="0.85"
            stroke="var(--bg-sidebar)" stroke-width="1.5"
            class="rc-slice"
            :style="{ '--rc-delay': `${i * 42}ms` }">
            <title>{{ s.label }}: {{ fmtNum(s.value) }} ({{ s.pct }}%)</title>
          </path>
          <circle v-if="chartType === 'donut'" :r="donutInnerRadius" fill="var(--bg-panel)" class="rc-donut-hole" />
        </g>
        <!-- Legend -->
        <g :transform="`translate(${svgW - padR - 10}, ${padT})`">
          <g v-for="(s, i) in pieSlices.slice(0, 12)" :key="i" :transform="`translate(0, ${i * 18})`">
            <rect width="12" height="12" :fill="pieColors[i % pieColors.length]" rx="2" />
            <text x="16" y="10" class="rc-axis-label">{{ truncate(String(s.label), 14) }}</text>
          </g>
        </g>
      </svg>

      <div v-if="chartData.length === 0" class="rc-empty">
        Select X and Y columns to render chart
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'

const props = defineProps<{
  columns: string[]
  rows: unknown[][]
  defaultChartType?: ChartType
  initialXCol?: string
  initialYCol?: string
  hideControls?: boolean
}>()

type ChartType = 'bar' | 'horizontal-bar' | 'line' | 'area' | 'scatter' | 'pie' | 'donut'
const chartTypeOptions: Array<{ value: ChartType; label: string }> = [
  { value: 'bar', label: 'Bar' },
  { value: 'horizontal-bar', label: 'H Bar' },
  { value: 'line', label: 'Line' },
  { value: 'area', label: 'Area' },
  { value: 'scatter', label: 'Scatter' },
  { value: 'pie', label: 'Pie' },
  { value: 'donut', label: 'Donut' },
]
const hideControls = computed(() => !!props.hideControls)
const chartType = ref<ChartType>(props.defaultChartType ?? 'bar')
const isPartitionChart = computed(() => chartType.value === 'pie' || chartType.value === 'donut')
const xCol = ref(props.initialXCol || props.columns[0] || '')
const yCol = ref(props.initialYCol || '')

const canvasW = ref(720)
const canvasH = ref(260)
let resizeObserver: ResizeObserver | null = null

const svgW = computed(() => Math.max(canvasW.value, 200))
const svgH = computed(() => Math.max(canvasH.value, 120))

const padL = 48
const padR = computed(() => Math.min(130, svgW.value * 0.18))
const padT = 12
const padB = 32

watch(() => props.columns, cols => {
  if (!xCol.value && cols[0]) xCol.value = cols[0]
})

watch(() => props.defaultChartType, value => {
  if (value) chartType.value = value
})

watch(() => props.initialXCol, value => {
  if (value) xCol.value = value
})

watch(() => props.initialYCol, value => {
  if (value) yCol.value = value
})

const colIdx = (name: string) => props.columns.indexOf(name)

const numericColumns = computed(() => {
  return props.columns.filter(c => {
    const idx = colIdx(c)
    const sample = props.rows.slice(0, 20).map(r => (r as unknown[])[idx])
    return sample.some(v => v !== null && v !== undefined && !isNaN(Number(v)))
  })
})

watch(numericColumns, cols => {
  if (!yCol.value && cols[0]) yCol.value = cols[0]
}, { immediate: true })

const chartData = computed(() => {
  const xi = colIdx(xCol.value)
  const yi = colIdx(yCol.value)
  if (xi < 0 || yi < 0) return []
  const MAX = 200
  return props.rows.slice(0, MAX).map(r => {
    const row = r as unknown[]
    return { x: row[xi] ?? '', y: Number(row[yi]) || 0 }
  })
})

const animationKey = computed(() => {
  const last = props.rows[props.rows.length - 1] as unknown[] | undefined
  return [
    chartType.value,
    xCol.value,
    yCol.value,
    props.rows.length,
    props.columns.length,
    formatCellValueForKey(last?.[colIdx(xCol.value)]),
    formatCellValueForKey(last?.[colIdx(yCol.value)]),
  ].join('|')
})

function formatCellValueForKey(value: unknown) {
  if (value === null || value === undefined) return ''
  if (typeof value === 'object') return JSON.stringify(value).slice(0, 80)
  return String(value).slice(0, 80)
}

const yMin = computed(() => Math.min(0, ...chartData.value.map(d => d.y)))
const yMax = computed(() => Math.max(...chartData.value.map(d => d.y), 1))

function yScale(v: number) {
  const range = yMax.value - yMin.value || 1
  return padT + (svgH.value - padT - padB) * (1 - (v - yMin.value) / range)
}

const yTicks = computed(() => {
  const count = 4
  const step = (yMax.value - yMin.value) / count
  return Array.from({ length: count + 1 }, (_, i) => yMin.value + i * step)
})

const xValueTicks = computed(() => {
  const count = 4
  const min = yMin.value
  const max = yMax.value
  const step = (max - min) / count
  return Array.from({ length: count + 1 }, (_, i) => min + i * step)
})

function xValueScale(v: number) {
  const range = yMax.value - yMin.value || 1
  return padL + (svgW.value - padL - padR.value) * ((v - yMin.value) / range)
}

const barW = computed(() => {
  if (!chartData.value.length) return 0
  const avail = svgW.value - padL - padR.value
  return Math.min(Math.max(6, avail / chartData.value.length * 0.6), 60)
})

function barX(i: number) {
  const avail = svgW.value - padL - padR.value
  const slot = avail / (chartData.value.length || 1)
  return padL + slot * i + (slot - barW.value) / 2
}

function lineX(i: number) {
  const avail = svgW.value - padL - padR.value
  if (chartData.value.length <= 1) return padL
  return padL + (avail / (chartData.value.length - 1)) * i
}

const horizontalData = computed(() => chartData.value.slice(0, Math.max(1, Math.min(16, Math.floor((svgH.value - padT - padB) / 18)))))
const horizontalBarH = computed(() => {
  if (!horizontalData.value.length) return 0
  const avail = svgH.value - padT - padB
  return Math.min(Math.max(8, (avail / horizontalData.value.length) * 0.62), 22)
})

function horizontalY(i: number) {
  const avail = svgH.value - padT - padB
  const slot = avail / (horizontalData.value.length || 1)
  return padT + slot * i + (slot - horizontalBarH.value) / 2
}

const linePoints = computed(() => chartData.value.map((d, i) => `${lineX(i)},${yScale(d.y)}`).join(' '))
const areaPoints = computed(() => {
  if (!chartData.value.length) return ''
  const pts = chartData.value.map((d, i) => `${lineX(i)},${yScale(d.y)}`).join(' ')
  const last = chartData.value.length - 1
  return `${padL},${svgH.value - padB} ${pts} ${lineX(last)},${svgH.value - padB}`
})

const scatterUsesNumericX = computed(() => {
  const values = chartData.value
    .map(d => d.x)
    .filter(v => v !== null && v !== undefined && v !== '')
  return values.length > 0 && values.every(v => !Number.isNaN(Number(v)))
})

const scatterXMin = computed(() => {
  if (!scatterUsesNumericX.value) return 0
  return Math.min(0, ...chartData.value.map(d => Number(d.x)).filter(v => !Number.isNaN(v)))
})

const scatterXMax = computed(() => {
  if (!scatterUsesNumericX.value) return Math.max(chartData.value.length - 1, 1)
  return Math.max(...chartData.value.map(d => Number(d.x)).filter(v => !Number.isNaN(v)), 1)
})

function scatterXScale(v: number) {
  const range = scatterXMax.value - scatterXMin.value || 1
  return padL + (svgW.value - padL - padR.value) * ((v - scatterXMin.value) / range)
}

function scatterX(index: number, value: unknown) {
  if (!scatterUsesNumericX.value) return scatterXScale(index)
  const n = Number(value)
  return scatterXScale(Number.isNaN(n) ? scatterXMin.value : n)
}

const scatterXTicks = computed(() => {
  const count = 4
  if (!scatterUsesNumericX.value) {
    const max = Math.max(chartData.value.length - 1, 0)
    const step = max / count || 1
    return Array.from({ length: count + 1 }, (_, i) => Math.min(max, i * step))
  }
  const step = (scatterXMax.value - scatterXMin.value) / count
  return Array.from({ length: count + 1 }, (_, i) => scatterXMin.value + i * step)
})

const pieCenter = computed(() => ({ x: Math.min(svgW.value / 2 - 30, svgW.value - padR.value - 80), y: svgH.value / 2 }))
const pieRadius = computed(() => Math.min(svgH.value / 2 - padT - 8, 110))
const donutInnerRadius = computed(() => Math.max(16, pieRadius.value * 0.58))

const barColors = ['#4f9cf9','#56c490','#f97f4f','#c45ef9','#f9d44f','#4fc8f9','#f9584f','#9cf94f']
const pieColors = barColors

const pieSlices = computed(() => {
  const xi = colIdx(xCol.value)
  const yi = colIdx(yCol.value)
  if (xi < 0 || yi < 0) return []
  const values = chartData.value
    .slice(0, 20)
    .map(d => ({ ...d, y: Math.max(0, d.y) }))
    .filter(d => d.y > 0)
  const total = values.reduce((s, d) => s + d.y, 0) || 1
  let angle = -Math.PI / 2
  const r = pieRadius.value
  return values.map(d => {
    const sweep = (d.y / total) * Math.PI * 2
    const x1 = r * Math.cos(angle)
    const y1 = r * Math.sin(angle)
    const x2 = r * Math.cos(angle + sweep)
    const y2 = r * Math.sin(angle + sweep)
    const large = sweep > Math.PI ? 1 : 0
    const path = `M 0 0 L ${x1} ${y1} A ${r} ${r} 0 ${large} 1 ${x2} ${y2} Z`
    angle += sweep
    return { label: d.x, value: d.y, pct: ((d.y / total) * 100).toFixed(1), path }
  })
})

function fmtNum(n: number) {
  if (Math.abs(n) >= 1e6) return (n / 1e6).toFixed(1) + 'M'
  if (Math.abs(n) >= 1e3) return (n / 1e3).toFixed(1) + 'K'
  return Number.isInteger(n) ? String(n) : n.toFixed(2)
}
function truncate(s: string, max: number) {
  return s.length > max ? s.slice(0, max) + '…' : s
}
const _midnightRe2 = /^(\d{4}-\d{2}-\d{2})[T ]00:00:00/
const _tsRe2 = /^(\d{4}-\d{2}-\d{2})[T ](\d{2}:\d{2}):\d{2}/
function fmtAxisLabel(v: unknown): string {
  const s = String(v ?? '').trim()
  if (_midnightRe2.test(s)) return s.slice(0, 10)
  const ts = _tsRe2.exec(s)
  if (ts) return `${ts[1]} ${ts[2]}`
  return s
}

const canvasRef = ref<HTMLElement>()

onMounted(() => {
  if (!canvasRef.value) return
  resizeObserver = new ResizeObserver((entries) => {
    const rect = entries[0]?.contentRect
    if (rect) {
      canvasW.value = rect.width || 720
      canvasH.value = rect.height || 260
    }
  })
  resizeObserver.observe(canvasRef.value)
})

onUnmounted(() => {
  resizeObserver?.disconnect()
})
</script>

<style scoped>
.rc-root { display: flex; flex-direction: column; gap: 10px; flex: 1; min-height: 0; }
.rc-controls { display: flex; gap: 14px; align-items: center; flex-wrap: wrap; padding: 0 4px; }
.rc-ctrl-group { display: flex; align-items: center; gap: 6px; }
.rc-ctrl-group label { font-size: 11px; color: var(--text-muted); white-space: nowrap; }
.rc-tabs { display: flex; flex-wrap: wrap; border: 1px solid var(--border); border-radius: 6px; overflow: hidden; max-width: min(100%, 520px); }
.rc-tab { padding: 3px 9px; font-size: 12px; background: transparent; border: none; color: var(--text-muted); cursor: pointer; }
.rc-tab.active { background: var(--accent); color: #fff; }
.rc-select { background: var(--bg-panel); border: 1px solid var(--border); border-radius: 4px; color: var(--text-primary); font-size: 12px; padding: 3px 6px; }
.rc-canvas { flex: 1; min-height: 0; width: 100%; height: 100%; display: flex; align-items: stretch; position: relative; overflow: hidden; }
.rc-svg { display: block; width: 100%; height: 100%; overflow: visible; }
.rc-bar { opacity: 0.82; cursor: pointer; transition: opacity .15s; }
.rc-bar:hover { opacity: 1; }
.rc-bar-v {
  transform-box: fill-box;
  transform-origin: center bottom;
  animation: rc-grow-y 620ms cubic-bezier(.22, 1, .36, 1) both;
  animation-delay: var(--rc-delay, 0ms);
}
.rc-bar-h {
  transform-box: fill-box;
  transform-origin: left center;
  animation: rc-grow-x 620ms cubic-bezier(.22, 1, .36, 1) both;
  animation-delay: var(--rc-delay, 0ms);
}
.rc-line-path {
  stroke-dasharray: 1;
  stroke-dashoffset: 1;
  animation: rc-line-draw 850ms cubic-bezier(.22, 1, .36, 1) 80ms both;
}
.rc-area-fill {
  transform-box: fill-box;
  transform-origin: center bottom;
  animation: rc-area-rise 780ms cubic-bezier(.22, 1, .36, 1) 120ms both;
}
.rc-point {
  transform-box: fill-box;
  transform-origin: center;
  animation: rc-point-pop 460ms cubic-bezier(.16, 1, .3, 1) both;
  animation-delay: var(--rc-delay, 0ms);
}
.rc-slice {
  transform-box: fill-box;
  transform-origin: center;
  animation: rc-slice-in 520ms cubic-bezier(.22, 1, .36, 1) both;
  animation-delay: var(--rc-delay, 0ms);
}
.rc-donut-hole {
  transform-box: fill-box;
  transform-origin: center;
  animation: rc-point-pop 420ms cubic-bezier(.16, 1, .3, 1) 180ms both;
}
.rc-axis-label { font-size: 10px; fill: var(--text-muted); font-family: inherit; letter-spacing: 0; }
.rc-empty { color: var(--text-muted); font-size: 13px; }

@keyframes rc-grow-y {
  from { transform: scaleY(0); opacity: .18; }
  to { transform: scaleY(1); opacity: .82; }
}

@keyframes rc-grow-x {
  from { transform: scaleX(0); opacity: .18; }
  to { transform: scaleX(1); opacity: .82; }
}

@keyframes rc-line-draw {
  from { stroke-dashoffset: 1; opacity: .35; }
  to { stroke-dashoffset: 0; opacity: 1; }
}

@keyframes rc-area-rise {
  from { transform: scaleY(.35); opacity: 0; }
  to { transform: scaleY(1); opacity: 1; }
}

@keyframes rc-point-pop {
  from { transform: scale(.35); opacity: 0; }
  to { transform: scale(1); opacity: 1; }
}

@keyframes rc-slice-in {
  from { transform: scale(.82); opacity: 0; }
  to { transform: scale(1); opacity: .85; }
}

@media (prefers-reduced-motion: reduce) {
  .rc-bar-v,
  .rc-bar-h,
  .rc-line-path,
  .rc-area-fill,
  .rc-point,
  .rc-slice,
  .rc-donut-hole {
    animation: none !important;
  }
}
</style>
