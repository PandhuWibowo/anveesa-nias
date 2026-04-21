<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import axios from 'axios'
import { useAuth } from '@/composables/useAuth'
import { useToast } from '@/composables/useToast'

interface ConnectionOption {
  id: number
  name: string
  driver: string
  environment?: string
}

interface NotificationItem {
  id: number
  event_id: number
  event_type: string
  type: string
  severity: string
  title: string
  message: string
  entity_type: string
  entity_id: number
  read: boolean
  created_at: string
}

interface NotificationTarget {
  id: number
  name: string
  type: string
  description: string
  config: Record<string, any>
  is_active: boolean
  has_secret: boolean
  has_secondary_secret: boolean
}

interface NotificationRule {
  id: number
  name: string
  description: string
  event_type: string
  severity: string
  entity_type: string
  connection_id: number
  actor_user_id: number
  title_template: string
  message_template: string
  target_id: number
  is_active: boolean
}

interface NotificationDelivery {
  id: number
  event_id: number
  target_id: number
  target_name: string
  channel: string
  status: string
  attempts: number
  last_error: string
  last_response_code: number
  created_at: string
  updated_at: string
  event: {
    event_type: string
    title: string
    message: string
    severity: string
  }
}

interface NotificationEvent {
  id: number
  event_type: string
  category: string
  severity: string
  title: string
  message: string
  entity_type: string
  entity_id: number
  created_at: string
}

const toast = useToast()
const { hasPermission } = useAuth()

const loading = ref(false)
const inbox = ref<NotificationItem[]>([])
const unreadCount = ref(0)
const targets = ref<NotificationTarget[]>([])
const rules = ref<NotificationRule[]>([])
const deliveries = ref<NotificationDelivery[]>([])
const events = ref<NotificationEvent[]>([])
const connections = ref<ConnectionOption[]>([])
const deliveriesError = ref('')
const deliveriesLoading = ref(false)
const manageDataLoading = ref(false)
const savingTarget = ref(false)
const savingRule = ref(false)

const canManage = computed(() => hasPermission('notifications.manage'))
const activeTab = ref<'inbox' | 'integrations' | 'rules' | 'deliveries'>('inbox')
const editingTargetId = ref<number | null>(null)
const editingRuleId = ref<number | null>(null)

const targetForm = reactive({
  name: '',
  type: 'slack',
  description: '',
  secret: '',
  secondary_secret: '',
  chat_id: '',
  slack_username: '',
  slack_channel: '',
  slack_icon_emoji: '',
  slack_icon_url: '',
  discord_username: '',
  discord_avatar_url: '',
  discord_footer_text: '',
  discord_author_name: '',
  telegram_disable_notification: false,
  webhook_headers_json: '',
  webhook_signing_header: '',
  is_active: true,
})

const ruleForm = reactive({
  name: '',
  event_type: '*',
  severity: '',
  entity_type: '',
  target_id: null as number | null,
  connection_id: 0,
  actor_user_id: 0,
  title_template: '',
  message_template: '',
  description: '',
  is_active: true,
})

const eventTypeSuggestions = [
  'approval_request.created',
  'approval_request.updated',
  'approval_request.approved',
  'approval_request.rejected',
  'approval_request.executed',
  'approval_request.failed',
  'approval_request.overdue',
  'data_script.submitted',
  'data_script.approved',
  'data_script.rejected',
  'data_script.executed',
  'data_script.failed',
  'data_script.overdue',
  'backup_request.created',
  'backup_request.approved',
  'backup_request.rejected',
  'backup_request.download_ready',
  'backup_request.failed',
  'backup_request.overdue',
  'schedule.alert',
  'system.test',
  '*',
]

const inboxItems = computed(() => inbox.value)
const targetOptions = computed(() => targets.value.filter((item) => item.is_active))
const ruleTargetOptions = computed(() =>
  targets.value.filter((item) => item.is_active || item.id === ruleForm.target_id)
)

function resetTargetForm() {
  editingTargetId.value = null
  targetForm.name = ''
  targetForm.type = 'slack'
  targetForm.description = ''
  targetForm.secret = ''
  targetForm.secondary_secret = ''
  targetForm.chat_id = ''
  targetForm.slack_username = ''
  targetForm.slack_channel = ''
  targetForm.slack_icon_emoji = ''
  targetForm.slack_icon_url = ''
  targetForm.discord_username = ''
  targetForm.discord_avatar_url = ''
  targetForm.discord_footer_text = ''
  targetForm.discord_author_name = ''
  targetForm.telegram_disable_notification = false
  targetForm.webhook_headers_json = ''
  targetForm.webhook_signing_header = ''
  targetForm.is_active = true
}

