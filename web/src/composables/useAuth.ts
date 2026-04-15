import { ref, computed } from 'vue'
import axios from 'axios'

interface User {
  id: number
  username: string
  role: string
}

const STORAGE_KEY = 'nias-token'

const user = ref<User | null>(null)
// Use sessionStorage for better security (token expires when browser closes)
const token = ref<string>(sessionStorage.getItem(STORAGE_KEY) ?? '')
const authReady = ref(false)
const authEnabled = ref(false)

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
      sessionStorage.removeItem(STORAGE_KEY)
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
      sessionStorage.removeItem(STORAGE_KEY)
    } finally {
      authReady.value = true
    }
  }

  async function login(username: string, password: string): Promise<boolean> {
    try {
      const { data } = await axios.post('/api/auth/login', { username, password })
      token.value = data.token
      user.value = data.user
      sessionStorage.setItem(STORAGE_KEY, data.token)
      return true
    } catch {
      return false
    }
  }

  function logout() {
    token.value = ''
    user.value = null
    sessionStorage.removeItem(STORAGE_KEY)
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
