<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import axios from 'axios'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'

interface AlertLogEntry {
  id: number
  channel: string
  target_name: string
  status: 'ok' | 'error'
  error_msg: string
  triggered_by: string
  created_at: string
}

interface StatEntry {
  channel: string
  status: string
  count: number
}

interface TelegramTarget { name: string; chat_id: string; topic_id: string; bot_token: string }
interface DiscordTarget  { name: string; webhook_url: string; username: string }
interface SlackTarget    { name: string; webhook_url: string; channel: string; username: string }

interface AlertSettings {
  telegram: { enabled: boolean; targets: TelegramTarget[] }
  discord:  { enabled: boolean; targets: DiscordTarget[]  }
  slack:    { enabled: boolean; targets: SlackTarget[]    }
  webhook:  { enabled: boolean; url: string               }
}

interface DbStatus {
  stored: boolean
  enabled?: boolean
  target_count?: number
  raw_bytes?: number
  error?: string
  parse_error?: string
}

interface AlertDbStatus {
  alert_telegram: DbStatus
  alert_discord:  DbStatus
  alert_slack:    DbStatus
  alert_webhook:  DbStatus
}

const toast = useToast()
const { confirm } = useConfirm()

const loading = ref(false)
const clearing = ref(false)
const autoRefresh = ref(false)
const entries = ref<AlertLogEntry[]>([])
const stats = ref<StatEntry[]>([])
const alertSettings = ref<AlertSettings | null>(null)
const dbStatus = ref<AlertDbStatus | null>(null)

const filterChannel = ref('')
const filterStatus = ref('')

let refreshTimer: ReturnType<typeof setInterval> | null = null

const channels = ['telegram', 'discord', 'slack', 'webhook']

const channelMeta: Record<string, { label: string; color: string; bg: string }> = {
  telegram: { label: 'Telegram', color: '#29b6f6', bg: 'rgba(41,182,246,0.12)' },
  discord:  { label: 'Discord',  color: '#5865f2', bg: 'rgba(88,101,242,0.12)'  },
  slack:    { label: 'Slack',    color: '#e01e5a', bg: 'rgba(224,30,90,0.12)'   },
  webhook:  { label: 'Webhook',  color: 'var(--brand)', bg: 'rgba(99,102,241,0.12)' },
}

const totalOk = computed(() => stats.value.filter(s => s.status === 'ok').reduce((a, s) => a + s.count, 0))
const totalError = computed(() => stats.value.filter(s => s.status === 'error').reduce((a, s) => a + s.count, 0))
const totalAll = computed(() => totalOk.value + totalError.value)

function statFor(channel: string, status: string) {
  return stats.value.find(s => s.channel === channel && s.status === status)?.count ?? 0
}

async function load() {
  loading.value = true
  try {
    const params: Record<string, string> = {}
    if (filterChannel.value) params.channel = filterChannel.value
    if (filterStatus.value) params.status = filterStatus.value

    const [logRes, statsRes, settingsRes, dbRes] = await Promise.all([
      axios.get<AlertLogEntry[]>('/api/alerts/log', { params }),
      axios.get<StatEntry[]>('/api/alerts/log/stats'),
      axios.get<AlertSettings>('/api/settings/alerts'),
      axios.get<AlertDbStatus>('/api/settings/alerts/status'),
    ])
    entries.value = logRes.data ?? []
    stats.value = statsRes.data ?? []
    alertSettings.value = settingsRes.data ?? null
    dbStatus.value = dbRes.data ?? null
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to load alert log')
  } finally {
    loading.value = false
  }
}

async function clearLog() {
  const ok = await confirm('This will permanently delete all alert log entries. Continue?', 'Clear Alert Log')
  if (!ok) return
  clearing.value = true
  try {
    await axios.delete('/api/alerts/log')
    entries.value = []
    stats.value = []
    toast.success('Alert log cleared')
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to clear alert log')
  } finally {
    clearing.value = false
  }
}

function toggleAutoRefresh() {
  autoRefresh.value = !autoRefresh.value
  if (autoRefresh.value) {
    refreshTimer = setInterval(load, 15000)
  } else {
    if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null }
  }
}

function formatTime(raw: string) {
  if (!raw) return '—'
  const d = new Date(raw.replace(' ', 'T') + 'Z')
  if (isNaN(d.getTime())) return raw
  return d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'medium' })
}