function resetRuleForm() {
  editingRuleId.value = null
  ruleForm.name = ''
  ruleForm.description = ''
  ruleForm.event_type = '*'
  ruleForm.severity = ''
  ruleForm.entity_type = ''
  ruleForm.target_id = null
  ruleForm.connection_id = 0
  ruleForm.actor_user_id = 0
  ruleForm.title_template = ''
  ruleForm.message_template = ''
  ruleForm.is_active = true
}

function editTarget(target: NotificationTarget) {
  editingTargetId.value = target.id
  targetForm.name = target.name
  targetForm.type = target.type
  targetForm.description = target.description || ''
  targetForm.secret = ''
  targetForm.secondary_secret = ''
  targetForm.chat_id = String(target.config?.chat_id || '')
  targetForm.slack_username = String(target.config?.username || '')
  targetForm.slack_channel = String(target.config?.channel || '')
  targetForm.slack_icon_emoji = String(target.config?.icon_emoji || '')
  targetForm.slack_icon_url = String(target.config?.icon_url || '')
  targetForm.discord_username = String(target.config?.username || '')
  targetForm.discord_avatar_url = String(target.config?.avatar_url || '')
  targetForm.discord_footer_text = String(target.config?.footer_text || '')
  targetForm.discord_author_name = String(target.config?.author_name || '')
  targetForm.telegram_disable_notification = Boolean(target.config?.disable_notification)
  targetForm.webhook_headers_json = target.config?.headers ? JSON.stringify(target.config.headers, null, 2) : ''
  targetForm.webhook_signing_header = String(target.config?.signing_header || '')
  targetForm.is_active = target.is_active
}

function editRule(rule: NotificationRule) {
  editingRuleId.value = rule.id
  ruleForm.name = rule.name
  ruleForm.description = rule.description || ''
  ruleForm.event_type = rule.event_type || '*'
  ruleForm.severity = rule.severity || ''
  ruleForm.entity_type = rule.entity_type || ''
  ruleForm.target_id = rule.target_id || null
  ruleForm.connection_id = rule.connection_id || 0
  ruleForm.actor_user_id = rule.actor_user_id || 0
  ruleForm.title_template = rule.title_template || ''
  ruleForm.message_template = rule.message_template || ''
  ruleForm.is_active = rule.is_active
}

function integrationLabel(targetId: number) {
  const target = targets.value.find((item) => item.id === targetId)
  return target ? `${target.name} · ${target.type}` : `target #${targetId}`
}

function entityRoute(item: { entity_type: string; entity_id: number }) {
  if (!item.entity_id) return null
  switch (item.entity_type) {
    case 'approval_request':
      return { name: 'approvals' }
    case 'data_change_plan':
      return { name: 'data-script-requests' }
    case 'backup_download_request':
      return { name: 'backup' }
    default:
      return null
  }
}

function fmtTime(value: string) {
  if (!value) return ''
  return new Date(value).toLocaleString()
}

async function loadInbox() {
  const [itemsRes, unreadRes] = await Promise.all([
    axios.get<NotificationItem[]>('/api/notifications'),
    axios.get<{ count: number }>('/api/notifications/unread'),
  ])
  inbox.value = itemsRes.data || []
  unreadCount.value = unreadRes.data?.count || 0
}

async function loadManageData() {
  if (!canManage.value) return
  manageDataLoading.value = true
  try {
    const [targetsRes, rulesRes, eventsRes] = await Promise.all([
      axios.get<NotificationTarget[]>('/api/notification-targets'),
      axios.get<NotificationRule[]>('/api/notification-rules'),
      axios.get<NotificationEvent[]>('/api/notification-events'),
    ])
    targets.value = targetsRes.data || []
    rules.value = rulesRes.data || []
    events.value = eventsRes.data || []
  } finally {
    manageDataLoading.value = false
  }
}

