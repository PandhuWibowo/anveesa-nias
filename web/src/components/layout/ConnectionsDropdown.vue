<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useConnections, type Connection } from '@/composables/useConnections'
import { useFolders, type ConnectionFolder } from '@/composables/useFolders'
import { useConfirm } from '@/composables/useConfirm'
import { useToast } from '@/composables/useToast'

const props = defineProps<{ activeConnId: number | null }>()
const emit = defineEmits<{
  (e: 'select-conn', id: number): void
  (e: 'close'): void
}>()

const router = useRouter()
const { connections, removeConnection, fetchConnections } = useConnections()
const { folders, fetchFolders, createFolder, updateFolder, deleteFolder, moveConnection, setConnectionVisibility } = useFolders()
const { confirm } = useConfirm()
const toast = useToast()

const connSearch = ref('')
const collapsedFolders = ref(new Set<number>())

// New folder form
const showNewFolder = ref(false)
const newFolderName = ref('')
const newFolderColor = ref('#4f9cf9')
const newFolderVisibility = ref<'private' | 'shared'>('private')
const newFolderParent = ref<number | null>(null)

// Edit folder
const editingFolder = ref<ConnectionFolder | null>(null)
const editFolderName = ref('')
const editFolderColor = ref('')
const editFolderVisibility = ref<'private' | 'shared'>('private')

// Context menu
const contextMenu = ref<{ x: number; y: number; type: 'folder' | 'conn'; item: ConnectionFolder | Connection } | null>(null)

const FOLDER_COLORS = ['#4f9cf9','#56c490','#f97f4f','#c45ef9','#f9d44f','#f9584f','#4fc8f9','#9cf94f','#f94f9c']
const driverLabel: Record<string, string> = { postgres: 'PG', mysql: 'MY', mariadb: 'MB', mssql: 'MS' }
const driverColor: Record<string, string> = { postgres: '#336791', mysql: '#f29111', mariadb: '#c0392b', mssql: '#cc2927' }

onMounted(async () => {
  await fetchFolders()
  // Ensure connections are loaded too (in case this is the first render)
  if (!connections.value.length) await fetchConnections()
})

const filteredConns = computed(() =>
  connSearch.value
    ? connections.value.filter(c => c.name.toLowerCase().includes(connSearch.value.toLowerCase()))
    : connections.value
)

// Set of folder ids that actually exist — used to fall back orphaned connections to Unfiled
const knownFolderIds = computed(() => new Set(folders.value.map(f => f.id)))

const connsByFolder = computed(() => {
  const map = new Map<number | null, Connection[]>()
  map.set(null, [])
  for (const f of folders.value) map.set(f.id, [])
  for (const c of filteredConns.value) {
    // If folder_id points to a folder that doesn't exist yet (race) or was deleted → treat as Unfiled
    const key = (c.folder_id != null && knownFolderIds.value.has(c.folder_id))
      ? c.folder_id
      : null
    map.get(key)!.push(c)
  }
  return map
})

const rootFolders = computed(() => folders.value.filter(f => f.parent_id === null))
function childFolders(parentId: number) { return folders.value.filter(f => f.parent_id === parentId) }
function toggleFolder(id: number) {
  if (collapsedFolders.value.has(id)) collapsedFolders.value.delete(id)
  else collapsedFolders.value.add(id)
}

function selectConn(conn: Connection) {
  emit('select-conn', conn.id)
  emit('close')
  const stayViews = ['data', 'er', 'dashboard']
  const current = router.currentRoute.value.name as string
  if (!stayViews.includes(current)) router.push({ name: 'data' })
}

// ── Folder ops ──
async function submitNewFolder() {
  if (!newFolderName.value.trim()) return
  const result = await createFolder({
    name: newFolderName.value.trim(),
    parent_id: newFolderParent.value,
    visibility: newFolderVisibility.value,
    color: newFolderColor.value,
  })
  if (result) {
    newFolderName.value = ''
    showNewFolder.value = false
    toast.success(`Folder "${result.name}" created`)
  } else {
    toast.error('Failed to create folder — check server logs')
  }
}

