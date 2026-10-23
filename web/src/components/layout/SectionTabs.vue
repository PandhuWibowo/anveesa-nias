<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useNavigation } from '@/composables/useNavigation'
import { navKey, sectionOf, type NavItem } from '@/navigation'

// The pages of the current section, as tabs across the top of the content panel.
// The sidebar picks the section; these pick the page — one click either way.
const props = defineProps<{ activeDriver: string | null }>()

const route = useRoute()
const router = useRouter()
const { visibleItems } = useNavigation()

const currentTab = computed(() => (typeof route.query.tab === 'string' ? route.query.tab : null))
const section = computed(() => sectionOf(route.name as string, currentTab.value))
const items = computed(() =>
  section.value
    ? visibleItems(section.value, props.activeDriver, { name: route.name as string, tab: currentTab.value })
    : [],
)

function isActive(item: NavItem) {
  if (route.name !== item.name) return false
  if (item.query?.tab) return currentTab.value === item.query.tab
  // "Roles" is the permissions page without the groups tab.
  return !items.value.some((i) => i.name === item.name && i.query?.tab && i.query.tab === currentTab.value)
}
</script>

<template>
  <nav v-if="items.length > 1" class="stabs" :aria-label="section?.label">
    <a
      v-for="item in items"
      :key="navKey(item)"
      :href="router.resolve({ name: item.name, query: item.query }).href"
      class="stabs__tab"
      :class="{ 'is-active': isActive(item) }"
      :aria-current="isActive(item) ? 'page' : undefined"
      :title="item.desc"
      @click.prevent="router.push({ name: item.name, query: item.query })"
    >
      {{ item.label }}
    </a>
  </nav>
</template>

<style scoped>
.stabs {
  display: flex;
  align-items: center;
  gap: 2px;
  height: 44px;
  padding: 0 12px;
  flex-shrink: 0;
  overflow-x: auto;
  scrollbar-width: none;
  border-bottom: 1px solid var(--border);
  background: var(--bg);
}
.stabs::-webkit-scrollbar { display: none; }
.stabs__tab {
  flex-shrink: 0;
  height: 30px;
  display: inline-flex;
  align-items: center;
  padding: 0 11px;
  border-radius: var(--r-sm);
  color: var(--text-2);
  font-size: 13.5px;
  font-weight: 500;
  text-decoration: none;
  white-space: nowrap;
  transition: background var(--dur) var(--ease), color var(--dur) var(--ease);
}
.stabs__tab:hover { background: var(--hover); color: var(--text); }
.stabs__tab.is-active {
  background: var(--accent-bg);
  color: var(--accent);
}
@media (max-width: 900px) {
  .stabs { height: 46px; padding: 0 8px; }
  .stabs__tab { height: 34px; }
}
</style>
