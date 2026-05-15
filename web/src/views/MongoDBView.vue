<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useConnections } from '@/composables/useConnections'
import { useMongoDB, type MongoCollectionStat, type MongoDashboardData, type MongoDatabaseSummary, type MongoHealthData, type MongoIndexRecommendation, type MongoIndexSummary, type MongoQueryEntry, type MongoSchemaAnalysis } from '@/composables/useMongoDB'
import { useToast } from '@/composables/useToast'
import { readableError } from '@/utils/httpError'

const props = defineProps<{ activeConnId?: number | null }>()
const emit = defineEmits<{ (e: 'set-conn', id: number): void }>()

const { connections, fetchConnections } = useConnections()
const mongo = useMongoDB()
const toast = useToast()

const loading = ref(false)
const loadingDocs = ref(false)
const error = ref('')
const dashboard = ref<MongoDashboardData | null>(null)
const databases = ref<MongoDatabaseSummary[]>([])
const collections = ref<MongoCollectionStat[]>([])
const selectedDb = ref('')
const selectedCollection = ref('')
const collectionSearch = ref('')
const filterText = ref('')
const sortText = ref('')
const projectionText = ref('')
const docs = ref<any[]>([])
const docLimit = ref(50)
const docPage = ref(1)
const docCount = ref(0)
const docHasNext = ref(false)
const docViewMode = ref<'json' | 'table'>('json')
const activeTab = ref<'documents' | 'bulk' | 'aggregation' | 'schema' | 'indexes' | 'explain' | 'health' | 'saved'>('documents')
const docEditorOpen = ref(false)
const docEditorMode = ref<'insert' | 'edit'>('insert')
const docEditorText = ref('{\n  \n}')
const docEditorFilter = ref('{}')
const originalDocText = ref('')
const savingDoc = ref(false)
const indexes = ref<MongoIndexSummary[]>([])
const indexRecommendations = ref<MongoIndexRecommendation[]>([])
const indexKeysText = ref('{\n  "field": 1\n}')
const indexName = ref('')
const indexUnique = ref(false)
const bulkMode = ref<'updateOne' | 'updateMany' | 'deleteMany'>('updateMany')
const bulkFilterText = ref('{}')
const bulkUpdateText = ref('{\n  "$set": {\n    \n  }\n}')
const bulkPreview = ref<any[]>([])
const bulkPreviewCount = ref(0)
const aggregationText = ref('[\n  { "$limit": 10 }\n]')
const aggregationResults = ref<any[]>([])
const explainResult = ref<any | null>(null)
const schemaAnalysis = ref<MongoSchemaAnalysis | null>(null)
const health = ref<MongoHealthData | null>(null)
const savedQueries = ref<MongoQueryEntry[]>([])
const saveQueryName = ref('')
const schemaLoading = ref(false)
const schemaLimit = ref(100)
const collectionModalOpen = ref(false)
const collectionMode = ref<'create' | 'rename'>('create')
const collectionName = ref('')
const collectionNewName = ref('')
const importModalOpen = ref(false)
const importText = ref('[\n  {\n    \n  }\n]')
const importing = ref(false)
const exporting = ref(false)
const exportFormat = ref<'json' | 'ndjson' | 'csv'>('json')

const mongoConnections = computed(() => connections.value.filter(c => c.driver === 'mongodb'))
const activeConn = computed(() => connections.value.find(c => c.id === props.activeConnId) ?? null)
const isMongo = computed(() => activeConn.value?.driver === 'mongodb')
const selectedCollectionInfo = computed(() => collections.value.find(c => c.name === selectedCollection.value) ?? null)
const filteredCollections = computed(() => {
  const q = collectionSearch.value.trim().toLowerCase()
  if (!q) return collections.value
  return collections.value.filter(c => c.name.toLowerCase().includes(q))
})
const tableColumns = computed(() => {
  const set = new Set<string>()
  docs.value.slice(0, 100).forEach(doc => Object.keys(flattenDoc(doc)).forEach(key => set.add(key)))
  const cols = Array.from(set).sort()
  const idIdx = cols.indexOf('_id')
  if (idIdx > 0) {
    cols.splice(idIdx, 1)
    cols.unshift('_id')
  }
  return cols.slice(0, 24)
})
const docStart = computed(() => docs.value.length ? ((docPage.value - 1) * docLimit.value) + 1 : 0)
const docEnd = computed(() => ((docPage.value - 1) * docLimit.value) + docs.value.length)
const filterError = computed(() => jsonValidationError(filterText.value, true))
const sortError = computed(() => jsonValidationError(sortText.value, true))
const projectionError = computed(() => jsonValidationError(projectionText.value, true))
const bulkFilterError = computed(() => jsonValidationError(bulkFilterText.value, false))
const bulkUpdateError = computed(() => bulkMode.value === 'deleteMany' ? '' : jsonValidationError(bulkUpdateText.value, false))
const queryHasError = computed(() => !!filterError.value || !!sortError.value || !!projectionError.value)
const bulkHasError = computed(() => !!bulkFilterError.value || !!bulkUpdateError.value)
const bulkCanRun = computed(() => !bulkHasError.value && bulkPreviewCount.value > 0 && bulkPreview.value.length > 0)
const schemaTopFields = computed(() => schemaAnalysis.value?.fields.slice(0, 12) ?? [])

function formatBytes(bytes: number): string {
  if (!bytes) return '-'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
}

function compactDoc(doc: any) {
  return JSON.stringify(doc, null, 2)
}

function compactInline(value: any) {
  return JSON.stringify(value)
}

function jsonValidationError(value: string, allowEmpty: boolean) {
  const text = value.trim()
  if (!text) return allowEmpty ? '' : 'Required JSON'
  try {
    JSON.parse(text)
    return ''
  } catch (e) {
    return e instanceof Error ? e.message : 'Invalid JSON'
  }
}

function parseObjectText(value: string) {
  const text = value.trim()
  if (!text) return {}
  const parsed = JSON.parse(text)
  return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {}
}

function flattenDoc(doc: any, prefix = '', out: Record<string, string> = {}) {
  if (doc && typeof doc === 'object' && !Array.isArray(doc)) {
    const keys = Object.keys(doc)
    if (keys.length === 1 && ('$oid' in doc || '$date' in doc || '$numberLong' in doc)) {
      out[prefix] = String(doc.$oid ?? doc.$date ?? doc.$numberLong ?? '')
      return out
    }
    for (const key of keys) {
      const path = prefix ? `${prefix}.${key}` : key
      flattenDoc(doc[key], path, out)
    }
    return out
  }
  out[prefix] = Array.isArray(doc) ? JSON.stringify(doc) : String(doc ?? '')
  return out
}

function tableCell(doc: any, key: string) {
  return flattenDoc(doc)[key] ?? ''
}

function docIdentityFilter(doc: any) {
  return JSON.stringify({ _id: doc?._id }, null, 2)
}

function serverMetric(path: string, fallback = '-') {
  const root = dashboard.value?.server ?? {}
  const value = path.split('.').reduce<any>((acc, key) => acc?.[key], root)
  return value == null ? fallback : value
}

async function selectMongoConnection(rawId: string | number) {
  const id = Number(rawId)
  if (!id) return
  emit('set-conn', id)
}

