<script setup lang="ts">
import { ref, watch, computed, onMounted, onBeforeUnmount } from 'vue'
import axios from 'axios'
import { useRouter } from 'vue-router'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ close: []; navigate: [{ connId: number; table: string; type: string }] }>()

const router = useRouter()
const q = ref('')
const results = ref<any[]>([])
const loading = ref(false)
const activeIdx = ref(0)
const inputRef = ref<HTMLInputElement>()

let debounceTimer: ReturnType<typeof setTimeout>

watch(q, (val) => {
  clearTimeout(debounceTimer)
  if (!val.trim()) { results.value = []; return }
  loading.value = true
  debounceTimer = setTimeout(async () => {
    try {
      const { data } = await axios.get('/api/schema/search', { params: { q: val } })
      results.value = data ?? []
    } catch { results.value = [] }
    finally { loading.value = false }
  }, 200)
})

watch(() => props.show, (v) => {
  if (v) {
    q.value = ''; results.value = []; activeIdx.value = 0
    setTimeout(() => inputRef.value?.focus(), 50)
  }
})

function select(r: any) {
  emit('navigate', { connId: r.conn_id, table: r.table || r.column, type: r.type })
  emit('close')
}

function onKey(e: KeyboardEvent) {
  if (!props.show) return
  if (e.key === 'ArrowDown') { activeIdx.value = Math.min(activeIdx.value + 1, results.value.length - 1); e.preventDefault() }
  if (e.key === 'ArrowUp') { activeIdx.value = Math.max(activeIdx.value - 1, 0); e.preventDefault() }
  if (e.key === 'Enter' && results.value[activeIdx.value]) select(results.value[activeIdx.value])
  if (e.key === 'Escape') emit('close')
}

onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))

const typeIcon: Record<string, string> = { table: '▦', column: '≡', view: '◈' }
const typeColor: Record<string, string> = { table: 'var(--brand)', column: 'var(--text-muted)', view: '#a78bfa' }
</script>

<template>
  <Teleport to="body">
    <div v-if="show" class="ss-overlay" @click.self="emit('close')">
      <div class="ss-modal">
        <div class="ss-search-wrap">
          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="ss-icon"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
          <input
            ref="inputRef"
            v-model="q"
            class="ss-input"
            placeholder="Search tables, columns… (↑↓ navigate, Enter to open)"
          />
          <svg v-if="loading" class="spin ss-spin" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
          <kbd class="ss-esc" @click="emit('close')">Esc</kbd>
        </div>

        <div class="ss-results">
          <div v-if="!q.trim()" class="ss-hint">Type to search tables and columns across all connections</div>
          <div v-else-if="results.length === 0 && !loading" class="ss-hint">No results for "{{ q }}"</div>
          <div
            v-for="(r, i) in results"
            :key="i"
            class="ss-item"
            :class="{ 'ss-item--active': i === activeIdx }"
            @mouseenter="activeIdx = i"
            @click="select(r)"
          >
            <span class="ss-type-icon" :style="{ color: typeColor[r.type] }">{{ typeIcon[r.type] ?? '?' }}</span>
            <div class="ss-item-body">
              <span class="ss-item-name">{{ r.type === 'column' ? r.column : r.table }}</span>
              <span v-if="r.type === 'column'" class="ss-item-sub">in {{ r.table }}</span>
              <span v-if="r.data_type" class="ss-item-dt">{{ r.data_type }}</span>
            </div>
            <div class="ss-item-conn">
              <span class="ss-conn-badge">{{ r.conn_name }}</span>
            </div>
          </div>
        </div>

        <div class="ss-footer">
          <span><kbd>↑↓</kbd> navigate</span>
          <span><kbd>↵</kbd> open</span>
          <span><kbd>Esc</kbd> close</span>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.ss-overlay {
  position: fixed; inset: 0; background: rgba(0,0,0,0.6);
  display: flex; align-items: flex-start; justify-content: center;
  padding-top: 10vh; z-index: 2000;
}
.ss-modal {
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 12px; width: min(620px, 94vw);
  box-shadow: 0 32px 80px rgba(0,0,0,0.6);
  display: flex; flex-direction: column; overflow: hidden;
}
.ss-search-wrap {
  display: flex; align-items: center; gap: 10px;
  padding: 12px 16px; border-bottom: 1px solid var(--border);
}
.ss-icon { color: var(--text-muted); flex-shrink: 0; }
.ss-spin { color: var(--text-muted); flex-shrink: 0; }
.ss-input {
  flex: 1; background: transparent; border: none; outline: none;
  font-size: 15px; color: var(--text-primary); font-family: inherit;
}
.ss-input::placeholder { color: var(--text-muted); }
.ss-esc {
  padding: 2px 7px; border-radius: 5px;
  background: var(--bg-body); border: 1px solid var(--border);
  font-size: 10.5px; color: var(--text-muted); cursor: pointer;
  font-family: inherit;
}
.ss-results { max-height: 380px; overflow-y: auto; }
.ss-hint { padding: 20px; text-align: center; font-size: 13px; color: var(--text-muted); }
.ss-item {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 16px; cursor: pointer; transition: background 0.08s;
}
.ss-item--active { background: var(--bg-hover); }
.ss-type-icon { font-size: 14px; flex-shrink: 0; width: 18px; text-align: center; }
.ss-item-body { flex: 1; display: flex; align-items: center; gap: 8px; min-width: 0; }
.ss-item-name { font-weight: 600; font-size: 13px; color: var(--text-primary); font-family: var(--mono, monospace); }
.ss-item-sub { font-size: 11px; color: var(--text-muted); }
.ss-item-dt { font-size: 10.5px; color: var(--text-muted); padding: 1px 5px; background: var(--bg-body); border-radius: 4px; }
.ss-item-conn { flex-shrink: 0; }
.ss-conn-badge {
  font-size: 10px; padding: 1px 6px; border-radius: 4px;
  background: var(--brand-dim); color: var(--brand); font-weight: 600;
}
.ss-footer {
  display: flex; gap: 16px; padding: 8px 16px;
  border-top: 1px solid var(--border); background: var(--bg-body);
  font-size: 11px; color: var(--text-muted);
}
.ss-footer kbd {
  padding: 1px 5px; border-radius: 3px;
  background: var(--bg-elevated); border: 1px solid var(--border);
  font-family: inherit;
}
</style>
