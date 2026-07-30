<script setup lang="ts">
import { ref, computed, onBeforeUnmount, nextTick } from 'vue'
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
const toggleBtn = ref<HTMLElement | null>(null)
const dropdown = ref<HTMLElement | null>(null)
const dropdownStyle = ref<Record<string, string>>({})

const primaryActions = computed(() => props.actions.filter((a) => a.primary))
const overflowActions = computed(() => props.actions.filter((a) => !a.primary))

// Positioned via getBoundingClientRect + `position: fixed` (rather than
// `position: absolute` inside the row) and teleported to <body> — table rows
// commonly sit inside an `overflow: hidden`/`overflow: auto` wrapper, which
// would otherwise clip the dropdown instead of letting it float above the row.
function position() {
  const btn = toggleBtn.value
  if (!btn) return
  const rect = btn.getBoundingClientRect()
  const menuWidth = dropdown.value?.offsetWidth || 160
  const menuHeight = dropdown.value?.offsetHeight ?? 0

  // Right-align the dropdown's right edge with the button's right edge by
  // default, but clamp horizontally so it never runs off either side of the
  // viewport — a button near the left edge of a narrow card would otherwise
  // push a right-anchored dropdown partly off-screen.
  let left = rect.right - menuWidth
  left = Math.min(Math.max(4, left), window.innerWidth - menuWidth - 4)

  const spaceBelow = window.innerHeight - rect.bottom
  const openUpward = menuHeight > 0 && spaceBelow < menuHeight + 8 && rect.top > menuHeight
  dropdownStyle.value = openUpward
    ? { left: `${left}px`, right: 'auto', bottom: `${window.innerHeight - rect.top + 4}px`, top: 'auto' }
    : { left: `${left}px`, right: 'auto', top: `${rect.bottom + 4}px`, bottom: 'auto' }
}

function onDocClick(ev: MouseEvent) {
  const target = ev.target as Node
  if (toggleBtn.value?.contains(target) || dropdown.value?.contains(target)) return
  close()
}
function onKeydown(ev: KeyboardEvent) {
  if (ev.key === 'Escape') close()
}
function onScroll() {
  close()
}
async function toggleMenu() {
  if (open.value) { close(); return }
  open.value = true
  await nextTick()
  position()
  document.addEventListener('click', onDocClick, true)
  document.addEventListener('keydown', onKeydown)
  window.addEventListener('scroll', onScroll, true)
  window.addEventListener('resize', position)
}
function close() {
  open.value = false
  document.removeEventListener('click', onDocClick, true)
  document.removeEventListener('keydown', onKeydown)
  window.removeEventListener('scroll', onScroll, true)
  window.removeEventListener('resize', position)
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

    <template v-if="overflowActions.length">
      <button ref="toggleBtn" type="button" class="icon-btn" title="More actions" aria-label="More actions" @click="toggleMenu">
        <ActionIcon name="more" />
      </button>
      <Teleport to="body">
        <div v-if="open" ref="dropdown" class="row-actions__dropdown" :style="dropdownStyle">
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
      </Teleport>
    </template>
  </div>
</template>

<style scoped>
.row-actions { display: inline-flex; align-items: center; gap: 4px; justify-content: flex-end; }
</style>

<style>
/* Unscoped: this content is teleported to <body>, outside the component's
   own DOM subtree, so scoped (data-v-xxxx) selectors can't be relied on. */
.row-actions__dropdown {
  position: fixed;
  min-width: 160px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  box-shadow: var(--shadow-lg);
  z-index: 1000;
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
