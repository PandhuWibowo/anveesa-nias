<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import axios from 'axios'
import { useConnections } from '@/composables/useConnections'
import { useToast } from '@/composables/useToast'
import { useAuth } from '@/composables/useAuth'

const props = defineProps<{ activeConnId?: number | null }>()
const emit = defineEmits<{ (e: 'set-conn', id: number): void }>()

interface KafkaTopic {
  name: string
  partitions: number
  replication_factor: number
  leader_count: number
  error?: string
}

interface KafkaGroup {
  group_id: string
  coordinator: number
  protocol_type: string
}

interface KafkaMessageHeader {
  key: string
  value: string
}

interface KafkaMessage {
  topic: string
  partition: number
  offset: number
  high_water_mark: number
  key: string
  value: string
  timestamp: string
  headers: KafkaMessageHeader[]
}

interface KafkaConsumeResult {
  group_id: string
  topic: string
  messages: KafkaMessage[]
  count: number
}

interface KafkaGroupDetail {
  group_id: string
  state: string
  total_lag: number
  members: Array<{ member_id: string; client_id: string; client_host: string; assignments: Array<{ topic: string; partitions: number[] }> }>
  offsets: Array<{ topic: string; partition: number; committed_offset: number; latest_offset: number; lag: number; error?: string }>
  error?: string
}

interface KafkaDiagnosticError {
  error: string
  code?: string
  operation?: string
  reason?: string
  suggestions?: string[]
  context?: Record<string, string>
  trace_id?: string
}

interface KafkaActivityItem {
  id: number
  at: string
  operation: string
  status: 'ok' | 'error'
  durationMs: number
  target: string
  reason: string
  traceId?: string
  diagnostic?: KafkaDiagnosticError
}

const { connections, fetchConnections } = useConnections()
const { hasAnyPermission } = useAuth()
const toast = useToast()

const topics = ref<KafkaTopic[]>([])
const groups = ref<KafkaGroup[]>([])
const loadingTopics = ref(false)
const loadingGroups = ref(false)
const topicFilter = ref('')
const showInternalTopics = ref(false)
const activeTab = ref<'topics' | 'messages' | 'produce' | 'groups' | 'health' | 'manage'>('topics')
const selectedTopic = ref<KafkaTopic | null>(null)
const messages = ref<KafkaMessage[]>([])
const loadingMessages = ref(false)
const messagePartition = ref(-1)
const messageLimit = ref(50)
const produceKey = ref('')
const produceValue = ref('')
const produceHeaders = ref('')
const producing = ref(false)
const testConsumerGroupId = ref('nias-dashboard-consumer')
const testConsumerTopic = ref('')
const testConsumerLimit = ref(10)
const consumingTest = ref(false)
const testConsumeResult = ref<KafkaConsumeResult | null>(null)
const selectedGroupId = ref('')
const groupDetail = ref<KafkaGroupDetail | null>(null)
const loadingGroupDetail = ref(false)
const newTopicName = ref('')
const newTopicPartitions = ref(3)
const newTopicReplication = ref(1)
const newTopicConfigs = ref('')
const updatePartitionCount = ref(0)
const managingTopic = ref(false)
const lastKafkaError = ref<KafkaDiagnosticError | null>(null)
const kafkaActivity = ref<KafkaActivityItem[]>([])
const traceQuery = ref('')
const traceDlqTopics = ref('')
const traceSearching = ref(false)
const traceResults = ref<Array<{ topic: string; messages: KafkaMessage[]; kind: 'primary' | 'dlq' }>>([])
const messageDecodeMode = ref<'json' | 'text' | 'base64' | 'raw'>('json')
const schemaRequiredFields = ref('event\nid')
const replayTargetTopic = ref('')
let kafkaActivitySeq = 0

// ── Queue Health ──────────────────────────────────────────────────
interface GroupHealthSummary {
  group_id: string
  state: string
  members: number
  topic_count: number
  part_count: number
  total_lag: number
  health: 'ok' | 'warn' | 'critical'
  health_reason: string
  error?: string
}
const groupsHealth = ref<GroupHealthSummary[]>([])
const loadingHealth = ref(false)
const healthFilter = ref<'all' | 'ok' | 'warn' | 'critical'>('all')
let healthAutoRefreshTimer: ReturnType<typeof setInterval> | null = null

const canProduce = computed(() => hasAnyPermission(['kafka.produce']))
const canManage = computed(() => hasAnyPermission(['kafka.manage']))

const kafkaConnections = computed(() => connections.value.filter(c => c.driver === 'kafka'))
const activeConn = computed(() => {
  const active = props.activeConnId ? connections.value.find(c => c.id === props.activeConnId) : null
  if (active?.driver === 'kafka') return active
  return kafkaConnections.value[0] ?? null
})

const filteredTopics = computed(() => {
  const query = topicFilter.value.trim().toLowerCase()
  const visibleTopics = showInternalTopics.value ? topics.value : topics.value.filter(topic => !isKafkaInternalTopic(topic.name))
  const filtered = query ? visibleTopics.filter(topic => topic.name.toLowerCase().includes(query)) : visibleTopics
  return [...filtered].sort((a, b) => {
    const aInternal = isKafkaInternalTopic(a.name)
    const bInternal = isKafkaInternalTopic(b.name)
    if (aInternal !== bInternal) return aInternal ? 1 : -1
    return a.name.localeCompare(b.name)
  })
})
const selectedTopicIsInternal = computed(() => !!selectedTopic.value && isKafkaInternalTopic(selectedTopic.value.name))
const normalizedTraceQuery = computed(() => traceQuery.value.trim().toLowerCase())
const filteredMessages = computed(() => {
  const query = normalizedTraceQuery.value
  if (!query) return messages.value
  return messages.value.filter(message => messageMatchesTrace(message, query))
})
const filteredKafkaActivity = computed(() => {
  const query = normalizedTraceQuery.value
  if (!query) return kafkaActivity.value
  return kafkaActivity.value.filter(item => activityMatchesTrace(item, query))
})
const traceMatchSummary = computed(() => {
  const query = normalizedTraceQuery.value
  if (!query) return ''
  return `${filteredMessages.value.length} message${filteredMessages.value.length === 1 ? '' : 's'} / ${filteredKafkaActivity.value.length} activity match${filteredKafkaActivity.value.length === 1 ? '' : 'es'}`
})
const allTraceMessages = computed(() => traceResults.value.flatMap(result => result.messages.map(message => ({ ...message, traceKind: result.kind }))))
const traceDlqMessages = computed(() => allTraceMessages.value.filter(message => message.traceKind === 'dlq'))
const tracePrimaryMessages = computed(() => allTraceMessages.value.filter(message => message.traceKind === 'primary'))
const traceTimeline = computed(() => {
  const query = traceQuery.value.trim()
  const storedMessage = tracePrimaryMessages.value[0] || filteredMessages.value.find(message => messageMatchesTrace(message, normalizedTraceQuery.value))
  const dlqMessage = traceDlqMessages.value[0]
  const produceActivity = kafkaActivity.value.find(item => item.operation === 'produce_message' && (!query || activityMatchesTrace(item, normalizedTraceQuery.value)))
  const consumeActivity = kafkaActivity.value.find(item => item.operation === 'consume_test' && (!query || activityMatchesTrace(item, normalizedTraceQuery.value)))
  const health = groupHealth.value
  return [
    {
      label: 'Produced',
      status: produceActivity ? produceActivity.status : (storedMessage ? 'ok' : 'pending'),
      text: produceActivity?.reason || (storedMessage ? 'A matching record exists in Kafka.' : 'No matching produce activity found in this browser session.'),
    },
    {
      label: 'Stored',
      status: storedMessage ? 'ok' : 'pending',
      text: storedMessage ? `${storedMessage.topic} / partition ${storedMessage.partition} / offset ${storedMessage.offset}` : 'Load messages or run trace search to find the stored record.',
    },
    {
      label: 'Consumed',
      status: consumeActivity ? consumeActivity.status : (groupConsumptionSummary.value?.tracked ? 'ok' : 'pending'),
      text: consumeActivity?.reason || groupConsumptionSummary.value?.explanation || 'Select or run a consumer group to verify commits.',
    },
    {
      label: 'Lag Status',
      status: health.level === 'ok' ? 'ok' : (health.level === 'warn' ? 'error' : 'pending'),
      text: `${health.label}: ${health.reason}`,
    },
    {
      label: 'DLQ / Failure',
      status: dlqMessage ? 'error' : 'ok',
      text: dlqMessage ? `Found in ${dlqMessage.topic} partition ${dlqMessage.partition} offset ${dlqMessage.offset}` : 'No matching DLQ/failure record found in configured topics.',
    },
  ]
})
const groupHealth = computed(() => classifyConsumerGroupHealth(groupDetail.value))
const schemaFields = computed(() => schemaRequiredFields.value.split(/[\n,]/).map(field => field.trim()).filter(Boolean))

const totalPartitions = computed(() => topics.value.reduce((sum, topic) => sum + topic.partitions, 0))
const averageReplication = computed(() => {
  if (!topics.value.length) return '0'
  const total = topics.value.reduce((sum, topic) => sum + topic.replication_factor, 0)
  return (total / topics.value.length).toFixed(1)
})
const underReplicatedTopics = computed(() => topics.value.filter(topic => topic.leader_count < topic.partitions).length)
const healthyTopics = computed(() => Math.max(topics.value.length - underReplicatedTopics.value, 0))
const selectedTopicLag = computed(() => {
  if (!selectedTopic.value || !groupDetail.value?.offsets?.length) return 0
  return groupDetail.value.offsets
    .filter(offset => offset.topic === selectedTopic.value?.name)
    .reduce((sum, offset) => sum + offset.lag, 0)
})
const selectedTopicConsumerGroups = computed(() => {
  if (!selectedTopic.value) return []
  const topicName = selectedTopic.value.name
  const rows: Array<{ groupId: string; state: string; members: number; partitions: number; lag: number; status: string }> = []

  if (groupDetail.value?.offsets?.some(offset => offset.topic === topicName)) {
    const offsets = groupDetail.value.offsets.filter(offset => offset.topic === topicName)
    const lag = offsets.reduce((sum, offset) => sum + Math.max(offset.lag, 0), 0)
    rows.push({
      groupId: groupDetail.value.group_id,
      state: groupDetail.value.state || 'unknown',
      members: groupDetail.value.members?.length || 0,
      partitions: offsets.length,
      lag,
      status: groupDetail.value.state === 'Stable' || groupDetail.value.state === 'STABLE'
        ? (lag > 0 ? 'Lagging' : 'Caught up')
        : (groupDetail.value.state || 'Unknown'),
    })
  }

  return rows
})
const groupConsumptionSummary = computed(() => {
  if (!groupDetail.value) return null
  const tracked = groupDetail.value.offsets?.length || 0
  const lag = groupDetail.value.total_lag || 0
  const activeMembers = groupDetail.value.members?.length || 0
  const topicsTracked = new Set((groupDetail.value.offsets || []).map(offset => offset.topic)).size
  let status = 'No committed offsets'
  let explanation = 'This group exists, but no committed offsets were returned for the selected cluster.'

  if (tracked > 0 && lag === 0) {
    status = 'Caught up'
    explanation = activeMembers > 0
      ? 'This group has active members and has committed through the latest known offsets.'
      : 'This group has committed through the latest known offsets. State Empty means no consumer is currently connected.'
  } else if (tracked > 0 && lag > 0) {
    status = 'Lagging'
    explanation = `This group is ${lag} message${lag === 1 ? '' : 's'} behind the latest known offsets.`
  }

  return {
    groupId: groupDetail.value.group_id,
    state: groupDetail.value.state || 'unknown',
    status,
    explanation,
    tracked,
    lag,
    activeMembers,
    topicsTracked,
  }
})

async function loadKafka() {
  if (!activeConn.value) return
  await Promise.all([loadTopics(), loadGroups()])
}

async function loadTopics() {
  if (!activeConn.value) return
  const started = performance.now()
  loadingTopics.value = true
  try {
    const { data } = await axios.get<KafkaTopic[]>(`/api/connections/${activeConn.value.id}/kafka/topics`)
    topics.value = data || []
    const currentTopicName = selectedTopic.value?.name || testConsumerTopic.value
    selectedTopic.value = topics.value.find(topic => topic.name === currentTopicName && (showInternalTopics.value || !isKafkaInternalTopic(topic.name)))
      || topics.value.find(topic => !isKafkaInternalTopic(topic.name))
      || topics.value[0]
      || null
    if (selectedTopic.value && !testConsumerTopic.value) {
      testConsumerTopic.value = selectedTopic.value.name
    }
    clearKafkaError('list_topics')
    recordKafkaActivity('list_topics', 'ok', started, `cluster ${activeConn.value.name}`, `Loaded ${topics.value.length} topics`)
  } catch (error: any) {
    topics.value = []
    selectedTopic.value = null
    captureKafkaError(error, 'list_topics', 'Failed to load Kafka topics', started, `cluster ${activeConn.value.name}`)
  } finally {
    loadingTopics.value = false
  }
}

