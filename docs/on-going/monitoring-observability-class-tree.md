# Monitoring & Observability — Class Tree

## Backend

### GET /api/admin/audit
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermAuditView)
    └── handlers.ListAuditLog()                                       [handlers/audit.go]
        ├── r.Header.Get("X-User-Role"), r.Header.Get("X-Username")
        ├── query filters: event_type, conn_id, since_hours, has_error, min_duration_ms, q
        ├── appdb.DB.Query(SELECT FROM audit_log WHERE ... ORDER BY id DESC LIMIT ?) [db/db.go]
        └── json.NewEncoder(w).Encode()
```

### DELETE /api/admin/audit
```
└── Middleware: mw.InjectUserContext → admin only
    └── handlers.ClearAuditLog()                                      [handlers/audit.go]
        └── appdb.DB.Exec(DELETE FROM audit_log)                      [db/db.go]
```

### GET /api/admin/audit/stats
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermAuditView)
    └── handlers.GetAuditStats()                                      [handlers/audit.go]
        ├── appdb.DB.QueryRow(SELECT COUNT(*), COUNT(errors), AVG(duration_ms) FROM audit_log) [db/db.go]
        ├── appdb.DB.QueryRow(SELECT COUNT(*) WHERE event_type='query_execution')
        ├── appdb.DB.QueryRow(SELECT COUNT(*) WHERE event_type='feature_access')
        └── json.NewEncoder(w).Encode({total, errors, avg_ms, query_count, feature_count})
```

### GET /api/query-performance/native
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermPerformanceView)
    └── handlers.ListNativeQueryPerformance()                         [handlers/query_performance_native.go]
        ├── listAccessibleConnectionSummaries(r)                      [handlers/query_performance_native.go]
        │   └── appdb.DB.Query(SELECT id, name, driver FROM connections) [db/db.go]
        ├── per conn: loadNativeStatsForConnection(r, conn, limit)    [handlers/query_performance_native.go]
        │   ├── GetDB(conn.ID)                                        [handlers/pool.go]
        │   ├── [postgres] db.QueryContext(SELECT FROM pg_stat_statements) [user DB]
        │   └── [mysql]    db.QueryContext(SELECT FROM performance_schema.events_statements_summary_by_digest) [user DB]
        └── json.NewEncoder(w).Encode(NativeQueryPerformanceResponse)
```

### GET /api/database-audit/native
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermDatabaseAuditView)
    └── handlers.ListNativeDatabaseAudit()                            [handlers/database_audit.go]
        ├── listAccessibleConnectionSummaries(r)
        ├── per conn: loadNativeAuditForConnection(r, conn)           [handlers/database_audit.go]
        │   ├── GetDB(conn.ID)                                        [handlers/pool.go]
        │   ├── [postgres] loadPostgresAuditSessions(r, db, conn)     [handlers/database_audit.go]
        │   │   └── db.QueryContext(SELECT FROM pg_stat_activity)     [user DB]
        │   └── [mysql]    loadMySQLAuditSessions(r, db, conn)        [handlers/database_audit.go]
        │       └── db.QueryContext(SELECT FROM information_schema.PROCESSLIST) [user DB]
        └── json.NewEncoder(w).Encode(NativeAuditResponse)
```

### GET /api/database-audit/history/native
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermDatabaseAuditView)
    └── handlers.ListNativeDatabaseAuditHistory()                     [handlers/database_audit_history.go]
        ├── listAccessibleConnectionSummaries(r)
        ├── per conn: GetDB(conn.ID)                                  [handlers/pool.go]
        ├── [postgres] db.QueryContext(SELECT FROM pg_log / pgaudit)  [user DB]
        ├── [mysql]    db.QueryContext(SELECT FROM mysql.general_log) [user DB]
        └── json.NewEncoder(w).Encode()
