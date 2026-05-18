import axios from 'axios'

export interface MongoDashboardData {
  driver: string
  database: string
  version: string
  size_bytes: number
  collections: number
  objects: number
  indexes: number
  storage_size: number
  data_size: number
  server: Record<string, any>
  collections_top: MongoCollectionStat[]
}

export interface MongoDatabaseSummary {
  name: string
  size_bytes: number
  empty: boolean
}

export interface MongoCollectionStat {
  name: string
  type: string
  count: number
  size_bytes: number
  storage_size: number
  indexes: number
  index_size: number
}

export interface MongoDocumentList {
  documents: any[]
  limit: number
  page: number
  skip: number
  count: number
  has_next: boolean
}

export interface MongoIndexSummary {
  name: string
  keys: any
  spec: any
}

export interface MongoSchemaField {
  path: string
  count: number
  occurrence: number
  types: string[]
  examples: string[]
}

export interface MongoSchemaAnalysis {
  sample_size: number
  fields: MongoSchemaField[]
}

export interface MongoHealthData {
  status: string
  version: string
  storage_engine: string
  server: Record<string, any>
  replica_set: Record<string, any>
  shards: Record<string, any>[]
  current_ops: any[]
}

export interface MongoQueryEntry {
  id: number
  name: string
  conn_id?: number | null
  payload: string
  description: string
  created_at: string
  updated_at: string
}

export interface MongoIndexRecommendation {
  field: string
  direction: number
  reason: string
  score: number
}