async function loadGroups() {
  if (!activeConn.value) return
  const started = performance.now()
  loadingGroups.value = true
  try {
    const { data } = await axios.get<KafkaGroup[]>(`/api/connections/${activeConn.value.id}/kafka/groups`)
    groups.value = data || []
    clearKafkaError('list_groups')
    recordKafkaActivity('list_groups', 'ok', started, `cluster ${activeConn.value.name}`, `Loaded ${groups.value.length} consumer groups`)
  } catch (error: any) {
    groups.value = []
    captureKafkaError(error, 'list_groups', 'Failed to load Kafka consumer groups', started, `cluster ${activeConn.value.name}`)
  } finally {
    loadingGroups.value = false
  }
}

async function loadGroupsHealth() {
  if (!activeConn.value) return
  loadingHealth.value = true
  try {
    const { data } = await axios.get<GroupHealthSummary[]>(`/api/connections/${activeConn.value.id}/kafka/groups-health`)
    groupsHealth.value = data || []
  } catch {
    groupsHealth.value = []
  } finally {
    loadingHealth.value = false
  }
}

async function loadMessages() {
  if (!activeConn.value || !selectedTopic.value) return
  if (isKafkaInternalTopic(selectedTopic.value.name)) {
    messages.value = []
    return   // silently clear — the template shows an inline notice for internal topics
  }
  const started = performance.now()
  loadingMessages.value = true
  try {
    const { data } = await axios.get<KafkaMessage[]>(`/api/connections/${activeConn.value.id}/kafka/messages`, {
      params: { topic: selectedTopic.value.name, partition: messagePartition.value, limit: messageLimit.value },
    })
    messages.value = data || []
    clearKafkaError('read_messages')
    recordKafkaActivity('read_messages', 'ok', started, kafkaTarget({ topic: selectedTopic.value.name, partition: messagePartition.value }), `Loaded ${messages.value.length} messages`)
  } catch (error: any) {
    messages.value = []
    captureKafkaError(error, 'read_messages', 'Failed to load Kafka messages', started, kafkaTarget({ topic: selectedTopic.value.name, partition: messagePartition.value }))
  } finally {
    loadingMessages.value = false
  }
}

async function produceMessage() {
  if (!activeConn.value || !selectedTopic.value) return
  const started = performance.now()
  producing.value = true
  try {
    await axios.post(`/api/connections/${activeConn.value.id}/kafka/produce`, {
      topic: selectedTopic.value.name,
      key: produceKey.value,
      value: produceValue.value,
      headers: parseHeaders(produceHeaders.value),
    })
    toast.success('Kafka message produced')
    clearKafkaError('produce_message')
    recordKafkaActivity('produce_message', 'ok', started, kafkaTarget({ topic: selectedTopic.value.name }), 'Message produced successfully')
    produceValue.value = ''
    await loadMessages()
  } catch (error: any) {
    captureKafkaError(error, 'produce_message', 'Failed to produce Kafka message', started, kafkaTarget({ topic: selectedTopic.value.name }))
  } finally {
    producing.value = false
  }
}

async function replayMessage(message: KafkaMessage) {
  if (!activeConn.value) return
  const topic = replayTargetTopic.value || selectedTopic.value?.name || message.topic
  const started = performance.now()
  try {
    await axios.post(`/api/connections/${activeConn.value.id}/kafka/produce`, {
      topic,
      key: message.key,
      value: message.value,
      headers: [...(message.headers || []), { key: 'replayed_from', value: `${message.topic}:${message.partition}:${message.offset}` }],
    })
    toast.success(`Replayed message to ${topic}`)
    recordKafkaActivity('replay_message', 'ok', started, kafkaTarget({ topic }), `Replayed ${message.topic}:${message.partition}:${message.offset}`)
  } catch (error: any) {
    captureKafkaError(error, 'replay_message', 'Failed to replay Kafka message', started, kafkaTarget({ topic }))
  }
}

async function runTraceSearch() {
  if (!activeConn.value || !traceQuery.value.trim()) return
  const started = performance.now()
  traceSearching.value = true
  try {
    const primaryTopic = selectedTopic.value && !isKafkaInternalTopic(selectedTopic.value.name) ? selectedTopic.value.name : ''
    const dlqTopics = parseTopicLines(traceDlqTopics.value)
    const targets = Array.from(new Set([primaryTopic, ...dlqTopics].filter(Boolean)))
    const results = await Promise.all(targets.map(async topic => {
      const { data } = await axios.get<KafkaMessage[]>(`/api/connections/${activeConn.value!.id}/kafka/messages`, {
        params: { topic, partition: -1, limit: Math.max(messageLimit.value, 50) },
      })
      return {
        topic,
        kind: dlqTopics.includes(topic) ? 'dlq' as const : 'primary' as const,
        messages: (data || []).filter(message => messageMatchesTrace(message, normalizedTraceQuery.value)),
      }
    }))
    traceResults.value = results
    recordKafkaActivity('trace_search', 'ok', started, targets.join(', '), `Found ${results.reduce((sum, item) => sum + item.messages.length, 0)} trace matches`)
  } catch (error: any) {
    captureKafkaError(error, 'trace_search', 'Failed to search trace across Kafka topics', started, traceQuery.value.trim())
  } finally {
    traceSearching.value = false
  }
}

async function runTestConsumer() {
  const topic = testConsumerTopic.value || selectedTopic.value?.name || ''
  if (!activeConn.value || !topic || !testConsumerGroupId.value.trim()) return
  const groupId = testConsumerGroupId.value.trim()
  const started = performance.now()
  consumingTest.value = true
  try {
    const { data } = await axios.post<KafkaConsumeResult>(`/api/connections/${activeConn.value.id}/kafka/consume-test`, {
      topic,
      group_id: groupId,
      limit: testConsumerLimit.value,
    })
    testConsumeResult.value = data
    selectedGroupId.value = groupId
    selectedTopic.value = topics.value.find(item => item.name === topic) || selectedTopic.value
    toast.success(`Consumed ${data.count} Kafka message${data.count === 1 ? '' : 's'}`)
    clearKafkaError('consume_test')
    recordKafkaActivity('consume_test', 'ok', started, kafkaTarget({ topic, group: groupId }), `Consumed and committed ${data.count} messages`)
    await Promise.all([loadGroups(), loadGroupDetail(groupId)])
  } catch (error: any) {
    captureKafkaError(error, 'consume_test', 'Failed to run dashboard consumer', started, kafkaTarget({ topic, group: groupId }))
  } finally {
    consumingTest.value = false
  }
}

async function loadGroupDetail(groupId = selectedGroupId.value) {
  if (!activeConn.value || !groupId) return
  const started = performance.now()
  selectedGroupId.value = groupId
  loadingGroupDetail.value = true
  try {
    const { data } = await axios.get<KafkaGroupDetail>(`/api/connections/${activeConn.value.id}/kafka/groups-detail`, {
      params: { group_id: groupId },
    })
    groupDetail.value = data
    clearKafkaError('group_detail')
    recordKafkaActivity('group_detail', 'ok', started, kafkaTarget({ group: groupId }), `Loaded group detail with ${groupDetail.value.offsets?.length || 0} offsets`)
  } catch (error: any) {
    groupDetail.value = null
    captureKafkaError(error, 'group_detail', 'Failed to load Kafka group detail', started, kafkaTarget({ group: groupId }))
  } finally {
    loadingGroupDetail.value = false
  }
}

async function createTopic() {
  if (!activeConn.value || !newTopicName.value.trim()) return
  const started = performance.now()
  const topic = newTopicName.value.trim()
  managingTopic.value = true
  try {
    await axios.post(`/api/connections/${activeConn.value.id}/kafka/topics`, {
      topic: newTopicName.value.trim(),
      partitions: newTopicPartitions.value,
      replication_factor: newTopicReplication.value,
      configs: parseConfigLines(newTopicConfigs.value),
    })
    toast.success('Kafka topic created')
    clearKafkaError('create_topic')
    recordKafkaActivity('create_topic', 'ok', started, kafkaTarget({ topic }), `Created topic with ${newTopicPartitions.value} partitions`)
    newTopicName.value = ''
    newTopicConfigs.value = ''
    await loadTopics()
  } catch (error: any) {
    captureKafkaError(error, 'create_topic', 'Failed to create Kafka topic', started, kafkaTarget({ topic }))
  } finally {
    managingTopic.value = false
  }
}

async function updatePartitions() {
  if (!activeConn.value || !selectedTopic.value || updatePartitionCount.value <= selectedTopic.value.partitions) return
  const started = performance.now()
  const topic = selectedTopic.value.name
  managingTopic.value = true
  try {
    await axios.put(`/api/connections/${activeConn.value.id}/kafka/topics/partitions`, {
      topic: selectedTopic.value.name,
      partitions: updatePartitionCount.value,
    })
    toast.success('Kafka partitions updated')
    clearKafkaError('update_partitions')
    recordKafkaActivity('update_partitions', 'ok', started, kafkaTarget({ topic }), `Updated partitions to ${updatePartitionCount.value}`)
    await loadTopics()
  } catch (error: any) {
    captureKafkaError(error, 'update_partitions', 'Failed to update Kafka partitions', started, kafkaTarget({ topic }))
  } finally {
    managingTopic.value = false
  }
}

async function deleteTopic() {
  if (!activeConn.value || !selectedTopic.value) return
  const topic = selectedTopic.value.name
  if (!window.confirm(`Delete Kafka topic "${topic}"? This cannot be undone.`)) return
  const started = performance.now()
  managingTopic.value = true
  try {
    await axios.delete(`/api/connections/${activeConn.value.id}/kafka/topics`, { params: { topic } })
    toast.success('Kafka topic deleted')
    clearKafkaError('delete_topic')
    recordKafkaActivity('delete_topic', 'ok', started, kafkaTarget({ topic }), 'Topic deleted')
    await loadTopics()
  } catch (error: any) {
    captureKafkaError(error, 'delete_topic', 'Failed to delete Kafka topic', started, kafkaTarget({ topic }))
  } finally {
    managingTopic.value = false
  }
}

function parseHeaders(raw: string): KafkaMessageHeader[] {
  return raw.split('\n').map(line => {
    const idx = line.indexOf('=')
    if (idx < 0) return null
    const key = line.slice(0, idx).trim()
    if (!key) return null
    return { key, value: line.slice(idx + 1) }
  }).filter(Boolean) as KafkaMessageHeader[]
}

function parseConfigLines(raw: string): Record<string, string> {
  return Object.fromEntries(parseHeaders(raw).map(header => [header.key, header.value]))
}

function parseTopicLines(raw: string): string[] {
  return raw.split(/[\n,]/).map(item => item.trim()).filter(Boolean)
}

function selectTopic(topic: KafkaTopic) {
  selectedTopic.value = topic
  updatePartitionCount.value = topic.partitions
  messages.value = []
}

function selectConnection(id: number) {
  emit('set-conn', id)
}

function captureKafkaError(error: any, operation: string, fallback: string, started = performance.now(), target = 'Kafka cluster') {
  const diagnostic = parseKafkaError(error, operation, fallback)
  lastKafkaError.value = diagnostic
  recordKafkaActivity(operation, 'error', started, target, diagnostic.reason || fallback, diagnostic)
  toast.error(diagnostic.reason || diagnostic.error || fallback)
}

function clearKafkaError(operation?: string) {
  if (!lastKafkaError.value) return
  if (!operation || lastKafkaError.value.operation === operation) {
    lastKafkaError.value = null
  }
}

function parseKafkaError(error: any, operation: string, fallback: string): KafkaDiagnosticError {
  const data = error?.response?.data
  if (data && typeof data === 'object') {
    return {
      error: String(data.error || fallback),
      code: data.code,
      operation: data.operation || operation,
      reason: data.reason,
      suggestions: Array.isArray(data.suggestions) ? data.suggestions : [],
      context: data.context || {},
      trace_id: data.trace_id,
    }
  }
  if (typeof data === 'string' && data.trim()) {
    try {
      const parsed = JSON.parse(data)
      if (parsed && typeof parsed === 'object') {
        return {
          error: String(parsed.error || fallback),
          code: parsed.code,
          operation: parsed.operation || operation,
          reason: parsed.reason,
          suggestions: Array.isArray(parsed.suggestions) ? parsed.suggestions : [],
          context: parsed.context || {},
          trace_id: parsed.trace_id,
        }
      }
    } catch {
      return { error: data.trim(), operation, reason: fallback, suggestions: defaultKafkaSuggestions(data) }
    }
  }
  const message = error?.message || fallback
  return { error: message, operation, reason: fallback, suggestions: defaultKafkaSuggestions(message) }
}

function defaultKafkaSuggestions(raw: string): string[] {
  const lower = raw.toLowerCase()
  if (lower.includes('network') || lower.includes('timeout') || lower.includes('refused')) {
    return ['Check broker host, port, firewall, Docker networking, and Kafka advertised.listeners.', 'Retry after confirming the backend can reach the broker.']
  }
  return ['Check the selected Kafka connection and retry.', 'Use the raw error and context to narrow the failing broker, topic, partition, or group.']
}

