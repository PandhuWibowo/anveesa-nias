import { ref } from 'vue'
import axios from 'axios'

export interface QueryResult {
  columns: string[]
  rows: unknown[][]
  row_count: number
  affected_rows: number
  duration_ms: number
  error?: string
}

export interface HistoryItem {
  id?: number
  sql: string
  time: Date
  connId: number
  duration_ms: number
  row_count: number
  error?: string
}

// In-memory list — merged with backend history on load
const localHistory = ref<HistoryItem[]>([])

export function useQuery() {
  const result = ref<QueryResult | null>(null)
  const running = ref(false)
  const error = ref('')

  async function execute(connId: number, sql: string) {
    if (!sql.trim()) return
    running.value = true
    error.value = ''
    result.value = null

    try {
      const { data } = await axios.post<QueryResult>(`/api/connections/${connId}/query`, { sql })
      result.value = data

      const item: HistoryItem = {
        sql,
        time: new Date(),
        connId,
        duration_ms: data.duration_ms,
        row_count: data.row_count,
      }
      localHistory.value.unshift(item)
      if (localHistory.value.length > 200) localHistory.value.pop()

      // Persist to backend (fire-and-forget)
      axios.post(`/api/connections/${connId}/history`, {
        sql,
        duration_ms: data.duration_ms,
        row_count: data.row_count,
      }).catch(() => {})
    } catch (e: unknown) {
      const msg = (e as { response?: { data?: { error?: string } } })?.response?.data?.error ?? 'Query failed'
      error.value = msg

      const item: HistoryItem = {
        sql,
        time: new Date(),
        connId,
        duration_ms: 0,
        row_count: 0,
        error: msg,
      }
      localHistory.value.unshift(item)
      axios.post(`/api/connections/${connId}/history`, {
        sql,
        duration_ms: 0,
        row_count: 0,
        error: msg,
      }).catch(() => {})
    } finally {
      running.value = false
    }
  }

  async function explain(connId: number, sql: string) {
    running.value = true
    error.value = ''
    result.value = null
    try {
      const { data } = await axios.post<QueryResult>(`/api/connections/${connId}/query`, {
        sql: `EXPLAIN ${sql}`,
      })
      result.value = data
    } catch (e: unknown) {
      const msg = (e as { response?: { data?: { error?: string } } })?.response?.data?.error ?? 'Explain failed'
      error.value = msg
    } finally {
      running.value = false
    }
  }

  async function fetchHistory(connId: number): Promise<HistoryItem[]> {
    try {
      const { data } = await axios.get<Array<{
        id: number; sql: string; duration_ms: number; row_count: number; error?: string; executed_at: string
      }>>(`/api/connections/${connId}/history`)
      return data.map((h) => ({
        id: h.id,
        sql: h.sql,
        time: new Date(h.executed_at),
        connId,
        duration_ms: h.duration_ms,
        row_count: h.row_count,
        error: h.error,
      }))
    } catch {
      return []
    }
  }

  async function clearHistory(connId: number) {
    await axios.delete(`/api/connections/${connId}/history`).catch(() => {})
    localHistory.value = localHistory.value.filter((h) => h.connId !== connId)
  }

  return { result, running, error, history: localHistory, execute, explain, fetchHistory, clearHistory }
}
