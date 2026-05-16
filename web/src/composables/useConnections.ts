import { ref } from 'vue'
import axios from 'axios'
import { readableError } from '@/utils/httpError'

export type DbDriver =
  | 'sqlite'
  | 'postgres'
  | 'mysql'
  | 'mariadb'
  | 'mssql'
  | 'redis'
  | 'memcache'
  | 'kafka'
  | 'mongodb'
  | 'cassandra'
  | 'elasticsearch'
  | 'opensearch'
  | 's3_aws'
  | 's3_gcp'
  | 's3_oss'
  | 's3_obs'

export interface Connection {
  id: number
  name: string
  driver: DbDriver
  host: string
  port: number
  database: string
  username: string
  ssl: boolean
  tags: string
  ssh_host: string
  ssh_port: number
  ssh_user: string
  folder_id: number | null
  visibility: 'private' | 'shared'
  owner_id: number
  disconnected: boolean
  created_at: string
}

export interface ConnectionForm {
  name: string
  driver: DbDriver
  host: string
  port: number
  database: string
  username: string
  password: string
  ssl: boolean
  tags: string
  ssh_host: string
  ssh_port: number
  ssh_user: string
  ssh_password: string
  ssh_key: string
  folder_id?: number | null
  visibility?: 'private' | 'shared'
}

const connections = ref<Connection[]>([])
const loading = ref(false)
const error = ref('')

export function useConnections() {
  async function fetchConnections() {
    loading.value = true
    try {
      const { data } = await axios.get<Connection[]>('/api/connections')
      connections.value = data
    } catch (e) {
      error.value = readableError(e, { action: 'Load connections', fallback: 'Failed to load connections' })
    } finally {
      loading.value = false
    }
  }

  async function testConnection(form: ConnectionForm): Promise<{ ok: boolean; message: string }> {
    try {
      const { data } = await axios.post('/api/connections/test', form)
      return { ok: true, message: data.message ?? 'Connection successful' }
    } catch (e: unknown) {
      const msg = readableError(e, { action: 'Test connection', fallback: 'Connection failed' })
      return { ok: false, message: msg }
    }
  }

  async function saveConnection(form: ConnectionForm): Promise<Connection | null> {
    try {
      const { data } = await axios.post<Connection>('/api/connections', form)
      connections.value.push(data)
      return data
    } catch (e) {
      error.value = readableError(e, { action: 'Save connection', fallback: 'Failed to save connection' })
      return null
    }
  }

  async function updateConnection(id: number, form: Partial<ConnectionForm>): Promise<boolean> {
    try {
      const { data } = await axios.put<Connection>(`/api/connections/${id}`, form)
      const idx = connections.value.findIndex((c) => c.id === id)
      if (idx !== -1) connections.value[idx] = data
      return true
    } catch (e) {
      error.value = readableError(e, { action: 'Update connection', fallback: 'Failed to update connection' })
      return false
    }
  }

  async function removeConnection(id: number): Promise<boolean> {
    try {
      await axios.delete(`/api/connections/${id}`)
      connections.value = connections.value.filter((c) => c.id !== id)
      return true
    } catch {
      return false
    }
  }

  async function disconnectConnection(id: number): Promise<boolean> {
    try {
      await axios.post(`/api/connections/${id}/disconnect`)
      const idx = connections.value.findIndex((c) => c.id === id)
      if (idx !== -1) connections.value[idx].disconnected = true
      return true
    } catch {
      return false
    }
  }

  async function reconnectConnection(id: number): Promise<boolean> {
    try {
      await axios.post(`/api/connections/${id}/reconnect`)
      const idx = connections.value.findIndex((c) => c.id === id)
      if (idx !== -1) connections.value[idx].disconnected = false
      return true
    } catch {
      return false
    }
  }

  return {
    connections,
    loading,
    error,
    fetchConnections,
    testConnection,
    saveConnection,
    updateConnection,
    removeConnection,
    disconnectConnection,
    reconnectConnection,
  }
}