```

### POST /api/audit/access
```
└── (No special middleware — any authenticated user)
    └── handlers.LogFeatureAccess()                                   [handlers/audit.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── WriteFeatureAccessAudit(username, action, target, details) [handlers/audit.go]
        └── writeAuditEvent("feature_access", ...)                    [handlers/audit.go]
            └── appdb.DB.Exec(INSERT INTO audit_log ...)              [db/db.go]
```

### POST /api/connections/{id}/profile
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermOperationsView)
    └── handlers.ProfileColumn()                                      [handlers/profiler.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── db.QueryContext(SELECT COUNT(*), COUNT(DISTINCT col), COUNT(*) WHERE col IS NULL) [user DB]
        ├── db.QueryContext(SELECT MIN, MAX, AVG — numeric stats)     [user DB]
        ├── db.QueryContext(SELECT col, COUNT(*) GROUP BY col ORDER BY COUNT(*) DESC LIMIT 10) [user DB]
        └── json.NewEncoder(w).Encode(ColumnProfileResult)
```

### GET /api/health
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermHealthView)
    └── handlers.PingAllConnections()                                 [handlers/health.go]
        ├── appdb.DB.Query(SELECT id, name, driver FROM connections)  [db/db.go]
        ├── per conn goroutine: pingConn(connID)                      [handlers/health.go]
        │   ├── appdb.DB.QueryRow(SELECT name, driver FROM connections) [db/db.go]
        │   ├── GetDB(connID)                                         [handlers/pool.go]
        │   ├── db.Ping()                                             [user DB]
        │   ├── db.QueryRow("SELECT 1")                               [user DB]
        │   └── db.Stats()
        └── json.NewEncoder(w).Encode([]HealthResult)
```

### GET/POST /api/schedules
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermSchedulesManage)
    ├── handlers.ListSchedules()                                      [handlers/scheduler.go]
    │   └── appdb.DB.Query(SELECT FROM schedules ORDER BY id)         [db/db.go]
    └── handlers.CreateSchedule()                                     [handlers/scheduler.go]
        ├── json.NewDecoder(r.Body).Decode()
        └── appdb.DB.Exec(INSERT INTO schedules ...)                  [db/db.go]
```

### PUT/DELETE /api/schedules/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermSchedulesManage)
    ├── handlers.UpdateSchedule()                                     [handlers/scheduler.go]
    │   └── appdb.DB.Exec(UPDATE schedules SET ...)                   [db/db.go]
    └── handlers.DeleteSchedule()                                     [handlers/scheduler.go]
        └── appdb.DB.Exec(DELETE FROM schedules WHERE id=?)           [db/db.go]
```

### POST /api/schedules/{id}/run
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermSchedulesManage)
    └── handlers.RunScheduleNow()                                     [handlers/scheduler.go]
        ├── appdb.DB.QueryRow(SELECT FROM schedules WHERE id=?)       [db/db.go]
        ├── GetDB(schedule.ConnID)                                    [handlers/pool.go]
        ├── db.QueryContext(schedule.SQL)                             [user DB]
        ├── appdb.DB.Exec(INSERT INTO schedule_runs ...)              [db/db.go]
        └── [if alert condition met] EmitNotificationEvent(...)       [handlers/notifications.go]
```

### GET /api/schedules/{id}/runs
```
└── handlers.GetScheduleRuns()                                        [handlers/scheduler.go]
    └── appdb.DB.Query(SELECT FROM schedule_runs WHERE schedule_id=?) [db/db.go]
```

### GET /api/connections/{id}/backup
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermBackupsManage)
    └── handlers.GetBackup()                                          [handlers/backup.go]
        ├── CheckReadPermission(r, connID)
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── writeBackupDump(ctx, w, db, driver, dbName)               [handlers/backup.go]
        │   ├── [postgres] db.Query(SELECT tablename FROM pg_tables)  [user DB]
        │   │   └── per table: db.Query(SELECT * FROM ...) → stream INSERT statements
        │   └── [mysql]    db.Query(SHOW TABLES)                      [user DB]
        │       └── per table: db.Query(SELECT * FROM ...) → stream INSERT statements
        └── w.Header streaming response (Content-Disposition: attachment)
```

### POST /api/connections/{id}/restore
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermBackupsManage)
    └── handlers.RestoreBackup()                                      [handlers/backup.go]
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── read multipart SQL file
        ├── splitStatements(sql)                                      [handlers/multi_exec.go]
        ├── isAllowedRestoreStatement(stmt)                           [handlers/backup.go]
        └── db.ExecContext(stmt) — per allowed statement              [user DB]
```

### GET/POST /api/backup-download-requests
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermBackupsManage)
    ├── handlers.ListBackupDownloadRequests()                         [handlers/backup_requests.go]
    │   └── appdb.DB.Query(SELECT FROM backup_download_requests)      [db/db.go]
    └── handlers.CreateBackupDownloadRequestHandler()                 [handlers/backup_requests.go]
        ├── json.NewDecoder(r.Body).Decode()
        └── appdb.DB.Exec(INSERT INTO backup_download_requests ...)   [db/db.go]
