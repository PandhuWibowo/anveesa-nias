<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import { useToast } from '@/composables/useToast'
import { pendingAIAnalytics } from '@/composables/usePendingAIAnalytics'

interface AnalyticsResponse {
  connection_id: number
  database: string
  driver: string
  question: string
  title: string
  summary: string
  chart_type: string
  sql: string
  columns: string[]
  rows: any[][]
  row_count: number
  duration_ms: number
  assumptions: string[]
  follow_up_questions: string[]
  report_cards: string[]
  compare_preset: string
}

interface AIReport {
  id: number
  connection_id: number
  title: string
  question: string
  summary: string
  chart_type: string
  sql: string
  columns: string[]
  rows: any[][]
  report_cards: string[]
  follow_ups: string[]
  created_at: string
}

const toast = useToast()
const { connections, fetchConnections } = useConnections()

const loading = ref(false)
const selectedConnId = ref<number | null>(null)
const question = ref('')
const comparePreset = ref('')
const providedSQL = ref('')
const providedTitle = ref('')
const analysisSource = ref<'saved_query' | 'query_result' | ''>('')
const result = ref<AnalyticsResponse | null>(null)
const error = ref('')
const pinnedReports = ref<AIReport[]>([])
const pinning = ref(false)

const prompts = [
  'What are the top 10 tables by row count in this database?',
  'Which categories have the highest total sales this month?',
  'Show the daily trend for new orders in the last 30 days.',
  'Which users generated the most failed transactions this week?',
  'What changed most in the last 7 days compared with the previous 7 days?',
]

const opsPrompts = [
  'Summarize the slowest queries and likely causes for this connection.',
  'Which scheduled jobs are failing most often and what changed recently?',
  'Summarize pending approvals and highlight operational bottlenecks.',
  'Review recent backup requests and flag anything unusual.',
]

const compareOptions = [
  { value: '', label: 'No compare' },
  { value: 'last_7_days_vs_previous_7_days', label: 'Last 7 days vs previous 7 days' },
  { value: 'this_month_vs_last_month', label: 'This month vs last month' },
  { value: 'today_vs_yesterday', label: 'Today vs yesterday' },
]

const selectedConnection = computed(() =>
  selectedConnId.value != null ? connections.value.find((item) => item.id === selectedConnId.value) ?? null : null
)

const chartTone = computed(() => {
  switch (result.value?.chart_type) {
    case 'line':
      return { label: 'Line trend', color: '#2563eb' }
    case 'bar':
      return { label: 'Bar comparison', color: '#7c3aed' }
    case 'pie':
      return { label: 'Pie distribution', color: '#ea580c' }
    case 'kpi':
      return { label: 'KPI card', color: '#059669' }
    default:
      return { label: 'Table view', color: '#64748b' }
  }
})

function usePrompt(value: string) {
  if (providedSQL.value) {
    providedSQL.value = ''
    providedTitle.value = ''
    analysisSource.value = ''
  }
  question.value = value
}

function loadPinnedReport(report: AIReport) {
  selectedConnId.value = report.connection_id
  question.value = report.question || ''
  providedSQL.value = report.sql || ''
  providedTitle.value = report.title || ''
  analysisSource.value = 'saved_query'
  result.value = {
    connection_id: report.connection_id,
    database: '',
    driver: '',
    question: report.question,
    title: report.title,
    summary: report.summary,
    chart_type: report.chart_type,
    sql: report.sql,
    columns: report.columns || [],
    rows: report.rows || [],
    row_count: report.rows?.length || 0,
    duration_ms: 0,
    assumptions: [],
    follow_up_questions: report.follow_ups || [],
    report_cards: report.report_cards || [],
    compare_preset: '',
  }
}

