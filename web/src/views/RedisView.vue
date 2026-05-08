<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useConnections } from '@/composables/useConnections'
import { useRedis, type RedisKeySummary, type RedisScriptResult, type RedisValueResponse, type RedisWritableType } from '@/composables/useRedis'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'

const props = defineProps<{ activeConnId: number | null }>()
const emit = defineEmits<{ (e: 'set-conn', id: number): void }>()

const { connections, fetchConnections } = useConnections()
const { ping, fetchKeys, fetchValue, saveKey, deleteKey, renameKey, moveKey, runCommand, generateScript, executeScript } = useRedis()
const toast = useToast()
const { confirm } = useConfirm()

const pattern = ref('*')
const cursor = ref('0')
const selectedDb = ref(0)
const keys = ref<RedisKeySummary[]>([])
const selectedKey = ref('')
const value = ref<RedisValueResponse | null>(null)
const loadingKeys = ref(false)
const loadingValue = ref(false)
const redisConnected = ref(true)
const reconnecting = ref(false)
const connectionError = ref('')
const lastPingMs = ref<number | null>(null)
let keyLoadSeq = 0
let valueLoadSeq = 0
const activeWorkTab = ref<'value' | 'edit' | 'script' | 'console'>('value')
const saving = ref(false)
const editorOpen = ref(false)
const editingExisting = ref(false)
const treeMode = ref(true)
const formKey = ref('')
const formType = ref<RedisWritableType>('string')
const formTTL = ref<number>(0)
const formValue = ref('')
const renameOpen = ref(false)
const renameValue = ref('')
const moveOpen = ref(false)
const moveTargetDb = ref(1)
const moveOverwrite = ref(false)
const command = ref('')
const commandResult = ref<unknown>(null)
const runningCommand = ref(false)
const scriptText = ref('')
const generatingScript = ref(false)
const runningScript = ref(false)
const scriptResults = ref<RedisScriptResult[]>([])
const editorPanel = ref<HTMLElement | null>(null)
const renamePanel = ref<HTMLElement | null>(null)
const movePanel = ref<HTMLElement | null>(null)
const scriptPanel = ref<HTMLElement | null>(null)

const redisTypes: Array<{ value: RedisWritableType; label: string }> = [
  { value: 'string', label: 'String' },
  { value: 'hash', label: 'Hash' },
  { value: 'list', label: 'List' },
  { value: 'set', label: 'Set' },
  { value: 'zset', label: 'Sorted Set' },
  { value: 'stream', label: 'Stream' },
  { value: 'json', label: 'JSON' },
]

const redisDbIndexes = Array.from({ length: 16 }, (_, index) => index)

const keyGroups = computed(() => {
  const groups = new Map<string, RedisKeySummary[]>()
  for (const item of keys.value) {
    const idx = item.key.indexOf(':')
    const group = idx > 0 ? item.key.slice(0, idx) : 'root'
    if (!groups.has(group)) groups.set(group, [])
    groups.get(group)!.push(item)
  }
  return Array.from(groups.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([name, items]) => ({ name, items }))
})

const commandPreview = computed(() => {
  const key = formKey.value.trim() || '<key>'
  const ttl = Number(formTTL.value || 0)
  const lines: string[] = [`DEL ${quoteCommandArg(key)}`]
  switch (formType.value) {
    case 'string':
      lines.push(`SET ${quoteCommandArg(key)} ${quoteCommandArg(formValue.value)}`)
      break
    case 'hash':
      lines.push(`HSET ${quoteCommandArg(key)} <field> <value> ...`)
      break
    case 'list':
      lines.push(`RPUSH ${quoteCommandArg(key)} <item> ...`)
      break
    case 'set':
      lines.push(`SADD ${quoteCommandArg(key)} <member> ...`)
      break
    case 'zset':
      lines.push(`ZADD ${quoteCommandArg(key)} <score> <member> ...`)
      break
    case 'stream':
      lines.push(`XADD ${quoteCommandArg(key)} * <field> <value> ...`)
      break
    case 'json':
      lines.push(`JSON.SET ${quoteCommandArg(key)} $ <json>`)
      break
  }
  if (ttl > 0) lines.push(`EXPIRE ${quoteCommandArg(key)} ${ttl}`)
  return lines.join('\n')
})

const activeConn = computed(() =>
  props.activeConnId != null ? connections.value.find((c) => c.id === props.activeConnId) ?? null : null,
)
const redisConnections = computed(() => connections.value.filter((c) => c.driver === 'redis'))
const isRedis = computed(() => activeConn.value?.driver === 'redis')
const redisUsable = computed(() => isRedis.value && redisConnected.value)
const keyTypeCounts = computed(() => {
  const counts = new Map<string, number>()
  for (const item of keys.value) counts.set(item.type, (counts.get(item.type) || 0) + 1)
  return Array.from(counts.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([type, count]) => ({ type, count }))
})

onMounted(async () => {
  if (!connections.value.length) await fetchConnections()
  if (!isRedis.value && redisConnections.value.length === 1) {
    emit('set-conn', redisConnections.value[0].id)
    return
  }
  if (isRedis.value) await reconnectRedis(false)
})

