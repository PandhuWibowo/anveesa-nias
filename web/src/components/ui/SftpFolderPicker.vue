<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import axios from 'axios'

interface Entry { name: string; isDir: boolean }

const props = defineProps<{
  show: boolean
  hostId: number | null
  startPath: string
  /** The directory the item(s) currently live in — picking it is a no-op. */
  sourceDir: string
  /** Full paths that can't be entered (folders being moved — can't move a folder into itself/its own subtree). */
  excludePaths?: string[]
  title?: string
}>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'pick', path: string): void }>()

const path = ref('/')
const dirs = ref<Entry[]>([])
const loading = ref(false)
const error = ref('')

function joinPath(base: string, name: string) {
  return base === '/' ? '/' + name : base.replace(/\/$/, '') + '/' + name
}
function parentPath(p: string) {
  if (p === '/' || !p.includes('/')) return '/'
  const i = p.replace(/\/$/, '').lastIndexOf('/')
  return i <= 0 ? '/' : p.slice(0, i)
}
const crumbs = computed(() => {
  const segs = path.value.split('/').filter(Boolean)
  let acc = ''
  return segs.map((s) => { acc += '/' + s; return { label: s, path: acc } })
})
const excluded = computed(() => new Set(props.excludePaths || []))
const isNoOp = computed(() => path.value === props.sourceDir)

async function load(p: string) {
  if (props.hostId === null) return
  loading.value = true
  error.value = ''
  try {
    const { data } = await axios.get<{ path: string; entries: Entry[] }>(
      `/api/sftp/hosts/${props.hostId}/list`,
      { params: { path: p } },
    )
    path.value = data.path || '/'
    dirs.value = (data.entries || []).filter((e) => e.isDir)
  } catch (e: any) {
    error.value = e?.response?.data?.error || 'Could not list directory'
    dirs.value = []
  } finally {
    loading.value = false
  }
}

function enter(name: string) {
  const full = joinPath(path.value, name)
  if (excluded.value.has(full)) return
  load(full)
}
function up() { load(parentPath(path.value)) }
function pick() {
  if (isNoOp.value) return
  emit('pick', path.value)
}

watch(() => props.show, (v) => { if (v) load(props.startPath || '/') })
</script>

<template>
  <div v-if="show" class="sfp-backdrop" @click.self="emit('close')">
    <div class="sfp-modal page-card">
      <div class="sfp-title">{{ title || 'Move to…' }}</div>

      <div class="sfp-bar">
        <button class="base-btn base-btn--xs" :disabled="path === '/'" @click="up">↑ Up</button>
        <nav class="sfp-crumbs">
          <button class="sfp-crumb" @click="load('/')">/</button>
          <template v-for="(c, i) in crumbs" :key="c.path">
            <span class="sfp-sep">/</span>
            <button class="sfp-crumb" :class="{ 'sfp-crumb--last': i === crumbs.length - 1 }" @click="load(c.path)">{{ c.label }}</button>
          </template>
        </nav>
      </div>

      <div class="sfp-list">
        <div v-if="loading" class="sfp-msg">Loading…</div>
        <div v-else-if="error" class="sfp-msg sfp-err">{{ error }}</div>
        <div v-else-if="!dirs.length" class="sfp-msg">No subfolders here.</div>
        <button
          v-for="d in dirs"
          :key="d.name"
          type="button"
          class="sfp-item"
          :disabled="excluded.has(joinPath(path, d.name))"
          :title="excluded.has(joinPath(path, d.name)) ? 'Can\'t move a folder into itself' : ''"
          @click="enter(d.name)"
        >📁 {{ d.name }}</button>
      </div>

      <div class="sfp-footer">
        <span class="sfp-current">Destination: <b>{{ path }}</b><template v-if="isNoOp"> (already here)</template></span>
        <div class="sfp-actions">
          <button class="base-btn base-btn--sm" @click="emit('close')">Cancel</button>
          <button class="base-btn base-btn--primary base-btn--sm" :disabled="isNoOp" @click="pick">Move here</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.sfp-backdrop { position: fixed; inset: 0; background: rgba(0, 0, 0, 0.5); display: flex; align-items: center; justify-content: center; z-index: 100; }
.sfp-modal { width: 480px; max-width: 92vw; max-height: 78vh; display: flex; flex-direction: column; padding: 18px; }
.sfp-title { font-size: 15px; font-weight: 600; color: var(--text-primary); margin-bottom: 12px; }
.sfp-bar { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; flex-wrap: wrap; }
.sfp-crumbs { display: flex; align-items: center; gap: 2px; flex-wrap: wrap; font-size: 12.5px; min-width: 0; }
.sfp-crumb { background: none; border: none; color: var(--brand); cursor: pointer; padding: 2px 4px; border-radius: var(--r-xs); font-family: var(--mono); font-size: 12px; }
.sfp-crumb:hover { background: var(--bg-hover); }
.sfp-crumb--last { color: var(--text-primary); font-weight: 600; }
.sfp-sep { color: var(--text-muted); }
.sfp-list { flex: 1; overflow-y: auto; min-height: 200px; max-height: 45vh; border: 1px solid var(--border); border-radius: var(--r-sm); padding: 4px; display: flex; flex-direction: column; gap: 2px; }
.sfp-item { display: flex; align-items: center; gap: 8px; width: 100%; padding: 8px 10px; border: none; background: transparent; border-radius: var(--r-sm); color: var(--text-primary); font-size: 13px; text-align: left; cursor: pointer; }
.sfp-item:hover:not(:disabled) { background: var(--bg-hover); }
.sfp-item:disabled { opacity: 0.4; cursor: default; }
.sfp-msg { text-align: center; color: var(--text-muted); padding: 24px; font-size: 13px; }
.sfp-err { color: var(--danger); }
.sfp-footer { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 12px; flex-wrap: wrap; }
.sfp-current { font-size: 12px; color: var(--text-muted); word-break: break-all; }
.sfp-actions { display: flex; gap: 8px; flex-shrink: 0; }
</style>
