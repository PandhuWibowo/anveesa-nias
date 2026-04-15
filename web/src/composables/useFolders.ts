import { ref } from 'vue'
import axios from 'axios'

export interface ConnectionFolder {
  id: number
  name: string
  parent_id: number | null
  owner_id: number
  visibility: 'private' | 'shared'
  color: string
  sort_order: number
  created_at: string
}

export interface FolderForm {
  name: string
  parent_id: number | null
  visibility: 'private' | 'shared'
  color: string
  sort_order?: number
}

const folders = ref<ConnectionFolder[]>([])
const loading = ref(false)

export function useFolders() {
  async function fetchFolders() {
    loading.value = true
    try {
      const { data } = await axios.get<ConnectionFolder[]>('/api/folders')
      folders.value = data
    } catch {
      folders.value = []
    } finally {
      loading.value = false
    }
  }

  async function createFolder(form: FolderForm): Promise<ConnectionFolder | null> {
    try {
      const { data } = await axios.post<ConnectionFolder>('/api/folders', form)
      await fetchFolders()
      return data
    } catch (e: any) {
      console.error('createFolder failed:', e?.response?.data ?? e)
      return null
    }
  }

  async function updateFolder(id: number, form: Partial<FolderForm>): Promise<boolean> {
    try {
      await axios.put(`/api/folders/${id}`, form)
      await fetchFolders()
      return true
    } catch {
      return false
    }
  }

  async function deleteFolder(id: number): Promise<boolean> {
    try {
      await axios.delete(`/api/folders/${id}`)
      await fetchFolders()
      return true
    } catch {
      return false
    }
  }

  async function moveConnection(connId: number, folderId: number | null, visibility?: string): Promise<void> {
    await axios.patch(`/api/connections/${connId}/folder`, { folder_id: folderId, visibility })
  }

  async function setConnectionVisibility(connId: number, visibility: 'private' | 'shared'): Promise<void> {
    await axios.patch(`/api/connections/${connId}/visibility`, { visibility })
  }

  return {
    folders,
    loading,
    fetchFolders,
    createFolder,
    updateFolder,
    deleteFolder,
    moveConnection,
    setConnectionVisibility,
  }
}