watch(() => props.activeConnId, async () => {
  resetRedisWorkspace()
  redisConnected.value = true
  connectionError.value = ''
  lastPingMs.value = null
  selectedDb.value = Number(activeConn.value?.database || 0)
  if (isRedis.value) await reconnectRedis(false)
})

watch(selectedDb, async () => {
  resetRedisWorkspace()
  editorOpen.value = false
  renameOpen.value = false
  moveOpen.value = false
  scriptText.value = ''
  if (redisUsable.value) await loadKeys(true)
})

async function loadKeys(reset = false) {
  if (!activeConn.value || !redisUsable.value) return
  const seq = ++keyLoadSeq
  const db = selectedDb.value
  loadingKeys.value = true
  try {
    const nextCursor = reset ? '0' : cursor.value
    const data = await fetchKeys(activeConn.value.id, pattern.value || '*', nextCursor, 100, db)
    if (seq !== keyLoadSeq || db !== selectedDb.value) return
    cursor.value = data.cursor
    keys.value = reset ? data.keys : [...keys.value, ...data.keys]
  } catch {
    if (seq === keyLoadSeq) {
      connectionError.value = 'Failed to load Redis keys'
      toast.error('Failed to load Redis keys')
    }
  } finally {
    if (seq === keyLoadSeq) loadingKeys.value = false
  }
}

async function openKey(key: string) {
  if (!activeConn.value || !redisUsable.value) return
  const seq = ++valueLoadSeq
  const db = selectedDb.value
  selectedKey.value = key
  loadingValue.value = true
  try {
    const result = await fetchValue(activeConn.value.id, key, db)
    if (seq !== valueLoadSeq || db !== selectedDb.value) return
    value.value = result
    activeWorkTab.value = 'value'
  } catch {
    if (seq === valueLoadSeq) {
      connectionError.value = 'Failed to read Redis key'
      toast.error('Failed to read Redis key')
    }
  } finally {
    if (seq === valueLoadSeq) loadingValue.value = false
  }
}

async function openCreateForm() {
  if (!redisUsable.value) return
  editingExisting.value = false
  formKey.value = ''
  formType.value = 'string'
  formTTL.value = 0
  formValue.value = ''
  editorOpen.value = true
  renameOpen.value = false
  activeWorkTab.value = 'edit'
}

async function openEditForm() {
  if (!value.value) return
  editingExisting.value = true
  formKey.value = value.value.key
  formType.value = writableType(value.value.type)
  formTTL.value = value.value.ttl > 0 ? value.value.ttl : 0
  formValue.value = value.value.type === 'string'
    ? String(value.value.value ?? '')
    : JSON.stringify(value.value.value, null, 2)
  editorOpen.value = true
  renameOpen.value = false
  activeWorkTab.value = 'edit'
}

async function saveRedisKey() {
  if (!activeConn.value || !redisUsable.value) return
  const key = formKey.value.trim()
  if (!key) {
    toast.error('Key is required')
    return
  }
  let parsed: unknown
  try {
    parsed = parseEditorValue()
  } catch (err) {
    toast.error(err instanceof Error ? err.message : 'Invalid value')
    return
  }
  saving.value = true
  try {
    await saveKey(activeConn.value.id, {
      key,
      type: formType.value,
      value: parsed,
      ttl: Number(formTTL.value || 0),
    }, selectedDb.value)
    toast.success('Redis key saved')
    editorOpen.value = false
    await loadKeys(true)
    await openKey(key)
  } catch {
    toast.error('Failed to save Redis key')
  } finally {
    saving.value = false
  }
}

async function removeSelectedKey() {
  if (!activeConn.value || !value.value || !redisUsable.value) return
  const ok = await confirm(`Delete Redis key "${value.value.key}"? This cannot be undone.`, 'Delete Redis Key')
  if (!ok) return
  try {
    await deleteKey(activeConn.value.id, value.value.key, selectedDb.value)
    toast.success('Redis key deleted')
    selectedKey.value = ''
    value.value = null
    await loadKeys(true)
  } catch {
    toast.error('Failed to delete Redis key')
  }
}

async function openRenameForm() {
  if (!value.value) return
  renameValue.value = value.value.key
  renameOpen.value = true
  moveOpen.value = false
  editorOpen.value = false
  activeWorkTab.value = 'edit'
}

async function renameSelectedKey() {
  if (!activeConn.value || !value.value || !redisUsable.value) return
  const next = renameValue.value.trim()
  if (!next) {
    toast.error('New key name is required')
    return
  }
  try {
    const previous = value.value.key
    await renameKey(activeConn.value.id, previous, next, selectedDb.value)
    toast.success('Redis key renamed')
    renameOpen.value = false
    selectedKey.value = next
    await loadKeys(true)
    await openKey(next)
  } catch {
    toast.error('Failed to rename Redis key')
  }
}

async function openMoveForm() {
  if (!value.value) return
  moveTargetDb.value = selectedDb.value === 0 ? 1 : 0
  moveOverwrite.value = false
  moveOpen.value = true
  editorOpen.value = false
  renameOpen.value = false
  activeWorkTab.value = 'edit'
}

