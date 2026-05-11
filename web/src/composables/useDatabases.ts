import { ref } from 'vue'
import axios from 'axios'

const cache = new Map<number, string[]>()

export function useDatabases() {
  const databases = ref<string[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchDatabases(connId: number | null) {
    databases.value = []
    error.value = null
    if (!connId) return
    if (cache.has(connId)) {
      databases.value = cache.get(connId)!
      return
    }
    loading.value = true
    try {
      const { data } = await axios.get<string[]>(`/api/connections/${connId}/databases`)
      databases.value = data ?? []
      cache.set(connId, databases.value)
    } catch (e: any) {
      databases.value = []
      error.value = e?.response?.data?.error ?? e?.message ?? 'Failed to connect to database'
    } finally {
      loading.value = false
    }
  }

  function invalidate(connId: number) {
    cache.delete(connId)
  }

  return { databases, loading, error, fetchDatabases, invalidate }
}
