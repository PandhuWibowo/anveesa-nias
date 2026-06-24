<script setup lang="ts">
import { ref, computed, watch, nextTick, onBeforeUnmount } from 'vue'

interface Opt {
  value: string | number
  label: string
}

const props = defineProps<{
  modelValue: string | number | null
  options: Opt[]
  placeholder?: string
  disabled?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [v: string | number] }>()

const open = ref(false)
const query = ref('')
const activeIdx = ref(0)
const rootEl = ref<HTMLElement>()
const inputEl = ref<HTMLInputElement>()

const selected = computed(() => props.options.find((o) => o.value === props.modelValue))
const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return props.options
  return props.options.filter((o) => o.label.toLowerCase().includes(q))
})

function toggle() {
  if (props.disabled) return
  open.value = !open.value
}

function pick(o: Opt) {
  emit('update:modelValue', o.value)
  open.value = false
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    open.value = false
  } else if (e.key === 'ArrowDown') {
    e.preventDefault()
    activeIdx.value = Math.min(activeIdx.value + 1, filtered.value.length - 1)
    scrollActive()
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    activeIdx.value = Math.max(activeIdx.value - 1, 0)
    scrollActive()
  } else if (e.key === 'Enter') {
    e.preventDefault()
    const o = filtered.value[activeIdx.value]
    if (o) pick(o)
  }
}

function scrollActive() {
  nextTick(() => {
    const el = rootEl.value?.querySelector('.ss-opt--active') as HTMLElement | null
    el?.scrollIntoView({ block: 'nearest' })
  })
}

function onDocPointer(e: MouseEvent) {
  if (rootEl.value && !rootEl.value.contains(e.target as Node)) open.value = false
}

watch(open, (v) => {
  if (v) {
    query.value = ''
    activeIdx.value = Math.max(
      0,
      filtered.value.findIndex((o) => o.value === props.modelValue),
    )
    document.addEventListener('mousedown', onDocPointer)
    nextTick(() => inputEl.value?.focus())
  } else {
    document.removeEventListener('mousedown', onDocPointer)
  }
})
watch(query, () => {
  activeIdx.value = 0
})

onBeforeUnmount(() => document.removeEventListener('mousedown', onDocPointer))
</script>

<template>
  <div ref="rootEl" class="ss" :class="{ 'ss--disabled': disabled }">
    <button type="button" class="ss-trigger base-input" :disabled="disabled" @click="toggle">
      <span class="ss-label" :class="{ 'ss-label--ph': !selected }">{{ selected?.label || placeholder || 'Select…' }}</span>
      <span class="ss-caret">▾</span>
    </button>
    <div v-if="open" class="ss-panel">
      <input
        ref="inputEl"
        v-model="query"
        class="ss-search"
        type="text"
        placeholder="Type to filter…"
        @keydown="onKeydown"
      />
      <div class="ss-list">
        <button
          v-for="(o, i) in filtered"
          :key="o.value"
          type="button"
          class="ss-opt"
          :class="{ 'ss-opt--active': i === activeIdx, 'ss-opt--selected': o.value === modelValue }"
          @mouseenter="activeIdx = i"
          @click="pick(o)"
        >
          {{ o.label }}
        </button>
        <div v-if="!filtered.length" class="ss-empty">No matches</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ss { position: relative; }
.ss-trigger { display: flex; align-items: center; justify-content: space-between; gap: 8px; width: 100%; cursor: pointer; text-align: left; }
.ss--disabled .ss-trigger { cursor: not-allowed; opacity: 0.6; }
.ss-label { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ss-label--ph { color: var(--text-muted); }
.ss-caret { color: var(--text-muted); font-size: 10px; flex-shrink: 0; }

.ss-panel {
  position: absolute;
  z-index: 50;
  top: calc(100% + 4px);
  left: 0;
  right: 0;
  min-width: 220px;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  box-shadow: var(--shadow-lg);
  overflow: hidden;
}
.ss-search {
  width: 100%;
  border: none;
  border-bottom: 1px solid var(--border);
  padding: 9px 12px;
  font-size: 13px;
  background: transparent;
  color: var(--text-primary);
  outline: none;
}
.ss-list { max-height: 320px; overflow-y: auto; padding: 4px; }
.ss-opt {
  display: block;
  width: 100%;
  text-align: left;
  background: none;
  border: none;
  padding: 7px 10px;
  font-size: 12.5px;
  color: var(--text-secondary);
  cursor: pointer;
  border-radius: var(--r-xs);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  font-family: var(--mono);
}
.ss-opt--active { background: var(--bg-hover); color: var(--text-primary); }
.ss-opt--selected { color: var(--brand); font-weight: 600; }
.ss-empty { padding: 12px; text-align: center; color: var(--text-muted); font-size: 12px; }
</style>
