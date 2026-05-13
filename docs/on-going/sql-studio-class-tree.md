# SQL Studio — Class Tree

## Backend

### POST /api/connections/{id}/query
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermQueryExecute)
              → mw.RequireDbPermission(DbPermSelect|write perms)
    └── handlers.ExecuteQuery()                                       [handlers/query.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── CheckWritePermission(r, connID)                           [handlers/models.go]
        ├── currentUserFromHeaders(r)                                 [handlers/auth.go]
        ├── findApplicableWorkflows(userID, role, connID)             [handlers/workflow_approval.go]
        │   └── appdb.DB.Query(SELECT FROM workflows)                 [db/db.go]
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── GetActiveTx(connID)                                       [handlers/transaction.go]
        ├── activeTx.QueryContext() / db.QueryContext()               [user DB — SELECT]
        ├── activeTx.ExecContext() / db.ExecContext()                 [user DB — DML]
        ├── go WriteAuditLog(username, connID, ...)                   [handlers/audit.go — async]
        │   └── appdb.DB.Exec(INSERT INTO audit_log ...)              [db/db.go]
        └── json.NewEncoder(w).Encode()
```

### POST /api/connections/{id}/query/stream
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermQueryExecute)
    └── handlers.StreamQuery()                                        [handlers/stream.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── db.QueryContext(ctx, req.SQL)                             [user DB]
        ├── sendSSE(w, StreamMeta{Columns})                           [handlers/stream.go]
        ├── sendSSE(w, StreamRow{Row}) — per row
        ├── flushSSE(w)                                               [handlers/stream.go]
        └── sendSSE(w, StreamDone{...})
```

### POST /api/connections/{id}/explain
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermQueryExecute)
    └── handlers.ExplainQuery()                                       [handlers/explain.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── [postgres] db.QueryContext("EXPLAIN (ANALYZE false, FORMAT JSON) "+sql) [user DB]
        ├── [mysql]    db.QueryContext("EXPLAIN FORMAT=JSON "+sql)    [user DB]
        ├── [default]  db.QueryContext("EXPLAIN "+sql)                [user DB]
        └── json.NewEncoder(w).Encode()
```

### GET /api/connections/{id}/schema
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermSchemaBrowse)
    └── handlers.GetSchema()                                          [handlers/schema.go]
        ├── cachedJSONResponse(w, r, "schema:list:{id}", 2min, ...)   [handlers/cache_helpers.go]
        │   ├── cache.Default().Get()                                 [cache/]
        │   ├── GetDB(connID)                                         [handlers/pool.go]
        │   ├── [postgres] db.Query(information_schema.tables)        [user DB]
        │   ├── [mysql]    db.Query(information_schema.TABLES)        [user DB]
        │   ├── [sqlserver] db.Query(INFORMATION_SCHEMA.TABLES)       [user DB]
        │   └── cache.Default().Set()
        └── json response (cached or fresh)
```

### GET /api/connections/{id}/schema/{table}/data
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermSQLStudioAccess)
    └── handlers.GetTableData()                                       [handlers/schema.go]
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── db.QueryRow(COUNT(*))                                     [user DB]
        ├── db.QueryContext(SELECT * FROM ... LIMIT ? OFFSET ?)       [user DB]
        └── json.NewEncoder(w).Encode({columns, rows, total_rows, page, page_size})
```

### GET /api/connections/{id}/schema/{table}/columns
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermSchemaBrowse)
    └── handlers.GetTableColumns()                                    [handlers/schema.go]
        ├── cachedJSONResponse(w, r, "schema:columns:{id}:{db}:{tbl}", 2min, ...)
        │   ├── GetDB(connID)                                         [handlers/pool.go]
        │   ├── [postgres] db.Query(information_schema.columns + key_column_usage) [user DB]
        │   ├── [mysql]    db.Query(information_schema.COLUMNS)       [user DB]
        │   └── [sqlserver] db.Query(INFORMATION_SCHEMA.COLUMNS)      [user DB]
        └── json response (cached or fresh)
```

### POST /api/connections/{id}/schema/{table}/rows
```
└── Middleware: mw.InjectUserContext → mw.RequireDbPermission(DbPermInsert)
    └── handlers.InsertRow()                                          [handlers/edit.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── quoteIdent(driver, col)                                   [handlers/schema.go]
        ├── qualifiedTableName(driver, dbName, tableName)             [handlers/schema.go]
        ├── db.ExecContext(INSERT INTO ... VALUES ...)                [user DB]
        └── json.NewEncoder(w).Encode()
```

### PUT /api/connections/{id}/schema/{table}/rows
```
└── Middleware: mw.InjectUserContext → mw.RequireDbPermission(DbPermUpdate)
    └── handlers.UpdateRow()                                          [handlers/edit.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── quoteIdent(), qualifiedTableName()
        ├── db.ExecContext(UPDATE ... SET ... WHERE pk=?)             [user DB]
        └── json.NewEncoder(w).Encode({affected_rows})
```

