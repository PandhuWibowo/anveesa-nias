# Schema Management — Class Tree

## Backend

### GET /api/connections/{id}/er
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermERView)
    └── handlers.GetERDiagram()                                       [handlers/er.go]
        ├── parseIDFromPath()
        ├── appdb.DB.QueryRow(SELECT database FROM connections)       [db/db.go — fallback db name]
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── cache.Default().Get(r.Context(), "er:{driver}:{id}:{db}") [cache/]
        ├── [postgres] db.Query(information_schema.tables, tables=public) [user DB]
        │   ├── per table: db.Query(information_schema.columns + key_column_usage) [user DB]
        │   └── db.Query(information_schema.table_constraints + key_column_usage + constraint_column_usage) [user DB — FKs]
        ├── [mysql] db.Query(information_schema.TABLES)               [user DB]
        │   ├── per table: db.Query(information_schema.COLUMNS)       [user DB]
        │   └── db.Query(information_schema.KEY_COLUMN_USAGE WHERE REFERENCED_TABLE_NAME IS NOT NULL) [user DB — FKs]
        ├── cache.Default().Set(..., 90*time.Second)                  [cache/]
        └── w.Write(json)
```

### GET /api/diff
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermSchemaDiffView)
    └── handlers.GetSchemaDiff()                                      [handlers/diff.go]
        ├── r.URL.Query() — conn_a, conn_b, db_a, db_b
        ├── GetDB(connA), GetDB(connB)                                [handlers/pool.go]
        ├── diffFetchSchema(dbADB, driverA, dbA)                      [handlers/diff.go]
        │   ├── db.Query(SELECT table_name FROM information_schema.tables) [user DB]
        │   └── per table: diffFetchColumns(db, driver, dbName, table)
        │       └── db.Query(SELECT column_name, data_type FROM information_schema) [user DB]
        ├── diffFetchSchema(dbBDB, driverB, dbB)
        ├── diffCompareColumns(colsA, colsB)                          [handlers/diff.go]
        ├── diffSort(diffs)                                           [handlers/diff.go]
        └── json.NewEncoder(w).Encode(SchemaDiffResult)
```

### POST /api/connections/{id}/schema/tables
```
└── Middleware: mw.InjectUserContext → mw.RequireDbPermission(DbPermCreate)
    └── handlers.CreateTable()                                        [handlers/schema_editor.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── quoteIdent(driver, col)                                   [handlers/schema.go]
        ├── qualifiedTableName(driver, dbName, tableName)             [handlers/schema.go]
        ├── db.ExecContext(CREATE TABLE ... (...))                    [user DB]
        └── json.NewEncoder(w).Encode({ok, ddl})
```

### PATCH /api/connections/{id}/schema/{table}
```
└── Middleware: mw.InjectUserContext → mw.RequireDbPermission(DbPermAlter)
    └── handlers.RenameTable()                                        [handlers/schema_editor.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── [mysql]     db.ExecContext(RENAME TABLE ...)              [user DB]
        ├── [sqlserver] db.ExecContext(EXEC sp_rename ...)            [user DB]
        ├── [postgres]  db.ExecContext(ALTER TABLE ... RENAME TO ...) [user DB]
        └── json.NewEncoder(w).Encode({ok})
```

### DELETE /api/connections/{id}/schema/{table}
```
└── Middleware: mw.InjectUserContext → mw.RequireDbPermission(DbPermDrop)
    └── handlers.DropTable()                                          [handlers/schema_editor.go]
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── qualifiedTableName(driver, dbName, tableName)
        ├── db.ExecContext(DROP TABLE IF EXISTS ...)                  [user DB]
        └── w.WriteHeader(http.StatusNoContent)
```

### POST /api/connections/{id}/schema/{table}/columns
```
└── Middleware: mw.InjectUserContext → mw.RequireDbPermission(DbPermAlter)
    └── handlers.AddColumn()                                          [handlers/schema_editor.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── quoteIdent(), qualifiedTableName()
        ├── db.ExecContext(ALTER TABLE ... ADD COLUMN ...)            [user DB]
        └── json.NewEncoder(w).Encode({ok})
```

