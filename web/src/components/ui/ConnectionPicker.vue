<script setup lang="ts">
import { ref, computed, nextTick, watch, onMounted, onBeforeUnmount } from 'vue'
import { useConnections, type Connection, type DbDriver } from '@/composables/useConnections'
import DriverIcon from '@/components/ui/DriverIcon.vue'

const props = defineProps<{
  modelValue: number | null
  placeholder?: string
  drivers?: DbDriver[]
  // Toolbar usages (SchemaView, etc.) want the compact default width; form
  // contexts where every other field is a full-width base-input want this
  // to match instead of looking like a stray small control.
  fullWidth?: boolean
  // Hides specific connections from the list — e.g. a source/target pair
  // where whichever one is already picked on the other side shouldn't be
  // selectable again.
  excludeIds?: number[]
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', id: number | null): void
}>()

const { connections, activeConnections } = useConnections()
const open = ref(false)
const search = ref('')
const wrapRef = ref<HTMLElement | null>(null)
const triggerRef = ref<HTMLElement | null>(null)
const searchRef = ref<HTMLInputElement | null>(null)
const panelStyle = ref<Record<string, string>>({})

const selected = computed<Connection | null>(() =>
  props.modelValue != null
    ? (connections.value.find(c => c.id === props.modelValue) ?? null)
    : null
)

const driverFiltered = computed<Connection[]>(() => {
  let list = props.drivers?.length
    ? activeConnections.value.filter(c => props.drivers!.includes(c.driver))
    : activeConnections.value
  if (props.excludeIds?.length) {
    list = list.filter(c => !props.excludeIds!.includes(c.id))
  }
  return list
})

const filteredConnections = computed<Connection[]>(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return driverFiltered.value
  return driverFiltered.value.filter(c =>
    c.name.toLowerCase().includes(q) ||
    c.host?.toLowerCase().includes(q) ||
    c.database?.toLowerCase().includes(q)
  )
})

// Panel is teleported to <body> and positioned with `fixed` coordinates so it
// can't be clipped by an ancestor with overflow:hidden — e.g. .page-hero,
// which uses overflow:hidden to clip its own decorative background glow and
// was cutting this dropdown off whenever the picker sat in a page's hero
// actions bar. It also auto-flips up/down and clamps horizontally based on
// available space, matching DatabasePicker.vue's pattern.
function calcPanelStyle() {
  if (!triggerRef.value) return
  const rect = triggerRef.value.getBoundingClientRect()
  const spaceBelow = window.innerHeight - rect.bottom - 6
  const spaceAbove = rect.top - 6
  const preferBelow = spaceBelow >= spaceAbove || spaceBelow >= 200

  const width = Math.max(rect.width, 260)
  const maxLeft = Math.max(8, window.innerWidth - width - 8)
  const left = Math.min(Math.max(8, rect.left), maxLeft)

  const style: Record<string, string> = {
    position: 'fixed',
    left: `${left}px`,
    minWidth: `${width}px`,
    maxWidth: '360px',
    zIndex: '9999',
  }
  if (preferBelow) {
    style.top = `${rect.bottom + 4}px`
    style.maxHeight = `${Math.max(spaceBelow - 8, 160)}px`
  } else {
    style.bottom = `${window.innerHeight - rect.top + 4}px`
    style.maxHeight = `${Math.max(spaceAbove - 8, 160)}px`
  }
  panelStyle.value = style
}

watch(open, (isOpen) => {
  if (isOpen) {
    search.value = ''
    nextTick(() => {
      calcPanelStyle()
      searchRef.value?.focus()
    })
    window.addEventListener('scroll', calcPanelStyle, { capture: true, passive: true })
    window.addEventListener('resize', calcPanelStyle)
  } else {
    window.removeEventListener('scroll', calcPanelStyle, { capture: true })
    window.removeEventListener('resize', calcPanelStyle)
  }
})