async function loadAll() {
  if (!activeConn.value || !isMongo.value) return
  loading.value = true
  error.value = ''
  docs.value = []
  try {
    const [dash, dbs] = await Promise.all([
      mongo.dashboard(activeConn.value.id),
      mongo.databases(activeConn.value.id),
    ])
    dashboard.value = dash
    databases.value = dbs
    selectedDb.value = selectedDb.value || dash.database || dbs[0]?.name || 'admin'
    await loadCollections()
    await loadHealth()
    await loadSavedQueries()
  } catch (e) {
    error.value = readableError(e, { action: 'Load MongoDB dashboard', fallback: 'Failed to load MongoDB dashboard' })
    toast.error(error.value)
  } finally {
    loading.value = false
  }
}

async function loadCollections() {
  if (!activeConn.value || !selectedDb.value) return
  const preferred = selectedCollection.value
  collections.value = await mongo.collections(activeConn.value.id, selectedDb.value)
  selectedCollection.value = collections.value.some(c => c.name === preferred) ? preferred : (collections.value[0]?.name ?? '')
  indexes.value = []
  explainResult.value = null
  aggregationResults.value = []
}

async function loadDocuments() {
  if (!activeConn.value || !selectedDb.value || !selectedCollection.value) return
  loadingDocs.value = true
  try {
    const result = await mongo.documents(activeConn.value.id, selectedDb.value, selectedCollection.value, filterText.value, docLimit.value, sortText.value, projectionText.value, docPage.value)
    docs.value = result.documents
    docCount.value = result.count
    docHasNext.value = result.has_next
  } catch (e) {
    toast.error(readableError(e, { action: 'Load MongoDB documents', fallback: 'Failed to load MongoDB documents' }))
  } finally {
    loadingDocs.value = false
  }
}

async function nextPage() {
  if (!docHasNext.value) return
  docPage.value += 1
  await loadDocuments()
}

async function prevPage() {
  if (docPage.value <= 1) return
  docPage.value -= 1
  await loadDocuments()
}

async function resetAndLoadDocuments() {
  if (queryHasError.value) {
    toast.error('Fix the filter, sort, or projection JSON before loading documents')
    return
  }
  docPage.value = 1
  await loadDocuments()
}

function clearQuery() {
  filterText.value = ''
  sortText.value = ''
  projectionText.value = ''
  docPage.value = 1
}

async function applyQueryAndLoad() {
  await resetAndLoadDocuments()
}

function addFieldFilter(path: string, rawExample?: string) {
  try {
    const filter = parseObjectText(filterText.value)
    let value: any = ''
    if (rawExample) {
      try {
        value = JSON.parse(rawExample)
      } catch {
        value = rawExample.replace(/^"|"$/g, '')
      }
    }
    filter[path] = value
    filterText.value = JSON.stringify(filter, null, 2)
    activeTab.value = 'documents'
  } catch {
    filterText.value = JSON.stringify({ [path]: '' }, null, 2)
    activeTab.value = 'documents'
  }
}

function addProjectionField(path: string) {
  try {
    const projection = parseObjectText(projectionText.value)
    projection[path] = 1
    projectionText.value = JSON.stringify(projection, null, 2)
    activeTab.value = 'documents'
  } catch {
    projectionText.value = JSON.stringify({ [path]: 1 }, null, 2)
    activeTab.value = 'documents'
  }
}

function sortByField(path: string, direction = 1) {
  sortText.value = JSON.stringify({ [path]: direction }, null, 2)
  activeTab.value = 'documents'
}

function openInsertDocument() {
  docEditorMode.value = 'insert'
  docEditorFilter.value = '{}'
  docEditorText.value = '{\n  \n}'
  docEditorOpen.value = true
}

function openEditDocument(doc: any) {
  docEditorMode.value = 'edit'
  docEditorFilter.value = docIdentityFilter(doc)
  docEditorText.value = compactDoc(doc)
  originalDocText.value = compactDoc(doc)
  docEditorOpen.value = true
}

async function saveDocument() {
  if (!activeConn.value || !selectedDb.value || !selectedCollection.value) return
  savingDoc.value = true
  try {
    if (docEditorMode.value === 'insert') {
      await mongo.insertDocument(activeConn.value.id, selectedDb.value, selectedCollection.value, docEditorText.value)
      toast.success('MongoDB document inserted')
    } else {
      await mongo.replaceDocument(activeConn.value.id, selectedDb.value, selectedCollection.value, docEditorFilter.value, docEditorText.value)
      toast.success('MongoDB document updated')
    }
    docEditorOpen.value = false
    await loadDocuments()
    await loadCollections()
  } catch (e) {
    toast.error(readableError(e, { action: docEditorMode.value === 'insert' ? 'Insert MongoDB document' : 'Update MongoDB document', fallback: 'Failed to save MongoDB document' }))
  } finally {
    savingDoc.value = false
  }
}

async function removeDocument(doc: any) {
  if (!activeConn.value || !selectedDb.value || !selectedCollection.value) return
  if (!window.confirm('Delete this MongoDB document?')) return
  try {
    await mongo.deleteDocument(activeConn.value.id, selectedDb.value, selectedCollection.value, docIdentityFilter(doc))
    toast.success('MongoDB document deleted')
    await loadDocuments()
    await loadCollections()
  } catch (e) {
    toast.error(readableError(e, { action: 'Delete MongoDB document', fallback: 'Failed to delete MongoDB document' }))
  }
}

async function previewBulk() {
  if (!activeConn.value || !selectedDb.value || !selectedCollection.value) return
  if (bulkHasError.value) {
    toast.error('Fix the bulk operation JSON before previewing')
    return
  }
  try {
    const result = bulkMode.value === 'deleteMany'
      ? await mongo.deleteDocument(activeConn.value.id, selectedDb.value, selectedCollection.value, bulkFilterText.value, 'deleteMany', true)
      : await mongo.updateWithOperators(activeConn.value.id, selectedDb.value, selectedCollection.value, bulkFilterText.value, bulkUpdateText.value, bulkMode.value, true)
    bulkPreview.value = result.documents ?? []
    bulkPreviewCount.value = result.count ?? bulkPreview.value.length
  } catch (e) {
    toast.error(readableError(e, { action: 'Preview MongoDB bulk operation', fallback: 'Failed to preview MongoDB bulk operation' }))
  }
}

async function runBulk() {
  if (!activeConn.value || !selectedDb.value || !selectedCollection.value) return
  if (!bulkCanRun.value) {
    toast.error('Preview the matching documents before running this operation')
    return
  }
  const action = bulkMode.value === 'deleteMany' ? 'delete matching documents' : 'update matching documents'
  if (!window.confirm(`Run bulk operation to ${action}? ${bulkPreviewCount.value || 'Matching'} document(s) may be affected.`)) return
  try {
    const result = bulkMode.value === 'deleteMany'
      ? await mongo.deleteDocument(activeConn.value.id, selectedDb.value, selectedCollection.value, bulkFilterText.value, 'deleteMany')
      : await mongo.updateWithOperators(activeConn.value.id, selectedDb.value, selectedCollection.value, bulkFilterText.value, bulkUpdateText.value, bulkMode.value)
    toast.success(bulkMode.value === 'deleteMany' ? `Deleted ${result.deleted ?? 0} document(s)` : `Updated ${result.modified ?? 0} document(s)`)
    bulkPreview.value = []
    await loadDocuments()
    await loadCollections()
  } catch (e) {
    toast.error(readableError(e, { action: 'Run MongoDB bulk operation', fallback: 'Failed to run MongoDB bulk operation' }))
  }
}

