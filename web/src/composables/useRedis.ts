import axios from 'axios'

export interface RedisKeySummary {
  key: string
  type: string
  ttl: number
}

export interface RedisKeysResponse {
  cursor: string
  keys: RedisKeySummary[]
}

export interface RedisValueResponse {
  key: string
  type: string
  ttl: number
  length?: number
  value: unknown
  truncated: boolean
  memory_bytes?: number
}

export interface RedisInfoResponse {
  sections: Record<string, Record<string, string>>
  keyspace: Record<string, number>
}

export interface RedisSlowlogEntry {
  id: number
  timestamp: number
  micros: number
  command: string
  client?: string
}

export interface RedisMonitorMessage {
  channel: string
  payload: string
  ts: number
}

export type RedisWritableType = 'string' | 'hash' | 'list' | 'set' | 'zset' | 'stream' | 'json'

export interface RedisWritePayload {
  key: string
  type: RedisWritableType
  value: unknown
  ttl: number
}

export interface RedisScriptResult {
  line: number
  command: string
  result?: unknown
  error?: string
}

export interface RedisPingResponse {
  status: string
  message: string
  latency_ms: number
}

export function useRedis() {
  async function ping(connId: number, db?: number) {
    const { data } = await axios.get<RedisPingResponse>(`/api/connections/${connId}/redis/ping`, {
      params: { db },
    })
    return data
  }

  async function fetchKeys(connId: number, pattern = '*', cursor = '0', count = 100, db?: number) {
    const { data } = await axios.get<RedisKeysResponse>(`/api/connections/${connId}/redis/keys`, {
      params: { pattern, cursor, count, db },
    })
    return data
  }

  async function fetchValue(connId: number, key: string, db?: number, limit?: number) {
    const { data } = await axios.get<RedisValueResponse>(`/api/connections/${connId}/redis/key`, {
      params: { key, db, limit },
    })
    return data
  }

  async function saveKey(connId: number, payload: RedisWritePayload, db?: number) {
    await axios.put(`/api/connections/${connId}/redis/key`, { ...payload, db })
  }

  async function deleteKey(connId: number, key: string, db?: number) {
    await axios.delete(`/api/connections/${connId}/redis/key`, { params: { key, db } })
  }

  async function renameKey(connId: number, oldKey: string, newKey: string, db?: number) {
    await axios.post(`/api/connections/${connId}/redis/rename`, { old_key: oldKey, new_key: newKey, db })
  }

  async function moveKey(connId: number, key: string, fromDb: number, toDb: number, overwrite: boolean) {
    await axios.post(`/api/connections/${connId}/redis/move`, {
      key,
      from_db: fromDb,
      to_db: toDb,
      overwrite,
    })
  }

  async function runCommand(connId: number, command: string, db?: number) {
    const { data } = await axios.post<{ result: unknown }>(`/api/connections/${connId}/redis/command`, { command, db })
    return data.result
  }

  async function generateScript(connId: number, params: { key?: string; pattern?: string; db?: number }) {
    const { data } = await axios.get<string>(`/api/connections/${connId}/redis/script`, {
      params,
      responseType: 'text',
    })
    return data
  }

  async function executeScript(connId: number, script: string, db?: number) {
    const { data } = await axios.post<{ results: RedisScriptResult[] }>(`/api/connections/${connId}/redis/script`, { script, db })
    return data.results
  }

  async function setTtl(connId: number, key: string, ttl: number, db?: number) {
    await axios.post(`/api/connections/${connId}/redis/ttl`, { key, ttl, db })
  }

  async function fetchInfo(connId: number, db?: number) {
    const { data } = await axios.get<RedisInfoResponse>(`/api/connections/${connId}/redis/info`, { params: { db } })
    return data
  }

  async function fetchSlowlog(connId: number, db?: number, count = 50) {
    const { data } = await axios.get<{ entries: RedisSlowlogEntry[] }>(`/api/connections/${connId}/redis/slowlog`, {
      params: { db, count },
    })
    return data.entries
  }

  // Opens an SSE stream of pub/sub messages. Returns a stop() function.
  function monitor(
    connId: number,
    channel: string,
    db: number | undefined,
    onMessage: (msg: RedisMonitorMessage) => void,
    onError?: (err: string) => void,
  ) {
    const controller = new AbortController()
    const token = localStorage.getItem('nias-token') || ''
    const params = new URLSearchParams({ channel })
    if (db != null) params.set('db', String(db))
    ;(async () => {
      try {
        const resp = await fetch(`/api/connections/${connId}/redis/monitor?${params.toString()}`, {
          headers: token ? { Authorization: `Bearer ${token}` } : {},
          signal: controller.signal,
        })
        if (!resp.ok || !resp.body) {
          onError?.(`Failed to subscribe (${resp.status})`)
          return
        }
        const reader = resp.body.getReader()
        const dec = new TextDecoder()
        let buf = ''
        while (true) {
          const { done, value } = await reader.read()
          if (done) break
          buf += dec.decode(value, { stream: true })
          const parts = buf.split('\n\n')
          buf = parts.pop() ?? ''
          for (const part of parts) {
            const lines = part.split('\n')
            const isError = lines.some((l) => l.startsWith('event: error'))
            for (const line of lines) {
              if (!line.startsWith('data: ')) continue
              const raw = line.slice(6)
              if (isError) {
                onError?.(raw)
                continue
              }
              try {
                onMessage(JSON.parse(raw) as RedisMonitorMessage)
              } catch {
                /* ignore malformed frame */
              }
            }
          }
        }
      } catch (err) {
        if (!controller.signal.aborted) onError?.(err instanceof Error ? err.message : 'Monitor stream failed')
      }
    })()
    return () => controller.abort()
  }

  return { ping, fetchKeys, fetchValue, saveKey, deleteKey, renameKey, moveKey, runCommand, generateScript, executeScript, setTtl, fetchInfo, fetchSlowlog, monitor }
}
