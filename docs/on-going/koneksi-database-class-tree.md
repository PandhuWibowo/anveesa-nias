# Koneksi Database — Class Tree

## Backend

### GET /api/connections
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermConnectionsView)
    └── handlers.ListConnections()                                    [handlers/connections.go]
        ├── r.Header.Get("X-User-ID"), r.Header.Get("X-User-Role")
        ├── isAuthEnabled()                                           [handlers/connections.go]
        ├── appdb.DB.Query()                                          [db/db.go — internal app DB]
        │   └── JOIN connection_folders, user_connections, folder_members
        └── json.NewEncoder(w).Encode()
```

### POST /api/connections
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermConnectionsCreate)
    └── handlers.CreateConnection()                                   [handlers/connections.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── validateConnectionInput()                                 [handlers/connections.go]
        ├── encryptCredential(password)                               [handlers/connections.go — AES-GCM]
        ├── encryptCredential(ssh_password)
        ├── encryptCredential(ssh_key)
        ├── appdb.DB.QueryRow(INSERT INTO connections ...)            [db/db.go — internal app DB]
        └── json.NewEncoder(w).Encode()
```

### GET /api/connections/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermConnectionsView)
    └── handlers.GetConnection()                                      [handlers/connections.go]
        ├── strconv.ParseInt(idStr)
        ├── appdb.DB.QueryRow(SELECT FROM connections WHERE id=?)     [db/db.go — internal app DB]
        ├── isAuthEnabled() + permission check
        ├── decryptCredential() — masks password as ••••••••
        └── json.NewEncoder(w).Encode()
```

### PUT /api/connections/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermConnectionsEdit)
    └── handlers.UpdateConnection()                                   [handlers/connections.go]
        ├── appdb.DB.QueryRow(SELECT owner_id FROM connections)       [db/db.go]
        ├── canModifyConnection(r, connID)                            [handlers/connections.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── validateConnectionInput()
        ├── encryptCredential() — re-encrypts if changed
        ├── appdb.DB.Exec(UPDATE connections SET ...)                 [db/db.go]
        ├── EvictFromPool(connID)                                     [handlers/pool.go]
        └── json.NewEncoder(w).Encode()
```

### DELETE /api/connections/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermConnectionsDelete)
    └── handlers.DeleteConnection()                                   [handlers/connections.go]
        ├── canModifyConnection(r, id)                                [handlers/connections.go]
        │   └── appdb.DB.QueryRow(SELECT owner_id FROM connections)   [db/db.go]
        ├── EvictFromPool(id)                                         [handlers/pool.go]
        └── appdb.DB.Exec(DELETE FROM connections WHERE id=?)         [db/db.go]
```

### PATCH /api/connections/{id}/folder
```
└── Middleware: mw.InjectUserContext
    └── handlers.UpdateConnectionFolder()                             [handlers/connections.go]
        ├── canModifyConnection(r, id)
        ├── json.NewDecoder(r.Body).Decode()
        ├── appdb.DB.Exec(UPDATE connections SET folder_id=?)         [db/db.go]
        └── appdb.DB.Exec(UPDATE connections SET visibility=?)        [db/db.go]
```

### PATCH /api/connections/{id}/visibility
```
└── Middleware: mw.InjectUserContext
    └── handlers.UpdateConnectionVisibility()                         [handlers/connections.go]
        ├── canModifyConnection(r, id)
        └── appdb.DB.Exec(UPDATE connections SET visibility=?)        [db/db.go]
```

### GET /api/connections/{id}/ping
```
└── Middleware: mw.InjectUserContext
    └── handlers.PingConnection()                                     [handlers/health.go]
        └── pingConn(connID)                                          [handlers/health.go]
            ├── appdb.DB.QueryRow(SELECT name, driver FROM connections) [db/db.go]
            ├── GetDB(connID)                                         [handlers/pool.go]
            │   └── openRemoteDB(connID)                              [handlers/connections.go]
            ├── db.Ping()                                             [user DB]
            └── db.Stats()
```

### POST /api/connections/{id}/disconnect
```
└── Middleware: mw.InjectUserContext
    └── handlers.DisconnectConnection()                               [handlers/connections.go]
        ├── appdb.DB.QueryRow(SELECT 1 FROM connections WHERE id=?)   [db/db.go]
        ├── appdb.DB.Exec(UPDATE connections SET disconnected=1)       [db/db.go]
        └── EvictFromPool(connID)                                     [handlers/pool.go]
```

### POST /api/connections/{id}/reconnect
```
└── Middleware: mw.InjectUserContext
    └── handlers.ReconnectConnection()                                [handlers/connections.go]
        ├── appdb.DB.QueryRow(SELECT 1 FROM connections WHERE id=?)   [db/db.go]
        └── appdb.DB.Exec(UPDATE connections SET disconnected=0)       [db/db.go]