// ── Message detail panel ─────────────────────────────────────────
const selectedMessage    = ref<KafkaMessage | null>(null)
const msgDetailTab       = ref<'parsed' | 'raw' | 'headers'>('parsed')
const msgDetailFullscreen = ref(false)

interface MessageSignals {
  level: 'error' | 'warn' | 'ok' | 'unknown'
  errorText: string
  status: string
  eventType: string
  parsedOk: boolean
  parsed: Record<string, unknown> | null
}

function extractMessageSignals(message: KafkaMessage): MessageSignals {
  let parsed: Record<string, unknown> | null = null
  let parsedOk = false
  try {
    parsed = JSON.parse(message.value)
    parsedOk = true
  } catch { /* not JSON */ }

  const p = parsed ?? {}
  const errorText = String(
    p.error ?? p.error_message ?? p.errorMessage ?? p.exception ??
    p.err ?? p.Error ?? p.message_error ?? ''
  ).trim()
  const statusRaw = String(p.status ?? p.Status ?? p.level ?? p.severity ?? p.log_level ?? '').trim()
  const eventType = String(p.event ?? p.event_type ?? p.type ?? p.action ?? '').trim()

  const isError = !!(
    errorText ||
    ['error', 'err', 'fatal', 'critical', 'failure', 'failed'].some(k =>
      statusRaw.toLowerCase().includes(k) ||
      (message.key || '').toLowerCase().includes(k)
    )
  )
  const isWarn = !isError && ['warn', 'warning', 'retry', 'timeout'].some(k =>
    statusRaw.toLowerCase().includes(k)
  )

  return {
    level: isError ? 'error' : isWarn ? 'warn' : parsedOk ? 'ok' : 'unknown',
    errorText,
    status: statusRaw,
    eventType,
    parsedOk,
    parsed,
  }
}

function msgSignalClass(message: KafkaMessage) {
  const s = extractMessageSignals(message)
  return { 'msg-row--error': s.level === 'error', 'msg-row--warn': s.level === 'warn', 'msg-row--ok': s.level === 'ok' }
}

function openMsgDetail(message: KafkaMessage) {
  selectedMessage.value = message
  msgDetailTab.value = 'parsed'
  msgDetailFullscreen.value = false
}

function parsedEntries(message: KafkaMessage): Array<{ key: string; value: unknown; isError: boolean }> {
  try {
    const p = JSON.parse(message.value) as Record<string, unknown>
    const errorKeys = new Set(['error', 'error_message', 'errorMessage', 'exception', 'err', 'Error', 'message_error', 'stack', 'stacktrace', 'stack_trace'])
    return Object.entries(p).map(([key, value]) => ({
      key, value, isError: errorKeys.has(key),
    }))
  } catch {
    return []
  }
}

function formatDetailValue(val: unknown): string {
  if (val === null || val === undefined) return 'null'
  if (typeof val === 'object') return JSON.stringify(val, null, 2)
  return String(val)
}

function messageMatchesTrace(message: KafkaMessage, query: string) {
  const headerText = (message.headers || []).map(header => `${header.key}=${header.value}`).join('\n')
  return [
    message.topic,
    String(message.partition),
    String(message.offset),
    message.key,
    message.value,
    headerText,
  ].some(value => String(value || '').toLowerCase().includes(query))
}

function activityMatchesTrace(item: KafkaActivityItem, query: string) {
  return [
    item.operation,
    item.target,
    item.reason,
    item.traceId,
    item.diagnostic?.error,
    item.diagnostic?.reason,
    item.diagnostic?.trace_id,
    item.diagnostic?.code,
    JSON.stringify(item.diagnostic?.context || {}),
  ].some(value => String(value || '').toLowerCase().includes(query))
}

function decodedMessageValue(message: KafkaMessage) {
  switch (messageDecodeMode.value) {
    case 'json':
      try {
        return JSON.stringify(JSON.parse(message.value), null, 2)
      } catch {
        return message.value
      }
    case 'base64':
      return btoa(unescape(encodeURIComponent(message.value)))
    case 'raw':
      return Array.from(message.value).map(char => char.charCodeAt(0).toString(16).padStart(2, '0')).join(' ')
    default:
      return message.value
  }
}

function messageValidation(message: KafkaMessage) {
  if (!schemaFields.value.length) return { ok: true, missing: [] as string[], reason: 'No required fields configured' }
  try {
    const parsed = JSON.parse(message.value)
    const missing = schemaFields.value.filter(field => parsed?.[field] === undefined || parsed?.[field] === null || parsed?.[field] === '')
    return { ok: missing.length === 0, missing, reason: missing.length ? `Missing ${missing.join(', ')}` : 'Payload matches required fields' }
  } catch {
    return { ok: false, missing: schemaFields.value, reason: 'Payload is not valid JSON' }
  }
}

function classifyConsumerGroupHealth(detail: KafkaGroupDetail | null) {
  if (!detail) return { label: 'No group selected', level: 'neutral' as const, reason: 'Select a consumer group or run the dashboard test consumer.' }
  const state = (detail.state || '').toLowerCase()
  const tracked = detail.offsets?.length || 0
  const lag = detail.total_lag || 0
  const members = detail.members?.length || 0
  if (state.includes('rebalanc')) return { label: 'Rebalancing', level: 'warn' as const, reason: 'The group is currently reassigning partitions.' }
  if (!tracked) return { label: 'No commits', level: 'warn' as const, reason: 'Kafka returned no committed offsets for this group.' }
  if (lag > 0) return { label: 'Lagging', level: 'warn' as const, reason: `${lag} messages behind latest offsets.` }
  if (members === 0 && tracked > 0) return { label: 'Empty but caught up', level: 'ok' as const, reason: 'No active consumer is connected, but committed offsets are caught up.' }
  if (members > 0 && lag === 0) return { label: 'Healthy', level: 'ok' as const, reason: 'Active members are caught up.' }
  return { label: detail.state || 'Unknown', level: 'neutral' as const, reason: 'No explicit health rule matched.' }
}

function recordKafkaActivity(operation: string, status: KafkaActivityItem['status'], started: number, target: string, reason: string, diagnostic?: KafkaDiagnosticError) {
  kafkaActivity.value.unshift({
    id: ++kafkaActivitySeq,
    at: new Date().toLocaleTimeString(),
    operation,
    status,
    durationMs: Math.max(0, Math.round(performance.now() - started)),
    target,
    reason,
    traceId: diagnostic?.trace_id,
    diagnostic,
  })
  kafkaActivity.value = kafkaActivity.value.slice(0, 8)
}

function kafkaTarget(parts: { topic?: string; group?: string; partition?: number }) {
  const bits: string[] = []
  if (parts.topic) bits.push(`topic ${parts.topic}`)
  if (parts.group) bits.push(`group ${parts.group}`)
  if (parts.partition !== undefined && parts.partition >= 0) bits.push(`partition ${parts.partition}`)
  return bits.join(' / ') || 'Kafka cluster'
}

function isKafkaInternalTopic(name: string) {
  return name.startsWith('__')
}

onMounted(async () => {
  if (!connections.value.length) await fetchConnections()
  if (!props.activeConnId && kafkaConnections.value.length) {
    emit('set-conn', kafkaConnections.value[0].id)
  }
  await loadKafka()
})

watch(() => props.activeConnId, () => {
  if (activeConn.value) void loadKafka()
})

watch(activeTab, (tab) => {
  if (tab === 'health') {
    void loadGroupsHealth()
    if (!healthAutoRefreshTimer) {
      healthAutoRefreshTimer = setInterval(() => {
        if (activeTab.value === 'health') void loadGroupsHealth()
      }, 30_000)
    }
  } else {
    if (healthAutoRefreshTimer) {
      clearInterval(healthAutoRefreshTimer)
      healthAutoRefreshTimer = null
    }
  }
})

onBeforeUnmount(() => {
  if (healthAutoRefreshTimer) clearInterval(healthAutoRefreshTimer)
})

watch(selectedTopic, (topic) => {
  updatePartitionCount.value = topic?.partitions ?? 0
  messages.value = []
  messagePartition.value = -1
  if (topic && !testConsumerTopic.value) {
    testConsumerTopic.value = topic.name
  }
  if (topic && !traceDlqTopics.value && !isKafkaInternalTopic(topic.name)) {
    traceDlqTopics.value = `${topic.name}.dlq\n${topic.name}.failed`
  }
})
</script>