const driverColors: Record<string, string> = {
  sqlite: '#4b5563',
  postgres: '#336791',
  mysql:    '#f29111',
  mariadb:  '#c0392b',
  mssql:    '#cc2927',
  redis:    '#c6302b',
  memcache: '#16a34a',
  kafka:    '#231f20',
  mongodb:  '#00a35c',
  cassandra: '#1f6feb',
  elasticsearch: '#00bfb3',
  opensearch: '#005eb8',
  s3_aws:   '#f59e0b',
  s3_gcp:   '#4285f4',
  s3_oss:   '#ff6a00',
  s3_obs:   '#c00000',
}
const driverLabels: Record<string, string> = {
  sqlite: 'SL',
  postgres: 'PG',
  mysql:    'MY',
  mariadb:  'MB',
  mssql:    'MS',
  redis:    'RD',
  memcache: 'MC',
  kafka:    'KF',
  mongodb:  'MG',
  cassandra: 'CA',
  elasticsearch: 'ES',
  opensearch: 'OS',
  s3_aws:   'S3',
  s3_gcp:   'GCS',
  s3_oss:   'OSS',
  s3_obs:   'OBS',
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
  const target = e.target as Node
  if (wrapRef.value?.contains(target)) return
  const panel = document.getElementById('cp-floating-dropdown')
  if (panel?.contains(target)) return
  open.value = false
}

onMounted(() => document.addEventListener('mousedown', handleOutside))
onBeforeUnmount(() => {
  document.removeEventListener('mousedown', handleOutside)
  window.removeEventListener('scroll', calcPanelStyle, { capture: true })
  window.removeEventListener('resize', calcPanelStyle)
})
</script>

<template>
  <div class="cp-wrap" :class="{ 'cp-wrap--full': fullWidth }" ref="wrapRef">
    <!-- Trigger -->
    <button
      ref="triggerRef"
      class="cp-trigger"
      :class="{ 'cp-trigger--open': open, 'cp-trigger--empty': !selected, 'cp-trigger--full': fullWidth }"
      @click="open = !open"
      type="button"
    >
      <span
        v-if="selected"
        class="cp-badge"
        :style="{ background: driverColors[selected.driver] ?? '#555' }"
      ><DriverIcon :driver="selected.driver" :size="12" /></span>
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
    <Teleport to="body">
      <div v-if="open" id="cp-floating-dropdown" class="cp-dropdown" :style="panelStyle">
        <div class="cp-search-wrap">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="cp-search-icon"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
          <input
            ref="searchRef"
            v-model="search"
            type="text"
            class="cp-search"
            placeholder="Search connections…"
            @keydown.esc="open = false"
          />
        </div>
        <div class="cp-list">
          <div
            v-for="conn in filteredConnections"
            :key="conn.id"
            class="cp-option"
            :class="{ 'cp-option--active': conn.id === modelValue }"
            @mousedown.prevent="pick(conn)"
          >
            <span
              class="cp-badge cp-badge--sm"
              :style="{ background: driverColors[conn.driver] ?? '#555' }"
            ><DriverIcon :driver="conn.driver" :size="11" /></span>
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
          <div v-if="!driverFiltered.length" class="cp-empty">
            No active connections
          </div>
          <div v-else-if="!filteredConnections.length" class="cp-empty">
            No matches for "{{ search }}"
          </div>
        </div>
        <div v-if="modelValue != null" class="cp-footer">
          <button class="cp-clear" @mousedown.prevent="clear" type="button">
            <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            Clear selection
          </button>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.cp-wrap {
  position: relative;
  display: inline-flex;
  flex-shrink: 0;
}
.cp-wrap--full {
  display: flex;
  width: 100%;
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
.cp-trigger--full {
  width: 100%;
  max-width: none;
  padding: 8px 12px;
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
  height: 22px;
  border-radius: 4px;
  color: #fff;
}
.cp-badge--sm {
  width: 20px;
  height: 20px;
  border-radius: 4px;
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
</style>

<!-- Not scoped: the dropdown is teleported to <body>, outside this
     component's scoped-style boundary, so its classes need global styling. -->
<style>
.cp-dropdown {
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: 0 8px 24px rgba(0,0,0,0.28);
  overflow: hidden;
}

.cp-search-wrap {
  position: relative;
  padding: 6px;
  border-bottom: 1px solid var(--border);
}
.cp-search-icon {
  position: absolute;
  left: 15px;
  top: 50%;
  transform: translateY(-50%);
  color: var(--text-muted);
  pointer-events: none;
}
.cp-search {
  width: 100%;
  padding: 6px 8px 6px 26px;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: 6px;
  font-size: 12.5px;
  color: var(--text-primary);
  outline: none;
}
.cp-search:focus {
  border-color: var(--brand);
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
