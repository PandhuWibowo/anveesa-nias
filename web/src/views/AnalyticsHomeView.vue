<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import { useSavedQueries } from '@/composables/useSavedQueries'
import { useAuth } from '@/composables/useAuth'
import { formatServerTimestamp } from '@/utils/datetime'
import NavIcon from '@/components/ui/NavIcon.vue'
import EmptyState from '@/components/ui/EmptyState.vue'

interface AIReport {
  id: number
  connection_id: number
  title: string
  summary: string
  chart_type: string
  created_at: string
}

interface DashboardSummary {
  id: number
  name: string
  description: string
  visibility: 'private' | 'shared' | 'public'
  updated_at?: string
  created_at: string
}

const router = useRouter()
const { hasAnyPermission } = useAuth()
const { connections, fetchConnections } = useConnections()
const { queries, fetchAll: fetchSavedQueries } = useSavedQueries()

const loading = ref(true)
const pinnedReports = ref<AIReport[]>([])
const dashboards = ref<DashboardSummary[]>([])

const canQuery = computed(() => hasAnyPermission(['sqlstudio.access']))
const canUseAI = computed(() => hasAnyPermission(['ai.use']))
const canSavedQueries = computed(() => hasAnyPermission(['savedqueries.manage']))
const canDashboards = computed(() => hasAnyPermission(['dashboards.manage']))