```

### POST /api/connections/test
```
└── Middleware: mw.InjectUserContext
    └── handlers.TestConnection()                                     [handlers/connections.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── validateConnectionInput()
        ├── [redis] testRedisInput(ctx, in)                           [handlers/redis.go]
        ├── [kafka] readKafkaTopics(ctx, in)                          [handlers/kafka.go]
        ├── buildDSN(in)                                              [handlers/connections.go]
        ├── sql.Open(driverName, dsn)
        ├── db.SetConnMaxLifetime(10s)
        ├── db.Ping()
        └── json.NewEncoder(w).Encode()
```

### GET/POST /api/folders
```
└── Middleware: mw.InjectUserContext
    ├── handlers.ListFolders()                                        [handlers/folders.go]
    │   ├── r.Header.Get("X-User-Role"), r.Header.Get("X-User-ID")
    │   └── appdb.DB.Query(SELECT FROM connection_folders)            [db/db.go]
    └── handlers.CreateFolder()                                       [handlers/folders.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── appdb.DB.QueryRow(SELECT MAX(sort_order) FROM connection_folders) [db/db.go]
        └── appdb.DB.Exec(INSERT INTO connection_folders ...)         [db/db.go]
```

### PUT /api/folders/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermFoldersManage)
    └── handlers.UpdateFolder()                                       [handlers/folders.go]
        ├── canModifyFolder(r, id)                                    [handlers/folders.go]
        │   └── appdb.DB.QueryRow(SELECT owner_id FROM connection_folders) [db/db.go]
        └── appdb.DB.Exec(UPDATE connection_folders SET ...)          [db/db.go]
```

### DELETE /api/folders/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermFoldersManage)
    └── handlers.DeleteFolder()                                       [handlers/folders.go]
        ├── canModifyFolder(r, id)
        ├── appdb.DB.Exec(UPDATE connections SET folder_id=NULL)      [db/db.go]
        ├── appdb.DB.Exec(UPDATE connection_folders SET parent_id=NULL) [db/db.go]
        └── appdb.DB.Exec(DELETE FROM connection_folders WHERE id=?)  [db/db.go]
```

### GET /api/users/{id}/connections
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermUsersManage)
    └── handlers.GetUserConnections()                                 [handlers/rbac.go]
        ├── db.GetUserRole(userID)                                    [db/db.go]
        └── db.GetUserConnectionAssignments(userID, role)             [db/db.go]
```

### POST /api/users/{id}/connections
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermUsersManage)
    └── handlers.SetUserConnections()                                 [handlers/rbac.go]
        ├── json.NewDecoder(r.Body).Decode()
        └── db.SetUserDirectConnections(userID, connIDs, permsMap)    [db/db.go]
```

---

### Pool internals
```
handlers.GetDB(connID)                                                [handlers/pool.go]
├── dbPool.RLock() → check cache → dbPool.RUnlock()
├── entry.db.Ping() — evict on failure
└── openRemoteDB(connID)                                              [handlers/connections.go]
    ├── appdb.DB.QueryRow(SELECT driver,host,port... FROM connections) [db/db.go]
    ├── decryptCredential(encPassword)
    ├── buildDSN(in)
    └── sql.Open(goDriver, dsn)

handlers.EvictFromPool(connID)                                        [handlers/pool.go]
├── entry.db.Close()
├── entry.listener.Close()   (SSH tunnel)
└── entry.sshClient.Close()  (SSH tunnel)
```

---

## Frontend

### Route: /connections
```
router/index.ts
└── { path: 'connections', meta: { requiredPermissionsAny: ['connections.view'] } }
    └── views/ConnectionsView.vue
        ├── useConnections()                                          [composables/useConnections.ts]
        │   ├── axios.get('/api/connections')
        │   ├── axios.post('/api/connections', form)
        │   ├── axios.put('/api/connections/{id}', form)
        │   ├── axios.delete('/api/connections/{id}')
        │   ├── axios.post('/api/connections/{id}/disconnect')
        │   └── axios.post('/api/connections/{id}/reconnect')
        ├── axios.post('/api/connections/test')                       [inline in view]
        ├── axios.get('/api/connections/{id}')
        ├── axios.patch('/api/connections/{id}/folder')
        ├── axios.patch('/api/connections/{id}/visibility')
        ├── useFolders()                                              [composables/useFolders.ts]
        │   ├── axios.get('/api/folders')
        │   ├── axios.post('/api/folders', form)
        │   ├── axios.put('/api/folders/{id}', form)
        │   └── axios.delete('/api/folders/{id}')
        └── usePermissions()                                          [composables/usePermissions.ts]
```

### Router guard
```
router.beforeEach()                                                   [router/index.ts]
└── useAuth().hasAnyPermission(requiredPermissionsAny)

router.afterEach()                                                    [router/index.ts]
└── axios.post('/api/audit/access', { action, target, details })
```
