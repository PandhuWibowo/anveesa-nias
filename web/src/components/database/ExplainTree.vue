<script setup lang="ts">
import { computed } from 'vue'

interface ExplainNode {
  'Node Type'?: string
  'Startup Cost'?: number
  'Total Cost'?: number
  'Plan Rows'?: number
  'Actual Rows'?: number
  Plans?: ExplainNode[]
  [k: string]: unknown
}

interface FlatNode {
  node: ExplainNode
  depth: number
  totalCost: number
}

interface Props {
  result: {
    driver?: string
    format?: string
    json?: unknown
    raw?: unknown[][]
    error?: string
  } | null
}

const props = defineProps<Props>()

function flattenPg(node: ExplainNode, depth = 0, out: FlatNode[] = []): FlatNode[] {
  out.push({ node, depth, totalCost: (node['Total Cost'] as number) ?? 0 })
  for (const child of (node.Plans ?? []) as ExplainNode[]) {
    flattenPg(child, depth + 1, out)
  }
  return out
}

const pgRoot = computed<ExplainNode | null>(() => {
  if (!props.result?.json) return null
  try {
    const arr = props.result.json as Array<{ Plan: ExplainNode }>
    if (Array.isArray(arr) && arr[0]?.Plan) return arr[0].Plan
  } catch {}
  return null
})

const pgNodes = computed<FlatNode[]>(() => pgRoot.value ? flattenPg(pgRoot.value) : [])

const maxCost = computed(() => Math.max(1, ...pgNodes.value.map((n) => n.totalCost)))

function barColor(cost: number): string {
  const r = cost / maxCost.value
  if (r > 0.7) return '#f87171'
  if (r > 0.3) return '#fbbf24'
  return '#4ade80'
}

const textRows = computed(() => {
  if (!props.result?.raw?.length || pgRoot.value) return null
  return props.result.raw
})
</script>

<template>
  <div class="et-root">
    <div v-if="!result" class="et-empty">Run EXPLAIN to visualize the query plan.</div>
    <div v-else-if="result.error" class="notice notice--error" style="margin:12px">{{ result.error }}</div>

    <!-- PostgreSQL / MySQL JSON tree (flattened with indentation) -->
    <template v-else-if="pgNodes.length">
      <div class="et-legend">
        <span class="et-dot" style="background:#4ade80"/> Low
        <span class="et-dot" style="background:#fbbf24"/> Medium
        <span class="et-dot" style="background:#f87171"/> High
      </div>
      <div class="et-list">
        <div
          v-for="(item, i) in pgNodes"
          :key="i"
          class="et-node"
          :style="{ paddingLeft: 12 + item.depth * 20 + 'px' }"
        >
          <div class="et-node-dot" :style="{ background: barColor(item.totalCost) }" />
          <div class="et-node-body">
            <span class="et-node-type">{{ item.node['Node Type'] ?? 'Node' }}</span>
            <span v-if="item.totalCost" class="et-meta">cost={{ item.totalCost.toFixed(2) }}</span>
            <span v-if="item.node['Plan Rows']" class="et-meta">rows={{ item.node['Plan Rows'] }}</span>
            <span v-if="item.node['Actual Rows'] != null" class="et-meta et-actual">actual={{ item.node['Actual Rows'] }}</span>
          </div>
          <div class="et-bar-wrap">
            <div
              class="et-bar"
              :style="{ width: Math.max(4, (item.totalCost / maxCost) * 100) + '%', background: barColor(item.totalCost) }"
            />
          </div>
        </div>
      </div>
    </template>

    <!-- Text fallback -->
    <template v-else-if="textRows">
      <div class="et-list">
        <div v-for="(row, i) in textRows" :key="i" class="et-text-row">
          {{ Array.isArray(row) ? row.join(' | ') : row }}
        </div>
      </div>
    </template>

    <div v-else class="et-empty">No plan data.</div>
  </div>
</template>

<style scoped>
.et-root { flex: 1; min-height: 0; overflow: auto; padding: 12px 14px; display: flex; flex-direction: column; gap: 8px; }
.et-empty { color: var(--text-muted); font-size: 12.5px; text-align: center; padding: 24px; }
.et-legend { display: flex; align-items: center; gap: 10px; font-size: 11px; color: var(--text-muted); }
.et-dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; }
.et-list { display: flex; flex-direction: column; gap: 4px; }
.et-node {
  display: flex; align-items: center; gap: 8px;
  padding-top: 6px; padding-bottom: 6px; padding-right: 12px;
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 7px; min-height: 36px;
}
.et-node-dot { width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0; }
.et-node-body { display: flex; align-items: center; gap: 10px; flex: 1; flex-wrap: wrap; }
.et-node-type { font-weight: 700; font-size: 12.5px; color: var(--text-primary); }
.et-meta { font-size: 11px; color: var(--text-muted); font-family: var(--mono, monospace); }
.et-actual { color: #a78bfa; }
.et-bar-wrap { width: 80px; height: 6px; background: var(--bg-body); border-radius: 3px; overflow: hidden; flex-shrink: 0; }
.et-bar { height: 100%; border-radius: 3px; opacity: 0.8; }
.et-text-row { padding: 5px 10px; font-family: var(--mono, monospace); font-size: 12px; color: var(--text-primary); border-bottom: 1px solid var(--border); }
</style>
