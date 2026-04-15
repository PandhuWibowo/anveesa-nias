<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuth } from '@/composables/useAuth'

const router = useRouter()
const { login } = useAuth()

const username = ref('')
const password = ref('')
const error = ref('')
const loading = ref(false)

async function handleLogin() {
  if (!username.value || !password.value) {
    error.value = 'Please enter your credentials.'
    return
  }
  loading.value = true
  error.value = ''
  const ok = await login(username.value, password.value)
  loading.value = false
  if (ok) {
    router.push({ name: 'welcome' })
  } else {
    error.value = 'Invalid username or password.'
  }
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

      <h1 class="auth-title">Sign in</h1>
      <p class="auth-sub">Enter your credentials to access the database studio.</p>

      <form class="auth-form" @submit.prevent="handleLogin">
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

        <div v-if="error" class="notice notice--error">{{ error }}</div>

        <div class="auth-actions">
          <button class="base-btn base-btn--primary" type="submit" :disabled="loading" style="width:100%;justify-content:center">
            <svg v-if="loading" class="spin" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
            {{ loading ? 'Signing in…' : 'Sign in' }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>