<template>
  <div class="kafka-console">
    <main class="kafka-main">
      <section v-if="!kafkaConnections.length" class="kafka-empty">
        <div class="kafka-empty__title">No Kafka connection available</div>
        <div class="kafka-empty__sub">Create a Kafka connection in Admin / Connections first.</div>
      </section>

      <template v-else>
        <header class="kafka-topbar">
          <div class="kafka-topbar__primary">
            <div class="kafka-cluster">
              <span class="kafka-logo">KF</span>
              <span class="status-dot" :class="{ warn: underReplicatedTopics > 0 }"></span>
              <div>
                <div class="kafka-cluster__name">{{ activeConn?.name || 'Kafka Browser' }}</div>
                <div class="kafka-cluster__meta">Kafka Browser · {{ activeConn?.host }}{{ activeConn?.port ? `:${activeConn.port}` : '' }}</div>
              </div>
            </div>

            <nav class="kafka-tabs" aria-label="Kafka sections">
              <button class="kafka-tab" :class="{ active: activeTab === 'topics' }" @click="activeTab = 'topics'">
                <span>Topics</span>
                <b>{{ topics.length }}</b>
              </button>
              <button class="kafka-tab" :class="{ active: activeTab === 'messages' }" @click="activeTab = 'messages'; loadMessages()">
                <span>Messages</span>
              </button>
              <button v-if="canProduce" class="kafka-tab" :class="{ active: activeTab === 'produce' }" @click="activeTab = 'produce'">
                <span>Produce</span>
              </button>
              <button class="kafka-tab" :class="{ active: activeTab === 'groups' }" @click="activeTab = 'groups'">
                <span>Consumer Groups</span>
                <b>{{ groups.length }}</b>
              </button>
              <button class="kafka-tab" :class="{ active: activeTab === 'health' }" @click="activeTab = 'health'">
                <span>Queue Health</span>
                <b
                  v-if="groupsHealth.some(g => g.health === 'critical')"
                  style="background:var(--danger,#ef4444);color:#fff"
                >{{ groupsHealth.filter(g => g.health === 'critical').length }}</b>
                <b
                  v-else-if="groupsHealth.some(g => g.health === 'warn')"
                  style="background:#f59e0b;color:#fff"
                >{{ groupsHealth.filter(g => g.health === 'warn').length }}</b>
              </button>
              <button v-if="canManage" class="kafka-tab" :class="{ active: activeTab === 'manage' }" @click="activeTab = 'manage'">
                <span>Topic Tools</span>
              </button>
            </nav>
          </div>

          <div class="kafka-topbar__actions">
            <select class="base-input kafka-select" :value="activeConn?.id" @change="selectConnection(Number(($event.target as HTMLSelectElement).value))">
              <option v-for="conn in kafkaConnections" :key="conn.id" :value="conn.id">{{ conn.name }}</option>
            </select>
            <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!activeConn || loadingTopics || loadingGroups" @click="loadKafka">
              <svg v-if="loadingTopics || loadingGroups" class="spin" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
              Refresh
            </button>
          </div>
        </header>

        <section class="kafka-metrics">
          <div class="kafka-metric">
            <span>Topics</span>
            <strong>{{ topics.length }}</strong>
            <small>{{ healthyTopics }} healthy</small>
          </div>
          <div class="kafka-metric">
            <span>Partitions</span>
            <strong>{{ totalPartitions }}</strong>
            <small>{{ averageReplication }} avg replication</small>
          </div>
          <div class="kafka-metric">
            <span>Consumer Groups</span>
            <strong>{{ groups.length }}</strong>
            <small>{{ selectedGroupId || 'No group selected' }}</small>
          </div>
          <div class="kafka-metric" :class="{ danger: underReplicatedTopics > 0 }">
            <span>Leader Coverage</span>
            <strong>{{ underReplicatedTopics }}</strong>
            <small>topics need attention</small>
          </div>
        </section>

        <section class="kafka-trace-search">
          <div>
            <div class="kafka-kicker">Trace lookup</div>
            <input v-model="traceQuery" class="base-input" placeholder="Search trace_id, message key/value, headers, activity, or raw errors" />
          </div>
          <div class="kafka-trace-search__meta">
            <span>{{ traceMatchSummary || 'No trace filter applied' }}</span>
            <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!traceQuery.trim() || traceSearching" @click="runTraceSearch">{{ traceSearching ? 'Searching...' : 'Search DLQ' }}</button>
            <button v-if="traceQuery" class="base-btn base-btn--ghost base-btn--sm" @click="traceQuery = ''">Clear</button>
          </div>
        </section>

        <details class="kafka-trace-view kafka-collapsible" open>
          <summary>
            <span>
              <span class="kafka-kicker">End-to-end trace</span>
              <strong>{{ traceQuery || 'Enter trace_id to build a timeline' }}</strong>
            </span>
          </summary>
          <div class="kafka-trace-grid">
            <div class="kafka-timeline">
              <div v-for="step in traceTimeline" :key="step.label" class="kafka-timeline__item" :class="step.status">
                <span></span>
                <div>
                  <strong>{{ step.label }}</strong>
                  <p>{{ step.text }}</p>
                </div>
              </div>
            </div>
            <div class="kafka-trace-config">
              <label class="form-label">DLQ / Failure Topics</label>
              <textarea v-model="traceDlqTopics" class="base-input kafka-textarea" placeholder="nias-demo.dlq&#10;nias-demo.failed"></textarea>
              <div class="kafka-helper">Trace search checks the selected topic and these failure topics for the same trace id.</div>
            </div>
          </div>
        </details>

        <section v-if="lastKafkaError" class="kafka-diagnostic">
          <div class="kafka-diagnostic__head">
            <div>
              <div class="kafka-kicker">Kafka troubleshooting</div>
              <h2>{{ lastKafkaError.reason || lastKafkaError.error }}</h2>
            </div>
            <div class="kafka-inline-controls">
              <span v-if="lastKafkaError.code" class="kafka-pill warn">{{ lastKafkaError.code }}</span>
              <button class="base-btn base-btn--ghost base-btn--sm" @click="lastKafkaError = null">Dismiss</button>
            </div>
          </div>
          <div class="kafka-diagnostic__grid">
            <div>
              <span>Operation</span>
              <strong>{{ lastKafkaError.operation || 'kafka_operation' }}</strong>
            </div>
            <div>
              <span>Trace ID</span>
              <strong>{{ lastKafkaError.trace_id || 'not provided' }}</strong>
            </div>
          </div>
          <div class="kafka-diagnostic__body">
            <div>
              <div class="kafka-card-title">What to check next</div>
              <ul>
                <li v-for="suggestion in lastKafkaError.suggestions || []" :key="suggestion">{{ suggestion }}</li>
              </ul>
            </div>
            <div>
              <div class="kafka-card-title">Context</div>
              <dl v-if="lastKafkaError.context && Object.keys(lastKafkaError.context).length">
                <template v-for="[key, value] in Object.entries(lastKafkaError.context)" :key="key">
                  <dt>{{ key }}</dt>
                  <dd>{{ value }}</dd>
                </template>
              </dl>
              <div v-else class="kafka-muted">No context returned.</div>
            </div>
          </div>
          <pre>{{ lastKafkaError.error }}</pre>
        </section>

        <details class="kafka-activity kafka-collapsible" open>
          <summary>
            <span>
              <span class="kafka-kicker">Recent Kafka activity</span>
              <strong>Trace operations and failures</strong>
            </span>
            <button v-if="kafkaActivity.length" class="base-btn base-btn--ghost base-btn--sm" @click.stop.prevent="kafkaActivity = []">Clear</button>
          </summary>
          <div v-if="!kafkaActivity.length" class="kafka-muted">Kafka activity will appear here after you refresh, browse, produce, or manage topics.</div>
          <div v-else-if="!filteredKafkaActivity.length" class="kafka-muted">No activity matches the trace filter.</div>
          <div v-else class="kafka-activity__list">
            <button
              v-for="item in filteredKafkaActivity"
              :key="item.id"
              class="kafka-activity__item"
              :class="{ failed: item.status === 'error' }"
              @click="item.diagnostic && (lastKafkaError = item.diagnostic)"
            >
              <span class="kafka-activity__status">{{ item.status === 'ok' ? 'OK' : 'ERR' }}</span>
              <span class="kafka-activity__main">
                <strong>{{ item.operation }}</strong>
                <small>{{ item.target }}</small>
              </span>
              <span class="kafka-activity__reason">{{ item.reason }}</span>
              <span class="kafka-activity__meta">{{ item.durationMs }}ms · {{ item.at }}</span>
            </button>
          </div>
        </details>

        <section class="kafka-workspace">
          <div v-if="activeTab === 'topics'" class="kafka-topic-layout">
            <aside class="kafka-topic-list">
              <div class="kafka-list-head">
                <input v-model="topicFilter" class="base-input kafka-search" placeholder="Search topics" />
                <label class="kafka-toggle-row">
                  <input v-model="showInternalTopics" type="checkbox" />
                  <span>Show internal topics</span>
                </label>
              </div>
              <div v-if="loadingTopics" class="kafka-muted">Loading topics...</div>
              <template v-else>
                <button
                  v-for="topic in filteredTopics"
                  :key="topic.name"
                  class="kafka-topic-row"
                  :class="{ active: selectedTopic?.name === topic.name, danger: topic.leader_count < topic.partitions }"
                  @click="selectTopic(topic)"
                >
                  <span class="topic-name">{{ topic.name }}</span>
                  <span class="topic-meta">{{ topic.partitions }} partitions / rf {{ topic.replication_factor }}</span>
                  <span class="topic-leader">{{ topic.leader_count }}/{{ topic.partitions }} leaders</span>
                </button>
              </template>
              <div v-if="!loadingTopics && !filteredTopics.length" class="kafka-muted">No topics found.</div>
            </aside>

            <main class="kafka-detail">
              <template v-if="selectedTopic">
                <div class="kafka-panel-head">
                  <div>
                    <div class="kafka-kicker">Topic overview</div>
                    <h1>{{ selectedTopic.name }}</h1>
                  </div>
                  <div class="kafka-inline-controls">
                    <button class="base-btn base-btn--ghost base-btn--sm" @click="activeTab = 'messages'; loadMessages()">Browse Messages</button>
                    <button v-if="canProduce" class="base-btn base-btn--ghost base-btn--sm" @click="activeTab = 'produce'">Produce</button>
                    <span class="kafka-pill" :class="{ warn: selectedTopic.leader_count < selectedTopic.partitions }">
                      {{ selectedTopic.leader_count < selectedTopic.partitions ? 'Needs attention' : 'Healthy' }}
                    </span>
                  </div>
                </div>

                <div class="kafka-stat-grid">
                  <div>
                    <span>Partitions</span>
                    <strong>{{ selectedTopic.partitions }}</strong>
                  </div>
                  <div>
                    <span>Replication Factor</span>
                    <strong>{{ selectedTopic.replication_factor }}</strong>
                  </div>
                  <div>
                    <span>Leader Partitions</span>
                    <strong>{{ selectedTopic.leader_count }}</strong>
                  </div>
                  <div>
                    <span>Selected Group Lag</span>
                    <strong>{{ selectedTopicLag }}</strong>
                  </div>
                </div>

                <details class="kafka-table-card kafka-collapsible" open>
                  <summary>
                    <span class="kafka-card-title">Partition Summary</span>
                  </summary>
                  <table class="kafka-table">
                    <thead>
                      <tr>
                        <th>Metric</th>
                        <th>Value</th>
                        <th>Status</th>
                      </tr>
                    </thead>
                    <tbody>
                      <tr>
                        <td>Partition count</td>
                        <td>{{ selectedTopic.partitions }}</td>
                        <td><span class="kafka-pill">Configured</span></td>
                      </tr>
                      <tr>
                        <td>Replication factor</td>
                        <td>{{ selectedTopic.replication_factor }}</td>
                        <td><span class="kafka-pill">Broker policy</span></td>
                      </tr>
                      <tr>
                        <td>Leader coverage</td>
                        <td>{{ selectedTopic.leader_count }} / {{ selectedTopic.partitions }}</td>
                        <td><span class="kafka-pill" :class="{ warn: selectedTopic.leader_count < selectedTopic.partitions }">{{ selectedTopic.leader_count < selectedTopic.partitions ? 'Partial' : 'Complete' }}</span></td>
                      </tr>
                    </tbody>
                  </table>
                </details>
                <details class="kafka-table-card kafka-consumers-card kafka-collapsible" open>
                  <summary>
                    <span class="kafka-card-title">Consumers for this topic</span>
                  </summary>
                  <table class="kafka-table">
                    <thead>
                      <tr>
                        <th>Consumer Group</th>
                        <th>State</th>
                        <th>Members</th>
                        <th>Tracked Partitions</th>
                        <th>Lag</th>
                        <th>Status</th>
                      </tr>
                    </thead>
                    <tbody>
                      <tr v-for="consumer in selectedTopicConsumerGroups" :key="consumer.groupId">
                        <td>{{ consumer.groupId }}</td>
                        <td>{{ consumer.state }}</td>
                        <td>{{ consumer.members }}</td>
                        <td>{{ consumer.partitions }}</td>
                        <td>{{ consumer.lag }}</td>
                        <td><span class="kafka-pill" :class="{ warn: consumer.lag > 0 || consumer.status === 'Unknown' }">{{ consumer.status }}</span></td>
                      </tr>
                      <tr v-if="!selectedTopicConsumerGroups.length">
                        <td colspan="6" class="kafka-empty-cell">Select a consumer group in the Consumer Groups tab to inspect who consumes this topic and whether it is caught up.</td>
                      </tr>
                    </tbody>
                  </table>
                </details>
                <div v-if="groupConsumptionSummary" class="kafka-summary-card kafka-consumers-card">
                  <div>
                    <div class="kafka-kicker">Selected group outcome</div>
                    <h2>{{ groupConsumptionSummary.status }}</h2>
                    <p>{{ groupConsumptionSummary.explanation }}</p>
                  </div>
                  <div class="kafka-summary-card__stats">
                    <span>{{ groupConsumptionSummary.groupId }}</span>
                    <strong>{{ groupConsumptionSummary.lag }} lag</strong>
                  </div>
                </div>
                <div v-if="selectedTopic.error" class="notice notice--error">{{ selectedTopic.error }}</div>
              </template>
              <div v-else class="kafka-empty-work">Select a topic to inspect metadata.</div>
            </main>
          </div>

          <div v-else-if="activeTab === 'messages'" class="kafka-pane">
            <div class="kafka-panel-head">
              <div>
                <div class="kafka-kicker">Message browser</div>
                <h1>{{ selectedTopic?.name || 'No topic selected' }}</h1>
                <p class="kafka-helper">This browser reads Kafka records for inspection. It does not prove an application consumer processed the message; check Consumer Groups for lag, members, and committed offsets.</p>
                <p v-if="selectedTopicIsInternal" class="kafka-helper kafka-helper--warn">This is a Kafka internal topic. Its records are binary protocol metadata, so message browsing is disabled for normal app debugging.</p>
              </div>
              <div class="kafka-inline-controls">
                <select v-model.number="messagePartition" class="base-input kafka-small-input">
                  <option :value="-1">All partitions</option>
                  <option v-for="n in selectedTopic?.partitions || 0" :key="n - 1" :value="n - 1">Partition {{ n - 1 }}</option>
                </select>
                <input v-model.number="messageLimit" class="base-input kafka-count-input" type="number" min="1" max="500" />
                <button class="base-btn base-btn--primary base-btn--sm" :disabled="!selectedTopic || selectedTopicIsInternal || loadingMessages" @click="loadMessages">Load</button>
              </div>
            </div>
            <details class="kafka-table-card kafka-collapsible kafka-message-tools" open>
              <summary>
                <span class="kafka-card-title">Message tools</span>
              </summary>
              <div class="kafka-message-tools__grid">
                <div class="form-group">
                  <label class="form-label">Decode Mode</label>
                  <select v-model="messageDecodeMode" class="base-input">
                    <option value="json">JSON</option>
                    <option value="text">Plain text</option>
                    <option value="base64">Base64</option>
                    <option value="raw">Raw bytes preview</option>
                  </select>
                </div>
                <div class="form-group">
                  <label class="form-label">Replay Target</label>
                  <select v-model="replayTargetTopic" class="base-input">
                    <option value="">Same selected topic</option>
                    <option v-for="topic in topics.filter(item => !isKafkaInternalTopic(item.name))" :key="topic.name" :value="topic.name">{{ topic.name }}</option>
                  </select>
                </div>
                <div class="form-group kafka-form-span">
                  <label class="form-label">Required JSON Fields</label>
                  <textarea v-model="schemaRequiredFields" class="base-input kafka-textarea" placeholder="event&#10;id"></textarea>
                </div>
              </div>
            </details>
            <div v-if="loadingMessages" class="kafka-muted" style="padding:16px">Loading messages…</div>
            <div v-else-if="selectedTopicIsInternal && !messages.length" class="kafka-internal-notice">
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" style="flex-shrink:0;opacity:.7"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
              <div>
                <strong>Internal topic — message browsing disabled</strong>
                <p>{{ selectedTopic?.name }} is a Kafka system topic. Its records contain binary protocol data. Select a regular application topic to browse messages.</p>
              </div>
            </div>
            <div v-else-if="!messages.length" class="kafka-empty-cell" style="padding:24px">
              Click <strong>Load</strong> to fetch the latest messages.
            </div>
            <div v-else-if="!filteredMessages.length" class="kafka-empty-cell" style="padding:24px">
              No messages match the trace filter.
            </div>

            <!-- ── Message stream + detail side panel ──────────────────── -->
            <div v-else class="msg-workspace">
              <!-- Compact message list -->
              <div class="msg-list">
                <!-- Summary bar -->
                <div class="msg-list__bar">
                  <span>{{ filteredMessages.length }} message{{ filteredMessages.length === 1 ? '' : 's' }}</span>
                  <span>
                    <span class="msg-badge msg-badge--error">{{ filteredMessages.filter(m => extractMessageSignals(m).level === 'error').length }} error</span>
                    <span class="msg-badge msg-badge--warn">{{ filteredMessages.filter(m => extractMessageSignals(m).level === 'warn').length }} warn</span>
                  </span>
                </div>

                <div
                  v-for="message in filteredMessages"
                  :key="`${message.partition}:${message.offset}`"
                  class="msg-row"
                  :class="[msgSignalClass(message), { 'msg-row--selected': selectedMessage === message, matched: normalizedTraceQuery && messageMatchesTrace(message, normalizedTraceQuery) }]"
                  @click="openMsgDetail(message)"
                >
                  <!-- Level indicator bar -->
                  <div class="msg-row__level-bar"></div>

                  <div class="msg-row__main">
                    <div class="msg-row__top">
                      <!-- Level badge -->
                      <span class="msg-level-dot" :class="`msg-level-dot--${extractMessageSignals(message).level}`"></span>
                      <!-- Event type or key -->
                      <span class="msg-row__event">
                        {{ extractMessageSignals(message).eventType || message.key || '(no key)' }}
                      </span>
                      <!-- Error excerpt -->
                      <span v-if="extractMessageSignals(message).errorText" class="msg-row__error-excerpt">
                        {{ extractMessageSignals(message).errorText.slice(0, 80) }}{{ extractMessageSignals(message).errorText.length > 80 ? '…' : '' }}
                      </span>
                      <span v-else-if="extractMessageSignals(message).status" class="msg-row__status">
                        {{ extractMessageSignals(message).status }}
                      </span>
                    </div>
                    <div class="msg-row__bottom">
                      <span class="msg-row__meta">p{{ message.partition }}·off{{ message.offset }}</span>
                      <span class="msg-row__meta">{{ new Date(message.timestamp).toLocaleTimeString() }}</span>
                      <span v-if="message.headers?.length" class="msg-row__meta">{{ message.headers.length }} hdr</span>
                      <span v-if="!messageValidation(message).ok" class="msg-row__meta msg-row__meta--warn">⚠ {{ messageValidation(message).reason }}</span>
                    </div>
                  </div>
                  <svg class="msg-row__chevron" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="9 18 15 12 9 6"/></svg>
                </div>
              </div>

              <!-- Detail panel -->
              <div v-if="selectedMessage" class="msg-detail" :class="{ 'msg-detail--full': msgDetailFullscreen }">
                <div class="msg-detail__head">
                  <div class="msg-detail__title">
                    <span class="msg-level-dot" :class="`msg-level-dot--${extractMessageSignals(selectedMessage).level}`" style="width:10px;height:10px"></span>
                    <span>{{ selectedMessage.topic }} · p{{ selectedMessage.partition }} · off{{ selectedMessage.offset }}</span>
                  </div>
                  <div style="display:flex;gap:6px;align-items:center">
                    <button class="base-btn base-btn--ghost base-btn--xs" @click="msgDetailFullscreen = !msgDetailFullscreen">
                      {{ msgDetailFullscreen ? '⊡ Exit' : '⊞ Full' }}
                    </button>
                    <button class="base-btn base-btn--ghost base-btn--xs" @click="selectedMessage = null">✕</button>
                  </div>
                </div>

                <!-- Error alert at the top -->
                <div v-if="extractMessageSignals(selectedMessage).errorText" class="msg-detail__error-alert">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
                  <span>{{ extractMessageSignals(selectedMessage).errorText }}</span>
                </div>

                <!-- Key metadata row -->
                <div class="msg-detail__meta-row">
                  <div class="msg-detail__meta-item"><span>Key</span><code>{{ selectedMessage.key || '(empty)' }}</code></div>
                  <div class="msg-detail__meta-item"><span>Time</span><code>{{ new Date(selectedMessage.timestamp).toLocaleString() }}</code></div>
                  <div class="msg-detail__meta-item"><span>HWM</span><code>{{ selectedMessage.high_water_mark }}</code></div>
                  <div v-if="extractMessageSignals(selectedMessage).status" class="msg-detail__meta-item"><span>Status</span><code>{{ extractMessageSignals(selectedMessage).status }}</code></div>
                </div>

                <!-- Tabs -->
                <div class="msg-detail__tabs">
                  <button :class="{ active: msgDetailTab === 'parsed' }" @click="msgDetailTab = 'parsed'">Fields</button>
                  <button :class="{ active: msgDetailTab === 'raw' }"    @click="msgDetailTab = 'raw'">Raw</button>
                  <button :class="{ active: msgDetailTab === 'headers' }" @click="msgDetailTab = 'headers'">
                    Headers <span v-if="selectedMessage.headers?.length">({{ selectedMessage.headers.length }})</span>
                  </button>
                </div>

                <!-- Fields tab -->
                <div v-if="msgDetailTab === 'parsed'" class="msg-detail__body">
                  <div v-if="!extractMessageSignals(selectedMessage).parsedOk" class="kafka-muted" style="padding:12px">
                    Payload is not valid JSON — switch to Raw tab to see the full value.
                  </div>
                  <table v-else class="msg-fields-table">
                    <tbody>
                      <tr
                        v-for="entry in parsedEntries(selectedMessage)"
                        :key="entry.key"
                        :class="{ 'msg-field--error': entry.isError && entry.value }"
                      >
                        <td class="msg-field__key">{{ entry.key }}</td>
                        <td class="msg-field__value">
                          <pre>{{ formatDetailValue(entry.value) }}</pre>
                        </td>
                      </tr>
                    </tbody>
                  </table>
                </div>

                <!-- Raw tab -->
                <div v-else-if="msgDetailTab === 'raw'" class="msg-detail__body">
                  <div class="msg-detail__raw-toolbar">
                    <select v-model="messageDecodeMode" class="base-input" style="font-size:11px;padding:2px 6px;height:auto">
                      <option value="json">JSON pretty</option>
                      <option value="text">Plain text</option>
                      <option value="base64">Base64</option>
                      <option value="raw">Hex bytes</option>
                    </select>
                    <button class="base-btn base-btn--ghost base-btn--xs" @click="navigator.clipboard.writeText(decodedMessageValue(selectedMessage))">Copy</button>
                  </div>
                  <pre class="msg-detail__pre">{{ decodedMessageValue(selectedMessage) }}</pre>
                </div>

                <!-- Headers tab -->
                <div v-else class="msg-detail__body">
                  <div v-if="!selectedMessage.headers?.length" class="kafka-muted" style="padding:12px">No headers on this message.</div>
                  <table v-else class="msg-fields-table">
                    <tbody>
                      <tr v-for="h in selectedMessage.headers" :key="h.key">
                        <td class="msg-field__key">{{ h.key }}</td>
                        <td class="msg-field__value"><code>{{ h.value }}</code></td>
                      </tr>
                    </tbody>
                  </table>
                </div>

                <!-- Replay button at bottom if allowed -->
                <div v-if="canProduce" class="msg-detail__footer">
                  <button class="base-btn base-btn--ghost base-btn--sm" @click="replayMessage(selectedMessage)">↩ Replay to {{ replayTargetTopic || selectedMessage.topic }}</button>
                </div>
              </div>

              <div v-else class="msg-detail msg-detail--empty">
                <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.2" style="opacity:.3"><rect x="3" y="3" width="18" height="18" rx="2"/><line x1="9" y1="9" x2="15" y2="9"/><line x1="9" y1="13" x2="13" y2="13"/></svg>
                <span>Select a message to inspect</span>
              </div>
            </div>
          </div>

          <div v-else-if="activeTab === 'produce'" class="kafka-pane">
            <div class="kafka-panel-head">
              <div>
                <div class="kafka-kicker">Producer</div>
                <h1>{{ selectedTopic?.name || 'Select a topic first' }}</h1>
              </div>
              <button class="base-btn base-btn--primary base-btn--sm" :disabled="!selectedTopic || producing" @click="produceMessage">Produce Message</button>
            </div>
            <div class="kafka-form-grid">
              <div class="form-group">
                <label class="form-label">Key</label>
                <input v-model="produceKey" class="base-input" placeholder="optional key" />
              </div>
              <div class="form-group">
                <label class="form-label">Headers</label>
                <textarea v-model="produceHeaders" class="base-input kafka-textarea" placeholder="trace_id=abc&#10;source=local"></textarea>
              </div>
              <div class="form-group kafka-form-span">
                <label class="form-label">Value</label>
                <textarea v-model="produceValue" class="base-input kafka-textarea kafka-textarea--value" placeholder='{"hello":"kafka"}'></textarea>
              </div>
            </div>
          </div>

          <div v-else-if="activeTab === 'groups'" class="kafka-pane kafka-groups-layout">
            <div class="kafka-groups-stack">
              <details class="kafka-table-card kafka-collapsible" open>
                <summary>
                  <span class="kafka-card-title">Dashboard test consumer</span>
                </summary>
                <div class="kafka-consumer-form">
                  <div class="form-group">
                    <label class="form-label">Topic</label>
                    <select v-model="testConsumerTopic" class="base-input">
                      <option value="" disabled>Select topic</option>
                      <option v-for="topic in topics.filter(item => !isKafkaInternalTopic(item.name))" :key="topic.name" :value="topic.name">{{ topic.name }}</option>
                    </select>
                  </div>
                  <div class="form-group">
                    <label class="form-label">Group ID</label>
                    <input v-model="testConsumerGroupId" class="base-input" placeholder="nias-dashboard-consumer" />
                  </div>
                  <div class="form-group">
                    <label class="form-label">Max Messages</label>
                    <input v-model.number="testConsumerLimit" class="base-input" type="number" min="1" max="100" />
                  </div>
                  <button class="base-btn base-btn--primary base-btn--sm" :disabled="!testConsumerTopic || !testConsumerGroupId.trim() || consumingTest" @click="runTestConsumer">
                    {{ consumingTest ? 'Consuming...' : 'Start Test Consumer' }}
                  </button>
                </div>
                <p class="kafka-helper">This joins the group, reads records from the selected topic, and commits offsets. It is useful for testing visibility, lag, and basic consumption from the dashboard.</p>
                <div v-if="testConsumeResult" class="kafka-consume-result">
                  <span class="kafka-pill">Consumed {{ testConsumeResult.count }}</span>
                  <span>{{ testConsumeResult.group_id }} / {{ testConsumeResult.topic }}</span>
                </div>
              </details>

              <details class="kafka-table-card kafka-collapsible" open>
                <summary>
                  <span class="kafka-card-title">Consumer Groups</span>
                </summary>
                <div v-if="loadingGroups" class="kafka-muted">Loading consumer groups...</div>
                <table v-else class="kafka-table">
                  <thead>
                    <tr>
                      <th>Group ID</th>
                      <th>Coordinator</th>
                      <th>Protocol</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="group in groups" :key="group.group_id" class="kafka-click-row" :class="{ active: selectedGroupId === group.group_id }" @click="loadGroupDetail(group.group_id)">
                      <td>{{ group.group_id }}</td>
                      <td>{{ group.coordinator }}</td>
                      <td>{{ group.protocol_type || 'unknown' }}</td>
                    </tr>
                    <tr v-if="!groups.length">
                      <td colspan="3" class="kafka-empty-cell">No consumer groups found.</td>
                    </tr>
                  </tbody>
                </table>
              </details>
            </div>

            <section class="kafka-group-detail">
              <div class="kafka-panel-head">
                <div>
                  <div class="kafka-kicker">Group detail</div>
                  <h1>{{ selectedGroupId || 'Select a consumer group' }}</h1>
                </div>
                <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!selectedGroupId || loadingGroupDetail" @click="loadGroupDetail()">Refresh Detail</button>
              </div>
              <div v-if="loadingGroupDetail" class="kafka-muted">Loading group detail...</div>
              <template v-else-if="groupDetail">
                <div v-if="groupConsumptionSummary" class="kafka-summary-card">
                  <div>
                    <div class="kafka-kicker">Consumption summary</div>
                    <h2>{{ groupHealth.label }}</h2>
                    <p>{{ groupHealth.reason }} {{ groupConsumptionSummary.explanation }}</p>
                  </div>
                  <div class="kafka-summary-card__stats">
                    <span>{{ groupConsumptionSummary.topicsTracked }} topics / {{ groupConsumptionSummary.tracked }} partitions</span>
                    <strong>{{ groupConsumptionSummary.lag }} lag</strong>
                  </div>
                </div>
                <div class="kafka-stat-grid">
                  <div><span>State</span><strong>{{ groupDetail.state || 'unknown' }}</strong></div>
                  <div><span>Members</span><strong>{{ groupDetail.members?.length || 0 }}</strong></div>
                  <div><span>Tracked Partitions</span><strong>{{ groupDetail.offsets?.length || 0 }}</strong></div>
                  <div><span>Total Lag</span><strong>{{ groupDetail.total_lag }}</strong></div>
                </div>
                <details class="kafka-table-card kafka-collapsible" open>
                  <summary>
                    <span class="kafka-card-title">Offsets</span>
                  </summary>
                  <table class="kafka-table">
                    <thead><tr><th>Topic</th><th>Partition</th><th>Committed</th><th>Latest</th><th>Lag</th></tr></thead>
                    <tbody>
                      <tr v-for="offset in groupDetail.offsets" :key="`${offset.topic}:${offset.partition}`">
                        <td>{{ offset.topic }}</td>
                        <td>{{ offset.partition }}</td>
                        <td>{{ offset.committed_offset }}</td>
                        <td>{{ offset.latest_offset }}</td>
                        <td>{{ offset.lag }}</td>
                      </tr>
                    </tbody>
                  </table>
                </details>
              </template>
              <div v-else class="kafka-empty-work">Pick a group to inspect offsets, lag, and members.</div>
            </section>
          </div>

          <!-- ── Queue Health ─────────────────────────────────── -->
          <div v-else-if="activeTab === 'health'" class="kafka-pane">
            <div class="kafka-panel-head">
              <div>
                <div class="kafka-kicker">Queue monitoring</div>
                <h1>Queue Health Overview</h1>
              </div>
              <div style="display:flex;gap:8px;align-items:center">
                <div class="qh-filter-row">
                  <button
                    v-for="f in (['all','ok','warn','critical'] as const)"
                    :key="f"
                    class="qh-filter-btn"
                    :class="{ active: healthFilter === f, [`qh-filter-btn--${f}`]: f !== 'all' }"
                    @click="healthFilter = f"
                  >{{ f === 'all' ? 'All' : f === 'ok' ? 'Healthy' : f === 'warn' ? 'Warning' : 'Critical' }}</button>
                </div>
                <button
                  class="base-btn base-btn--ghost base-btn--sm"
                  :disabled="loadingHealth"
                  @click="loadGroupsHealth"
                >
                  <svg v-if="loadingHealth" class="spin" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
                  Refresh
                </button>
              </div>
            </div>

            <!-- Summary banner -->
            <div class="qh-summary-row">
              <div class="qh-stat qh-stat--ok">
                <span class="qh-stat-num">{{ groupsHealth.filter(g => g.health === 'ok').length }}</span>
                <span class="qh-stat-label">Healthy</span>
              </div>
              <div class="qh-stat qh-stat--warn">
                <span class="qh-stat-num">{{ groupsHealth.filter(g => g.health === 'warn').length }}</span>
                <span class="qh-stat-label">Warning</span>
              </div>
              <div class="qh-stat qh-stat--critical">
                <span class="qh-stat-num">{{ groupsHealth.filter(g => g.health === 'critical').length }}</span>
                <span class="qh-stat-label">Critical</span>
              </div>
              <div class="qh-stat">
                <span class="qh-stat-num">{{ groupsHealth.reduce((s,g) => s + g.total_lag, 0).toLocaleString() }}</span>
                <span class="qh-stat-label">Total Lag</span>
              </div>
            </div>

            <div v-if="loadingHealth && !groupsHealth.length" class="kafka-muted" style="padding:24px">
              Loading health for all consumer groups…
            </div>
            <div v-else-if="!groupsHealth.length" class="kafka-empty-work">
              No consumer groups found. Click Refresh to load.
            </div>
            <template v-else>
              <table class="kafka-table qh-table">
                <thead>
                  <tr>
                    <th>Group ID</th>
                    <th>State</th>
                    <th style="text-align:right">Members</th>
                    <th style="text-align:right">Topics</th>
                    <th style="text-align:right">Partitions</th>
                    <th style="text-align:right">Total Lag</th>
                    <th>Status</th>
                    <th>Reason</th>
                  </tr>
                </thead>
                <tbody>
                  <template v-for="g in groupsHealth.filter(g => healthFilter === 'all' || g.health === healthFilter)" :key="g.group_id">
                    <tr
                      class="qh-row kafka-click-row"
                      :class="`qh-row--${g.health}`"
                      @click="activeTab = 'groups'; selectedGroupId = g.group_id; loadGroupDetail(g.group_id)"
                    >
                      <td class="qh-group-id">{{ g.group_id }}</td>
                      <td>
                        <span class="qh-state-badge" :class="`qh-state--${(g.state||'unknown').toLowerCase()}`">
                          {{ g.state || 'unknown' }}
                        </span>
                      </td>
                      <td style="text-align:right">{{ g.members }}</td>
                      <td style="text-align:right">{{ g.topic_count }}</td>
                      <td style="text-align:right">{{ g.part_count }}</td>
                      <td style="text-align:right">
                        <span :class="g.total_lag > 1000 ? 'qh-lag-critical' : g.total_lag > 0 ? 'qh-lag-warn' : 'qh-lag-ok'">
                          {{ g.total_lag.toLocaleString() }}
                        </span>
                      </td>
                      <td>
                        <span class="qh-health-badge" :class="`qh-health--${g.health}`">
                          {{ g.health === 'ok' ? '✓ Healthy' : g.health === 'warn' ? '⚠ Warning' : '✕ Critical' }}
                        </span>
                      </td>
                      <td class="qh-reason">{{ g.error || g.health_reason }}</td>
                    </tr>
                  </template>
                  <tr v-if="groupsHealth.filter(g => healthFilter === 'all' || g.health === healthFilter).length === 0">
                    <td colspan="8" class="kafka-empty-cell">No groups match the selected filter.</td>
                  </tr>
                </tbody>
              </table>
              <p class="kafka-helper" style="margin-top:8px">
                Auto-refreshes every 30 s while this tab is open. Click any row to open full group detail.
              </p>
            </template>
          </div>

          <div v-else class="kafka-pane">
            <div class="kafka-panel-head">
              <div>
                <div class="kafka-kicker">Topic admin</div>
                <h1>Create and manage topics</h1>
              </div>
            </div>
            <div class="kafka-form-grid">
              <div class="form-group">
                <label class="form-label">New Topic</label>
                <input v-model="newTopicName" class="base-input" placeholder="orders.events" />
              </div>
              <div class="form-group">
                <label class="form-label">Partitions</label>
                <input v-model.number="newTopicPartitions" class="base-input" type="number" min="1" />
              </div>
              <div class="form-group">
                <label class="form-label">Replication</label>
                <input v-model.number="newTopicReplication" class="base-input" type="number" min="1" />
              </div>
              <div class="form-group kafka-form-span">
                <label class="form-label">Topic Configs</label>
                <textarea v-model="newTopicConfigs" class="base-input kafka-textarea" placeholder="cleanup.policy=delete&#10;retention.ms=86400000"></textarea>
              </div>
            </div>
            <button class="base-btn base-btn--primary base-btn--sm" :disabled="managingTopic || !newTopicName.trim()" @click="createTopic">Create Topic</button>

            <div class="kafka-manage-danger">
              <div>
                <div class="kafka-kicker">Selected topic</div>
                <h2>{{ selectedTopic?.name || 'Select a topic' }}</h2>
                <p>Partitions can only be increased. Delete is irreversible.</p>
              </div>
              <div class="kafka-inline-controls">
                <input v-model.number="updatePartitionCount" class="base-input kafka-count-input" type="number" min="1" />
                <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!selectedTopic || managingTopic || updatePartitionCount <= (selectedTopic?.partitions || 0)" @click="updatePartitions">Update Partitions</button>
                <button class="base-btn base-btn--danger base-btn--sm" :disabled="!selectedTopic || managingTopic" @click="deleteTopic">Delete Topic</button>
              </div>
            </div>
          </div>
        </section>
      </template>
    </main>
  </div>
