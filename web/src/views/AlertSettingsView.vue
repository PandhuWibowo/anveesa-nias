<script setup lang="ts">
import { onMounted, ref } from 'vue'
import axios from 'axios'
import { useToast } from '@/composables/useToast'

interface TelegramTarget {
  name: string
  bot_token: string
  chat_id: string
  topic_id: string
  disable_notification: boolean
}

interface TelegramConfig {
  enabled: boolean
  targets: TelegramTarget[]
}

interface DiscordTarget {
  name: string
  webhook_url: string
  username: string
  avatar_url: string
  footer_text: string
}

interface DiscordConfig {
  enabled: boolean
  targets: DiscordTarget[]
}

interface SlackTarget {
  name: string
  webhook_url: string
  channel: string
  username: string
  icon_emoji: string
}

interface SlackConfig {
  enabled: boolean
  targets: SlackTarget[]
}

interface WebhookConfig {
  enabled: boolean
  url: string
  auth_header: string
}

interface AlertSettings {
  telegram: TelegramConfig
  discord: DiscordConfig
  slack: SlackConfig
  webhook: WebhookConfig
}

const toast = useToast()

const loading = ref(false)
const saving = ref(false)
const testing = ref<string | null>(null)
const creatingTopic = ref<number | null>(null)
const newTopicName = ref<Record<number, string>>({})
const newTopicColor = ref<Record<number, number>>({})

const telegramTopicColors = [
  { value: 7322096,  label: 'Blue',   hex: '#6fb9f0' },
  { value: 16766590, label: 'Yellow', hex: '#ffd67e' },
  { value: 13338331, label: 'Green',  hex: '#cb86db' },
  { value: 9367192,  label: 'Purple', hex: '#8eee98' },
  { value: 16749490, label: 'Red',    hex: '#ff93b2' },
  { value: 16478047, label: 'Orange', hex: '#fb6f5f' },
]

const settings = ref<AlertSettings>({
  telegram: { enabled: false, targets: [] },
  discord: { enabled: false, targets: [] },
  slack: { enabled: false, targets: [] },
  webhook: { enabled: false, url: '', auth_header: '' },
})

function makeTelegramTarget(): TelegramTarget {
  return { name: '', bot_token: '', chat_id: '', topic_id: '', disable_notification: false }
}

function makeDiscordTarget(): DiscordTarget {
  return { name: '', webhook_url: '', username: '', avatar_url: '', footer_text: '' }
}

function makeSlackTarget(): SlackTarget {
  return { name: '', webhook_url: '', channel: '', username: '', icon_emoji: '' }
}

function addTelegramTarget() { settings.value.telegram.targets.push(makeTelegramTarget()) }
function removeTelegramTarget(i: number) { settings.value.telegram.targets.splice(i, 1) }

function addDiscordTarget() { settings.value.discord.targets.push(makeDiscordTarget()) }
function removeDiscordTarget(i: number) { settings.value.discord.targets.splice(i, 1) }

function addSlackTarget() { settings.value.slack.targets.push(makeSlackTarget()) }
function removeSlackTarget(i: number) { settings.value.slack.targets.splice(i, 1) }

async function loadSettings() {
  loading.value = true
  try {
    const { data } = await axios.get<AlertSettings>('/api/settings/alerts')
    settings.value = {
      telegram: {
        enabled: Boolean(data?.telegram?.enabled),
        targets: (data?.telegram?.targets || []).map(t => ({
          name: t.name || '',
          bot_token: t.bot_token || '',
          chat_id: t.chat_id || '',
          topic_id: t.topic_id || '',
          disable_notification: Boolean(t.disable_notification),
        })),
      },
      discord: {
        enabled: Boolean(data?.discord?.enabled),
        targets: (data?.discord?.targets || []).map(t => ({
          name: t.name || '',
          webhook_url: t.webhook_url || '',
          username: t.username || '',
          avatar_url: t.avatar_url || '',
          footer_text: t.footer_text || '',
        })),
      },
      slack: {
        enabled: Boolean(data?.slack?.enabled),
        targets: (data?.slack?.targets || []).map(t => ({
          name: t.name || '',
          webhook_url: t.webhook_url || '',
          channel: t.channel || '',
          username: t.username || '',
          icon_emoji: t.icon_emoji || '',
        })),
      },
      webhook: {
        enabled: Boolean(data?.webhook?.enabled),
        url: data?.webhook?.url || '',
        auth_header: data?.webhook?.auth_header || '',
      },
    }
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to load alert settings')
  } finally {
    loading.value = false
  }
}

