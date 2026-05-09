import axios from 'axios'

export interface LaravelQueueSummary {
  name: string
  ready: number
  delayed: number
  reserved: number
  notify: boolean
}

export interface LaravelQueueJob {
  id: string
  state: 'ready' | 'delayed' | 'reserved'
  queue: string
  uuid?: string
  display_name?: string
  job?: string
  command_name?: string
  attempts: number
  max_tries?: number
  timeout?: number
  backoff?: unknown
  score?: number
  available_at?: string
  payload?: Record<string, unknown>
  raw: string
}

export interface LaravelFailedJob {
  id: number
  uuid?: string
  connection: string
  queue: string
  payload?: Record<string, unknown>
  raw_payload: string
  exception: string
  failed_at: string
}

export interface LaravelHorizonSummary {
  detected: boolean
  key_count: number
  supervisors: number
  masters: number
  recent_jobs: number
  failed_jobs: number
  workload?: Record<string, number>
  sample_keys: string[]
}

export interface LaravelQueueFeatureFlags {
  retry: boolean
  delete: boolean
  clear: boolean
  editedReplay: boolean
  readOnly: boolean
  requireConfirm: boolean
}

export interface LaravelQueueRules {
  readyMax: number
  failedMax: number
  stuckMax: number
  oldestMinutesMax: number
  noConsumption: boolean
}

export interface LaravelQueueOpsSettings {
  featureFlags: LaravelQueueFeatureFlags
  queueRules: LaravelQueueRules
  businessFieldsInput: string
  sandboxQueue: string
  environment?: string
  updated_at?: string
  updated_by?: number
}

export interface LaravelQueueAuditItem {
  id: number
  conn_id: number
  failed_conn_id: number
  user_id: number
  username: string
  action: string
  queue: string
  target_queue: string
  job_uuid: string
  failed_job_id: number
  payload_edited: boolean
  status: string
  error: string
  details: Record<string, unknown>
  created_at: string
}

export interface LaravelQueueQuarantineItem {
  id: number
  conn_id: number
  failed_conn_id: number
  failed_job_id: number
  uuid: string
  queue: string
  job_name: string
  payload: string
  exception: string
  reason: string
  status: string
  created_by: number
  created_at: string
  updated_at: string
}

export function useLaravelQueue() {
  async function fetchQueues(connId: number, params: { db?: number; prefix?: string }) {
    const { data } = await axios.get<LaravelQueueSummary[]>(`/api/connections/${connId}/laravel-queue/queues`, {
      params,
    })
    return data
  }

  async function fetchJobs(connId: number, params: { queue: string; db?: number; prefix?: string; limit?: number }) {
    const { data } = await axios.get<{ queue: string; jobs: LaravelQueueJob[] }>(`/api/connections/${connId}/laravel-queue/jobs`, {
      params,
    })
    return data
  }

  async function deleteJob(connId: number, payload: { queue: string; prefix?: string; db?: number; state: LaravelQueueJob['state']; raw: string }) {
    await axios.post(`/api/connections/${connId}/laravel-queue/delete`, payload)
  }

  async function requeueJob(connId: number, payload: { queue: string; prefix?: string; db?: number; state: LaravelQueueJob['state']; raw: string }) {
    await axios.post(`/api/connections/${connId}/laravel-queue/requeue`, payload)
  }

  async function clearQueue(connId: number, payload: { queue: string; prefix?: string; db?: number; state?: 'all' | LaravelQueueJob['state'] | 'notify' }) {
    await axios.post(`/api/connections/${connId}/laravel-queue/clear`, payload)
  }

  async function fetchFailedJobs(connId: number, limit = 100) {
    const { data } = await axios.get<LaravelFailedJob[]>(`/api/connections/${connId}/laravel-queue/failed-jobs`, {
      params: { limit },
    })
    return data
  }

  async function retryFailedJob(connId: number, payload: { id: number; redis_conn_id: number; redis_db?: number; prefix?: string; queue: string; payload: string; delete_after: boolean; payload_edited?: boolean }) {
    await axios.post(`/api/connections/${connId}/laravel-queue/retry-failed`, payload)
  }

  async function deleteFailedJob(connId: number, id: number, redisConnId?: number) {
    await axios.post(`/api/connections/${connId}/laravel-queue/delete-failed`, { id, redis_conn_id: redisConnId })
  }

  async function fetchHorizon(connId: number, db?: number) {
    const { data } = await axios.get<LaravelHorizonSummary>(`/api/connections/${connId}/laravel-queue/horizon`, {
      params: { db },
    })
    return data
  }

  async function fetchOpsSettings(connId: number) {
    const { data } = await axios.get<LaravelQueueOpsSettings>(`/api/connections/${connId}/laravel-queue/ops-settings`)
    return data
  }

  async function saveOpsSettings(connId: number, payload: LaravelQueueOpsSettings) {
    const { data } = await axios.put<LaravelQueueOpsSettings>(`/api/connections/${connId}/laravel-queue/ops-settings`, payload)
    return data
  }

  async function fetchQueueAudit(connId: number, limit = 100) {
    const { data } = await axios.get<LaravelQueueAuditItem[]>(`/api/connections/${connId}/laravel-queue/audit`, { params: { limit } })
    return data
  }

  async function fetchQuarantine(connId: number) {
    const { data } = await axios.get<LaravelQueueQuarantineItem[]>(`/api/connections/${connId}/laravel-queue/quarantine`)
    return data
  }

  async function quarantineFailedJob(connId: number, payload: { failed_conn_id: number; failed_job_id: number; uuid?: string; queue: string; job_name: string; payload: string; exception: string; reason?: string }) {
    await axios.post(`/api/connections/${connId}/laravel-queue/quarantine`, payload)
  }

  async function releaseQuarantine(connId: number, id: number) {
    await axios.delete(`/api/connections/${connId}/laravel-queue/quarantine/${id}`)
  }

  async function emitQueueAlerts(connId: number, payload: { queue: string; prefix: string; alerts: Array<{ level: string; title: string; detail: string }> }) {
    await axios.post(`/api/connections/${connId}/laravel-queue/alerts`, payload)
  }

  async function runLaravelAgent(connId: number, payload: { command: string; queue?: string; options?: Record<string, unknown> }) {
    await axios.post(`/api/connections/${connId}/laravel-queue/agent`, payload)
  }

  return {
    fetchQueues,
    fetchJobs,
    deleteJob,
    requeueJob,
    clearQueue,
    fetchFailedJobs,
    retryFailedJob,
    deleteFailedJob,
    fetchHorizon,
    fetchOpsSettings,
    saveOpsSettings,
    fetchQueueAudit,
    fetchQuarantine,
    quarantineFailedJob,
    releaseQuarantine,
    emitQueueAlerts,
    runLaravelAgent,
  }
}
