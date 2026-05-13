<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useConnections } from '@/composables/useConnections'
import { useMemcache, type MemcacheValueResponse } from '@/composables/useMemcache'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'

const props = defineProps<{ activeConnId: number | null }>()
const emit = defineEmits<{ (e: 'set-conn', id: number): void }>()

const { connections, fetchConnections } = useConnections()
const memcache = useMemcache()
const toast = useToast()
const { confirm } = useConfirm()

const keyInput = ref('')
const selectedKey = ref('')
const currentValue = ref<MemcacheValueResponse | null>(null)
const knownKeys = ref<string[]>([])
const stats = ref<Record<string, string>>({})
const connected = ref(false)
const statusText = ref('')
const latencyMs = ref<number | null>(null)
const loading = ref(false)
const saving = ref(false)
const flushing = ref(false)
const editorKey = ref('')
const editorValue = ref('')
const editorTTL = ref(0)
const editorFlags = ref(0)

const activeConn = computed(() =>
  props.activeConnId != null ? connections.value.find((c) => c.id === props.activeConnId) ?? null : null,
)
const memcacheConnections = computed(() => connections.value.filter((c) => c.driver === 'memcache'))
const isMemcache = computed(() => activeConn.value?.driver === 'memcache')

const statCards = computed(() => [
  { label: 'Version', value: stats.value.version ?? '-' },
  { label: 'Items', value: stats.value.curr_items ?? '0' },
  { label: 'Bytes', value: formatBytes(Number(stats.value.bytes || 0)) },
  { label: 'Limit', value: formatBytes(Number(stats.value.limit_maxbytes || 0)) },
  { label: 'Gets', value: stats.value.cmd_get ?? '0' },
  { label: 'Sets', value: stats.value.cmd_set ?? '0' },
  { label: 'Hits', value: stats.value.get_hits ?? '0' },
  { label: 'Misses', value: stats.value.get_misses ?? '0' },
])

onMounted(async () => {
  if (!connections.value.length) await fetchConnections()
  if (!isMemcache.value && memcacheConnections.value.length === 1) {
    emit('set-conn', memcacheConnections.value[0].id)
    return
  }
  if (isMemcache.value) await reconnect()
})

watch(() => props.activeConnId, async () => {
  resetWorkspace()
  if (isMemcache.value) await reconnect()
})

async function reconnect() {
  if (!activeConn.value) return
  loading.value = true
  try {
    const pong = await memcache.ping(activeConn.value.id)
    connected.value = true
    statusText.value = pong.message
    latencyMs.value = pong.latency_ms
    stats.value = await memcache.stats(activeConn.value.id)
  } catch (e: any) {
    connected.value = false
    statusText.value = e?.response?.data?.error ?? 'Connection failed'
    stats.value = {}
  } finally {
    loading.value = false
  }
}

async function loadKey(rawKey = keyInput.value) {
  if (!activeConn.value || !rawKey.trim()) return
  const key = rawKey.trim()
  loading.value = true
  try {
    const data = await memcache.fetchKey(activeConn.value.id, key)
    currentValue.value = data
    selectedKey.value = key
    editorKey.value = key
    editorValue.value = data.found ? data.value : ''
    editorFlags.value = data.flags ?? 0
    if (!knownKeys.value.includes(key)) knownKeys.value = [key, ...knownKeys.value].slice(0, 50)
    if (!data.found) toast.error('Key not found')
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to read key')
  } finally {
    loading.value = false
  }
}

function newKey() {
  currentValue.value = null
  selectedKey.value = ''
  editorKey.value = ''
  editorValue.value = ''
  editorTTL.value = 0
  editorFlags.value = 0
}

async function save() {
  if (!activeConn.value || !editorKey.value.trim()) return
  saving.value = true
  try {
    await memcache.saveKey(activeConn.value.id, {
      key: editorKey.value.trim(),
      value: editorValue.value,
      ttl: Number(editorTTL.value || 0),
      flags: Number(editorFlags.value || 0),
    })
    keyInput.value = editorKey.value.trim()
    toast.success('Key stored')
    await loadKey(editorKey.value)
    await reconnect()
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to store key')
  } finally {
    saving.value = false
  }
}

async function removeKey() {
  if (!activeConn.value || !editorKey.value.trim()) return
  const ok = await confirm(`Delete key "${editorKey.value}"?`, 'Delete Memcache Key')
  if (!ok) return
  try {
    const deleted = await memcache.deleteKey(activeConn.value.id, editorKey.value.trim())
    knownKeys.value = knownKeys.value.filter((key) => key !== editorKey.value.trim())
    currentValue.value = null
    selectedKey.value = ''
    toast.success(deleted ? 'Key deleted' : 'Key was not found')
    await reconnect()
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to delete key')
  }
}