async function moveSelectedKey() {
  if (!activeConn.value || !value.value || !redisUsable.value) return
  const target = Number(moveTargetDb.value)
  if (!Number.isFinite(target) || target < 0) {
    toast.error('Target DB must be a non-negative number')
    return
  }
  if (target === selectedDb.value) {
    toast.error('Target DB must be different')
    return
  }
  const ok = await confirm(`Move "${value.value.key}" from DB ${selectedDb.value} to DB ${target}?`, 'Move Redis Key')
  if (!ok) return
  try {
    await moveKey(activeConn.value.id, value.value.key, selectedDb.value, target, moveOverwrite.value)
    toast.success('Redis key moved')
    value.value = null
    selectedKey.value = ''
    moveOpen.value = false
    await loadKeys(true)
  } catch {
    toast.error('Failed to move Redis key')
  }
}

async function executeCommand() {
  if (!activeConn.value || !redisUsable.value || !command.value.trim()) return
  runningCommand.value = true
  try {
    commandResult.value = await runCommand(activeConn.value.id, command.value, selectedDb.value)
  } catch {
    toast.error('Redis command failed')
  } finally {
    runningCommand.value = false
  }
}

async function generateKeyScript() {
  if (!activeConn.value || !value.value || !redisUsable.value) return
  generatingScript.value = true
  try {
    scriptText.value = await generateScript(activeConn.value.id, { key: value.value.key, db: selectedDb.value })
    scriptResults.value = []
    activeWorkTab.value = 'script'
  } catch {
    toast.error('Failed to generate Redis script')
  } finally {
    generatingScript.value = false
  }
}

async function generatePatternScript() {
  if (!activeConn.value || !redisUsable.value) return
  generatingScript.value = true
  try {
    scriptText.value = await generateScript(activeConn.value.id, { pattern: pattern.value || '*', db: selectedDb.value })
    scriptResults.value = []
    activeWorkTab.value = 'script'
  } catch {
    toast.error('Failed to generate Redis script')
  } finally {
    generatingScript.value = false
  }
}

