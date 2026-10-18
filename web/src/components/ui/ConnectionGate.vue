<script setup lang="ts">
import { computed } from 'vue'
import { useAuth } from '@/composables/useAuth'
import { useConnections, type DbDriver } from '@/composables/useConnections'
import { DRIVER_COLOR, DRIVER_NAME } from '@/utils/drivers'
import DriverIcon from '@/components/ui/DriverIcon.vue'
import EmptyState from '@/components/ui/EmptyState.vue'

// Shown by pages that need a connection of a particular kind. Lists the matching
// connections so picking one is a single click, or offers to create the first one.
const props = withDefaults(defineProps<{
  drivers: DbDriver[]
  label: string
  purpose: string
  icon?: string
  /** Short noun for the button, e.g. "search" → "Add search connection". Defaults to label. */
  short?: string
}>(), { icon: 'plug' })

const emit = defineEmits<{ (e: 'select', id: number): void }>()

const { connections } = useConnections()
const { hasAnyPermission } = useAuth()

const matches = computed(() => connections.value.filter((c) => props.drivers.includes(c.driver) && !c.disconnected))
const canCreate = computed(() => hasAnyPermission(['connections.create', 'connections.view']))
const article = computed(() => (/^[aeiou]/i.test(props.label) ? 'an' : 'a'))
const addLabel = computed(() => `Add ${props.short ?? props.label} connection`)
const addTo = computed(() => ({ name: 'connections', query: { new: '1', driver: props.drivers[0] } }))
</script>

<template>
  <div class="gate">
    <EmptyState
      v-if="!matches.length"
      :icon="icon"
      :title="`No ${label} connections yet`"
      :text="`Add ${article} ${label} connection ${purpose}.`"
      :action-label="canCreate ? addLabel : undefined"
      :action-to="canCreate ? addTo : undefined"
    />
    <template v-else>
      <EmptyState :icon="icon" :title="`Choose a ${label} connection`" :text="`Pick one ${purpose}.`" />
      <div class="gate__list">
        <button v-for="c in matches" :key="c.id" class="gate__item" type="button" @click="emit('select', c.id)">
          <span class="gate__driver" :style="{ background: DRIVER_COLOR[c.driver] ?? '#4b5563' }">
            <DriverIcon :driver="c.driver" :size="15" />
          </span>
          <span class="gate__text">
            <span class="gate__name">{{ c.name }}</span>
            <span class="gate__sub">{{ DRIVER_NAME[c.driver] ?? c.driver }}<template v-if="c.host"> · {{ c.host }}<template v-if="c.port">:{{ c.port }}</template></template></span>
          </span>
        </button>
      </div>
    </template>
  </div>
</template>

<style scoped>
.gate {
  width: 100%;
  /* Span the whole page even when the parent is a multi-column workbench grid. */
  grid-column: 1 / -1;
  align-self: start;
  padding: 24px 16px 48px;
}
.gate :deep(.es) { padding-bottom: 20px; }
.gate__list {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 8px;
  max-width: 760px;
  margin: 0 auto;
}
.gate__item {
  display: flex;
  align-items: center;
  gap: 11px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--r);
  background: var(--bg-surface);
  color: var(--text-primary);
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: border-color var(--dur) var(--ease), background var(--dur) var(--ease);
}
.gate__item:hover { border-color: var(--brand); background: var(--bg-hover-strong); }
.gate__driver {
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border-radius: var(--r-sm);
  flex-shrink: 0;
}
.gate__text { min-width: 0; display: flex; flex-direction: column; line-height: 1.3; }
.gate__name,
.gate__sub { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.gate__name { font-size: 14px; font-weight: 500; }
.gate__sub { font-size: 12.5px; color: var(--text-muted); }
</style>
