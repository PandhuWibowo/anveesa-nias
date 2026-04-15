<template>
  <div class="rc-root">
    <div class="rc-controls">
      <div class="rc-ctrl-group">
        <label>Chart</label>
        <div class="rc-tabs">
          <button v-for="t in ['bar','line','pie']" :key="t"
            :class="['rc-tab', { active: chartType === t }]"
            @click="chartType = t as any">{{ t }}</button>
        </div>
      </div>
      <div class="rc-ctrl-group">
        <label>X Axis</label>
        <select v-model="xCol" class="rc-select">
          <option v-for="c in columns" :key="c" :value="c">{{ c }}</option>
        </select>
      </div>
      <div class="rc-ctrl-group" v-if="chartType !== 'pie'">
        <label>Y Axis</label>
        <select v-model="yCol" class="rc-select">
          <option v-for="c in numericColumns" :key="c" :value="c">{{ c }}</option>
        </select>
      </div>
      <div class="rc-ctrl-group" v-if="chartType === 'pie'">
        <label>Value</label>
        <select v-model="yCol" class="rc-select">
          <option v-for="c in numericColumns" :key="c" :value="c">{{ c }}</option>
        </select>
      </div>
    </div>

    <div class="rc-canvas" ref="canvasRef">
      <svg v-if="chartType === 'bar'" :width="svgW" :height="svgH" class="rc-svg">
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
          :x="barX(i)" :y="yScale(d.y)"
          :width="barW" :height="Math.max(0, svgH - padB - yScale(d.y))"
          class="rc-bar" :title="`${d.x}: ${d.y}`">
          <title>{{ d.x }}: {{ d.y }}</title>
        </rect>
        <!-- X labels -->
        <text v-for="(d, i) in chartData" :key="'xl'+i"
          :x="barX(i) + barW / 2" :y="svgH - padB + 14"
          text-anchor="middle" class="rc-axis-label">
          {{ truncate(String(d.x), 10) }}
        </text>
        <!-- Axes -->
        <line :x1="padL" :x2="padL" :y1="padT" :y2="svgH - padB" stroke="var(--border)" />
        <line :x1="padL" :x2="svgW - padR" :y1="svgH - padB" :y2="svgH - padB" stroke="var(--border)" />
      </svg>

      <svg v-else-if="chartType === 'line'" :width="svgW" :height="svgH" class="rc-svg">
        <line v-for="(yt, i) in yTicks" :key="i"
          :x1="padL" :x2="svgW - padR" :y1="yScale(yt)" :y2="yScale(yt)"
          stroke="var(--border)" stroke-dasharray="3,3" />
        <text v-for="(yt, i) in yTicks" :key="'yt'+i"
          :x="padL - 6" :y="yScale(yt) + 4" text-anchor="end" class="rc-axis-label">
          {{ fmtNum(yt) }}
        </text>
        <polyline :points="linePoints" fill="none" stroke="var(--accent)" stroke-width="2" stroke-linejoin="round" />
        <polygon :points="areaPoints" fill="var(--accent)" fill-opacity="0.12" />
        <circle v-for="(d, i) in chartData" :key="i"
          :cx="lineX(i)" :cy="yScale(d.y)" r="3" fill="var(--accent)">
          <title>{{ d.x }}: {{ d.y }}</title>
        </circle>
        <text v-for="(d, i) in chartData.filter((_,ii) => ii % Math.max(1, Math.floor(chartData.length/10)) === 0)" :key="'xl'+i"
          :x="lineX(chartData.indexOf(d))" :y="svgH - padB + 14"
          text-anchor="middle" class="rc-axis-label">
          {{ truncate(String(d.x), 8) }}
        </text>
        <line :x1="padL" :x2="padL" :y1="padT" :y2="svgH - padB" stroke="var(--border)" />
        <line :x1="padL" :x2="svgW - padR" :y1="svgH - padB" :y2="svgH - padB" stroke="var(--border)" />
      </svg>

      <svg v-else-if="chartType === 'pie'" :width="svgW" :height="svgH" class="rc-svg">
        <g :transform="`translate(${pieCenter.x},${pieCenter.y})`">
          <path v-for="(s, i) in pieSlices" :key="i"
            :d="s.path"
            :fill="pieColors[i % pieColors.length]"
            :opacity="0.85"
            stroke="var(--bg-sidebar)" stroke-width="1.5">
            <title>{{ s.label }}: {{ fmtNum(s.value) }} ({{ s.pct }}%)</title>
          </path>
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
import { ref, computed, watch } from 'vue'

const props = defineProps<{
  columns: string[]
  rows: unknown[][]
}>()