async function saveSettings() {
  saving.value = true
  try {
    await axios.post('/api/settings/alerts', settings.value)
    toast.success('Alert settings saved')
    await loadSettings()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to save alert settings')
  } finally {
    saving.value = false
  }
}

async function createTelegramTopic(targetIndex: number) {
  const name = (newTopicName.value[targetIndex] || '').trim()
  if (!name) { toast.error('Enter a topic name'); return }
  const target = settings.value.telegram.targets[targetIndex]
  if (!target.chat_id.trim()) { toast.error('Fill in the Chat ID first'); return }
  creatingTopic.value = targetIndex
  try {
    const { data } = await axios.post<{ topic_id: string; name: string }>('/api/settings/alerts/telegram/create-topic', {
      bot_token: target.bot_token,
      chat_id: target.chat_id,
      topic_name: name,
      icon_color: newTopicColor.value[targetIndex] || 0,
    })
    settings.value.telegram.targets[targetIndex].topic_id = data.topic_id
    newTopicName.value[targetIndex] = ''
    toast.success(`Topic "${data.name}" created (ID: ${data.topic_id}) — saving settings…`)
    await saveSettings()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to create topic')
  } finally {
    creatingTopic.value = null
  }
}

async function testChannel(channel: string, targetIndex: number = 0) {
  const key = `${channel}-${targetIndex}`
  testing.value = key
  try {
    await axios.post('/api/settings/alerts/test', { channel, target_index: targetIndex })
    toast.success(`Test message sent`)
  } catch (e: any) {
    toast.error(e?.response?.data?.error || `Failed to send test`)
  } finally {
    testing.value = null
  }
}

onMounted(loadSettings)
</script>

