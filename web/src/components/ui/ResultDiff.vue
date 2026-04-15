<template>
  <div class="rd-root">
    <div class="rd-header">
      <span class="rd-stat added">+{{ stats.added }} added</span>
      <span class="rd-stat removed">-{{ stats.removed }} removed</span>
      <span class="rd-stat changed">~ {{ stats.changed }} changed</span>
      <span class="rd-stat same">= {{ stats.same }} same</span>
    </div>
    <div class="rd-table-wrap">
      <table class="rd-table">
        <thead>
          <tr>
            <th class="rd-status-col">Status</th>
            <th v-for="c in allCols" :key="c">{{ c }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, i) in diffRows" :key="i" :class="'rd-row--' + row.status">
            <td class="rd-status-cell">
              <span class="rd-badge" :class="'rd-badge--' + row.status">
                {{ statusLabel(row.status) }}
              </span>
            </td>
            <td v-for="c in allCols" :key="c"
              :class="{ 'rd-cell--changed': row.changedCols?.includes(c) }">
              <template v-if="row.status === 'changed' && row.changedCols?.includes(c)">
                <span class="rd-before">{{ fmt(row.before?.[c]) }}</span>
                <span class="rd-arrow">→</span>
                <span class="rd-after">{{ fmt(row.after?.[c]) }}</span>
              </template>
              <template v-else>{{ fmt(row.data?.[c]) }}</template>
            </td>
          </tr>
          <tr v-if="diffRows.length === 0">
            <td :colspan="allCols.length + 1" class="rd-empty">Results are identical</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

interface ResultSet {
  columns: string[]
  rows: unknown[][]
}

const props = defineProps<{
  left: ResultSet
  right: ResultSet
  pkCol?: string  // column to use as primary key for matching; falls back to first col
}>()

type DiffStatus = 'added' | 'removed' | 'changed' | 'same'
interface DiffRow {
  status: DiffStatus
  data?: Record<string, unknown>
  before?: Record<string, unknown>
  after?: Record<string, unknown>
  changedCols?: string[]
}

const allCols = computed(() => {
  const set = new Set([...props.left.columns, ...props.right.columns])
  return [...set]
})

function rowToMap(cols: string[], row: unknown[]): Record<string, unknown> {
  const m: Record<string, unknown> = {}
  cols.forEach((c, i) => { m[c] = (row as unknown[])[i] })
  return m
}

function getKey(m: Record<string, unknown>): string {
  const pk = props.pkCol || Object.keys(m)[0] || ''
  return String(m[pk] ?? JSON.stringify(Object.values(m).slice(0, 3)))
}

const diffRows = computed((): DiffRow[] => {
  const leftMap = new Map<string, Record<string, unknown>>()
  const rightMap = new Map<string, Record<string, unknown>>()

  props.left.rows.forEach(r => {
    const m = rowToMap(props.left.columns, r as unknown[])
    leftMap.set(getKey(m), m)
  })
  props.right.rows.forEach(r => {
    const m = rowToMap(props.right.columns, r as unknown[])
    rightMap.set(getKey(m), m)
  })

  const result: DiffRow[] = []
  const rightKeys = new Set(rightMap.keys())

  for (const [k, leftRow] of leftMap) {
    if (!rightMap.has(k)) {
      result.push({ status: 'removed', data: leftRow })
    } else {
      const rightRow = rightMap.get(k)!
      rightKeys.delete(k)
      const changed: string[] = []
      for (const col of allCols.value) {
        if (String(leftRow[col] ?? '') !== String(rightRow[col] ?? '')) {
          changed.push(col)
        }
      }
      if (changed.length > 0) {
        result.push({ status: 'changed', before: leftRow, after: rightRow, changedCols: changed })
      } else {
        result.push({ status: 'same', data: leftRow })
      }
    }
  }

  for (const k of rightKeys) {
    result.push({ status: 'added', data: rightMap.get(k)! })
  }

  return result.filter(r => r.status !== 'same' || false)
})

const stats = computed(() => {
  const all = diffRows.value
  return {
    added: all.filter(r => r.status === 'added').length,
    removed: all.filter(r => r.status === 'removed').length,
    changed: all.filter(r => r.status === 'changed').length,
    same: props.left.rows.length - all.filter(r => r.status !== 'same').length,
  }
})

function statusLabel(s: DiffStatus) {
  return { added: '+ added', removed: '− removed', changed: '~ changed', same: '= same' }[s]
}
function fmt(v: unknown) {
  if (v === null || v === undefined) return '∅'
  return String(v)
}
</script>

<style scoped>
.rd-root { display: flex; flex-direction: column; height: 100%; min-height: 0; }
.rd-header { display: flex; gap: 12px; padding: 8px 12px; background: var(--bg-panel); border-bottom: 1px solid var(--border); font-size: 12px; }
.rd-stat { font-weight: 600; }
.rd-stat.added  { color: #56c490; }
.rd-stat.removed { color: #f97f4f; }
.rd-stat.changed { color: #f9d44f; }
.rd-stat.same   { color: var(--text-muted); }
.rd-table-wrap { flex: 1; overflow: auto; min-height: 0; }
.rd-table { width: 100%; border-collapse: collapse; font-size: 12px; }
.rd-table th { position: sticky; top: 0; background: var(--bg-sidebar); padding: 6px 10px; text-align: left; font-weight: 600; color: var(--text-muted); border-bottom: 1px solid var(--border); white-space: nowrap; }
.rd-table td { padding: 5px 10px; border-bottom: 1px solid var(--border); color: var(--text-primary); white-space: nowrap; max-width: 240px; overflow: hidden; text-overflow: ellipsis; }
.rd-status-col { width: 90px; }
.rd-status-cell { white-space: nowrap; }
.rd-badge { font-size: 10px; font-weight: 700; padding: 2px 6px; border-radius: 4px; text-transform: uppercase; letter-spacing: .3px; }
.rd-badge--added   { background: rgba(86,196,144,.2); color: #56c490; }
.rd-badge--removed { background: rgba(249,127,79,.2); color: #f97f4f; }
.rd-badge--changed { background: rgba(249,212,79,.2); color: #c9a800; }
.rd-badge--same    { background: var(--bg-panel); color: var(--text-muted); }
.rd-row--added   td { background: rgba(86,196,144,.06); }
.rd-row--removed td { background: rgba(249,127,79,.06); }
.rd-row--changed td { background: rgba(249,212,79,.04); }
.rd-cell--changed { background: rgba(249,212,79,.15) !important; }
.rd-before { color: #f97f4f; text-decoration: line-through; }
.rd-after  { color: #56c490; }
.rd-arrow  { color: var(--text-muted); margin: 0 4px; }
.rd-empty  { text-align: center; padding: 24px; color: var(--text-muted); }
</style>