### DELETE /api/connections/{id}/schema/{table}/columns/{col}
```
└── Middleware: mw.InjectUserContext → mw.RequireDbPermission(DbPermAlter)
    └── handlers.DropColumn()                                         [handlers/schema_editor.go]
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── quoteIdent(), qualifiedTableName()
        ├── db.ExecContext(ALTER TABLE ... DROP COLUMN ...)           [user DB]
        └── w.WriteHeader(http.StatusNoContent)
```

### GET /api/connections/{id}/schema/{table}/metadata
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermSchemaBrowse)
    └── handlers.ListSchemaMetadata()                                 [handlers/schema_metadata.go]
        ├── schemaTargetFromPath(r.URL.Path)
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── [postgres] db.Query(pg_tables, pg_views, pg_proc, pg_type, information_schema.sequences, pg_trigger) [user DB]
        ├── [mysql]    db.Query(information_schema.TABLES, ROUTINES, TRIGGERS) [user DB]
        └── json.NewEncoder(w).Encode(SchemaMetadataCatalog)
```

### GET /api/connections/{id}/schema/{table}/object-detail
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermSchemaBrowse)
    └── handlers.GetSchemaObjectDetail()                              [handlers/schema_metadata.go]
        ├── schemaTargetFromPath(r.URL.Path)
        ├── r.URL.Query().Get("type"), r.URL.Query().Get("name")
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── [type=table]    fetchTableDetail(db, driver, dbName, name) [handlers/schema_metadata.go]
        │   ├── db.Query(information_schema.columns)                  [user DB]
        │   ├── db.Query(pg_indexes / SHOW INDEX)                     [user DB — indexes]
        │   ├── db.Query(information_schema.table_constraints)        [user DB — constraints]
        │   └── db.Query(triggers)                                    [user DB]
        ├── [type=view]     fetchViewDetail(db, driver, dbName, name) [handlers/schema_metadata.go]
        ├── [type=function] fetchRoutineDetail(...)                   [handlers/schema_metadata.go]
        └── json.NewEncoder(w).Encode(SchemaObjectDetail)
```

### GET /api/connections/{id}/databases
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermSchemaBrowse)
    └── handlers.ListDatabases()                                      [handlers/databases.go]
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── [postgres]  db.Query(SELECT datname FROM pg_database)     [user DB]
        ├── [mysql]     db.Query(SHOW DATABASES)                      [user DB]
        └── json.NewEncoder(w).Encode()
```

---

## Frontend

### Route: /er
```
router/index.ts
└── { path: 'er', meta: { requiredPermissionsAny: ['er.view'] } }
    └── views/ERDiagramView.vue                                       (23 KB)
        ├── useConnections()                                          [composables/useConnections.ts]
        ├── useForeignKeys()                                          [composables/useForeignKeys.ts]
        │   └── axios.get('/api/connections/{id}/er/{db}')
        └── SVG rendering logic (inline in view)
```

### Route: /diff
```
router/index.ts
└── { path: 'diff', meta: { requiredPermissionsAny: ['schema.diff.view'] } }
    └── views/SchemaDiffView.vue                                      (10 KB)
        ├── useConnections()
        └── axios.get('/api/diff?conn_a=...&conn_b=...&db_a=...&db_b=...')
```

### Route: /data (schema panel inside SQL Studio)
```
└── views/DataView.vue
    └── useSchema()                                                   [composables/useSchema.ts]
        ├── axios.get('/api/connections/{id}/schema')
        ├── axios.get('/api/connections/{id}/schema/{db}/tables/{tbl}/columns')
        ├── axios.get('/api/connections/{id}/schema/{db}/metadata')
        └── axios.get('/api/connections/{id}/schema/{db}/object-detail?type=...&name=...')
```

### Route: /schema (redirects to /data)
```
router/index.ts
└── { path: 'schema', redirect: { name: 'data' } }
```
