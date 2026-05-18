# Analytics & Dashboard — Class Tree

## Backend

### GET /api/analytics-dashboards
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermDashboardsManage)
    └── handlers.ListAnalyticsDashboards()                            [handlers/analytics_dashboards.go]
        ├── queryAnalyticsDashboardsForRequest(r)                     [handlers/analytics_dashboards.go]
        │   ├── currentUserFromHeaders(r)                             [handlers/auth.go]
        │   └── appdb.DB.Query(SELECT FROM analytics_dashboards)      [db/db.go — internal app DB]
        └── json.NewEncoder(w).Encode()
```

### POST /api/analytics-dashboards
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermDashboardsManage)
    └── handlers.CreateAnalyticsDashboard()                           [handlers/analytics_dashboards.go]
        ├── currentUserFromHeaders(r)
        ├── json.NewDecoder(r.Body).Decode()
        ├── insertRowReturningID(INSERT INTO analytics_dashboards ...) [db/db.go]
        └── getAnalyticsDashboardByID(id)                             [handlers/analytics_dashboards.go]
            └── appdb.DB.QueryRow(SELECT FROM analytics_dashboards)   [db/db.go]
```

### GET /api/analytics-dashboards/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermDashboardsManage)
    └── handlers.GetAnalyticsDashboard()                              [handlers/analytics_dashboards.go]
        ├── parseIDFromPath(r.URL.Path, "/api/analytics-dashboards/")
        ├── getAnalyticsDashboardByID(id)                             [handlers/analytics_dashboards.go]
        ├── canAccessAnalyticsDashboard(r, id)                        [handlers/analytics_dashboards.go]
        │   └── appdb.DB.QueryRow(SELECT FROM analytics_dashboard_access) [db/db.go]
        ├── listAnalyticsDashboardBlocks(id)                          [handlers/analytics_dashboards.go]
        │   └── appdb.DB.Query(SELECT FROM analytics_dashboard_blocks) [db/db.go]
        └── json.NewEncoder(w).Encode()
```

### PUT /api/analytics-dashboards/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermDashboardsManage)
    └── handlers.UpdateAnalyticsDashboard()                           [handlers/analytics_dashboards.go]
        ├── canManageAnalyticsDashboard(r, id)                        [handlers/analytics_dashboards.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── normalizeDashboardVisibility()
        ├── normalizeDashboardViewPresets()
        ├── normalizeDashboardAccessEntries()
        ├── [if visibility=public] generateDashboardShareToken()      [crypto/rand]
        ├── appdb.DB.Exec(UPDATE analytics_dashboards SET ...)        [db/db.go]
        ├── replaceAnalyticsDashboardAccess(id, entries)              [handlers/analytics_dashboards.go]
        │   └── appdb.DB.Exec(DELETE/INSERT analytics_dashboard_access) [db/db.go]
        └── json.NewEncoder(w).Encode()
```

### DELETE /api/analytics-dashboards/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermDashboardsManage)
    └── handlers.DeleteAnalyticsDashboard()                           [handlers/analytics_dashboards.go]
        ├── canManageAnalyticsDashboard(r, id)
        ├── appdb.DB.Exec(DELETE FROM analytics_dashboard_blocks WHERE dashboard_id=?) [db/db.go]
        └── appdb.DB.Exec(DELETE FROM analytics_dashboards WHERE id=?) [db/db.go]
```

### GET /api/analytics-dashboards/{id}/render
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermDashboardsManage)
    └── handlers.RenderAnalyticsDashboard()                           [handlers/analytics_dashboards.go]
        ├── parseIDFromPath()
        ├── canAccessAnalyticsDashboard(r, id)
        ├── renderAnalyticsDashboardForHeaders(role, id, params)      [handlers/analytics_dashboards.go]
        │   ├── getAnalyticsDashboardByID(id)
        │   ├── listAnalyticsDashboardBlocks(id)
        │   ├── per block: appdb.DB.QueryRow(SELECT sql FROM saved_queries WHERE id=?) [db/db.go]
        │   ├── normalizeAnalyticsSQL() + validateAnalyticsSQL()
        │   ├── GetDB(block.ConnectionID)                             [handlers/pool.go]
        │   └── executeAnalyticsQuery(ctx, db, sql)                  [handlers/analytics_dashboards.go]
        │       └── db.QueryContext()                                 [user DB]
        └── json.NewEncoder(w).Encode()
```

