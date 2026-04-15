<script setup lang="ts">
import { ref, computed } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import { useDatabases } from '@/composables/useDatabases'

const { connections } = useConnections()
const { databases: dbsA, fetchDatabases: fetchA } = useDatabases()
const { databases: dbsB, fetchDatabases: fetchB } = useDatabases()

const connA = ref<number | null>(null)
const connB = ref<number | null>(null)
const dbA = ref('')
const dbB = ref('')
const loading = ref(false)
const error = ref('')
const result = ref<any>(null)

async function onConnAChange() {
  dbA.value = ''
  if (connA.value) {
    await fetchA(connA.value)
    dbA.value = dbsA.value[0] ?? ''
  }
}

async function onConnBChange() {
  dbB.value = ''
  if (connB.value) {
    await fetchB(connB.value)
    dbB.value = dbsB.value[0] ?? ''
  }
}

async function runDiff() {
  if (!connA.value || !connB.value) return
  loading.value = true
  error.value = ''
  result.value = null
  try {
    const { data } = await axios.get('/api/diff', {
      params: { conn_a: connA.value, conn_b: connB.value, db_a: dbA.value, db_b: dbB.value },
    })
    result.value = data
  } catch (e: any) {
    error.value = e?.response?.data?.error ?? 'Diff failed'
  } finally {
    loading.value = false
  }
}

const statusColors: Record<string, string> = {
  added: '#4ade80',
  removed: '#f87171',
  changed: '#fbbf24',
  same: 'var(--text-muted)',
}
const statusIcons: Record<string, string> = {
  added: '+',
  removed: '-',
  changed: '~',
  same: '=',
}

const summaryAdded = computed(() => result.value?.diffs?.filter((d: any) => d.status === 'added').length ?? 0)
const summaryRemoved = computed(() => result.value?.diffs?.filter((d: any) => d.status === 'removed').length ?? 0)
const summaryChanged = computed(() => result.value?.diffs?.filter((d: any) => d.status === 'changed').length ?? 0)
const summarySame = computed(() => result.value?.diffs?.filter((d: any) => d.status === 'same').length ?? 0)

const showSame = ref(false)
const visibleDiffs = computed(() => {
  if (!result.value?.diffs) return []
  return showSame.value ? result.value.diffs : result.value.diffs.filter((d: any) => d.status !== 'same')
})
</script>

<template>
  <div class="sd-root">
    <div class="sd-scroll">
      <!-- Header -->
      <div class="sd-title">Schema Diff</div>
      <div class="sd-sub">Compare table structures across two connections or databases.</div>

      <!-- Pickers -->
      <div class="sd-pickers">
        <div class="sd-picker">
          <div class="sd-picker-label">Connection A</div>
          <select class="sd-select" v-model="connA" @change="onConnAChange">
            <option :value="null">— select —</option>
            <option v-for="c in connections" :key="c.id" :value="c.id">{{ c.name }}</option>
          </select>
          <select v-if="dbsA.length > 1" class="sd-select" v-model="dbA">
            <option v-for="db in dbsA" :key="db" :value="db">{{ db }}</option>
          </select>
        </div>

        <div class="sd-swap">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="17 1 21 5 17 9"/><path d="M3 11V9a4 4 0 0 1 4-4h14"/><polyline points="7 23 3 19 7 15"/><path d="M21 13v2a4 4 0 0 1-4 4H3"/></svg>
        </div>

        <div class="sd-picker">
          <div class="sd-picker-label">Connection B</div>
          <select class="sd-select" v-model="connB" @change="onConnBChange">
            <option :value="null">— select —</option>
            <option v-for="c in connections" :key="c.id" :value="c.id">{{ c.name }}</option>
          </select>
          <select v-if="dbsB.length > 1" class="sd-select" v-model="dbB">
            <option v-for="db in dbsB" :key="db" :value="db">{{ db }}</option>
          </select>
        </div>

        <button
          class="base-btn base-btn--primary base-btn--sm"
          :disabled="!connA || !connB || loading"
          @click="runDiff"
        >
          <svg v-if="loading" class="spin" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
          {{ loading ? 'Comparing…' : 'Compare' }}
        </button>
      </div>

      <div v-if="error" class="notice notice--error">{{ error }}</div>

      <!-- Summary -->
      <div v-if="result" class="sd-summary">
        <div class="sd-sum-item" style="color:#4ade80"><strong>{{ summaryAdded }}</strong> added</div>
        <div class="sd-sum-item" style="color:#f87171"><strong>{{ summaryRemoved }}</strong> removed</div>
        <div class="sd-sum-item" style="color:#fbbf24"><strong>{{ summaryChanged }}</strong> changed</div>
        <div class="sd-sum-item" style="color:var(--text-muted)"><strong>{{ summarySame }}</strong> identical</div>
        <div style="flex:1"/>
        <label class="sd-toggle">
          <input type="checkbox" v-model="showSame" />
          Show identical
        </label>
      </div>

      <!-- Diff list -->
      <div v-if="result" class="sd-list">
        <div v-if="visibleDiffs.length === 0" class="sd-empty">
          {{ showSame ? 'No tables found.' : 'All tables are identical. Enable "Show identical" to see them.' }}
        </div>
        <div
          v-for="d in visibleDiffs"
          :key="d.name"
          class="sd-item"
          :class="`sd-item--${d.status}`"
        >
          <div class="sd-item-head">
            <span class="sd-icon" :style="{ color: statusColors[d.status] }">{{ statusIcons[d.status] }}</span>
            <span class="sd-item-name">{{ d.name }}</span>
            <span class="sd-item-status" :style="{ color: statusColors[d.status] }">{{ d.status }}</span>
          </div>

          <div v-if="d.changes?.length" class="sd-changes">
            <div v-for="(c, i) in d.changes" :key="i" class="sd-change">{{ c }}</div>
          </div>

          <!-- Columns side-by-side for changed/added/removed -->
          <div v-if="d.status !== 'same' && (d.cols_a?.length || d.cols_b?.length)" class="sd-cols">
            <div class="sd-cols-side">
              <div class="sd-cols-label">A {{ result.db_a ? '· ' + result.db_a : '' }}</div>
              <div v-for="col in (d.cols_a ?? [])" :key="col.name" class="sd-col">
                <span class="sd-col-name">{{ col.name }}</span>
                <span class="sd-col-type">{{ col.data_type }}</span>
              </div>
              <div v-if="!d.cols_a?.length" class="sd-col-empty">—</div>
            </div>
            <div class="sd-cols-side">
              <div class="sd-cols-label">B {{ result.db_b ? '· ' + result.db_b : '' }}</div>
              <div v-for="col in (d.cols_b ?? [])" :key="col.name" class="sd-col">
                <span class="sd-col-name">{{ col.name }}</span>
                <span class="sd-col-type">{{ col.data_type }}</span>
              </div>
              <div v-if="!d.cols_b?.length" class="sd-col-empty">—</div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.sd-root { width: 100%; height: 100%; display: flex; flex-direction: column; overflow: hidden; }
