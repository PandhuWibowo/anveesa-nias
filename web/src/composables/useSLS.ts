import axios from 'axios'

export type SLSLogRow = Record<string, string>

export interface SLSQueryPayload {
  logstore: string
  query: string
  from: number
  to: number
  line?: number
  offset?: number
}

export interface SLSLogsResponse {
  logs: SLSLogRow[]
  count: number
  progress: string
  processed_rows: number
  has_sql: boolean
  keys: string[] | null
  took_ms: number
}

export interface SLSHistogramBucket {
  from: number
  to: number
  count: number
  progress: string
}

export interface SLSHistogramResponse {
  buckets: SLSHistogramBucket[]
  total: number
  progress: string
}

export function useSLS() {
  async function logstores(connId: number) {
    const { data } = await axios.get<{ project: string; logstores: string[] }>(`/api/connections/${connId}/sls/logstores`)
    return data
  }

  async function logs(connId: number, payload: SLSQueryPayload) {
    const { data } = await axios.post<SLSLogsResponse>(`/api/connections/${connId}/sls/logs`, payload)
    return data
  }

  async function histogram(connId: number, payload: SLSQueryPayload) {
    const { data } = await axios.post<SLSHistogramResponse>(`/api/connections/${connId}/sls/histogram`, payload)
    return data
  }

  return { logstores, logs, histogram }
}
