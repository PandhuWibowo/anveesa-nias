import { ref, computed } from 'vue'
import axios from 'axios'

interface User {
  id: number
  username: string
  role: string
  permissions: string[]
  mfa_enabled?: boolean
  mfa_enforced?: boolean
  mfa_required_setup?: boolean
}

const STORAGE_KEY = 'nias-token'
const LEGACY_STORAGE_KEY = STORAGE_KEY

const user = ref<User | null>(null)
const persistedToken = localStorage.getItem(STORAGE_KEY) ?? sessionStorage.getItem(LEGACY_STORAGE_KEY) ?? ''
const token = ref<string>(persistedToken)
const authReady = ref(false)
const authEnabled = ref(false)

if (persistedToken && !localStorage.getItem(STORAGE_KEY)) {
  localStorage.setItem(STORAGE_KEY, persistedToken)
}
sessionStorage.removeItem(LEGACY_STORAGE_KEY)

window.addEventListener('storage', (event) => {
  if (event.key !== STORAGE_KEY) return
  token.value = event.newValue ?? ''
  if (!token.value) {
    user.value = null
  }
})

// Add token to all requests
axios.interceptors.request.use((config) => {
  if (token.value) {
    config.headers.Authorization = `Bearer ${token.value}`
    // Also add user ID and role for RBAC (if user is loaded)
    if (user.value) {
      config.headers['X-User-ID'] = String(user.value.id)
      config.headers['X-User-Role'] = user.value.role
      config.headers['X-Username'] = user.value.username
    }
  }
  return config
})

// Handle 401/423 responses — only clear auth for real session failures,
// not for permission-denied 401s from resource endpoints (which would cascade-logout the user).
axios.interceptors.response.use(
  (response) => response,
  (error) => {
    const status = error.response?.status
    const url: string = error.config?.url ?? ''

    if (status === 423) {
      // Account locked — always clear
      token.value = ''
      user.value = null
      localStorage.removeItem(STORAGE_KEY)
      sessionStorage.removeItem(LEGACY_STORAGE_KEY)
    } else if (status === 401) {
      // Only clear auth when the 401 comes from core auth endpoints.
      // Resource-level 401s (e.g. /api/connections/*/cloud-config) must NOT log the user out
      // because they may fire before user.value is hydrated on first page load.
      const isAuthEndpoint = url.startsWith('/api/auth/')
      if (isAuthEndpoint) {
        token.value = ''
        user.value = null
        localStorage.removeItem(STORAGE_KEY)
        sessionStorage.removeItem(LEGACY_STORAGE_KEY)
      }
    }
    return Promise.reject(error)
  }
)

export function useAuth() {
  const isAuthenticated = computed(() => !!user.value || !authEnabled.value)
  const isAdmin = computed(() => user.value?.role === 'admin')
  const permissions = computed(() => user.value?.permissions ?? [])
  const mustSetupMfa = computed(() => !!user.value?.mfa_required_setup)

  function hasPermission(permission: string): boolean {
    if (!authEnabled.value) return true
    if (user.value?.role === 'admin') return true
    return permissions.value.includes(permission)
  }

  function hasAnyPermission(required: string[]): boolean {
    if (!authEnabled.value) return true
    if (user.value?.role === 'admin') return true
    return required.some((permission) => permissions.value.includes(permission))
  }

  async function fetchMe() {
    try {
      const { data } = await axios.get('/api/auth/setup')
      authEnabled.value = data.auth_enabled

      if (authEnabled.value && token.value) {
        const me = await axios.get('/api/auth/me')
        user.value = {
          ...me.data,
          permissions: Array.isArray(me.data?.permissions) ? me.data.permissions : [],
        }
      }
    } catch {
      // Token invalid or expired
      token.value = ''
      user.value = null
      localStorage.removeItem(STORAGE_KEY)
      sessionStorage.removeItem(LEGACY_STORAGE_KEY)
    } finally {
      authReady.value = true
    }
  }

  async function login(username: string, password: string, totpCode?: string): Promise<{ success: boolean; requires2fa?: boolean; username?: string; mustSetupMfa?: boolean; error?: string }> {
    try {
      const { data } = await axios.post('/api/auth/login', { username, password, totp_code: totpCode })
      
      // Check if 2FA is required
      if (data.requires_2fa) {
        return { success: false, requires2fa: true, username: data.username }
      }
      
      // Successful login
      token.value = data.token
      user.value = {
        ...data.user,
        permissions: Array.isArray(data.user?.permissions) ? data.user.permissions : [],
      }
      localStorage.setItem(STORAGE_KEY, data.token)
      sessionStorage.removeItem(LEGACY_STORAGE_KEY)
      return { success: true, mustSetupMfa: !!data.user?.mfa_required_setup }
    } catch (error: any) {
      return { success: false, error: error.response?.data?.error || 'Login failed' }
    }
  }

  function markMfaSetupComplete() {
    if (!user.value) return
    user.value = {
      ...user.value,
      mfa_enabled: true,
      mfa_required_setup: false,
    }
  }

  async function logout() {
    try {
      if (token.value) {
        await axios.post('/api/auth/logout')
      }
    } catch {
      // Best-effort logout; local token should still be cleared.
    }
    token.value = ''
    user.value = null
    localStorage.removeItem(STORAGE_KEY)
    sessionStorage.removeItem(LEGACY_STORAGE_KEY)
  }

  return {
    user,
    token,
    authReady,
    authEnabled,
    isAuthenticated,
    isAdmin,
    permissions,
    mustSetupMfa,
    hasPermission,
    hasAnyPermission,
    fetchMe,
    login,
    markMfaSetupComplete,
    logout,
  }
}
