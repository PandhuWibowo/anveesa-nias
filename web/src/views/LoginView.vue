<script setup lang="ts">
import { ref, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import NavIcon from '@/components/ui/NavIcon.vue'
import LanguageMenu from '@/components/ui/LanguageMenu.vue'
import { t, type MessageKey } from '@/i18n'

const router = useRouter()
const { login } = useAuth()

const username = ref('')
const password = ref('')
const totpCode = ref('')
// A message key, or a server-provided message shown as-is.
const error = ref<{ key?: MessageKey; text?: string } | null>(null)
const loading = ref(false)
const requires2fa = ref(false)
const showPassword = ref(false)
const codeInput = ref<HTMLInputElement | null>(null)

async function handleLogin() {
  if (!username.value || !password.value) {
    error.value = { key: 'auth.err.missing' }
    return
  }

  if (requires2fa.value && !totpCode.value) {
    error.value = { key: 'auth.err.missingCode' }
    return
  }

  loading.value = true
  error.value = null

  const result = await login(username.value, password.value, totpCode.value || undefined)
  loading.value = false

  if (result.success) {
    router.push(result.mustSetupMfa ? { name: 'security', query: { setup: 'mfa' } } : { name: 'welcome' })
  } else if (result.requires2fa) {
    requires2fa.value = true
    error.value = null
    await nextTick()
    codeInput.value?.focus()
  } else {
    error.value = result.error ? { text: result.error } : { key: 'auth.err.mismatch' }
  }
}

function back() {
  requires2fa.value = false
  totpCode.value = ''
  error.value = null
}
</script>

<template>
  <div class="login">
    <main class="login__panel">
      <div class="login__brand">
        <span class="login__mark"><NavIcon name="nias" :size="18" /></span>
        <span class="login__name">Anveesa Nias</span>
        <LanguageMenu class="login__lang" variant="inline" />
      </div>

      <h1 class="login__title">{{ requires2fa ? t('auth.twoStep') : t('auth.signIn') }}</h1>
      <p class="login__sub">
        {{ requires2fa ? t('auth.twoStepHelp', { user: username }) : t('auth.signInHelp') }}
      </p>

      <form class="login__form" novalidate @submit.prevent="handleLogin">
        <template v-if="!requires2fa">
          <div class="form-group">
            <label class="form-label" for="login-user">{{ t('auth.username') }}</label>
            <input
              id="login-user"
              v-model="username"
              class="base-input"
              type="text"
              autocomplete="username"
              autocapitalize="off"
              spellcheck="false"
              autofocus
            />
          </div>
          <div class="form-group">
            <label class="form-label" for="login-pass">{{ t('auth.password') }}</label>
            <div class="login__pw">
              <input
                id="login-pass"
                v-model="password"
                class="base-input"
                :type="showPassword ? 'text' : 'password'"
                autocomplete="current-password"
              />
              <button
                type="button"
                class="login__pw-toggle"
                :aria-label="showPassword ? t('auth.hidePassword') : t('auth.showPassword')"
                :aria-pressed="showPassword"
                @click="showPassword = !showPassword"
              >
                {{ showPassword ? t('common.hide') : t('common.show') }}
              </button>
            </div>
          </div>
        </template>

        <div v-else class="form-group">
          <label class="form-label" for="login-code">{{ t('auth.code') }}</label>
          <input
            id="login-code"
            ref="codeInput"
            v-model="totpCode"
            class="base-input login__code"
            type="text"
            maxlength="16"
            inputmode="numeric"
            autocomplete="one-time-code"
          />
        </div>

        <div v-if="error" class="notice notice--error" role="alert">{{ error.key ? t(error.key) : error.text }}</div>

        <button class="base-btn base-btn--primary login__submit" type="submit" :disabled="loading">
          <svg v-if="loading" class="spin" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" aria-hidden="true"><path d="M21 12a9 9 0 1 1-6.219-8.56" /></svg>
          {{ loading ? (requires2fa ? t('auth.verifying') : t('auth.signingIn')) : (requires2fa ? t('auth.verify') : t('auth.signIn')) }}
        </button>
        <button v-if="requires2fa" type="button" class="base-btn base-btn--ghost login__submit" @click="back">
          {{ t('auth.differentAccount') }}
        </button>
      </form>
    </main>
  </div>
</template>

<style scoped>
.login {
  min-height: 100dvh;
  display: grid;
  place-items: center;
  padding: 24px 16px;
  background: var(--canvas);
  overflow-y: auto;
}
.login__panel {
  width: 100%;
  max-width: 360px;
}
.login__lang { margin-left: auto; }
.login__brand {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 40px;
}
.login__mark {
  width: 32px;
  height: 32px;
  border-radius: 30%;
  display: grid;
  place-items: center;
  background: var(--signature-bg);
  color: var(--signature);
}
.login__name {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
}
.login__title {
  /* One of the few places Nias speaks directly to you, so it uses the family display face. */
  font-family: var(--display);
  font-variation-settings: 'SOFT' 50;
  font-size: 30px;
  font-weight: 500;
  letter-spacing: -0.01em;
  line-height: 1.15;
  color: var(--text-primary);
}
.login__sub {
  margin-top: 6px;
  font-size: 14px;
  line-height: 1.55;
  color: var(--text-secondary);
}
.login__form {
  display: flex;
  flex-direction: column;
  gap: 16px;
  margin-top: 28px;
}
.login__form .base-input { min-height: 40px; font-size: 14px; }
.login__pw { position: relative; }
.login__pw .base-input { padding-right: 64px; }
.login__pw-toggle {
  position: absolute;
  top: 50%;
  right: 6px;
  transform: translateY(-50%);
  height: 28px;
  padding: 0 8px;
  border: none;
  border-radius: var(--r-xs);
  background: transparent;
  color: var(--text-muted);
  font: inherit;
  font-size: 12.5px;
  font-weight: 500;
  cursor: pointer;
}
.login__pw-toggle:hover { color: var(--text-primary); background: var(--bg-hover-strong); }
.login__code {
  text-align: center;
  font-family: var(--mono);
  font-size: 22px !important;
  letter-spacing: 0.4em;
  min-height: 48px !important;
}
.login__submit {
  width: 100%;
  height: 40px;
  font-size: 14px;
}
.login__submit + .login__submit { margin-top: -6px; }
</style>
