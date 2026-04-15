<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  show: boolean
  column: string
  value: unknown
  rowIndex?: number
}>()

const emit = defineEmits<{ close: [] }>()

const displayValue = computed(() => {
  if (props.value === null || props.value === undefined) return 'NULL'
  if (typeof props.value === 'object') return JSON.stringify(props.value, null, 2)
  return String(props.value)
})

const valueType = computed(() => {
  if (props.value === null || props.value === undefined) return 'null'
  if (typeof props.value === 'number') return 'number'
  if (typeof props.value === 'boolean') return 'boolean'
  const str = String(props.value)
  try { JSON.parse(str); return 'json' } catch { /* */ }
  return 'text'
})

const isJson = computed(() => valueType.value === 'json')
const isNull = computed(() => valueType.value === 'null')

const prettyJson = computed(() => {
  if (!isJson.value) return displayValue.value
  try { return JSON.stringify(JSON.parse(String(props.value)), null, 2) } catch { return displayValue.value }
})

async function copy() {
  await navigator.clipboard.writeText(displayValue.value)
}
</script>

<template>
  <Teleport to="body">
    <div v-if="show" class="ci-overlay" @click.self="emit('close')">
      <div class="ci-modal">
        <div class="ci-header">
          <div class="ci-title">
            <span class="ci-col">{{ column }}</span>
            <span class="ci-badge" :class="`ci-badge--${valueType}`">{{ valueType }}</span>
          </div>
          <div class="ci-actions">
            <button class="ci-btn" @click="copy" title="Copy value">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
              Copy
            </button>
            <button class="ci-btn ci-btn--close" @click="emit('close')" title="Close">×</button>
          </div>
        </div>
        <div class="ci-body">
          <pre v-if="isJson" class="ci-pre ci-pre--json">{{ prettyJson }}</pre>
          <pre v-else-if="isNull" class="ci-pre ci-pre--null">NULL</pre>
          <pre v-else class="ci-pre">{{ displayValue }}</pre>
        </div>
        <div class="ci-footer">
          <span v-if="!isNull">{{ displayValue.length.toLocaleString() }} characters</span>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.ci-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}
.ci-modal {
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 8px;
  width: min(680px, 90vw);
  max-height: 70vh;
  display: flex;
  flex-direction: column;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.5);
}
.ci-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border);
  gap: 12px;
}
.ci-title {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.ci-col {
  font-weight: 600;
  font-size: 13px;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ci-badge {
  font-size: 10px;
  padding: 1px 6px;
  border-radius: 4px;
  font-family: monospace;
  flex-shrink: 0;
}
.ci-badge--number { background: rgba(59, 130, 246, 0.2); color: #60a5fa; }
.ci-badge--boolean { background: rgba(168, 85, 247, 0.2); color: #c084fc; }
.ci-badge--json { background: rgba(34, 197, 94, 0.2); color: #4ade80; }
.ci-badge--null { background: rgba(239, 68, 68, 0.15); color: #f87171; }
.ci-badge--text { background: rgba(255, 255, 255, 0.08); color: var(--text-muted); }
.ci-actions {
  display: flex;
  align-items: center;
  gap: 6px;
}
.ci-btn {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 4px 10px;
  border-radius: 5px;
  border: 1px solid var(--border);
  background: transparent;
  color: var(--text-secondary);
  font-size: 12px;
  cursor: pointer;
  transition: all 0.15s;
}
.ci-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
.ci-btn--close {
  padding: 4px 8px;
  font-size: 16px;
  line-height: 1;
}
.ci-body {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 16px;
}
.ci-pre {
  margin: 0;
  font-family: "JetBrains Mono", "Fira Mono", monospace;
  font-size: 12.5px;
  line-height: 1.6;
  color: var(--text-primary);
  white-space: pre-wrap;
  word-break: break-all;
}
.ci-pre--json { color: #4ade80; }
.ci-pre--null { color: #f87171; font-style: italic; }
.ci-footer {
  padding: 8px 16px;
  border-top: 1px solid var(--border);
  font-size: 11px;
  color: var(--text-muted);
}
</style>
