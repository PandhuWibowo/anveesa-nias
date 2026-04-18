<script setup lang="ts">
import { ref } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import { useDatabases } from '@/composables/useDatabases'

const { connections } = useConnections()
const { databases, fetchDatabases } = useDatabases()

const connId = ref<number | null>(null)
const database = ref('')
const restoreSQL = ref('')
const restoreResult = ref('')
const restoreLoading = ref(false)
const restoreError = ref('')
const activeTab = ref<'backup' | 'restore'>('backup')

async function onConnChange() {
  database.value = ''
  if (connId.value) {
    await fetchDatabases(connId.value)
    database.value = databases.value[0] ?? ''
  }
}

function downloadBackup() {
  if (!connId.value) return
  const url = `/api/connections/${connId.value}/backup?database=${encodeURIComponent(database.value)}`
  const a = document.createElement('a')
  a.href = url; a.download = `backup_${database.value || 'db'}.sql`
  document.body.appendChild(a); a.click(); document.body.removeChild(a)
}

async function uploadFile(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  restoreSQL.value = await file.text()
}

async function runRestore() {
  if (!connId.value || !restoreSQL.value) return
  restoreLoading.value = true; restoreError.value = ''; restoreResult.value = ''
  try {
    const { data } = await axios.post(`/api/connections/${connId.value}/restore`, { sql: restoreSQL.value })
    restoreResult.value = `Executed ${data.executed} statement(s) successfully.`
  } catch (e: any) {
    restoreError.value = e?.response?.data?.error ?? 'Restore failed'
  } finally {
    restoreLoading.value = false
  }
}
</script>

<template>
  <div class="page-shell bv-root">
    <div class="page-scroll bv-scroll">
      <div class="page-stack">
      <section class="page-hero">
        <div class="page-hero__content">
          <div class="page-kicker">Operations</div>
          <div class="page-title">Backup &amp; Restore</div>
          <div class="page-subtitle">Export a SQL snapshot or restore from a reviewed script when you need to recover or clone data quickly.</div>
        </div>
      </section>

      <div class="page-tabs bv-tabs">
        <button class="page-tab bv-tab" :class="{ 'is-active': activeTab === 'backup' }" @click="activeTab='backup'">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
          Backup
        </button>
        <button class="page-tab bv-tab" :class="{ 'is-active': activeTab === 'restore' }" @click="activeTab='restore'">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
          Restore
        </button>
      </div>

      <!-- Backup -->
      <div v-if="activeTab === 'backup'" class="page-card bv-card">
        <div class="page-card__head">
          <div>
            <div class="page-card__title">Download SQL Dump</div>
            <div class="page-card__sub">Generate a portable SQL export for the selected target.</div>
          </div>
        </div>
        <div class="page-card__body bv-card-body">
          <div class="form-group">
            <label class="form-label">Connection</label>
            <select class="base-input" v-model.number="connId" @change="onConnChange">
              <option :value="null">— select —</option>
              <option v-for="c in connections" :key="c.id" :value="c.id">{{ c.name }}</option>
            </select>
          </div>
          <div v-if="databases.length > 1" class="form-group">
            <label class="form-label">Database</label>
            <select class="base-input" v-model="database">
              <option v-for="db in databases" :key="db" :value="db">{{ db }}</option>
            </select>
          </div>
          <div class="bv-info">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
            Generates INSERT statements for all rows in every table. DDL (CREATE TABLE) included for SQLite.
          </div>
          <button
            class="base-btn base-btn--primary"
            :disabled="!connId"
            @click="downloadBackup"
          >
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
            Download .sql
          </button>
        </div>
      </div>

      <!-- Restore -->
      <div v-if="activeTab === 'restore'" class="page-card bv-card">
        <div class="page-card__head">
          <div>
            <div class="page-card__title">Restore from SQL File</div>
            <div class="page-card__sub">Upload or paste a script and apply it to the chosen connection.</div>
          </div>
        </div>
        <div class="page-card__body bv-card-body">
          <div class="form-group">
            <label class="form-label">Connection</label>
            <select class="base-input" v-model.number="connId" @change="onConnChange">
              <option :value="null">— select —</option>
              <option v-for="c in connections" :key="c.id" :value="c.id">{{ c.name }}</option>
            </select>
          </div>

          <!-- Drop zone -->
          <div class="bv-drop" @dragover.prevent @drop.prevent="(e) => { const f=e.dataTransfer?.files?.[0]; if(f) f.text().then(t=>restoreSQL=t) }">
            <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
            <span>Drop a .sql file here or</span>
            <label class="bv-file-btn">
              Browse
              <input type="file" accept=".sql,.txt" style="display:none" @change="uploadFile" />
            </label>
          </div>

          <div v-if="restoreSQL" class="bv-preview">
            <div class="bv-preview-header">
              <span>{{ restoreSQL.split('\n').length }} lines loaded</span>
              <button class="base-btn base-btn--ghost base-btn--sm" @click="restoreSQL=''">Clear</button>
            </div>
            <pre class="bv-preview-pre">{{ restoreSQL.slice(0, 500) }}{{ restoreSQL.length > 500 ? '\n…' : '' }}</pre>
          </div>

          <div v-if="restoreResult" class="notice notice--ok">{{ restoreResult }}</div>
          <div v-if="restoreError" class="notice notice--error">{{ restoreError }}</div>

          <button
            class="base-btn base-btn--primary"
            :disabled="!connId || !restoreSQL || restoreLoading"
            @click="runRestore"
          >
            <svg v-if="restoreLoading" class="spin" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
            {{ restoreLoading ? 'Restoring…' : 'Run Restore' }}
          </button>
        </div>
      </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.bv-root { background: var(--bg-body); }
.bv-card-body { display:flex; flex-direction:column; gap:14px; }
.bv-info { display:flex; align-items:flex-start; gap:8px; font-size:12px; color:var(--text-muted); padding:10px; background:var(--bg-body); border-radius:6px; border:1px solid var(--border); }
.bv-drop {
  border:2px dashed var(--border); border-radius:8px; padding:28px;
  display:flex; align-items:center; justify-content:center; gap:10px;
  font-size:13px; color:var(--text-muted); cursor:pointer;
  transition:border-color 0.15s;
}
.bv-drop:hover { border-color:var(--brand); }
.bv-file-btn {
  padding:4px 12px; background:var(--bg-elevated); border:1px solid var(--border);
  border-radius:6px; font-size:12px; color:var(--text-secondary); cursor:pointer;
  transition:all 0.12s;
}
.bv-file-btn:hover { background:var(--bg-hover); }
.bv-preview { background:var(--bg-body); border:1px solid var(--border); border-radius:6px; overflow:hidden; }
.bv-preview-header { display:flex; justify-content:space-between; align-items:center; padding:8px 12px; border-bottom:1px solid var(--border); font-size:12px; color:var(--text-muted); }
.bv-preview-pre { margin:0; padding:10px 12px; font-family:var(--mono,monospace); font-size:11.5px; line-height:1.5; color:var(--text-primary); white-space:pre-wrap; word-break:break-all; }
.notice--ok { background:rgba(74,222,128,0.1); border:1px solid rgba(74,222,128,0.3); border-radius:10px; padding:10px 14px; font-size:12.5px; color:#4ade80; }
</style>