<template>
  <div class="page-shell ast-root">
    <div class="page-scroll">
      <div class="page-stack">

        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Settings</div>
            <div class="page-title">Alert Settings</div>
            <div class="page-subtitle">Configure global alert channels. Each channel supports multiple targets — send alerts to several chat IDs, channel webhooks, or topics simultaneously.</div>
          </div>
          <div class="page-hero__actions">
            <button class="base-btn base-btn--ghost base-btn--sm" :disabled="loading" @click="loadSettings">
              {{ loading ? 'Refreshing…' : 'Refresh' }}
            </button>
            <button class="base-btn base-btn--primary base-btn--sm" :disabled="saving" @click="saveSettings">
              {{ saving ? 'Saving…' : 'Save All' }}
            </button>
          </div>
        </section>

        <!-- ── Telegram ────────────────────────────────────────────────── -->
        <section class="page-panel ast-panel">
          <div class="ast-panel__head">
            <div class="ast-panel__title-row">
              <div class="ast-channel-icon ast-channel-icon--telegram">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M22 2L11 13"/><path d="M22 2L15 22 11 13 2 9l20-7z"/>
                </svg>
              </div>
              <div>
                <div class="ast-panel__title">Telegram</div>
                <div class="ast-panel__sub">Multiple targets, each with its own bot token, chat ID, and optional topic.</div>
              </div>
            </div>
            <label class="ast-toggle">
              <input type="checkbox" v-model="settings.telegram.enabled" />
              <span class="ast-toggle__track"></span>
              <span class="ast-toggle__label">{{ settings.telegram.enabled ? 'Enabled' : 'Disabled' }}</span>
            </label>
          </div>

          <div :class="{ 'ast-section--dimmed': !settings.telegram.enabled }">
            <div class="ast-targets-header">
              <span class="ast-targets-label">Chat / Channel Targets <span class="ast-count">{{ settings.telegram.targets.length }}</span></span>
              <button class="base-btn base-btn--ghost base-btn--xs" :disabled="!settings.telegram.enabled" @click="addTelegramTarget">
                + Add Target
              </button>
            </div>

            <div v-if="settings.telegram.targets.length === 0" class="ast-empty">
              No targets yet. Add a chat ID, channel ID, or group ID.
            </div>

            <div v-for="(target, i) in settings.telegram.targets" :key="i" class="ast-target-card">
              <div class="ast-target-card__head">
                <span class="ast-target-num">Target {{ i + 1 }}</span>
                <button class="ast-remove-btn" :disabled="!settings.telegram.enabled" @click="removeTelegramTarget(i)" title="Remove">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                </button>
              </div>

              <div class="ast-target-grid">
                <label class="ast-field">
                  <span class="ast-label">Label <span class="ast-optional">optional</span></span>
                  <input v-model="target.name" class="base-input" type="text" placeholder="e.g. Ops Alerts" :disabled="!settings.telegram.enabled" />
                </label>

                <label class="ast-field">
                  <span class="ast-label">Bot Token</span>
                  <input v-model="target.bot_token" class="base-input" type="password" placeholder="1234567890:AAExxxxxxxxxxxxxx" autocomplete="off" :disabled="!settings.telegram.enabled" />
                  <span class="ast-hint">Token from @BotFather. Leave blank to keep the current value.</span>
                </label>

                <label class="ast-field">
                  <span class="ast-label">Chat ID</span>
                  <input v-model="target.chat_id" class="base-input" type="text" placeholder="-1001234567890" :disabled="!settings.telegram.enabled" />
                  <span class="ast-hint">Negative for groups/channels, positive for private chats.</span>
                </label>

                <label class="ast-field">
                  <span class="ast-label">Topic / Thread ID <span class="ast-optional">optional</span></span>
                  <input v-model="target.topic_id" class="base-input" type="text" placeholder="5" :disabled="!settings.telegram.enabled" />
                  <span class="ast-hint">For supergroups with forum topics enabled. Leave blank to post to the general topic.</span>
                </label>

                <!-- Create topic inline -->
                <div class="ast-create-topic" v-if="settings.telegram.enabled">
                  <div class="ast-create-topic__label">Create a new topic in this chat</div>
                  <div class="ast-create-topic__row">
                    <input
                      v-model="newTopicName[i]"
                      class="base-input ast-create-topic__input"
                      type="text"
                      placeholder="Topic name…"
                    />
                    <div class="ast-color-swatches">
                      <button
                        v-for="c in telegramTopicColors"
                        :key="c.value"
                        class="ast-swatch"
                        :class="{ 'ast-swatch--active': newTopicColor[i] === c.value }"
                        :style="{ background: c.hex }"
                        :title="c.label"
                        type="button"
                        @click="newTopicColor[i] = newTopicColor[i] === c.value ? 0 : c.value"
                      ></button>
                    </div>
                    <button
                      class="base-btn base-btn--ghost base-btn--xs"
                      :disabled="creatingTopic !== null"
                      @click="createTelegramTopic(i)"
                    >
                      {{ creatingTopic === i ? 'Creating…' : 'Create Topic' }}
                    </button>
                  </div>
                  <span class="ast-hint">Requires the bot to be an admin with <strong>Manage Topics</strong> permission. The new Topic ID is filled in automatically.</span>
                </div>

                <label class="ast-field ast-field--inline">
                  <input type="checkbox" v-model="target.disable_notification" :disabled="!settings.telegram.enabled" />
                  <span class="ast-label">Silent (disable notification sound)</span>
                </label>
              </div>

              <div class="ast-target-actions">
                <button
                  class="base-btn base-btn--ghost base-btn--xs"
                  :disabled="!settings.telegram.enabled || testing !== null"
                  @click="testChannel('telegram', i)"
                >
                  {{ testing === `telegram-${i}` ? 'Sending…' : 'Test this target' }}
                </button>
              </div>
            </div>
          </div>
        </section>

        <!-- ── Discord ────────────────────────────────────────────────── -->
        <section class="page-panel ast-panel">
          <div class="ast-panel__head">
            <div class="ast-panel__title-row">
              <div class="ast-channel-icon ast-channel-icon--discord">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M9 12c0 .6-.4 1-1 1s-1-.4-1-1 .4-1 1-1 1 .4 1 1z"/><path d="M15 12c0 .6-.4 1-1 1s-1-.4-1-1 .4-1 1-1 1 .4 1 1z"/>
                  <path d="M8.5 3C6 3.5 4 5 3 7c-1.3 4-1 8.5 1.5 11.5C5.6 20 7.5 21 9.5 21l1-2c-.9-.3-1.7-.8-2.5-1.4"/>
                  <path d="M15.5 3C18 3.5 20 5 21 7c1.3 4 1 8.5-1.5 11.5-1.1 1.5-3 2.5-5 2.5l-1-2c.9-.3 1.7-.8 2.5-1.4"/>
                  <path d="M8.5 18c1 .5 2.3.8 3.5.8s2.5-.3 3.5-.8"/>
                </svg>
              </div>
              <div>
                <div class="ast-panel__title">Discord</div>
                <div class="ast-panel__sub">Multiple channel targets, each with its own webhook URL.</div>
              </div>
            </div>
            <label class="ast-toggle">
              <input type="checkbox" v-model="settings.discord.enabled" />
              <span class="ast-toggle__track"></span>
              <span class="ast-toggle__label">{{ settings.discord.enabled ? 'Enabled' : 'Disabled' }}</span>
            </label>
          </div>

          <div :class="{ 'ast-section--dimmed': !settings.discord.enabled }">
            <div class="ast-targets-header">
              <span class="ast-targets-label">Channel Targets <span class="ast-count">{{ settings.discord.targets.length }}</span></span>
              <button class="base-btn base-btn--ghost base-btn--xs" :disabled="!settings.discord.enabled" @click="addDiscordTarget">
                + Add Target
              </button>
            </div>

            <div v-if="settings.discord.targets.length === 0" class="ast-empty">
              No targets yet. Add a Discord channel webhook.
            </div>

            <div v-for="(target, i) in settings.discord.targets" :key="i" class="ast-target-card">
              <div class="ast-target-card__head">
                <span class="ast-target-num">Target {{ i + 1 }}</span>
                <button class="ast-remove-btn" :disabled="!settings.discord.enabled" @click="removeDiscordTarget(i)" title="Remove">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                </button>
              </div>

              <div class="ast-target-grid">
                <label class="ast-field">
                  <span class="ast-label">Label <span class="ast-optional">optional</span></span>
                  <input v-model="target.name" class="base-input" type="text" placeholder="e.g. #ops-alerts" :disabled="!settings.discord.enabled" />
                </label>

                <label class="ast-field">
                  <span class="ast-label">Webhook URL</span>
                  <input v-model="target.webhook_url" class="base-input" type="password" placeholder="https://discord.com/api/webhooks/…" autocomplete="off" :disabled="!settings.discord.enabled" />
                  <span class="ast-hint">Server Settings → Integrations → Webhooks. Leave blank to keep the current URL.</span>
                </label>

                <div class="ast-row">
                  <label class="ast-field">
                    <span class="ast-label">Bot Username <span class="ast-optional">optional</span></span>
                    <input v-model="target.username" class="base-input" type="text" placeholder="Anveesa Alerts" :disabled="!settings.discord.enabled" />
                  </label>
                  <label class="ast-field">
                    <span class="ast-label">Avatar URL <span class="ast-optional">optional</span></span>
                    <input v-model="target.avatar_url" class="base-input" type="text" placeholder="https://…/avatar.png" :disabled="!settings.discord.enabled" />
                  </label>
                </div>

                <label class="ast-field">
                  <span class="ast-label">Footer Text <span class="ast-optional">optional</span></span>
                  <input v-model="target.footer_text" class="base-input" type="text" placeholder="Anveesa Nias" :disabled="!settings.discord.enabled" />
                </label>
              </div>

              <div class="ast-target-actions">
                <button
                  class="base-btn base-btn--ghost base-btn--xs"
                  :disabled="!settings.discord.enabled || testing !== null"
                  @click="testChannel('discord', i)"
                >
                  {{ testing === `discord-${i}` ? 'Sending…' : 'Test this target' }}
                </button>
              </div>
            </div>
          </div>
        </section>

        <!-- ── Slack ──────────────────────────────────────────────────── -->
        <section class="page-panel ast-panel">
          <div class="ast-panel__head">
            <div class="ast-panel__title-row">
              <div class="ast-channel-icon ast-channel-icon--slack">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <rect x="13" y="2" width="3" height="8" rx="1.5"/><path d="M19 8.5V10h1.5A1.5 1.5 0 1 0 19 8.5"/>
                  <rect x="8" y="14" width="3" height="8" rx="1.5"/><path d="M5 15.5V14H3.5A1.5 1.5 0 1 0 5 15.5"/>
                  <rect x="14" y="13" width="8" height="3" rx="1.5"/><path d="M15.5 19H14v1.5a1.5 1.5 0 1 0 1.5-1.5"/>
                  <rect x="2" y="8" width="8" height="3" rx="1.5"/><path d="M8.5 5H10V3.5A1.5 1.5 0 1 0 8.5 5"/>
                </svg>
              </div>
              <div>
                <div class="ast-panel__title">Slack</div>
                <div class="ast-panel__sub">Multiple channel targets, each with its own webhook URL.</div>
              </div>
            </div>
            <label class="ast-toggle">
              <input type="checkbox" v-model="settings.slack.enabled" />
              <span class="ast-toggle__track"></span>
              <span class="ast-toggle__label">{{ settings.slack.enabled ? 'Enabled' : 'Disabled' }}</span>
            </label>
          </div>

          <div :class="{ 'ast-section--dimmed': !settings.slack.enabled }">
            <div class="ast-targets-header">
              <span class="ast-targets-label">Channel Targets <span class="ast-count">{{ settings.slack.targets.length }}</span></span>
              <button class="base-btn base-btn--ghost base-btn--xs" :disabled="!settings.slack.enabled" @click="addSlackTarget">
                + Add Target
              </button>
            </div>

            <div v-if="settings.slack.targets.length === 0" class="ast-empty">
              No targets yet. Add a Slack channel webhook.
            </div>

            <div v-for="(target, i) in settings.slack.targets" :key="i" class="ast-target-card">
              <div class="ast-target-card__head">
                <span class="ast-target-num">Target {{ i + 1 }}</span>
                <button class="ast-remove-btn" :disabled="!settings.slack.enabled" @click="removeSlackTarget(i)" title="Remove">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                </button>
              </div>

              <div class="ast-target-grid">
                <label class="ast-field">
                  <span class="ast-label">Label <span class="ast-optional">optional</span></span>
                  <input v-model="target.name" class="base-input" type="text" placeholder="e.g. #dev-alerts" :disabled="!settings.slack.enabled" />
                </label>

                <label class="ast-field">
                  <span class="ast-label">Webhook URL</span>
                  <input v-model="target.webhook_url" class="base-input" type="password" placeholder="https://hooks.slack.com/services/…" autocomplete="off" :disabled="!settings.slack.enabled" />
                  <span class="ast-hint">Slack App → Incoming Webhooks. Leave blank to keep the current URL.</span>
                </label>

                <div class="ast-row">
                  <label class="ast-field">
                    <span class="ast-label">Channel <span class="ast-optional">optional</span></span>
                    <input v-model="target.channel" class="base-input" type="text" placeholder="#alerts" :disabled="!settings.slack.enabled" />
                  </label>
                  <label class="ast-field">
                    <span class="ast-label">Bot Username <span class="ast-optional">optional</span></span>
                    <input v-model="target.username" class="base-input" type="text" placeholder="Anveesa Alerts" :disabled="!settings.slack.enabled" />
                  </label>
                </div>

                <label class="ast-field">
                  <span class="ast-label">Icon Emoji <span class="ast-optional">optional</span></span>
                  <input v-model="target.icon_emoji" class="base-input" type="text" placeholder=":bell:" :disabled="!settings.slack.enabled" />
                </label>
              </div>

              <div class="ast-target-actions">
                <button
                  class="base-btn base-btn--ghost base-btn--xs"
                  :disabled="!settings.slack.enabled || testing !== null"
                  @click="testChannel('slack', i)"
                >
                  {{ testing === `slack-${i}` ? 'Sending…' : 'Test this target' }}
                </button>
              </div>
            </div>
          </div>
        </section>

        <!-- ── Custom Webhook ─────────────────────────────────────────── -->
        <section class="page-panel ast-panel">
          <div class="ast-panel__head">
            <div class="ast-panel__title-row">
              <div class="ast-channel-icon ast-channel-icon--webhook">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/>
                  <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>
                </svg>
              </div>
              <div>
                <div class="ast-panel__title">Custom Webhook</div>
                <div class="ast-panel__sub">POST alert payloads as JSON to any HTTP endpoint.</div>
              </div>
            </div>
            <label class="ast-toggle">
              <input type="checkbox" v-model="settings.webhook.enabled" />
              <span class="ast-toggle__track"></span>
              <span class="ast-toggle__label">{{ settings.webhook.enabled ? 'Enabled' : 'Disabled' }}</span>
            </label>
          </div>

          <div class="ast-grid" :class="{ 'ast-section--dimmed': !settings.webhook.enabled }">
            <label class="ast-field">
              <span class="ast-label">Endpoint URL</span>
              <input v-model="settings.webhook.url" class="base-input" type="password" placeholder="https://your-service.example.com/hooks/alerts" autocomplete="off" :disabled="!settings.webhook.enabled" />
              <span class="ast-hint">A POST with a JSON body will be sent to this URL. Leave blank to keep the current URL.</span>
            </label>

            <label class="ast-field">
              <span class="ast-label">Authorization Header <span class="ast-optional">optional</span></span>
              <input v-model="settings.webhook.auth_header" class="base-input" type="password" placeholder="Bearer your-secret-token" autocomplete="off" :disabled="!settings.webhook.enabled" />
              <span class="ast-hint">Sent as the <code>Authorization</code> header. Leave blank to keep the current value.</span>
            </label>
          </div>

          <div class="ast-actions">
            <button
              class="base-btn base-btn--ghost base-btn--sm"
              :disabled="!settings.webhook.enabled || testing !== null"
              @click="testChannel('webhook')"
            >
              {{ testing === 'webhook-0' ? 'Sending…' : 'Send Test Message' }}
            </button>
          </div>
        </section>

      </div>
    </div>
  </div>