</template>

<style scoped>
.kafka-console {
  display: flex;
  flex-direction: column;
  height: calc(100vh - var(--topbar-h) - var(--statusbar-h));
  min-height: 0;
  background:
    radial-gradient(circle at 12% 0%, rgba(92, 184, 165, 0.10), transparent 28%),
    var(--bg-body);
  color: var(--text-primary);
}

.kafka-rail {
  display: flex;
  flex-direction: column;
  min-width: 0;
  border-right: 1px solid rgba(255, 255, 255, 0.08);
  background: #151719;
  padding: 14px 10px;
}

.kafka-brand {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 8px 18px;
}

.kafka-logo {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 8px;
  background: #151719;
  color: #f2f4f7;
  font-size: 12px;
  font-weight: 900;
  flex: 0 0 auto;
}

.kafka-brand strong,
.kafka-brand small {
  display: block;
}

.kafka-brand strong {
  font-size: 14px;
}

.kafka-brand small {
  color: var(--text-muted);
  font-size: 11px;
}

.kafka-rail__section {
  padding: 8px 8px 6px;
  color: var(--text-muted);
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.08em;
}

.kafka-rail__item {
  display: grid;
  grid-template-columns: 24px minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
  width: 100%;
  min-height: 36px;
  border: 1px solid transparent;
  border-radius: 7px;
  background: transparent;
  color: var(--text-secondary);
  font: inherit;
  font-size: 13px;
  text-align: left;
  cursor: pointer;
  padding: 6px 8px;
}