.sd-scroll { flex: 1; min-height: 0; overflow-y: auto; padding: 24px 28px 40px; display: flex; flex-direction: column; gap: 16px; }
.sd-title { font-size: 20px; font-weight: 700; color: var(--text-primary); }
.sd-sub { font-size: 13px; color: var(--text-muted); margin-top: -10px; }
.sd-pickers { display: flex; align-items: flex-end; gap: 12px; flex-wrap: wrap; }
.sd-picker { display: flex; flex-direction: column; gap: 6px; }
.sd-picker-label { font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.4px; color: var(--text-muted); }
.sd-select {
  padding: 6px 10px; background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 6px; color: var(--text-primary); font-size: 13px; min-width: 180px;
  outline: none; cursor: pointer;
}
.sd-swap { display: flex; align-items: center; color: var(--text-muted); padding-bottom: 4px; }
.sd-summary {
  display: flex; align-items: center; gap: 20px;
  padding: 12px 16px; background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 8px; font-size: 13px;
}
.sd-sum-item { display: flex; gap: 4px; }
.sd-toggle { display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--text-muted); cursor: pointer; }
.sd-list { display: flex; flex-direction: column; gap: 8px; }
.sd-empty { font-size: 13px; color: var(--text-muted); text-align: center; padding: 24px; }
.sd-item {
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 8px; overflow: hidden;
}
.sd-item--added { border-left: 3px solid #4ade80; }
.sd-item--removed { border-left: 3px solid #f87171; }
.sd-item--changed { border-left: 3px solid #fbbf24; }
.sd-item--same { border-left: 3px solid var(--border); opacity: 0.6; }
.sd-item-head {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 14px;
}
.sd-icon { font-weight: 900; font-size: 14px; width: 18px; text-align: center; }
.sd-item-name { font-weight: 600; font-size: 13px; color: var(--text-primary); font-family: var(--mono, monospace); flex: 1; }
.sd-item-status { font-size: 10.5px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.4px; }
.sd-changes { padding: 0 14px 8px 42px; display: flex; flex-direction: column; gap: 3px; }
.sd-change { font-size: 12px; color: var(--text-secondary); font-family: var(--mono, monospace); }
.sd-cols { display: grid; grid-template-columns: 1fr 1fr; gap: 1px; background: var(--border); border-top: 1px solid var(--border); }
.sd-cols-side { background: var(--bg-body); padding: 10px 14px; display: flex; flex-direction: column; gap: 4px; }
.sd-cols-label { font-size: 10px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.5px; color: var(--text-muted); margin-bottom: 4px; }
.sd-col { display: flex; gap: 8px; font-size: 12px; }
.sd-col-name { color: var(--text-primary); font-family: var(--mono, monospace); }
.sd-col-type { color: var(--text-muted); font-size: 11px; }
.sd-col-empty { color: var(--text-muted); font-style: italic; font-size: 12px; }
</style>