async function runAnalysis() {
  if (!selectedConnId.value) {
    toast.error('Select a connection first')
    return
  }
  if (!question.value.trim() && !providedSQL.value.trim()) {
    toast.error('Enter an analytics question')
    return
  }
  loading.value = true
  error.value = ''
  try {
    const { data } = await axios.post<AnalyticsResponse>('/api/ai/analytics', {
      conn_id: selectedConnId.value,
      question: question.value.trim(),
      sql: providedSQL.value.trim() || undefined,
      title: providedTitle.value.trim() || undefined,
      compare_preset: comparePreset.value || undefined,
    })
    result.value = data
  } catch (e: any) {
    result.value = null
    error.value = e?.response?.data?.error || 'Failed to run AI analytics'
    toast.error(error.value)
  } finally {
    loading.value = false
  }
}

async function loadPinnedReports() {
  try {
    const { data } = await axios.get<AIReport[]>('/api/ai/reports')
    pinnedReports.value = data || []
  } catch {
    pinnedReports.value = []
  }
}

async function pinCurrentReport() {
  if (!result.value) return
  pinning.value = true
  try {
    await axios.post('/api/ai/reports', {
      connection_id: result.value.connection_id,
      title: result.value.title,
      question: result.value.question,
      summary: result.value.summary,
      chart_type: result.value.chart_type,
      sql: result.value.sql,
      columns: result.value.columns,
      rows: result.value.rows,
      report_cards: result.value.report_cards,
      follow_ups: result.value.follow_up_questions,
    })
    await loadPinnedReports()
    toast.success('AI report pinned')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to pin AI report')
  } finally {
    pinning.value = false
  }
}

async function deletePinnedReport(id: number) {
  try {
    await axios.delete(`/api/ai/reports/${id}`)
    await loadPinnedReports()
    toast.success('Pinned report removed')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to delete report')
  }
}

async function copySQL() {
  if (!result.value?.sql) return
  try {
    await navigator.clipboard.writeText(result.value.sql)
    toast.success('SQL copied')
  } catch {
    toast.error('Failed to copy SQL')
  }
}

function applyPendingAnalysis() {
  const pending = pendingAIAnalytics.value
  if (!pending) return
  selectedConnId.value = pending.connId
  question.value = pending.question || ''
  providedSQL.value = pending.sql || ''
  providedTitle.value = pending.title || ''
  analysisSource.value = pending.source || ''
  pendingAIAnalytics.value = null
}

watch(connections, (items) => {
  if (!selectedConnId.value && items.length > 0) {
    selectedConnId.value = items[0].id
  }
}, { immediate: true })

onMounted(() => {
  applyPendingAnalysis()
  fetchConnections()
  loadPinnedReports()
})
</script>