.kafka-rail__item:hover,
.kafka-rail__item.active {
  background: rgba(255, 255, 255, 0.06);
  border-color: rgba(255, 255, 255, 0.08);
  color: var(--text-primary);
}

.kafka-rail__item span:nth-child(2) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kafka-rail__item b {
  color: var(--text-muted);
  font-size: 11px;
}

.kafka-rail__icon {
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  border-radius: 6px;
  background: rgba(255, 255, 255, 0.06);
  color: #f2f4f7;
  font-size: 11px;
  font-weight: 800;
}

.kafka-rail__footer {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: auto;
  padding: 12px 8px 4px;
  color: var(--text-secondary);
  font-size: 12px;
}

.kafka-main {
  flex: 1;
  min-width: 0;
  min-height: 0;
  overflow: auto;
  background: transparent;
}

.kafka-topbar {
  position: sticky;
  top: 0;
  z-index: 8;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  min-height: 68px;
  padding: 14px 22px;
  border-bottom: 1px solid var(--border);
  background: color-mix(in srgb, var(--bg-body) 84%, transparent);
  backdrop-filter: blur(12px);
}

.kafka-topbar__primary {
  display: flex;
  align-items: center;
  min-width: 0;
  gap: 20px;
}

.kafka-cluster {
  display: flex;
  align-items: center;
  min-width: 0;
  gap: 12px;
  flex: 0 0 auto;
}

.kafka-cluster__name {
  color: var(--text-primary);
  font-size: 16px;
  font-weight: 800;
}

.kafka-cluster__meta,
.kafka-muted,
.kafka-empty__sub {
  color: var(--text-muted);
  font-size: 12px;
}

.kafka-topbar__actions,
.kafka-inline-controls {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.kafka-select {
  width: 230px;
}

.kafka-tabs {
  display: flex;
  gap: 6px;
  min-width: 0;
  padding: 0;
  overflow-x: auto;
}

.kafka-tab {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-height: 34px;
  border: 1px solid transparent;
  border-radius: 8px;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font: inherit;
  font-size: 13px;
  font-weight: 700;
  padding: 7px 12px;
  white-space: nowrap;
}

.kafka-tab:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.kafka-tab.active {
  border-color: var(--border);
  background: var(--bg-surface);
  color: var(--text-primary);
  box-shadow: var(--shadow-sm);
}

.kafka-tab b {
  display: inline-grid;
  place-items: center;
  min-width: 22px;
  height: 20px;
  border-radius: 999px;
  background: var(--bg-elevated);
  color: var(--text-muted);
  font-size: 11px;
  padding: 0 6px;
}

.status-dot {
  width: 9px;
  height: 9px;
  border-radius: 999px;
  background: var(--success);
  box-shadow: 0 0 0 3px var(--success-bg);
  flex: 0 0 auto;
}

.status-dot.warn {
  background: var(--warning);
  box-shadow: 0 0 0 3px var(--warning-bg);
}

.kafka-metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
  padding: 16px 22px;
}

.kafka-metric {
  min-width: 0;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-surface);
  padding: 14px;
}

.kafka-metric span,
.kafka-stat-grid span {
  display: block;
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.kafka-metric strong {
  display: block;
  margin-top: 8px;
  color: var(--text-primary);
  font-size: 28px;
  line-height: 1;
}

.kafka-metric small {
  display: block;
  margin-top: 8px;
  color: var(--text-secondary);
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kafka-metric.danger strong {
  color: var(--warning);
}

.kafka-workspace {
  margin: 0 22px 22px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-surface);
  overflow: hidden;
  box-shadow: var(--shadow-sm);
}

.kafka-trace-search {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: end;
  gap: 14px;
  margin: 0 22px 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-surface);
  padding: 12px;
  box-shadow: var(--shadow-sm);
}

.kafka-trace-search .base-input {
  margin-top: 6px;
}

.kafka-trace-search__meta {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--text-muted);
  font-size: 12px;
  white-space: nowrap;
}

.kafka-trace-view {
  margin: 0 22px 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-surface);
  padding: 12px;
  box-shadow: var(--shadow-sm);
}

