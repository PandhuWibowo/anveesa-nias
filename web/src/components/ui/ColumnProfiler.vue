<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import axios from 'axios'

interface TopValue { value: string; count: number }
interface ProfileResult {
  table: string; column: string
  total: number; non_null: number; null_count: number; null_pct: number
  distinct: number; min: string; max: string; avg: string
  top_values: TopValue[]
  histogram: number[] | null
}

const props = defineProps<{
  show: boolean
  connId: number | null
  table: string
  column: string
  database?: string
}>()
const emit = defineEmits<{ close: [] }>()

const result = ref<ProfileResult | null>(null)
const loading = ref(false)
const error = ref('')

watch(() => props.show, (v) => {
  if (v && props.connId && props.table && props.column) load()
})

async function load() {
  if (!props.connId) return
  loading.value = true; error.value = ''; result.value = null
  try {
    const { data } = await axios.post<ProfileResult>(`/api/connections/${props.connId}/profile`, {
      table: props.table, column: props.column, database: props.database,
    })
    result.value = data
  } catch (e: any) {
    error.value = e?.response?.data?.error ?? 'Profiling failed'
  } finally {
    loading.value = false
  }
}

const maxTopCount = computed(() =>
  Math.max(1, ...(result.value?.top_values?.map((t) => t.count) ?? [1])),
)
const maxHistBucket = computed(() =>
  Math.max(1, ...(result.value?.histogram ?? [1])),
)
const nullFill = computed(() => result.value ? (result.value.null_pct / 100) * 100 : 0)
</script>