<template>
  <div class="page-shell aia-root">
    <div class="page-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">AI</div>
            <div class="page-title">AI Analytics</div>
            <div class="page-subtitle">Ask business questions in plain language, generate safe read-only SQL, and get an executive summary on top of the result set.</div>
          </div>
          <div class="page-hero__actions">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="fetchConnections">Refresh Connections</button>
          </div>
        </section>

        <section class="page-panel aia-input-panel">
          <div class="aia-panel-head">
            <div>
              <div class="aia-panel-title">{{ providedSQL ? 'Analyze Saved Query' : 'Ask Your Data' }}</div>
              <div class="aia-panel-sub">
                {{ providedSQL ? 'Review an existing read-only SQL query with AI, summarize the result, and get follow-up ideas.' : 'Pick a connection, write a business question, and let AI generate a safe read-only analytics query.' }}
              </div>
            </div>
          </div>

          <div v-if="providedSQL" class="aia-context-banner">
            <span class="aia-context-banner__label">{{ analysisSource === 'saved_query' ? 'Saved query' : 'Existing query' }}</span>
            <strong>{{ providedTitle || 'Untitled query' }}</strong>
            <span class="aia-context-banner__sub">AI will run this exact read-only SQL instead of generating a new one.</span>
          </div>

          <div class="aia-input-grid">
            <div class="aia-field">
              <label class="aia-label">Connection</label>
              <select v-model.number="selectedConnId" class="base-select">
                <option :value="null">Select connection</option>
                <option v-for="conn in connections" :key="conn.id" :value="conn.id">
                  {{ conn.name }} · {{ conn.driver }} · {{ conn.database }}
                </option>
              </select>
            </div>
            <div class="aia-field aia-field--wide">
              <label class="aia-label">{{ providedSQL ? 'Focus' : 'Question' }}</label>
              <textarea
                v-model="question"
                class="base-textarea aia-textarea"
                rows="4"
                :placeholder="providedSQL ? 'Tell AI what to focus on for this query, for example: summarize the biggest takeaway and risks.' : 'Ask a business question, for example: what are the top 5 products by revenue in the last 30 days?'"
              ></textarea>
            </div>
          </div>

          <div class="aia-toolbar-row">
            <div class="aia-field">
              <label class="aia-label">Compare</label>
              <select v-model="comparePreset" class="base-select">
                <option v-for="option in compareOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
              </select>
            </div>
          </div>

          <div v-if="!providedSQL" class="aia-prompts">
            <button v-for="prompt in prompts" :key="prompt" class="aia-prompt" @click="usePrompt(prompt)">
              {{ prompt }}
            </button>
          </div>

          <div class="aia-prompt-group">
            <div class="aia-label">Ops & Audit Prompts</div>
            <div class="aia-prompts">
              <button v-for="prompt in opsPrompts" :key="prompt" class="aia-prompt" @click="usePrompt(prompt)">
                {{ prompt }}
              </button>
            </div>
          </div>

          <div class="aia-actions">
            <button class="base-btn base-btn--primary base-btn--sm" :disabled="loading" @click="runAnalysis">
              {{ loading ? 'Analyzing…' : (providedSQL ? 'Analyze Saved Query' : 'Run AI Analysis') }}
            </button>
          </div>
        </section>

        <div v-if="error" class="notice notice--error">{{ error }}</div>

        <div v-if="loading" class="page-panel aia-loading">
          <svg class="spin" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
          <span>Running AI analytics…</span>
        </div>

        <div v-else-if="!result" class="page-panel aia-empty">
          <div class="aia-empty__title">No analysis yet</div>
          <div class="aia-empty__sub">Ask a question above to generate SQL, inspect the result, and get a narrative summary.</div>
        </div>

        <template v-if="result">
          <section v-if="result.report_cards?.length" class="aia-summary-grid">
            <div v-for="card in result.report_cards" :key="card" class="page-panel aia-summary-card">
              <div class="aia-summary-card__label">Report Card</div>
              <div class="aia-summary-card__text">{{ card }}</div>
            </div>
          </section>

          <section class="aia-summary-grid">
            <div class="page-panel aia-summary-card">
              <div class="aia-summary-card__label">Result</div>
              <div class="aia-summary-card__value">{{ result.title }}</div>
              <div class="aia-summary-card__sub">{{ result.database }} · {{ result.driver }}</div>
            </div>
            <div class="page-panel aia-summary-card">
              <div class="aia-summary-card__label">Rows</div>
              <div class="aia-summary-card__value">{{ result.row_count }}</div>
              <div class="aia-summary-card__sub">Preview rows returned</div>
            </div>
            <div class="page-panel aia-summary-card">
              <div class="aia-summary-card__label">Runtime</div>
              <div class="aia-summary-card__value">{{ result.duration_ms }} ms</div>
              <div class="aia-summary-card__sub">Database execution time</div>
            </div>
            <div class="page-panel aia-summary-card">
              <div class="aia-summary-card__label">Recommended Visual</div>
              <div class="aia-summary-card__value" :style="{ color: chartTone.color }">{{ chartTone.label }}</div>
              <div class="aia-summary-card__sub">Suggested by AI from the result shape</div>
            </div>
          </section>

          <section class="page-panel aia-narrative">
            <div class="aia-panel-head">
              <div>
                <div class="aia-panel-title">Narrative Summary</div>
                <div class="aia-panel-sub">AI-written overview grounded on the executed query result<span v-if="result.compare_preset"> · compare mode {{ result.compare_preset }}</span>.</div>
              </div>
              <button class="base-btn base-btn--ghost base-btn--sm" :disabled="pinning" @click="pinCurrentReport">{{ pinning ? 'Pinning…' : 'Pin Report' }}</button>
            </div>
            <p class="aia-summary-text">{{ result.summary }}</p>

            <div v-if="result.assumptions?.length" class="aia-chip-group">
              <span v-for="item in result.assumptions" :key="item" class="aia-chip">Assumption: {{ item }}</span>
            </div>
          </section>

          <section class="page-panel aia-query-panel">
            <div class="aia-panel-head">
              <div>
                <div class="aia-panel-title">Generated SQL</div>
                <div class="aia-panel-sub">This is the exact read-only query executed for the answer.</div>
              </div>
              <button class="base-btn base-btn--ghost base-btn--sm" @click="copySQL">Copy SQL</button>
            </div>
            <pre class="aia-code">{{ result.sql }}</pre>
          </section>

          <section class="page-panel">
            <div class="aia-panel-head">
              <div>
                <div class="aia-panel-title">Result Preview</div>
                <div class="aia-panel-sub">Table preview of the executed analytics query.</div>
              </div>
            </div>

            <div v-if="!result.columns.length" class="aia-empty">No columns returned.</div>
            <div v-else class="aia-table-wrap">
              <table class="aia-table">
                <thead>
                  <tr>
                    <th v-for="column in result.columns" :key="column">{{ column }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(row, rowIndex) in result.rows" :key="rowIndex">
                    <td v-for="(value, colIndex) in row" :key="`${rowIndex}-${colIndex}`">{{ value ?? '—' }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>

          <section v-if="result.follow_up_questions?.length" class="page-panel">
            <div class="aia-panel-head">
              <div>
                <div class="aia-panel-title">Suggested Follow-Ups</div>
                <div class="aia-panel-sub">Use these as the next AI analytics questions.</div>
              </div>
            </div>
            <div class="aia-prompts">
              <button v-for="item in result.follow_up_questions" :key="item" class="aia-prompt" @click="usePrompt(item)">
                {{ item }}
              </button>
            </div>
          </section>

          <section v-if="pinnedReports.length" class="page-panel">
            <div class="aia-panel-head">
              <div>
                <div class="aia-panel-title">Pinned AI Reports</div>
                <div class="aia-panel-sub">Reusable AI summaries you saved from previous analyses.</div>
              </div>
            </div>
            <div class="aia-report-list">
              <article v-for="report in pinnedReports" :key="report.id" class="aia-report-card">
                <div>
                  <div class="aia-report-card__title">{{ report.title }}</div>
                  <div class="aia-report-card__meta">{{ new Date(report.created_at).toLocaleString() }}</div>
                  <p class="aia-report-card__summary">{{ report.summary }}</p>
                </div>
                <div class="aia-report-card__actions">
                  <button class="base-btn base-btn--ghost base-btn--xs" @click="loadPinnedReport(report)">Open</button>
                  <button class="base-btn base-btn--ghost base-btn--xs" @click="deletePinnedReport(report.id)">Delete</button>
                </div>
              </article>
            </div>
          </section>
        </template>
      </div>
    </div>
  </div>
</template>

<style scoped>
.aia-root {
  background: var(--bg-body);
}

.page-scroll {
  padding: 16px 20px 24px;
}

.aia-input-grid {
  display: grid;
  grid-template-columns: minmax(220px, 260px) minmax(0, 1fr);
  gap: 12px;
}

.aia-toolbar-row,
.aia-prompt-group {
  margin-top: 10px;
}

.aia-input-panel {
  padding: 16px 20px;
}

.aia-context-banner {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.02);
  color: var(--text-secondary);
  font-size: 12px;
  flex-wrap: wrap;
}

.aia-context-banner__label {
  padding: 3px 8px;
  border-radius: 999px;
  border: 1px solid var(--brand-ring);
  background: var(--brand-dim);
  color: var(--brand);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.aia-context-banner__sub {
  color: var(--text-muted);
}

.aia-summary-card,
.aia-narrative,
.aia-query-panel,
.aia-loading,
.aia-empty {
  padding: 20px 24px;
}

.aia-field {
  display: grid;
  gap: 4px;
  align-content: start;
}

.aia-field--wide {
  min-width: 0;
}

.aia-label,
.aia-summary-card__label,
.aia-panel-sub {
  font-size: 12px;
  color: var(--text-muted);
}

.aia-panel-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}

.aia-textarea {
  min-height: 76px;
}

.aia-prompts {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 2px;
}

.aia-prompt,
.aia-chip {
  border: 1px solid var(--border);
  background: var(--bg-surface);
  color: var(--text-secondary);
  border-radius: 999px;
  padding: 6px 10px;
  font-size: 11px;
}

.aia-prompt {
  cursor: pointer;
}

.aia-prompt:hover {
  border-color: var(--brand-ring);
  color: var(--text-primary);
}

.aia-actions {
  display: flex;
  justify-content: flex-end;
  margin-top: 10px;
}

.aia-summary-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px;
}