.kafka-trace-view > summary strong {
  display: block;
  margin-top: 3px;
  color: var(--text-primary);
  font-size: 15px;
  overflow-wrap: anywhere;
}

.kafka-trace-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.1fr) minmax(260px, 0.8fr);
  gap: 14px;
  margin-top: 12px;
}

.kafka-timeline {
  display: grid;
  gap: 10px;
}

.kafka-timeline__item {
  display: grid;
  grid-template-columns: 18px minmax(0, 1fr);
  gap: 10px;
}

.kafka-timeline__item > span {
  width: 12px;
  height: 12px;
  border-radius: 999px;
  margin-top: 4px;
  background: var(--text-muted);
  box-shadow: 0 0 0 4px var(--bg-elevated);
}

.kafka-timeline__item.ok > span {
  background: var(--success);
}

.kafka-timeline__item.error > span {
  background: var(--warning);
}

.kafka-timeline__item.pending > span {
  background: var(--text-muted);
}

.kafka-timeline__item strong {
  color: var(--text-primary);
  font-size: 13px;
}

.kafka-timeline__item p {
  margin-top: 2px;
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.45;
}

.kafka-trace-config {
  min-width: 0;
}

.kafka-collapsible {
  min-width: 0;
}

.kafka-collapsible > summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-height: 42px;
  cursor: pointer;
  list-style: none;
  user-select: none;
}

.kafka-collapsible > summary::-webkit-details-marker {
  display: none;
}

.kafka-collapsible > summary::after {
  content: '▾';
  color: var(--text-muted);
  font-size: 12px;
  transition: transform var(--dur) var(--ease);
}

.kafka-collapsible[open] > summary::after {
  transform: rotate(180deg);
}

.kafka-table-card.kafka-collapsible > summary {
  border-bottom: 1px solid var(--border);
  padding: 0 12px;
  position: sticky;
  top: 0;
  z-index: 2;
  background: var(--bg-surface);
}

.kafka-table-card.kafka-collapsible:not([open]) > summary {
  border-bottom: 0;
}

.kafka-table-card.kafka-collapsible[open] {
  max-height: 430px;
  overflow: auto;
}

.kafka-collapsible .kafka-card-title {
  border-bottom: 0;
  padding: 0;
}

.kafka-diagnostic {
  margin: 0 22px 14px;
  border: 1px solid color-mix(in srgb, var(--warning) 35%, var(--border));
  border-radius: 8px;
  background: color-mix(in srgb, var(--warning-bg) 42%, var(--bg-surface));
  padding: 14px;
  box-shadow: var(--shadow-sm);
}

.kafka-diagnostic__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
  margin-bottom: 12px;
}

.kafka-diagnostic__head h2 {
  margin: 4px 0 0;
  color: var(--text-primary);
  font-size: 16px;
  line-height: 1.35;
}

.kafka-diagnostic__grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.4fr);
  gap: 10px;
  margin-bottom: 12px;
}

.kafka-diagnostic__grid div {
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-surface);
  padding: 10px;
  min-width: 0;
}

.kafka-diagnostic__grid span {
  display: block;
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 800;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.kafka-diagnostic__grid strong {
  display: block;
  margin-top: 4px;
  color: var(--text-primary);
  font-size: 12px;
  overflow-wrap: anywhere;
}

.kafka-diagnostic__body {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(280px, 0.8fr);
  gap: 10px;
  margin-bottom: 12px;
  max-height: 260px;
  overflow: auto;
}

.kafka-diagnostic__body > div {
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-surface);
  overflow: hidden;
}

.kafka-diagnostic ul {
  padding: 10px 12px 12px 28px;
  color: var(--text-secondary);
  font-size: 13px;
}

.kafka-diagnostic li + li {
  margin-top: 6px;
}

.kafka-diagnostic dl {
  display: grid;
  grid-template-columns: minmax(100px, 0.45fr) minmax(0, 1fr);
  gap: 0;
  font-size: 12px;
}

.kafka-diagnostic dt,
.kafka-diagnostic dd {
  border-top: 1px solid var(--border);
  padding: 8px 10px;
  min-width: 0;
}

.kafka-diagnostic dt {
  color: var(--text-muted);
  font-weight: 700;
}

.kafka-diagnostic dd {
  color: var(--text-primary);
  overflow-wrap: anywhere;
}

.kafka-diagnostic pre {
  max-height: 140px;
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-elevated);
  color: var(--text-primary);
  padding: 10px;
  font-family: var(--mono);
  font-size: 12px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.kafka-activity {
  margin: 0 22px 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-surface);
  padding: 12px;
  box-shadow: var(--shadow-sm);
}

.kafka-activity > summary {
  min-height: 0;
  margin-bottom: 10px;
}

.kafka-activity:not([open]) > summary {
  margin-bottom: 0;
}

.kafka-activity > summary strong {
  display: block;
  margin-top: 3px;
  color: var(--text-primary);
  font-size: 15px;
}

.kafka-activity__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
  margin-bottom: 10px;
}

.kafka-activity__head h2 {
  margin: 3px 0 0;
  color: var(--text-primary);
  font-size: 15px;
}

.kafka-activity__list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 260px;
  overflow: auto;
  padding-right: 2px;
}

.kafka-activity__item {
  display: grid;
  grid-template-columns: 44px minmax(150px, 0.8fr) minmax(0, 1.2fr) auto;
  align-items: center;
  gap: 10px;
  width: 100%;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-elevated);
  color: var(--text-primary);
  cursor: default;
  padding: 9px 10px;
  text-align: left;
}

.kafka-activity__item.failed {
  border-color: color-mix(in srgb, var(--warning) 35%, var(--border));
  background: color-mix(in srgb, var(--warning-bg) 28%, var(--bg-elevated));
  cursor: pointer;
}

.kafka-activity__status {
  display: inline-grid;
  place-items: center;
  height: 24px;
  border-radius: 999px;
  background: var(--success-bg);
  color: var(--success);
  font-size: 10px;
  font-weight: 900;
}

.kafka-activity__item.failed .kafka-activity__status {
  background: var(--warning-bg);
  color: var(--warning);
}

.kafka-activity__main {
  min-width: 0;
}

.kafka-activity__main strong,
.kafka-activity__main small {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kafka-activity__main strong {
  font-size: 12px;
}

.kafka-activity__main small,
.kafka-activity__meta,
.kafka-activity__reason {
  color: var(--text-muted);
  font-size: 12px;
}

.kafka-activity__reason {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.kafka-activity__meta {
  white-space: nowrap;
}

.kafka-topic-layout {
  display: grid;
  grid-template-columns: minmax(280px, 380px) minmax(0, 1fr);
  min-height: 600px;
  max-height: calc(100vh - 280px);
}

.kafka-topic-list {
  min-width: 0;
  border-right: 1px solid var(--border);
  background: color-mix(in srgb, var(--bg-surface) 82%, var(--bg-elevated));
  overflow: auto;
}

.kafka-list-head {
  padding: 12px;
  border-bottom: 1px solid var(--border);
  position: sticky;
  top: 0;
  z-index: 2;
  background: color-mix(in srgb, var(--bg-surface) 90%, var(--bg-elevated));
}

.kafka-search {
  height: 34px;
}

.kafka-toggle-row {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-top: 9px;
  color: var(--text-muted);
  font-size: 12px;
  user-select: none;
}

.kafka-toggle-row input {
  accent-color: var(--brand);
}

.kafka-topic-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 4px 10px;
  width: 100%;
  border: 0;
  border-bottom: 1px solid var(--border);
  background: transparent;
  color: var(--text-primary);
  text-align: left;
  cursor: pointer;
  padding: 11px 12px;
}

.kafka-topic-row:hover,
.kafka-topic-row.active {
  background: var(--bg-elevated);
}

.kafka-topic-row.active {
  box-shadow: inset 3px 0 0 var(--brand);
}

.kafka-topic-row.danger {
  box-shadow: inset 3px 0 0 var(--warning);
}

.topic-name {
  min-width: 0;
  overflow-wrap: anywhere;
  font-size: 13px;
  font-weight: 700;
}

.topic-meta,
.topic-leader {
  color: var(--text-muted);
  font-size: 11px;
}

.topic-meta {
  grid-column: 1;
}

.topic-leader {
  grid-column: 2;
  grid-row: 1 / span 2;
  align-self: center;
  white-space: nowrap;
}

.kafka-detail,
.kafka-pane,
.kafka-empty {
  min-width: 0;
  padding: 18px;
  overflow: auto;
}

.kafka-panel-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 18px;
}

.kafka-kicker {
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 800;
  text-transform: uppercase;
  letter-spacing: 0.08em;
}

.kafka-panel-head h1,
.kafka-manage-danger h2 {
  margin: 4px 0 0;
  color: var(--text-primary);
  font-size: 20px;
  line-height: 1.25;
  overflow-wrap: anywhere;
}

.kafka-helper {
  max-width: 760px;
  margin-top: 6px;
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.5;
}

.kafka-helper--warn {
  color: var(--warning);
}

.kafka-manage-danger h2 {
  font-size: 16px;
}

/* ── Message workspace (list + detail) ─────────────────────── */
.msg-workspace {
  display: flex;
  flex: 1;
  min-height: 0;
  gap: 0;
  overflow: hidden;
  margin-top: 8px;
  border: 1px solid var(--border);
  border-radius: 8px;
}

