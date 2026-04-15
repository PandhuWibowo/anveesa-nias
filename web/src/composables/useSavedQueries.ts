import { ref } from 'vue'
import axios from 'axios'

export interface SavedQuery {
  id: number
  name: string
  conn_id: number | null
  sql: string
  description: string
  created_at: string
  updated_at: string
}

const queries = ref<SavedQuery[]>([])
const loading = ref(false)

async function fetchAll() {
  loading.value = true
  try {
    const { data } = await axios.get<SavedQuery[]>('/api/saved-queries')
    queries.value = data ?? []
  } finally {
    loading.value = false
  }
}

async function save(name: string, sql: string, description = '', connId: number | null = null) {
  const { data } = await axios.post<{ id: number }>('/api/saved-queries', {
    name,
    sql,
    description,
    conn_id: connId,
  })
  await fetchAll()
  return data.id
}

async function remove(id: number) {
  await axios.delete(`/api/saved-queries/${id}`)
  queries.value = queries.value.filter((q) => q.id !== id)
}

export function useSavedQueries() {
  return { queries, loading, fetchAll, save, remove }
}