function startEditFolder(f: ConnectionFolder) {
  editingFolder.value = f; editFolderName.value = f.name
  editFolderColor.value = f.color; editFolderVisibility.value = f.visibility
  closeCtx()
}

async function submitEditFolder() {
  if (!editingFolder.value || !editFolderName.value.trim()) return
  await updateFolder(editingFolder.value.id, { name: editFolderName.value.trim(), color: editFolderColor.value, visibility: editFolderVisibility.value, parent_id: editingFolder.value.parent_id })
  editingFolder.value = null
}

async function doDeleteFolder(f: ConnectionFolder) {
  const ok = await confirm(`Delete folder "${f.name}"?`, 'Delete Folder')
  if (!ok) return
  await deleteFolder(f.id); toast.success('Folder deleted'); closeCtx()
}

async function doDeleteConn(c: Connection) {
  const ok = await confirm(`Delete connection "${c.name}"?`, 'Delete Connection')
  if (!ok) return
  await removeConnection(c.id); toast.success('Connection deleted'); closeCtx()
}

async function toggleFolderVis(f: ConnectionFolder) {
  await updateFolder(f.id, { ...f, visibility: f.visibility === 'shared' ? 'private' : 'shared' }); closeCtx()
}

async function toggleConnVis(c: Connection) {
  await setConnectionVisibility(c.id, c.visibility === 'shared' ? 'private' : 'shared')
  await fetchConnections(); closeCtx()
}

async function moveConnToFolder(connId: number, folderId: number | null) {
  await moveConnection(connId, folderId); await fetchConnections(); closeCtx()
}

// ── Context menu ──
function openCtx(e: MouseEvent, type: 'folder' | 'conn', item: ConnectionFolder | Connection) {
  e.preventDefault(); e.stopPropagation()
  contextMenu.value = { x: e.clientX, y: e.clientY, type, item }
}
function closeCtx() { contextMenu.value = null }

// ── Drag & Drop ──
const draggedConn = ref<Connection | null>(null)
const draggedFolder = ref<ConnectionFolder | null>(null)
const dropTarget = ref<{ type: string; id: number | null; position?: string } | null>(null)

function onConnDragStart(e: DragEvent, conn: Connection) {
  draggedConn.value = conn; draggedFolder.value = null
  if (e.dataTransfer) { e.dataTransfer.effectAllowed = 'move'; e.dataTransfer.setData('text/plain', `conn:${conn.id}`) }
}
function onConnDragEnd() { draggedConn.value = null; dropTarget.value = null }
function onFolderDragStart(e: DragEvent, folder: ConnectionFolder) {
  draggedFolder.value = folder; draggedConn.value = null
  if (e.dataTransfer) { e.dataTransfer.effectAllowed = 'move'; e.dataTransfer.setData('text/plain', `folder:${folder.id}`) }
}
function onFolderDragEnd() { draggedFolder.value = null; dropTarget.value = null }
function onFolderDragOver(e: DragEvent, folderId: number | null) {
  if (!draggedConn.value && !draggedFolder.value) return
  e.preventDefault()
  if (draggedConn.value) dropTarget.value = { type: folderId === null ? 'unfiled' : 'folder', id: folderId }
  else if (draggedFolder.value && folderId !== null && draggedFolder.value.id !== folderId) {
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    dropTarget.value = { type: 'folder-reorder', id: folderId, position: e.clientY < rect.top + rect.height / 2 ? 'before' : 'after' }
  }
}
function onDragLeave(e: DragEvent) {
  const rel = e.relatedTarget as HTMLElement | null
  if (!rel || !(e.currentTarget as HTMLElement).contains(rel)) dropTarget.value = null
}
async function onFolderDrop(e: DragEvent, folderId: number | null) {
  e.preventDefault()
  const target = dropTarget.value; dropTarget.value = null
  if (draggedConn.value) {
    const conn = draggedConn.value; draggedConn.value = null
    if (conn.folder_id !== folderId) { await moveConnection(conn.id, folderId); await fetchConnections() }
  } else if (draggedFolder.value && target?.type === 'folder-reorder' && target.id !== null) {
    const dragged = draggedFolder.value; draggedFolder.value = null
    if (dragged.id === target.id) return
    const list = [...folders.value.filter(f => f.parent_id === dragged.parent_id)]
    const fromIdx = list.findIndex(f => f.id === dragged.id)
    const toIdx = list.findIndex(f => f.id === target.id)
    if (fromIdx < 0 || toIdx < 0) return
    list.splice(fromIdx, 1)
    const insertAt = target.position === 'before' ? toIdx : toIdx + (fromIdx < toIdx ? 0 : 1)
    list.splice(Math.max(0, Math.min(insertAt, list.length)), 0, dragged)
    for (let i = 0; i < list.length; i++) updateFolder(list[i].id, { ...list[i], sort_order: i } as never)
    const others = folders.value.filter(f => f.parent_id !== dragged.parent_id)
    folders.value.splice(0, folders.value.length, ...others, ...list)
  }
}
function isDropTarget(folderId: number | null) {
  if (!dropTarget.value) return false
  if (dropTarget.value.type === 'unfiled' && folderId === null) return true
  if (dropTarget.value.type === 'folder' && dropTarget.value.id === folderId) return true
  return false
}
</script>