<template>
  <Teleport to="body">
    <div v-if="show" class="cp-overlay" @click.self="emit('close')">
      <div class="cp-modal">
        <div class="cp-header">
          <div class="cp-title">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="20" x2="18" y2="10"/><line x1="12" y1="20" x2="12" y2="4"/><line x1="6" y1="20" x2="6" y2="14"/></svg>
            Column Profile
          </div>
          <div class="cp-subtitle">{{ table }}<span class="cp-sep">.</span><strong>{{ column }}</strong></div>
          <div style="flex:1"/>
          <button class="cp-close" @click="emit('close')">×</button>
        </div>

        <div class="cp-body">
          <!-- Loading -->
          <div v-if="loading" class="cp-center">
            <svg class="spin" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
          </div>
          <div v-else-if="error" class="notice notice--error" style="margin:20px">{{ error }}</div>
          <template v-else-if="result">
            <!-- Stats grid -->
            <div class="cp-stats">
              <div class="cp-stat">
                <div class="cp-stat-val">{{ result.total.toLocaleString() }}</div>
                <div class="cp-stat-lbl">Total Rows</div>
              </div>
              <div class="cp-stat">
                <div class="cp-stat-val">{{ result.distinct.toLocaleString() }}</div>
                <div class="cp-stat-lbl">Distinct</div>
              </div>
              <div class="cp-stat">
                <div class="cp-stat-val" :style="{ color: result.null_count > 0 ? '#f87171' : 'inherit' }">
                  {{ result.null_count.toLocaleString() }}
                </div>
                <div class="cp-stat-lbl">Null ({{ result.null_pct.toFixed(1) }}%)</div>
              </div>
              <div class="cp-stat" v-if="result.avg">
                <div class="cp-stat-val">{{ result.avg ? Number(result.avg).toFixed(2) : '—' }}</div>
                <div class="cp-stat-lbl">Average</div>
              </div>
            </div>

            <!-- Null fill bar -->
            <div class="cp-section-label">Null density</div>
            <div class="cp-fill-bar">
              <div class="cp-fill-inner" :style="{ width: nullFill + '%' }" />
              <span class="cp-fill-pct">{{ result.null_pct.toFixed(1) }}% null</span>
            </div>

            <!-- Min / Max -->
            <div v-if="result.min || result.max" class="cp-minmax">
              <div class="cp-minmax-item"><span class="cp-mm-lbl">Min</span><span class="cp-mm-val">{{ result.min || '—' }}</span></div>
              <div class="cp-minmax-item"><span class="cp-mm-lbl">Max</span><span class="cp-mm-val">{{ result.max || '—' }}</span></div>
            </div>

            <!-- Histogram -->
            <template v-if="result.histogram?.length">
              <div class="cp-section-label">Distribution ({{ result.histogram.length }} buckets)</div>
              <div class="cp-histogram">
                <div
                  v-for="(cnt, i) in result.histogram"
                  :key="i"
                  class="cp-hist-bar"
                  :style="{ height: Math.max(4, (cnt / maxHistBucket) * 80) + 'px' }"
                  :title="cnt.toLocaleString()"
                />
              </div>
              <div class="cp-hist-labels">
                <span>{{ result.min }}</span><span>{{ result.max }}</span>
              </div>
            </template>

            <!-- Top values -->
            <template v-if="result.top_values?.length">
              <div class="cp-section-label">Top values</div>
              <div class="cp-top-list">
                <div v-for="(tv, i) in result.top_values" :key="i" class="cp-top-row">
                  <span class="cp-top-rank">{{ i + 1 }}</span>
                  <span class="cp-top-val">{{ tv.value === '' ? '(empty)' : tv.value ?? 'NULL' }}</span>
                  <div class="cp-top-bar-wrap">
                    <div class="cp-top-bar" :style="{ width: (tv.count / maxTopCount * 100) + '%' }" />
                  </div>
                  <span class="cp-top-cnt">{{ tv.count.toLocaleString() }}</span>
                </div>
              </div>
            </template>
          </template>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.cp-overlay {
  position: fixed; inset: 0; background: rgba(0,0,0,0.55);
  display: flex; align-items: center; justify-content: center; z-index: 1200;
}
.cp-modal {
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 10px; width: min(520px, 94vw); max-height: 85vh;
  display: flex; flex-direction: column;
  box-shadow: 0 24px 64px rgba(0,0,0,0.55);
}
.cp-header {
  display: flex; align-items: center; gap: 10px;
  padding: 12px 16px; border-bottom: 1px solid var(--border);
}
.cp-title {
  display: flex; align-items: center; gap: 6px;
  font-size: 13px; font-weight: 700; color: var(--text-primary);
}
.cp-subtitle { font-size: 12px; color: var(--text-muted); font-family: var(--mono, monospace); }
.cp-sep { color: var(--text-muted); margin: 0 2px; }
.cp-close {
  background: transparent; border: none; font-size: 20px;
  color: var(--text-muted); cursor: pointer; padding: 0 4px; line-height: 1;
}
.cp-body { flex: 1; min-height: 0; overflow-y: auto; padding: 16px; display: flex; flex-direction: column; gap: 14px; }
.cp-center { display: flex; align-items: center; justify-content: center; padding: 40px; color: var(--text-muted); }
.cp-stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 10px; }
.cp-stat {
  background: var(--bg-body); border: 1px solid var(--border); border-radius: 8px;
  padding: 12px; text-align: center;
}
.cp-stat-val { font-size: 20px; font-weight: 700; color: var(--text-primary); }
.cp-stat-lbl { font-size: 10.5px; color: var(--text-muted); margin-top: 2px; }
.cp-section-label { font-size: 10.5px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.4px; color: var(--text-muted); }
.cp-fill-bar {
  height: 10px; background: var(--bg-body); border-radius: 5px;
  border: 1px solid var(--border); overflow: hidden; position: relative;
}
.cp-fill-inner { height: 100%; background: #f87171; border-radius: 5px; transition: width 0.4s; }
.cp-fill-pct { position: absolute; right: 6px; top: 50%; transform: translateY(-50%); font-size: 9px; color: var(--text-muted); }
.cp-minmax { display: flex; gap: 10px; }
.cp-minmax-item {
  flex: 1; padding: 10px 14px; background: var(--bg-body);
  border: 1px solid var(--border); border-radius: 8px;
  display: flex; align-items: center; gap: 8px;
}
.cp-mm-lbl { font-size: 10px; font-weight: 700; text-transform: uppercase; color: var(--text-muted); }
.cp-mm-val { font-family: var(--mono, monospace); font-size: 13px; color: var(--text-primary); }
.cp-histogram {
  display: flex; align-items: flex-end; gap: 3px; height: 80px;
  padding: 0 2px;
}
.cp-hist-bar {
  flex: 1; background: var(--brand); border-radius: 2px 2px 0 0;
  opacity: 0.8; transition: opacity 0.1s; cursor: default; min-width: 4px;
}
.cp-hist-bar:hover { opacity: 1; }
.cp-hist-labels { display: flex; justify-content: space-between; font-size: 9px; color: var(--text-muted); padding: 0 2px; }
.cp-top-list { display: flex; flex-direction: column; gap: 5px; }
.cp-top-row { display: flex; align-items: center; gap: 8px; }
.cp-top-rank { font-size: 10px; color: var(--text-muted); width: 14px; text-align: right; flex-shrink: 0; }
.cp-top-val { font-family: var(--mono, monospace); font-size: 12px; color: var(--text-primary); min-width: 120px; max-width: 160px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cp-top-bar-wrap { flex: 1; height: 6px; background: var(--bg-body); border-radius: 3px; overflow: hidden; }
.cp-top-bar { height: 100%; background: var(--brand); border-radius: 3px; opacity: 0.7; }
.cp-top-cnt { font-size: 11px; color: var(--text-muted); min-width: 40px; text-align: right; flex-shrink: 0; font-variant-numeric: tabular-nums; }
</style>
