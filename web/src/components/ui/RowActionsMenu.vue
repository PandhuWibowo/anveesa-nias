<script setup lang="ts">
import { ref, computed, onBeforeUnmount } from 'vue'
import ActionIcon from '@/components/ui/ActionIcon.vue'

export interface RowAction {
  key: string
  label: string
  icon: string
  primary?: boolean
  danger?: boolean
  disabled?: boolean
  onClick: () => void
}

const props = defineProps<{ actions: RowAction[] }>()

const open = ref(false)
const root = ref<HTMLElement | null>(null)

const primaryActions = computed(() => props.actions.filter((a) => a.primary))
const overflowActions = computed(() => props.actions.filter((a) => !a.primary))

function onDocClick(ev: MouseEvent) {
  if (root.value && !root.value.contains(ev.target as Node)) close()
}
function onKeydown(ev: KeyboardEvent) {
  if (ev.key === 'Escape') close()
}
function toggle() {
  if (open.value) { close(); return }
  open.value = true
  document.addEventListener('click', onDocClick, true)
  document.addEventListener('keydown', onKeydown)
}
function close() {
  open.value = false
  document.removeEventListener('click', onDocClick, true)
  document.removeEventListener('keydown', onKeydown)
}
function run(action: RowAction) {
  close()
  if (!action.disabled) action.onClick()
}
onBeforeUnmount(close)
</script>

<template>
  <div class="row-actions" @click.stop>
    <button
      v-for="a in primaryActions"
      :key="a.key"
      type="button"
      class="icon-btn"
      :class="{ danger: a.danger }"
      :disabled="a.disabled"
      :title="a.label"
      :aria-label="a.label"
      @click="run(a)"
    >
      <ActionIcon :name="a.icon" />
    </button>

    <div v-if="overflowActions.length" ref="root" class="row-actions__menu">
      <button type="button" class="icon-btn" title="More actions" aria-label="More actions" @click="toggle">
        <ActionIcon name="more" />
      </button>
      <div v-if="open" class="row-actions__dropdown">
        <button
          v-for="a in overflowActions"
          :key="a.key"
          type="button"
          class="row-actions__item"
          :class="{ 'row-actions__item--danger': a.danger }"
          :disabled="a.disabled"
          @click="run(a)"
        >
          <ActionIcon :name="a.icon" />
          <span>{{ a.label }}</span>
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.row-actions { display: inline-flex; align-items: center; gap: 4px; justify-content: flex-end; }
.row-actions__menu { position: relative; display: inline-flex; }
.row-actions__dropdown {
  position: absolute;
  top: 100%;
  right: 0;
  margin-top: 4px;
  min-width: 160px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  box-shadow: var(--shadow-lg);
  z-index: 200;
  padding: 4px;
  display: flex;
  flex-direction: column;
}
.row-actions__item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 7px 9px;
  border: none;
  background: transparent;
  border-radius: var(--r-sm);
  color: var(--text-primary);
  font-size: 12.5px;
  text-align: left;
  cursor: pointer;
  white-space: nowrap;
}
.row-actions__item:hover:not(:disabled) { background: var(--bg-hover); }
.row-actions__item:disabled { opacity: 0.4; cursor: default; }
.row-actions__item--danger { color: var(--danger); }
.row-actions__item--danger:hover:not(:disabled) { background: var(--danger-bg); }
</style>
