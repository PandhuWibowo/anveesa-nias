<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
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

interface KafkaGroupDetail {
  group_id: string
  state: string
  total_lag: number
  members: Array<{ member_id: string; client_id: string; client_host: string; assignments: Array<{ topic: string; partitions: number[] }> }>
  offsets: Array<{ topic: string; partition: number; committed_offset: number; latest_offset: number; lag: number; error?: string }>
  error?: string
}

const { connections, fetchConnections } = useConnections()
const { hasAnyPermission } = useAuth()
const toast = useToast()

const topics = ref<KafkaTopic[]>([])
const groups = ref<KafkaGroup[]>([])
const loadingTopics = ref(false)
const loadingGroups = ref(false)
const topicFilter = ref('')
const activeTab = ref<'topics' | 'messages' | 'produce' | 'groups' | 'manage'>('topics')
const selectedTopic = ref<KafkaTopic | null>(null)
const messages = ref<KafkaMessage[]>([])
const loadingMessages = ref(false)
const messagePartition = ref(-1)
const messageLimit = ref(50)
const produceKey = ref('')
const produceValue = ref('')
const produceHeaders = ref('')
const producing = ref(false)
const selectedGroupId = ref('')
const groupDetail = ref<KafkaGroupDetail | null>(null)
const loadingGroupDetail = ref(false)
const newTopicName = ref('')
const newTopicPartitions = ref(3)
const newTopicReplication = ref(1)
const newTopicConfigs = ref('')
const updatePartitionCount = ref(0)
const managingTopic = ref(false)

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
  if (!query) return topics.value
  return topics.value.filter(topic => topic.name.toLowerCase().includes(query))
})

const totalPartitions = computed(() => topics.value.reduce((sum, topic) => sum + topic.partitions, 0))
const averageReplication = computed(() => {
  if (!topics.value.length) return '0'
  const total = topics.value.reduce((sum, topic) => sum + topic.replication_factor, 0)
  return (total / topics.value.length).toFixed(1)
})

async function loadKafka() {
  if (!activeConn.value) return
  await Promise.all([loadTopics(), loadGroups()])
}

async function loadTopics() {
  if (!activeConn.value) return
  loadingTopics.value = true
  try {
    const { data } = await axios.get<KafkaTopic[]>(`/api/connections/${activeConn.value.id}/kafka/topics`)
    topics.value = data || []
    selectedTopic.value = topics.value[0] ?? null
  } catch (error: any) {
    topics.value = []
    selectedTopic.value = null
    toast.error(error.response?.data?.error || 'Failed to load Kafka topics')
  } finally {
    loadingTopics.value = false
  }
}

async function loadGroups() {
  if (!activeConn.value) return
  loadingGroups.value = true
  try {
    const { data } = await axios.get<KafkaGroup[]>(`/api/connections/${activeConn.value.id}/kafka/groups`)
    groups.value = data || []
  } catch (error: any) {
    groups.value = []
    toast.error(error.response?.data?.error || 'Failed to load Kafka consumer groups')
  } finally {
    loadingGroups.value = false
  }
}

async function loadMessages() {
  if (!activeConn.value || !selectedTopic.value) return
  loadingMessages.value = true
  try {
    const { data } = await axios.get<KafkaMessage[]>(`/api/connections/${activeConn.value.id}/kafka/messages`, {
      params: { topic: selectedTopic.value.name, partition: messagePartition.value, limit: messageLimit.value },
    })
    messages.value = data || []
  } catch (error: any) {
    messages.value = []
    toast.error(error.response?.data?.error || 'Failed to load Kafka messages')
  } finally {
    loadingMessages.value = false
  }
}

async function produceMessage() {
  if (!activeConn.value || !selectedTopic.value) return
  producing.value = true
  try {
    await axios.post(`/api/connections/${activeConn.value.id}/kafka/produce`, {
      topic: selectedTopic.value.name,
      key: produceKey.value,
      value: produceValue.value,
      headers: parseHeaders(produceHeaders.value),
    })
    toast.success('Kafka message produced')
    produceValue.value = ''
    await loadMessages()
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to produce Kafka message')
  } finally {
    producing.value = false
  }
}