async function loadDeliveries() {
  if (!canManage.value) return
  deliveriesLoading.value = true
  deliveriesError.value = ''
  try {
    const { data } = await axios.get<NotificationDelivery[]>('/api/notification-deliveries')
    deliveries.value = data || []
  } catch (error: any) {
    deliveries.value = []
    deliveriesError.value = error.response?.data?.error || 'Failed to load delivery logs'
  } finally {
    deliveriesLoading.value = false
  }
}

async function loadConnections() {
  if (!canManage.value) return
  const { data } = await axios.get<ConnectionOption[]>('/api/connections')
  connections.value = data || []
}

async function loadAll() {
  loading.value = true
  try {
    await loadInbox()
    await Promise.all([loadManageData(), loadConnections(), loadDeliveries()])
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to load notifications')
  } finally {
    loading.value = false
  }
}

async function markAllRead() {
  try {
    await axios.put('/api/notifications')
    await loadInbox()
    toast.success('Notifications marked as read')
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to mark notifications as read')
  }
}

async function submitTarget() {
  const isEditing = editingTargetId.value !== null
  savingTarget.value = true
  try {
    const config: Record<string, any> = {}
    if (targetForm.type === 'telegram' && targetForm.chat_id.trim()) {
      config.chat_id = targetForm.chat_id.trim()
      config.disable_notification = targetForm.telegram_disable_notification
    }
    if (targetForm.type === 'slack') {
      if (targetForm.slack_username.trim()) config.username = targetForm.slack_username.trim()
      if (targetForm.slack_channel.trim()) config.channel = targetForm.slack_channel.trim()
      if (targetForm.slack_icon_emoji.trim()) config.icon_emoji = targetForm.slack_icon_emoji.trim()
      if (targetForm.slack_icon_url.trim()) config.icon_url = targetForm.slack_icon_url.trim()
    }
    if (targetForm.type === 'discord') {
      if (targetForm.discord_username.trim()) config.username = targetForm.discord_username.trim()
      if (targetForm.discord_avatar_url.trim()) config.avatar_url = targetForm.discord_avatar_url.trim()
      if (targetForm.discord_footer_text.trim()) config.footer_text = targetForm.discord_footer_text.trim()
      if (targetForm.discord_author_name.trim()) config.author_name = targetForm.discord_author_name.trim()
    }
    if (targetForm.type === 'webhook') {
      if (targetForm.webhook_signing_header.trim()) config.signing_header = targetForm.webhook_signing_header.trim()
      if (targetForm.webhook_headers_json.trim()) {
        try {
          config.headers = JSON.parse(targetForm.webhook_headers_json)
        } catch {
          toast.error('Webhook headers must be valid JSON')
          return
        }
      }
    }
    const payload = {
      name: targetForm.name.trim(),
      type: targetForm.type,
      description: targetForm.description.trim(),
      secret: targetForm.secret.trim(),
      secondary_secret: targetForm.secondary_secret.trim(),
      config,
      is_active: targetForm.is_active,
    }
    if (editingTargetId.value) {
      await axios.put(`/api/notification-targets/${editingTargetId.value}`, payload)
    } else {
      await axios.post('/api/notification-targets', payload)
    }
    resetTargetForm()
    await loadManageData()
    void loadDeliveries()
    toast.success(isEditing ? 'Integration updated' : 'Integration created')
  } catch (error: any) {
    toast.error(error.response?.data?.error || `Failed to ${isEditing ? 'update' : 'create'} integration`)
  } finally {
    savingTarget.value = false
  }
}

async function deleteTarget(id: number) {
  try {
    await axios.delete(`/api/notification-targets/${id}`)
    await loadManageData()
    void loadDeliveries()
    toast.success('Integration deleted')
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to delete integration')
  }
}

async function testTarget(id: number) {
  try {
    await axios.post(`/api/notification-targets/${id}/test`)
    toast.success('Test notification sent')
    await loadDeliveries()
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to test integration')
  }
}

