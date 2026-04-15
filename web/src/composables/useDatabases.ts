import { ref } from 'vue'
import axios from 'axios'

const cache = new Map<number, string[]>()

export function useDatabases() {
  const databases = ref<string[]>([])
  const loading = ref(false)

  async function fetchDatabases(connId: number | null) {
    databases.value = []
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
    } catch {
      databases.value = []
    } finally {
      loading.value = false
    }
  }

  function invalidate(connId: number) {
    cache.delete(connId)
  }

  return { databases, loading, fetchDatabases, invalidate }
}