async function scrollToPanel(panel: { value: HTMLElement | null }) {
  await nextTick()
  panel.value?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

async function runGeneratedScript() {
  if (!activeConn.value || !redisUsable.value || !scriptText.value.trim()) return
  const ok = await confirm('Run this Redis script against the selected connection?', 'Run Redis Script')
  if (!ok) return
  runningScript.value = true
  try {
    scriptResults.value = await executeScript(activeConn.value.id, scriptText.value, selectedDb.value)
    const failed = scriptResults.value.find(r => r.error)
    if (failed) toast.error(`Script stopped at line ${failed.line}`)
    else {
      toast.success('Redis script executed')
      await loadKeys(true)
      if (selectedKey.value) await openKey(selectedKey.value).catch(() => {})
    }
  } catch {
    toast.error('Failed to execute Redis script')
  } finally {
    runningScript.value = false
  }
}

async function selectRedisConnection(rawId: string | number) {
  const id = Number(rawId)
  if (!Number.isFinite(id)) return
  emit('set-conn', id)
}

function resetRedisWorkspace() {
  keyLoadSeq++
  valueLoadSeq++
  keys.value = []
  value.value = null
  selectedKey.value = ''
  cursor.value = '0'
  loadingKeys.value = false
  loadingValue.value = false
}

function disconnectRedis() {
  redisConnected.value = false
  connectionError.value = 'Disconnected'
  lastPingMs.value = null
  resetRedisWorkspace()
  toast.success('Redis disconnected')
}

async function reconnectRedis(showToast = true) {
  if (!activeConn.value || !isRedis.value) return
  reconnecting.value = true
  connectionError.value = ''
  try {
    const result = await ping(activeConn.value.id, selectedDb.value)
    redisConnected.value = true
    lastPingMs.value = result.latency_ms
    await loadKeys(true)
    if (showToast) toast.success(`Redis reconnected (${result.latency_ms}ms)`)
  } catch {
    redisConnected.value = false
    resetRedisWorkspace()
    connectionError.value = 'Redis connection failed'
    if (showToast) toast.error('Failed to reconnect Redis')
  } finally {
    reconnecting.value = false
  }
}

function ttlLabel(ttl: number) {
  if (ttl === -2) return 'missing'
  if (ttl === -1) return 'no expiry'
  if (ttl < 60) return `${ttl}s`
  if (ttl < 3600) return `${Math.floor(ttl / 60)}m`
  return `${Math.floor(ttl / 3600)}h`
}

function formatValue(raw: unknown) {
  if (typeof raw === 'string') return raw
  return JSON.stringify(raw, null, 2)
}

function valueRows(raw: unknown) {
  if (raw == null) return []
  if (Array.isArray(raw)) {
    return raw.map((item, index) => ({ key: String(index), value: typeof item === 'string' ? item : JSON.stringify(item) }))
  }
  if (typeof raw === 'object') {
    return Object.entries(raw as Record<string, unknown>).map(([key, item]) => ({
      key,
      value: typeof item === 'string' ? item : JSON.stringify(item),
    }))
  }
  return [{ key: 'value', value: String(raw) }]
}

function writableType(raw: string): RedisWritableType {
  const normalized = raw === 'ReJSON-RL' ? 'json' : raw
  return redisTypes.some(t => t.value === normalized) ? normalized as RedisWritableType : 'string'
}

function parseEditorValue() {
  if (formType.value === 'string') return formValue.value
  try {
    return JSON.parse(formValue.value)
  } catch {
    throw new Error('Value must be valid JSON for this Redis type')
  }
}

function valuePlaceholder(type: RedisWritableType) {
  switch (type) {
    case 'string':
      return 'plain text value'
    case 'hash':
      return '{\n  "field": "value"\n}'
    case 'list':
    case 'set':
      return '[\n  "item-1",\n  "item-2"\n]'
    case 'zset':
      return '[\n  { "member": "item-1", "score": 1 },\n  { "member": "item-2", "score": 2 }\n]'
    case 'stream':
      return '{\n  "field": "value"\n}'
    case 'json':
      return '{\n  "name": "Anveesa",\n  "enabled": true\n}'
  }
}

function quoteCommandArg(value: string) {
  return `"${value.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`
}
</script>

<template>
  <div class="redis-workbench">
    <section v-if="!isRedis" class="page-panel redis-empty">
      <div class="redis-empty__title">No Redis connection selected</div>
      <div v-if="redisConnections.length" class="redis-picker">
        <label class="form-label">Redis Connection</label>
        <select class="base-input" @change="selectRedisConnection(($event.target as HTMLSelectElement).value)">
          <option value="">Select Redis connection</option>
          <option v-for="conn in redisConnections" :key="conn.id" :value="conn.id">
            {{ conn.name }} - {{ conn.host }}:{{ conn.port }}
          </option>
        </select>
      </div>
      <div v-else class="redis-empty__sub">Create a Redis connection in Admin / Connections first.</div>
    </section>

    <template v-else>
      <aside class="redis-left">
        <div class="redis-panel-title">
          <span>Explorer</span>
          <button class="icon-btn" :disabled="loadingKeys || !redisUsable" title="Refresh keys" @click="loadKeys(true)">↻</button>
        </div>

        <div class="redis-left__top">
          <div class="redis-conn">
            <span class="redis-conn__badge">RD</span>
            <div>
              <div class="redis-conn__name">{{ activeConn?.name }}</div>
              <div class="redis-conn__sub">{{ activeConn?.host }}:{{ activeConn?.port }}</div>
              <div class="redis-conn__state" :class="{ 'is-offline': !redisConnected }">
                {{ redisConnected ? `Connected${lastPingMs !== null ? ` / ${lastPingMs}ms` : ''}` : connectionError || 'Disconnected' }}
              </div>
            </div>
          </div>
          <select v-model.number="selectedDb" class="base-input redis-db-select" :disabled="reconnecting" title="Redis database index">
            <option v-for="db in redisDbIndexes" :key="db" :value="db">DB {{ db }}</option>
          </select>
        </div>

        <div class="redis-db-tree">
          <div class="redis-tree-root">
            <span class="redis-tree-caret">▾</span>
            <span class="redis-tree-icon">◉</span>
            <span class="redis-tree-name">{{ activeConn?.name }}</span>
          </div>
          <div class="redis-tree-db">
            <span class="redis-tree-caret">▾</span>
            <span class="redis-tree-icon">▤</span>
            <span>Database {{ selectedDb }}</span>
            <span class="redis-tree-count">{{ keys.length }}</span>
          </div>
          <div class="redis-type-strip">
            <span v-for="item in keyTypeCounts" :key="item.type">{{ item.type }} {{ item.count }}</span>
          </div>
        </div>

        <div class="redis-searchbar">
          <input v-model="pattern" class="base-input" :disabled="!redisUsable" placeholder="Filter keys" @keydown.enter="loadKeys(true)" />
          <button class="icon-btn" :disabled="loadingKeys || !redisUsable" title="Scan keys" @click="loadKeys(true)">↻</button>
        </div>

        <div class="redis-sidebar-actions">
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="!redisUsable" @click="openCreateForm">New</button>
          <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!redisUsable" @click="treeMode = !treeMode">{{ treeMode ? 'Flat' : 'Tree' }}</button>
          <button class="base-btn base-btn--ghost base-btn--sm" :disabled="generatingScript || !redisUsable" @click="generatePatternScript">Script</button>
          <button v-if="redisConnected" class="base-btn base-btn--ghost base-btn--sm" :disabled="reconnecting" @click="disconnectRedis">Disconnect</button>
          <button v-else class="base-btn base-btn--primary base-btn--sm" :disabled="reconnecting" @click="reconnectRedis(true)">{{ reconnecting ? 'Connecting...' : 'Reconnect' }}</button>
        </div>

        <div v-if="!redisConnected" class="redis-disconnected">
          <div class="redis-empty__title">Redis is disconnected</div>
          <div class="redis-empty__sub">Reconnect to scan keys and run commands for DB {{ selectedDb }}.</div>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="reconnecting" @click="reconnectRedis(true)">{{ reconnecting ? 'Connecting...' : 'Reconnect' }}</button>
        </div>

        <div v-else-if="treeMode" class="redis-key-list redis-key-list--tree">
          <div v-for="group in keyGroups" :key="group.name" class="redis-tree-group">
            <div class="redis-tree-group__head">
              <span>▾ {{ group.name }}</span>
              <span>{{ group.items.length }}</span>
            </div>
            <button
              v-for="item in group.items"
              :key="item.key"
              class="redis-key redis-key--nested"
              :class="{ 'is-active': selectedKey === item.key }"
              @click="openKey(item.key)"
            >
              <span class="redis-key__name">{{ item.key.includes(':') ? item.key.slice(item.key.indexOf(':') + 1) : item.key }}</span>
              <span class="redis-key__meta">{{ item.type }} / {{ ttlLabel(item.ttl) }}</span>
            </button>
          </div>
          <div v-if="!loadingKeys && keys.length === 0" class="redis-muted">No keys found.</div>
        </div>

        <div v-else class="redis-key-list">
          <button
            v-for="item in keys"
            :key="item.key"
            class="redis-key"
            :class="{ 'is-active': selectedKey === item.key }"
            @click="openKey(item.key)"
          >
            <span class="redis-key__name">{{ item.key }}</span>
            <span class="redis-key__meta">{{ item.type }} / {{ ttlLabel(item.ttl) }}</span>
          </button>
          <div v-if="!loadingKeys && keys.length === 0" class="redis-muted">No keys found.</div>
        </div>

        <button v-if="redisConnected && cursor !== '0'" class="base-btn base-btn--ghost base-btn--sm redis-more" :disabled="loadingKeys" @click="loadKeys(false)">Load more</button>
      </aside>

      <main class="redis-main">
        <div class="redis-main-titlebar">
          <div class="redis-breadcrumbs">
            <span>{{ activeConn?.name }}</span>
            <span>/</span>
            <span>DB {{ selectedDb }}</span>
            <span v-if="value">/</span>
            <span v-if="value" class="redis-breadcrumbs__key">{{ value.key }}</span>
          </div>
          <div class="redis-main-actions">
            <button class="base-btn base-btn--ghost base-btn--sm" :disabled="loadingKeys || !redisUsable" @click="loadKeys(true)">Refresh</button>
            <button v-if="redisConnected" class="base-btn base-btn--ghost base-btn--sm" :disabled="reconnecting" @click="disconnectRedis">Disconnect</button>
            <button v-else class="base-btn base-btn--primary base-btn--sm" :disabled="reconnecting" @click="reconnectRedis(true)">{{ reconnecting ? 'Connecting...' : 'Reconnect' }}</button>
          </div>
        </div>

        <div class="redis-tabbar">
          <button class="redis-tab" :class="{ active: activeWorkTab === 'value' }" @click="activeWorkTab = 'value'"><span>▦</span> Data</button>
          <button class="redis-tab" :class="{ active: activeWorkTab === 'edit' }" @click="activeWorkTab = 'edit'"><span>✎</span> Edit</button>
          <button class="redis-tab" :class="{ active: activeWorkTab === 'script' }" @click="activeWorkTab = 'script'"><span>⌘</span> Script</button>
          <button class="redis-tab" :class="{ active: activeWorkTab === 'console' }" @click="activeWorkTab = 'console'"><span>&gt;_</span> Console</button>
        </div>

        <div v-if="!redisConnected" class="redis-offline-banner">
          <span>{{ connectionError || 'Disconnected' }}</span>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="reconnecting" @click="reconnectRedis(true)">{{ reconnecting ? 'Connecting...' : 'Reconnect' }}</button>
        </div>

        <section v-if="activeWorkTab === 'value'" class="redis-tabbody" :class="{ 'is-disabled': !redisConnected }">
          <div v-if="loadingValue" class="redis-muted">Loading value...</div>
          <template v-else-if="value">
            <div class="redis-object-head">
              <div>
                <div class="redis-detail__key">{{ value.key }}</div>
                <div class="redis-detail__meta">
                  {{ value.type }} / {{ ttlLabel(value.ttl) }}
                  <span v-if="value.length != null"> / {{ value.length }} item{{ value.length === 1 ? '' : 's' }}</span>
                  <span v-if="value.truncated"> / preview</span>
                </div>
              </div>
              <div class="redis-detail__actions">
                <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!redisUsable" @click="openEditForm">Edit</button>
                <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!redisUsable" @click="openRenameForm">Rename</button>
                <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!redisUsable" @click="openMoveForm">Move</button>
                <button class="base-btn base-btn--ghost base-btn--sm" :disabled="generatingScript || !redisUsable" @click="generateKeyScript">Script</button>
                <button class="base-btn base-btn--danger base-btn--sm" :disabled="!redisUsable" @click="removeSelectedKey">Delete</button>
              </div>
            </div>

            <div class="redis-data-toolbar">
              <span class="redis-data-toolbar__item">{{ value.type }}</span>
              <span class="redis-data-toolbar__item">TTL {{ ttlLabel(value.ttl) }}</span>
              <span v-if="value.length != null" class="redis-data-toolbar__item">{{ value.length }} row{{ value.length === 1 ? '' : 's' }}</span>
              <span v-if="value.truncated" class="redis-data-toolbar__item">Preview</span>
            </div>

            <div class="redis-table-wrap">
              <table class="redis-data-table">
                <thead>
                  <tr>
                    <th>Key / Index</th>
                    <th>Value</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="row in valueRows(value.value)" :key="row.key">
                    <td>{{ row.key }}</td>
                    <td><code>{{ row.value }}</code></td>
                  </tr>
                </tbody>
              </table>
            </div>
          </template>
          <div v-else class="redis-muted redis-empty-work">Select a key from the explorer.</div>
        </section>

        <section v-if="activeWorkTab === 'edit'" class="redis-tabbody" :class="{ 'is-disabled': !redisConnected }">
          <div class="redis-editor__head">
            <div class="redis-empty__title">{{ editingExisting ? 'Edit Redis Key' : renameOpen ? 'Rename Redis Key' : moveOpen ? 'Move Redis Key' : 'Create Redis Key' }}</div>
          </div>

          <div v-if="renameOpen" class="redis-editor__grid redis-editor__grid--rename">
            <div class="form-group">
              <label class="form-label">New Key</label>
              <input v-model="renameValue" class="base-input" @keydown.enter="renameSelectedKey" />
            </div>
            <div class="redis-editor__actions">
              <button class="base-btn base-btn--ghost base-btn--sm" @click="renameOpen = false">Cancel</button>
              <button class="base-btn base-btn--primary base-btn--sm" :disabled="!redisUsable" @click="renameSelectedKey">Rename</button>
            </div>
          </div>

          <div v-else-if="moveOpen">
            <div class="redis-editor__grid redis-editor__grid--move">
              <div class="form-group">
                <label class="form-label">From DB</label>
                <input class="base-input" :value="`DB ${selectedDb}`" disabled />
              </div>
              <div class="form-group">
                <label class="form-label">Target DB</label>
                <select v-model.number="moveTargetDb" class="base-input">
                  <option v-for="db in redisDbIndexes" :key="db" :value="db" :disabled="db === selectedDb">DB {{ db }}</option>
                </select>
              </div>
              <label class="redis-check"><input v-model="moveOverwrite" type="checkbox" />Overwrite target key</label>
            </div>
            <div class="redis-editor__actions">
              <button class="base-btn base-btn--ghost base-btn--sm" @click="moveOpen = false">Cancel</button>
              <button class="base-btn base-btn--primary base-btn--sm" :disabled="!redisUsable" @click="moveSelectedKey">Move Key</button>
            </div>
          </div>

          <div v-else>
            <div class="redis-editor__grid">
              <div class="form-group">
                <label class="form-label">Key</label>
                <input v-model="formKey" class="base-input" :disabled="editingExisting" placeholder="app:user:1" />
              </div>
              <div class="form-group">
                <label class="form-label">Type</label>
                <select v-model="formType" class="base-input">
                  <option v-for="type in redisTypes" :key="type.value" :value="type.value">{{ type.label }}</option>
                </select>
              </div>
              <div class="form-group">
                <label class="form-label">TTL Seconds</label>
                <input v-model.number="formTTL" class="base-input" type="number" min="0" placeholder="0" />
              </div>
            </div>
            <div class="redis-edit-split">
              <div class="form-group">
                <label class="form-label">Value</label>
                <textarea v-model="formValue" class="base-input redis-editor__value" rows="12" :placeholder="valuePlaceholder(formType)" />
              </div>
              <div class="form-group">
                <label class="form-label">Command Preview</label>
                <pre class="redis-value redis-editor__preview">{{ commandPreview }}</pre>
              </div>
            </div>
            <div class="redis-editor__actions">
              <button class="base-btn base-btn--ghost base-btn--sm" @click="editorOpen = false">Cancel</button>
              <button class="base-btn base-btn--primary base-btn--sm" :disabled="saving || !redisUsable" @click="saveRedisKey">{{ saving ? 'Saving...' : 'Save Key' }}</button>
            </div>
          </div>
        </section>

        <section v-if="activeWorkTab === 'script'" class="redis-tabbody" :class="{ 'is-disabled': !redisConnected }">
          <div class="redis-editor__head">
            <div class="redis-empty__title">Generated Redis Script</div>
            <div class="redis-detail__actions">
              <button class="base-btn base-btn--ghost base-btn--sm" :disabled="generatingScript || !redisUsable" @click="generatePatternScript">Generate Pattern</button>
              <button class="base-btn base-btn--primary base-btn--sm" :disabled="runningScript || !scriptText || !redisUsable" @click="runGeneratedScript">{{ runningScript ? 'Running...' : 'Run Script' }}</button>
            </div>
          </div>
          <textarea v-model="scriptText" class="base-input redis-editor__value redis-script-editor" rows="14" />
          <pre v-if="scriptResults.length" class="redis-value redis-console__result">{{ formatValue(scriptResults) }}</pre>
        </section>

        <section v-if="activeWorkTab === 'console'" class="redis-tabbody" :class="{ 'is-disabled': !redisConnected }">
          <div class="redis-empty__title">Command Console</div>
          <div class="redis-console__row">
            <input v-model="command" class="base-input redis-console__input" :disabled="!redisUsable" placeholder='GET "app:user:1"' @keydown.enter="executeCommand" />
            <button class="base-btn base-btn--primary base-btn--sm" :disabled="runningCommand || !redisUsable" @click="executeCommand">{{ runningCommand ? 'Running...' : 'Run' }}</button>
          </div>
          <pre v-if="commandResult !== null" class="redis-value redis-console__result">{{ formatValue(commandResult) }}</pre>
        </section>
      </main>
    </template>
  </div>
</template>

<style scoped>
.redis-workbench {
  display: grid;
  grid-template-columns: 260px minmax(0, 1fr);
  height: 100%;
  min-height: calc(100vh - 76px);
  background: var(--bg-body);
  border-top: 1px solid var(--border);
}

.redis-left {
  display: flex;
  flex-direction: column;
  min-width: 0;
  flex-shrink: 0;
  border-right: 1px solid var(--border);
  background: var(--bg-surface);
  overflow: hidden;
  box-shadow: 2px 0 12px rgba(0,0,0,.03);
}

.redis-panel-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
  background: color-mix(in srgb, var(--bg-surface) 95%, transparent);
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 600;
  flex-shrink: 0;
}