export function useMongoDB() {
  async function dashboard(connId: number) {
    const { data } = await axios.get<MongoDashboardData>(`/api/connections/${connId}/mongodb/dashboard`)
    return data
  }

  async function databases(connId: number) {
    const { data } = await axios.get<MongoDatabaseSummary[]>(`/api/connections/${connId}/mongodb/databases`)
    return data
  }

  async function health(connId: number) {
    const { data } = await axios.get<MongoHealthData>(`/api/connections/${connId}/mongodb/health`)
    return data
  }

  async function collections(connId: number, database?: string) {
    const { data } = await axios.get<MongoCollectionStat[]>(`/api/connections/${connId}/mongodb/collections`, {
      params: { database },
    })
    return data
  }

  async function createCollection(connId: number, database: string, collection: string) {
    const { data } = await axios.post(`/api/connections/${connId}/mongodb/collections`, { database, collection })
    return data
  }

  async function renameCollection(connId: number, database: string, collection: string, newName: string) {
    const { data } = await axios.put(`/api/connections/${connId}/mongodb/collections`, { database, collection, new_name: newName })
    return data
  }

  async function dropCollection(connId: number, database: string, collection: string) {
    const { data } = await axios.delete(`/api/connections/${connId}/mongodb/collections`, { params: { database, collection } })
    return data
  }

  async function documents(connId: number, database: string, collection: string, filter = '', limit = 50, sort = '', projection = '', page = 1) {
    const { data } = await axios.get<MongoDocumentList>(`/api/connections/${connId}/mongodb/documents`, {
      params: { database, collection, filter: filter || undefined, sort: sort || undefined, projection: projection || undefined, limit, page },
    })
    return data
  }

  async function insertDocument(connId: number, database: string, collection: string, document: string) {
    const { data } = await axios.post(`/api/connections/${connId}/mongodb/documents`, { database, collection, document: JSON.parse(document) })
    return data
  }

  async function replaceDocument(connId: number, database: string, collection: string, filter: string, document: string) {
    const { data } = await axios.put(`/api/connections/${connId}/mongodb/documents`, { database, collection, filter: JSON.parse(filter), document: JSON.parse(document) })
    return data
  }

  async function updateWithOperators(connId: number, database: string, collection: string, filter: string, update: string, mode: 'updateOne' | 'updateMany', preview = false) {
    const { data } = await axios.put(`/api/connections/${connId}/mongodb/documents`, { database, collection, filter: JSON.parse(filter), update: JSON.parse(update), mode, preview })
    return data
  }

  async function deleteDocument(connId: number, database: string, collection: string, filter: string, mode: 'deleteOne' | 'deleteMany' = 'deleteOne', preview = false) {
    const { data } = await axios.delete(`/api/connections/${connId}/mongodb/documents`, { data: { database, collection, filter: JSON.parse(filter), mode, preview } })
    return data
  }

  async function indexes(connId: number, database: string, collection: string) {
    const { data } = await axios.get<MongoIndexSummary[]>(`/api/connections/${connId}/mongodb/indexes`, {
      params: { database, collection },
    })
    return data
  }

  async function createIndex(connId: number, database: string, collection: string, keys: string, name = '', unique = false) {
    const { data } = await axios.post(`/api/connections/${connId}/mongodb/indexes`, { database, collection, keys: JSON.parse(keys), name, unique })
    return data
  }

  async function dropIndex(connId: number, database: string, collection: string, name: string) {
    const { data } = await axios.delete(`/api/connections/${connId}/mongodb/indexes`, { params: { database, collection, name } })
    return data
  }

  async function recommendIndexes(connId: number, database: string, collection: string, filter = '', sort = '') {
    const { data } = await axios.get<MongoIndexRecommendation[]>(`/api/connections/${connId}/mongodb/recommend-indexes`, {
      params: { database, collection, filter: filter || undefined, sort: sort || undefined },
    })
    return data
  }

  async function aggregate(connId: number, database: string, collection: string, pipeline: string, limit = 50) {
    const { data } = await axios.post<MongoDocumentList>(`/api/connections/${connId}/mongodb/aggregate`, {
      database,
      collection,
      pipeline: JSON.parse(pipeline),
      limit,
    })
    return data
  }

  async function explain(connId: number, database: string, collection: string, filter: string) {
    const { data } = await axios.post(`/api/connections/${connId}/mongodb/explain`, {
      database,
      collection,
      filter: filter ? JSON.parse(filter) : {},
    })
    return data
  }

  async function schema(connId: number, database: string, collection: string, filter = '', limit = 100) {
    const { data } = await axios.get<MongoSchemaAnalysis>(`/api/connections/${connId}/mongodb/schema`, {
      params: { database, collection, filter: filter || undefined, limit },
    })
    return data
  }

  async function importJson(connId: number, database: string, collection: string, documents: string) {
    const { data } = await axios.post(`/api/connections/${connId}/mongodb/import`, {
      database,
      collection,
      documents: JSON.parse(documents),
    })
    return data
  }

  async function exportData(connId: number, database: string, collection: string, filter = '', limit = 1000, format: 'json' | 'ndjson' | 'csv' = 'json') {
    const { data } = await axios.get(format === 'json' ? `/api/connections/${connId}/mongodb/export` : `/api/connections/${connId}/mongodb/export`, {
      params: { database, collection, filter: filter || undefined, limit, format },
      responseType: format === 'json' ? 'json' : 'text',
    })
    return data
  }

  async function exportJson(connId: number, database: string, collection: string, filter = '', limit = 1000) {
    return exportData(connId, database, collection, filter, limit, 'json') as Promise<any[]>
  }

  async function savedQueries(connId: number) {
    const { data } = await axios.get<MongoQueryEntry[]>(`/api/connections/${connId}/mongodb/queries`)
    return data
  }

  async function saveQuery(connId: number, payload: { name: string; database: string; collection: string; filter?: any; sort?: any; projection?: any; pipeline?: any; description?: string }) {
    const { data } = await axios.post(`/api/connections/${connId}/mongodb/queries`, payload)
    return data
  }

  async function deleteSavedQuery(connId: number, id: number) {
    await axios.delete(`/api/connections/${connId}/mongodb/queries/${id}`)
  }

  return { dashboard, databases, health, collections, createCollection, renameCollection, dropCollection, documents, insertDocument, replaceDocument, updateWithOperators, deleteDocument, indexes, createIndex, dropIndex, recommendIndexes, aggregate, explain, schema, importJson, exportData, exportJson, savedQueries, saveQuery, deleteSavedQuery }
}