.aia-summary-card {
  display: grid;
  gap: 6px;
}

.aia-summary-card__value {
  font-size: 24px;
  font-weight: 700;
  color: var(--text-primary);
}

.aia-summary-card__text {
  color: var(--text-primary);
  font-size: 14px;
  line-height: 1.5;
}

.aia-summary-card__sub,
.aia-empty {
  color: var(--text-secondary);
  font-size: 13px;
}

.aia-panel-title {
  font-size: 15px;
  font-weight: 700;
  color: var(--text-primary);
}

.aia-summary-text {
  margin: 0;
  color: var(--text-primary);
  line-height: 1.6;
}

.aia-chip-group {
  margin-top: 12px;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.aia-code {
  margin: 0;
  overflow-x: auto;
  padding: 14px 16px;
  border-radius: 10px;
  background: rgba(255, 255, 255, 0.02);
  border: 1px solid var(--border);
  color: var(--text-primary);
  font-size: 12px;
  line-height: 1.6;
}

.aia-table-wrap {
  overflow: hidden;
}

.aia-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.aia-table th {
  text-align: left;
  padding: 11px 18px;
  border-bottom: 1px solid var(--border);
  color: var(--text-muted);
  background: rgba(255, 255, 255, 0.02);
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.12em;
}

.aia-table td {
  padding: 12px 18px;
  border-bottom: 1px solid var(--border);
  color: var(--text-primary);
  vertical-align: top;
}

.aia-table tr:last-child td {
  border-bottom: none;
}

.aia-table tr:hover td {
  background: rgba(255, 255, 255, 0.03);
}

.aia-loading,
.aia-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 8px;
  min-height: 140px;
  color: var(--text-muted);
}

.aia-report-list {
  display: grid;
  gap: 12px;
}

.aia-report-card {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.02);
}

.aia-report-card__title {
  font-size: 14px;
  font-weight: 700;
  color: var(--text-primary);
}

.aia-report-card__meta {
  font-size: 11px;
  color: var(--text-muted);
  margin-top: 2px;
}

.aia-report-card__summary {
  margin: 8px 0 0;
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.5;
}

.aia-report-card__actions {
  display: flex;
  gap: 8px;
  align-items: flex-start;
}

.aia-empty__title {
  font-size: 15px;
  font-weight: 700;
  color: var(--text-primary);
}

.aia-empty__sub {
  max-width: 520px;
  text-align: center;
  font-size: 13px;
}

@media (max-width: 960px) {
  .page-scroll {
    padding: 12px 14px 20px;
  }

  .aia-input-panel,
  .aia-summary-card,
  .aia-narrative,
  .aia-query-panel,
  .aia-loading,
  .aia-empty {
    padding: 16px;
  }

  .aia-input-grid,
  .aia-summary-grid {
    grid-template-columns: 1fr;
  }

  .aia-panel-head {
    margin-bottom: 10px;
  }

  .aia-actions {
    justify-content: stretch;
  }
}
</style>
