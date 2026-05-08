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

  async function fetchValue(connId: number, key: string, db?: number) {
    const { data } = await axios.get<RedisValueResponse>(`/api/connections/${connId}/redis/key`, {
      params: { key, db },
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

  return { ping, fetchKeys, fetchValue, saveKey, deleteKey, renameKey, moveKey, runCommand, generateScript, executeScript }
}