async function loadIndexes() {
  if (!activeConn.value || !selectedDb.value || !selectedCollection.value) return
  try {
    indexes.value = await mongo.indexes(activeConn.value.id, selectedDb.value, selectedCollection.value)
    indexRecommendations.value = await mongo.recommendIndexes(activeConn.value.id, selectedDb.value, selectedCollection.value, filterText.value, sortText.value)
  } catch (e) {
    toast.error(readableError(e, { action: 'Load MongoDB indexes', fallback: 'Failed to load MongoDB indexes' }))
  }
}

function useRecommendation(rec: MongoIndexRecommendation) {
  indexKeysText.value = JSON.stringify({ [rec.field]: rec.direction || 1 }, null, 2)
  indexName.value = `${rec.field.replace(/\W+/g, '_')}_${rec.direction === -1 ? 'desc' : 'asc'}`
}

async function createIndex() {
  if (!activeConn.value || !selectedDb.value || !selectedCollection.value) return
  try {
    await mongo.createIndex(activeConn.value.id, selectedDb.value, selectedCollection.value, indexKeysText.value, indexName.value, indexUnique.value)
    toast.success('MongoDB index created')
    indexName.value = ''
    await loadIndexes()
    await loadCollections()
  } catch (e) {
    toast.error(readableError(e, { action: 'Create MongoDB index', fallback: 'Failed to create MongoDB index' }))
  }
}

async function dropIndex(name: string) {
  if (!activeConn.value || !selectedDb.value || !selectedCollection.value) return
  if (!window.confirm(`Drop index "${name}"?`)) return
  try {
    await mongo.dropIndex(activeConn.value.id, selectedDb.value, selectedCollection.value, name)
    toast.success('MongoDB index dropped')
    await loadIndexes()
    await loadCollections()
  } catch (e) {
    toast.error(readableError(e, { action: 'Drop MongoDB index', fallback: 'Failed to drop MongoDB index' }))
  }
}

async function runAggregation() {
  if (!activeConn.value || !selectedDb.value || !selectedCollection.value) return
  try {
    const result = await mongo.aggregate(activeConn.value.id, selectedDb.value, selectedCollection.value, aggregationText.value, docLimit.value)
    aggregationResults.value = result.documents
  } catch (e) {
    toast.error(readableError(e, { action: 'Run MongoDB aggregation', fallback: 'Failed to run MongoDB aggregation' }))
  }
}

function setPipelinePreset(kind: 'match-limit' | 'group-count' | 'sort-limit') {
  if (kind === 'group-count') {
    aggregationText.value = '[\n  { "$group": { "_id": null, "count": { "$sum": 1 } } },\n  { "$sort": { "count": -1 } }\n]'
  } else if (kind === 'sort-limit') {
    aggregationText.value = '[\n  { "$sort": { "_id": -1 } },\n  { "$limit": 20 }\n]'
  } else {
    aggregationText.value = '[\n  { "$match": {} },\n  { "$limit": 20 }\n]'
  }
}

async function runExplain() {
  if (!activeConn.value || !selectedDb.value || !selectedCollection.value) return
  try {
    explainResult.value = await mongo.explain(activeConn.value.id, selectedDb.value, selectedCollection.value, filterText.value)
  } catch (e) {
    toast.error(readableError(e, { action: 'Explain MongoDB query', fallback: 'Failed to explain MongoDB query' }))
  }
}

async function loadHealth() {
  if (!activeConn.value) return
  try {
    health.value = await mongo.health(activeConn.value.id)
  } catch {
    health.value = null
  }
}

async function loadSavedQueries() {
  if (!activeConn.value) return
  try {
    savedQueries.value = await mongo.savedQueries(activeConn.value.id)
  } catch {
    savedQueries.value = []
  }
}

async function saveCurrentQuery() {
  if (!activeConn.value || !saveQueryName.value.trim()) return
  try {
    await mongo.saveQuery(activeConn.value.id, {
      name: saveQueryName.value.trim(),
      database: selectedDb.value,
      collection: selectedCollection.value,
      filter: filterText.value ? JSON.parse(filterText.value) : {},
      sort: sortText.value ? JSON.parse(sortText.value) : {},
      projection: projectionText.value ? JSON.parse(projectionText.value) : {},
      pipeline: aggregationText.value ? JSON.parse(aggregationText.value) : [],
    })
    saveQueryName.value = ''
    await loadSavedQueries()
    toast.success('MongoDB query saved')
  } catch (e) {
    toast.error(readableError(e, { action: 'Save MongoDB query', fallback: 'Failed to save MongoDB query' }))
  }
}

async function applySavedQuery(item: MongoQueryEntry) {
  try {
    const payload = JSON.parse(item.payload)
    selectedDb.value = payload.database || selectedDb.value
    selectedCollection.value = payload.collection || selectedCollection.value
    filterText.value = JSON.stringify(payload.filter || {}, null, 2)
    sortText.value = Object.keys(payload.sort || {}).length ? JSON.stringify(payload.sort, null, 2) : ''
    projectionText.value = Object.keys(payload.projection || {}).length ? JSON.stringify(payload.projection, null, 2) : ''
    if (payload.pipeline) aggregationText.value = JSON.stringify(payload.pipeline, null, 2)
    activeTab.value = 'documents'
    await resetAndLoadDocuments()
  } catch {
    toast.error('Saved MongoDB query is invalid')
  }
}

async function deleteSavedQuery(id: number) {
  if (!activeConn.value) return
  await mongo.deleteSavedQuery(activeConn.value.id, id)
  await loadSavedQueries()
}

async function analyzeSchema() {
  if (!activeConn.value || !selectedDb.value || !selectedCollection.value) return
  schemaLoading.value = true
  try {
    schemaAnalysis.value = await mongo.schema(activeConn.value.id, selectedDb.value, selectedCollection.value, filterText.value, schemaLimit.value)
  } catch (e) {
    toast.error(readableError(e, { action: 'Analyze MongoDB schema', fallback: 'Failed to analyze MongoDB schema' }))
  } finally {
    schemaLoading.value = false
  }
}

function openCreateCollection() {
  collectionMode.value = 'create'
  collectionName.value = ''
  collectionNewName.value = ''
  collectionModalOpen.value = true
}

function openRenameCollection() {
  if (!selectedCollection.value) return
  collectionMode.value = 'rename'
  collectionName.value = selectedCollection.value
  collectionNewName.value = selectedCollection.value
  collectionModalOpen.value = true
}

async function saveCollection() {
  if (!activeConn.value || !selectedDb.value) return
  try {
    if (collectionMode.value === 'create') {
      await mongo.createCollection(activeConn.value.id, selectedDb.value, collectionName.value.trim())
      selectedCollection.value = collectionName.value.trim()
      toast.success('MongoDB collection created')
    } else {
      await mongo.renameCollection(activeConn.value.id, selectedDb.value, collectionName.value, collectionNewName.value.trim())
      selectedCollection.value = collectionNewName.value.trim()
      toast.success('MongoDB collection renamed')
    }
    collectionModalOpen.value = false
    await loadCollections()
  } catch (e) {
    toast.error(readableError(e, { action: collectionMode.value === 'create' ? 'Create MongoDB collection' : 'Rename MongoDB collection', fallback: 'Failed to save MongoDB collection' }))
  }
}

