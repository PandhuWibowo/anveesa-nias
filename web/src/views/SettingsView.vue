<script setup lang="ts">
import { onMounted, ref } from 'vue'
import axios from 'axios'
import { useToast } from '@/composables/useToast'

interface AISettings {
  api_key: string
  base_url: string
  model: string
  source?: string
  fallback_available?: boolean
}

const toast = useToast()

const loading = ref(false)
const saving = ref(false)
const settings = ref<AISettings>({
  api_key: '',
  base_url: 'https://api.openai.com/v1',
  model: 'gpt-4o-mini',
})

async function loadSettings() {
  loading.value = true
  try {
    const { data } = await axios.get<AISettings>('/api/ai/settings')
    settings.value = {
      api_key: data?.api_key || '',
      base_url: data?.base_url || 'https://api.openai.com/v1',
      model: data?.model || 'gpt-4o-mini',
      source: data?.source || 'user',
      fallback_available: Boolean(data?.fallback_available),
    }
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to load settings')
  } finally {
    loading.value = false
  }
}

async function saveSettings() {
  saving.value = true
  try {
    await axios.post('/api/ai/settings', settings.value)
    toast.success('Settings saved')
    await loadSettings()
  } catch (e: any) {
    toast.error(e?.response?.data?.error || 'Failed to save settings')
  } finally {
    saving.value = false
  }
}

onMounted(loadSettings)
</script>

<template>
  <div class="page-shell set-root">
    <div class="page-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">AI</div>
            <div class="page-title">AI Settings</div>
            <div class="page-subtitle">Configure your personal AI provider key, base URL, and model for the SQL assistant and AI Analytics.</div>
          </div>
          <div class="page-hero__actions">
            <button class="base-btn base-btn--ghost base-btn--sm" :disabled="loading" @click="loadSettings">
              {{ loading ? 'Refreshing…' : 'Refresh' }}
            </button>
          </div>
        </section>

        <section class="page-panel set-panel">
          <div class="set-panel__head">
            <div>
              <div class="set-panel__title">AI Provider</div>
              <div class="set-panel__sub">Used by the SQL assistant and the AI Analytics page for your account.</div>
            </div>
            <div class="set-badge">Personal</div>
          </div>

          <div v-if="settings.source === 'global' && settings.fallback_available" class="notice notice--warning">
            No personal AI key is saved yet. Current AI requests will fall back to the existing shared default until you add your own key.
          </div>

          <div class="set-grid">
            <label class="set-field">
              <span class="set-label">API Key</span>
              <input v-model="settings.api_key" class="base-input" type="password" placeholder="sk-..." autocomplete="off" />
              <span class="set-hint">Stored server-side for your account. If a masked value is shown, leave it unchanged to keep your current key.</span>
            </label>

            <label class="set-field">
              <span class="set-label">Base URL</span>
              <input v-model="settings.base_url" class="base-input" type="text" placeholder="https://api.openai.com/v1" />
              <span class="set-hint">Keep the default for OpenAI. Change only if you are using a compatible provider endpoint.</span>
            </label>

            <label class="set-field">
              <span class="set-label">Model</span>
              <input v-model="settings.model" class="base-input" type="text" placeholder="gpt-4o-mini" />
              <span class="set-hint">This model will be used for your SQL assistance and AI analytics planning and summaries.</span>
            </label>
          </div>

          <div class="set-actions">
            <button class="base-btn base-btn--primary base-btn--sm" :disabled="saving" @click="saveSettings">
              {{ saving ? 'Saving…' : 'Save Settings' }}
            </button>
          </div>
        </section>

        <section class="page-panel set-panel">
          <div class="set-panel__head">
            <div>
              <div class="set-panel__title">Scope</div>
              <div class="set-panel__sub">Where these settings are used in the current product.</div>
            </div>
          </div>

          <div class="set-usage">
            <div class="set-usage__item">
              <div class="set-usage__title">AI Query Assistant</div>
              <div class="set-usage__desc">Used inside the query workspace to explain SQL, fix errors, and draft new queries with your provider settings.</div>
            </div>
            <div class="set-usage__item">
              <div class="set-usage__title">AI Analytics</div>
              <div class="set-usage__desc">Used to plan read-only analytics SQL, summarize results, and suggest follow-up questions for your sessions.</div>
            </div>
          </div>
        </section>
      </div>
    </div>
  </div>
</template>

<style scoped>
.set-root {
  background: var(--bg-body);
}

.page-scroll {
  padding: 16px 20px 24px;
}

.set-panel {
  display: grid;
  gap: 18px;
  padding: 20px 24px;
}

.set-panel__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.set-badge {
  padding: 4px 10px;
  border-radius: 999px;
  border: 1px solid var(--brand-ring);
  background: var(--brand-dim);
  color: var(--brand);
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.08em;
}

.set-panel__title {
  font-size: 15px;
  font-weight: 700;
  color: var(--text-primary);
}

.set-panel__sub,
.set-hint {
  color: var(--text-muted);
  font-size: 12px;
}

.set-grid {
  display: grid;
  gap: 14px;
}

.set-field {
  display: grid;
  gap: 6px;
  padding: 14px 16px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.02);
}

.set-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-secondary);
}

.set-actions {
  display: flex;
  justify-content: flex-end;
}

.set-usage {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.set-usage__item {
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.02);
}

.set-usage__title {
  font-size: 14px;
  font-weight: 700;
  color: var(--text-primary);
}

.set-usage__desc {
  margin-top: 6px;
  font-size: 13px;
  color: var(--text-secondary);
  line-height: 1.5;
}

@media (max-width: 960px) {
  .page-scroll {
    padding: 12px 14px 20px;
  }

  .set-panel {
    padding: 16px;
  }

  .set-panel__head,
  .set-usage {
    grid-template-columns: 1fr;
    flex-direction: column;
    align-items: stretch;
  }

  .set-actions {
    justify-content: stretch;
  }
}
</style>
