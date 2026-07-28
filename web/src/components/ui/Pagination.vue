<script setup lang="ts">
defineProps<{
  page: number
  totalPages: number
  total?: number
  itemLabel?: string
}>()
const emit = defineEmits<{ 'update:page': [number] }>()
</script>

<template>
  <div class="pagination">
    <span v-if="total != null" class="pagination__info">
      {{ total.toLocaleString() }}{{ itemLabel ? ' ' + itemLabel : '' }}
    </span>
    <div class="pagination__spacer"></div>
    <button class="base-btn base-btn--ghost base-btn--xs" type="button" :disabled="page <= 1" @click="emit('update:page', 1)">«</button>
    <button class="base-btn base-btn--ghost base-btn--xs" type="button" :disabled="page <= 1" @click="emit('update:page', page - 1)">‹ Prev</button>
    <span class="pagination__page">{{ page }} / {{ totalPages }}</span>
    <button class="base-btn base-btn--ghost base-btn--xs" type="button" :disabled="page >= totalPages" @click="emit('update:page', page + 1)">Next ›</button>
    <button class="base-btn base-btn--ghost base-btn--xs" type="button" :disabled="page >= totalPages" @click="emit('update:page', totalPages)">»</button>
  </div>
</template>

<style scoped>
.pagination__page { font-size: 12px; color: var(--text-secondary); white-space: nowrap; }
</style>