.redis-left__top {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 68px;
  gap: 8px;
  padding: 10px;
  border-bottom: 1px solid var(--border);
}

.redis-conn {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.redis-conn__badge {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  border-radius: 6px;
  background: rgba(198, 48, 43, 0.14);
  color: #c6302b;
  font-size: 10px;
  font-weight: 800;
  flex-shrink: 0;
}

.redis-conn__name,
.redis-conn__sub {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.redis-conn__name {
  color: var(--text-primary);
  font-size: 12px;
  font-weight: 700;
}

.redis-conn__sub {
  color: var(--text-muted);
  font-family: var(--mono);
  font-size: 10.5px;
}

.redis-conn__state {
  margin-top: 2px;
  color: #21834a;
  font-size: 10.5px;
  font-weight: 700;
}

.redis-conn__state.is-offline {
  color: var(--danger);
}

.redis-db-tree {
  display: grid;
  gap: 2px;
  padding: 8px 10px;
  border-bottom: 1px solid var(--border);
}

.redis-tree-root,
.redis-tree-db {
  display: grid;
  grid-template-columns: 16px 18px minmax(0, 1fr) auto;
  align-items: center;
  min-height: 26px;
  color: var(--text-primary);
  font-size: 12px;
}

.redis-tree-db {
  margin-left: 16px;
  color: var(--text-muted);
}

.redis-tree-caret,
.redis-tree-icon,
.redis-tree-count {
  color: var(--text-muted);
  font-size: 11px;
}

.redis-tree-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.redis-type-strip {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin: 4px 0 0 34px;
}

.redis-type-strip span {
  padding: 2px 6px;
  border: 1px solid var(--border);
  border-radius: 4px;
  background: var(--bg-body);
  color: var(--text-muted);
  font-size: 10.5px;
}

.redis-searchbar {
  display: flex;
  gap: 6px;
  padding: 10px;
  border-bottom: 1px solid var(--border);
}

.redis-sidebar-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding: 8px 10px;
  border-bottom: 1px solid var(--border);
}

.redis-disconnected {
  display: grid;
  gap: 10px;
  margin: 10px;
  padding: 14px;
  border: 1px dashed var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-body);
}

