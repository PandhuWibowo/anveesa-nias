# Data Pipeline — Class Tree

## Overview

Fitur Data Pipeline memungkinkan user membuat dan menjalankan pipeline ETL sederhana secara visual — drag-and-drop node di canvas, lalu eksekusi manual. Pipeline direpresentasikan sebagai DAG (Directed Acyclic Graph) dari node-node yang saling terhubung.

**Phase yang sudah diimplementasi:** Phase 1 — `source_query`, `source_table`, `sink_table` (async execution via goroutine + polling).

---

## Data Model (Internal DB)

```
pipelines
  id, name, description, created_by → users(id), status (draft|active|paused),
  schedule (cron, nullable), last_run_at, created_at, updated_at

pipeline_nodes
  id, pipeline_id → pipelines(id) CASCADE, node_type, connection_id → connections(id),
  config (JSONB), position_x, position_y, label

pipeline_edges
  id, pipeline_id → pipelines(id) CASCADE,
  source_node_id → pipeline_nodes(id) CASCADE,
  target_node_id → pipeline_nodes(id) CASCADE

pipeline_runs
  id, pipeline_id → pipelines(id) CASCADE, triggered_by (manual|schedule),
  status (running|success|failed), started_at, finished_at, rows_processed, error_message

pipeline_run_logs
  id, run_id → pipeline_runs(id) CASCADE, node_id → pipeline_nodes(id),
  node_label, message, rows_affected, duration_ms, logged_at
```

Indexes: `idx_pipeline_nodes_pipeline`, `idx_pipeline_edges_pipeline`, `idx_pipeline_runs_pipeline`, `idx_pipeline_run_logs_run`

---

## Permissions

Didefinisikan di `handlers/models.go`, group "Data Engineering":

| Konstanta | String | Deskripsi |
|---|---|---|
| `PermPipelinesView` | `pipelines.view` | Lihat list pipeline & riwayat run |
| `PermPipelinesManage` | `pipelines.manage` | Buat, edit, hapus pipeline |
| `PermPipelinesRun` | `pipelines.run` | Trigger eksekusi pipeline |

Auto-granted ke role `admin` dan `poweruser` saat migrasi DB.

---

## Backend

### GET /api/pipelines
```
└── mw.RequireAnyAppPermission(PermPipelinesView)
    └── handlers.ListPipelines()                              [handlers/pipelines.go]
        └── appdb.DB.QueryContext(SELECT FROM pipelines ORDER BY created_at DESC)
```

### POST /api/pipelines
```
└── mw.RequireAnyAppPermission(PermPipelinesManage)
    └── handlers.CreatePipeline()                             [handlers/pipelines.go]
        ├── json.NewDecoder(r.Body).Decode() — name, description
        ├── appdb.DB.QueryRow(SELECT id FROM users WHERE username=?)  — resolve user_id dari X-Username header
        └── appdb.DB.ExecContext(INSERT INTO pipelines ... status='draft')
```

### GET /api/pipelines/{id}
```
└── mw.RequireAnyAppPermission(PermPipelinesView)
    └── handlers.GetPipeline()                                [handlers/pipelines.go]
        ├── parsePipelineID(r, "/api/pipelines/")
        ├── appdb.DB.QueryRowContext(SELECT FROM pipelines WHERE id=?)
        ├── appdb.DB.QueryContext(SELECT FROM pipeline_nodes WHERE pipeline_id=?)
        └── appdb.DB.QueryContext(SELECT FROM pipeline_edges WHERE pipeline_id=?)
```

### PUT /api/pipelines/{id}
```
└── mw.RequireAnyAppPermission(PermPipelinesManage)
    └── handlers.UpdatePipeline()                             [handlers/pipelines.go]
        ├── json.NewDecoder(r.Body).Decode() — name, description, status, schedule, nodes[], edges[]
        ├── appdb.DB.ExecContext(UPDATE pipelines SET ...)
        ├── appdb.DB.ExecContext(DELETE FROM pipeline_nodes WHERE pipeline_id=?)
        ├── per node: appdb.DB.ExecContext(INSERT INTO pipeline_nodes ...) → map temp_id → real_id
        ├── appdb.DB.ExecContext(DELETE FROM pipeline_edges WHERE pipeline_id=?)
        └── per edge: appdb.DB.ExecContext(INSERT INTO pipeline_edges ...) — pakai nodeIDMap untuk remap ID
```

### DELETE /api/pipelines/{id}
```
└── mw.RequireAnyAppPermission(PermPipelinesManage)
    └── handlers.DeletePipeline()                             [handlers/pipelines.go]
        └── appdb.DB.ExecContext(DELETE FROM pipelines WHERE id=?) — cascade ke nodes, edges, runs, logs
```

### POST /api/pipelines/{id}/run
```
└── mw.RequireAnyAppPermission(PermPipelinesRun)
    └── handlers.TriggerPipelineRun()                         [handlers/pipelines.go]
        ├── appdb.DB.ExecContext(INSERT INTO pipeline_runs ... status='running') → run_id
        ├── go RunPipeline(pipelineID, runID, username)        [async goroutine]
        └── json.Encode({ run_id })                            — return segera, eksekusi di background
```