async function loadGroupDetail(groupId = selectedGroupId.value) {
  if (!activeConn.value || !groupId) return
  selectedGroupId.value = groupId
  loadingGroupDetail.value = true
  try {
    const { data } = await axios.get<KafkaGroupDetail>(`/api/connections/${activeConn.value.id}/kafka/groups-detail`, {
      params: { group_id: groupId },
    })
    groupDetail.value = data
  } catch (error: any) {
    groupDetail.value = null
    toast.error(error.response?.data?.error || 'Failed to load Kafka group detail')
  } finally {
    loadingGroupDetail.value = false
  }
}

async function createTopic() {
  if (!activeConn.value || !newTopicName.value.trim()) return
  managingTopic.value = true
  try {
    await axios.post(`/api/connections/${activeConn.value.id}/kafka/topics`, {
      topic: newTopicName.value.trim(),
      partitions: newTopicPartitions.value,
      replication_factor: newTopicReplication.value,
      configs: parseConfigLines(newTopicConfigs.value),
    })
    toast.success('Kafka topic created')
    newTopicName.value = ''
    newTopicConfigs.value = ''
    await loadTopics()
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to create Kafka topic')
  } finally {
    managingTopic.value = false
  }
}

async function updatePartitions() {
  if (!activeConn.value || !selectedTopic.value || updatePartitionCount.value <= selectedTopic.value.partitions) return
  managingTopic.value = true
  try {
    await axios.put(`/api/connections/${activeConn.value.id}/kafka/topics/partitions`, {
      topic: selectedTopic.value.name,
      partitions: updatePartitionCount.value,
    })
    toast.success('Kafka partitions updated')
    await loadTopics()
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to update Kafka partitions')
  } finally {
    managingTopic.value = false
  }
}