async function flushAll() {
  if (!activeConn.value) return
  const ok = await confirm('Flush all keys from this Memcache server?', 'Flush Memcache')
  if (!ok) return
  flushing.value = true
  try {
    await memcache.flush(activeConn.value.id, 0)
    currentValue.value = null
    knownKeys.value = []
    toast.success('Memcache flushed')
    await reconnect()
  } catch (e: any) {
    toast.error(e?.response?.data?.error ?? 'Failed to flush Memcache')
  } finally {
    flushing.value = false
  }
}

function resetWorkspace() {
  keyInput.value = ''
  selectedKey.value = ''
  currentValue.value = null
  stats.value = {}
  connected.value = false
  statusText.value = ''
  latencyMs.value = null
  newKey()
}

function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let size = value
  let unit = 0
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024
    unit += 1
  }
  return `${size.toFixed(size >= 10 || unit === 0 ? 0 : 1)} ${units[unit]}`
}
</script>

<template>
  <div class="page-shell mem-root">
    <header class="mem-toolbar">
      <div class="mem-title">
        <span class="mem-badge">MC</span>
        <div>
          <h1>Memcache</h1>
          <p>{{ activeConn ? activeConn.name : 'No Memcache connection selected' }}</p>
        </div>
      </div>
      <div class="mem-actions">
        <select
          class="base-input mem-select"
          :value="activeConnId ?? ''"
          @change="emit('set-conn', Number(($event.target as HTMLSelectElement).value))"
        >
          <option value="" disabled>Select Memcache</option>
          <option v-for="conn in memcacheConnections" :key="conn.id" :value="conn.id">{{ conn.name }}</option>
        </select>
        <button class="base-btn base-btn--ghost" :disabled="!isMemcache || loading" @click="reconnect">Refresh</button>
        <button class="base-btn base-btn--danger" :disabled="!connected || flushing" @click="flushAll">Flush All</button>
      </div>
    </header>

    <div v-if="!isMemcache" class="mem-empty">
      <h2>Select a Memcache connection</h2>
      <p>Create or choose a Memcache connection to read, write, delete, and inspect cache keys.</p>
    </div>

    <template v-else>
      <section class="mem-status">
        <div class="mem-conn" :class="{ 'mem-conn--ok': connected }">
          <span class="mem-dot"></span>
          <strong>{{ connected ? 'Connected' : 'Disconnected' }}</strong>
          <span>{{ statusText }}</span>
          <span v-if="latencyMs !== null">{{ latencyMs }} ms</span>
        </div>
        <div class="mem-stats">
          <div v-for="card in statCards" :key="card.label" class="mem-stat">
            <span>{{ card.label }}</span>
            <strong>{{ card.value }}</strong>
          </div>
        </div>
      </section>

      <main class="mem-body">
        <aside class="mem-side">
          <div class="mem-search">
            <input v-model="keyInput" class="base-input" placeholder="Key name" @keyup.enter="loadKey()" />
            <button class="base-btn base-btn--primary" :disabled="!connected || loading" @click="loadKey()">Get</button>
          </div>
          <button class="base-btn base-btn--ghost mem-new" :disabled="!connected" @click="newKey">New Key</button>
          <div class="mem-side-title">Recent Keys</div>
          <button
            v-for="key in knownKeys"
            :key="key"
            class="mem-key"
            :class="{ 'mem-key--active': selectedKey === key }"
            @click="keyInput = key; loadKey(key)"
          >
            {{ key }}
          </button>
          <div v-if="knownKeys.length === 0" class="mem-muted">Memcache cannot scan all keys. Search for a key or create one here.</div>
        </aside>

        <section class="mem-editor">
          <div class="mem-editor-head">
            <div>
              <h2>{{ selectedKey ? selectedKey : 'Key Editor' }}</h2>
              <p v-if="currentValue?.found">{{ currentValue.bytes }} bytes · flags {{ currentValue.flags }}</p>
              <p v-else-if="selectedKey">Key not found. Saving will create it.</p>
              <p v-else>Create a key or load an existing key by exact name.</p>
            </div>
            <button class="base-btn base-btn--danger" :disabled="!connected || !editorKey" @click="removeKey">Delete</button>
          </div>

          <div class="mem-form">
            <label>
              <span>Key</span>
              <input v-model="editorKey" class="base-input" placeholder="cache:key" />
            </label>
            <label>
              <span>TTL Seconds</span>
              <input v-model.number="editorTTL" class="base-input" type="number" min="0" />
            </label>
            <label>
              <span>Flags</span>
              <input v-model.number="editorFlags" class="base-input" type="number" min="0" />
            </label>
          </div>

          <label class="mem-value">
            <span>Value</span>
            <textarea v-model="editorValue" class="base-input" spellcheck="false" placeholder="Value stored as a Memcache string"></textarea>
          </label>

          <div class="mem-editor-actions">
            <button class="base-btn base-btn--primary" :disabled="!connected || saving || !editorKey.trim()" @click="save">
              {{ saving ? 'Saving...' : 'Save Key' }}
            </button>
          </div>
        </section>
      </main>
    </template>
  </div>