### DELETE /api/connections/{id}/schema/{table}/rows
```
└── Middleware: mw.InjectUserContext → mw.RequireDbPermission(DbPermDelete)
    └── handlers.DeleteRow()                                          [handlers/edit.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── quoteIdent(), qualifiedTableName()
        ├── db.ExecContext(DELETE FROM ... WHERE pk=?)                [user DB]
        └── json.NewEncoder(w).Encode({affected_rows})
```

### GET /api/connections/{id}/history
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermQueryExecute)
    └── handlers.GetHistory()                                         [handlers/history.go]
        ├── appdb.DB.Query(SELECT FROM query_history WHERE conn_id=? ORDER BY executed_at DESC LIMIT 200) [db/db.go]
        └── json.NewEncoder(w).Encode()
```

### POST /api/connections/{id}/history
```
└── Middleware: mw.InjectUserContext
    └── handlers.SaveHistory()                                        [handlers/history.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── appdb.DB.Exec(INSERT INTO query_history ...)              [db/db.go]
        └── appdb.DB.Exec(DELETE old rows beyond 500)                 [db/db.go]
```

### DELETE /api/connections/{id}/history
```
└── Middleware: mw.InjectUserContext
    └── handlers.ClearHistory()                                       [handlers/history.go]
        └── appdb.DB.Exec(DELETE FROM query_history WHERE conn_id=?) [db/db.go]
```

### GET/POST /api/saved-queries
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermSavedQueriesManage)
    ├── handlers.ListSavedQueries()                                   [handlers/saved_queries.go]
    │   ├── isAuthEnabled(), role check
    │   └── appdb.DB.Query(SELECT FROM saved_queries)                 [db/db.go]
    └── handlers.CreateSavedQuery()                                   [handlers/saved_queries.go]
        ├── json.NewDecoder(r.Body).Decode()
        └── appdb.DB.QueryRow/Exec(INSERT INTO saved_queries ...)     [db/db.go]
```

### PUT/DELETE /api/saved-queries/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermSavedQueriesManage)
    ├── handlers.UpdateSavedQuery()                                   [handlers/saved_queries.go]
    │   ├── ownership check → appdb.DB.QueryRow(SELECT user_id)       [db/db.go]
    │   └── appdb.DB.Exec(UPDATE saved_queries SET ...)               [db/db.go]
    └── handlers.DeleteSavedQuery()                                   [handlers/saved_queries.go]
        ├── ownership check → appdb.DB.QueryRow(SELECT user_id)       [db/db.go]
        └── appdb.DB.Exec(DELETE FROM saved_queries WHERE id=?)       [db/db.go]
```

### GET/POST /api/snippets
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermSnippetsManage)
    ├── handlers.ListSnippets()                                       [handlers/snippets.go]
    │   └── appdb.DB.Query(SELECT FROM snippets WHERE name/tags LIKE ?) [db/db.go]
    └── handlers.CreateSnippet()                                      [handlers/snippets.go]
        └── appdb.DB.Exec(INSERT INTO snippets ...)                   [db/db.go]
```

### PUT/DELETE /api/snippets/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermSnippetsManage)
    ├── handlers.UpdateSnippet()                                      [handlers/snippets.go]
    │   └── appdb.DB.Exec(UPDATE snippets SET ...)                    [db/db.go]
    └── handlers.DeleteSnippet()                                      [handlers/snippets.go]
        └── appdb.DB.Exec(DELETE FROM snippets WHERE id=?)            [db/db.go]
```

### POST /api/connections/{id}/script
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermQueryExecute)
    └── handlers.RunScript()                                          [handlers/multi_exec.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── splitStatements(req.SQL)                                  [handlers/multi_exec.go]
        ├── CheckWritePermission(r, connID)
        ├── findApplicableWorkflows(userID, role, connID)             [handlers/workflow_approval.go]
        ├── GetDB(connID)                                             [handlers/pool.go]
        └── per statement: db.QueryContext() / db.ExecContext()       [user DB]
```

### POST /api/connections/{id}/schema/{table}/import
```
└── Middleware: mw.InjectUserContext → mw.RequireDbPermission(DbPermInsert)
    └── handlers.ImportRows()                                         [handlers/import.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── quoteIdent(), qualifiedTableName()
        ├── db.BeginTx()                                              [user DB]
        ├── tx.PrepareContext(INSERT INTO ...)
        ├── prepared.ExecContext() — per row
        └── tx.Commit()
```

### GET/POST /api/connections/{id}/row-history
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermRowHistoryView)
    ├── handlers.ListRowHistory()                                     [handlers/row_history.go]
    │   └── appdb.DB.Query(SELECT FROM row_changes WHERE conn_id=?)   [db/db.go]
    └── handlers.UndoRowChange()                                      [handlers/row_history.go]
        ├── appdb.DB.QueryRow(SELECT FROM row_changes WHERE id=?)     [db/db.go]
        ├── GetDB(connID)                                             [handlers/pool.go]
        └── db.ExecContext(DELETE/INSERT/UPDATE to undo change)       [user DB]