async function submitRule() {
  const isEditing = editingRuleId.value !== null
  if (!ruleForm.target_id) {
    toast.error('Select an integration target')
    return
  }
  savingRule.value = true
  try {
    const payload = {
      name: ruleForm.name.trim(),
      description: ruleForm.description.trim(),
      event_type: ruleForm.event_type.trim() || '*',
      severity: ruleForm.severity.trim(),
      entity_type: ruleForm.entity_type.trim(),
      target_id: ruleForm.target_id,
      connection_id: Number(ruleForm.connection_id) || 0,
      actor_user_id: Number(ruleForm.actor_user_id) || 0,
      title_template: ruleForm.title_template.trim(),
      message_template: ruleForm.message_template.trim(),
      is_active: ruleForm.is_active,
    }
    if (editingRuleId.value) {
      await axios.put(`/api/notification-rules/${editingRuleId.value}`, payload)
    } else {
      await axios.post('/api/notification-rules', payload)
    }
    resetRuleForm()
    await loadManageData()
    void loadDeliveries()
    toast.success(isEditing ? 'Rule updated' : 'Rule created')
  } catch (error: any) {
    toast.error(error.response?.data?.error || `Failed to ${isEditing ? 'update' : 'create'} rule`)
  } finally {
    savingRule.value = false
  }
}

async function deleteRule(id: number) {
  try {
    await axios.delete(`/api/notification-rules/${id}`)
    await loadManageData()
    void loadDeliveries()
    toast.success('Rule deleted')
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to delete rule')
  }
}

onMounted(loadAll)
</script>