### POST /api/analytics-dashboards/{id}/blocks
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermDashboardsManage)
    └── handlers.CreateAnalyticsDashboardBlock()                      [handlers/analytics_dashboards.go]
        ├── canManageAnalyticsDashboard(r, id)
        ├── json.NewDecoder(r.Body).Decode()
        └── appdb.DB.Exec(INSERT INTO analytics_dashboard_blocks ...) [db/db.go]
```

### PUT/DELETE /api/analytics-dashboards/blocks/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermDashboardsManage)
    ├── handlers.UpdateAnalyticsDashboardBlock()                      [handlers/analytics_dashboards.go]
    │   ├── canManageAnalyticsDashboard(r, dashboardID)
    │   ├── json.NewDecoder(r.Body).Decode()
    │   └── appdb.DB.Exec(UPDATE analytics_dashboard_blocks SET ...)  [db/db.go]
    └── handlers.DeleteAnalyticsDashboardBlock()                      [handlers/analytics_dashboards.go]
        ├── canManageAnalyticsDashboard(r, dashboardID)
        └── appdb.DB.Exec(DELETE FROM analytics_dashboard_blocks WHERE id=?) [db/db.go]
```

### GET /api/analytics-dashboards/shared/{token}
```
└── (No auth middleware — public endpoint)
    └── handlers.RenderSharedAnalyticsDashboard()                     [handlers/analytics_dashboards.go]
        ├── getAnalyticsDashboardByShareToken(token)                  [handlers/analytics_dashboards.go]
        │   └── appdb.DB.QueryRow(SELECT FROM analytics_dashboards WHERE share_token=?) [db/db.go]
        ├── check visibility == "public"
        └── renderAnalyticsDashboardForHeaders("", "admin", id, params)
```

### POST /api/analytics-dashboards/preview
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermDashboardsManage)
    └── handlers.PreviewAnalyticsDashboardQuery()                     [handlers/analytics_dashboards.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── CheckReadPermission(r, connID)                            [handlers/models.go]
        ├── normalizeAnalyticsSQL() + validateAnalyticsSQL()
        ├── GetDB(connID)                                             [handlers/pool.go]
        ├── executeAnalyticsQuery(ctx, db, sql)                       [handlers/analytics_dashboards.go]
        └── json.NewEncoder(w).Encode()
```

### GET /api/analytics-dashboards/users
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermDashboardsManage)
    └── handlers.ListAnalyticsDashboardUsers()                        [handlers/analytics_dashboards.go]
        └── appdb.DB.Query(SELECT u.id, u.username, r.name FROM users LEFT JOIN roles) [db/db.go]
```

---

## Frontend

### Route: /dashboards
```
router/index.ts
└── { path: 'dashboards', meta: { requiredPermissionsAny: ['dashboards.manage'] } }
    └── views/AnalyticsDashboardsView.vue                             (124 KB)
        ├── axios.get('/api/analytics-dashboards')
        ├── axios.post('/api/analytics-dashboards', body)
        ├── axios.get('/api/analytics-dashboards/{id}')
        ├── axios.put('/api/analytics-dashboards/{id}', body)
        ├── axios.delete('/api/analytics-dashboards/{id}')
        ├── axios.get('/api/analytics-dashboards/{id}/render')
        ├── axios.post('/api/analytics-dashboards/{id}/blocks', body)
        ├── axios.put('/api/analytics-dashboards/blocks/{id}', body)
        ├── axios.delete('/api/analytics-dashboards/blocks/{id}')
        ├── axios.post('/api/analytics-dashboards/preview', body)
        ├── axios.get('/api/analytics-dashboards/users')
        └── useConnections()                                          [composables/useConnections.ts]
```

### Route: /shared-dashboards/:token (public)
```
router/index.ts
└── { path: '/shared-dashboards/:token', meta: { guest: true, publicDashboard: true } }
    └── views/AnalyticsDashboardsView.vue
        └── axios.get('/api/analytics-dashboards/shared/{token}')
```

### Route: /analytics
```
router/index.ts
└── { path: 'analytics', meta: { requiredPermissionsAny: ['analytics.view'] } }
    └── views/AnalyticsHomeView.vue
        └── axios.get('/api/connections/{id}/dashboard')              [SQL Studio dashboard widget]
```
