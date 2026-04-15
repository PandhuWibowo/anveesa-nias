import { ref } from 'vue'
import axios from 'axios'

export interface SchemaTable {
  name: string
  type: 'table' | 'view'
  row_count?: number
}

export interface SchemaColumn {
  name: string
  data_type: string
  is_nullable: boolean
  is_primary_key: boolean
  default_value?: string
}

export interface SchemaDatabase {
  name: string
  tables: SchemaTable[]
}

export function useSchema() {
  // All state is per-instance so multiple sessions never share schema data
  const databases = ref<SchemaDatabase[]>([])
  const loadingSchema = ref(false)
  const columns = ref<SchemaColumn[]>([])

  async function fetchSchema(connId: number) {
    loadingSchema.value = true
    try {
      const { data } = await axios.get<SchemaDatabase[]>(`/api/connections/${connId}/schema`)
      databases.value = data
    } finally {
      loadingSchema.value = false
    }
  }

  async function fetchColumns(connId: number, db: string, table: string) {
    try {
      const { data } = await axios.get<SchemaColumn[]>(
        `/api/connections/${connId}/schema/${db}/tables/${table}/columns`,
      )
      columns.value = data
    } catch {
      columns.value = []
    }
  }

  async function fetchTableData(
    connId: number,
    db: string,
    table: string,
    page = 1,
    pageSize = 100,
    orderBy?: string,
    orderDir: 'asc' | 'desc' = 'asc',
  ) {
    try {
      const { data } = await axios.get(`/api/connections/${connId}/schema/${db}/tables/${table}/data`, {
        params: { page, page_size: pageSize, order_by: orderBy, order_dir: orderDir },
      })
      return data
    } catch {
      return null
    }
  }

  async function fetchTableColumns(connId: number, db: string, table: string): Promise<SchemaColumn[]> {
    try {
      const { data } = await axios.get<SchemaColumn[]>(
        `/api/connections/${connId}/schema/${db}/tables/${table}/columns`,
      )
      return data
    } catch {
      return []
    }
  }

  return {
    databases,
    loadingSchema,
    columns,
    fetchSchema,
    fetchColumns,
    fetchTableData,
    fetchTableColumns,
  }
}
