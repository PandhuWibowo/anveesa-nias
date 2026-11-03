<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n, type Locale } from '@/i18n'
import NavIcon from '@/components/ui/NavIcon.vue'

// Language picker. A globe works without reading any language, and each option
// is written in its own script ("English", "简体中文"), so it is findable from
// whichever language is currently showing.
const props = withDefaults(defineProps<{
  /** "icon": sidebar button. "inline": globe + current language, for the sign-in screen. */
  variant?: 'icon' | 'inline'
  /** Sidebar rail: open the menu beside the button instead of above it. */
  collapsed?: boolean
}>(), { variant: 'icon', collapsed: false })

const { t, locale, setLocale, LOCALES } = useI18n()
const open = ref(false)
const wrap = ref<HTMLElement | null>(null)
const items = ref<HTMLButtonElement[]>([])
const current = computed(() => LOCALES.find((l) => l.code === locale.value) ?? LOCALES[0])

async function toggle() {
  open.value = !open.value
  if (open.value) {
    await nextTick()
    const i = LOCALES.findIndex((l) => l.code === locale.value)
    items.value[Math.max(0, i)]?.focus()
  }
}

function pick(code: Locale) {
  setLocale(code)
  open.value = false
}

function onKey(e: KeyboardEvent) {
  if (!open.value) return
  if (e.key === 'Escape') { open.value = false; return }
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault()
    const i = items.value.indexOf(document.activeElement as HTMLButtonElement)
    const n = items.value.length
    items.value[(i + (e.key === 'ArrowDown' ? 1 : n - 1)) % n]?.focus()
  }
}
function onDocDown(e: MouseEvent) {
  if (open.value && !wrap.value?.contains(e.target as Node)) open.value = false
}
onMounted(() => document.addEventListener('mousedown', onDocDown))
onBeforeUnmount(() => document.removeEventListener('mousedown', onDocDown))
</script>

<template>
  <div ref="wrap" class="lang" :class="[`lang--${props.variant}`, { 'lang--side': props.collapsed }]" @keydown="onKey">
    <button
      type="button"
      class="lang__btn"
      :title="t('language.choose')"
      :aria-label="`${t('language.choose')} · ${current.label}`"
      aria-haspopup="menu"
      :aria-expanded="open"
      @click="toggle"
    >
      <NavIcon name="globe" :size="16" />
      <span v-if="props.variant === 'inline'" class="lang__current">{{ current.label }}</span>
    </button>
    <Transition name="pop">
      <div v-if="open" class="lang__menu" role="menu" :aria-label="t('language.label')">
        <button
          v-for="l in LOCALES"
          :key="l.code"
          ref="items"
          type="button"
          role="menuitemradio"
          class="lang__item"
          :class="{ 'is-active': l.code === locale }"
          :aria-checked="l.code === locale"
          :lang="l.code"
          @click="pick(l.code)"
        >
          <span>{{ l.label }}</span>
          <svg v-if="l.code === locale" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m5 12 5 5 9-10" /></svg>
        </button>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.lang { position: relative; }
.lang__btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  height: 30px;
  min-width: 30px;
  padding: 0;
  border: none;
  border-radius: var(--r-sm);
  background: transparent;
  color: var(--muted);
  font: inherit;
  font-size: 13px;
  cursor: pointer;
  transition: background var(--dur) var(--ease), color var(--dur) var(--ease);
}
.lang__btn:hover, .lang__btn[aria-expanded='true'] { background: var(--hover-strong, var(--bg-hover-strong)); color: var(--text); }
.lang__btn:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.lang--inline .lang__btn { padding: 0 10px; color: var(--text-2); }

.lang__menu {
  position: absolute;
  right: 0;
  bottom: calc(100% + 6px);
  min-width: 168px;
  padding: 4px;
  background: var(--surface);
  border: 1px solid var(--border-2);
  border-radius: 12px;
  box-shadow: var(--shadow-md);
  z-index: 70;
}
.lang--inline .lang__menu { bottom: auto; top: calc(100% + 6px); }
.lang--side .lang__menu { right: auto; left: calc(100% + 10px); bottom: 0; }
.lang__item {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 7px 10px;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: var(--text-2);
  font: inherit;
  font-size: 13.5px;
  text-align: left;
  cursor: pointer;
}
.lang__item:hover, .lang__item:focus-visible { background: var(--hover); color: var(--text); outline: none; }
.lang__item.is-active { color: var(--accent); font-weight: 500; }

.pop-enter-active, .pop-leave-active { transition: opacity 110ms var(--ease), transform 110ms var(--ease); }
.pop-enter-from, .pop-leave-to { opacity: 0; transform: translateY(4px); }
@media (prefers-reduced-motion: reduce) {
  .pop-enter-active, .pop-leave-active { transition: none; }
}
@media (max-width: 900px) {
  .lang--side .lang__menu { left: auto; right: 0; bottom: calc(100% + 6px); }
  .lang__btn { height: 38px; min-width: 38px; }
}
</style>
