<template>
  <div class="ai-root">
    <div class="ai-header">
      <span class="ai-title">AI Query Assistant</span>
      <div class="ai-quick-btns">
        <button class="ai-quick" @click="quickExplain" :disabled="!activeSql || loading" title="Explain active query">Explain</button>
        <button class="ai-quick" @click="quickFix" :disabled="!lastError || loading" title="Fix last error">Fix Error</button>
        <button class="ai-quick" @click="quickIndex" :disabled="!activeSql || loading" title="Suggest index">Index</button>
        <button class="ai-quick ai-quick--settings" @click="showSettings = !showSettings" title="AI settings">⚙</button>
      </div>
    </div>

    <!-- Settings panel -->
    <div class="ai-settings" v-if="showSettings">
      <div class="ai-settings-row">
        <label>API Key</label>
        <input v-model="settings.api_key" type="password" placeholder="sk-..." class="ai-input" />
      </div>
      <div class="ai-settings-row">
        <label>Base URL</label>
        <input v-model="settings.base_url" type="text" placeholder="https://api.openai.com/v1" class="ai-input" />
      </div>
      <div class="ai-settings-row">
        <label>Model</label>
        <input v-model="settings.model" type="text" placeholder="gpt-4o-mini" class="ai-input" />
      </div>
      <button class="ai-btn ai-btn--primary" @click="saveSettings">Save Settings</button>
    </div>

    <!-- Chat messages -->
    <div class="ai-messages" ref="messagesRef">
      <div v-for="(msg, i) in messages" :key="i" :class="['ai-msg', 'ai-msg--' + msg.role]">
        <div class="ai-msg-label">{{ msg.role === 'user' ? 'You' : 'AI' }}</div>
        <div class="ai-msg-content" v-html="renderMsg(msg.content)"></div>
        <button v-if="msg.role === 'assistant' && extractSQL(msg.content)"
          class="ai-copy-sql" @click="emit('insert-sql', extractSQL(msg.content)!)">
          Insert SQL →
        </button>
      </div>
      <div v-if="loading" class="ai-msg ai-msg--assistant">
        <div class="ai-msg-label">AI</div>
        <div class="ai-thinking">
          <span></span><span></span><span></span>
        </div>
      </div>
      <div v-if="messages.length === 0 && !loading" class="ai-welcome">
        Ask me anything about SQL — generate queries, explain plans, or fix errors.
      </div>
    </div>

    <!-- Input -->
    <div class="ai-input-row">
      <textarea v-model="input" class="ai-textarea" rows="2"
        placeholder="Ask AI… e.g. 'find the top 10 orders by total in the last 30 days'"
        @keydown.ctrl.enter.prevent="send"
        @keydown.meta.enter.prevent="send" />
      <button class="ai-send-btn" @click="send" :disabled="!input.trim() || loading">
        {{ loading ? '…' : '↵' }}
      </button>
    </div>
    <div class="ai-hint">Ctrl+Enter to send</div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, nextTick, onMounted } from 'vue'
import axios from 'axios'

const props = defineProps<{
  activeSql?: string
  lastError?: string
  schema?: string
  connectionInfo?: string
}>()

const emit = defineEmits<{
  'insert-sql': [sql: string]
}>()

interface Message { role: 'user' | 'assistant' | 'system'; content: string }

const messages = ref<Message[]>([])
const input = ref('')
const loading = ref(false)
const showSettings = ref(false)
const messagesRef = ref<HTMLElement>()

const settings = ref({ api_key: '', base_url: '', model: '' })

onMounted(async () => {
  try {
    const { data } = await axios.get('/api/ai/settings')
    settings.value = data
  } catch {}
})

async function saveSettings() {
  await axios.post('/api/ai/settings', settings.value)
  showSettings.value = false
}

function buildSystemPrompt(): string {
  let sys = `You are an expert SQL assistant embedded in Anveesa Nias, a database management tool.
When generating SQL, always wrap it in a \`\`\`sql code block.
Be concise and accurate.`
  if (props.connectionInfo) sys += `\nDatabase connection: ${props.connectionInfo}`
  if (props.schema) sys += `\n\nDatabase schema:\n${props.schema}`
  return sys
}

async function send() {
  const text = input.value.trim()
  if (!text || loading.value) return
  input.value = ''
  messages.value.push({ role: 'user', content: text })
  await chat()
}

async function chat(extraUserMsg?: string) {
  loading.value = true
  await nextTick()
  scrollToBottom()

  const contextMsgs: Message[] = [
    { role: 'system', content: buildSystemPrompt() },
    ...messages.value.slice(-20),
  ]
  if (extraUserMsg) {
    messages.value.push({ role: 'user', content: extraUserMsg })
    contextMsgs.push({ role: 'user', content: extraUserMsg })
  }

  try {
    const { data } = await axios.post('/api/ai/chat', { messages: contextMsgs })
    const content = data.choices?.[0]?.message?.content ?? data.error ?? 'No response'
    messages.value.push({ role: 'assistant', content })
  } catch (e: unknown) {
    const msg = (e as { response?: { data?: { error?: string } } }).response?.data?.error ?? String(e)
    messages.value.push({ role: 'assistant', content: `Error: ${msg}` })
  } finally {
    loading.value = false
    await nextTick()
    scrollToBottom()
  }
}

function quickExplain() {
  chat(`Explain what this SQL query does, step by step:\n\`\`\`sql\n${props.activeSql}\n\`\`\``)
}
function quickFix() {
  chat(`Fix this SQL error:\nQuery: \`\`\`sql\n${props.activeSql}\n\`\`\`\nError: ${props.lastError}`)
}
function quickIndex() {
  chat(`Suggest the most effective indexes for this query:\n\`\`\`sql\n${props.activeSql}\n\`\`\``)
}