/* ── Message list ── */
.msg-list {
  flex: 0 0 380px;
  min-width: 260px;
  max-width: 460px;
  display: flex;
  flex-direction: column;
  overflow-y: auto;
  border-right: 1px solid var(--border);
  background: var(--bg-surface);
}
.msg-list__bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 12px;
  border-bottom: 1px solid var(--border);
  font-size: 11px;
  color: var(--text-muted);
  background: var(--bg-elevated);
  gap: 8px;
  flex-shrink: 0;
}
.msg-badge {
  display: inline-block;
  padding: 1px 6px;
  border-radius: 3px;
  font-size: 10px;
  font-weight: 600;
  margin-left: 4px;
}
.msg-badge--error { background: rgba(239,68,68,.15); color: var(--danger,#ef4444); }
.msg-badge--warn  { background: rgba(245,158,11,.12); color: #f59e0b; }

.msg-row {
  display: flex;
  align-items: center;
  gap: 0;
  padding: 0;
  border-bottom: 1px solid var(--border);
  cursor: pointer;
  transition: background 0.1s;
  position: relative;
}
.msg-row:hover { background: var(--bg-hover); }
.msg-row--selected { background: color-mix(in srgb, var(--brand) 8%, transparent) !important; }

.msg-row__level-bar {
  width: 3px;
  align-self: stretch;
  flex-shrink: 0;
  background: var(--border);
  border-radius: 0;
}
.msg-row--error .msg-row__level-bar { background: var(--danger,#ef4444); }
.msg-row--warn  .msg-row__level-bar { background: #f59e0b; }
.msg-row--ok    .msg-row__level-bar { background: #22c55e; }

.msg-row__main {
  flex: 1;
  min-width: 0;
  padding: 8px 10px;
}
.msg-row__top {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}
.msg-level-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex-shrink: 0;
  background: var(--text-muted);
}
.msg-level-dot--error  { background: var(--danger,#ef4444); }
.msg-level-dot--warn   { background: #f59e0b; }
.msg-level-dot--ok     { background: #22c55e; }
.msg-level-dot--unknown { background: var(--text-muted); }

.msg-row__event {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 140px;
  flex-shrink: 0;
}
.msg-row__error-excerpt {
  font-size: 11px;
  color: var(--danger,#ef4444);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  flex: 1;
  min-width: 0;
}
.msg-row__status {
  font-size: 11px;
  color: var(--text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  flex: 1;
  min-width: 0;
}
.msg-row__bottom {
  display: flex;
  gap: 8px;
  margin-top: 3px;
  flex-wrap: nowrap;
  overflow: hidden;
}
.msg-row__meta {
  font-size: 10px;
  color: var(--text-muted);
  white-space: nowrap;
  font-family: var(--mono);
}
.msg-row__meta--warn { color: #f59e0b; }
.msg-row__chevron {
  color: var(--text-muted);
  flex-shrink: 0;
  margin-right: 8px;
  opacity: 0;
}
.msg-row:hover .msg-row__chevron,
.msg-row--selected .msg-row__chevron { opacity: 1; }

/* ── Detail panel ── */
.msg-detail {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--bg-surface);
}
.msg-detail--full {
  position: fixed;
  inset: 0;
  z-index: 200;
  background: var(--bg-surface);
}
.msg-detail--empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--text-muted);
  font-size: 13px;
}
.msg-detail__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-elevated);
  flex-shrink: 0;
  gap: 8px;
}
.msg-detail__title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  font-family: var(--mono);
  color: var(--text-primary);
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.msg-detail__error-alert {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin: 10px 14px 0;
  padding: 10px 12px;
  background: rgba(239,68,68,.08);
  border: 1px solid rgba(239,68,68,.25);
  border-radius: 6px;
  color: var(--danger,#ef4444);
  font-size: 12px;
  line-height: 1.5;
  flex-shrink: 0;
}
.msg-detail__meta-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
  background: var(--bg-elevated);
}
.msg-detail__meta-item {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
}
.msg-detail__meta-item span { color: var(--text-muted); }
.msg-detail__meta-item code { font-family: var(--mono); font-size: 11px; color: var(--text-primary); }

.msg-detail__tabs {
  display: flex;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
  background: var(--bg-elevated);
}
.msg-detail__tabs button {
  padding: 7px 14px;
  font-size: 12px;
  background: none;
  border: none;
  border-bottom: 2px solid transparent;
  color: var(--text-muted);
  cursor: pointer;
  transition: all 0.12s;
}
.msg-detail__tabs button:hover  { color: var(--text-primary); }
.msg-detail__tabs button.active { color: var(--brand); border-bottom-color: var(--brand); }

.msg-detail__body {
  flex: 1;
  overflow-y: auto;
  min-height: 0;
}
.msg-detail__raw-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 14px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-elevated);
}
.msg-detail__pre {
  padding: 14px;
  margin: 0;
  font-size: 12px;
  font-family: var(--mono);
  white-space: pre-wrap;
  word-break: break-all;
  color: var(--text-primary);
  line-height: 1.6;
}
.msg-detail__footer {
  border-top: 1px solid var(--border);
  padding: 8px 14px;
  flex-shrink: 0;
  background: var(--bg-elevated);
}

/* ── Fields table ── */
.msg-fields-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}
.msg-fields-table tr { border-bottom: 1px solid var(--border); }
.msg-fields-table tr:last-child { border-bottom: none; }
.msg-field--error td { background: rgba(239,68,68,.05); }
.msg-field__key {
  padding: 7px 12px;
  font-family: var(--mono);
  font-weight: 600;
  color: var(--text-secondary);
  white-space: nowrap;
  width: 160px;
  vertical-align: top;
}
.msg-field--error .msg-field__key { color: var(--danger,#ef4444); }
.msg-field__value {
  padding: 7px 12px;
  color: var(--text-primary);
  word-break: break-all;
}
.msg-field__value pre {
  margin: 0;
  font-size: 12px;
  font-family: var(--mono);
  white-space: pre-wrap;
  word-break: break-word;
}

/* ── Internal topic notice ──────────────────────────────────── */
.kafka-internal-notice {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 16px;
  margin: 12px 0;
  background: color-mix(in srgb, var(--bg-elevated) 80%, transparent);
  border: 1px solid var(--border);
  border-radius: 8px;
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.5;
}
.kafka-internal-notice strong { display: block; margin-bottom: 4px; color: var(--text-primary); }
.kafka-internal-notice p { margin: 0; color: var(--text-muted); font-size: 12px; }

/* ── Queue Health tab ──────────────────────────────────────── */
.qh-filter-row {
  display: flex;
  gap: 4px;
}
.qh-filter-btn {
  padding: 3px 10px;
  border-radius: 4px;
  border: 1px solid var(--border);
  background: transparent;
  color: var(--text-secondary);
  font-size: 12px;
  cursor: pointer;
  transition: all 0.12s;
}
.qh-filter-btn.active,
.qh-filter-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
.qh-filter-btn--ok.active    { border-color: #22c55e; color: #22c55e; }
.qh-filter-btn--warn.active  { border-color: #f59e0b; color: #f59e0b; }
.qh-filter-btn--critical.active { border-color: var(--danger,#ef4444); color: var(--danger,#ef4444); }

.qh-summary-row {
  display: flex;
  gap: 12px;
  margin: 16px 0;
  flex-wrap: wrap;
}
.qh-stat {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 12px 20px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 8px;
  min-width: 90px;
}
.qh-stat-num {
  font-size: 22px;
  font-weight: 700;
  line-height: 1;
  color: var(--text-primary);
}
.qh-stat-label {
  font-size: 11px;
  color: var(--text-muted);
  margin-top: 4px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.qh-stat--ok     .qh-stat-num { color: #22c55e; }
.qh-stat--warn   .qh-stat-num { color: #f59e0b; }
.qh-stat--critical .qh-stat-num { color: var(--danger,#ef4444); }

.qh-table { margin-top: 8px; }
.qh-row--warn td   { background: rgba(245,158,11,0.04); }
.qh-row--critical td { background: rgba(239,68,68,0.06); }

.qh-group-id { font-family: var(--mono); font-size: 12px; max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.qh-reason   { font-size: 12px; color: var(--text-muted); max-width: 300px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

.qh-health-badge {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
  white-space: nowrap;
}
.qh-health--ok       { background: rgba(34,197,94,.15);  color: #22c55e; }
.qh-health--warn     { background: rgba(245,158,11,.15); color: #f59e0b; }
.qh-health--critical { background: rgba(239,68,68,.15);  color: var(--danger,#ef4444); }

.qh-state-badge {
  display: inline-block;
  padding: 1px 6px;
  border-radius: 3px;
  font-size: 11px;
  background: var(--bg-hover);
  color: var(--text-secondary);
  white-space: nowrap;
}
.qh-state--stable { background: rgba(34,197,94,.1); color: #22c55e; }
.qh-state--dead   { background: rgba(239,68,68,.1); color: var(--danger,#ef4444); }
.qh-state--empty  { background: rgba(245,158,11,.1); color: #f59e0b; }

.qh-lag-ok       { color: #22c55e; font-weight: 600; }
.qh-lag-warn     { color: #f59e0b; font-weight: 600; }
.qh-lag-critical { color: var(--danger,#ef4444); font-weight: 600; }

.kafka-manage-danger p {
  margin-top: 4px;
  color: var(--text-muted);
  font-size: 12px;
}

.kafka-stat-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
  margin-bottom: 18px;
}

.kafka-stat-grid div {
  min-width: 0;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-elevated);
  padding: 13px;
}

.kafka-stat-grid strong {
  display: block;
  margin-top: 8px;
  color: var(--text-primary);
  font-size: 20px;
  overflow-wrap: anywhere;
}

.kafka-table-card {
  border: 1px solid var(--border);
  border-radius: 8px;
  overflow: hidden;
  background: var(--bg-surface);
}

.kafka-consumers-card {
  margin-top: 14px;
}

.kafka-summary-card {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--success-bg);
  padding: 14px;
  margin-bottom: 14px;
}

.kafka-summary-card h2 {
  margin: 4px 0 0;
  color: var(--text-primary);
  font-size: 18px;
}

.kafka-summary-card p {
  margin-top: 6px;
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.45;
}

.kafka-summary-card__stats {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 5px;
  flex: 0 0 auto;
}

.kafka-summary-card__stats span {
  color: var(--text-muted);
  font-size: 12px;
}

.kafka-summary-card__stats strong {
  color: var(--success);
  font-size: 20px;
  line-height: 1;
}

.kafka-card-title {
  padding: 11px 12px;
  border-bottom: 1px solid var(--border);
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 800;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.kafka-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.kafka-table th,
.kafka-table td {
  padding: 11px 12px;
  border-bottom: 1px solid var(--border);
  color: var(--text-primary);
  text-align: left;
  vertical-align: top;
}

.kafka-table th {
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 800;
  text-transform: uppercase;
  letter-spacing: 0.08em;
}

.kafka-table tr:last-child td {
  border-bottom: 0;
}

.kafka-click-row {
  cursor: pointer;
}

.kafka-click-row:hover,
.kafka-click-row.active {
  background: var(--bg-elevated);
}

.kafka-pill {
  display: inline-flex;
  align-items: center;
  min-height: 24px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--success-bg);
  color: var(--success);
  padding: 3px 9px;
  font-size: 11px;
  font-weight: 800;
  white-space: nowrap;
}

.kafka-pill.warn {
  background: var(--warning-bg);
  color: var(--warning);
}

.kafka-message-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-height: calc(100vh - 360px);
  overflow: auto;
  padding-right: 2px;
}

.kafka-message-tools {
  margin-bottom: 12px;
}

.kafka-message-tools__grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 12px;
  padding: 12px;
}

.kafka-message {
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-surface);
  padding: 12px;
}

.kafka-message.matched {
  border-color: color-mix(in srgb, var(--brand) 45%, var(--border));
  box-shadow: inset 3px 0 0 var(--brand);
}

.kafka-message__actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 10px;
}

.kafka-message__meta,
.kafka-message__kv,
.kafka-message__headers {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  color: var(--text-muted);
  font-size: 12px;
}

.kafka-message__kv {
  margin-top: 8px;
}

.kafka-message__kv code,
.kafka-message pre,
.kafka-textarea {
  font-family: var(--mono);
}

.kafka-message__kv code,
.kafka-message pre {
  color: var(--text-primary);
}

.kafka-message pre {
  margin: 10px 0 0;
  max-height: 300px;
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-elevated);
  padding: 10px;
  font-size: 12px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.kafka-message__headers {
  margin-top: 10px;
}

.kafka-message__headers span {
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--bg-elevated);
  padding: 3px 8px;
}

.kafka-form-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 14px;
}

.kafka-form-span {
  grid-column: 1 / -1;
}

.kafka-textarea {
  min-height: 90px;
  resize: vertical;
  font-size: 12px;
}

.kafka-textarea--value {
  min-height: 220px;
}

.kafka-groups-layout {
  display: grid;
  grid-template-columns: minmax(320px, 0.95fr) minmax(0, 1.45fr);
  gap: 16px;
}

.kafka-groups-stack {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-width: 0;
  max-height: calc(100vh - 320px);
  overflow: auto;
  padding-right: 2px;
}

.kafka-group-detail {
  min-width: 0;
  max-height: calc(100vh - 320px);
  overflow: auto;
  padding-right: 2px;
}

.kafka-consumer-form {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) 110px auto;
  align-items: end;
  gap: 10px;
  padding: 12px;
}

.kafka-consume-result {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  border-top: 1px solid var(--border);
  padding: 10px 12px;
  color: var(--text-secondary);
  font-size: 12px;
}

.kafka-manage-danger {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-top: 20px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-elevated);
  padding: 14px;
}

.kafka-small-input {
  width: 160px;
}

.kafka-count-input {
  width: 92px;
}

.kafka-empty__title {
  color: var(--text-primary);
  font-size: 15px;
  font-weight: 800;
}

.kafka-empty-work,
.kafka-empty-cell {
  color: var(--text-muted) !important;
  font-size: 13px;
}

.kafka-empty-work {
  padding: 28px 0;
}

.kafka-empty-cell {
  padding: 28px 12px !important;
  text-align: center !important;
}

@media (max-width: 1100px) {
  .kafka-console {
    height: calc(100vh - var(--topbar-h) - var(--statusbar-h));
  }

  .kafka-metrics,
  .kafka-stat-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .kafka-groups-layout {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 760px) {
  .kafka-console {
    height: auto;
    min-height: calc(100vh - var(--topbar-h) - var(--statusbar-h));
  }

  .kafka-topbar,
  .kafka-topbar__primary,
  .kafka-panel-head,
  .kafka-diagnostic__head,
  .kafka-activity__head,
  .kafka-summary-card,
  .kafka-manage-danger {
    flex-direction: column;
    align-items: stretch;
  }

  .kafka-summary-card__stats {
    align-items: flex-start;
  }

  .kafka-topbar__actions,
  .kafka-inline-controls {
    align-items: stretch;
  }

  .kafka-topbar {
    padding: 14px;
  }

  .kafka-tabs {
    padding: 0;
  }

  .kafka-metrics {
    padding: 14px;
  }

  .kafka-workspace {
    margin: 0 14px 14px;
  }

  .kafka-trace-search {
    grid-template-columns: 1fr;
    margin: 0 14px 14px;
  }

  .kafka-trace-view {
    margin: 0 14px 14px;
  }

  .kafka-trace-grid,
  .kafka-message-tools__grid {
    grid-template-columns: 1fr;
  }

  .kafka-trace-search__meta {
    justify-content: space-between;
    white-space: normal;
  }

  .kafka-diagnostic {
    margin: 0 14px 14px;
  }

  .kafka-activity {
    margin: 0 14px 14px;
  }

  .kafka-diagnostic__grid,
  .kafka-diagnostic__body {
    grid-template-columns: 1fr;
  }

  .kafka-activity__item {
    grid-template-columns: 44px minmax(0, 1fr);
  }

  .kafka-activity__reason,
  .kafka-activity__meta {
    grid-column: 2;
  }

  .kafka-consumer-form {
    grid-template-columns: 1fr;
  }

  .kafka-select,
  .kafka-small-input,
  .kafka-count-input {
    width: 100%;
  }

  .kafka-metrics,
  .kafka-stat-grid,
  .kafka-form-grid,
  .kafka-topic-layout {
    grid-template-columns: 1fr;
  }

  .kafka-topic-layout,
  .kafka-message-list,
  .kafka-groups-stack,
  .kafka-group-detail {
    max-height: none;
  }

  .kafka-table-card.kafka-collapsible[open],
  .kafka-activity__list,
  .kafka-diagnostic__body {
    max-height: 360px;
  }

  .kafka-topic-list {
    border-right: 0;
    border-bottom: 1px solid var(--border);
    max-height: 360px;
  }
}
</style>