.redis-main {
  display: flex;
  flex-direction: column;
  min-width: 0;
  background: var(--bg-body);
}

.redis-main-titlebar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  min-height: 48px;
  padding: 12px 18px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-surface);
  flex-shrink: 0;
}

.redis-breadcrumbs,
.redis-main-actions,
.redis-data-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.redis-breadcrumbs {
  color: var(--text-muted);
  font-family: var(--mono);
  font-size: 12px;
}

.redis-breadcrumbs__key {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-primary);
}

.redis-tabbar {
  display: flex;
  gap: 0;
  min-height: 32px;
  padding: 0 4px;
  background: var(--bg-elevated);
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
  overflow-x: auto;
  scrollbar-width: none;
}

.redis-tabbar::-webkit-scrollbar {
  display: none;
}

.redis-tab {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 32px;
  padding: 0 10px;
  border: none;
  border-bottom: 2px solid transparent;
  border-radius: 0;
  background: transparent;
  color: var(--text-muted);
  font-size: 11.5px;
  cursor: pointer;
  white-space: nowrap;
}

.redis-tab.active {
  background: var(--bg-surface);
  border-bottom-color: var(--brand);
  color: var(--text-primary);
}

.redis-offline-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border);
  background: color-mix(in srgb, var(--danger) 8%, var(--bg-surface));
  color: var(--text-primary);
  font-size: 12px;
  font-weight: 700;
}

