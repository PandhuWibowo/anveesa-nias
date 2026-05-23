import { ref } from 'vue'
import axios from 'axios'
import { readableError } from '@/utils/httpError'

export interface SchemaTable {
  name: string
  type: 'table' | 'view'
  row_count?: number
}

export interface SchemaObjectItem {
  name: string
  type: string
  parent_name?: string
  summary?: string
}

export interface SchemaObjectGroup {
  key: string
  label: string
  items: SchemaObjectItem[]
}

export interface SchemaMetadataCatalog {
  database: string
  groups: SchemaObjectGroup[]
}

export interface SchemaColumn {
  name: string
  data_type: string
  is_nullable: boolean
  is_primary_key: boolean
  default_value?: string
}

export interface SchemaProperty {
  label: string
  value: string
}

export interface SchemaIndexDetail {
  name: string
  table_name: string
  method: string
  is_unique: boolean
  is_primary: boolean
  columns: string[]
  definition: string
}

export interface SchemaConstraintDetail {
  name: string
  constraint_type: string
  columns: string[]
  definition: string
  referenced_table?: string
}

export interface SchemaTriggerDetail {
  name: string
  table_name: string
  timing: string
  events: string
  definition: string
}

export interface SchemaSequenceDetail {
  name: string
  start_value: string
  increment_by: string
  min_value: string
  max_value: string
  cache_size: string
  cycle: boolean
  owned_by?: string
  definition?: string
}

export interface SchemaRoutineDetail {
  name: string
  routine_type: string
  identity: string
  return_type?: string
  definition: string
}

export interface SchemaObjectDetail {
  type: string
  name: string
  database: string
  ddl: string
  properties: SchemaProperty[]
  columns: SchemaColumn[]
  indexes: SchemaIndexDetail[]
  constraints: SchemaConstraintDetail[]
  triggers: SchemaTriggerDetail[]
  sequences: SchemaSequenceDetail[]
  routine?: SchemaRoutineDetail
  enum_values?: string[]
  dependencies: SchemaProperty[]
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
  const metadata = ref<SchemaMetadataCatalog | null>(null)
  const objectDetail = ref<SchemaObjectDetail | null>(null)
  const error = ref('')

  async function fetchSchema(connId: number, options: { refresh?: boolean } = {}) {
    loadingSchema.value = true
    error.value = ''
    try {
      const { data } = await axios.get<SchemaDatabase[]>(`/api/connections/${connId}/schema`, {
        params: options.refresh ? { refresh: 1 } : undefined,
      })
      databases.value = Array.isArray(data)
        ? data.map((db) => ({ ...db, tables: Array.isArray(db.tables) ? db.tables : [] }))
        : []
    } catch (err) {
      error.value = readableError(err, { action: 'Load schema', fallback: 'Failed to load schema' })
      databases.value = []
    } finally {
      loadingSchema.value = false
    }
  }

  async function fetchColumns(connId: number, db: string, table: string) {
    error.value = ''
    try {
      const url = `/api/connections/${connId}/schema/${db}/tables/${table}/columns`
      const { data } = await axios.get<SchemaColumn[]>(url)
      columns.value = Array.isArray(data) ? data : []
    } catch (err: any) {
      error.value = readableError(err, { action: 'Load columns', fallback: 'Failed to load columns' })
      columns.value = []
    }
  }

  async function fetchMetadata(connId: number, db: string) {
    error.value = ''
    try {
      const { data } = await axios.get<SchemaMetadataCatalog>(
        `/api/connections/${connId}/schema/${encodeURIComponent(db)}/metadata`,
      )
      metadata.value = data
      return data
    } catch (err) {
      error.value = readableError(err, { action: 'Load schema metadata', fallback: 'Failed to load schema metadata' })
      metadata.value = null
      return null
    }
  }

  async function fetchObjectDetail(connId: number, db: string, type: string, name: string) {
    error.value = ''
    try {
      const { data } = await axios.get<SchemaObjectDetail>(
        `/api/connections/${connId}/schema/${encodeURIComponent(db)}/object-detail`,
        { params: { type, name } },
      )
      objectDetail.value = data
      return data
    } catch (err) {
      error.value = readableError(err, { action: 'Load object detail', fallback: 'Failed to load object detail' })
      objectDetail.value = null
      return null
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
    error.value = ''
    try {
      const { data } = await axios.get(`/api/connections/${connId}/schema/${db}/tables/${table}/data`, {
        params: { page, page_size: pageSize, order_by: orderBy, order_dir: orderDir },
      })
      return data
    } catch (err) {
      error.value = readableError(err, { action: 'Load table data', fallback: 'Failed to load table data' })
      return null
    }
  }

  async function fetchTableColumns(connId: number, db: string, table: string): Promise<SchemaColumn[]> {
    error.value = ''
    try {
      const { data } = await axios.get<SchemaColumn[]>(
        `/api/connections/${connId}/schema/${db}/tables/${table}/columns`,
      )
      return data
    } catch (err) {
      error.value = readableError(err, { action: 'Load table columns', fallback: 'Failed to load table columns' })
      return []
    }
  }

  return {
    databases,
    loadingSchema,
    columns,
    metadata,
    objectDetail,
    error,
    fetchSchema,
    fetchColumns,
    fetchMetadata,
    fetchObjectDetail,
    fetchTableData,
    fetchTableColumns,
  }
}
