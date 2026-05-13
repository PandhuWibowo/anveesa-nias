<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { fetchIndices, type IndexEntry } from '@/composables/useObsSettings'

const props = defineProps<{
  connId: number | null
  modelValue: string
  placeholder?: string
}>()
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()

const showPicker  = ref(false)
const query       = ref('')
const indices     = ref<IndexEntry[]>([])
const loading     = ref(false)
const error       = ref('')

const filtered = computed(() => {
  const q = query.value.toLowerCase()
  return indices.value.filter(i => i.index.toLowerCase().includes(q))
})

async function open() {
  if (!props.connId) return
  showPicker.value = true
  if (indices.value.length) return
  loading.value = true
  error.value = ''
  try {
    indices.value = await fetchIndices(props.connId)
  } catch (e: any) {
    error.value = e?.message ?? 'Failed to load indices'
  } finally {
    loading.value = false
  }
}

function pick(idx: IndexEntry) {
  // Convert exact index name to a wildcard pattern (strip date suffix)
  const pattern = toPattern(idx.index)
  emit('update:modelValue', pattern)
  showPicker.value = false
  query.value = ''
}

function toPattern(name: string): string {
  // Data stream backing indices (.ds-*) must be converted to their data stream
  // alias name for Elasticsearch to accept them in searches.
  //   .ds-heartbeat-8.15.0-2026.04.29-000007  →  heartbeat-*
  //   .ds-filebeat-8.15.0-2026.01.29-000004   →  filebeat-*
  //   .ds-metricbeat-2026.04.29-000003        →  metricbeat-*
  if (name.startsWith('.ds-')) {
    const inner = name.slice(4)
    // Strip semver + date suffix (e.g. -8.15.0-2026.04.29-000007)
    let base = inner.replace(/-\d+\.\d+\.\d+(-\d{4}[.\-]\d{2}[.\-]\d{2}-\d+)?$/, '')
    if (base === inner) {
      // No semver — strip plain date suffix (e.g. -2026.04.29-000007)
      base = inner.replace(/-\d{4}[.\-]\d{2}[.\-]\d{2}-\d+$/, '')
    }
    return (base || inner) + '-*'
  }
  // Regular indices: strip rollover date suffix
  //   k8s-logs-env-prod-2026.03.20  →  k8s-logs-env-prod-*
  //   elastalert_status             →  elastalert_status (no change)
  const datePattern = /[-.]?\d{4}[.\-]\d{2}[.\-]\d{2}.*$/
  const numSuffix   = /[-_]\d{6}$/
  let p = name.replace(datePattern, '').replace(numSuffix, '')
  if (p !== name) p += '-*'
  return p || name
}

function healthColor(h: string) {
  if (h === 'green')  return 'ip-green'
  if (h === 'yellow') return 'ip-yellow'
  return 'ip-red'
}

function fmtDocs(v: string) {
  const n = parseInt(v)
  if (isNaN(n)) return v
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M'
  if (n >= 1_000)     return (n / 1_000).toFixed(0) + 'K'
  return String(n)
}
</script>

<template>
  <div class="ip-wrap">
    <div class="ip-input-row">
      <input
        :value="modelValue"
        class="base-input ip-input"
        :placeholder="placeholder ?? 'e.g. logs-*, .ds-filebeat-*'"
        @input="emit('update:modelValue', ($event.target as HTMLInputElement).value)"
      />
      <button
        type="button"
        class="base-btn base-btn--ghost base-btn--sm ip-browse-btn"
        :disabled="!connId"
        :title="connId ? 'Browse available indices' : 'Select a connection first'"
        @click="open"
      >Browse</button>
    </div>

    <!-- picker overlay -->
    <Teleport to="body">
      <div v-if="showPicker" class="ip-overlay" @click.self="showPicker = false">
        <div class="ip-modal">
          <div class="ip-modal-head">
            <span class="ip-modal-title">Browse Indices</span>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="showPicker = false">✕</button>
          </div>
          <input
            v-model="query"
            class="base-input ip-search"
            placeholder="Filter by name…"
            autofocus
          />
          <div v-if="loading" class="ip-state">Loading indices…</div>
          <div v-else-if="error" class="ip-state ip-error">{{ error }}</div>
          <div v-else-if="!filtered.length" class="ip-state">No indices match.</div>
          <div v-else class="ip-list">
            <button
              v-for="idx in filtered"
              :key="idx.index"
              class="ip-row"
              @click="pick(idx)"
            >
              <span class="ip-dot" :class="healthColor(idx.health)" />
              <span class="ip-name">{{ idx.index }}</span>
              <span v-if="toPattern(idx.index) !== idx.index" class="ip-pattern-hint">→ {{ toPattern(idx.index) }}</span>
              <span class="ip-meta">{{ fmtDocs(idx['docs.count']) }} docs</span>
              <span class="ip-meta">{{ idx['store.size'] }}</span>
            </button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.ip-wrap { display: flex; flex-direction: column; gap: 4px; }
.ip-input-row { display: flex; gap: 6px; }
.ip-input { flex: 1; font-family: var(--mono); font-size: 12px; }
.ip-browse-btn { white-space: nowrap; flex-shrink: 0; }

.ip-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.5); z-index: 10000; display: flex; align-items: center; justify-content: center; padding: 20px; }
.ip-modal { background: var(--bg-elevated); border: 1px solid var(--border); border-radius: 10px; width: 600px; max-width: 100%; max-height: 80vh; display: flex; flex-direction: column; overflow: hidden; }
.ip-modal-head { display: flex; align-items: center; justify-content: space-between; padding: 14px 16px; border-bottom: 1px solid var(--border); }
.ip-modal-title { font-size: 14px; font-weight: 700; color: var(--text-primary); }
.ip-search { margin: 10px 12px; font-size: 13px; }
.ip-state { padding: 20px; text-align: center; color: var(--text-muted); font-size: 13px; }
.ip-error { color: var(--danger); }
.ip-list { overflow-y: auto; flex: 1; }
.ip-row { display: flex; align-items: center; gap: 10px; width: 100%; padding: 8px 14px; border: 0; background: transparent; cursor: pointer; text-align: left; transition: background 0.1s; }
.ip-row:hover { background: var(--bg-body); }
.ip-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.ip-green  { background: var(--success); }
.ip-yellow { background: var(--warning); }
.ip-red    { background: var(--danger); }
.ip-name { flex: 1; font-size: 12.5px; font-family: var(--mono); color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ip-meta { font-size: 11px; color: var(--text-muted); white-space: nowrap; }
.ip-pattern-hint { font-size: 10.5px; color: #00bfb3; font-family: var(--mono); white-space: nowrap; flex-shrink: 0; }
</style>
