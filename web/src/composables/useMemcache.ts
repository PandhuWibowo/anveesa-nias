import axios from 'axios'

export interface MemcachePingResponse {
  status: string
  message: string
  latency_ms: number
}

export interface MemcacheValueResponse {
  key: string
  flags: number
  bytes: number
  value: string
  found: boolean
}

export interface MemcacheWritePayload {
  key: string
  value: string
  flags: number
  ttl: number
}

export function useMemcache() {
  async function ping(connId: number) {
    const { data } = await axios.get<MemcachePingResponse>(`/api/connections/${connId}/memcache/ping`)
    return data
  }

  async function stats(connId: number) {
    const { data } = await axios.get<Record<string, string>>(`/api/connections/${connId}/memcache/stats`)
    return data
  }

  async function fetchKey(connId: number, key: string) {
    const { data } = await axios.get<MemcacheValueResponse>(`/api/connections/${connId}/memcache/key`, {
      params: { key },
    })
    return data
  }

  async function saveKey(connId: number, payload: MemcacheWritePayload) {
    await axios.put(`/api/connections/${connId}/memcache/key`, payload)
  }

  async function deleteKey(connId: number, key: string) {
    const { data } = await axios.delete<{ deleted: boolean }>(`/api/connections/${connId}/memcache/key`, {
      params: { key },
    })
    return data.deleted
  }

  async function flush(connId: number, delay = 0) {
    await axios.post(`/api/connections/${connId}/memcache/flush`, { delay })
  }

  return { ping, stats, fetchKey, saveKey, deleteKey, flush }
}