</template>

<style scoped>
.mem-root { display: flex; flex-direction: column; height: 100%; overflow: hidden; background: var(--bg-body); }
.mem-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 14px 16px; border-bottom: 1px solid var(--border); background: var(--bg-surface); }
.mem-title { display: flex; align-items: center; gap: 12px; min-width: 0; }
.mem-title h1 { margin: 0; font-size: 18px; color: var(--text-primary); }
.mem-title p { margin: 2px 0 0; font-size: 12px; color: var(--text-muted); }
.mem-badge { display: grid; place-items: center; width: 34px; height: 34px; border-radius: 6px; background: #16a34a; color: white; font-weight: 800; font-size: 12px; }
.mem-actions { display: flex; align-items: center; gap: 8px; }
.mem-select { width: 220px; }
.mem-empty { flex: 1; display: grid; place-content: center; text-align: center; color: var(--text-muted); }
.mem-empty h2 { color: var(--text-primary); margin: 0 0 6px; }
.mem-status { padding: 12px 16px; border-bottom: 1px solid var(--border); background: var(--bg-surface); }
.mem-conn { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--text-muted); margin-bottom: 12px; }
.mem-dot { width: 8px; height: 8px; border-radius: 50%; background: #ef4444; }
.mem-conn--ok .mem-dot { background: #22c55e; }
.mem-conn strong { color: var(--text-primary); }
.mem-stats { display: grid; grid-template-columns: repeat(8, minmax(90px, 1fr)); gap: 8px; }
.mem-stat { padding: 8px 10px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-elevated); }
.mem-stat span { display: block; font-size: 10px; text-transform: uppercase; color: var(--text-muted); font-weight: 700; }
.mem-stat strong { display: block; margin-top: 4px; font-size: 13px; color: var(--text-primary); word-break: break-word; }
.mem-body { flex: 1; min-height: 0; display: flex; overflow: hidden; }
.mem-side { width: 300px; flex-shrink: 0; padding: 12px; border-right: 1px solid var(--border); background: var(--bg-surface); overflow-y: auto; }
.mem-search { display: flex; gap: 8px; }
.mem-new { width: 100%; margin: 10px 0 14px; }
.mem-side-title { font-size: 11px; font-weight: 800; text-transform: uppercase; color: var(--text-muted); margin-bottom: 8px; }
.mem-key { width: 100%; border: 0; background: transparent; color: var(--text-secondary); text-align: left; padding: 8px 10px; border-radius: 6px; font-family: var(--mono, monospace); cursor: pointer; word-break: break-all; }
.mem-key:hover, .mem-key--active { background: var(--bg-elevated); color: var(--text-primary); }
.mem-muted { color: var(--text-muted); font-size: 12px; line-height: 1.45; }
.mem-editor { flex: 1; min-width: 0; overflow-y: auto; padding: 16px; }
.mem-editor-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 14px; }
.mem-editor-head h2 { margin: 0; font-size: 17px; color: var(--text-primary); font-family: var(--mono, monospace); word-break: break-all; }
.mem-editor-head p { margin: 4px 0 0; color: var(--text-muted); font-size: 12px; }
.mem-form { display: grid; grid-template-columns: 1fr 140px 120px; gap: 12px; margin-bottom: 12px; }
.mem-form label, .mem-value { display: flex; flex-direction: column; gap: 6px; }
.mem-form span, .mem-value span { color: var(--text-muted); font-size: 11px; font-weight: 800; text-transform: uppercase; }
.mem-value textarea { min-height: 320px; resize: vertical; font-family: var(--mono, monospace); line-height: 1.45; }
.mem-editor-actions { display: flex; justify-content: flex-end; margin-top: 12px; }
@media (max-width: 900px) {
  .mem-toolbar, .mem-body { flex-direction: column; align-items: stretch; }
  .mem-side { width: auto; border-right: 0; border-bottom: 1px solid var(--border); }
  .mem-stats, .mem-form { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
</style>
