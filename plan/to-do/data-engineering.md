# Plan: Data Pipeline (Data Engineering)

## Gambaran Umum

Fitur ini memungkinkan user membuat pipeline ETL sederhana secara visual — drag-and-drop node di canvas, lalu jalankan secara manual atau terjadwal. Setiap pipeline adalah DAG (Directed Acyclic Graph) dari node-node yang dihubungkan.

**Flow yang didukung (target):**
```
Table → Query → Table
Table → Query → Export → S3
Table → Query → Export → File (CSV/JSON/Excel)
```

---

## Komponen yang Bisa Dipakai Ulang dari Kode yang Sudah Ada

Tidak perlu menulis dari nol — banyak infrastruktur yang sudah ada:

| Yang Dipakai | File Sumber | Untuk Apa |
|---|---|---|
| `GetDB(connID)` | `handlers/pool.go` | Buka koneksi ke DB source/sink |
| `db.QueryContext()` | pola dari `handlers/query.go` | Eksekusi SQL source query |
| `tx.PrepareContext() + ExecContext()` | `handlers/import.go` | Batch INSERT ke sink table |
| `EmitNotificationEvent()` | `handlers/notifications.go` | Notifikasi ketika run selesai/gagal |
| `WriteAuditLog()` | `handlers/audit.go` | Audit log setiap eksekusi pipeline |
| `splitStatements()` | `handlers/multi_exec.go` | Multi-statement transform SQL |
| Export (CSV/JSON/Excel) | `handlers/analytics_dashboards.go` | Sink export file |
| Schedule/cron pattern | `handlers/scheduler.go` | Trigger terjadwal |
| Permission constants | `handlers/models.go` | Tambah perm baru |
| `decryptCredential()` | `handlers/connections.go` | Buka cred koneksi terenkripsi |

---

## Data Model (DB Internal)

```sql
-- Pipeline definition
CREATE TABLE pipelines (
  id          SERIAL PRIMARY KEY,
  name        TEXT NOT NULL,
  description TEXT,
  created_by  INTEGER REFERENCES users(id),
  status      TEXT DEFAULT 'draft',     -- draft | active | paused
  schedule    TEXT,                      -- cron expression, nullable
  last_run_at TIMESTAMP,
  created_at  TIMESTAMP DEFAULT NOW(),
  updated_at  TIMESTAMP DEFAULT NOW()
);

-- Nodes dalam pipeline (sumber, transform, tujuan)
CREATE TABLE pipeline_nodes (
  id            SERIAL PRIMARY KEY,
  pipeline_id   INTEGER REFERENCES pipelines(id) ON DELETE CASCADE,
  node_type     TEXT NOT NULL,           -- lihat "Tipe Node" di bawah
  connection_id INTEGER REFERENCES connections(id) ON DELETE SET NULL,
  config        JSONB NOT NULL DEFAULT '{}',  -- konfigurasi per tipe node
  position_x    FLOAT,                   -- posisi di canvas UI
  position_y    FLOAT,
  label         TEXT,
  created_at    TIMESTAMP DEFAULT NOW()
);

-- Edge (koneksi antar node)
CREATE TABLE pipeline_edges (
  id             SERIAL PRIMARY KEY,
  pipeline_id    INTEGER REFERENCES pipelines(id) ON DELETE CASCADE,
  source_node_id INTEGER REFERENCES pipeline_nodes(id) ON DELETE CASCADE,
  target_node_id INTEGER REFERENCES pipeline_nodes(id) ON DELETE CASCADE
);

-- Riwayat eksekusi pipeline
CREATE TABLE pipeline_runs (
  id             SERIAL PRIMARY KEY,
  pipeline_id    INTEGER REFERENCES pipelines(id) ON DELETE CASCADE,
  triggered_by   TEXT DEFAULT 'manual',  -- manual | schedule
  status         TEXT DEFAULT 'running', -- running | success | failed
  started_at     TIMESTAMP DEFAULT NOW(),
  finished_at    TIMESTAMP,
  rows_processed INTEGER DEFAULT 0,
  error_message  TEXT
);

-- Log per node per run
CREATE TABLE pipeline_run_logs (
  id            SERIAL PRIMARY KEY,
  run_id        INTEGER REFERENCES pipeline_runs(id) ON DELETE CASCADE,
  node_id       INTEGER REFERENCES pipeline_nodes(id),
  node_label    TEXT,
  message       TEXT,
  rows_affected INTEGER,
  duration_ms   INTEGER,
  logged_at     TIMESTAMP DEFAULT NOW()
);
```

---

## Tipe Node

### Source Nodes
| node_type | Deskripsi | Config |
|---|---|---|
| `source_query` | Koneksi + SQL query custom | `{ connection_id, sql, limit? }` |
| `source_table` | Koneksi + pilih tabel (auto SELECT *) | `{ connection_id, table, schema?, limit? }` |

### Transform Nodes
| node_type | Deskripsi | Config |
|---|---|---|
| `transform_sql` | SQL yang di-apply ke result set sebelumnya (input sebagai `__input__`) | `{ sql }` — mis. `SELECT a, b FROM __input__ WHERE c > 10` |

