import { ref, computed } from 'vue'
import axios from 'axios'

interface User {
  id: number
  username: string
  role: string
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

// Handle 401 responses (expired/invalid token)
axios.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      // Clear invalid token
      token.value = ''
      user.value = null
      localStorage.removeItem(STORAGE_KEY)
      sessionStorage.removeItem(LEGACY_STORAGE_KEY)
    }
    return Promise.reject(error)
  }
)

export function useAuth() {
  const isAuthenticated = computed(() => !!user.value || !authEnabled.value)
  const isAdmin = computed(() => user.value?.role === 'admin')

  async function fetchMe() {
    try {
      const { data } = await axios.get('/api/auth/setup')
      authEnabled.value = data.auth_enabled

      if (authEnabled.value && token.value) {
        const me = await axios.get('/api/auth/me')
        user.value = me.data
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

  async function login(username: string, password: string, totpCode?: string): Promise<{ success: boolean; requires2fa?: boolean; username?: string; error?: string }> {
    try {
      const { data } = await axios.post('/api/auth/login', { username, password, totp_code: totpCode })
      
      // Check if 2FA is required
      if (data.requires_2fa) {
        return { success: false, requires2fa: true, username: data.username }
      }
      
      // Successful login
      token.value = data.token
      user.value = data.user
      localStorage.setItem(STORAGE_KEY, data.token)
      sessionStorage.removeItem(LEGACY_STORAGE_KEY)
      return { success: true }
    } catch (error: any) {
      return { success: false, error: error.response?.data?.error || 'Login failed' }
    }
  }

  function logout() {
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
    fetchMe,
    login,
    logout,
  }
}