async function deleteTopic() {
  if (!activeConn.value || !selectedTopic.value) return
  const topic = selectedTopic.value.name
  if (!window.confirm(`Delete Kafka topic "${topic}"? This cannot be undone.`)) return
  managingTopic.value = true
  try {
    await axios.delete(`/api/connections/${activeConn.value.id}/kafka/topics`, { params: { topic } })
    toast.success('Kafka topic deleted')
    await loadTopics()
  } catch (error: any) {
    toast.error(error.response?.data?.error || 'Failed to delete Kafka topic')
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

function selectTopic(topic: KafkaTopic) {
  selectedTopic.value = topic
  updatePartitionCount.value = topic.partitions
  messages.value = []
}

function selectConnection(id: number) {
  emit('set-conn', id)
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

watch(selectedTopic, (topic) => {
  updatePartitionCount.value = topic?.partitions ?? 0
})
</script>

<template>
  <div class="page-shell kafka-page">
    <div class="page-scroll">
      <div class="page-stack">
        <section class="page-hero">
          <div class="page-hero__content">
            <div class="page-kicker">Messaging</div>
            <div class="page-title">Kafka Browser</div>
            <div class="page-subtitle">Inspect Kafka topics, partitions, replication, and consumer groups from configured Kafka connections.</div>
          </div>
          <div class="page-hero__actions">
            <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!activeConn || loadingTopics || loadingGroups" @click="loadKafka">
              <svg v-if="loadingTopics || loadingGroups" class="spin" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 12a9 9 0 1 1-6.219-8.56"/></svg>
              Refresh
            </button>
          </div>
        </section>

        <section v-if="!kafkaConnections.length" class="page-panel kafka-empty">
          <div class="kafka-empty__title">No Kafka connection available</div>
          <div class="kafka-empty__sub">Create a Kafka connection in Admin / Connections first.</div>
        </section>

        <template v-else>
          <section class="page-panel kafka-toolbar">
            <div class="kafka-conn">
              <span class="kafka-badge">KF</span>
              <div>
                <div class="kafka-conn__name">{{ activeConn?.name }}</div>
                <div class="kafka-conn__sub">{{ activeConn?.host }}{{ activeConn?.port ? `:${activeConn.port}` : '' }}</div>
              </div>
            </div>
            <select class="base-input kafka-select" :value="activeConn?.id" @change="selectConnection(Number(($event.target as HTMLSelectElement).value))">
              <option v-for="conn in kafkaConnections" :key="conn.id" :value="conn.id">{{ conn.name }}</option>
            </select>
          </section>

          <section class="kafka-metrics">
            <div class="page-panel kafka-metric">
              <span>Topics</span>
              <strong>{{ topics.length }}</strong>
            </div>
            <div class="page-panel kafka-metric">
              <span>Partitions</span>
              <strong>{{ totalPartitions }}</strong>
            </div>
            <div class="page-panel kafka-metric">
              <span>Avg Replication</span>
              <strong>{{ averageReplication }}</strong>
            </div>
            <div class="page-panel kafka-metric">
              <span>Consumer Groups</span>
              <strong>{{ groups.length }}</strong>
            </div>
          </section>

          <section class="page-panel kafka-workbench">
            <div class="kafka-tabs">
              <button class="kafka-tab" :class="{ active: activeTab === 'topics' }" @click="activeTab = 'topics'">Topics</button>
              <button class="kafka-tab" :class="{ active: activeTab === 'messages' }" @click="activeTab = 'messages'; loadMessages()">Messages</button>
              <button v-if="canProduce" class="kafka-tab" :class="{ active: activeTab === 'produce' }" @click="activeTab = 'produce'">Produce</button>
              <button class="kafka-tab" :class="{ active: activeTab === 'groups' }" @click="activeTab = 'groups'">Consumer Groups</button>
              <button v-if="canManage" class="kafka-tab" :class="{ active: activeTab === 'manage' }" @click="activeTab = 'manage'">Manage</button>
            </div>

            <div v-if="activeTab === 'topics'" class="kafka-split">
              <aside class="kafka-list">
                <div class="kafka-list__head">
                  <input v-model="topicFilter" class="base-input" placeholder="Filter topics" />
                </div>
                <div v-if="loadingTopics" class="kafka-muted">Loading topics...</div>
                <template v-else>
                  <button
                    v-for="topic in filteredTopics"
                    :key="topic.name"
                    class="kafka-topic"
                    :class="{ active: selectedTopic?.name === topic.name }"
                    @click="selectTopic(topic)"
                  >
                    <span>{{ topic.name }}</span>
                    <small>{{ topic.partitions }} partitions</small>
                  </button>
                </template>
                <div v-if="!loadingTopics && !filteredTopics.length" class="kafka-muted">No topics found.</div>
              </aside>

              <main class="kafka-detail">
                <template v-if="selectedTopic">
                  <div class="kafka-detail__head">
                    <div>
                      <div class="kafka-detail__title">{{ selectedTopic.name }}</div>
                      <div class="kafka-detail__sub">Topic metadata</div>
                    </div>
                  </div>
                  <div class="kafka-detail-grid">
                    <div><span>Partitions</span><strong>{{ selectedTopic.partitions }}</strong></div>
                    <div><span>Replication Factor</span><strong>{{ selectedTopic.replication_factor }}</strong></div>
                    <div><span>Partitions With Leader</span><strong>{{ selectedTopic.leader_count }}</strong></div>
                  </div>
                  <div v-if="selectedTopic.error" class="notice notice--error">{{ selectedTopic.error }}</div>
                </template>
                <div v-else class="kafka-empty-work">Select a topic to inspect metadata.</div>
              </main>
            </div>

            <div v-else-if="activeTab === 'messages'" class="kafka-pane">
              <div class="kafka-actionbar">
                <div>
                  <div class="kafka-detail__title">{{ selectedTopic?.name || 'No topic selected' }}</div>
                  <div class="kafka-detail__sub">Latest messages, read-only</div>
                </div>
                <div class="kafka-inline-controls">
                  <select v-model.number="messagePartition" class="base-input kafka-small-input">
                    <option :value="-1">All partitions</option>
                    <option v-for="n in selectedTopic?.partitions || 0" :key="n - 1" :value="n - 1">Partition {{ n - 1 }}</option>
                  </select>
                  <input v-model.number="messageLimit" class="base-input kafka-count-input" type="number" min="1" max="500" />
                  <button class="base-btn base-btn--primary base-btn--sm" :disabled="!selectedTopic || loadingMessages" @click="loadMessages">Load</button>
                </div>
              </div>
              <div v-if="loadingMessages" class="kafka-muted">Loading messages...</div>
              <div v-else class="kafka-message-list">
                <article v-for="message in messages" :key="`${message.partition}:${message.offset}`" class="kafka-message">
                  <div class="kafka-message__meta">
                    <span>p{{ message.partition }} / offset {{ message.offset }}</span>
                    <span>{{ new Date(message.timestamp).toLocaleString() }}</span>
                  </div>
                  <div class="kafka-message__kv"><span>Key</span><code>{{ message.key || '(empty)' }}</code></div>
                  <pre>{{ message.value }}</pre>
                  <div v-if="message.headers?.length" class="kafka-message__headers">
                    <span v-for="header in message.headers" :key="`${message.partition}:${message.offset}:${header.key}`">{{ header.key }}={{ header.value }}</span>
                  </div>
                </article>
                <div v-if="!messages.length" class="kafka-empty-cell">No messages loaded.</div>
              </div>
            </div>

            <div v-else-if="activeTab === 'produce'" class="kafka-pane">
              <div class="kafka-actionbar">
                <div>
                  <div class="kafka-detail__title">Produce Test Message</div>
                  <div class="kafka-detail__sub">{{ selectedTopic?.name || 'Select a topic first' }}</div>
                </div>
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
              <button class="base-btn base-btn--primary base-btn--sm" :disabled="!selectedTopic || producing" @click="produceMessage">Produce Message</button>
            </div>

            <div v-else-if="activeTab === 'groups'" class="kafka-table-wrap">
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
                  <tr v-for="group in groups" :key="group.group_id" class="kafka-click-row" @click="loadGroupDetail(group.group_id)">
                    <td>{{ group.group_id }}</td>
                    <td>{{ group.coordinator }}</td>
                    <td>{{ group.protocol_type || 'unknown' }}</td>
                  </tr>
                  <tr v-if="!groups.length">
                    <td colspan="3" class="kafka-empty-cell">No consumer groups found.</td>
                  </tr>
                </tbody>
              </table>
              <section v-if="selectedGroupId" class="kafka-group-detail">
                <div class="kafka-actionbar">
                  <div>
                    <div class="kafka-detail__title">{{ selectedGroupId }}</div>
                    <div class="kafka-detail__sub">State {{ groupDetail?.state || 'unknown' }} · total lag {{ groupDetail?.total_lag ?? 0 }}</div>
                  </div>
                  <button class="base-btn base-btn--ghost base-btn--sm" :disabled="loadingGroupDetail" @click="loadGroupDetail()">Refresh Detail</button>
                </div>
                <div v-if="loadingGroupDetail" class="kafka-muted">Loading group detail...</div>
                <template v-else-if="groupDetail">
                  <div class="kafka-detail-grid">
                    <div><span>Members</span><strong>{{ groupDetail.members?.length || 0 }}</strong></div>
                    <div><span>Tracked Partitions</span><strong>{{ groupDetail.offsets?.length || 0 }}</strong></div>
                    <div><span>Total Lag</span><strong>{{ groupDetail.total_lag }}</strong></div>
                  </div>
                  <table class="kafka-table kafka-subtable">
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
                </template>
              </section>
            </div>

            <div v-else class="kafka-pane">
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
                  <div class="kafka-detail__title">{{ selectedTopic?.name || 'Select a topic' }}</div>
                  <div class="kafka-detail__sub">Partitions can only be increased. Delete is irreversible.</div>
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
      </div>
    </div>
  </div>
</template>

<style scoped>
.kafka-page {
  background: var(--bg-body);
}

.kafka-empty,
.kafka-toolbar {
  padding: 18px;
}

.kafka-empty__title {
  font-size: 15px;
  font-weight: 700;
  color: var(--text-primary);
}

.kafka-empty__sub,
.kafka-muted {
  color: var(--text-muted);
  font-size: 13px;
  line-height: 1.5;
}

.kafka-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.kafka-conn {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}

.kafka-badge {
  width: 38px;
  height: 38px;
  border-radius: 8px;
  background: #231f20;
  color: #fff;
  display: grid;
  place-items: center;
  font-weight: 800;
  font-size: 12px;
}

.kafka-conn__name {
  color: var(--text-primary);
  font-weight: 700;
  font-size: 14px;
}

.kafka-conn__sub {
  color: var(--text-muted);
  font-size: 12px;
  margin-top: 2px;
}

.kafka-select {
  max-width: 280px;
}

.kafka-metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

.kafka-metric {
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.kafka-metric span {
  color: var(--text-muted);
  font-size: 12px;
}

.kafka-metric strong {
  color: var(--text-primary);
  font-size: 24px;
}

.kafka-workbench {
  overflow: hidden;
}

.kafka-tabs {
  display: flex;
  gap: 4px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border);
}

.kafka-tab {
  border: 1px solid transparent;
  background: transparent;
  color: var(--text-secondary);
  padding: 7px 11px;
  border-radius: 7px;
  cursor: pointer;
  font: inherit;
  font-size: 13px;
}

.kafka-tab.active {
  border-color: var(--border);
  background: var(--bg-elevated);
  color: var(--text-primary);
}

.kafka-split {
  display: grid;
  grid-template-columns: minmax(260px, 340px) 1fr;
  min-height: 480px;
}

.kafka-list {
  border-right: 1px solid var(--border);
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}

.kafka-list__head {
  margin-bottom: 4px;
}

.kafka-topic {
  border: 1px solid var(--border);
  background: var(--bg-surface);
  border-radius: 8px;
  color: var(--text-primary);
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 4px;
  padding: 10px 12px;
  cursor: pointer;
  min-width: 0;
}

.kafka-topic:hover,
.kafka-topic.active {
  border-color: var(--brand);
}

.kafka-topic span {
  font-weight: 600;
  font-size: 13px;
  overflow-wrap: anywhere;
}

.kafka-topic small {
  color: var(--text-muted);
  font-size: 11px;
}

.kafka-detail {
  padding: 18px;
  min-width: 0;
}

.kafka-detail__head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 16px;
}

.kafka-detail__title {
  color: var(--text-primary);
  font-size: 18px;
  font-weight: 800;
  overflow-wrap: anywhere;
}

.kafka-detail__sub {
  color: var(--text-muted);
  font-size: 12px;
  margin-top: 4px;
}

.kafka-detail-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.kafka-detail-grid div {
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 14px;
  background: var(--bg-surface);
}

.kafka-detail-grid span {
  display: block;
  color: var(--text-muted);
  font-size: 12px;
  margin-bottom: 6px;
}

.kafka-detail-grid strong {
  color: var(--text-primary);
  font-size: 20px;
}

.kafka-empty-work {
  color: var(--text-muted);
  font-size: 13px;
  padding: 28px 0;
}

.kafka-table-wrap {
  padding: 14px;
  overflow-x: auto;
}

.kafka-pane {
  padding: 18px;
  min-height: 480px;
}

.kafka-actionbar {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 14px;
  margin-bottom: 16px;
}

.kafka-inline-controls {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.kafka-small-input {
  width: 160px;
}

.kafka-count-input {
  width: 92px;
}

.kafka-message-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.kafka-message {
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-surface);
  padding: 12px;
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
.kafka-message pre {
  color: var(--text-primary);
  font-family: var(--font-mono);
}

.kafka-message pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-elevated);
  padding: 10px;
  margin: 10px 0 0;
  font-size: 12px;
  max-height: 260px;
  overflow: auto;
}

.kafka-message__headers {
  margin-top: 10px;
}

.kafka-message__headers span {
  border: 1px solid var(--border);
  border-radius: 999px;
  padding: 3px 8px;
  background: var(--bg-elevated);
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
  min-height: 86px;
  resize: vertical;
  font-family: var(--font-mono);
  font-size: 12px;
}

.kafka-textarea--value {
  min-height: 180px;
}

.kafka-click-row {
  cursor: pointer;
}

.kafka-click-row:hover {
  background: var(--bg-elevated);
}

.kafka-group-detail,
.kafka-manage-danger {
  border-top: 1px solid var(--border);
  margin-top: 18px;
  padding-top: 18px;
}

.kafka-subtable {
  margin-top: 14px;
}

.kafka-manage-danger {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
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
  text-align: left;
}

.kafka-table th {
  color: var(--text-muted);
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.08em;
}

.kafka-table td {
  color: var(--text-primary);
}

.kafka-empty-cell {
  color: var(--text-muted) !important;
  text-align: center !important;
  padding: 28px 12px !important;
}

@media (max-width: 900px) {
  .kafka-metrics,
  .kafka-detail-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .kafka-split {
    grid-template-columns: 1fr;
  }

  .kafka-list {
    border-right: 0;
    border-bottom: 1px solid var(--border);
  }
}

@media (max-width: 640px) {
  .kafka-toolbar {
    align-items: stretch;
    flex-direction: column;
  }

  .kafka-actionbar,
  .kafka-manage-danger {
    flex-direction: column;
  }

  .kafka-select {
    max-width: none;
  }

  .kafka-metrics,
  .kafka-detail-grid,
  .kafka-form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