</template>

<style scoped>
.ast-root {
  background: var(--bg-body);
}

.page-scroll {
  padding: 16px 20px 24px;
}

.ast-panel {
  display: grid;
  gap: 18px;
  padding: 20px 24px;
}

.ast-panel__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.ast-panel__title-row {
  display: flex;
  align-items: center;
  gap: 12px;
}

.ast-panel__title {
  font-size: 15px;
  font-weight: 700;
  color: var(--text-primary);
}

.ast-panel__sub {
  font-size: 12px;
  color: var(--text-muted);
  margin-top: 2px;
}

.ast-channel-icon {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.ast-channel-icon--telegram { background: rgba(41, 182, 246, 0.12); color: #29b6f6; }
.ast-channel-icon--discord  { background: rgba(88, 101, 242, 0.12); color: #5865f2; }
.ast-channel-icon--slack    { background: rgba(224, 30, 90, 0.12);  color: #e01e5a; }
.ast-channel-icon--webhook  { background: rgba(99, 102, 241, 0.12); color: var(--brand); }

/* Toggle */
.ast-toggle {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  flex-shrink: 0;
}

.ast-toggle input {
  position: absolute;
  opacity: 0;
  width: 0;
  height: 0;
}

.ast-toggle__track {
  position: relative;
  display: inline-block;
  width: 34px;
  height: 18px;
  border-radius: 999px;
  background: var(--border);
  transition: background 0.15s;
  flex-shrink: 0;
}

.ast-toggle__track::after {
  content: '';
  position: absolute;
  top: 2px;
  left: 2px;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: white;
  transition: transform 0.15s;
}

.ast-toggle input:checked ~ .ast-toggle__track { background: var(--brand); }
.ast-toggle input:checked ~ .ast-toggle__track::after { transform: translateX(16px); }

.ast-toggle__label {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-secondary);
  min-width: 54px;
}

/* Section dimming */
.ast-section--dimmed {
  opacity: 0.4;
  pointer-events: none;
}

/* Targets header */
.ast-targets-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 0 6px;
  border-top: 1px solid var(--border);
}

.ast-targets-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-secondary);
  display: flex;
  align-items: center;
  gap: 6px;
}