function scrollToBottom() {
  if (messagesRef.value) messagesRef.value.scrollTop = messagesRef.value.scrollHeight
}

function escapeHtml(str: string): string {
  const htmlEscapes: Record<string, string> = {
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
  }
  return str.replace(/[&<>"']/g, c => htmlEscapes[c] || c)
}

function renderMsg(content: string) {
  // First, escape all HTML to prevent XSS
  let safe = escapeHtml(content)
  
  // Process markdown-like code blocks (the content is already escaped)
  // Only allow our specific class attributes, no other HTML
  safe = safe
    .replace(/```sql\n?([\s\S]*?)```/g, '<pre class="ai-code ai-code--sql">$1</pre>')
    .replace(/```(?:[\w]*\n)?([\s\S]*?)```/g, '<pre class="ai-code">$1</pre>')
    .replace(/`([^`]+)`/g, '<code class="ai-inline-code">$1</code>')
    .replace(/\n/g, '<br>')
  
  return safe
}

function extractSQL(content: string): string | null {
  const m = content.match(/```sql\n?([\s\S]*?)```/)
  return m ? m[1].trim() : null
}
</script>

<style scoped>
.ai-root { display: flex; flex-direction: column; height: 100%; min-height: 0; background: var(--bg-panel); }
.ai-header { display: flex; align-items: center; justify-content: space-between; padding: 8px 12px; border-bottom: 1px solid var(--border); flex-shrink: 0; }
.ai-title { font-size: 13px; font-weight: 600; color: var(--text-primary); }
.ai-quick-btns { display: flex; gap: 4px; }
.ai-quick { padding: 3px 9px; font-size: 11px; background: var(--bg-sidebar); border: 1px solid var(--border); border-radius: 4px; color: var(--text-muted); cursor: pointer; }
.ai-quick:hover:not(:disabled) { background: var(--accent); color: #fff; border-color: var(--accent); }
.ai-quick:disabled { opacity: .4; cursor: default; }
.ai-quick--settings { font-size: 13px; padding: 2px 8px; }
.ai-settings { padding: 10px 12px; background: var(--bg-sidebar); border-bottom: 1px solid var(--border); flex-shrink: 0; display: flex; flex-direction: column; gap: 8px; }
.ai-settings-row { display: flex; align-items: center; gap: 8px; }
.ai-settings-row label { font-size: 11px; color: var(--text-muted); width: 64px; flex-shrink: 0; }
.ai-input { flex: 1; background: var(--bg-panel); border: 1px solid var(--border); border-radius: 4px; color: var(--text-primary); font-size: 12px; padding: 4px 8px; }
.ai-messages { flex: 1; overflow-y: auto; padding: 10px 12px; display: flex; flex-direction: column; gap: 10px; min-height: 0; }
.ai-welcome { color: var(--text-muted); font-size: 13px; text-align: center; margin: auto; }
.ai-msg { display: flex; flex-direction: column; gap: 4px; max-width: 100%; }
.ai-msg--user .ai-msg-content { background: var(--accent); color: #fff; border-radius: 8px 8px 2px 8px; padding: 8px 12px; font-size: 13px; align-self: flex-end; }
.ai-msg--user { align-items: flex-end; }
.ai-msg--assistant .ai-msg-content { background: var(--bg-sidebar); border: 1px solid var(--border); border-radius: 2px 8px 8px 8px; padding: 8px 12px; font-size: 13px; }
.ai-msg-label { font-size: 10px; color: var(--text-muted); font-weight: 600; text-transform: uppercase; letter-spacing: .4px; }
.ai-copy-sql { align-self: flex-start; margin-top: 4px; font-size: 11px; background: var(--accent); color: #fff; border: none; border-radius: 4px; padding: 3px 10px; cursor: pointer; }
.ai-thinking { display: flex; gap: 4px; padding: 8px 12px; background: var(--bg-sidebar); border-radius: 8px; width: fit-content; }
.ai-thinking span { width: 6px; height: 6px; background: var(--accent); border-radius: 50%; animation: ai-bounce .8s infinite; }
.ai-thinking span:nth-child(2) { animation-delay: .15s; }
.ai-thinking span:nth-child(3) { animation-delay: .3s; }
@keyframes ai-bounce { 0%,60%,100% { transform: translateY(0); } 30% { transform: translateY(-5px); } }
.ai-input-row { display: flex; gap: 6px; padding: 8px 12px; border-top: 1px solid var(--border); flex-shrink: 0; }
.ai-textarea { flex: 1; background: var(--bg-sidebar); border: 1px solid var(--border); border-radius: 6px; color: var(--text-primary); font-size: 13px; padding: 6px 10px; resize: none; font-family: inherit; }
.ai-send-btn { background: var(--accent); border: none; border-radius: 6px; color: #fff; font-size: 16px; padding: 0 14px; cursor: pointer; flex-shrink: 0; }
.ai-send-btn:disabled { opacity: .4; cursor: default; }
.ai-hint { font-size: 10px; color: var(--text-muted); text-align: right; padding: 0 12px 6px; flex-shrink: 0; }
</style>

<style>
.ai-code { background: var(--bg-panel); border: 1px solid var(--border); border-radius: 6px; padding: 8px 12px; font-family: var(--font-mono); font-size: 12px; overflow-x: auto; margin: 6px 0; white-space: pre; }
.ai-code--sql { border-left: 3px solid var(--accent); }
.ai-inline-code { background: var(--bg-panel); border: 1px solid var(--border); border-radius: 3px; padding: 1px 5px; font-family: var(--font-mono); font-size: 12px; }
</style>