.redis-tabbody {
  min-height: 0;
  padding: 0;
  overflow: auto;
}

.redis-tabbody.is-disabled {
  opacity: 0.72;
}

.redis-object-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  min-height: 48px;
  padding: 12px 18px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-surface);
  flex-shrink: 0;
}

.redis-data-toolbar {
  min-height: 36px;
  padding: 6px 18px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-surface);
  flex-shrink: 0;
}

.redis-data-toolbar__item {
  padding: 3px 7px;
  border: 1px solid var(--border);
  border-radius: 4px;
  background: var(--bg-body);
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 700;
}

.redis-table-wrap {
  min-height: 0;
  overflow: auto;
}

.redis-data-table {
  width: 100%;
  border-collapse: collapse;
  font-family: var(--mono);
  font-size: 12.5px;
}

.redis-data-table thead th {
  position: sticky;
  top: 0;
  z-index: 2;
  padding: 7px 12px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-elevated);
  color: var(--text-muted);
  font-family: 'Inter', sans-serif;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.4px;
  text-align: left;
  text-transform: uppercase;
  white-space: nowrap;
}

.redis-data-table tbody tr:hover td {
  background: var(--bg-hover);
}

.redis-data-table tbody tr:nth-child(even) td {
  background: rgba(255,255,255,0.01);
}