### Sink Nodes
| node_type | Deskripsi | Config |
|---|---|---|
| `sink_table` | Insert/upsert result ke tabel di koneksi lain | `{ connection_id, table, schema?, write_mode: insert/upsert/replace, conflict_columns? }` |
| `sink_export` | Export ke file (download) | `{ format: csv/json/excel }` |
| `sink_s3` | Upload ke S3 | `{ bucket, key, region, format: csv/json/parquet, access_key_id, secret_access_key }` — credential dienkripsi |

---

## Backend API

File baru: `handlers/pipelines.go` + `handlers/pipeline_executor.go`

```
GET    /api/pipelines                          → ListPipelines()
POST   /api/pipelines                          → CreatePipeline()
GET    /api/pipelines/{id}                     → GetPipeline() — termasuk nodes & edges
PUT    /api/pipelines/{id}                     → UpdatePipeline() — save full canvas state
DELETE /api/pipelines/{id}                     → DeletePipeline()
POST   /api/pipelines/{id}/run                 → TriggerPipelineRun() — async, return run_id
GET    /api/pipelines/{id}/runs                → ListPipelineRuns()
GET    /api/pipelines/{id}/runs/{runId}/logs   → GetRunLogs()
POST   /api/pipelines/{id}/validate            → ValidatePipeline() — cek koneksi + SQL syntax
POST   /api/pipelines/{id}/preview             → PreviewSourceNode() — return N rows dari source
```

### Execution Engine (`handlers/pipeline_executor.go`)

```
RunPipeline(pipelineID, triggeredBy string) error
├── INSERT INTO pipeline_runs → dapat run_id
├── topological sort nodes (via DFS pada edges)
├── per node (in order):
│   ├── [source_query / source_table]
│   │   ├── GetDB(node.connection_id)           [pool.go]
│   │   ├── db.QueryContext(node.config.sql)    [user DB]
│   │   └── simpan result set ke memory buffer
│   │
│   ├── [transform_sql]
│   │   ├── ambil result dari node upstream
│   │   ├── load ke SQLite in-memory (atau apply filter di Go)
│   │   └── simpan result baru ke buffer
│   │
│   ├── [sink_table]
│   │   ├── GetDB(node.connection_id)           [pool.go]
│   │   ├── db.BeginTx()
│   │   ├── tx.PrepareContext(INSERT INTO ...)  [pola dari import.go]
│   │   ├── prepared.ExecContext() per row
│   │   └── tx.Commit()
│   │
│   ├── [sink_export]
│   │   └── generate file → simpan ke temp, return download URL
│   │
│   └── [sink_s3]
│       └── stream result → AWS SDK PutObject
│
├── log setiap step ke pipeline_run_logs
├── UPDATE pipeline_runs SET status='success/failed'
├── WriteAuditLog(...)                          [audit.go]
└── EmitNotificationEvent(...)                  [notifications.go]
```

### Permissions Baru (tambah ke `handlers/models.go`)
```go
PermPipelinesView   = "pipelines.view"
PermPipelinesManage = "pipelines.manage"
PermPipelinesRun    = "pipelines.run"
```

---

## Frontend

