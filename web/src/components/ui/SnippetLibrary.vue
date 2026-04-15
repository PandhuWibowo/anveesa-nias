<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'
import axios from 'axios'

interface Snippet { id: number; name: string; description: string; sql: string; tags: string }

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ close: []; insert: [sql: string] }>()

const snippets = ref<Snippet[]>([])
const q = ref('')
const view = ref<'list' | 'edit'>('list')
const editing = ref<Partial<Snippet>>({})
const saving = ref(false)

async function load() {
  const { data } = await axios.get<Snippet[]>('/api/snippets', { params: { q: q.value || undefined } })
  snippets.value = data
}

watch(() => props.show, (v) => { if (v) { q.value = ''; view.value = 'list'; load() } })
watch(q, load)

function startCreate() {
  editing.value = { name: '', description: '', sql: '', tags: '' }
  view.value = 'edit'
}

function startEdit(s: Snippet) {
  editing.value = { ...s }
  view.value = 'edit'
}

async function save() {
  if (!editing.value.name?.trim()) return
  saving.value = true
  try {
    if (editing.value.id) {
      await axios.put(`/api/snippets/${editing.value.id}`, editing.value)
    } else {
      await axios.post('/api/snippets', editing.value)
    }
    view.value = 'list'; load()
  } finally { saving.value = false }
}

async function del(s: Snippet) {
  if (!confirm(`Delete "${s.name}"?`)) return
  await axios.delete(`/api/snippets/${s.id}`)
  load()
}

const filteredSnippets = ref<Snippet[]>([])
watch([snippets, q], () => {
  const term = q.value.toLowerCase()
  filteredSnippets.value = term
    ? snippets.value.filter((s) => s.name.toLowerCase().includes(term) || s.tags.toLowerCase().includes(term) || s.sql.toLowerCase().includes(term))
    : snippets.value
}, { immediate: true })
</script>

<template>
  <Teleport to="body">
    <div v-if="show" class="sl-overlay" @click.self="emit('close')">
      <div class="sl-modal">
        <!-- Header -->
        <div class="sl-header">
          <span class="sl-title">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
            Snippet Library
          </span>
          <button v-if="view === 'list'" class="base-btn base-btn--primary base-btn--sm" @click="startCreate">+ New</button>
          <button v-else class="base-btn base-btn--ghost base-btn--sm" @click="view='list'">← Back</button>
          <div style="flex:1"/>
          <button class="sl-close" @click="emit('close')">×</button>
        </div>

        <!-- List view -->
        <template v-if="view === 'list'">
          <div class="sl-search-wrap">
            <input v-model="q" class="sl-search" placeholder="Search snippets…" />
          </div>
          <div class="sl-list">
            <div v-if="filteredSnippets.length === 0" class="sl-empty">
              {{ q ? 'No snippets match your search.' : 'No snippets yet. Create one!' }}
            </div>
            <div v-for="s in filteredSnippets" :key="s.id" class="sl-item">
              <div class="sl-item-head">
                <span class="sl-item-name">{{ s.name }}</span>
                <span v-if="s.tags" class="sl-tags">
                  <span v-for="t in s.tags.split(',')" :key="t" class="sl-tag">{{ t.trim() }}</span>
                </span>
                <div style="flex:1"/>
                <button class="base-btn base-btn--primary base-btn--sm" @click="emit('insert', s.sql); emit('close')">Insert</button>
                <button class="base-btn base-btn--ghost base-btn--sm" @click="startEdit(s)">Edit</button>
                <button class="base-btn base-btn--ghost base-btn--sm" style="color:var(--danger)" @click="del(s)">×</button>
              </div>
              <div v-if="s.description" class="sl-desc">{{ s.description }}</div>
              <pre class="sl-sql">{{ s.sql }}</pre>
            </div>
          </div>
        </template>

        <!-- Edit view -->
        <template v-else>
          <div class="sl-edit-body">
            <div class="form-group">
              <label class="form-label">Name</label>
              <input v-model="editing.name" class="base-input" placeholder="Count active users" />
            </div>
            <div class="form-group">
              <label class="form-label">Description <span class="form-hint" style="display:inline">(optional)</span></label>
              <input v-model="editing.description" class="base-input" placeholder="Returns count of users where active=1" />
            </div>
            <div class="form-group">
              <label class="form-label">SQL</label>
              <textarea v-model="editing.sql" class="base-input" rows="6" placeholder="SELECT COUNT(*) FROM users WHERE active = 1" style="font-family:monospace;font-size:12px;resize:vertical" />
            </div>
            <div class="form-group">
              <label class="form-label">Tags <span class="form-hint" style="display:inline">(comma separated)</span></label>
              <input v-model="editing.tags" class="base-input" placeholder="users, reporting, admin" />
            </div>
          </div>
          <div class="sl-edit-footer">
            <button class="base-btn base-btn--ghost" @click="view='list'">Cancel</button>
            <button class="base-btn base-btn--primary" :disabled="!editing.name?.trim() || saving" @click="save">
              {{ saving ? 'Saving…' : editing.id ? 'Save' : 'Create' }}
            </button>
          </div>
        </template>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.sl-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.55); display: flex; align-items: center; justify-content: center; z-index: 1500; }
