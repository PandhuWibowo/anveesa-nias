<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'

const props = defineProps<{
  modelValue: string
  databases: string[]
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', val: string): void
}>()

const open = ref(false)
const wrapRef = ref<HTMLElement | null>(null)

function pick(db: string) {
  emit('update:modelValue', db)
  open.value = false
}

function handleOutside(e: MouseEvent) {
  if (wrapRef.value && !wrapRef.value.contains(e.target as Node)) {
    open.value = false
  }
}

onMounted(() => document.addEventListener('mousedown', handleOutside))
onBeforeUnmount(() => document.removeEventListener('mousedown', handleOutside))
</script>

<template>
  <div class="dbpick" ref="wrapRef">
    <button
      class="dbpick__trigger"
      :class="{ 'dbpick__trigger--open': open }"
      @click="open = !open"
      type="button"
      :title="modelValue || 'Select database'"
    >
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="flex-shrink:0;color:var(--brand)">
        <ellipse cx="12" cy="5" rx="9" ry="3"/>
        <path d="M3 5V19A9 3 0 0 0 21 19V5"/>
        <path d="M3 12A9 3 0 0 0 21 12"/>
      </svg>
      <span class="dbpick__label">{{ modelValue || 'Select DB' }}</span>
      <svg class="dbpick__chevron" :class="{ 'dbpick__chevron--up': open }" width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
        <polyline points="6 9 12 15 18 9"/>
      </svg>
    </button>

    <div v-if="open" class="dbpick__dropdown">
      <div class="dbpick__list">
        <button
          v-for="db in databases"
          :key="db"
          class="dbpick__option"
          :class="{ 'dbpick__option--active': db === modelValue }"
          @mousedown.prevent="pick(db)"
          type="button"
        >
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="flex-shrink:0;color:var(--text-muted)">
            <ellipse cx="12" cy="5" rx="9" ry="3"/>
            <path d="M3 5V19A9 3 0 0 0 21 19V5"/>
            <path d="M3 12A9 3 0 0 0 21 12"/>
          </svg>
          <span class="dbpick__option-name">{{ db }}</span>
          <svg v-if="db === modelValue" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" style="color:var(--brand);flex-shrink:0">
            <polyline points="20 6 9 17 4 12"/>
          </svg>
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.dbpick {
  position: relative;
  display: inline-flex;
  flex-shrink: 0;
}

.dbpick__trigger {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 4px 8px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 5px;
  cursor: pointer;
  font-size: 12px;
  color: var(--text-primary);
  max-width: 200px;
  white-space: nowrap;
  transition: border-color 0.12s, background 0.12s;
}
.dbpick__trigger:hover,
.dbpick__trigger--open {
  border-color: var(--brand);
  background: var(--bg-surface);
}

.dbpick__label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  font-weight: 500;
  text-align: left;
}

.dbpick__chevron {
  flex-shrink: 0;
  color: var(--text-muted);
  transition: transform 0.15s;
}
.dbpick__chevron--up { transform: rotate(180deg); }

.dbpick__dropdown {
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
  z-index: 9999;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: 0 8px 24px rgba(0,0,0,0.28);
  min-width: 200px;
  max-width: 320px;
  overflow: hidden;
}

.dbpick__list {
  max-height: 280px;
  overflow-y: auto;
  padding: 4px;
}

.dbpick__option {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 6px 10px;
  border: none;
  border-radius: 5px;
  background: transparent;
  cursor: pointer;
  text-align: left;
  transition: background 0.1s;
}
.dbpick__option:hover { background: var(--bg-surface); }
.dbpick__option--active { background: var(--brand-dim); }

.dbpick__option-name {
  flex: 1;
  font-size: 12px;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.dbpick__option--active .dbpick__option-name {
  color: var(--brand);
  font-weight: 600;
}
</style>
