<script setup lang="ts">
defineProps<{ active: boolean; dir: 'asc' | 'desc' }>()
</script>

<template>
  <span class="sort-icon">{{ active ? (dir === 'asc' ? '▲' : '▼') : '↕' }}</span>
</template>
