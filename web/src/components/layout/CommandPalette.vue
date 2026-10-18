<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import { useTheme } from '@/composables/useTheme'
import { useConnections } from '@/composables/useConnections'
import { useCommandPalette } from '@/composables/useCommandPalette'
import { NAV_GROUPS, navKey, type NavItem } from '@/navigation'
import { DRIVER_COLOR, DRIVER_NAME, connectionRoute } from '@/utils/drivers'
import NavIcon from '@/components/ui/NavIcon.vue'
import DriverIcon from '@/components/ui/DriverIcon.vue'

const emit = defineEmits<{ (e: 'select-conn', id: number): void }>()

const router = useRouter()
const { authEnabled, hasAnyPermission, logout } = useAuth()
const { mode, toggleTheme } = useTheme()
const { connections } = useConnections()
const { open, recent, hide } = useCommandPalette()

const query = ref('')
const cursor = ref(0)
const inputEl = ref<HTMLInputElement | null>(null)
const listEl = ref<HTMLElement | null>(null)

type Entry = {
  id: string
  section: string
  label: string
  hint: string
  icon?: string
  driver?: string
  run: () => void
}

const pages = computed<Array<NavItem & { group: string }>>(() =>
  NAV_GROUPS.flatMap((g) =>
    g.items
      .filter((i) => !i.permissionsAny?.length || hasAnyPermission(i.permissionsAny))
      .map((i) => ({ ...i, group: g.label })),
  ),
)

function go(item: NavItem) {
  router.push({ name: item.name, query: item.query })
}

const pageEntries = computed<Entry[]>(() =>
  pages.value.map((p) => ({
    id: `page:${navKey(p)}`,
    section: 'Pages',
    label: p.label,
    hint: p.group,
    icon: p.icon,
    run: () => go(p),
  })),
)

const connEntries = computed<Entry[]>(() =>
  connections.value.map((c) => ({
    id: `conn:${c.id}`,
    section: 'Connections',
    label: c.name,
    hint: DRIVER_NAME[c.driver] ?? c.driver,
    driver: c.driver,
    run: () => {
      emit('select-conn', c.id)
      router.push({ name: connectionRoute(c.driver) })
    },
  })),
)

const actionEntries = computed<Entry[]>(() => {
  const list: Entry[] = []
  if (hasAnyPermission(['connections.view'])) {
    list.push({ id: 'act:new-conn', section: 'Actions', label: 'Add connection', hint: 'Connections', icon: 'plus', run: () => router.push({ name: 'connections', query: { new: '1' } }) })
  }
  list.push({ id: 'act:theme', section: 'Actions', label: mode.value === 'dark' ? 'Switch to light theme' : 'Switch to dark theme', hint: 'Appearance', icon: mode.value === 'dark' ? 'sun' : 'moon', run: toggleTheme })
  list.push({ id: 'act:docs', section: 'Actions', label: 'Open documentation', hint: 'Help', icon: 'book', run: () => router.push({ name: 'docs' }) })
  if (authEnabled.value) {
    list.push({ id: 'act:security', section: 'Actions', label: 'Security and 2FA', hint: 'Account', icon: 'lock', run: () => router.push({ name: 'security' }) })
    list.push({ id: 'act:logout', section: 'Actions', label: 'Sign out', hint: 'Account', icon: 'logout', run: async () => { await logout(); router.push({ name: 'login' }) } })
  }
  return list
})

// Subsequence match with a bonus for word starts — "sqs" finds "SQL Studio".
function score(text: string, q: string): number {
  const t = text.toLowerCase()
  if (t.startsWith(q)) return 100 - t.length
  const idx = t.indexOf(q)
  if (idx >= 0) return 60 - idx
  let ti = 0
  let s = 0
  for (const ch of q) {
    const found = t.indexOf(ch, ti)
    if (found < 0) return -1
    s += found === 0 || t[found - 1] === ' ' ? 3 : 1
    ti = found + 1
  }
  return s
}

const results = computed<Entry[]>(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) {
    const byId = new Map(pageEntries.value.map((e) => [e.id, e]))
    const recents = recent.value
      .map((k) => byId.get(`page:${k}`))
      .filter((e): e is Entry => !!e)
      .map((e) => ({ ...e, section: 'Recent' }))
    const recentIds = new Set(recents.map((e) => e.id))
    return [
      ...recents,
      ...connEntries.value.slice(0, 5),
      ...pageEntries.value.filter((e) => !recentIds.has(e.id)),
      ...actionEntries.value,
    ]
  }
  const all = [...pageEntries.value, ...connEntries.value, ...actionEntries.value]
  const keywordsById = new Map(pages.value.map((p) => [`page:${navKey(p)}`, `${p.keywords ?? ''} ${p.desc}`]))
  return all
    .map((e) => {
      const primary = score(e.label, q)
      const secondary = Math.max(score(e.hint, q), score(keywordsById.get(e.id) ?? '', q)) - 20
      return { e, s: Math.max(primary, secondary) }
    })
    .filter((r) => r.s > 0)
    .sort((a, b) => b.s - a.s)
    .slice(0, 30)
    .map((r) => r.e)
})

const grouped = computed(() => {
  const out: Array<{ section: string; items: Array<{ entry: Entry; index: number }> }> = []
  results.value.forEach((entry, index) => {
    let g = out.find((s) => s.section === entry.section)
    if (!g) out.push((g = { section: entry.section, items: [] }))
    g.items.push({ entry, index })
  })
  return out
})

watch(query, () => { cursor.value = 0 })

watch(open, async (v) => {
  if (!v) return
  query.value = ''
  cursor.value = 0
  await nextTick()
  inputEl.value?.focus()
})

