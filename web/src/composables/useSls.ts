import axios from 'axios'

export interface SlsField {
  name: string
  type: string
}

export interface SlsFieldsResponse {
  indexed: boolean
  full_text: boolean
  fields: SlsField[]
}

export interface SlsBucket {
  from: number
  to: number
  count: number
}

export interface SlsHistogramResponse {
  count: number
  complete: boolean
  buckets: SlsBucket[]
}

export type SlsRow = Record<string, unknown>

export interface SlsLogsResponse {
  rows: SlsRow[]
  count: number
  complete: boolean
  elapsed_ms: number
  has_more: boolean
}

export interface SlsSearch {
  logstore: string
  query: string
  from: number
  to: number
}

const base = (connId: number) => `/api/connections/${connId}/sls`

export function useSls() {
  async function logstores(connId: number) {
    const { data } = await axios.get<{ project: string; logstores: string[] }>(`${base(connId)}/logstores`)
    return data
  }

  async function fields(connId: number, logstore: string) {
    const { data } = await axios.get<SlsFieldsResponse>(`${base(connId)}/fields`, { params: { logstore } })
    return data
  }

  async function histogram(connId: number, s: SlsSearch, signal?: AbortSignal) {
    const { data } = await axios.get<SlsHistogramResponse>(`${base(connId)}/histogram`, { params: s, signal })
    return data
  }

  async function logs(connId: number, s: SlsSearch & { offset?: number; line?: number }, signal?: AbortSignal) {
    const { data } = await axios.get<SlsLogsResponse>(`${base(connId)}/logs`, { params: s, signal })
    return data
  }

  return { logstores, fields, histogram, logs }
}