<template>
  <section class="notif-page">
    <header class="notif-hero">
      <div>
        <div class="notif-eyebrow">Notifications</div>
        <h1>Inbox, routing rules, and platform delivery</h1>
        <p>Track approvals, data script requests, backup requests, and wire alerts into Slack, Discord, Telegram, or generic webhooks.</p>
      </div>
      <div class="notif-hero__stats">
        <div class="notif-stat">
          <span class="notif-stat__label">Unread</span>
          <strong>{{ unreadCount }}</strong>
        </div>
        <div class="notif-stat" v-if="canManage">
          <span class="notif-stat__label">Integrations</span>
          <strong>{{ targets.length }}</strong>
        </div>
        <div class="notif-stat" v-if="canManage">
          <span class="notif-stat__label">Rules</span>
          <strong>{{ rules.length }}</strong>
        </div>
      </div>
    </header>

    <div class="notif-tabs">
      <button class="notif-tab" :class="{ 'notif-tab--active': activeTab === 'inbox' }" @click="activeTab = 'inbox'">Inbox</button>
      <button v-if="canManage" class="notif-tab" :class="{ 'notif-tab--active': activeTab === 'integrations' }" @click="activeTab = 'integrations'">Integrations</button>
      <button v-if="canManage" class="notif-tab" :class="{ 'notif-tab--active': activeTab === 'rules' }" @click="activeTab = 'rules'">Rules</button>
      <button v-if="canManage" class="notif-tab" :class="{ 'notif-tab--active': activeTab === 'deliveries' }" @click="activeTab = 'deliveries'">Delivery Logs</button>
    </div>

    <div v-if="activeTab === 'inbox'" class="notif-panel">
      <div class="notif-panel__head">
        <div>
          <h2>Inbox</h2>
          <p>Personal and global workflow notifications.</p>
        </div>
        <button class="base-btn base-btn--ghost base-btn--sm" :disabled="loading || unreadCount === 0" @click="markAllRead">Mark All Read</button>
      </div>
      <div v-if="!inboxItems.length" class="notif-empty">No notifications yet.</div>
      <div v-else class="notif-list">
        <article v-for="item in inboxItems" :key="item.id" class="notif-item" :class="[`notif-item--${item.severity || 'info'}`, { 'notif-item--read': item.read }]">
          <div class="notif-item__meta">
            <span class="notif-pill">{{ item.event_type || item.type }}</span>
            <span>{{ fmtTime(item.created_at) }}</span>
          </div>
          <h3>{{ item.title }}</h3>
          <p>{{ item.message }}</p>
          <router-link v-if="entityRoute(item)" class="notif-link" :to="entityRoute(item)!">Open related page</router-link>
        </article>
      </div>
    </div>

    <template v-if="canManage && activeTab === 'integrations'">
      <div class="notif-grid">
        <section class="notif-panel">
          <div class="notif-panel__head">
            <div>
              <h2>{{ editingTargetId ? 'Edit Integration' : 'New Integration' }}</h2>
              <p>{{ editingTargetId ? 'Update routing settings without losing the existing secret unless you replace it.' : 'Store a destination and secret for alert delivery.' }}</p>
            </div>
            <button v-if="editingTargetId" class="base-btn base-btn--ghost base-btn--sm" @click="resetTargetForm">Cancel</button>
          </div>
          <div class="notif-form">
            <input v-model="targetForm.name" class="base-input" placeholder="Integration name" />
            <select v-model="targetForm.type" class="base-select">
              <option value="slack">Slack</option>
              <option value="discord">Discord</option>
              <option value="telegram">Telegram</option>
              <option value="webhook">Webhook</option>
            </select>
            <input v-model="targetForm.description" class="base-input" placeholder="Description" />
            <input v-model="targetForm.secret" class="base-input" :placeholder="editingTargetId ? (targetForm.type === 'telegram' ? 'Bot token, leave blank to keep current' : 'Webhook URL, leave blank to keep current') : (targetForm.type === 'telegram' ? 'Bot token' : 'Webhook URL')" />
            <input v-if="targetForm.type === 'telegram'" v-model="targetForm.chat_id" class="base-input" placeholder="Telegram chat_id" />
            <label v-if="targetForm.type === 'telegram'" class="notif-check">
              <input v-model="targetForm.telegram_disable_notification" type="checkbox" />
              Send silently
            </label>
            <template v-if="targetForm.type === 'slack'">
              <input v-model="targetForm.slack_username" class="base-input" placeholder="Slack username override" />
              <input v-model="targetForm.slack_channel" class="base-input" placeholder="Slack channel override" />
              <input v-model="targetForm.slack_icon_emoji" class="base-input" placeholder="Slack icon emoji, for example :rotating_light:" />
              <input v-model="targetForm.slack_icon_url" class="base-input" placeholder="Slack icon URL" />
            </template>
            <template v-if="targetForm.type === 'discord'">
              <input v-model="targetForm.discord_username" class="base-input" placeholder="Discord username override" />
              <input v-model="targetForm.discord_avatar_url" class="base-input" placeholder="Discord avatar URL" />
              <input v-model="targetForm.discord_footer_text" class="base-input" placeholder="Discord embed footer text" />
              <input v-model="targetForm.discord_author_name" class="base-input" placeholder="Discord embed author name" />
            </template>
            <template v-if="targetForm.type === 'webhook'">
              <textarea v-model="targetForm.webhook_headers_json" class="base-textarea" rows="4" placeholder='Webhook headers JSON, for example {"X-Team":"ops"}'></textarea>
              <input v-model="targetForm.secondary_secret" class="base-input" :placeholder="editingTargetId ? 'Webhook signing secret, leave blank to keep current' : 'Webhook signing secret'" />
              <input v-model="targetForm.webhook_signing_header" class="base-input" placeholder="Signature header name, default X-Nias-Signature-256" />
            </template>
            <label class="notif-check">
              <input v-model="targetForm.is_active" type="checkbox" />
              Integration active
            </label>
            <div class="notif-form__actions">
              <button class="base-btn base-btn--primary base-btn--sm" :disabled="savingTarget" @click="submitTarget">
                {{ savingTarget ? (editingTargetId ? 'Saving...' : 'Creating...') : (editingTargetId ? 'Save Integration' : 'Create Integration') }}
              </button>
              <button v-if="editingTargetId" class="base-btn base-btn--ghost base-btn--sm" :disabled="savingTarget" @click="resetTargetForm">Cancel</button>
            </div>
          </div>
        </section>

        <section class="notif-panel">
          <div class="notif-panel__head">
            <div>
              <h2>Configured Integrations</h2>
              <p>Test delivery or remove unused targets.</p>
            </div>
            <button class="base-btn base-btn--ghost base-btn--sm" :disabled="manageDataLoading" @click="loadManageData">
              {{ manageDataLoading ? 'Refreshing...' : 'Refresh' }}
            </button>
          </div>
          <div v-if="manageDataLoading && !targets.length" class="notif-empty">Loading integrations...</div>
          <div v-else-if="!targets.length" class="notif-empty">No integrations configured.</div>
          <div v-else class="notif-stack">
            <article v-for="target in targets" :key="target.id" class="notif-card">
              <div>
                <strong>{{ target.name }}</strong>
                <div class="notif-card__sub">{{ target.type }} · {{ target.is_active ? 'active' : 'inactive' }}<span v-if="target.description"> · {{ target.description }}</span></div>
                <div v-if="Object.keys(target.config || {}).length" class="notif-card__template">Config: {{ JSON.stringify(target.config) }}</div>
              </div>
              <div class="notif-card__actions">
                <button class="base-btn base-btn--ghost base-btn--xs" @click="editTarget(target)">Edit</button>
                <button class="base-btn base-btn--ghost base-btn--xs" @click="testTarget(target.id)">Test</button>
                <button class="base-btn base-btn--ghost base-btn--xs notif-btn-del" @click="deleteTarget(target.id)">Delete</button>
              </div>
            </article>
          </div>
        </section>
      </div>
    </template>

    <template v-if="canManage && activeTab === 'rules'">
      <div class="notif-grid">
        <section class="notif-panel">
          <div class="notif-panel__head">
            <div>
              <h2>{{ editingRuleId ? 'Edit Rule' : 'New Rule' }}</h2>
              <p>{{ editingRuleId ? 'Adjust match conditions, templates, and active state for this route.' : 'Match event types to integrations.' }}</p>
            </div>
            <button v-if="editingRuleId" class="base-btn base-btn--ghost base-btn--sm" @click="resetRuleForm">Cancel</button>
          </div>
          <div class="notif-form">
            <input v-model="ruleForm.name" class="base-input" placeholder="Rule name" />
            <input v-model="ruleForm.event_type" class="base-input" list="notif-event-types" placeholder="Event type, for example approval_request.created or *" />
            <datalist id="notif-event-types">
              <option v-for="eventType in eventTypeSuggestions" :key="eventType" :value="eventType">{{ eventType }}</option>
            </datalist>
            <select v-model="ruleForm.severity" class="base-select">
              <option value="">Any severity</option>
              <option value="info">Info</option>
              <option value="success">Success</option>
              <option value="warning">Warning</option>
              <option value="error">Error</option>
            </select>
            <select v-model="ruleForm.entity_type" class="base-select">
              <option value="">Any entity type</option>
              <option value="approval_request">Approval Request</option>
              <option value="data_change_plan">Data Script Request</option>
              <option value="backup_download_request">Backup Request</option>
              <option value="schedule">Schedule</option>
            </select>
            <select v-model="ruleForm.target_id" class="base-select">
              <option :value="null">Select integration</option>
              <option v-for="target in ruleTargetOptions" :key="target.id" :value="target.id">{{ target.name }} · {{ target.type }}{{ target.is_active ? '' : ' · inactive' }}</option>
            </select>
            <select v-model.number="ruleForm.connection_id" class="base-select">
              <option :value="0">All connections</option>
              <option v-for="connection in connections" :key="connection.id" :value="connection.id">
                {{ connection.name }} · {{ connection.driver }}{{ connection.environment ? ` · ${connection.environment}` : '' }}
              </option>
            </select>
            <input v-model.number="ruleForm.actor_user_id" class="base-input" type="number" min="0" placeholder="Actor user ID filter, 0 for any" />
            <input v-model="ruleForm.title_template" class="base-input" placeholder="Optional title template, for example Alert: {{title}}" />
            <textarea v-model="ruleForm.message_template" class="base-textarea" rows="4" placeholder="Optional message template, for example Request {{entity_id}} on connection {{connection_id}} is {{severity}}"></textarea>
            <input v-model="ruleForm.description" class="base-input" placeholder="Optional description" />
            <div class="notif-hint" v-pre>
              Available placeholders: {{title}}, {{message}}, {{event_type}}, {{severity}}, {{entity_id}}, {{connection_id}}, {{payload.status}}, {{payload.note}}
            </div>
            <label class="notif-check">
              <input v-model="ruleForm.is_active" type="checkbox" />
              Rule active
            </label>
            <div class="notif-form__actions">
              <button class="base-btn base-btn--primary base-btn--sm" :disabled="savingRule" @click="submitRule">
                {{ savingRule ? (editingRuleId ? 'Saving...' : 'Creating...') : (editingRuleId ? 'Save Rule' : 'Create Rule') }}
              </button>
              <button v-if="editingRuleId" class="base-btn base-btn--ghost base-btn--sm" :disabled="savingRule" @click="resetRuleForm">Cancel</button>
            </div>
          </div>
        </section>

        <section class="notif-panel">
          <div class="notif-panel__head">
            <div>
              <h2>Active Rules</h2>
              <p>Current routing logic for outbound notifications.</p>
            </div>
            <button class="base-btn base-btn--ghost base-btn--sm" :disabled="manageDataLoading" @click="loadManageData">
              {{ manageDataLoading ? 'Refreshing...' : 'Refresh' }}
            </button>
          </div>
          <div v-if="manageDataLoading && !rules.length" class="notif-empty">Loading rules...</div>
          <div v-else-if="!rules.length" class="notif-empty">No rules configured.</div>
          <div v-else class="notif-stack">
            <article v-for="rule in rules" :key="rule.id" class="notif-card">
              <div>
                <strong>{{ rule.name }}</strong>
                <div class="notif-card__sub">
                  {{ rule.event_type }} · {{ rule.severity || 'any severity' }} · {{ rule.entity_type || 'any entity' }} · {{ rule.connection_id ? `connection #${rule.connection_id}` : 'all connections' }} · {{ integrationLabel(rule.target_id) }} · {{ rule.is_active ? 'active' : 'inactive' }}
                </div>
                <div v-if="rule.title_template || rule.message_template" class="notif-card__template">
                  <div v-if="rule.title_template"><strong>Title:</strong> {{ rule.title_template }}</div>
                  <div v-if="rule.message_template"><strong>Message:</strong> {{ rule.message_template }}</div>
                </div>
              </div>
              <div class="notif-card__actions">
                <button class="base-btn base-btn--ghost base-btn--xs" @click="editRule(rule)">Edit</button>
                <button class="base-btn base-btn--ghost base-btn--xs notif-btn-del" @click="deleteRule(rule.id)">Delete</button>
              </div>
            </article>
          </div>
        </section>
      </div>
    </template>

    <template v-if="canManage && activeTab === 'deliveries'">
      <div class="notif-grid">
        <section class="notif-panel">
          <div class="notif-panel__head">
            <div>
              <h2>Delivery Logs</h2>
              <p>Outbound send attempts and retry state.</p>
            </div>
            <button class="base-btn base-btn--ghost base-btn--sm" :disabled="deliveriesLoading" @click="loadDeliveries">
              {{ deliveriesLoading ? 'Loading...' : 'Retry' }}
            </button>
          </div>
          <div v-if="deliveriesError" class="notif-error">
            <strong>Delivery logs unavailable.</strong>
            <span>{{ deliveriesError }}</span>
          </div>
          <div v-else-if="deliveriesLoading && !deliveries.length" class="notif-empty">Loading delivery logs...</div>
          <div v-else-if="!deliveries.length" class="notif-empty">No deliveries yet.</div>
          <div v-else class="notif-stack">
            <article v-for="delivery in deliveries" :key="delivery.id" class="notif-card notif-card--log">
              <div>
                <strong>{{ delivery.target_name || delivery.channel }}</strong>
                <div class="notif-card__sub">{{ delivery.event?.event_type }} · {{ delivery.status }} · {{ fmtTime(delivery.updated_at) }}</div>
                <div class="notif-card__body">{{ delivery.event?.title }}</div>
                <div v-if="delivery.last_error" class="notif-card__error">{{ delivery.last_error }}</div>
              </div>
            </article>
          </div>
        </section>

        <section class="notif-panel">
          <div class="notif-panel__head">
            <div>
              <h2>Recent Events</h2>
              <p>Event stream emitted by approval, script, backup, and scheduler flows.</p>
            </div>
          </div>
          <div v-if="!events.length" class="notif-empty">No events recorded yet.</div>
          <div v-else class="notif-stack">
            <article v-for="event in events" :key="event.id" class="notif-card notif-card--log">
              <div>
                <strong>{{ event.title }}</strong>
                <div class="notif-card__sub">{{ event.event_type }} · {{ event.severity }} · {{ fmtTime(event.created_at) }}</div>
                <div class="notif-card__body">{{ event.message }}</div>
              </div>
            </article>
          </div>
        </section>
      </div>
    </template>
  </section>