<template>
  <!-- Panel -->
  <div class="cdrop">

    <!-- Header -->
    <div class="cdrop__header">
      <span class="cdrop__title">Connections</span>
      <div style="display:flex;gap:4px">
        <button class="cdrop__icon-btn" title="New folder" @click="showNewFolder = !showNewFolder">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/><line x1="12" y1="11" x2="12" y2="17"/><line x1="9" y1="14" x2="15" y2="14"/></svg>
        </button>
        <router-link :to="{ name: 'connections' }" class="cdrop__icon-btn" title="Manage connections" @click="$emit('close')">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
        </router-link>
      </div>
    </div>

    <!-- Search -->
    <div class="cdrop__search">
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" style="color:var(--text-muted);flex-shrink:0"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
      <input v-model="connSearch" class="cdrop__search-input" placeholder="Search connections…" autofocus />
    </div>

    <!-- New folder form -->
    <div v-if="showNewFolder" class="cdrop__folder-form">
      <input v-model="newFolderName" class="cdrop__form-input" placeholder="Folder name…" @keydown.enter="submitNewFolder" @keydown.esc="showNewFolder=false" autofocus />
      <div class="cdrop__form-row">
        <select v-model="newFolderVisibility" class="cdrop__form-select">
          <option value="private">🔒 Private</option>
          <option value="shared">🌐 Shared</option>
        </select>
        <select v-model="newFolderParent" class="cdrop__form-select">
          <option :value="null">No parent</option>
          <option v-for="f in folders" :key="f.id" :value="f.id">{{ f.name }}</option>
        </select>
      </div>
      <div class="cdrop__colors">
        <button v-for="c in FOLDER_COLORS" :key="c" class="cdrop__color-dot"
          :style="{ background: c, boxShadow: newFolderColor === c ? `0 0 0 2px var(--bg-elevated), 0 0 0 3px ${c}` : 'none' }"
          @click="newFolderColor = c" />
      </div>
      <div class="cdrop__form-actions">
        <button class="cdrop__btn" @click="showNewFolder=false">Cancel</button>
        <button class="cdrop__btn cdrop__btn--primary" @click="submitNewFolder" :disabled="!newFolderName.trim()">Create</button>
      </div>
    </div>

    <!-- Tree -->
    <div class="cdrop__tree">

      <!-- Root folders -->
      <template v-for="folder in rootFolders" :key="folder.id">
        <div class="cdrop__folder"
          :class="{ 'cdrop__folder--drop': isDropTarget(folder.id), 'cdrop__folder--dragging': draggedFolder?.id === folder.id }"
          draggable="true"
          @dragstart="onFolderDragStart($event, folder)" @dragend="onFolderDragEnd"
          @dragover="onFolderDragOver($event, folder.id)" @dragleave="onDragLeave" @drop="onFolderDrop($event, folder.id)"
          @contextmenu="openCtx($event, 'folder', folder)"
        >
          <span class="cdrop__drag-handle">⠿</span>
          <button class="cdrop__fold-toggle" @click.stop="toggleFolder(folder.id)">
            <svg width="9" height="9" viewBox="0 0 24 24" fill="currentColor"
              :style="{ transform: collapsedFolders.has(folder.id) ? 'rotate(-90deg)' : 'rotate(0deg)', transition: 'transform .15s' }">
              <path d="M5 3l14 9-14 9z"/>
            </svg>
          </button>
          <svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor" :style="{ color: folder.color, flexShrink: 0 }"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
          <span class="cdrop__folder-name">{{ folder.name }}</span>
          <span class="cdrop__vis" :title="folder.visibility">{{ folder.visibility === 'shared' ? '🌐' : '🔒' }}</span>
          <span class="cdrop__count">{{ (connsByFolder.get(folder.id) ?? []).length }}</span>
        </div>

        <template v-if="!collapsedFolders.has(folder.id)">
          <!-- Child folders -->
          <template v-for="child in childFolders(folder.id)" :key="child.id">
            <div class="cdrop__folder cdrop__folder--child"
              :class="{ 'cdrop__folder--drop': isDropTarget(child.id) }"
              draggable="true"
              @dragstart="onFolderDragStart($event, child)" @dragend="onFolderDragEnd"
              @dragover="onFolderDragOver($event, child.id)" @dragleave="onDragLeave" @drop="onFolderDrop($event, child.id)"
              @contextmenu="openCtx($event, 'folder', child)"
            >
              <span class="cdrop__drag-handle">⠿</span>
              <button class="cdrop__fold-toggle" @click.stop="toggleFolder(child.id)">
                <svg width="8" height="8" viewBox="0 0 24 24" fill="currentColor"
                  :style="{ transform: collapsedFolders.has(child.id) ? 'rotate(-90deg)' : 'rotate(0deg)', transition: 'transform .15s' }">
                  <path d="M5 3l14 9-14 9z"/>
                </svg>
              </button>
              <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor" :style="{ color: child.color, flexShrink: 0 }"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
              <span class="cdrop__folder-name">{{ child.name }}</span>
              <span class="cdrop__vis">{{ child.visibility === 'shared' ? '🌐' : '🔒' }}</span>
            </div>
            <template v-if="!collapsedFolders.has(child.id)">
              <div v-for="conn in (connsByFolder.get(child.id) ?? [])" :key="conn.id"
                class="cdrop__conn cdrop__conn--deep"
                :class="{ 'cdrop__conn--active': activeConnId === conn.id }"
                draggable="true"
                @dragstart="onConnDragStart($event, conn)" @dragend="onConnDragEnd"
                @click="selectConn(conn)" @contextmenu="openCtx($event, 'conn', conn)"
              >
                <span class="cdrop__drag-handle">⠿</span>
                <span class="cdrop__driver" :style="{ background: driverColor[conn.driver] ?? '#555' }">{{ driverLabel[conn.driver] ?? '??' }}</span>
                <span class="cdrop__conn-name">{{ conn.name }}</span>
                <span v-if="activeConnId === conn.id" class="cdrop__active-dot"></span>
              </div>
            </template>
          </template>

          <!-- Connections in folder -->
          <div v-for="conn in (connsByFolder.get(folder.id) ?? [])" :key="conn.id"
            class="cdrop__conn cdrop__conn--nested"
            :class="{ 'cdrop__conn--active': activeConnId === conn.id }"
            draggable="true"
            @dragstart="onConnDragStart($event, conn)" @dragend="onConnDragEnd"
            @click="selectConn(conn)" @contextmenu="openCtx($event, 'conn', conn)"
          >
            <span class="cdrop__drag-handle">⠿</span>
            <span class="cdrop__driver" :style="{ background: driverColor[conn.driver] ?? '#555' }">{{ driverLabel[conn.driver] ?? '??' }}</span>
            <div class="cdrop__conn-info">
              <span class="cdrop__conn-name">{{ conn.name }}</span>
              <span class="cdrop__conn-host">{{ conn.host }}{{ conn.port ? ':' + conn.port : '' }}</span>
            </div>
            <span v-if="activeConnId === conn.id" class="cdrop__active-dot"></span>
          </div>
        </template>
      </template>

      <!-- Unfiled -->
      <div class="cdrop__folder cdrop__folder--unfiled"
        :class="{ 'cdrop__folder--drop': isDropTarget(null) }"
        @dragover="onFolderDragOver($event, null)" @dragleave="onDragLeave" @drop="onFolderDrop($event, null)"
      >
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="flex-shrink:0;color:var(--text-muted)"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
        <span class="cdrop__folder-name" style="color:var(--text-muted);font-style:italic">Unfiled</span>
        <span class="cdrop__count">{{ (connsByFolder.get(null) ?? []).length }}</span>
      </div>
      <div v-for="conn in (connsByFolder.get(null) ?? [])" :key="conn.id"
        class="cdrop__conn cdrop__conn--nested"
        :class="{ 'cdrop__conn--active': activeConnId === conn.id }"
        draggable="true"
        @dragstart="onConnDragStart($event, conn)" @dragend="onConnDragEnd"
        @click="selectConn(conn)" @contextmenu="openCtx($event, 'conn', conn)"
      >
        <span class="cdrop__drag-handle">⠿</span>
        <span class="cdrop__driver" :style="{ background: driverColor[conn.driver] ?? '#555' }">{{ driverLabel[conn.driver] ?? '??' }}</span>
        <div class="cdrop__conn-info">
          <span class="cdrop__conn-name">{{ conn.name }}</span>
          <span class="cdrop__conn-host">{{ conn.host }}{{ conn.port ? ':' + conn.port : '' }}</span>
        </div>
        <span v-if="activeConnId === conn.id" class="cdrop__active-dot"></span>
      </div>

      <div v-if="connections.length === 0" class="cdrop__empty">
        <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" style="color:var(--text-muted)"><path d="M18.36 6.64a9 9 0 1 1-12.73 0"/><line x1="12" y1="2" x2="12" y2="12"/></svg>
        <span>No connections yet</span>
        <router-link :to="{ name: 'connections' }" class="cdrop__empty-link" @click="$emit('close')">Add connection →</router-link>
      </div>
    </div>

    <!-- Edit folder modal -->
    <Teleport to="body">
      <div v-if="editingFolder" class="cdrop-modal-overlay" @click.self="editingFolder=null">
        <div class="cdrop-modal">
          <div class="cdrop-modal__header">Edit Folder<button class="cdrop__icon-btn" @click="editingFolder=null">✕</button></div>
          <div class="cdrop-modal__body">
            <label class="cdrop-modal__label">Name</label>
            <input v-model="editFolderName" class="cdrop__form-input" />
            <label class="cdrop-modal__label">Visibility</label>
            <select v-model="editFolderVisibility" class="cdrop__form-select" style="width:100%">
              <option value="private">🔒 Private</option>
              <option value="shared">🌐 Shared</option>
            </select>
            <label class="cdrop-modal__label">Color</label>
            <div class="cdrop__colors">
              <button v-for="c in FOLDER_COLORS" :key="c" class="cdrop__color-dot"
                :style="{ background: c, boxShadow: editFolderColor === c ? `0 0 0 2px var(--bg-elevated), 0 0 0 3px ${c}` : 'none' }"
                @click="editFolderColor = c" />
            </div>
            <div class="cdrop__form-actions" style="margin-top:12px">
              <button class="cdrop__btn" @click="editingFolder=null">Cancel</button>
              <button class="cdrop__btn cdrop__btn--primary" @click="submitEditFolder">Save</button>
            </div>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- Context menu -->
    <Teleport to="body">
      <div v-if="contextMenu" class="ctx-backdrop" @click="closeCtx">
        <div class="ctx-menu" :style="{ top: contextMenu.y + 'px', left: contextMenu.x + 'px' }" @click.stop>
          <template v-if="contextMenu.type === 'folder'">
            <button class="ctx-item" @click="startEditFolder(contextMenu.item as ConnectionFolder)">✎ Edit folder</button>
            <button class="ctx-item" @click="toggleFolderVis(contextMenu.item as ConnectionFolder)">
              {{ (contextMenu.item as ConnectionFolder).visibility === 'shared' ? '🔒 Make private' : '🌐 Make shared' }}
            </button>
            <div class="ctx-divider"/>
            <button class="ctx-item ctx-item--danger" @click="doDeleteFolder(contextMenu.item as ConnectionFolder)">🗑 Delete folder</button>
          </template>
          <template v-else>
            <button class="ctx-item" @click="selectConn(contextMenu.item as Connection)">▶ Open</button>
            <button class="ctx-item" @click="toggleConnVis(contextMenu.item as Connection)">
              {{ (contextMenu.item as Connection).visibility === 'shared' ? '🔒 Make private' : '🌐 Make shared' }}
            </button>
            <div class="ctx-divider"/>
            <div class="ctx-submenu-label">Move to folder</div>
            <button class="ctx-item" @click="moveConnToFolder((contextMenu.item as Connection).id, null)">📂 Unfiled</button>
            <button v-for="f in folders" :key="f.id" class="ctx-item" @click="moveConnToFolder((contextMenu.item as Connection).id, f.id)">
              <span :style="{ color: f.color }">📁</span> {{ f.name }}
            </button>
            <div class="ctx-divider"/>
            <button class="ctx-item ctx-item--danger" @click="doDeleteConn(contextMenu.item as Connection)">🗑 Delete</button>
          </template>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.cdrop {
  display: flex;
  flex-direction: column;
  width: 300px;
  max-height: 520px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 12px;
  box-shadow: var(--shadow-lg);
  overflow: hidden;
}