function run(entry: Entry | undefined) {
  if (!entry) return
  hide()
  entry.run()
}

function move(delta: number) {
  const n = results.value.length
  if (!n) return
  cursor.value = (cursor.value + delta + n) % n
  nextTick(() => {
    listEl.value?.querySelector('[data-active="true"]')?.scrollIntoView({ block: 'nearest' })
  })
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'ArrowDown') { e.preventDefault(); move(1) }
  else if (e.key === 'ArrowUp') { e.preventDefault(); move(-1) }
  else if (e.key === 'Enter') { e.preventDefault(); run(results.value[cursor.value]) }
  else if (e.key === 'Escape') { e.preventDefault(); hide() }
}
</script>

<template>
  <Teleport to="body">
    <Transition name="palette">
      <div v-if="open" class="palette-backdrop" @mousedown.self="hide">
        <div class="palette" role="dialog" aria-label="Go to page or connection" @keydown="onKey">
          <div class="palette__search">
            <NavIcon name="search" :size="16" />
            <input
              ref="inputEl"
              v-model="query"
              class="palette__input"
              type="text"
              placeholder="Jump to a page, connection, or action"
              aria-label="Search"
              role="combobox"
              aria-expanded="true"
              aria-controls="palette-list"
              :aria-activedescendant="results[cursor] ? `pal-${cursor}` : undefined"
              autocomplete="off"
              spellcheck="false"
            />
            <kbd>Esc</kbd>
          </div>

          <div id="palette-list" ref="listEl" class="palette__list" role="listbox">
            <div v-if="!results.length" class="palette__empty">
              Nothing matches “{{ query }}”. Try a page name like “docker” or a connection name.
            </div>
            <div v-for="group in grouped" :key="group.section" class="palette__group">
              <div class="palette__section">{{ group.section }}</div>
              <button
                v-for="{ entry, index } in group.items"
                :id="`pal-${index}`"
                :key="entry.id"
                type="button"
                role="option"
                class="palette__item"
                :data-active="index === cursor"
                :aria-selected="index === cursor"
                @mousemove="cursor = index"
                @click="run(entry)"
              >
                <span v-if="entry.driver" class="palette__driver" :style="{ background: DRIVER_COLOR[entry.driver] ?? '#4b5563' }">
                  <DriverIcon :driver="entry.driver as any" :size="14" />
                </span>
                <span v-else class="palette__icon"><NavIcon :name="entry.icon || 'dashboard'" :size="16" /></span>
                <span class="palette__label">{{ entry.label }}</span>
                <span class="palette__hint">{{ entry.hint }}</span>
              </button>
            </div>
          </div>

          <div class="palette__foot">
            <span><kbd>↑</kbd><kbd>↓</kbd> move</span>
            <span><kbd>↵</kbd> open</span>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.palette-backdrop {
  position: fixed;
  inset: 0;
  z-index: 1000;
  background: var(--overlay);
  display: flex;
  justify-content: center;
  align-items: flex-start;
  padding: min(14vh, 120px) 16px 16px;
}
.palette {
  width: 100%;
  max-width: 600px;
  max-height: min(560px, calc(100dvh - 140px));
  display: flex;
  flex-direction: column;
  background: var(--bg-elevated);
  border: 1px solid var(--border-2);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-lg);
  overflow: hidden;
}
.palette__search {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 14px;
  border-bottom: 1px solid var(--border);
  color: var(--text-muted);
}
.palette__input {
  flex: 1;
  min-width: 0;
  height: 52px;
  border: none;
  outline: none;
  background: transparent;
  color: var(--text-primary);
  font: inherit;
  font-size: 15px;
}
.palette__input::placeholder { color: var(--text-muted); }
.palette__list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 6px;
}
.palette__group + .palette__group { margin-top: 4px; }
.palette__section {
  padding: 8px 10px 4px;
  font-size: 12px;
  font-weight: 500;
  color: var(--text-muted);
}
.palette__item {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border: none;
  border-radius: var(--r-sm);
  background: transparent;
  color: var(--text-secondary);
  font: inherit;
  font-size: 14px;
  text-align: left;
  cursor: pointer;
}
.palette__item[data-active='true'] {
  background: var(--bg-hover-strong);
  color: var(--text-primary);
}
.palette__icon {
  display: flex;
  color: var(--text-muted);
}
.palette__item[data-active='true'] .palette__icon { color: var(--brand); }
.palette__driver {
  width: 20px;
  height: 20px;
  border-radius: 5px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.palette__label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.palette__hint {
  flex-shrink: 0;
  font-size: 12px;
  color: var(--text-muted);
}
.palette__empty {
  padding: 28px 16px;
  text-align: center;
  font-size: 13px;
  color: var(--text-muted);
}
.palette__foot {
  display: flex;
  gap: 16px;
  padding: 8px 14px;
  border-top: 1px solid var(--border);
  font-size: 12px;
  color: var(--text-muted);
}
.palette__foot kbd + kbd { margin-left: 2px; }
.palette__foot kbd { margin-right: 4px; }

.palette-enter-active,
.palette-leave-active { transition: opacity 120ms var(--ease); }
.palette-enter-active .palette,
.palette-leave-active .palette { transition: transform 120ms var(--ease); }
.palette-enter-from,
.palette-leave-to { opacity: 0; }
.palette-enter-from .palette,
.palette-leave-to .palette { transform: translateY(-6px) scale(0.99); }

@media (prefers-reduced-motion: reduce) {
  .palette-enter-active, .palette-leave-active,
  .palette-enter-active .palette, .palette-leave-active .palette { transition: none; }
}
</style>