const recentSavedQueries = computed(() =>
  [...queries.value]
    .sort((a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime())
    .slice(0, 8),
)

const recentReports = computed(() =>
  [...pinnedReports.value]
    .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
    .slice(0, 5),
)

const recentDashboards = computed(() => dashboards.value.slice(0, 6))

function connectionNameByID(connID: number | null) {
  if (!connID) return 'Any connection'
  return connections.value.find((item) => item.id === connID)?.name ?? `Connection #${connID}`
}

function sqlPreview(sql: string) {
  return sql.replace(/\s+/g, ' ').trim().slice(0, 140)
}

async function loadReports() {
  if (!canUseAI.value) return
  try {
    const { data } = await axios.get<AIReport[]>('/api/ai/reports')
    pinnedReports.value = data ?? []
  } catch {
    pinnedReports.value = []
  }
}

async function loadDashboards() {
  if (!canDashboards.value) return
  try {
    const { data } = await axios.get<DashboardSummary[]>('/api/analytics-dashboards')
    dashboards.value = data ?? []
  } catch {
    dashboards.value = []
  }
}

async function loadAll() {
  loading.value = true
  try {
    await Promise.all([
      fetchConnections(),
      canSavedQueries.value ? fetchSavedQueries() : Promise.resolve(),
      loadReports(),
      loadDashboards(),
    ])
  } finally {
    loading.value = false
  }
}

onMounted(loadAll)
</script>

<template>
  <div class="page-shell">
    <div class="page-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-title">Analytics</div>
            <div class="page-subtitle">Your saved queries, dashboards, and AI reports.</div>
          </div>
          <div class="page-hero__actions">
            <button v-if="canUseAI" class="base-btn base-btn--sm" type="button" @click="router.push({ name: 'ai-analytics' })">
              <NavIcon name="chat" :size="15" /> Ask your data
            </button>
            <button v-if="canQuery" class="base-btn base-btn--primary base-btn--sm" type="button" @click="router.push({ name: 'data' })">
              <NavIcon name="plus" :size="15" /> New query
            </button>
          </div>
        </section>

        <div class="ah-grid">
          <!-- Saved queries: the dataset library everything else builds on -->
          <section v-if="canSavedQueries" class="page-panel ah-panel ah-panel--main">
            <header class="ah-head">
              <h2 class="ah-title">Saved queries <span v-if="queries.length" class="ah-count">{{ queries.length }}</span></h2>
              <router-link v-if="queries.length" :to="{ name: 'saved-queries' }" class="ah-link">View all</router-link>
            </header>
            <div v-if="loading" class="ah-skel">
              <div v-for="n in 4" :key="n" class="skeleton ah-skel__row" />
            </div>
            <EmptyState
              v-else-if="!recentSavedQueries.length"
              compact
              icon="saved"
              title="No saved queries yet"
              text="Write a query in SQL Studio and save it — it becomes reusable in dashboards, schedules, and alerts."
              :action-label="canQuery ? 'Open SQL Studio' : undefined"
              :action-to="{ name: 'data' }"
            />
            <ul v-else class="ah-list">
              <li v-for="q in recentSavedQueries" :key="q.id">
                <router-link :to="{ name: 'saved-queries' }" class="ah-row">
                  <span class="ah-row__main">
                    <span class="ah-row__title">{{ q.name }}</span>
                    <code class="ah-row__sql">{{ sqlPreview(q.sql) }}</code>
                  </span>
                  <span class="ah-row__meta">
                    <span>{{ connectionNameByID(q.conn_id) }}</span>
                    <span>{{ formatServerTimestamp(q.updated_at) }}</span>
                  </span>
                </router-link>
              </li>
            </ul>
          </section>

          <div class="ah-side">
            <section v-if="canDashboards" class="page-panel ah-panel">
              <header class="ah-head">
                <h2 class="ah-title">Dashboards <span v-if="dashboards.length" class="ah-count">{{ dashboards.length }}</span></h2>
                <router-link :to="{ name: 'dashboards' }" class="ah-link">{{ dashboards.length ? 'Open' : 'Create' }}</router-link>
              </header>
              <div v-if="loading" class="ah-skel"><div v-for="n in 2" :key="n" class="skeleton ah-skel__row" /></div>
              <p v-else-if="!recentDashboards.length" class="ah-empty">Combine saved queries into charts and tables you can share.</p>
              <ul v-else class="ah-list">
                <li v-for="d in recentDashboards" :key="d.id">
                  <router-link :to="{ name: 'dashboards', query: { id: String(d.id) } }" class="ah-row ah-row--compact">
                    <NavIcon name="dashboard" :size="16" class="ah-row__icon" />
                    <span class="ah-row__title">{{ d.name }}</span>
                    <span class="badge">{{ d.visibility }}</span>
                  </router-link>
                </li>
              </ul>
            </section>

            <section v-if="canUseAI" class="page-panel ah-panel">
              <header class="ah-head">
                <h2 class="ah-title">Pinned reports <span v-if="pinnedReports.length" class="ah-count">{{ pinnedReports.length }}</span></h2>
                <router-link :to="{ name: 'ai-analytics' }" class="ah-link">Ask a question</router-link>
              </header>
              <div v-if="loading" class="ah-skel"><div v-for="n in 2" :key="n" class="skeleton ah-skel__row" /></div>
              <p v-else-if="!recentReports.length" class="ah-empty">Ask a question in plain language, then pin the answer to keep it here.</p>
              <ul v-else class="ah-list">
                <li v-for="r in recentReports" :key="r.id">
                  <router-link :to="{ name: 'ai-analytics' }" class="ah-row">
                    <span class="ah-row__main">
                      <span class="ah-row__title">{{ r.title }}</span>
                      <span class="ah-row__summary">{{ r.summary }}</span>
                    </span>
                    <span class="ah-row__meta">
                      <span>{{ connectionNameByID(r.connection_id) }}</span>
                      <span>{{ formatServerTimestamp(r.created_at) }}</span>
                    </span>
                  </router-link>
                </li>
              </ul>
            </section>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ah-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.6fr) minmax(300px, 1fr);
  gap: 16px;
  align-items: start;
}
.ah-side {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-width: 0;
}
.ah-panel { overflow: hidden; }
.ah-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 16px;
  border-bottom: 1px solid var(--border);
}
.ah-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14.5px;
  font-weight: 600;
  color: var(--text-primary);
}
.ah-count {
  font-size: 12px;
  font-weight: 500;
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}
.ah-link {
  font-size: 13px;
  color: var(--brand);
  text-decoration: none;
  white-space: nowrap;
}
.ah-link:hover { text-decoration: underline; }
.ah-empty {
  padding: 16px;
  font-size: 13.5px;
  line-height: 1.55;
  color: var(--text-secondary);
}
.ah-list { list-style: none; }
.ah-list li + li { border-top: 1px solid var(--border); }
.ah-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 12px 16px;
  color: var(--text-primary);
  text-decoration: none;
  transition: background var(--dur) var(--ease);
}
.ah-row:hover { background: var(--bg-hover-strong); }
.ah-row--compact { justify-content: flex-start; gap: 10px; padding: 10px 16px; }
.ah-row--compact .ah-row__title { flex: 1; }
.ah-row__icon { color: var(--text-muted); flex-shrink: 0; }
.ah-row__main {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.ah-row__title {
  font-size: 14px;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ah-row__sql {
  font-family: var(--mono);
  font-size: 12px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ah-row__summary {
  font-size: 13px;
  color: var(--text-secondary);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.ah-row__meta {
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 2px;
  font-size: 12px;
  color: var(--text-muted);
  white-space: nowrap;
}
.ah-skel { padding: 12px 16px; display: flex; flex-direction: column; gap: 10px; }
.ah-skel__row { height: 36px; }

@media (max-width: 1000px) {
  .ah-grid { grid-template-columns: minmax(0, 1fr); }
}
@media (max-width: 560px) {
  .ah-row { flex-direction: column; align-items: flex-start; gap: 6px; }
  .ah-row--compact { flex-direction: row; align-items: center; }
  .ah-row__meta { flex-direction: row; gap: 10px; align-items: flex-start; }
}
</style>