</template>

<style scoped>
.notif-page {
  display: flex;
  flex-direction: column;
  gap: 18px;
  padding: 18px;
}

.notif-hero {
  display: flex;
  justify-content: space-between;
  gap: 18px;
  padding: 22px;
  border: 1px solid var(--border);
  border-radius: 20px;
  background:
    radial-gradient(circle at top right, rgba(79, 156, 249, 0.18), transparent 32%),
    linear-gradient(135deg, var(--bg-surface), var(--bg-surface-hover));
}

.notif-hero h1 {
  margin: 4px 0 8px;
  font-size: 28px;
  line-height: 1.1;
}

.notif-hero p,
.notif-panel__head p,
.notif-empty,
.notif-card__sub {
  color: var(--text-secondary);
}

.notif-eyebrow {
  font-size: 12px;
  text-transform: uppercase;
  letter-spacing: 0.14em;
  color: var(--brand);
  font-weight: 700;
}

.notif-hero__stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
  gap: 10px;
  min-width: 280px;
}

.notif-stat,
.notif-panel,
.notif-card,
.notif-item {
  border: 1px solid var(--border);
  background: var(--bg-surface);
}

.notif-stat {
  padding: 14px;
  border-radius: 16px;
}

.notif-stat strong {
  display: block;
  margin-top: 8px;
  font-size: 26px;
}