```

### GET /api/connections/{id}/dashboard
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermOperationsView)
    └── handlers.GetDashboard()                                       [handlers/dashboard.go]
        ├── appdb.DB.QueryRow(SELECT driver FROM connections)         [db/db.go]
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── loadSlowQueries(connID)                                   [handlers/dashboard.go]
        │   └── appdb.DB.Query(SELECT FROM query_history WHERE duration_ms >= 1000) [db/db.go]
        ├── [postgres] db.QueryRow(SELECT current_database(), version(), pg_database_size) [user DB]
        ├── [postgres] db.Query(SELECT FROM pg_stat_user_tables)      [user DB]
        ├── [mysql]    db.QueryRow(SELECT DATABASE(), VERSION(), SUM(DATA_LENGTH)) [user DB]
        ├── [mysql]    db.Query(SELECT FROM information_schema.TABLES) [user DB]
        ├── [sqlserver] db.QueryRow(SELECT DB_NAME(), @@VERSION)      [user DB]
        └── json.NewEncoder(w).Encode()
```

### Transaction endpoints: /api/connections/{id}/tx/begin|commit|rollback|status
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermQueryExecute)
    ├── handlers.BeginTransaction()                                   [handlers/transaction.go]
    │   ├── GetDB(connID)                                             [handlers/pool.go]
    │   ├── db.BeginTx()                                              [user DB]
    │   └── txPool.txs[connID] = &txEntry{tx, driver}
    ├── handlers.CommitTransaction()                                  [handlers/transaction.go]
    │   ├── delete(txPool.txs, connID)
    │   └── entry.tx.Commit()                                         [user DB]
    ├── handlers.RollbackTransaction()                                [handlers/transaction.go]
    │   ├── delete(txPool.txs, connID)
    │   └── entry.tx.Rollback()                                       [user DB]
    └── handlers.TxStatus()                                           [handlers/transaction.go]
        └── txPool.RLock() → check txPool.txs[connID]
```

---

## Frontend

### Route: /data (SQL Studio)
```
router/index.ts
└── { path: 'data', meta: { requiredPermissionsAny: ['sqlstudio.access'] } }
    └── views/DataView.vue                                            (30–56 KB)
        ├── useQuery()                                                [composables/useQuery.ts]
        │   ├── axios.post('/api/connections/{id}/query', { sql })
        │   ├── axios.post('/api/connections/{id}/history', {...})
        │   ├── axios.get('/api/connections/{id}/history')
        │   ├── axios.delete('/api/connections/{id}/history')
        │   └── axios.post('/api/connections/{id}/query', { sql: 'EXPLAIN ...' })
        ├── useSchema()                                               [composables/useSchema.ts]
        │   ├── axios.get('/api/connections/{id}/schema')
        │   ├── axios.get('/api/connections/{id}/schema/{db}/tables/{tbl}/columns')
        │   ├── axios.get('/api/connections/{id}/schema/{db}/tables/{tbl}/data')
        │   └── axios.get('/api/connections/{id}/schema/{db}/metadata')
        ├── useSavedQueries()                                         [composables/useSavedQueries.ts]
        │   └── axios.get/post/put/delete('/api/saved-queries')
        ├── useConnections()                                          [composables/useConnections.ts]
        ├── fetch('/api/connections/{id}/query/stream')               [inline SSE fetch]
        ├── axios.post('/api/connections/{id}/explain')
        ├── axios.post('/api/connections/{id}/script')
        ├── axios.post('/api/connections/{id}/schema/{tbl}/rows')     [insert]
        ├── axios.put('/api/connections/{id}/schema/{tbl}/rows')      [update]
        ├── axios.delete('/api/connections/{id}/schema/{tbl}/rows')   [delete]
        └── axios.post('/api/connections/{id}/schema/{tbl}/import')
```

### Route: /saved-queries
```
router/index.ts
└── { path: 'saved-queries', meta: { requiredPermissionsAny: ['savedqueries.manage'] } }
    └── views/SavedQueriesView.vue
        ├── axios.get('/api/saved-queries')
        ├── axios.post('/api/saved-queries', body)
        ├── axios.put('/api/saved-queries/{id}', body)
        └── axios.delete('/api/saved-queries/{id}')
```

### Route: /row-history
```
router/index.ts
└── { path: 'row-history', meta: { requiredPermissionsAny: ['rowhistory.view'] } }
    └── views/RowHistoryView.vue
        ├── axios.get('/api/connections/{id}/row-history')
        └── axios.post('/api/connections/{id}/row-history')           [undo]
```
