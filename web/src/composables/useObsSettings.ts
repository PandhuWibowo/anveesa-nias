import { ref, watch } from 'vue'
import axios from 'axios'

// ── Preset definitions ────────────────────────────────────────────────────────
export interface ObsPreset {
  id: string
  label: string
  desc: string
  icon: string
  logIndex: string
  serviceField: string
  envField: string
  errorKeywords: string
  metricIndex: string
  cpuField: string
  memField: string
}

export const OBS_PRESETS: ObsPreset[] = [
  {
    id: 'filebeat',
    label: 'Filebeat',
    desc: 'Container logs shipped via Filebeat agent. Uses app_name and environment fields.',
    icon: '📦',
    logIndex: '.ds-filebeat-*',
    serviceField: 'app_name',
    envField: 'environment',
    errorKeywords: 'ERROR,Exception,CRITICAL,FATAL',
    metricIndex: '.ds-metricbeat-*',
    cpuField: 'host.cpu.usage',
    memField: 'system.memory.actual.used.pct',
  },
  {
    id: 'k8s-logstash',
    label: 'K8s / Logstash',
    desc: 'Kubernetes logs via Fluent Bit or Logstash. Groups by kubernetes.labels.app.',
    icon: '☸️',
    logIndex: 'k8s-logs-*',
    serviceField: 'kubernetes.labels.app.keyword',
    envField: 'kubernetes.namespace',
    errorKeywords: 'ERROR,Exception,CRITICAL,FATAL',
    metricIndex: '.ds-metricbeat-*',
    cpuField: 'host.cpu.usage',
    memField: 'system.memory.actual.used.pct',
  },
  {
    id: 'ecs',
    label: 'ECS Standard',
    desc: 'Elastic Common Schema logs. Uses service.name and service.environment.',
    icon: '🔷',
    logIndex: 'logs-*',
    serviceField: 'service.name',
    envField: 'service.environment',
    errorKeywords: 'ERROR,Exception,CRITICAL,FATAL',
    metricIndex: 'metrics-*',
    cpuField: 'system.cpu.total.norm.pct',
    memField: 'system.memory.actual.used.pct',
  },
  {
    id: 'custom',
    label: 'Custom',
    desc: 'Manually configure all field names for your own log format.',
    icon: '⚙️',
    logIndex: '',
    serviceField: '',
    envField: '',
    errorKeywords: 'ERROR,Exception,CRITICAL,FATAL',
    metricIndex: '',
    cpuField: 'host.cpu.usage',
    memField: 'system.memory.actual.used.pct',
  },
]

// ── Default settings shape ────────────────────────────────────────────────────
export interface ServiceHealthSettings {
  presetId: string
  logIndex: string
  serviceField: string
  envField: string
  errorKeywords: string
  metricIndex: string
  cpuField: string
  memField: string
  alertIndex: string
  alertRuleField: string
  alertHostField: string
  serviceLimit: number
}

export const SERVICE_HEALTH_DEFAULTS: ServiceHealthSettings = {
  presetId: '',
  logIndex: '',
  serviceField: 'app_name',
  envField: 'environment',
  errorKeywords: 'ERROR,Exception,CRITICAL,FATAL',
  metricIndex: '',
  cpuField: 'host.cpu.usage',
  memField: 'system.memory.actual.used.pct',
  alertIndex: '',
  alertRuleField: 'rule_name',
  alertHostField: 'match_body.host.name',
  serviceLimit: 20,
}

export interface UptimeSettings {
  heartbeatIndex: string
}

export const UPTIME_DEFAULTS: UptimeSettings = {
  heartbeatIndex: '',
}

// ── Composable ────────────────────────────────────────────────────────────────
const STORAGE_PREFIX = 'anveesa:obs:'

export function useObsSettings<T extends object>(key: string, defaults: T) {
  const stored = localStorage.getItem(STORAGE_PREFIX + key)
  const parsed = stored ? (() => { try { return JSON.parse(stored) } catch { return null } })() : null
  const settings = ref<T>(parsed ? { ...defaults, ...parsed } : { ...defaults })

  watch(settings, val => {
    localStorage.setItem(STORAGE_PREFIX + key, JSON.stringify(val))
  }, { deep: true })

  function reset() {
    settings.value = { ...defaults } as T
    localStorage.removeItem(STORAGE_PREFIX + key)
  }

  return { settings, reset }
}

// ── Index browser API ─────────────────────────────────────────────────────────
export interface IndexEntry {
  index: string
  'docs.count': string
  'store.size': string
  health: string
  status: string
}

export async function fetchIndices(connId: number, pattern = '*'): Promise<IndexEntry[]> {
  const { data } = await axios.get(`/api/connections/${connId}/search/list-indices`, {
    params: { pattern },
  })
  return Array.isArray(data) ? data : []
}

// ── Field auto-detect from mapping ────────────────────────────────────────────
const SERVICE_FIELD_CANDIDATES = [
  'app_name', 'service.name', 'kubernetes.labels.app.keyword',
  'application', 'app', 'service', 'container.name',
]
const ENV_FIELD_CANDIDATES = [
  'environment', 'env', 'kubernetes.namespace',
  'service.environment', 'deployment.environment',
]

export function suggestFields(flatFields: string[]): { serviceField: string; envField: string } {
  const fieldSet = new Set(flatFields)
  const serviceField = SERVICE_FIELD_CANDIDATES.find(f => fieldSet.has(f)) ?? flatFields[0] ?? ''
  const envField     = ENV_FIELD_CANDIDATES.find(f => fieldSet.has(f)) ?? ''
  return { serviceField, envField }
}