.notif-stat__label {
  font-size: 12px;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--text-muted);
}

.notif-tabs {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.notif-tab {
  border: 1px solid var(--border);
  background: var(--bg-surface);
  color: var(--text-secondary);
  padding: 10px 14px;
  border-radius: 999px;
  cursor: pointer;
}

.notif-tab--active {
  color: var(--brand);
  border-color: var(--brand-ring);
  background: var(--brand-dim);
}

.notif-panel {
  border-radius: 18px;
  padding: 18px;
}

.notif-panel__head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: center;
  margin-bottom: 14px;
}

.notif-panel__head h2 {
  margin: 0 0 4px;
  font-size: 18px;
}

.notif-list,
.notif-stack {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.notif-item {
  border-radius: 16px;
  padding: 14px 16px;
}

.notif-item--read {
  opacity: 0.72;
}

.notif-item--info {
  border-left: 4px solid #4f9cf9;
}

.notif-item--success {
  border-left: 4px solid #27ae60;
}

.notif-item--warning {
  border-left: 4px solid #f39c12;
}

.notif-item--error {
  border-left: 4px solid #e74c3c;
}

.notif-item__meta {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  font-size: 12px;
  color: var(--text-muted);
  margin-bottom: 8px;
}

.notif-item h3 {
  margin: 0 0 6px;
  font-size: 16px;
}

.notif-item p,
.notif-card__body {
  margin: 0;
  color: var(--text-secondary);
}

.notif-pill,
.notif-link {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.notif-pill {
  padding: 3px 8px;
  border-radius: 999px;
  background: var(--bg-surface-hover);
}

.notif-link {
  margin-top: 10px;
  color: var(--brand);
  text-decoration: none;
  font-size: 13px;
}

.notif-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.notif-form {
  display: grid;
  gap: 10px;
}

.notif-form__actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 4px;
}

.notif-card__actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
  align-items: flex-start;
  min-width: 196px;
}