```

### POST /api/backup-download-requests/{id}/review
```
└── handlers.ReviewBackupDownloadRequestHandler()                     [handlers/backup_requests.go]
    ├── currentUserFromHeaders(r)
    └── appdb.DB.Exec(UPDATE backup_download_requests SET status=?)   [db/db.go]
```

### GET /api/backup-download-requests/{id}/download
```
└── handlers.DownloadApprovedBackupRequest()                          [handlers/backup_requests.go]
    ├── appdb.DB.QueryRow(SELECT FROM backup_download_requests WHERE id=? AND status='approved') [db/db.go]
    ├── GetDB(req.ConnID)                                             [handlers/pool.go]
    └── writeBackupDump(ctx, w, db, driver, dbName)                   [handlers/backup.go]
```

### Internal: WriteAuditLog (called from ExecuteQuery)
```
handlers.WriteAuditLog(username, connID, connName, sql, durationMs, rowCount, errMsg) [handlers/audit.go]
└── writeAuditEvent("query_execution", ...)
    ├── appdb.DB.Exec(INSERT INTO audit_log ...)                      [db/db.go]
    └── go appdb.DB.Exec(DELETE old rows beyond 10000)                [db/db.go — async]
```

---

## Frontend

### Route: /audit
```
router/index.ts
└── { path: 'audit', meta: { requiredPermissionsAny: ['audit.view'] } }
    └── views/AuditLogView.vue                                        (16 KB)
        ├── axios.get('/api/admin/audit?limit=...&event_type=...&conn_id=...')
        ├── axios.get('/api/admin/audit/stats')
        ├── axios.delete('/api/admin/audit')
        └── useConnections()
```

### Route: /query-performance
```
router/index.ts
└── { path: 'query-performance', meta: { requiredPermissionsAny: ['performance.view'] } }
    └── views/QueryPerformanceView.vue                                (26 KB)
        └── axios.get('/api/query-performance/native?conn_id=...&limit=...')
```

### Route: /database-audit
```
router/index.ts
└── { path: 'database-audit', meta: { requiredPermissionsAny: ['databaseaudit.view'] } }
    └── views/DatabaseAuditView.vue                                   (15 KB)
        ├── axios.get('/api/database-audit/native?conn_id=...')
        └── axios.get('/api/database-audit/history/native?conn_id=...')
```

### Route: /health
```
router/index.ts
└── { path: 'health', meta: { requiredPermissionsAny: ['health.view'] } }
    └── views/HealthView.vue                                          (6.6 KB)
        └── axios.get('/api/health')
```

### Route: /scheduler
```
router/index.ts
└── { path: 'scheduler', meta: { requiredPermissionsAny: ['schedules.manage'] } }
    └── views/SchedulerView.vue                                       (16 KB)
        ├── axios.get('/api/schedules')
        ├── axios.post('/api/schedules', body)
        ├── axios.put('/api/schedules/{id}', body)
        ├── axios.delete('/api/schedules/{id}')
        ├── axios.post('/api/schedules/{id}/run')
        └── axios.get('/api/schedules/{id}/runs')
```

### Route: /backup
```
router/index.ts
└── { path: 'backup', meta: { requiredPermissionsAny: ['backups.manage', ...] } }
    └── views/BackupView.vue                                          (22 KB)
        ├── axios.get('/api/connections/{id}/backup?database=...')    [streaming download]
        ├── axios.post('/api/connections/{id}/restore')               [file upload]
        ├── axios.get('/api/backup-download-requests')
        ├── axios.post('/api/backup-download-requests', body)
        ├── axios.post('/api/backup-download-requests/{id}/review')
        └── axios.get('/api/backup-download-requests/{id}/download')
```

### Route: /dashboard (operations overview)
```
router/index.ts
└── { path: 'dashboard', meta: { requiredPermissionsAny: ['operations.view'] } }
    └── views/DashboardView.vue                                       (23 KB)
        ├── axios.get('/api/connections/{id}/dashboard')
        ├── axios.post('/api/connections/{id}/profile')
        └── useConnections()
```

### Router afterEach — auto audit all navigation
```
router.afterEach()                                                    [router/index.ts]
└── axios.post('/api/audit/access', { action: 'open_feature', target: routeName, details: fullPath })
```