### GET /api/pipelines/{id}/runs
```
└── mw.RequireAnyAppPermission(PermPipelinesView)
    └── handlers.ListPipelineRuns()                           [handlers/pipelines.go]
        └── appdb.DB.QueryContext(SELECT FROM pipeline_runs WHERE pipeline_id=? ORDER BY started_at DESC LIMIT 50)
```

### GET /api/pipelines/{id}/runs/{runId}
```
└── mw.RequireAnyAppPermission(PermPipelinesView)
    └── handlers.GetPipelineRunStatus()                       [handlers/pipelines.go]
        └── appdb.DB.QueryRowContext(SELECT FROM pipeline_runs WHERE id=?)
```

### GET /api/pipelines/{id}/runs/{runId}/logs
```
└── mw.RequireAnyAppPermission(PermPipelinesView)
    └── handlers.GetRunLogs()                                 [handlers/pipelines.go]
        └── appdb.DB.QueryContext(SELECT FROM pipeline_run_logs WHERE run_id=? ORDER BY logged_at ASC)
```

---

## Execution Engine

### RunPipeline (async goroutine)
```
handlers.RunPipeline(pipelineID, runID int64, triggeredBy string)  [handlers/pipeline_executor.go]
│
├── logStep(nodeID, label, message, rowsAffected, durationMs)
│   └── appdb.DB.ExecContext(INSERT INTO pipeline_run_logs ...)
│
├── finishRun(status, errMsg, rowsProcessed)
│   ├── appdb.DB.ExecContext(UPDATE pipeline_runs SET status=?, finished_at=NOW(), ...)
│   ├── appdb.DB.ExecContext(UPDATE pipelines SET last_run_at=NOW() WHERE id=?)
│   └── EmitNotification(NotificationEventInput{...})              [handlers/notifications.go]
│       — severity: "info" (success) | "error" (failed)
│
├── appdb.DB.QueryContext(SELECT FROM pipeline_nodes WHERE pipeline_id=?)
│   └── scan ke map[int64]*execNode + []nodeOrder
│
├── appdb.DB.QueryContext(SELECT source_node_id, target_node_id FROM pipeline_edges)
│   └── build inEdges map[targetID][]sourceID
│
├── topoSort(nodeOrder, inEdges)                                    [handlers/pipeline_executor.go]
│   └── Kahn's algorithm (BFS) — error jika graph mengandung cycle
│
└── per node (dalam urutan topologis):
    ├── [source_query | source_table]
    │   └── execSourceNode(ctx, n, buffers, colNames)              [handlers/pipeline_executor.go]
    │       ├── GetDB(*n.connectionID)                             [handlers/pool.go]
    │       ├── source_query: ambil config["sql"]
    │       ├── source_table: build "SELECT * FROM [schema.]table [LIMIT n]"
    │       ├── db.QueryContext(sqlStr)                            [user DB]
    │       ├── rows.Columns() → colNames[n.id]
    │       └── scan rows → buffers[n.id] (max 500_000 rows)
    │
    ├── [sink_table]
    │   └── execSinkTable(ctx, n, rows, cols)                      [handlers/pipeline_executor.go]
    │       ├── GetDB(*n.connectionID)                             [handlers/pool.go]
    │       ├── quoteIdent(driver, col) per kolom
    │       ├── qualifiedTableName(driver, schema, table)
    │       ├── build INSERT INTO ... statement (? atau $N tergantung driver)
    │       ├── db.BeginTx(ctx, nil)
    │       ├── tx.PrepareContext(ctx, stmt)
    │       ├── per row: prepared.ExecContext(ctx, args...)
    │       └── tx.Commit()
    │
    └── [node type lain] → log "Skipping unsupported node type"
```

**Catatan implementasi:**
- Buffer in-memory: hard limit **500k rows** per source node
- Sink table: mode INSERT saja (phase 1); upsert/replace belum diimplementasi
- Error pada satu node langsung `finishRun("failed", ...)` dan stop eksekusi

---

## Frontend

### File
| File | Deskripsi |
|---|---|
| `web/src/views/DataPipelinesView.vue` | Main view — list pipelines + canvas editor |
| `web/src/composables/usePipelines.ts` | API layer — semua HTTP calls ke /api/pipelines |

### Route
```
/data-pipelines
  requiredPermissionsAny: ['pipelines.view']
  component: () => import('@/views/DataPipelinesView.vue')
```

### usePipelines Composable
```
usePipelines()                                                [composables/usePipelines.ts]
├── state: pipelines[], loading, error
├── fetchPipelines()      → GET /api/pipelines
├── createPipeline()      → POST /api/pipelines
├── getPipeline(id)       → GET /api/pipelines/{id}
├── savePipeline(id, payload) → PUT /api/pipelines/{id}
├── deletePipeline(id)    → DELETE /api/pipelines/{id}
├── triggerRun(id)        → POST /api/pipelines/{id}/run  → run_id
├── fetchRuns(id)         → GET /api/pipelines/{id}/runs
├── fetchRunStatus(pid, rid) → GET /api/pipelines/{id}/runs/{runId}
└── fetchRunLogs(pid, rid)   → GET /api/pipelines/{id}/runs/{runId}/logs
```