.notif-hint,
.notif-card__template {
  font-size: 12px;
  color: var(--text-muted);
}

.notif-check {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--text-secondary);
}

.notif-card__template {
  margin-top: 8px;
  display: grid;
  gap: 4px;
}

.notif-card {
  border-radius: 14px;
  padding: 12px 14px;
  display: flex;
  justify-content: space-between;
  gap: 12px;
}

.notif-card--log {
  display: block;
}

.notif-card__error {
  margin-top: 8px;
  color: #e74c3c;
  font-size: 12px;
}

.notif-btn-del {
  color: var(--danger) !important;
}

.notif-btn-del:hover {
  background: rgba(239, 68, 68, 0.1) !important;
}

.notif-empty {
  padding: 20px 0;
}

.notif-error {
  display: grid;
  gap: 6px;
  padding: 14px 16px;
  border: 1px solid rgba(231, 76, 60, 0.24);
  border-radius: 14px;
  background: rgba(231, 76, 60, 0.08);
  color: #c0392b;
}

@media (max-width: 960px) {
  .notif-hero,
  .notif-grid,
  .notif-panel__head {
    grid-template-columns: 1fr;
    flex-direction: column;
    align-items: stretch;
  }

  .notif-hero__stats {
    min-width: 0;
  }

  .notif-form__actions :deep(.base-btn),
  .notif-card__actions :deep(.base-btn),
  .notif-panel__head :deep(.base-btn) {
    width: 100%;
  }

  .notif-card__actions {
    min-width: 0;
    justify-content: stretch;
  }
}
</style>
