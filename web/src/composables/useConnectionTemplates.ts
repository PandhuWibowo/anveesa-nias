import { ref } from 'vue'
import axios from 'axios'

export interface ConnectionTemplate {
  id: number
  name: string
  driver: string
  host: string
  port: number
  database: string
  ssl: boolean
  tags: string
  ssh_host: string
  ssh_port: number
  ssh_user: string
  description: string
  visibility: string
  owner_id: number
  created_at: string
  updated_at: string
}

export interface ConnectionTemplateInput {
  name: string
  driver: string
  host: string
  port: number
  database: string
  ssl: boolean
  tags: string
  ssh_host: string
  ssh_port: number
  ssh_user: string
  description: string
  visibility: string
}

const templates = ref<ConnectionTemplate[]>([])
const loading = ref(false)

async function fetchTemplates() {
  loading.value = true
  try {
    const { data } = await axios.get('/api/connection-templates')
    templates.value = data
  } catch {
    templates.value = []
  } finally {
    loading.value = false
  }
}

async function createTemplate(input: ConnectionTemplateInput): Promise<ConnectionTemplate | null> {
  try {
    const { data } = await axios.post('/api/connection-templates', input)
    templates.value = [...templates.value, data].sort((a, b) => a.name.localeCompare(b.name))
    return data
  } catch {
    return null
  }
}

async function deleteTemplate(id: number): Promise<boolean> {
  try {
    await axios.delete(`/api/connection-templates/${id}`)
    templates.value = templates.value.filter(t => t.id !== id)
    return true
  } catch {
    return false
  }
}

export function useConnectionTemplates() {
  return { templates, loading, fetchTemplates, createTemplate, deleteTemplate }
}
