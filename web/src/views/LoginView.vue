<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuth } from '@/composables/useAuth'

const router = useRouter()
const { login } = useAuth()

const username = ref('')
const password = ref('')
const totpCode = ref('')
const error = ref('')
const loading = ref(false)
const requires2fa = ref(false)

async function handleLogin() {
  if (!username.value || !password.value) {
    error.value = 'Please enter your credentials.'
    return
  }
  
  if (requires2fa.value && !totpCode.value) {
    error.value = 'Please enter your 2FA code.'
    return
  }
  
  loading.value = true
  error.value = ''
  
  const result = await login(username.value, password.value, totpCode.value || undefined)
  loading.value = false
  
  if (result.success) {
    router.push({ name: 'data' })
  } else if (result.requires2fa) {
    requires2fa.value = true
    error.value = ''
  } else {
    error.value = result.error || 'Invalid credentials.'
  }
}

function back() {
  requires2fa.value = false
  totpCode.value = ''
  error.value = ''
}
</script>

<template>
  <div class="auth-screen">
    <div class="auth-card">
      <div class="auth-brand">
        <div class="brand-icon">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
            <ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5V19A9 3 0 0 0 21 19V5"/><path d="M3 12A9 3 0 0 0 21 12"/>
          </svg>
        </div>
        <div>
          <div class="auth-brand__name">Anveesa <span>Nias</span></div>
        </div>
      </div>

      <h1 class="auth-title">{{ requires2fa ? 'Two-Factor Authentication' : 'Sign in' }}</h1>
      <p class="auth-sub">{{ requires2fa ? 'Enter the 6-digit code from your authenticator app.' : 'Enter your credentials to access the database studio.' }}</p>

      <form class="auth-form" @submit.prevent="handleLogin">
        <div v-if="!requires2fa">
          <div class="form-group">
            <label class="form-label">Username</label>
            <input
              v-model="username"
              class="base-input"
              type="text"
              placeholder="admin"
              autocomplete="username"
            />
          </div>
          <div class="form-group">
            <label class="form-label">Password</label>
            <input
              v-model="password"
              class="base-input"
              type="password"
              placeholder="••••••••"
              autocomplete="current-password"
            />
          </div>
        </div>

        <div v-else class="form-group">
          <label class="form-label">2FA Code</label>
          <input
            v-model="totpCode"
            class="base-input auth-code-input"
            type="text"
            placeholder="000000"
            maxlength="6"
            inputmode="numeric"
            pattern="[0-9]*"
            autocomplete="one-time-code"
            autofocus
          />
          <p class="form-hint">Enter the 6-digit code or use a backup code</p>
        </div>

        <div v-if="error" class="notice notice--error">{{ error }}</div>

        <div class="auth-actions">
          <button v-if="requires2fa" type="button" class="base-btn base-btn--ghost" @click="back" style="width:100%;justify-content:center">
            ← Back
          </button>
          <button class="base-btn base-btn--primary" type="submit" :disabled="loading" style="width:100%;justify-content:center">
            <svg v-if="loading" class="spin" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
            {{ loading ? 'Verifying…' : (requires2fa ? 'Verify' : 'Sign in') }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<style scoped>
.auth-code-input {
  text-align: center;
  font-size: 20px;
  font-weight: 600;
  letter-spacing: 8px;
  font-family: var(--mono, monospace);
}

.form-hint {
  margin: 6px 0 0;
  font-size: 11px;
  color: var(--text-muted);
}
</style>