.ast-count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  border-radius: 999px;
  background: var(--brand-dim);
  color: var(--brand);
  font-size: 11px;
  font-weight: 700;
}

/* Empty state */
.ast-empty {
  padding: 16px;
  text-align: center;
  font-size: 12px;
  color: var(--text-muted);
  border: 1px dashed var(--border);
  border-radius: 10px;
}

/* Target card */
.ast-target-card {
  border: 1px solid var(--border);
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.02);
  overflow: hidden;
}

.ast-target-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  background: rgba(255, 255, 255, 0.03);
  border-bottom: 1px solid var(--border);
}

.ast-target-num {
  font-size: 11px;
  font-weight: 700;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.ast-remove-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border-radius: 6px;
  border: none;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}

.ast-remove-btn:hover:not(:disabled) {
  background: rgba(239, 68, 68, 0.12);
  color: #ef4444;
}

.ast-remove-btn:disabled { opacity: 0.4; cursor: not-allowed; }

.ast-target-grid {
  display: grid;
  gap: 12px;
  padding: 14px;
}

.ast-target-actions {
  display: flex;
  justify-content: flex-end;
  padding: 10px 14px;
  border-top: 1px solid var(--border);
}

/* Shared field/label/row styles */
.ast-grid {
  display: grid;
  gap: 14px;
}