### DataPipelinesView — State & Logic
```
DataPipelinesView.vue
│
├── view: 'list' | 'canvas'
├── usePipelines() + useConnections() + useToast()
│
├── [List View]
│   ├── Tampilkan pipelines[] dari fetchPipelines()
│   ├── openCreateModal() → modal nama + deskripsi → confirmCreate()
│   │   └── createPipeline() → openPipeline(id)
│   ├── openPipeline(id)
│   │   ├── getPipeline(id) → currentPipeline
│   │   ├── toFlowNodes(p.nodes) → nodes (VueFlow format)
│   │   ├── toFlowEdges(p.edges) → edges (VueFlow format)
│   │   └── fetchRuns(id) → runs
│   └── handleDelete(p) → deletePipeline(id) → fetchPipelines()
│
├── [Canvas View — VueFlow]
│   ├── Library: @vue-flow/core + @vue-flow/background + @vue-flow/controls
│   ├── NODE_TYPES: source_query, source_table, sink_table
│   │   — source: biru (#3b82f6), sink: hijau (#10b981)
│   ├── relationalConnections — filter connections by driver (postgres/mysql/mariadb/mssql/sqlite)
│   ├── Node palette (drag ke canvas) → addNode() → nodes.value.push(...)
│   ├── onNodeClick → selectedNode → config panel kanan
│   ├── onConnect → addEdges() — hubungkan node via drag
│   │
│   ├── save()
│   │   ├── fromFlowNodes(nodes, edges) → { pNodes, pEdges }
│   │   │   — remap negative temp IDs untuk node baru
│   │   ├── savePipeline(id, { name, description, status, schedule, nodes, edges })
│   │   └── getPipeline(id) → reload dengan real DB IDs
│   │
│   └── runPipeline()
│       ├── save() — auto-save sebelum run
│       ├── triggerRun(id) → runId
│       ├── startPolling(runId)
│       │   └── setInterval(1500ms):
│       │       ├── fetchRunStatus(pid, runId) → update runs[]
│       │       ├── fetchRunLogs(pid, runId) → runLogs (jika runId dipilih)
│       │       └── [jika status != 'running'] clearInterval + toast
│       └── showRunDrawer = true
│
└── [Run History Drawer]
    ├── runs[] — list 50 run terbaru
    ├── viewRunLogs(run) → fetchRunLogs() → runLogs[]
    └── runLogs[] — log per node per run (node_label, message, rows_affected, duration_ms)
```

### Node Config Panel
Setiap node yang dipilih menampilkan form sesuai `node_type`:

| node_type | Config Fields |
|---|---|
| `source_query` | connection (dropdown relational), SQL editor |
| `source_table` | connection (dropdown relational), schema (opsional), table name, limit (opsional) |
| `sink_table` | connection (dropdown relational), schema (opsional), table name |

---

## Node Conversion (VueFlow ↔ Backend)

```
toFlowNodes(pipelineNodes[])  → Node[]          (backend → VueFlow)
  id: String(n.id)
  type: 'default'
  position: { x: n.position_x, y: n.position_y }
  data: { nodeType, connectionId, config, label }
  style: warna berdasarkan node_type prefix

toFlowEdges(pipelineEdges[]) → Edge[]           (backend → VueFlow)
  id: "e{source}-{target}"
  animated: true

fromFlowNodes(flowNodes[], flowEdges[])          (VueFlow → backend)
  node.id: parseInt(n.id) || -(idx+1)           — negatif untuk node baru (belum ada di DB)
  edge: { source_node_id, target_node_id }       — UpdatePipeline handler yang remap ke real ID
```

---

## Alur Eksekusi End-to-End

```
User klik "Run"
  → save() — auto-save canvas state ke DB
  → POST /api/pipelines/{id}/run
      → INSERT pipeline_runs (status='running') → run_id
      → go RunPipeline(...)                      — goroutine, non-blocking
      → response: { run_id }
  → startPolling(runId) setiap 1500ms
      → GET /api/pipelines/{id}/runs/{runId}     — cek status
      → GET /api/pipelines/{id}/runs/{runId}/logs — update log drawer
      → [jika status != 'running'] stop polling + toast

RunPipeline goroutine:
  → load nodes + edges dari DB
  → topoSort (Kahn's algorithm)
  → per node:
      source → execSourceNode → buffer hasil di memory
      sink   → execSinkTable  → INSERT ke target DB (transaksional)
  → UPDATE pipeline_runs SET status='success'|'failed'
  → EmitNotification
```

---

## Phase Berikutnya (Belum Diimplementasi)

| Phase | Fitur |
|---|---|
| Phase 2 | `sink_export` (CSV/JSON/Excel), `sink_s3` (AWS SDK + S3 connection type) |
| Phase 3 | `transform_sql` node (in-memory filter), schedule trigger, SSE live log |
| Phase 4 | Streaming cursor untuk large dataset, preview per node, `source_table` shortcut, multi-branch fan-out |