.sl-modal { background: var(--bg-elevated); border: 1px solid var(--border); border-radius: 12px; width: min(640px,94vw); max-height: 82vh; display: flex; flex-direction: column; box-shadow: 0 24px 64px rgba(0,0,0,0.55); }
.sl-header { display: flex; align-items: center; gap: 8px; padding: 12px 16px; border-bottom: 1px solid var(--border); }
.sl-title { display: flex; align-items: center; gap: 6px; font-size: 13px; font-weight: 700; color: var(--text-primary); }
.sl-close { background: transparent; border: none; font-size: 20px; color: var(--text-muted); cursor: pointer; padding: 0 4px; line-height: 1; }
.sl-search-wrap { padding: 10px 16px; border-bottom: 1px solid var(--border); }
.sl-search { width: 100%; padding: 6px 12px; background: var(--bg-body); border: 1px solid var(--border); border-radius: 6px; color: var(--text-primary); font-size: 13px; font-family: inherit; outline: none; }
.sl-search:focus { border-color: var(--brand); }
.sl-list { flex: 1; min-height: 0; overflow-y: auto; display: flex; flex-direction: column; gap: 8px; padding: 12px 16px; }
.sl-empty { text-align: center; color: var(--text-muted); font-size: 13px; padding: 24px; }
.sl-item { background: var(--bg-body); border: 1px solid var(--border); border-radius: 8px; overflow: hidden; }
.sl-item-head { display: flex; align-items: center; gap: 8px; padding: 10px 12px; flex-wrap: wrap; }
.sl-item-name { font-weight: 700; font-size: 13px; color: var(--text-primary); }
.sl-tags { display: flex; gap: 4px; flex-wrap: wrap; }
.sl-tag { font-size: 10px; padding: 1px 6px; border-radius: 4px; background: var(--bg-elevated); border: 1px solid var(--border); color: var(--text-muted); }
.sl-desc { padding: 0 12px 6px; font-size: 12px; color: var(--text-muted); }
.sl-sql { margin: 0; padding: 8px 12px; background: var(--bg-elevated); border-top: 1px solid var(--border); font-family: var(--mono,monospace); font-size: 11.5px; color: var(--text-secondary); white-space: pre-wrap; word-break: break-all; max-height: 100px; overflow: auto; }
.sl-edit-body { flex: 1; min-height: 0; overflow-y: auto; padding: 16px; display: flex; flex-direction: column; gap: 12px; }
.sl-edit-footer { display: flex; justify-content: flex-end; gap: 8px; padding: 12px 16px; border-top: 1px solid var(--border); }
</style>
