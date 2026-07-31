import { ref } from 'vue'
import axios from 'axios'

export interface KubeCluster {
  id: number
  name: string
  provider: 'alibaba' | 'huawei' | 'other'
  context: string
  owner_id: number
  created_at: string
}

export interface KubeClusterInput {
  name: string
  provider: 'alibaba' | 'huawei' | 'other'
  context: string
  kubeconfig: string
}

export interface KubeSummary {
  cluster_id: number
  name: string
  provider: string
  reachable: boolean
  version: string
  nodes: number
  namespaces: number
  pods: number
  error?: string
}

const clusters = ref<KubeCluster[]>([])
const loading = ref(false)

async function fetchClusters(): Promise<void> {
  loading.value = true
  try {
    const { data } = await axios.get<KubeCluster[]>('/api/kube/clusters')
    clusters.value = data ?? []
  } finally {
    loading.value = false
  }
}

async function createCluster(input: KubeClusterInput): Promise<number | null> {
  const { data } = await axios.post<{ id: number }>('/api/kube/clusters', input)
  return data?.id ?? null
}

async function updateCluster(id: number, input: KubeClusterInput): Promise<boolean> {
  await axios.put(`/api/kube/clusters/${id}`, input)
  return true
}

async function deleteCluster(id: number): Promise<boolean> {
  await axios.delete(`/api/kube/clusters/${id}`)
  return true
}

export function useKubeClusters() {
  return { clusters, loading, fetchClusters, createCluster, updateCluster, deleteCluster }
}