async function dropSelectedCollection() {
  if (!activeConn.value || !selectedDb.value || !selectedCollection.value) return
  if (!window.confirm(`Drop collection "${selectedCollection.value}"? This deletes all documents in it.`)) return
  try {
    await mongo.dropCollection(activeConn.value.id, selectedDb.value, selectedCollection.value)
    toast.success('MongoDB collection dropped')
    selectedCollection.value = ''
    docs.value = []
    await loadCollections()
  } catch (e) {
    toast.error(readableError(e, { action: 'Drop MongoDB collection', fallback: 'Failed to drop MongoDB collection' }))
  }
}

async function importDocuments() {
  if (!activeConn.value || !selectedDb.value || !selectedCollection.value) return
  importing.value = true
  try {
    const result = await mongo.importJson(activeConn.value.id, selectedDb.value, selectedCollection.value, importText.value)
    toast.success(`Imported ${result.inserted ?? 0} document(s)`)
    importModalOpen.value = false
    await loadDocuments()
    await loadCollections()
  } catch (e) {
    toast.error(readableError(e, { action: 'Import MongoDB JSON', fallback: 'Failed to import MongoDB JSON' }))
  } finally {
    importing.value = false
  }
}

async function exportDocuments() {
  if (!activeConn.value || !selectedDb.value || !selectedCollection.value) return
  if (filterError.value) {
    toast.error('Fix the filter JSON before exporting')
    return
  }
  exporting.value = true
  try {
    const format = exportFormat.value
    const data = await mongo.exportData(activeConn.value.id, selectedDb.value, selectedCollection.value, filterText.value, 1000, format)
    const blob = new Blob([format === 'json' ? JSON.stringify(data, null, 2) : String(data)], { type: format === 'csv' ? 'text/csv' : 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${selectedDb.value}.${selectedCollection.value}.${format}`
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
    toast.success('MongoDB export completed')
  } catch (e) {
    toast.error(readableError(e, { action: 'Export MongoDB JSON', fallback: 'Failed to export MongoDB JSON' }))
  } finally {
    exporting.value = false
  }
}

watch(() => props.activeConnId, () => {
  dashboard.value = null
  collections.value = []
  docs.value = []
  docPage.value = 1
  if (isMongo.value) void loadAll()
})

watch(selectedDb, async (next, prev) => {
  if (next && prev && next !== prev) {
    docs.value = []
    await loadCollections()
  }
})

watch(selectedCollection, () => {
  docs.value = []
  indexes.value = []
  aggregationResults.value = []
  explainResult.value = null
  schemaAnalysis.value = null
  if (activeTab.value === 'indexes') void loadIndexes()
})

watch(activeTab, (tab) => {
  if (tab === 'indexes') void loadIndexes()
  if (tab === 'documents' && !docs.value.length) void loadDocuments()
  if (tab === 'schema' && !schemaAnalysis.value) void analyzeSchema()
  if (tab === 'health') void loadHealth()
  if (tab === 'saved') void loadSavedQueries()
})

onMounted(async () => {
  await fetchConnections()
  if (!isMongo.value && mongoConnections.value.length === 1) {
    emit('set-conn', mongoConnections.value[0].id)
    return
  }
  if (isMongo.value) await loadAll()
})
</script>

<template>
  <div class="mongo-page">
    <section v-if="!isMongo" class="page-panel mongo-empty">
      <div class="mongo-empty__title">Select a MongoDB connection</div>
      <div v-if="mongoConnections.length" class="mongo-picker">
        <label class="form-label">MongoDB Connection</label>
        <select class="base-input" @change="selectMongoConnection(($event.target as HTMLSelectElement).value)">
          <option value="">Select MongoDB connection</option>
          <option v-for="conn in mongoConnections" :key="conn.id" :value="conn.id">{{ conn.name }}</option>
        </select>
      </div>
      <div v-else class="mongo-muted">Create a MongoDB connection in Admin / Connections first.</div>
    </section>

    <template v-else>
      <header class="mongo-head">
        <div>
          <div class="mongo-kicker">MongoDB Management</div>
          <h1>{{ activeConn?.name }}</h1>
          <p>{{ activeConn?.host }}{{ activeConn?.port ? `:${activeConn.port}` : '' }}</p>
        </div>
        <div class="mongo-actions">
          <select v-model="selectedDb" class="base-input mongo-db-select">
            <option v-for="db in databases" :key="db.name" :value="db.name">{{ db.name }}</option>
          </select>
          <button class="base-btn base-btn--ghost base-btn--sm" :disabled="loading" @click="loadAll">Refresh</button>
        </div>
      </header>

      <div v-if="error" class="mongo-error">{{ error }}</div>

      <section class="mongo-grid">
        <div class="page-panel mongo-card">
          <span>Database Size</span>
          <strong>{{ formatBytes(dashboard?.size_bytes ?? 0) }}</strong>
        </div>
        <div class="page-panel mongo-card">
          <span>Collections</span>
          <strong>{{ dashboard?.collections?.toLocaleString() ?? '-' }}</strong>
        </div>
        <div class="page-panel mongo-card">
          <span>Objects</span>
          <strong>{{ dashboard?.objects?.toLocaleString() ?? '-' }}</strong>
        </div>
        <div class="page-panel mongo-card">
          <span>Indexes</span>
          <strong>{{ dashboard?.indexes?.toLocaleString() ?? '-' }}</strong>
        </div>
        <div class="page-panel mongo-card">
          <span>Connections</span>
          <strong>{{ serverMetric('connections.current') }}</strong>
        </div>
        <div class="page-panel mongo-card">
          <span>Uptime</span>
          <strong>{{ serverMetric('uptime') }}s</strong>
        </div>
      </section>

      <main class="mongo-layout">
        <aside class="page-panel mongo-side">
          <div class="mongo-side-head">
            <div class="mongo-panel-title">Collections</div>
            <button class="base-btn base-btn--primary base-btn--sm" @click="openCreateCollection">New</button>
          </div>
          <input v-model="collectionSearch" class="base-input mongo-side-search" placeholder="Search collections" />
          <div class="mongo-list">
            <button
              v-for="col in filteredCollections"
              :key="col.name"
              class="mongo-list-row"
              :class="{ active: selectedCollection === col.name }"
              @click="selectedCollection = col.name; docs = []"
            >
              <span>{{ col.name }}</span>
              <small>{{ col.count.toLocaleString() }} docs · {{ formatBytes(col.storage_size) }}</small>
            </button>
          </div>
        </aside>

        <section class="page-panel mongo-main">
          <div class="mongo-panel-head">
            <div>
              <div class="mongo-panel-title">{{ selectedCollection || 'No collection selected' }}</div>
              <div v-if="selectedCollectionInfo" class="mongo-muted">
                {{ selectedCollectionInfo.count.toLocaleString() }} docs · {{ selectedCollectionInfo.indexes }} indexes · {{ formatBytes(selectedCollectionInfo.index_size) }} index size
              </div>
            </div>
            <div class="mongo-main-actions">
              <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!selectedCollection || loadingDocs" @click="loadDocuments">
                {{ loadingDocs ? 'Loading...' : 'Refresh' }}
              </button>
              <button class="base-btn base-btn--primary base-btn--sm" :disabled="!selectedCollection" @click="openInsertDocument">
                Add Document
              </button>
              <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!selectedCollection" @click="openRenameCollection">Rename</button>
              <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!selectedCollection" @click="dropSelectedCollection">Drop</button>
            </div>
          </div>

          <nav class="mongo-tabs">
            <button :class="{ active: activeTab === 'documents' }" @click="activeTab = 'documents'">Documents</button>
            <button :class="{ active: activeTab === 'bulk' }" @click="activeTab = 'bulk'">Bulk Ops</button>
            <button :class="{ active: activeTab === 'aggregation' }" @click="activeTab = 'aggregation'">Aggregations</button>
            <button :class="{ active: activeTab === 'schema' }" @click="activeTab = 'schema'">Schema</button>
            <button :class="{ active: activeTab === 'indexes' }" @click="activeTab = 'indexes'">Indexes</button>
            <button :class="{ active: activeTab === 'explain' }" @click="activeTab = 'explain'">Explain</button>
            <button :class="{ active: activeTab === 'health' }" @click="activeTab = 'health'">Health</button>
            <button :class="{ active: activeTab === 'saved' }" @click="activeTab = 'saved'">Saved</button>
          </nav>

          <div v-if="activeTab === 'documents'" class="mongo-tabbody">
            <div class="mongo-filter-row mongo-filter-row--advanced">
              <input v-model="filterText" class="base-input" :class="{ 'is-invalid': filterError }" placeholder='Filter JSON, e.g. {"status":"active"}' @keydown.enter="resetAndLoadDocuments" />
              <input v-model="sortText" class="base-input" :class="{ 'is-invalid': sortError }" placeholder='Sort, e.g. {"created_at":-1}' />
              <input v-model="projectionText" class="base-input" :class="{ 'is-invalid': projectionError }" placeholder='Projection, e.g. {"name":1}' />
              <input v-model.number="docLimit" class="base-input mongo-limit" type="number" min="1" max="200" />
            </div>
            <div v-if="queryHasError" class="mongo-validation">
              {{ filterError || sortError || projectionError }}
            </div>
            <div class="mongo-toolbar">
              <div class="mongo-segment">
                <button :class="{ active: docViewMode === 'json' }" @click="docViewMode = 'json'">JSON</button>
                <button :class="{ active: docViewMode === 'table' }" @click="docViewMode = 'table'">Table</button>
              </div>
              <button class="base-btn base-btn--primary base-btn--sm" :disabled="!selectedCollection || loadingDocs || queryHasError" @click="applyQueryAndLoad">
                {{ loadingDocs ? 'Loading...' : 'Run Query' }}
              </button>
              <button class="base-btn base-btn--ghost base-btn--sm" @click="clearQuery">Clear</button>
              <button class="base-btn base-btn--ghost base-btn--sm" @click="activeTab = 'saved'">Saved</button>
              <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!selectedCollection" @click="importModalOpen = true">Import</button>
              <select v-model="exportFormat" class="base-input mongo-export-select">
                <option value="json">JSON</option>
                <option value="ndjson">NDJSON</option>
                <option value="csv">CSV</option>
              </select>
              <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!selectedCollection || exporting" @click="exportDocuments">{{ exporting ? 'Exporting...' : 'Export' }}</button>
            </div>
            <div class="mongo-page-tools">
              <span>{{ docStart.toLocaleString() }}-{{ docEnd.toLocaleString() }} of {{ docCount.toLocaleString() }}</span>
              <button class="base-btn base-btn--ghost base-btn--sm" :disabled="docPage <= 1 || loadingDocs" @click="prevPage">Prev</button>
              <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!docHasNext || loadingDocs" @click="nextPage">Next</button>
            </div>

            <div v-if="docs.length === 0" class="mongo-doc-empty">Documents will appear here after loading the selected collection.</div>
            <div v-else-if="docViewMode === 'table'" class="mongo-table-wrap">
              <table class="mongo-table">
                <thead>
                  <tr>
                    <th v-for="col in tableColumns" :key="col">{{ col }}</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(doc, idx) in docs" :key="idx">
                    <td v-for="col in tableColumns" :key="col"><code>{{ tableCell(doc, col) }}</code></td>
                    <td class="mongo-row-actions">
                      <button class="base-btn base-btn--ghost base-btn--sm" @click="openEditDocument(doc)">Edit</button>
                      <button class="base-btn base-btn--ghost base-btn--sm" @click="addFieldFilter('_id', compactInline(doc._id)); resetAndLoadDocuments()">Filter</button>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div v-else class="mongo-docs">
              <article v-for="(doc, idx) in docs" :key="idx" class="mongo-doc-card">
                <div class="mongo-doc-card__head">
                  <code>{{ compactInline(doc._id) }}</code>
                  <div>
                    <button class="base-btn base-btn--ghost base-btn--sm" @click="openEditDocument(doc)">Edit</button>
                    <button class="base-btn base-btn--ghost base-btn--sm" @click="removeDocument(doc)">Delete</button>
                  </div>
                </div>
                <pre>{{ compactDoc(doc) }}</pre>
              </article>
            </div>
          </div>

          <div v-else-if="activeTab === 'bulk'" class="mongo-tabbody">
            <div class="mongo-action-strip" :class="{ danger: bulkMode === 'deleteMany' }">
              <strong>{{ bulkMode === 'deleteMany' ? 'Delete many requires preview first' : 'Bulk changes require preview first' }}</strong>
              <span>{{ bulkPreviewCount.toLocaleString() }} matching document(s) from the last preview.</span>
            </div>
            <div class="mongo-index-create">
              <select v-model="bulkMode" class="base-input">
                <option value="updateOne">Update one</option>
                <option value="updateMany">Update many</option>
                <option value="deleteMany">Delete many</option>
              </select>
              <button class="base-btn base-btn--ghost base-btn--sm" :disabled="!selectedCollection || bulkHasError" @click="previewBulk">Preview</button>
              <button class="base-btn base-btn--primary base-btn--sm" :disabled="!selectedCollection || !bulkCanRun" @click="runBulk">Run Previewed Operation</button>
            </div>
            <div class="mongo-editor-grid">
              <div>
                <label class="form-label">Filter</label>
                <textarea v-model="bulkFilterText" class="base-input mongo-code-editor mongo-code-editor--small" :class="{ 'is-invalid': bulkFilterError }" spellcheck="false"></textarea>
              </div>
              <div v-if="bulkMode !== 'deleteMany'">
                <label class="form-label">Update operators</label>
                <textarea v-model="bulkUpdateText" class="base-input mongo-code-editor mongo-code-editor--small" :class="{ 'is-invalid': bulkUpdateError }" spellcheck="false"></textarea>
              </div>
            </div>
            <div v-if="bulkHasError" class="mongo-validation">{{ bulkFilterError || bulkUpdateError }}</div>
            <div class="mongo-muted">Preview matched {{ bulkPreviewCount.toLocaleString() }} document(s).</div>
            <div class="mongo-docs">
              <pre v-for="(doc, idx) in bulkPreview" :key="idx">{{ compactDoc(doc) }}</pre>
            </div>
          </div>

          <div v-else-if="activeTab === 'aggregation'" class="mongo-tabbody">
            <div class="mongo-editor-grid">
              <textarea v-model="aggregationText" class="base-input mongo-code-editor" spellcheck="false"></textarea>
              <div class="mongo-editor-side">
                <div class="mongo-preset-list">
                  <button class="base-btn base-btn--ghost base-btn--sm" @click="setPipelinePreset('match-limit')">Match + Limit</button>
                  <button class="base-btn base-btn--ghost base-btn--sm" @click="setPipelinePreset('group-count')">Count Group</button>
                  <button class="base-btn base-btn--ghost base-btn--sm" @click="setPipelinePreset('sort-limit')">Sort + Limit</button>
                </div>
                <button class="base-btn base-btn--primary base-btn--sm" :disabled="!selectedCollection" @click="runAggregation">Run Pipeline</button>
                <div class="mongo-muted">Pipeline must be a JSON array. Results are limited by the same limit value.</div>
              </div>
            </div>
            <div v-if="aggregationResults.length === 0" class="mongo-doc-empty">Aggregation results will appear here.</div>
            <div v-else class="mongo-docs">
              <pre v-for="(doc, idx) in aggregationResults" :key="idx">{{ compactDoc(doc) }}</pre>
            </div>
          </div>

          <div v-else-if="activeTab === 'indexes'" class="mongo-tabbody">
            <div v-if="indexRecommendations.length" class="mongo-recommendations">
              <div class="mongo-panel-title">Recommendations</div>
              <button v-for="rec in indexRecommendations" :key="rec.field" class="mongo-rec" @click="useRecommendation(rec)">
                <strong>{{ rec.field }}: {{ rec.direction }}</strong>
                <span>{{ rec.reason }} · {{ Math.round(rec.score * 100) }}%</span>
              </button>
            </div>
            <div class="mongo-index-create">
              <textarea v-model="indexKeysText" class="base-input mongo-code-editor mongo-code-editor--small" spellcheck="false"></textarea>
              <input v-model="indexName" class="base-input" placeholder="Index name, optional" />
              <label class="mongo-check"><input v-model="indexUnique" type="checkbox" /> Unique</label>
              <button class="base-btn base-btn--primary base-btn--sm" :disabled="!selectedCollection" @click="createIndex">Create Index</button>
            </div>
            <div class="mongo-index-list">
              <div v-for="idx in indexes" :key="idx.name" class="mongo-index-row">
                <div>
                  <strong>{{ idx.name }}</strong>
                  <code>{{ compactInline(idx.keys) }}</code>
                </div>
                <button class="base-btn base-btn--ghost base-btn--sm" :disabled="idx.name === '_id_'" @click="dropIndex(idx.name)">Drop</button>
              </div>
            </div>
          </div>

          <div v-else-if="activeTab === 'schema'" class="mongo-tabbody">
            <div v-if="schemaTopFields.length" class="mongo-field-chips">
              <button v-for="field in schemaTopFields" :key="field.path" @click="addFieldFilter(field.path, field.examples[0])">{{ field.path }}</button>
            </div>
            <div class="mongo-filter-row">
              <input v-model="filterText" class="base-input" placeholder='Optional sample filter, e.g. {"status":"active"}' @keydown.enter="analyzeSchema" />
              <input v-model.number="schemaLimit" class="base-input mongo-limit" type="number" min="1" max="1000" />
              <button class="base-btn base-btn--primary base-btn--sm" :disabled="!selectedCollection || schemaLoading" @click="analyzeSchema">
                {{ schemaLoading ? 'Analyzing...' : 'Analyze Schema' }}
              </button>
            </div>
            <div v-if="!schemaAnalysis" class="mongo-doc-empty">Schema field analysis will appear here.</div>
            <div v-else class="mongo-schema">
              <div class="mongo-schema__summary">
                Sampled {{ schemaAnalysis.sample_size.toLocaleString() }} document{{ schemaAnalysis.sample_size === 1 ? '' : 's' }} · {{ schemaAnalysis.fields.length.toLocaleString() }} field paths
              </div>
              <div class="mongo-schema-table">
                <div class="mongo-schema-row mongo-schema-row--head">
                  <span>Field</span>
                  <span>Occurrence</span>
                  <span>Types</span>
                  <span>Examples</span>
                  <span>Actions</span>
                </div>
                <div
                  v-for="field in schemaAnalysis.fields"
                  :key="field.path"
                  class="mongo-schema-row"
                  :class="{ 'is-mixed': field.types.length > 1 }"
                >
                  <code>{{ field.path }}</code>
                  <span>{{ field.occurrence.toFixed(1) }}% · {{ field.count.toLocaleString() }}</span>
                  <span class="mongo-type-list">
                    <b v-for="type in field.types" :key="type">{{ type }}</b>
                  </span>
                  <span class="mongo-example-list">
                    <code v-for="example in field.examples" :key="example">{{ example }}</code>
                  </span>
                  <span class="mongo-field-actions">
                    <button @click="addFieldFilter(field.path, field.examples[0])">Filter</button>
                    <button @click="addProjectionField(field.path)">Project</button>
                    <button @click="sortByField(field.path, 1)">Sort</button>
                  </span>
                </div>
              </div>
            </div>
          </div>

          <div v-else-if="activeTab === 'health'" class="mongo-tabbody">
            <div class="mongo-grid">
              <div class="page-panel mongo-card">
                <span>Status</span>
                <strong>{{ health?.status ?? '-' }}</strong>
              </div>
              <div class="page-panel mongo-card">
                <span>Version</span>
                <strong>{{ health?.version ?? '-' }}</strong>
              </div>
              <div class="page-panel mongo-card">
                <span>Storage</span>
                <strong>{{ health?.storage_engine ?? '-' }}</strong>
              </div>
              <div class="page-panel mongo-card">
                <span>Shards</span>
                <strong>{{ health?.shards?.length ?? 0 }}</strong>
              </div>
            </div>
            <div class="mongo-editor-grid">
              <pre class="mongo-explain">{{ compactDoc(health?.replica_set ?? {}) }}</pre>
              <pre class="mongo-explain">{{ compactDoc(health?.current_ops ?? []) }}</pre>
            </div>
            <pre class="mongo-explain">{{ compactDoc(health?.shards ?? []) }}</pre>
          </div>

          <div v-else-if="activeTab === 'saved'" class="mongo-tabbody">
            <div class="mongo-filter-row">
              <input v-model="saveQueryName" class="base-input" placeholder="Saved query name" @keydown.enter="saveCurrentQuery" />
              <button class="base-btn base-btn--primary base-btn--sm" @click="saveCurrentQuery">Save Current</button>
            </div>
            <div class="mongo-index-list">
              <div v-for="item in savedQueries" :key="item.id" class="mongo-index-row">
                <div>
                  <strong>{{ item.name }}</strong>
                  <code>{{ item.description || item.updated_at }}</code>
                </div>
                <div>
                  <button class="base-btn base-btn--ghost base-btn--sm" @click="applySavedQuery(item)">Apply</button>
                  <button class="base-btn base-btn--ghost base-btn--sm" @click="deleteSavedQuery(item.id)">Delete</button>
                </div>
              </div>
            </div>
          </div>

          <div v-else class="mongo-tabbody">
            <div class="mongo-filter-row">
              <input v-model="filterText" class="base-input" placeholder='Filter JSON, e.g. {"status":"active"}' @keydown.enter="runExplain" />
              <button class="base-btn base-btn--primary base-btn--sm" :disabled="!selectedCollection" @click="runExplain">Explain Query</button>
            </div>
            <div v-if="!explainResult" class="mongo-doc-empty">Explain plan will appear here.</div>
            <pre v-else class="mongo-explain">{{ compactDoc(explainResult) }}</pre>
          </div>
        </section>
      </main>

      <div v-if="docEditorOpen" class="mongo-modal-backdrop" @click.self="docEditorOpen = false">
        <section class="page-panel mongo-modal">
          <div class="mongo-panel-head">
            <div>
              <div class="mongo-panel-title">{{ docEditorMode === 'insert' ? 'Add Document' : 'Edit Document' }}</div>
              <div class="mongo-muted">{{ selectedDb }}.{{ selectedCollection }}</div>
            </div>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="docEditorOpen = false">Close</button>
          </div>
          <div v-if="docEditorMode === 'edit'" class="mongo-form-block">
            <label class="form-label">Document filter</label>
            <textarea v-model="docEditorFilter" class="base-input mongo-code-editor mongo-code-editor--small" spellcheck="false"></textarea>
          </div>
          <div class="mongo-form-block">
            <label class="form-label">Document JSON</label>
            <textarea v-model="docEditorText" class="base-input mongo-code-editor" spellcheck="false"></textarea>
          </div>
          <div v-if="docEditorMode === 'edit'" class="mongo-form-block">
            <label class="form-label">Before / after diff source</label>
            <div class="mongo-diff-grid">
              <pre>{{ originalDocText }}</pre>
              <pre>{{ docEditorText }}</pre>
            </div>
          </div>
          <div class="mongo-modal-actions">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="docEditorOpen = false">Cancel</button>
            <button class="base-btn base-btn--primary base-btn--sm" :disabled="savingDoc" @click="saveDocument">{{ savingDoc ? 'Saving...' : 'Save Document' }}</button>
          </div>
        </section>
      </div>

      <div v-if="collectionModalOpen" class="mongo-modal-backdrop" @click.self="collectionModalOpen = false">
        <section class="page-panel mongo-modal mongo-modal--small">
          <div class="mongo-panel-head">
            <div>
              <div class="mongo-panel-title">{{ collectionMode === 'create' ? 'Create Collection' : 'Rename Collection' }}</div>
              <div class="mongo-muted">{{ selectedDb }}</div>
            </div>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="collectionModalOpen = false">Close</button>
          </div>
          <div v-if="collectionMode === 'create'" class="mongo-form-block">
            <label class="form-label">Collection name</label>
            <input v-model="collectionName" class="base-input" placeholder="orders" @keydown.enter="saveCollection" />
          </div>
          <template v-else>
            <div class="mongo-form-block">
              <label class="form-label">Current name</label>
              <input v-model="collectionName" class="base-input" disabled />
            </div>
            <div class="mongo-form-block">
              <label class="form-label">New name</label>
              <input v-model="collectionNewName" class="base-input" @keydown.enter="saveCollection" />
            </div>
          </template>
          <div class="mongo-modal-actions">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="collectionModalOpen = false">Cancel</button>
            <button class="base-btn base-btn--primary base-btn--sm" @click="saveCollection">Save</button>
          </div>
        </section>
      </div>

      <div v-if="importModalOpen" class="mongo-modal-backdrop" @click.self="importModalOpen = false">
        <section class="page-panel mongo-modal">
          <div class="mongo-panel-head">
            <div>
              <div class="mongo-panel-title">Import JSON</div>
              <div class="mongo-muted">{{ selectedDb }}.{{ selectedCollection }}</div>
            </div>
            <button class="base-btn base-btn--ghost base-btn--sm" @click="importModalOpen = false">Close</button>
          </div>
          <div class="mongo-form-block">
            <label class="form-label">JSON array</label>
            <textarea v-model="importText" class="base-input mongo-code-editor" spellcheck="false"></textarea>
          </div>
          <div class="mongo-modal-actions">
            <button class="base-btn base-btn--ghost base-btn--sm" @click="importModalOpen = false">Cancel</button>
            <button class="base-btn base-btn--primary base-btn--sm" :disabled="importing" @click="importDocuments">{{ importing ? 'Importing...' : 'Import' }}</button>
          </div>
        </section>
      </div>
    </template>
  </div>
</template>

<style scoped>
.mongo-page {
  height: 100%;
  padding: 18px;
  overflow: auto;
  background: var(--bg-body);
}
.mongo-empty {
  max-width: 520px;
  margin: 48px auto;
  padding: 28px;
}
.mongo-empty__title {
  font-size: 18px;
  font-weight: 700;
  color: var(--text-primary);
  margin-bottom: 14px;
}
.mongo-picker { display: grid; gap: 8px; }
.mongo-head {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: flex-start;
  margin-bottom: 16px;
}
.mongo-kicker {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  color: #00a35c;
}
.mongo-head h1 {
  margin: 3px 0;
  font-size: 24px;
  color: var(--text-primary);
}
.mongo-head p {
  margin: 0;
  font-family: var(--mono, monospace);
  font-size: 12px;
  color: var(--text-muted);
}
.mongo-actions {
  display: flex;
  gap: 8px;
}
.mongo-db-select { min-width: 180px; }
.mongo-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}
.mongo-card {
  padding: 14px;
  display: grid;
  gap: 5px;
}
.mongo-card span {
  font-size: 11px;
  font-weight: 700;
  color: var(--text-muted);
  text-transform: uppercase;
}
.mongo-card strong {
  font-size: 20px;
  color: var(--text-primary);
}
.mongo-layout {
  display: grid;
  grid-template-columns: 340px minmax(0, 1fr);
  gap: 16px;
}
.mongo-side,
.mongo-main {
  min-height: 520px;
  padding: 14px;
}
.mongo-panel-title {
  font-size: 14px;
  font-weight: 700;
  color: var(--text-primary);
}
.mongo-panel-head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}
.mongo-main-actions {
  display: flex;
  gap: 8px;
  align-items: center;
}
.mongo-tabs {
  display: flex;
  gap: 6px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 12px;
  overflow-x: auto;
}
.mongo-tabs button {
  border: 0;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--text-muted);
  padding: 9px 10px;
  font-size: 12px;
  font-weight: 700;
  cursor: pointer;
}
.mongo-tabs button.active {
  color: #00a35c;
  border-bottom-color: #00a35c;
}
.mongo-tabbody {
  display: grid;
  gap: 12px;
}
.mongo-list {
  display: grid;
  gap: 6px;
  margin-top: 12px;
}
.mongo-side-head {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  align-items: center;
}
.mongo-list-row {
  display: grid;
  gap: 3px;
  text-align: left;
  border: 1px solid var(--border);
  background: var(--bg-surface);
  color: var(--text-primary);
  border-radius: 8px;
  padding: 10px;
  cursor: pointer;
}
.mongo-list-row.active,
.mongo-list-row:hover {
  border-color: #00a35c;
  background: rgba(0, 163, 92, 0.08);
}
.mongo-list-row span {
  font-family: var(--mono, monospace);
  font-size: 12px;
}
.mongo-side-search {
  margin-top: 12px;
}
.mongo-list-row small,
.mongo-muted {
  color: var(--text-muted);
  font-size: 12px;
}
.base-input.is-invalid {
  border-color: #dc2626;
  box-shadow: 0 0 0 2px rgba(220, 38, 38, 0.12);
}
.mongo-filter-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 100px;
  gap: 8px;
  margin-bottom: 12px;
}
.mongo-filter-row--advanced {
  grid-template-columns: minmax(180px, 1.4fr) minmax(140px, 1fr) minmax(140px, 1fr) 92px;
}
.mongo-toolbar,
.mongo-import-export {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  justify-content: flex-end;
  align-items: center;
  margin-top: -4px;
}
.mongo-segment {
  display: inline-flex;
  border: 1px solid var(--border);
  border-radius: 8px;
  overflow: hidden;
  background: var(--bg-surface);
}
.mongo-segment button {
  border: 0;
  background: transparent;
  color: var(--text-muted);
  padding: 6px 10px;
  font-size: 12px;
  font-weight: 700;
  cursor: pointer;
}
.mongo-segment button.active {
  background: rgba(0, 163, 92, 0.14);
  color: #00a35c;
}
.mongo-export-select {
  width: 112px;
  min-height: 30px;
}
.mongo-validation {
  border: 1px solid rgba(220, 38, 38, 0.35);
  background: rgba(220, 38, 38, 0.08);
  color: #ef4444;
  border-radius: 8px;
  padding: 8px 10px;
  font-size: 12px;
}
.mongo-page-tools {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 8px;
  color: var(--text-muted);
  font-size: 12px;
}
.mongo-table-wrap {
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.mongo-table {
  width: 100%;
  border-collapse: collapse;
  min-width: 720px;
}
.mongo-table th,
.mongo-table td {
  border-bottom: 1px solid var(--border);
  border-right: 1px solid var(--border);
  padding: 8px 10px;
  text-align: left;
  vertical-align: top;
  max-width: 260px;
}
.mongo-table th {
  color: var(--text-muted);
  background: var(--bg-surface);
  font-size: 11px;
  text-transform: uppercase;
}
.mongo-table td code {
  display: block;
  color: var(--text-secondary);
  font-family: var(--mono, monospace);
  font-size: 11px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.mongo-row-actions {
  white-space: nowrap;
}
.mongo-doc-empty {
  display: grid;
  place-items: center;
  min-height: 280px;
  color: var(--text-muted);
  border: 1px dashed var(--border);
  border-radius: 8px;
}
.mongo-docs {
  display: grid;
  gap: 10px;
}
.mongo-doc-card {
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-panel);
  overflow: hidden;
}
.mongo-doc-card__head {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  align-items: center;
  padding: 8px 10px;
  border-bottom: 1px solid var(--border);
}
.mongo-doc-card__head code,
.mongo-index-row code {
  font-size: 11px;
  color: var(--text-muted);
  font-family: var(--mono, monospace);
  word-break: break-word;
}
.mongo-docs pre,
.mongo-explain {
  margin: 0;
  padding: 12px;
  overflow: auto;
  max-height: 420px;
  background: var(--bg-surface);
  color: var(--text-primary);
  font-size: 12px;
  line-height: 1.45;
}
.mongo-editor-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 220px;
  gap: 12px;
}
.mongo-editor-side {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.mongo-preset-list {
  display: grid;
  gap: 6px;
}
.mongo-code-editor {
  min-height: 260px;
  font-family: var(--mono, monospace);
  font-size: 12px;
  line-height: 1.5;
  resize: vertical;
}
.mongo-code-editor--small {
  min-height: 96px;
}
.mongo-index-create {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) minmax(160px, 240px) auto auto;
  gap: 8px;
  align-items: center;
}
.mongo-check {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--text-secondary);
}
.mongo-index-list {
  display: grid;
  gap: 8px;
}
.mongo-action-strip {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: center;
  border: 1px solid rgba(0, 163, 92, 0.28);
  background: rgba(0, 163, 92, 0.08);
  color: var(--text-primary);
  border-radius: 8px;
  padding: 10px 12px;
}
.mongo-action-strip.danger {
  border-color: rgba(220, 38, 38, 0.35);
  background: rgba(220, 38, 38, 0.08);
}
.mongo-action-strip span {
  color: var(--text-muted);
  font-size: 12px;
}
.mongo-recommendations {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}
.mongo-rec {
  border: 1px solid var(--border);
  background: var(--bg-surface);
  color: var(--text-primary);
  border-radius: 8px;
  padding: 8px 10px;
  display: grid;
  gap: 3px;
  cursor: pointer;
  text-align: left;
}
.mongo-rec span {
  color: var(--text-muted);
  font-size: 11px;
}
.mongo-index-row {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  align-items: center;
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 10px;
  background: var(--bg-surface);
}
.mongo-index-row strong {
  display: block;
  margin-bottom: 4px;
  color: var(--text-primary);
}
.mongo-schema {
  display: grid;
  gap: 10px;
}
.mongo-schema__summary {
  font-size: 12px;
  color: var(--text-muted);
}
.mongo-schema-table {
  display: grid;
  border: 1px solid var(--border);
  border-radius: 8px;
  overflow: hidden;
}
.mongo-schema-row {
  display: grid;
  grid-template-columns: minmax(170px, 1fr) 140px minmax(150px, 0.8fr) minmax(200px, 1.1fr) 170px;
  gap: 10px;
  align-items: start;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-panel);
  font-size: 12px;
}
.mongo-schema-row:last-child {
  border-bottom: 0;
}
.mongo-schema-row--head {
  background: var(--bg-surface);
  color: var(--text-muted);
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
}
.mongo-schema-row.is-mixed {
  background: rgba(245, 158, 11, 0.08);
}
.mongo-schema-row code {
  font-family: var(--mono, monospace);
  color: var(--text-primary);
  word-break: break-word;
}
.mongo-type-list,
.mongo-example-list {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
}
.mongo-type-list b {
  border-radius: 999px;
  background: rgba(0, 163, 92, 0.1);
  color: #00a35c;
  padding: 3px 7px;
  font-size: 11px;
}
.mongo-example-list code {
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg-surface);
  padding: 3px 6px;
  color: var(--text-muted);
}
.mongo-field-chips,
.mongo-field-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.mongo-field-chips button,
.mongo-field-actions button {
  border: 1px solid var(--border);
  background: var(--bg-surface);
  color: var(--text-secondary);
  border-radius: 6px;
  padding: 4px 7px;
  font-size: 11px;
  cursor: pointer;
}
.mongo-field-chips button:hover,
.mongo-field-actions button:hover {
  border-color: #00a35c;
  color: #00a35c;
}
.mongo-modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 10000;
  background: rgba(15, 23, 42, 0.46);
  display: grid;
  place-items: center;
  padding: 18px;
}
.mongo-modal {
  width: min(860px, 100%);
  max-height: min(760px, calc(100vh - 36px));
  overflow: auto;
  padding: 16px;
}
.mongo-modal--small {
  width: min(520px, 100%);
}
.mongo-form-block {
  display: grid;
  gap: 6px;
  margin-bottom: 12px;
}
.mongo-modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
.mongo-diff-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}
.mongo-diff-grid pre {
  margin: 0;
  max-height: 220px;
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-surface);
  padding: 10px;
  color: var(--text-secondary);
  font-size: 11px;
}
.mongo-error {
  margin-bottom: 12px;
  padding: 10px 12px;
  border: 1px solid #b91c1c;
  background: #fee2e2;
  color: #7f1d1d;
  border-radius: 8px;
  white-space: pre-wrap;
}
@media (max-width: 900px) {
  .mongo-head,
  .mongo-actions,
  .mongo-panel-head {
    flex-direction: column;
  }
  .mongo-main-actions,
  .mongo-filter-row,
  .mongo-filter-row--advanced,
  .mongo-editor-grid,
  .mongo-index-create,
  .mongo-schema-row,
  .mongo-diff-grid {
    grid-template-columns: 1fr;
  }
  .mongo-main-actions {
    flex-direction: column;
    align-items: stretch;
  }
  .mongo-import-export,
  .mongo-toolbar,
  .mongo-action-strip,
  .mongo-side-head {
    flex-direction: column;
    align-items: stretch;
  }
  .mongo-layout {
    grid-template-columns: 1fr;
  }
}
</style>