function relativeTime(raw: string) {
  if (!raw) return ''
  const d = new Date(raw.replace(' ', 'T') + 'Z')
  if (isNaN(d.getTime())) return ''
  const diff = (Date.now() - d.getTime()) / 1000
  if (diff < 60) return `${Math.floor(diff)}s ago`
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
  return `${Math.floor(diff / 86400)}d ago`
}

onMounted(load)
onUnmounted(() => { if (refreshTimer) clearInterval(refreshTimer) })
</script>

<template>
  <div class="page-shell alg-root">
    <div class="page-scroll">
      <div class="page-stack">

        <!-- Hero -->
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Alerts</div>
            <div class="page-title">Alert Log</div>
            <div class="page-subtitle">History of every alert delivery — tests and automated triggers across all configured channels.</div>
          </div>
          <div class="page-hero__actions">
            <button
              class="base-btn base-btn--ghost base-btn--sm"
              :class="{ 'base-btn--active': autoRefresh }"
              @click="toggleAutoRefresh"
              :title="autoRefresh ? 'Auto-refresh on (every 15s) — click to stop' : 'Enable auto-refresh'"
            >
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" :class="{ 'alg-spin': autoRefresh }"><polyline points="23 4 23 10 17 10"/><polyline points="1 20 1 14 7 14"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/></svg>
              {{ autoRefresh ? 'Live' : 'Auto-refresh' }}
            </button>
            <button class="base-btn base-btn--ghost base-btn--sm" :disabled="loading" @click="load">
              {{ loading ? 'Loading…' : 'Refresh' }}
            </button>
            <button class="base-btn base-btn--danger base-btn--sm" :disabled="clearing || entries.length === 0" @click="clearLog">
              Clear Log
            </button>
          </div>
        </section>

        <!-- Stats strip -->
        <div class="alg-stats">
          <div class="alg-stat">
            <span class="alg-stat__num">{{ totalAll }}</span>
            <span class="alg-stat__label">Total</span>
          </div>
          <div class="alg-stat alg-stat--ok">
            <span class="alg-stat__num">{{ totalOk }}</span>
            <span class="alg-stat__label">Delivered</span>
          </div>
          <div class="alg-stat alg-stat--error">
            <span class="alg-stat__num">{{ totalError }}</span>
            <span class="alg-stat__label">Failed</span>
          </div>
          <div class="alg-stat-divider"></div>
          <div v-for="ch in channels" :key="ch" class="alg-stat alg-stat--channel">
            <div class="alg-stat__ch-label" :style="{ color: channelMeta[ch]?.color }">{{ channelMeta[ch]?.label }}</div>
            <div class="alg-stat__ch-row">
              <span class="alg-mini-badge alg-mini-badge--ok">{{ statFor(ch, 'ok') }}</span>
              <span class="alg-mini-badge alg-mini-badge--error">{{ statFor(ch, 'error') }}</span>
            </div>
          </div>
        </div>

        <!-- Database storage status -->
        <section class="page-panel alg-dbstatus-panel" v-if="dbStatus">
          <div class="alg-dbstatus-head">
            <span class="alg-dbstatus-title">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/></svg>
              Database Storage
            </span>
            <span class="alg-dbstatus-sub">What is actually saved in the Anveesa internal database right now</span>
          </div>
          <div class="alg-dbstatus-grid">
            <div
              v-for="key in (['alert_telegram','alert_discord','alert_slack','alert_webhook'] as const)"
              :key="key"
              class="alg-dbstatus-row"
              :class="dbStatus[key]?.error ? 'alg-dbstatus-row--error' : dbStatus[key]?.stored ? 'alg-dbstatus-row--ok' : 'alg-dbstatus-row--empty'"
            >
              <div class="alg-dbstatus-row__left">
                <span class="alg-dbstatus-key">{{ key }}</span>
                <span v-if="dbStatus[key]?.error" class="alg-dbstatus-badge alg-dbstatus-badge--error">DB Error</span>
                <span v-else-if="dbStatus[key]?.parse_error" class="alg-dbstatus-badge alg-dbstatus-badge--error">Parse Error</span>
                <span v-else-if="dbStatus[key]?.stored" class="alg-dbstatus-badge alg-dbstatus-badge--ok">Saved</span>
                <span v-else class="alg-dbstatus-badge alg-dbstatus-badge--empty">Not saved</span>
              </div>
              <div class="alg-dbstatus-row__right" v-if="dbStatus[key]?.stored && !dbStatus[key]?.error">
                <span class="alg-dbstatus-meta">
                  enabled: <strong>{{ dbStatus[key]?.enabled ? 'yes' : 'no' }}</strong>
                </span>
                <span class="alg-dbstatus-meta" v-if="key !== 'alert_webhook'">
                  targets: <strong>{{ dbStatus[key]?.target_count ?? 0 }}</strong>
                </span>
                <span class="alg-dbstatus-meta">
                  {{ dbStatus[key]?.raw_bytes }} bytes
                </span>
              </div>
              <div class="alg-dbstatus-row__right" v-else-if="dbStatus[key]?.error">
                <span class="alg-dbstatus-meta alg-dbstatus-meta--error">{{ dbStatus[key]?.error }}</span>
              </div>
              <div class="alg-dbstatus-row__right" v-else>
                <span class="alg-dbstatus-meta">No data — go to Alert Settings and click Save All</span>
              </div>
            </div>
          </div>
        </section>

        <!-- Configured channels overview -->
        <section class="page-panel alg-channels-panel" v-if="alertSettings">
          <div class="alg-channels-head">
            <span class="alg-channels-title">Configured Channels</span>
            <router-link :to="{ name: 'alert-settings' }" class="base-btn base-btn--ghost base-btn--xs">
              Edit Settings
            </router-link>
          </div>

          <div class="alg-channels-grid">

            <!-- Telegram -->
            <div class="alg-ch-card">
              <div class="alg-ch-card__head">
                <span class="alg-ch-label" style="color:#29b6f6">Telegram</span>
                <span class="alg-ch-status" :class="alertSettings.telegram.enabled ? 'alg-ch-status--on' : 'alg-ch-status--off'">
                  {{ alertSettings.telegram.enabled ? 'Enabled' : 'Disabled' }}
                </span>
              </div>
              <div v-if="alertSettings.telegram.targets.length === 0" class="alg-ch-empty">No targets configured</div>
              <div v-for="(t, i) in alertSettings.telegram.targets" :key="i" class="alg-ch-target">
                <div class="alg-ch-target__name">{{ t.name || `Target ${i + 1}` }}</div>
                <div class="alg-ch-target__meta">
                  <span class="alg-ch-meta-item">
                    <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>
                    {{ t.chat_id || '—' }}
                  </span>
                  <span v-if="t.topic_id" class="alg-ch-meta-item">
                    <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z"/></svg>
                    Topic {{ t.topic_id }}
                  </span>
                  <span class="alg-ch-meta-item" :class="t.bot_token ? 'alg-ch-meta-item--ok' : 'alg-ch-meta-item--warn'">
                    {{ t.bot_token ? 'Token set' : 'No token' }}
                  </span>
                </div>
              </div>
            </div>

            <!-- Discord -->
            <div class="alg-ch-card">
              <div class="alg-ch-card__head">
                <span class="alg-ch-label" style="color:#5865f2">Discord</span>
                <span class="alg-ch-status" :class="alertSettings.discord.enabled ? 'alg-ch-status--on' : 'alg-ch-status--off'">
                  {{ alertSettings.discord.enabled ? 'Enabled' : 'Disabled' }}
                </span>
              </div>
              <div v-if="alertSettings.discord.targets.length === 0" class="alg-ch-empty">No targets configured</div>
              <div v-for="(t, i) in alertSettings.discord.targets" :key="i" class="alg-ch-target">
                <div class="alg-ch-target__name">{{ t.name || `Target ${i + 1}` }}</div>
                <div class="alg-ch-target__meta">
                  <span class="alg-ch-meta-item" :class="t.webhook_url ? 'alg-ch-meta-item--ok' : 'alg-ch-meta-item--warn'">
                    {{ t.webhook_url ? 'Webhook set' : 'No webhook' }}
                  </span>
                  <span v-if="t.username" class="alg-ch-meta-item">{{ t.username }}</span>
                </div>
              </div>
            </div>

            <!-- Slack -->
            <div class="alg-ch-card">
              <div class="alg-ch-card__head">
                <span class="alg-ch-label" style="color:#e01e5a">Slack</span>
                <span class="alg-ch-status" :class="alertSettings.slack.enabled ? 'alg-ch-status--on' : 'alg-ch-status--off'">
                  {{ alertSettings.slack.enabled ? 'Enabled' : 'Disabled' }}
                </span>
              </div>
              <div v-if="alertSettings.slack.targets.length === 0" class="alg-ch-empty">No targets configured</div>
              <div v-for="(t, i) in alertSettings.slack.targets" :key="i" class="alg-ch-target">
                <div class="alg-ch-target__name">{{ t.name || `Target ${i + 1}` }}</div>
                <div class="alg-ch-target__meta">
                  <span v-if="t.channel" class="alg-ch-meta-item">{{ t.channel }}</span>
                  <span v-if="t.username" class="alg-ch-meta-item">{{ t.username }}</span>
                  <span class="alg-ch-meta-item" :class="t.webhook_url ? 'alg-ch-meta-item--ok' : 'alg-ch-meta-item--warn'">
                    {{ t.webhook_url ? 'Webhook set' : 'No webhook' }}
                  </span>
                </div>
              </div>
            </div>

            <!-- Webhook -->
            <div class="alg-ch-card">
              <div class="alg-ch-card__head">
                <span class="alg-ch-label" style="color:var(--brand)">Webhook</span>
                <span class="alg-ch-status" :class="alertSettings.webhook.enabled ? 'alg-ch-status--on' : 'alg-ch-status--off'">
                  {{ alertSettings.webhook.enabled ? 'Enabled' : 'Disabled' }}
                </span>
              </div>
              <div v-if="!alertSettings.webhook.url" class="alg-ch-empty">No endpoint configured</div>
              <div v-else class="alg-ch-target">
                <div class="alg-ch-target__meta">
                  <span class="alg-ch-meta-item alg-ch-meta-item--ok">Endpoint set</span>
                </div>
              </div>
            </div>

          </div>
        </section>

        <!-- Filters + table -->
        <section class="page-panel alg-panel">

          <!-- Filter bar -->
          <div class="alg-filters">
            <div class="alg-filters__left">
              <select v-model="filterChannel" class="base-input alg-select" @change="load">
                <option value="">All channels</option>
                <option v-for="ch in channels" :key="ch" :value="ch">{{ channelMeta[ch]?.label }}</option>
              </select>
              <select v-model="filterStatus" class="base-input alg-select" @change="load">
                <option value="">All statuses</option>
                <option value="ok">Delivered</option>
                <option value="error">Failed</option>
              </select>
            </div>
            <span class="alg-filters__count">{{ entries.length }} entr{{ entries.length === 1 ? 'y' : 'ies' }}</span>
          </div>

          <!-- Empty state -->
          <div v-if="!loading && entries.length === 0" class="alg-empty">
            <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" style="color:var(--text-muted)"><path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.73 21a2 2 0 0 1-3.46 0"/></svg>
            <div class="alg-empty__title">No alert deliveries yet</div>
            <div class="alg-empty__sub">Send a test message from Alert Settings to see entries here.</div>
          </div>

          <!-- Table -->
          <div v-else class="alg-table-wrap">
            <table class="alg-table">
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Channel</th>
                  <th>Target</th>
                  <th>Triggered By</th>
                  <th>Status</th>
                  <th>Error</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="e in entries"
                  :key="e.id"
                  :class="{ 'alg-row--error': e.status === 'error' }"
                >
                  <td class="alg-td-time">
                    <span :title="formatTime(e.created_at)">{{ relativeTime(e.created_at) }}</span>
                    <span class="alg-td-time__abs">{{ formatTime(e.created_at) }}</span>
                  </td>
                  <td>
                    <span
                      class="alg-channel-badge"
                      :style="{ color: channelMeta[e.channel]?.color, background: channelMeta[e.channel]?.bg }"
                    >{{ channelMeta[e.channel]?.label || e.channel }}</span>
                  </td>
                  <td class="alg-td-target">{{ e.target_name || '—' }}</td>
                  <td>
                    <span class="alg-trigger-badge" :class="`alg-trigger-badge--${e.triggered_by}`">
                      {{ e.triggered_by }}
                    </span>
                  </td>
                  <td>
                    <span class="alg-status-badge" :class="e.status === 'ok' ? 'alg-status-badge--ok' : 'alg-status-badge--error'">
                      {{ e.status === 'ok' ? 'Delivered' : 'Failed' }}
                    </span>
                  </td>
                  <td class="alg-td-error">{{ e.error_msg || '—' }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>

      </div>
    </div>
  </div>
</template>

<style scoped>
.alg-root { background: var(--bg-body); }
.page-scroll { padding: 16px 20px 24px; }

/* Stats strip */
.alg-stats {
  display: flex;
  align-items: center;
  gap: 0;
  padding: 14px 20px;
  background: var(--bg-panel);
  border: 1px solid var(--border);
  border-radius: 14px;
  flex-wrap: wrap;
  gap: 4px;
}

.alg-stat {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
  padding: 6px 16px;
  border-radius: 8px;
}

.alg-stat__num {
  font-size: 22px;
  font-weight: 800;
  color: var(--text-primary);
  line-height: 1;
}

.alg-stat__label {
  font-size: 11px;
  color: var(--text-muted);
  font-weight: 500;
}

.alg-stat--ok .alg-stat__num  { color: #22c55e; }
.alg-stat--error .alg-stat__num { color: #ef4444; }

.alg-stat-divider {
  width: 1px;
  height: 40px;
  background: var(--border);
  margin: 0 8px;
  flex-shrink: 0;
}

.alg-stat--channel {
  flex-direction: column;
  gap: 4px;
  align-items: flex-start;
  padding: 4px 14px;
}

.alg-stat__ch-label {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.alg-stat__ch-row {
  display: flex;
  gap: 4px;
}

.alg-mini-badge {
  font-size: 11px;
  font-weight: 700;
  padding: 1px 6px;
  border-radius: 999px;
}

.alg-mini-badge--ok    { background: rgba(34,197,94,0.12); color: #22c55e; }
.alg-mini-badge--error { background: rgba(239,68,68,0.12);  color: #ef4444; }

/* DB status panel */
.alg-dbstatus-panel { padding: 14px 18px; display: grid; gap: 12px; }

.alg-dbstatus-head {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.alg-dbstatus-title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  font-weight: 700;
  color: var(--text-secondary);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.alg-dbstatus-sub {
  font-size: 11px;
  color: var(--text-muted);
}

.alg-dbstatus-grid {
  display: grid;
  gap: 6px;
}

.alg-dbstatus-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 14px;
  border-radius: 8px;
  border: 1px solid var(--border);
  flex-wrap: wrap;
}

.alg-dbstatus-row--ok    { background: rgba(34,197,94,0.04);  border-color: rgba(34,197,94,0.2); }
.alg-dbstatus-row--empty { background: rgba(255,255,255,0.02); }
.alg-dbstatus-row--error { background: rgba(239,68,68,0.05);  border-color: rgba(239,68,68,0.25); }

.alg-dbstatus-row__left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.alg-dbstatus-key {
  font-size: 12px;
  font-weight: 700;
  font-family: monospace;
  color: var(--text-primary);
}

.alg-dbstatus-badge {
  font-size: 10px;
  font-weight: 700;
  padding: 2px 7px;
  border-radius: 999px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.alg-dbstatus-badge--ok    { background: rgba(34,197,94,0.15); color: #22c55e; }
.alg-dbstatus-badge--empty { background: rgba(255,255,255,0.07); color: var(--text-muted); }
.alg-dbstatus-badge--error { background: rgba(239,68,68,0.15); color: #ef4444; }

.alg-dbstatus-row__right {
  display: flex;
  gap: 14px;
  flex-wrap: wrap;
}

.alg-dbstatus-meta {
  font-size: 12px;
  color: var(--text-muted);
}

.alg-dbstatus-meta strong { color: var(--text-primary); }
.alg-dbstatus-meta--error { color: #ef4444; }

/* Configured channels */
.alg-channels-panel { padding: 16px 20px; display: grid; gap: 14px; }

.alg-channels-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.alg-channels-title {
  font-size: 12px;
  font-weight: 700;
  color: var(--text-secondary);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.alg-channels-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

.alg-ch-card {
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 12px 14px;
  display: grid;
  gap: 8px;
  background: rgba(255,255,255,0.02);
}

.alg-ch-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.alg-ch-label {
  font-size: 12px;
  font-weight: 800;
  text-transform: uppercase;
  letter-spacing: 0.07em;
}

.alg-ch-status {
  font-size: 10px;
  font-weight: 700;
  padding: 2px 7px;
  border-radius: 999px;
}

.alg-ch-status--on  { background: rgba(34,197,94,0.12); color: #22c55e; }
.alg-ch-status--off { background: rgba(255,255,255,0.06); color: var(--text-muted); }

.alg-ch-empty {
  font-size: 11px;
  color: var(--text-muted);
  font-style: italic;
}

.alg-ch-target {
  padding: 6px 8px;
  background: rgba(255,255,255,0.03);
  border-radius: 7px;
  border: 1px solid var(--border);
  display: grid;
  gap: 4px;
}

.alg-ch-target__name {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.alg-ch-target__meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.alg-ch-meta-item {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 10.5px;
  color: var(--text-muted);
  background: rgba(255,255,255,0.05);
  padding: 2px 6px;
  border-radius: 5px;
}

.alg-ch-meta-item--ok   { color: #22c55e; background: rgba(34,197,94,0.1); }
.alg-ch-meta-item--warn { color: #f59e0b; background: rgba(245,158,11,0.1); }

/* Panel */
.alg-panel { padding: 0; overflow: hidden; }

/* Filters */
.alg-filters {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 18px;
  border-bottom: 1px solid var(--border);
  gap: 10px;
  flex-wrap: wrap;
}

.alg-filters__left {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.alg-select {
  width: auto;
  min-width: 130px;
  padding: 6px 10px;
  font-size: 12px;
  cursor: pointer;
}

.alg-filters__count {
  font-size: 12px;
  color: var(--text-muted);
  flex-shrink: 0;
}

/* Empty */
.alg-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 52px 20px;
  text-align: center;
}

.alg-empty__title {
  font-size: 14px;
  font-weight: 700;
  color: var(--text-secondary);
}

.alg-empty__sub {
  font-size: 12px;
  color: var(--text-muted);
}

/* Table */
.alg-table-wrap {
  overflow-x: auto;
}

.alg-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12.5px;
}

.alg-table th {
  padding: 10px 14px;
  text-align: left;
  font-size: 11px;
  font-weight: 700;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  border-bottom: 1px solid var(--border);
  white-space: nowrap;
  background: rgba(255, 255, 255, 0.02);
}

.alg-table td {
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
  color: var(--text-primary);
  vertical-align: middle;
}

.alg-table tr:last-child td { border-bottom: none; }

.alg-row--error td { background: rgba(239, 68, 68, 0.03); }

.alg-td-time {
  white-space: nowrap;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.alg-td-time__abs {
  font-size: 10.5px;
  color: var(--text-muted);
}

.alg-td-target {
  color: var(--text-secondary);
  max-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.alg-td-error {
  color: #ef4444;
  font-size: 12px;
  max-width: 260px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Badges */
.alg-channel-badge {
  display: inline-flex;
  align-items: center;
  padding: 3px 8px;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 700;
  white-space: nowrap;
}

.alg-status-badge {
  display: inline-flex;
  align-items: center;
  padding: 3px 8px;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 700;
  white-space: nowrap;
}

.alg-status-badge--ok    { background: rgba(34,197,94,0.12); color: #22c55e; }
.alg-status-badge--error { background: rgba(239,68,68,0.12);  color: #ef4444; }

.alg-trigger-badge {
  display: inline-flex;
  align-items: center;
  padding: 3px 8px;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 600;
  background: rgba(255,255,255,0.06);
  color: var(--text-secondary);
  text-transform: capitalize;
  white-space: nowrap;
}

.alg-trigger-badge--test {
  background: rgba(var(--brand-rgb, 99, 102, 241), 0.12);
  color: var(--brand);
}

/* Spin animation for live refresh icon */
@keyframes alg-spin {
  to { transform: rotate(360deg); }
}
.alg-spin { animation: alg-spin 1.5s linear infinite; }

@media (max-width: 960px) {
  .page-scroll { padding: 12px 14px 20px; }
  .alg-stats { gap: 2px; }
  .alg-stat { padding: 4px 10px; }
  .alg-channels-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

@media (max-width: 600px) {
  .alg-channels-grid { grid-template-columns: 1fr; }
}
</style>
