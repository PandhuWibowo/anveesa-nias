import axios from 'axios'

export interface CassandraDashboardData {
  driver: string
  keyspace: string
  cluster_name: string
  version: string
  cql_version: string
  data_center: string
  rack: string
  host_id: string
  keyspaces: number
  tables: number
  native_protocol: string
  local: Record<string, any>
}

export interface CassandraKeyspaceSummary {
  name: string
  replication: string
  durable_writes: boolean
  table_count: number
}

export interface CassandraTableSummary {
  keyspace_name: string
  name: string
  comment: string
  columns: number
  partition_key: string
  clustering_key: string
}

export interface CassandraColumnSummary {
  keyspace_name: string
  table_name: string
  name: string
  type: string
  kind: string
  position: number
}

export interface CassandraResult {
  columns: string[]
  rows: Record<string, any>[]
  row_count: number
  applied: boolean
  duration_ms: number
}

export function useCassandra() {
  async function dashboard(connId: number) {
    const { data } = await axios.get<CassandraDashboardData>(`/api/connections/${connId}/cassandra/dashboard`)
    return data
  }

  async function keyspaces(connId: number) {
    const { data } = await axios.get<CassandraKeyspaceSummary[]>(`/api/connections/${connId}/cassandra/keyspaces`)
    return data
  }

  async function tables(connId: number, keyspace: string) {
    const { data } = await axios.get<CassandraTableSummary[]>(`/api/connections/${connId}/cassandra/tables`, {
      params: { keyspace },
    })
    return data
  }

  async function columns(connId: number, keyspace: string, table: string) {
    const { data } = await axios.get<CassandraColumnSummary[]>(`/api/connections/${connId}/cassandra/columns`, {
      params: { keyspace, table },
    })
    return data
  }

  async function rows(connId: number, keyspace: string, table: string, limit = 100) {
    const { data } = await axios.get<CassandraResult>(`/api/connections/${connId}/cassandra/rows`, {
      params: { keyspace, table, limit },
    })
    return data
  }

  async function query(connId: number, keyspace: string, cql: string, limit = 100) {
    const { data } = await axios.post<CassandraResult>(`/api/connections/${connId}/cassandra/query`, {
      keyspace,
      cql,
      limit,
    })
    return data
  }

  return { dashboard, keyspaces, tables, columns, rows, query }
}
