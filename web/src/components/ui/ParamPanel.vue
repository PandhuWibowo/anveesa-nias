<script setup lang="ts">
import { computed, watch, ref } from 'vue'

const props = defineProps<{ sql: string }>()
const emit = defineEmits<{
  (e: 'update:params', params: Record<string, string>): void
}>()

const params = ref<Record<string, string>>({})

const paramNames = computed<string[]>(() => {
  const matches = props.sql.match(/:([a-zA-Z_][a-zA-Z0-9_]*)/g) ?? []
  return [...new Set(matches.map((m) => m.slice(1)))]
})

watch(paramNames, (names) => {
  const next: Record<string, string> = {}
  for (const n of names) {
    next[n] = params.value[n] ?? ''
  }
  params.value = next
  emit('update:params', next)
}, { immediate: true })

function update(name: string, val: string) {
  params.value[name] = val
  emit('update:params', { ...params.value })
}

/** Build the final SQL with params substituted */
function buildSQL(sql: string, p: Record<string, string>): string {
  return sql.replace(/:([a-zA-Z_][a-zA-Z0-9_]*)/g, (_, name) => {
    const val = p[name] ?? ''
    if (val === '') return `:${name}`
    return isNaN(Number(val)) || val.trim() === '' ? `'${val.replace(/'/g, "''")}'` : val
  })
}

defineExpose({ buildSQL, params })
</script>

<template>
  <div v-if="paramNames.length" class="pp-root">
    <div class="pp-header">
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/><circle cx="5" cy="12" r="1"/></svg>
      Parameters
      <span class="pp-count">{{ paramNames.length }}</span>
    </div>
    <div class="pp-fields">
      <div v-for="name in paramNames" :key="name" class="pp-field">
        <label class="pp-label">:{{ name }}</label>
        <input
          class="pp-input"
          :value="params[name]"
          :placeholder="'value for ' + name"
          @input="update(name, ($event.target as HTMLInputElement).value)"
        />
      </div>
    </div>
  </div>
</template>

<style scoped>
.pp-root {
  flex-shrink: 0;
  border-top: 1px solid var(--border);
  background: var(--bg-elevated);
  padding: 8px 14px;
}
.pp-header {
  display: flex; align-items: center; gap: 6px;
  font-size: 10.5px; font-weight: 700; text-transform: uppercase;
  letter-spacing: 0.4px; color: var(--text-muted);
  margin-bottom: 8px;
}
.pp-count {
  padding: 1px 5px; border-radius: 4px;
  background: var(--brand-dim); color: var(--brand);
  font-size: 9px; font-weight: 900;
}
.pp-fields { display: flex; flex-wrap: wrap; gap: 8px; }
.pp-field { display: flex; align-items: center; gap: 6px; }
.pp-label {
  font-family: var(--mono, monospace); font-size: 12px;
  color: var(--brand); white-space: nowrap;
}
.pp-input {
  padding: 4px 8px; border-radius: 5px;
  border: 1px solid var(--border); background: var(--bg-body);
  color: var(--text-primary); font-size: 12px;
  outline: none; width: 140px;
  font-family: var(--mono, monospace);
  transition: border-color 0.15s;
}
.pp-input:focus { border-color: var(--brand); }
.pp-input::placeholder { color: var(--text-muted); }
</style>
