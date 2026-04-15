<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useConnections, type Connection } from '@/composables/useConnections'
import { useFolders, type ConnectionFolder } from '@/composables/useFolders'
import { useAuth } from '@/composables/useAuth'
import { useConfirm } from '@/composables/useConfirm'
import { useToast } from '@/composables/useToast'

const props = defineProps<{ activeConnId: number | null }>()
const emit = defineEmits<{ (e: 'select-conn', id: number): void }>()

const router = useRouter()
const { connections, removeConnection, fetchConnections } = useConnections()
const { folders, fetchFolders, createFolder, updateFolder, deleteFolder, moveConnection, setConnectionVisibility } = useFolders()
const { user } = useAuth()
const { confirm } = useConfirm()
const toast = useToast()

const collapsed = ref(false)
const connSearch = ref('')

// Folder state
const collapsedFolders = ref(new Set<number>())
const showNewFolder = ref(false)
const newFolderName = ref('')
const newFolderColor = ref('#4f9cf9')
const newFolderVisibility = ref<'private' | 'shared'>('private')
const newFolderParent = ref<number | null>(null)

// Edit folder state
const editingFolder = ref<ConnectionFolder | null>(null)
const editFolderName = ref('')
const editFolderColor = ref('')
const editFolderVisibility = ref<'private' | 'shared'>('private')

// Context menu
const contextMenu = ref<{ x: number; y: number; type: 'folder' | 'conn'; item: ConnectionFolder | Connection } | null>(null)

onMounted(() => fetchFolders())

const filteredConns = computed(() =>
  connSearch.value
    ? connections.value.filter(c => c.name.toLowerCase().includes(connSearch.value.toLowerCase()))
    : connections.value,
)

