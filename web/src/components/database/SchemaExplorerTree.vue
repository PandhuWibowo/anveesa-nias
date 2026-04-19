<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { SchemaMetadataCatalog, SchemaObjectGroup, SchemaObjectItem } from '@/composables/useSchema'

const props = defineProps<{
  catalog: SchemaMetadataCatalog | null
  selectedKey: string
}>()

const emit = defineEmits<{
  (e: 'select-object', payload: { type: string; name: string }): void
}>()

const expandedGroups = ref<Set<string>>(new Set(['tables', 'views', 'indexes']))
const query = ref('')

watch(() => props.catalog?.database, () => {
  query.value = ''
})

const visibleGroups = computed(() => {
  const groups = props.catalog?.groups ?? []
  if (!query.value.trim()) return groups
  const q = query.value.trim().toLowerCase()
  return groups
    .map((group) => ({
      ...group,
      items: group.items.filter((item) =>
        item.name.toLowerCase().includes(q) ||
        item.parent_name?.toLowerCase().includes(q) ||
        item.type.toLowerCase().includes(q),
      ),
    }))
    .filter((group) => group.items.length > 0)
})

function toggleGroup(key: string) {
  if (expandedGroups.value.has(key)) expandedGroups.value.delete(key)
  else expandedGroups.value.add(key)
}

function selectItem(item: SchemaObjectItem) {
  emit('select-object', { type: item.type, name: item.name })
}

function iconForGroup(group: SchemaObjectGroup) {
  switch (group.key) {
    case 'tables': return 'table'
    case 'views':
    case 'materialized_views': return 'view'
    case 'indexes': return 'index'
    case 'sequences': return 'sequence'
    case 'triggers': return 'trigger'
    case 'functions':
    case 'procedures': return 'routine'
    case 'types': return 'type'
    default: return 'table'
  }
}
</script>

<template>
  <div class="explorer-tree">
    <div class="explorer-tree__search">
      <input v-model="query" class="base-input" placeholder="Filter objects…" />
    </div>

    <div class="explorer-tree__scroll-area">
      <div v-if="!catalog" class="explorer-tree__empty">Select a database or schema.</div>
      <div v-else-if="visibleGroups.length === 0" class="explorer-tree__empty">No matching objects.</div>

      <template v-else>
        <div v-for="group in visibleGroups" :key="group.key" class="explorer-tree__group">
        <button class="explorer-tree__group-btn" @click="toggleGroup(group.key)">
          <span class="explorer-tree__caret" :class="{ 'explorer-tree__caret--open': expandedGroups.has(group.key) }">›</span>
          <span class="explorer-tree__group-label">{{ group.label }}</span>
          <span class="explorer-tree__count">{{ group.items.length }}</span>
        </button>

        <div v-if="expandedGroups.has(group.key)" class="explorer-tree__items">
          <button
            v-for="item in group.items"
            :key="`${group.key}:${item.name}`"
            class="explorer-tree__item"
            :class="{ 'explorer-tree__item--active': selectedKey === `${item.type}:${item.name}` }"
            @click="selectItem(item)"
          >
            <span class="explorer-tree__item-ico" :data-icon="iconForGroup(group)" />
            <span class="explorer-tree__item-main">
              <span class="explorer-tree__item-name">{{ item.name }}</span>
              <span v-if="item.parent_name" class="explorer-tree__item-sub">{{ item.parent_name }}</span>
            </span>
          </button>
        </div>
      </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.explorer-tree { 
  height: 100%; 
  display: flex; 
  flex-direction: column;
  background: var(--bg-surface);
  overflow: hidden;
}

.explorer-tree__search { 
  flex-shrink: 0;
  padding: 14px 12px;
  background: var(--bg-surface); 
  border-bottom: 1px solid var(--border);
  z-index: 10;
}