type ChartType = 'bar' | 'line' | 'pie'
const chartType = ref<ChartType>('bar')
const xCol = ref(props.columns[0] ?? '')
const yCol = ref('')

const svgW = 720
const svgH = 300
const padL = 52, padR = 140, padT = 16, padB = 36

watch(() => props.columns, cols => {
  if (!xCol.value && cols[0]) xCol.value = cols[0]
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

const yMin = computed(() => Math.min(0, ...chartData.value.map(d => d.y)))
const yMax = computed(() => Math.max(...chartData.value.map(d => d.y), 1))

function yScale(v: number) {
  const range = yMax.value - yMin.value || 1
  return padT + (svgH - padT - padB) * (1 - (v - yMin.value) / range)
}

const yTicks = computed(() => {
  const count = 5
  const step = (yMax.value - yMin.value) / count
  return Array.from({ length: count + 1 }, (_, i) => yMin.value + i * step)
})

const barW = computed(() => {
  if (!chartData.value.length) return 0
  const avail = svgW - padL - padR
  return Math.min(40, Math.max(4, avail / chartData.value.length * 0.7))
})

function barX(i: number) {
  const avail = svgW - padL - padR
  const slot = avail / (chartData.value.length || 1)
  return padL + slot * i + (slot - barW.value) / 2
}

function lineX(i: number) {
  const avail = svgW - padL - padR
  if (chartData.value.length <= 1) return padL
  return padL + (avail / (chartData.value.length - 1)) * i
}

const linePoints = computed(() => chartData.value.map((d, i) => `${lineX(i)},${yScale(d.y)}`).join(' '))
const areaPoints = computed(() => {
  if (!chartData.value.length) return ''
  const pts = chartData.value.map((d, i) => `${lineX(i)},${yScale(d.y)}`).join(' ')
  const last = chartData.value.length - 1
  return `${padL},${svgH - padB} ${pts} ${lineX(last)},${svgH - padB}`
})

const pieCenter = computed(() => ({ x: Math.min(svgW / 2 - 30, svgW - padR - 80), y: svgH / 2 }))
const pieRadius = computed(() => Math.min(svgH / 2 - padT - 8, 110))

const pieColors = ['#4f9cf9','#56c490','#f97f4f','#c45ef9','#f9d44f','#4fc8f9','#f9584f','#9cf94f']

const pieSlices = computed(() => {
  const xi = colIdx(xCol.value)
  const yi = colIdx(yCol.value)
  if (xi < 0 || yi < 0) return []
  const total = chartData.value.reduce((s, d) => s + d.y, 0) || 1
  let angle = -Math.PI / 2
  const r = pieRadius.value
  const cx = pieCenter.value.x
  const cy = pieCenter.value.y
  return chartData.value.slice(0, 20).map(d => {
    const sweep = (d.y / total) * Math.PI * 2
    const x1 = cx + r * Math.cos(angle)
    const y1 = cy + r * Math.sin(angle)
    const x2 = cx + r * Math.cos(angle + sweep)
    const y2 = cy + r * Math.sin(angle + sweep)
    const large = sweep > Math.PI ? 1 : 0
    const path = `M ${cx} ${cy} L ${x1} ${y1} A ${r} ${r} 0 ${large} 1 ${x2} ${y2} Z`
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

const canvasRef = ref<HTMLElement>()
</script>

<style scoped>
.rc-root { display: flex; flex-direction: column; gap: 10px; height: 100%; min-height: 0; }
.rc-controls { display: flex; gap: 14px; align-items: center; flex-wrap: wrap; padding: 0 4px; }
.rc-ctrl-group { display: flex; align-items: center; gap: 6px; }
.rc-ctrl-group label { font-size: 11px; color: var(--text-muted); white-space: nowrap; }
.rc-tabs { display: flex; border: 1px solid var(--border); border-radius: 6px; overflow: hidden; }
.rc-tab { padding: 3px 10px; font-size: 12px; background: transparent; border: none; color: var(--text-muted); cursor: pointer; text-transform: capitalize; }
.rc-tab.active { background: var(--accent); color: #fff; }
.rc-select { background: var(--bg-panel); border: 1px solid var(--border); border-radius: 4px; color: var(--text-primary); font-size: 12px; padding: 3px 6px; }
.rc-canvas { flex: 1; min-height: 0; display: flex; align-items: center; justify-content: center; position: relative; }
.rc-svg { overflow: visible; }
.rc-bar { fill: var(--accent); opacity: 0.8; cursor: pointer; transition: opacity .15s; }
.rc-bar:hover { opacity: 1; }
.rc-axis-label { font-size: 10px; fill: var(--text-muted); font-family: var(--font-mono); }
.rc-empty { color: var(--text-muted); font-size: 13px; }
</style>