const connsByFolder = computed(() => {
  const map = new Map<number | null, Connection[]>()
  map.set(null, [])
  for (const f of folders.value) map.set(f.id, [])
  for (const c of filteredConns.value) {
    const key = c.folder_id ?? null
    if (!map.has(key)) map.set(key, [])
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

const driverLabel: Record<string, string> = { postgres: 'PG', mysql: 'MY', sqlite: 'SQ', mssql: 'MS' }

function selectConn(conn: Connection) {
  emit('select-conn', conn.id)
  const stayViews = ['schema', 'data', 'er', 'dashboard', 'query']
  const current = router.currentRoute.value.name as string
  if (!stayViews.includes(current)) router.push({ name: 'query' })
}

async function deleteConn(conn: Connection) {
  const ok = await confirm(`Delete connection "${conn.name}"? This cannot be undone.`, 'Delete Connection')
  if (!ok) return
  const success = await removeConnection(conn.id)
  if (success) { toast.success('Connection deleted'); closeContextMenu() }
  else toast.error('Failed to delete connection')
}

async function submitNewFolder() {
  if (!newFolderName.value.trim()) return
  await createFolder({ name: newFolderName.value.trim(), parent_id: newFolderParent.value, visibility: newFolderVisibility.value, color: newFolderColor.value })
  newFolderName.value = ''
  newFolderColor.value = '#4f9cf9'
  newFolderVisibility.value = 'private'
  newFolderParent.value = null
  showNewFolder.value = false
}

function startEditFolder(f: ConnectionFolder) {
  editingFolder.value = f
  editFolderName.value = f.name
  editFolderColor.value = f.color
  editFolderVisibility.value = f.visibility
  closeContextMenu()
}

async function submitEditFolder() {
  if (!editingFolder.value || !editFolderName.value.trim()) return
  await updateFolder(editingFolder.value.id, { name: editFolderName.value.trim(), color: editFolderColor.value, visibility: editFolderVisibility.value, parent_id: editingFolder.value.parent_id })
  editingFolder.value = null
}

async function doDeleteFolder(f: ConnectionFolder) {
  const ok = await confirm(`Delete folder "${f.name}"? Connections inside will become unfiled.`, 'Delete Folder')
  if (!ok) return
  await deleteFolder(f.id)
  toast.success('Folder deleted')
  closeContextMenu()
}

async function toggleFolderVisibility(f: ConnectionFolder) {
  await updateFolder(f.id, { ...f, visibility: f.visibility === 'shared' ? 'private' : 'shared' })
  closeContextMenu()
}

async function toggleConnVisibility(c: Connection) {
  await setConnectionVisibility(c.id, c.visibility === 'shared' ? 'private' : 'shared')
  await fetchConnections()
  closeContextMenu()
}

async function moveConnToFolder(connId: number, folderId: number | null) {
  await moveConnection(connId, folderId)
  await fetchConnections()
  closeContextMenu()
}

function openContextMenu(e: MouseEvent, type: 'folder' | 'conn', item: ConnectionFolder | Connection) {
  e.preventDefault()
  e.stopPropagation()
  contextMenu.value = { x: e.clientX, y: e.clientY, type, item }
}

function closeContextMenu() { contextMenu.value = null }

const FOLDER_COLORS = ['#4f9cf9','#56c490','#f97f4f','#c45ef9','#f9d44f','#f9584f','#4fc8f9','#9cf94f','#f94f9c']

// ── Drag and Drop ────────────────────────────────────────────────
const draggedConn = ref<Connection | null>(null)
const draggedFolder = ref<ConnectionFolder | null>(null)
const dropTarget = ref<{ type: 'folder' | 'unfiled' | 'folder-reorder'; id: number | null; position?: 'before' | 'after' } | null>(null)

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
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  if (draggedConn.value) {
    dropTarget.value = { type: folderId === null ? 'unfiled' : 'folder', id: folderId }
  } else if (draggedFolder.value && folderId !== null && draggedFolder.value.id !== folderId) {
    const el = (e.currentTarget as HTMLElement)
    const rect = el.getBoundingClientRect()
    const position = e.clientY < rect.top + rect.height / 2 ? 'before' : 'after'
    dropTarget.value = { type: 'folder-reorder', id: folderId, position }
  }
}

function onDragLeave(e: DragEvent) {
  const related = e.relatedTarget as HTMLElement | null
  if (!related || !(e.currentTarget as HTMLElement).contains(related)) dropTarget.value = null
}

async function onFolderDrop(e: DragEvent, folderId: number | null) {
  e.preventDefault()
  const target = dropTarget.value
  dropTarget.value = null

  if (draggedConn.value) {
    const conn = draggedConn.value; draggedConn.value = null
    if (conn.folder_id === folderId) return
    await moveConnection(conn.id, folderId)
    await fetchConnections()
    toast.success(`Moved "${conn.name}"`)
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
    const otherFolders = folders.value.filter(f => f.parent_id !== dragged.parent_id)
    folders.value.splice(0, folders.value.length, ...otherFolders, ...list)
  }
}

function isDropTarget(folderId: number | null) {
  if (!dropTarget.value) return false
  if (dropTarget.value.type === 'unfiled' && folderId === null) return true
  if (dropTarget.value.type === 'folder' && dropTarget.value.id === folderId) return true
  return false
}
function isReorderTarget(folderId: number) { return dropTarget.value?.type === 'folder-reorder' && dropTarget.value.id === folderId }
function reorderPosition(folderId: number): 'before' | 'after' | null {
  if (!isReorderTarget(folderId)) return null
  return dropTarget.value?.position ?? null
}
</script>

<template>
  <aside class="connpanel" :class="{ 'connpanel--collapsed': collapsed }">

    <!-- Panel header -->
    <div class="connpanel__header">
      <span v-if="!collapsed" class="connpanel__title">Connections</span>
      <div style="display:flex;gap:4px;margin-left:auto">
        <button v-if="!collapsed" class="icon-btn" @click="router.push({ name: 'connections' })" title="New connection">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><path d="M18.36 6.64a9 9 0 1 1-12.73 0"/><line x1="12" y1="2" x2="12" y2="12"/><line x1="9" y1="9" x2="15" y2="9"/></svg>
        </button>
        <button v-if="!collapsed" class="icon-btn" @click="showNewFolder = !showNewFolder" title="New folder">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/><line x1="12" y1="11" x2="12" y2="17"/><line x1="9" y1="14" x2="15" y2="14"/></svg>
        </button>
        <button class="icon-btn" @click="collapsed = !collapsed" :title="collapsed ? 'Expand panel' : 'Collapse panel'">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path v-if="!collapsed" d="M11 19l-7-7 7-7"/>
            <path v-else d="M13 5l7 7-7 7"/>
          </svg>
        </button>
      </div>
    </div>

    <!-- Collapsed: just show icon per connection -->
    <div v-if="collapsed" class="connpanel__collapsed-list">
      <button
        v-for="conn in connections"
        :key="conn.id"
        class="connpanel__mini-btn"
        :class="{ 'connpanel__mini-btn--active': activeConnId === conn.id }"
        :title="conn.name"
        @click="selectConn(conn)"
      >
        <div class="conn-badge conn-badge--sm" :class="`conn-badge--${conn.driver}`">{{ driverLabel[conn.driver] ?? '??' }}</div>
      </button>
    </div>

    <!-- Expanded -->
    <template v-else>
      <!-- New folder form -->
      <div v-if="showNewFolder" class="new-folder-form">
        <input v-model="newFolderName" class="nf-input" placeholder="Folder name…" @keydown.enter="submitNewFolder" @keydown.esc="showNewFolder=false" autofocus />
        <div class="nf-row">
          <select v-model="newFolderVisibility" class="nf-select">
            <option value="private">🔒 Private</option>
            <option value="shared">🌐 Shared</option>
          </select>
          <select v-model="newFolderParent" class="nf-select">
            <option :value="null">No parent</option>
            <option v-for="f in folders" :key="f.id" :value="f.id">{{ f.name }}</option>
          </select>
        </div>
        <div class="nf-colors">
          <button v-for="c in FOLDER_COLORS" :key="c" class="nf-color-dot"
            :style="{ background: c, outline: newFolderColor === c ? '2px solid #fff' : 'none' }"
            @click="newFolderColor = c" />
        </div>
        <div class="nf-actions">
          <button class="nf-btn" @click="showNewFolder=false">Cancel</button>
          <button class="nf-btn nf-btn--primary" @click="submitNewFolder" :disabled="!newFolderName.trim()">Create</button>
        </div>
      </div>

      <!-- Search -->
      <div style="position:relative;padding:6px 8px">
        <svg style="position:absolute;left:16px;top:50%;transform:translateY(-50%);color:var(--text-muted);pointer-events:none" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
        <input v-model="connSearch" class="base-input" style="padding-left:28px;height:28px;font-size:12px;width:100%" placeholder="Search connections..." />
      </div>

      <!-- Scrollable tree -->
      <div class="connpanel__tree">
        <!-- Root folders -->
        <template v-for="folder in rootFolders" :key="folder.id">
          <div v-if="reorderPosition(folder.id) === 'before'" class="drop-reorder-line" />
          <div class="folder-row" :class="{ 'is-drop-target': isDropTarget(folder.id), 'is-dragging': draggedFolder?.id === folder.id }"
            draggable="true" @dragstart="onFolderDragStart($event, folder)" @dragend="onFolderDragEnd"
            @dragover="onFolderDragOver($event, folder.id)" @dragleave="onDragLeave" @drop="onFolderDrop($event, folder.id)"
            @contextmenu="openContextMenu($event, 'folder', folder)">
            <span class="drag-handle" title="Drag to reorder">⠿</span>
            <button class="folder-toggle" @click.stop="toggleFolder(folder.id)">
              <svg width="10" height="10" viewBox="0 0 24 24" fill="currentColor" style="transition:transform .15s" :style="{ transform: collapsedFolders.has(folder.id) ? 'rotate(-90deg)' : 'rotate(0deg)' }"><path d="M5 3l14 9-14 9z"/></svg>
            </button>
            <span class="folder-icon" :style="{ color: folder.color }"><svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg></span>
            <span class="folder-name">{{ folder.name }}</span>
            <span v-if="folder.visibility === 'shared'" class="folder-badge" title="Shared">🌐</span>
            <span v-else class="folder-badge" title="Private">🔒</span>
            <span class="folder-count">{{ (connsByFolder.get(folder.id) ?? []).length }}</span>
          </div>
          <div v-if="reorderPosition(folder.id) === 'after'" class="drop-reorder-line" />

          <template v-if="!collapsedFolders.has(folder.id)">
            <template v-for="child in childFolders(folder.id)" :key="child.id">
              <div class="folder-row folder-row--child" :class="{ 'is-drop-target': isDropTarget(child.id), 'is-dragging': draggedFolder?.id === child.id }"
                draggable="true" @dragstart="onFolderDragStart($event, child)" @dragend="onFolderDragEnd"
                @dragover="onFolderDragOver($event, child.id)" @dragleave="onDragLeave" @drop="onFolderDrop($event, child.id)"
                @contextmenu="openContextMenu($event, 'folder', child)">
                <span class="drag-handle">⠿</span>
                <button class="folder-toggle" @click.stop="toggleFolder(child.id)">
                  <svg width="9" height="9" viewBox="0 0 24 24" fill="currentColor" style="transition:transform .15s" :style="{ transform: collapsedFolders.has(child.id) ? 'rotate(-90deg)' : 'rotate(0deg)' }"><path d="M5 3l14 9-14 9z"/></svg>
                </button>
                <span class="folder-icon" :style="{ color: child.color }"><svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg></span>
                <span class="folder-name">{{ child.name }}</span>
                <span v-if="child.visibility === 'shared'" class="folder-badge" title="Shared">🌐</span>
                <span v-else class="folder-badge" title="Private">🔒</span>
              </div>
              <template v-if="!collapsedFolders.has(child.id)">
                <div v-for="conn in (connsByFolder.get(child.id) ?? [])" :key="conn.id"
                  class="conn-item conn-item--nested conn-item--deep"
                  :class="{ 'is-active': activeConnId === conn.id, 'is-dragging': draggedConn?.id === conn.id }"
                  draggable="true" @dragstart="onConnDragStart($event, conn)" @dragend="onConnDragEnd"
                  @click="selectConn(conn)" @contextmenu="openContextMenu($event, 'conn', conn)">
                  <span class="drag-handle">⠿</span>
                  <div class="conn-badge conn-badge--sm" :class="`conn-badge--${conn.driver}`">{{ driverLabel[conn.driver] ?? '??' }}</div>
                  <div class="conn-item__body"><div class="conn-item__name">{{ conn.name }}</div></div>
                  <span v-if="conn.visibility === 'private'" class="conn-vis-icon" title="Private">🔒</span>
                </div>
              </template>
            </template>

            <div v-for="conn in (connsByFolder.get(folder.id) ?? [])" :key="conn.id"
              class="conn-item conn-item--nested"
              :class="{ 'is-active': activeConnId === conn.id, 'is-dragging': draggedConn?.id === conn.id }"
              draggable="true" @dragstart="onConnDragStart($event, conn)" @dragend="onConnDragEnd"
              @click="selectConn(conn)" @contextmenu="openContextMenu($event, 'conn', conn)">
              <span class="drag-handle">⠿</span>
              <div class="conn-badge conn-badge--sm" :class="`conn-badge--${conn.driver}`">{{ driverLabel[conn.driver] ?? '??' }}</div>
              <div class="conn-item__body">
                <div class="conn-item__name">{{ conn.name }}</div>
                <div class="conn-item__host">{{ conn.host }}{{ conn.port ? ':' + conn.port : '' }}</div>
              </div>
              <span v-if="conn.visibility === 'private'" class="conn-vis-icon" title="Private">🔒</span>
            </div>

            <div v-if="draggedConn && (connsByFolder.get(folder.id) ?? []).length === 0"
              class="folder-drop-empty" @dragover="onFolderDragOver($event, folder.id)"
              @dragleave="onDragLeave" @drop="onFolderDrop($event, folder.id)">Drop here</div>
          </template>
        </template>

        <!-- Unfiled -->
        <div class="folder-row folder-row--unfiled" :class="{ 'is-drop-target': isDropTarget(null) }"
          @dragover="onFolderDragOver($event, null)" @dragleave="onDragLeave" @drop="onFolderDrop($event, null)">
          <span class="folder-icon" style="color:var(--text-muted)"><svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg></span>
          <span class="folder-name" style="color:var(--text-muted);font-style:italic">Unfiled</span>
          <span class="folder-count">{{ (connsByFolder.get(null) ?? []).length }}</span>
        </div>
        <div v-for="conn in (connsByFolder.get(null) ?? [])" :key="conn.id"
          class="conn-item conn-item--nested"
          :class="{ 'is-active': activeConnId === conn.id, 'is-dragging': draggedConn?.id === conn.id }"
          draggable="true" @dragstart="onConnDragStart($event, conn)" @dragend="onConnDragEnd"
          @click="selectConn(conn)" @contextmenu="openContextMenu($event, 'conn', conn)">
          <span class="drag-handle">⠿</span>
          <div class="conn-badge conn-badge--sm" :class="`conn-badge--${conn.driver}`">{{ driverLabel[conn.driver] ?? '??' }}</div>
          <div class="conn-item__body">
            <div class="conn-item__name">{{ conn.name }}</div>
            <div class="conn-item__host">{{ conn.host }}{{ conn.port ? ':' + conn.port : '' }}</div>
          </div>
          <span v-if="conn.visibility === 'private'" class="conn-vis-icon" title="Private">🔒</span>
        </div>

        <div v-if="connections.length === 0" class="empty-state" style="padding:20px 12px;font-size:12px">
          No connections yet.<br>
          <router-link :to="{ name: 'connections' }" style="color:var(--brand);font-size:11px;margin-top:6px;display:inline-block">Add your first connection →</router-link>
        </div>
      </div>
    </template>

    <!-- Edit folder modal -->
    <Teleport to="body">
      <div v-if="editingFolder" class="folder-edit-overlay" @click.self="editingFolder=null">
        <div class="folder-edit-modal">
          <div class="fem-header">Edit Folder <button class="icon-btn" @click="editingFolder=null">✕</button></div>
          <div class="fem-body">
            <label>Name</label>
            <input v-model="editFolderName" class="fem-input" />
            <label>Visibility</label>
            <select v-model="editFolderVisibility" class="fem-input">
              <option value="private">🔒 Private</option>
              <option value="shared">🌐 Shared</option>
            </select>
            <label>Color</label>
            <div class="nf-colors">
              <button v-for="c in FOLDER_COLORS" :key="c" class="nf-color-dot"
                :style="{ background: c, outline: editFolderColor === c ? '2px solid #fff' : 'none' }"
                @click="editFolderColor = c" />
            </div>
            <div class="fem-actions">
              <button class="nf-btn" @click="editingFolder=null">Cancel</button>
              <button class="nf-btn nf-btn--primary" @click="submitEditFolder">Save</button>
            </div>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- Context menu -->
    <Teleport to="body">
      <div v-if="contextMenu" class="ctx-backdrop" @click="closeContextMenu">
        <div class="ctx-menu" :style="{ top: contextMenu.y + 'px', left: contextMenu.x + 'px' }" @click.stop>
          <template v-if="contextMenu.type === 'folder'">
            <button class="ctx-item" @click="startEditFolder(contextMenu.item as ConnectionFolder)">✎ Edit folder</button>
            <button class="ctx-item" @click="toggleFolderVisibility(contextMenu.item as ConnectionFolder)">
              {{ (contextMenu.item as ConnectionFolder).visibility === 'shared' ? '🔒 Make private' : '🌐 Make shared' }}
            </button>
            <div class="ctx-divider" />
            <button class="ctx-item ctx-item--danger" @click="doDeleteFolder(contextMenu.item as ConnectionFolder)">🗑 Delete folder</button>
          </template>
          <template v-else>
            <button class="ctx-item" @click="selectConn(contextMenu.item as Connection); closeContextMenu()">▶ Open</button>
            <button class="ctx-item" @click="toggleConnVisibility(contextMenu.item as Connection)">
              {{ (contextMenu.item as Connection).visibility === 'shared' ? '🔒 Make private' : '🌐 Make shared' }}
            </button>
            <div class="ctx-divider" />
            <div class="ctx-submenu-label">Move to folder</div>
            <button class="ctx-item" @click="moveConnToFolder((contextMenu.item as Connection).id, null)">📂 Unfiled</button>
            <button v-for="f in folders" :key="f.id" class="ctx-item" @click="moveConnToFolder((contextMenu.item as Connection).id, f.id)">
              <span :style="{ color: f.color }">📁</span> {{ f.name }}
            </button>
            <div class="ctx-divider" />
            <button class="ctx-item ctx-item--danger" @click="deleteConn(contextMenu.item as Connection)">🗑 Delete</button>
          </template>
        </div>
      </div>
    </Teleport>
  </aside>
</template>

<style scoped>
.connpanel {
  width: 220px;
  flex-shrink: 0;
  height: 100%;
  background: var(--bg-surface);
  border-right: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  transition: width 0.2s ease;
}
.connpanel--collapsed {
  width: 44px;
}

.connpanel__header {
  display: flex;
  align-items: center;
  padding: 8px 10px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
  min-height: 38px;
}
.connpanel__title {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.6px;
  color: var(--text-muted);
}

.connpanel__collapsed-list {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  padding: 8px 4px;
  overflow-y: auto;
}
.connpanel__mini-btn {
  background: transparent;
  border: 1px solid transparent;
  border-radius: 6px;
  padding: 4px;
  cursor: pointer;
  transition: background 0.12s, border-color 0.12s;
}
.connpanel__mini-btn:hover { background: var(--bg-hover); }
.connpanel__mini-btn--active { border-color: var(--brand); background: var(--brand-dim); }

.connpanel__tree {
  flex: 1;
  overflow-y: auto;
  padding: 4px 0;
}
</style>