**Library canvas:** [Vue Flow](https://vueflow.dev/) — Vue 3 native, reactive, sudah mature.

**Route baru:** `/data-pipelines` — permission: `pipelines.view`

**File baru:**
- `views/DataPipelinesView.vue` — main view
- `composables/usePipelines.ts` — API calls

### Layout UI

```
┌─────────────────────────────────────────────────────────────────┐
│  [← Pipelines]  Pipeline: "Orders to DW"    [Validate] [▶ Run] │
├──────────┬──────────────────────────────────────┬───────────────┤
│  Node    │                                      │  Node Config  │
│ Palette  │           Canvas (Vue Flow)          │    Panel      │
│          │                                      │               │
│ ○ Source │   [Source: orders] ──→ [Transform]  │  Connection:  │
│   Table  │                           ──→ [Sink] │  [dropdown]   │
│          │                                      │               │
│ ○ Source │                                      │  SQL:         │
│   Query  │                                      │  [editor]     │
│          │                                      │               │
│ ○ Trans- │                                      │  Table:       │
│   form   │                                      │  [input]      │
│          │                                      │               │
│ ○ Sink   │                                      │  Write mode:  │
│   Table  │                                      │  [select]     │
│          │                                      │               │
│ ○ Sink   ├──────────────────────────────────────┤               │
│   Export │  Run History (bottom drawer)         │               │
│          │  #12 success  2026-05-12 14:30  320ms│               │
│ ○ Sink   │  #11 failed   2026-05-12 13:15       │               │
│   S3     │                                      │               │
└──────────┴──────────────────────────────────────┴───────────────┘
```

### Alur User
1. Buka `/data-pipelines` → lihat list pipelines
2. Klik "New Pipeline" → buka canvas kosong
3. Drag node dari palette ke canvas
4. Hubungkan node dengan tarik arrow dari output ke input
5. Klik node → isi config di panel kanan (connection, SQL, table name, dll)
6. Klik "Validate" → backend cek syntax + koneksi
7. Klik "Run" → pipeline dieksekusi async, status update via polling/SSE
8. Lihat run history + log per node di drawer bawah

---

## Keputusan Arsitektur yang Perlu Didiskusikan

### 1. Transform Node: In-Memory vs SQLite Temp
- **In-memory Go slice**: simpel, cukup untuk data kecil-medium (<100k rows)
- **SQLite in-process**: bisa jalankan SQL transform yang kompleks, tapi tambah dependency
- **Proposal**: mulai dengan in-memory, apply transform SQL sebagai filter Go. Batasi 50k rows untuk fase awal.

### 2. S3 Credentials
- **Opsi A**: Simpan di `config` kolom node (JSONB, dienkripsi per field) — isolasi per pipeline
- **Opsi B**: Tambah S3 sebagai tipe koneksi baru (reuse connection management + encryption) — lebih reusable, user bisa pakai ulang koneksi S3 di banyak pipeline
- **Proposal**: Opsi B lebih bersih. Tambah `driver = 's3'` ke connection model.

### 3. Eksekusi: Sync vs Async
- Untuk pipeline panjang, sync (blocking HTTP) tidak cocok
- **Proposal**: Async — `POST /run` return `run_id` langsung, eksekusi di goroutine. Frontend polling `GET /runs/{id}` atau SSE untuk status realtime.

### 4. Large Dataset
- Memuat seluruh result set ke memory bisa bermasalah untuk jutaan rows
- **Proposal fase awal**: tambahkan hard limit (e.g. 500k rows) + warning di UI
- **Fase lanjut**: streaming cursor — baca source row-by-row, langsung tulis ke sink tanpa buffer penuh

### 5. Vue Flow vs Custom Canvas
- Vue Flow adalah library yang sudah mature, MIT license, cocok untuk Vue 3
- Alternatif: custom SVG canvas (lebih kontrol, tapi costly)
- **Proposal**: Vue Flow untuk fase awal

---

## Fase Implementasi

### Phase 1 — Core: Table → SQL → Table
- [ ] Migrasi DB: `pipelines`, `pipeline_nodes`, `pipeline_edges`, `pipeline_runs`, `pipeline_run_logs`
- [ ] Backend: CRUD pipeline + nodes + edges (`handlers/pipelines.go`)
- [ ] Backend: Executor async (`handlers/pipeline_executor.go`) — hanya node `source_query`, `sink_table`
- [ ] Frontend: List pipelines + canvas editor (Vue Flow) + node palette + run button
- [ ] Frontend: Node config panel + run history drawer
- [ ] Permissions: `pipelines.view`, `pipelines.manage`, `pipelines.run`

### Phase 2 — Export Sinks
- [ ] Backend: `sink_export` (CSV, JSON, Excel) — reuse logic dari `analytics_dashboards.go`
- [ ] Backend: `sink_s3` — AWS SDK, S3 sebagai connection type baru
- [ ] Frontend: Export node config, S3 node config

### Phase 3 — Transform Node + Schedule
- [ ] Backend: `transform_sql` node — apply SQL filter/aggregation di memory
- [ ] Backend: Schedule trigger — tambah `pipeline_id` ke tabel `schedules` atau cron field di pipelines
- [ ] Frontend: Transform node SQL editor, schedule config di pipeline settings
- [ ] Frontend: SSE progress stream per run (live log per node)

### Phase 4 — Advanced (Opsional)
- [ ] Streaming cursor untuk large dataset (tanpa full in-memory buffer)
- [ ] Preview output per node (dry-run N rows)
- [ ] `source_table` node sebagai shortcut (auto `SELECT * FROM table`)
- [ ] Multi-branch (fan-out): satu source ke beberapa sink

---

## File yang Akan Dibuat / Dimodifikasi

| File | Aksi | Catatan |
|---|---|---|
| `server/handlers/pipelines.go` | Buat baru | CRUD + run trigger |
| `server/handlers/pipeline_executor.go` | Buat baru | Async execution engine |
| `server/handlers/models.go` | Edit | Tambah permission constants |
| `server/main.go` | Edit | Register routes baru |
| `server/db/db.go` | Edit | Tambah migrasi tabel baru |
| `web/src/views/DataPipelinesView.vue` | Buat baru | Main view |
| `web/src/composables/usePipelines.ts` | Buat baru | API composable |
| `web/src/router/index.ts` | Edit | Tambah route `/data-pipelines` |
| `docs/on-going/data-pipeline-class-tree.md` | Buat baru | Dokumentasi class tree |

---

## Dependensi Baru

| Package | Tujuan | Catatan |
|---|---|---|
| `github.com/aws/aws-sdk-go-v2/service/s3` | Upload ke S3 | Hanya untuk Phase 2 |
| `@vue-flow/core` | Canvas drag-and-drop | Frontend, MIT license |
| `@vue-flow/background` | Grid background canvas | Frontend addon |
| `@vue-flow/controls` | Zoom/pan controls | Frontend addon |
