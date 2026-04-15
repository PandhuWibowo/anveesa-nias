import { ref } from 'vue'
import axios from 'axios'

export type DbDriver = 'postgres' | 'mysql' | 'mariadb' | 'sqlite' | 'mssql'

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
    } catch {
      error.value = 'Failed to load connections'
    } finally {
      loading.value = false
    }
  }

  async function testConnection(form: ConnectionForm): Promise<{ ok: boolean; message: string }> {
    try {
      const { data } = await axios.post('/api/connections/test', form)
      return { ok: true, message: data.message ?? 'Connection successful' }
    } catch (e: unknown) {
      const msg = (e as { response?: { data?: { error?: string } } })?.response?.data?.error ?? 'Connection failed'
      return { ok: false, message: msg }
    }
  }

  async function saveConnection(form: ConnectionForm): Promise<Connection | null> {
    try {
      const { data } = await axios.post<Connection>('/api/connections', form)
      connections.value.push(data)
      return data
    } catch {
      return null
    }
  }

  async function updateConnection(id: number, form: Partial<ConnectionForm>): Promise<boolean> {
    try {
      const { data } = await axios.put<Connection>(`/api/connections/${id}`, form)
      const idx = connections.value.findIndex((c) => c.id === id)
      if (idx !== -1) connections.value[idx] = data
      return true
    } catch {
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

  return {
    connections,
    loading,
    error,
    fetchConnections,
    testConnection,
    saveConnection,
    updateConnection,
    removeConnection,
  }
}