.ast-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
}

.ast-field {
  display: grid;
  gap: 6px;
  padding: 14px 16px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.02);
}

.ast-field--inline {
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
}

.ast-field--inline input[type="checkbox"] {
  width: 15px;
  height: 15px;
  flex-shrink: 0;
  cursor: pointer;
  accent-color: var(--brand);
}

.ast-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-secondary);
  display: flex;
  gap: 6px;
  align-items: center;
  flex-wrap: wrap;
}

.ast-badge {
  padding: 2px 7px;
  border-radius: 999px;
  border: 1px solid var(--brand-ring);
  background: var(--brand-dim);
  color: var(--brand);
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.07em;
}

.ast-optional {
  font-weight: 400;
  color: var(--text-muted);
  font-size: 11px;
}

.ast-hint {
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.5;
}

.ast-hint code {
  font-family: monospace;
  background: rgba(255, 255, 255, 0.06);
  padding: 1px 4px;
  border-radius: 4px;
  font-size: 11px;
}

/* Create Topic */
.ast-create-topic {
  display: grid;
  gap: 8px;
  padding: 12px 14px;
  border: 1px dashed var(--border);
  border-radius: 10px;
  background: rgba(255, 255, 255, 0.01);
}

.ast-create-topic__label {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.ast-create-topic__row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.ast-create-topic__input {
  flex: 1;
  min-width: 140px;
}

.ast-color-swatches {
  display: flex;
  gap: 5px;
  flex-shrink: 0;
}

.ast-swatch {
  width: 18px;
  height: 18px;
  border-radius: 50%;
  border: 2px solid transparent;
  cursor: pointer;
  transition: transform 0.1s, border-color 0.1s;
  flex-shrink: 0;
}

.ast-swatch:hover { transform: scale(1.2); }
.ast-swatch--active { border-color: var(--text-primary); transform: scale(1.15); }

.ast-actions {
  display: flex;
  justify-content: flex-end;
  padding-top: 2px;
  border-top: 1px solid var(--border);
}

@media (max-width: 960px) {
  .page-scroll { padding: 12px 14px 20px; }
  .ast-panel { padding: 16px; }
  .ast-panel__head { flex-direction: column; align-items: flex-start; }
  .ast-row { grid-template-columns: 1fr; }
}
</style>