html[data-theme='light'] .redis-data-table tbody tr:nth-child(even) td {
  background: rgba(0,0,0,0.01);
}

.redis-data-table tbody td {
  max-width: 520px;
  padding: 6px 12px;
  border-bottom: 1px solid var(--border);
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  vertical-align: middle;
  white-space: nowrap;
}

.redis-data-table tbody td:first-child {
  width: 240px;
  max-width: 260px;
  color: var(--text-muted);
}

.redis-data-table code {
  font-family: var(--mono);
  font-size: 12.5px;
}

.redis-empty-work {
  padding: 28px;
}

.redis-edit-split {
  display: grid;
  grid-template-columns: minmax(0, 1.2fr) minmax(260px, 0.8fr);
  gap: 12px;
}

.redis-layout {
  display: grid;
  grid-template-columns: minmax(280px, 380px) minmax(0, 1fr);
  gap: 16px;
  align-items: start;
}

.redis-keys,
.redis-detail,
.redis-empty {
  padding: 16px;
}

.redis-toolbar {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}

.redis-db-select {
  width: 68px;
  height: 28px;
  padding: 0 6px;
  flex-shrink: 0;
  font-size: 11px;
}

.redis-key-list {
  display: flex;
  flex-direction: column;
  gap: 0;
  max-height: none;
  min-height: 0;
  flex: 1;
  overflow: auto;
  padding: 4px 0;
}

.redis-key {
  width: 100%;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
  padding: 6px 10px 6px 18px;
  border: 1px solid transparent;
  border-radius: 0;
  background: transparent;
  color: var(--text-primary);
  text-align: left;
  cursor: pointer;
}

.redis-key:hover,
.redis-key.is-active {
  border-color: transparent;
  background: color-mix(in srgb, var(--brand) 11%, var(--bg-surface));
}

.redis-key--nested {
  margin-top: 0;
  padding-left: 28px;
}

.redis-tree-group {
  border-bottom: 1px solid var(--border);
  padding-bottom: 4px;
}

.redis-tree-group__head {
  display: flex;
  justify-content: space-between;
  padding: 7px 10px;
  background: color-mix(in srgb, var(--bg-body) 72%, var(--bg-surface));
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
}

.redis-key__name,
.redis-detail__key {
  font-family: var(--mono);
  font-size: 12px;
  overflow-wrap: anywhere;
}

.redis-key__meta,
.redis-detail__meta,
.redis-muted,
.redis-empty__sub {
  color: var(--text-muted);
  font-size: 12px;
}

.redis-more {
  width: 100%;
  justify-content: center;
  margin: 6px 0 10px;
}

.redis-detail__head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.redis-detail__actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}

.redis-value {
  margin: 0;
  min-height: 260px;
  max-height: calc(100vh - 285px);
  overflow: auto;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-body);
  color: var(--text-primary);
  font-family: var(--mono);
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.redis-empty__title {
  font-weight: 700;
  margin-bottom: 4px;
}

.redis-picker {
  max-width: 360px;
  margin-top: 12px;
}

.redis-editor {
  padding: 16px;
}

.redis-editor__head,
.redis-editor__actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.redis-editor__grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 160px 140px;
  gap: 10px;
  margin: 12px 0;
}

.redis-editor__grid--rename {
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: end;
}

.redis-editor__grid--move {
  grid-template-columns: 1fr 1fr auto;
  align-items: end;
}

.redis-editor__value {
  font-family: var(--mono);
  font-size: 12px;
  resize: vertical;
}

.redis-editor__preview {
  min-height: 86px;
  max-height: 160px;
}

.redis-editor__actions {
  justify-content: flex-end;
}

.redis-check {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 36px;
  color: var(--text-muted);
  font-size: 12px;
}

.redis-console {
  padding: 16px;
}

.redis-console__row {
  display: flex;
  gap: 8px;
  margin-top: 12px;
}

.redis-console__input {
  font-family: var(--mono);
}

.redis-console__result {
  min-height: 120px;
  margin-top: 12px;
}

.redis-script-editor {
  margin-top: 12px;
  min-height: 220px;
}

@media (max-width: 900px) {
  .redis-workbench {
    grid-template-columns: 1fr;
  }

  .redis-left {
    min-height: 360px;
    border-right: 0;
    border-bottom: 1px solid var(--border);
  }

  .redis-edit-split {
    grid-template-columns: 1fr;
  }

  .redis-data-table tbody td {
    max-width: 220px;
  }

  .redis-layout {
    grid-template-columns: 1fr;
  }

  .redis-editor__grid {
    grid-template-columns: 1fr;
  }

  .redis-editor__grid--rename,
  .redis-editor__grid--move,
  .redis-console__row {
    grid-template-columns: 1fr;
    flex-direction: column;
  }
}
</style>