.explorer-tree__scroll-area {
  flex: 1;
  overflow-y: auto;
  overflow-x: hidden;
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.explorer-tree__scroll-area::-webkit-scrollbar {
  width: 8px;
}

.explorer-tree__scroll-area::-webkit-scrollbar-track {
  background: transparent;
}

.explorer-tree__scroll-area::-webkit-scrollbar-thumb {
  background: var(--border);
  border-radius: 10px;
}

.explorer-tree__scroll-area::-webkit-scrollbar-thumb:hover {
  background: color-mix(in srgb, var(--border) 70%, var(--brand) 30%);
}

.explorer-tree__empty { 
  padding: 48px 24px; 
  color: var(--text-muted); 
  font-size: 14px; 
  text-align: center;
  background: var(--bg-elevated);
  border-radius: 14px;
  border: 2px dashed var(--border);
}

.explorer-tree__group { 
  border: 1px solid var(--border);
  border-radius: 10px;
  overflow: hidden;
  background: var(--bg-elevated);
  transition: border-color .15s ease, box-shadow .15s ease;
  box-shadow: 0 1px 2px rgba(0,0,0,.04);
  flex-shrink: 0;
}

.explorer-tree__group:hover { 
  border-color: color-mix(in srgb, var(--border) 60%, var(--brand) 40%);
  box-shadow: 0 2px 8px rgba(0,0,0,.08);
}

.explorer-tree__group-btn {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
  background: var(--bg-elevated);
  border: 0;
  color: var(--text-primary);
  font-size: 11.5px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.6px;
  cursor: pointer;
  transition: background .15s ease;
  line-height: 1;
}

.explorer-tree__group-btn:hover { 
  background: color-mix(in srgb, var(--bg-surface) 50%, var(--bg-elevated));
}

.explorer-tree__caret { 
  transition: transform .2s cubic-bezier(0.4, 0, 0.2, 1);
  color: var(--text-muted);
  font-size: 16px;
  font-weight: bold;
  width: 14px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  line-height: 1;
}

.explorer-tree__caret--open { 
  transform: rotate(90deg);
  color: var(--brand);
}

.explorer-tree__group-label { 
  flex: 1; 
  text-align: left;
  opacity: 1;
  line-height: 1;
}

.explorer-tree__count { 
  font-size: 11px;
  color: var(--brand);
  background: color-mix(in srgb, var(--brand) 15%, transparent);
  padding: 4px 9px;
  border-radius: 8px;
  font-weight: 700;
  letter-spacing: 0.3px;
  min-width: 28px;
  text-align: center;
  line-height: 1.3;
}

.explorer-tree__items { 
  display: flex; 
  flex-direction: column;
  background: var(--bg-surface);
  border-top: 1px solid var(--border);
  max-height: 380px;
  overflow-y: auto;
  overflow-x: hidden;
}

.explorer-tree__items::-webkit-scrollbar {
  width: 6px;
}

.explorer-tree__items::-webkit-scrollbar-track {
  background: transparent;
}

.explorer-tree__items::-webkit-scrollbar-thumb {
  background: var(--border);
  border-radius: 6px;
}

.explorer-tree__items::-webkit-scrollbar-thumb:hover {
  background: color-mix(in srgb, var(--border) 65%, var(--brand) 35%);
}

.explorer-tree__item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 11px 16px;
  border: 0;
  background: transparent;
  text-align: left;
  cursor: pointer;
  transition: background .12s ease, border-color .12s ease;
  position: relative;
  border-left: 3px solid transparent;
  width: 100%;
}

.explorer-tree__item:hover {
  background: var(--bg-elevated);
  border-left-color: var(--brand);
}

.explorer-tree__item + .explorer-tree__item { 
  border-top: 1px solid color-mix(in srgb, var(--border) 50%, transparent);
}

.explorer-tree__item--active { 
  background: color-mix(in srgb, var(--brand) 14%, var(--bg-surface));
  border-left-color: var(--brand);
}

.explorer-tree__item-ico {
  width: 10px;
  height: 10px;
  border-radius: 999px;
  background: var(--text-muted);
  flex-shrink: 0;
  transition: transform .15s ease;
}

.explorer-tree__item:hover .explorer-tree__item-ico {
  transform: scale(1.2);
}

.explorer-tree__item-ico[data-icon="table"] { 
  background: #0f766e;
  box-shadow: 0 0 12px rgba(15, 118, 110, 0.7), 0 2px 6px rgba(15, 118, 110, 0.4);
}

.explorer-tree__item-ico[data-icon="view"] { 
  background: #2563eb;
  box-shadow: 0 0 12px rgba(37, 99, 235, 0.7), 0 2px 6px rgba(37, 99, 235, 0.4);
}

.explorer-tree__item-ico[data-icon="index"] { 
  background: #f59e0b;
  box-shadow: 0 0 12px rgba(245, 158, 11, 0.7), 0 2px 6px rgba(245, 158, 11, 0.4);
}

.explorer-tree__item-ico[data-icon="sequence"] { 
  background: #7c3aed;
  box-shadow: 0 0 12px rgba(124, 58, 237, 0.7), 0 2px 6px rgba(124, 58, 237, 0.4);
}

.explorer-tree__item-ico[data-icon="trigger"] { 
  background: #dc2626;
  box-shadow: 0 0 12px rgba(220, 38, 38, 0.7), 0 2px 6px rgba(220, 38, 38, 0.4);
}

.explorer-tree__item-ico[data-icon="routine"] { 
  background: #059669;
  box-shadow: 0 0 12px rgba(5, 150, 105, 0.7), 0 2px 6px rgba(5, 150, 105, 0.4);
}

.explorer-tree__item-ico[data-icon="type"] { 
  background: #475569;
  box-shadow: 0 0 12px rgba(71, 85, 105, 0.7), 0 2px 6px rgba(71, 85, 105, 0.4);
}

.explorer-tree__item-main { 
  display: flex; 
  flex-direction: column; 
  min-width: 0; 
  flex: 1;
  gap: 2px;
}

.explorer-tree__item-name { 
  color: var(--text-primary);
  font-size: 13px;
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  line-height: 1.4;
}

.explorer-tree__item--active .explorer-tree__item-name {
  color: var(--brand);
  font-weight: 600;
}

.explorer-tree__item-sub { 
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 400;
  opacity: 0.85;
  line-height: 1.3;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
