import { ref } from 'vue'
import axios from 'axios'

export interface PipelineNode {
  id: number
  pipeline_id: number
  node_type: string
  connection_id: number | null
  config: Record<string, any>
  position_x: number
  position_y: number
  label: string
}

export interface PipelineEdge {
  id: number
  pipeline_id: number
  source_node_id: number
  target_node_id: number
}

export interface Pipeline {
  id: number
  name: string
  description: string
  pipeline_type: string
  created_by: number | null
  status: string
  schedule: string | null
  api_enabled: boolean
  last_run_at: string | null
  created_at: string
  updated_at: string
  nodes?: PipelineNode[]
  edges?: PipelineEdge[]
}

export interface PipelineRun {
  id: number
  pipeline_id: number
  triggered_by: string
  status: string
  business_date: string
  run_params: Record<string, any>
  parent_run_id: number | null
  return_payload: Record<string, any>
  started_at: string
  finished_at: string | null
  rows_processed: number
  error_message: string | null
}

export interface PipelineRunLog {
  id: number
  run_id: number
  node_id: number | null
  node_label: string
  message: string
  rows_affected: number
  duration_ms: number
  logged_at: string
}

export function usePipelines() {
  const pipelines = ref<Pipeline[]>([])
  const loading = ref(false)
  const error = ref('')

  async function fetchPipelines() {
    loading.value = true
    error.value = ''
    try {
      const { data } = await axios.get<Pipeline[]>('/api/pipelines')
      pipelines.value = data
    } catch (e: any) {
      error.value = e.response?.data?.error || e.message
    } finally {
      loading.value = false
    }
  }

  async function createPipeline(name: string, description = '', pipeline_type = 'custom') {
    const { data } = await axios.post<{ id: number }>('/api/pipelines', { name, description, pipeline_type })
    return data.id
  }

  async function getPipeline(id: number): Promise<Pipeline> {
    const { data } = await axios.get<Pipeline>(`/api/pipelines/${id}`)
    return data
  }

  async function savePipeline(id: number, payload: Partial<Pipeline> & { nodes: any[]; edges: any[] }) {
    await axios.put(`/api/pipelines/${id}`, payload)
  }

  async function deletePipeline(id: number) {
    await axios.delete(`/api/pipelines/${id}`)
  }

  async function triggerRun(id: number, payload: { business_date?: string; params?: Record<string, any>; parent_run_id?: number | null } = {}): Promise<number> {
    const { data } = await axios.post<{ run_id: number }>(`/api/pipelines/${id}/run`, payload)
    return data.run_id
  }

  async function rerunRun(pipelineId: number, runId: number, payload: { business_date?: string; params?: Record<string, any> } = {}): Promise<number> {
    const { data } = await axios.post<{ run_id: number }>(`/api/pipelines/${pipelineId}/runs/${runId}/rerun`, payload)
    return data.run_id
  }

  async function fetchRuns(id: number): Promise<PipelineRun[]> {
    const { data } = await axios.get<PipelineRun[]>(`/api/pipelines/${id}/runs`)
    return data
  }

  async function fetchRunStatus(pipelineId: number, runId: number): Promise<PipelineRun> {
    const { data } = await axios.get<PipelineRun>(`/api/pipelines/${pipelineId}/runs/${runId}`)
    return data
  }

  async function fetchRunLogs(pipelineId: number, runId: number): Promise<PipelineRunLog[]> {
    const { data } = await axios.get<PipelineRunLog[]>(`/api/pipelines/${pipelineId}/runs/${runId}/logs`)
    return data
  }

  return {
    pipelines,
    loading,
    error,
    fetchPipelines,
    createPipeline,
    getPipeline,
    savePipeline,
    deletePipeline,
    triggerRun,
    rerunRun,
    fetchRuns,
    fetchRunStatus,
    fetchRunLogs,
  }
}
