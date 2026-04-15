<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useConnections, type Connection } from '@/composables/useConnections'

const props = defineProps<{
  modelValue: number | null
  placeholder?: string
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', id: number | null): void
}>()

const { connections } = useConnections()
const open = ref(false)
const wrapRef = ref<HTMLElement | null>(null)

const selected = computed<Connection | null>(() =>
  props.modelValue != null
    ? (connections.value.find(c => c.id === props.modelValue) ?? null)
    : null
)

const driverColors: Record<string, string> = {
  postgres: '#336791',
  mysql:    '#f29111',
  sqlite:   '#7bc8f6',
  mssql:    '#cc2927',
}
const driverLabels: Record<string, string> = {
  postgres: 'PG',
  mysql:    'MY',
  sqlite:   'SQ',
  mssql:    'MS',
}

function pick(conn: Connection) {
  emit('update:modelValue', conn.id)
  open.value = false
}

function clear() {
  emit('update:modelValue', null)
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
  <div class="cp-wrap" ref="wrapRef">
    <!-- Trigger -->
    <button
      class="cp-trigger"
      :class="{ 'cp-trigger--open': open, 'cp-trigger--empty': !selected }"
      @click="open = !open"
      type="button"
    >
      <span
        v-if="selected"
        class="cp-badge"
        :style="{ background: driverColors[selected.driver] ?? '#555' }"
      >{{ driverLabels[selected.driver] ?? '??' }}</span>
      <svg v-else width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="cp-icon-plug">
        <path d="M18.36 6.64a9 9 0 1 1-12.73 0"/><line x1="12" y1="2" x2="12" y2="12"/>
      </svg>
      <span class="cp-name">{{ selected ? selected.name : (placeholder ?? 'Select connection…') }}</span>
      <span v-if="selected?.host" class="cp-host">
        {{ selected.host }}{{ selected.port ? ':' + selected.port : '' }}
      </span>
      <svg class="cp-chevron" :class="{ 'cp-chevron--up': open }" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
        <polyline points="6 9 12 15 18 9"/>
      </svg>
    </button>

    <!-- Dropdown -->
    <div v-if="open" class="cp-dropdown">
      <div class="cp-list">
        <div
          v-for="conn in connections"
          :key="conn.id"
          class="cp-option"
          :class="{ 'cp-option--active': conn.id === modelValue }"
          @mousedown.prevent="pick(conn)"
        >
          <span
            class="cp-badge cp-badge--sm"
            :style="{ background: driverColors[conn.driver] ?? '#555' }"
          >{{ driverLabels[conn.driver] ?? '??' }}</span>
          <div class="cp-option-info">
            <span class="cp-option-name">{{ conn.name }}</span>
            <span class="cp-option-host" v-if="conn.host">
              {{ conn.host }}{{ conn.port ? ':' + conn.port : '' }}
              <template v-if="conn.database"> / {{ conn.database }}</template>
            </span>
          </div>
          <svg v-if="conn.id === modelValue" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" class="cp-check">
            <polyline points="20 6 9 17 4 12"/>
          </svg>
        </div>
        <div v-if="!connections.length" class="cp-empty">
          No connections configured
        </div>
      </div>
      <div v-if="modelValue != null" class="cp-footer">
        <button class="cp-clear" @mousedown.prevent="clear" type="button">
          <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
          Clear selection
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.cp-wrap {
  position: relative;
  display: inline-flex;
  flex-shrink: 0;
}

/* Trigger */
.cp-trigger {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 5px 10px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 6px;
  cursor: pointer;
  font-size: 12.5px;
  color: var(--text-primary);
  transition: border-color 0.15s, background 0.15s;
  min-width: 180px;
  max-width: 300px;
  white-space: nowrap;
  overflow: hidden;
}
.cp-trigger:hover,
.cp-trigger--open {
  border-color: var(--brand);
  background: var(--bg-surface);
  outline: none;
}
.cp-trigger--empty .cp-name {
  color: var(--text-muted);
}

/* Badge */
.cp-badge {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 16px;
  border-radius: 3px;
  font-size: 9px;
  font-weight: 700;
  letter-spacing: 0.3px;
  color: #fff;
}
.cp-badge--sm {
  width: 20px;
  height: 15px;
}

.cp-icon-plug {
  color: var(--text-muted);
  flex-shrink: 0;
}

.cp-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  font-weight: 500;
  text-align: left;
}

.cp-host {
  font-size: 11px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 110px;
  flex-shrink: 0;
}

.cp-chevron {
  flex-shrink: 0;
  color: var(--text-muted);
  transition: transform 0.15s;
}
.cp-chevron--up {
  transform: rotate(180deg);
}

/* Dropdown */
.cp-dropdown {
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
  z-index: 9999;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: 0 8px 24px rgba(0,0,0,0.28);
  min-width: 260px;
  max-width: 360px;
  overflow: hidden;
}

.cp-list {
  max-height: 260px;
  overflow-y: auto;
  padding: 4px;
}

.cp-option {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 10px;
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.1s;
}
.cp-option:hover {
  background: var(--bg-surface);
}
.cp-option--active {
  background: color-mix(in srgb, var(--brand) 12%, transparent);
}

.cp-option-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.cp-option-name {
  font-size: 12.5px;
  font-weight: 500;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cp-option-host {
  font-size: 11px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cp-check {
  color: var(--brand);
  flex-shrink: 0;
}

.cp-empty {
  padding: 20px;
  text-align: center;
  font-size: 12px;
  color: var(--text-muted);
}

.cp-footer {
  border-top: 1px solid var(--border);
  padding: 5px 6px;
}

.cp-clear {
  display: flex;
  align-items: center;
  gap: 5px;
  width: 100%;
  background: transparent;
  border: none;
  font-size: 11px;
  color: var(--text-muted);
  cursor: pointer;
  padding: 4px 6px;
  border-radius: 4px;
  transition: color 0.1s, background 0.1s;
}
.cp-clear:hover {
  color: #e55;
  background: var(--bg-surface);
}
</style>