.cdrop__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.cdrop__title {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.6px;
  color: var(--text-muted);
}
.cdrop__icon-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border: none;
  background: transparent;
  border-radius: 5px;
  color: var(--text-muted);
  cursor: pointer;
  transition: background 0.1s, color 0.1s;
  text-decoration: none;
}
.cdrop__icon-btn:hover { background: var(--bg-surface); color: var(--text-primary); }

.cdrop__search {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.cdrop__search-input {
  flex: 1;
  background: transparent;
  border: none;
  outline: none;
  font-size: 12.5px;
  color: var(--text-primary);
}
.cdrop__search-input::placeholder { color: var(--text-muted); }

.cdrop__folder-form {
  padding: 10px 12px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-surface);
  display: flex;
  flex-direction: column;
  gap: 6px;
  flex-shrink: 0;
}
.cdrop__form-input {
  width: 100%;
  padding: 6px 8px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 5px;
  font-size: 12px;
  color: var(--text-primary);
  outline: none;
}
.cdrop__form-input:focus { border-color: var(--brand); }
.cdrop__form-row { display: flex; gap: 6px; }
.cdrop__form-select {
  flex: 1;
  padding: 4px 6px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 5px;
  font-size: 11.5px;
  color: var(--text-primary);
}
.cdrop__colors { display: flex; gap: 5px; flex-wrap: wrap; }
.cdrop__color-dot {
  width: 16px; height: 16px;
  border-radius: 50%;
  border: none;
  cursor: pointer;
  transition: transform 0.1s;
}
.cdrop__color-dot:hover { transform: scale(1.2); }
.cdrop__form-actions { display: flex; gap: 6px; justify-content: flex-end; }
.cdrop__btn {
  padding: 5px 12px;
  border-radius: 5px;
  border: 1px solid var(--border);
  background: var(--bg-elevated);
  color: var(--text-secondary);
  font-size: 11.5px;
  cursor: pointer;
}
.cdrop__btn--primary { background: var(--brand); color: var(--brand-fg, #fff); border-color: var(--brand); }
.cdrop__btn--primary:disabled { opacity: 0.5; cursor: not-allowed; }

.cdrop__tree {
  flex: 1;
  overflow-y: auto;
  padding: 4px 0 8px;
}

/* Folder rows */
.cdrop__folder {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 5px 10px;
  cursor: default;
  transition: background 0.1s;
  border-radius: 0;
  user-select: none;
}
.cdrop__folder:hover { background: var(--bg-hover); }
.cdrop__folder--child { padding-left: 26px; }
.cdrop__folder--drop { background: var(--brand-dim); outline: 1px solid var(--brand); }
.cdrop__folder--dragging { opacity: 0.4; }
.cdrop__folder--unfiled { margin-top: 4px; border-top: 1px solid var(--border); padding-top: 8px; }

.cdrop__drag-handle {
  color: var(--text-muted);
  font-size: 12px;
  cursor: grab;
  opacity: 0.4;
  flex-shrink: 0;
}
.cdrop__drag-handle:hover { opacity: 1; }

.cdrop__fold-toggle {
  background: transparent;
  border: none;
  padding: 2px;
  cursor: pointer;
  color: var(--text-muted);
  display: flex;
  align-items: center;
  flex-shrink: 0;
}

.cdrop__folder-name {
  flex: 1;
  font-size: 12.5px;
  font-weight: 500;
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cdrop__vis { font-size: 10px; flex-shrink: 0; }
.cdrop__count {
  font-size: 10px;
  color: var(--text-muted);
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 1px 5px;
  flex-shrink: 0;
}

/* Connection rows */
.cdrop__conn {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 5px 10px;
  cursor: pointer;
  transition: background 0.1s;
  border-radius: 0;
}
.cdrop__conn:hover { background: var(--bg-hover); }
.cdrop__conn--nested { padding-left: 28px; }
.cdrop__conn--deep { padding-left: 44px; }
.cdrop__conn--active { background: var(--brand-dim); }
.cdrop__conn--active .cdrop__conn-name { color: var(--brand); font-weight: 600; }

.cdrop__driver {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 16px;
  border-radius: 3px;
  font-size: 9px;
  font-weight: 700;
  color: #fff;
  letter-spacing: 0.3px;
}

.cdrop__conn-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.cdrop__conn-name {
  font-size: 12.5px;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.cdrop__conn-host {
  font-size: 10.5px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cdrop__active-dot {
  width: 6px; height: 6px;
  border-radius: 50%;
  background: var(--brand);
  flex-shrink: 0;
}

.cdrop__empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 32px 20px;
  color: var(--text-muted);
  font-size: 12px;
}
.cdrop__empty-link {
  color: var(--brand);
  font-size: 12px;
  text-decoration: none;
}
.cdrop__empty-link:hover { text-decoration: underline; }

/* Modal */
.cdrop-modal-overlay {
  position: fixed; inset: 0;
  background: rgba(0,0,0,0.5);
  display: flex; align-items: center; justify-content: center;
  z-index: 9999;
}
.cdrop-modal {
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 10px;
  width: 320px;
  box-shadow: var(--shadow-lg);
  overflow: hidden;
}
.cdrop-modal__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 14px;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
  border-bottom: 1px solid var(--border);
}
.cdrop-modal__body { padding: 14px; display: flex; flex-direction: column; gap: 6px; }
.cdrop-modal__label { font-size: 11px; color: var(--text-muted); font-weight: 600; text-transform: uppercase; letter-spacing: 0.4px; }
</style>
