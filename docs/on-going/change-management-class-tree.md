# Change Management — Class Tree

## Backend

### GET/POST /api/change-sets
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermChangeSetsManage, PermQueryApprove)
    ├── handlers.ListChangeSets()                                     [handlers/change_sets.go]
    │   ├── currentUserFromHeaders(r)
    │   ├── [admin] appdb.DB.QueryContext(SELECT FROM change_sets JOIN connections JOIN users) [db/db.go]
    │   ├── [user]  appdb.DB.QueryContext(... WHERE creator_id=? OR step approver match) [db/db.go]
    │   └── scanChangeSets(rows)
    └── handlers.CreateChangeSet()                                    [handlers/change_sets.go]
        ├── json.NewDecoder(r.Body).Decode()
        ├── currentUserFromHeaders(r)
        ├── findApplicableWorkflows(userID, role, connID)             [handlers/workflow_approval.go]
        │   └── appdb.DB.Query(SELECT FROM workflows)                 [db/db.go]
        └── appdb.DB.Exec(INSERT INTO change_sets ...)                [db/db.go]
```

### GET/PUT /api/change-sets/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermChangeSetsManage)
    ├── handlers.GetChangeSet()                                       [handlers/change_sets.go]
    │   └── getChangeSetByID(id)                                      [handlers/change_sets.go]
    │       └── appdb.DB.QueryRow(SELECT FROM change_sets WHERE id=?) [db/db.go]
    └── handlers.UpdateChangeSet()                                    [handlers/change_sets.go]
        ├── getChangeSetByID(id)
        ├── json.NewDecoder(r.Body).Decode()
        └── appdb.DB.Exec(UPDATE change_sets SET ...)                 [db/db.go]
```

### POST /api/change-sets/{id}/validate
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermChangeSetsManage)
    └── handlers.ValidateChangeSet()                                  [handlers/change_sets.go]
        ├── getChangeSetByID(id)
        ├── GetDB(cs.ConnID)                                          [handlers/pool.go]
        ├── db.ExecContext(BEGIN / ROLLBACK — dry run)                [user DB]
        └── appdb.DB.Exec(UPDATE change_sets SET validation_status=?) [db/db.go]
```

### POST /api/change-sets/{id}/submit
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermChangeSetsManage)
    └── handlers.SubmitChangeSet()                                    [handlers/change_sets.go]
        ├── getChangeSetByID(id)
        ├── findApplicableWorkflows(...)                              [handlers/workflow_approval.go]
        ├── appdb.DB.Exec(UPDATE change_sets SET status='pending', workflow_id=?) [db/db.go]
        └── EmitNotificationEvent(...)                                [handlers/notifications.go]
```

### GET /api/change-sets/{id}/approval-progress
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermChangeSetsManage, PermQueryApprove)
    └── handlers.GetChangeSetApprovalProgress()                       [handlers/change_sets.go]
        ├── getChangeSetByID(id)
        └── appdb.DB.Query(SELECT FROM workflow_step WHERE workflow_id=? ORDER BY step_order) [db/db.go]
```

### POST /api/change-sets/{id}/approve-step
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermQueryApprove)
    └── handlers.ApproveChangeSetStep()                               [handlers/change_sets.go]
        ├── getChangeSetByID(id)
        ├── currentUserFromHeaders(r)
        ├── appdb.DB.QueryRow(SELECT FROM workflow_step WHERE step_order=?) [db/db.go]
        ├── appdb.DB.Exec(INSERT INTO change_set_approvals ...)        [db/db.go]
        └── advanceChangeSetStep(cs, nextStep)                        [handlers/change_sets.go]
            └── appdb.DB.Exec(UPDATE change_sets SET current_step=?) [db/db.go]
```

### POST /api/change-sets/{id}/execute
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermChangeSetsManage)
    └── handlers.ExecuteChangeSet()                                   [handlers/change_sets.go]
        ├── getChangeSetByID(id)
        ├── check status == 'approved'
        ├── GetDB(cs.ConnID)                                          [handlers/pool.go]
        ├── db.ExecContext(cs.Statement)                              [user DB — executes DDL/DML]
        ├── appdb.DB.Exec(UPDATE change_sets SET status='executed', executed_at=?) [db/db.go]
        └── EmitNotificationEvent(...)                                [handlers/notifications.go]
```

### GET/POST /api/workflows
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermWorkflowsManage)
    ├── handlers.ListWorkflows()                                      [handlers/workflow_approval.go]
    │   └── appdb.DB.Query(SELECT FROM workflows ORDER BY name)       [db/db.go]
    └── handlers.CreateWorkflow()                                     [handlers/workflow_approval.go]
        ├── json.NewDecoder(r.Body).Decode()
        └── appdb.DB.Exec(INSERT INTO workflows ...)                  [db/db.go]
```

### GET/PUT/DELETE /api/workflows/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermWorkflowsManage)
    ├── handlers.GetWorkflow()
    │   └── appdb.DB.QueryRow(SELECT FROM workflows WHERE id=?)       [db/db.go]
    ├── handlers.UpdateWorkflow()
    │   └── appdb.DB.Exec(UPDATE workflows SET ...)                   [db/db.go]
    └── handlers.DeleteWorkflow()                                     [handlers/workflow_approval.go]
        └── appdb.DB.Exec(DELETE FROM workflows WHERE id=?)           [db/db.go]
```

### GET /api/workflows/applicable
```
└── Middleware: mw.InjectUserContext
    └── handlers.ListApplicableWorkflows()                            [handlers/workflow_approval.go]
        ├── currentUserFromHeaders(r)
        └── findApplicableWorkflows(userID, role, connID)             [handlers/workflow_approval.go]
            └── appdb.DB.Query(SELECT FROM workflows WHERE ...)       [db/db.go]
```

### PUT /api/workflows/{id}/active
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermWorkflowsManage)
    └── handlers.ToggleWorkflowActive()                               [handlers/workflow_approval.go]
        └── appdb.DB.Exec(UPDATE workflows SET is_active=?)           [db/db.go]
```

### GET/POST /api/approval-requests
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermApprovalsView, PermQueryApprove)
    ├── handlers.ListApprovalRequests()                               [handlers/workflow_approval.go]
    │   ├── currentUserFromHeaders(r)
    │   └── appdb.DB.Query(SELECT FROM approval_requests JOIN ...)    [db/db.go]
    └── handlers.CreateApprovalRequest()                              [handlers/workflow_approval.go]
        ├── json.NewDecoder(r.Body).Decode()
        └── appdb.DB.Exec(INSERT INTO approval_requests ...)          [db/db.go]
```

### GET/PUT /api/approval-requests/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermApprovalsView)
    ├── handlers.GetApprovalRequest()
    │   └── appdb.DB.QueryRow(SELECT FROM approval_requests WHERE id=?) [db/db.go]
    └── handlers.UpdateApprovalRequest()
        └── appdb.DB.Exec(UPDATE approval_requests SET ...)            [db/db.go]
```

### GET /api/approval-requests/{id}/approval-progress
```
└── handlers.GetApprovalProgress()                                    [handlers/workflow_approval.go]
    └── appdb.DB.Query(SELECT FROM workflow_step WHERE workflow_id=?) [db/db.go]
```

### POST /api/approval-requests/{id}/approve-step
```
└── handlers.ApproveApprovalStep()                                    [handlers/workflow_approval.go]
    ├── currentUserFromHeaders(r)
    ├── appdb.DB.Exec(INSERT INTO approval_step_approvals ...)        [db/db.go]
    └── advanceApprovalStep(req, nextStep)                            [handlers/workflow_approval.go]
```

### POST /api/approval-requests/{id}/execute
```
└── handlers.ExecuteApprovalRequest()                                 [handlers/workflow_approval.go]
    ├── GetDB(req.ConnID)                                             [handlers/pool.go]
    ├── db.ExecContext(req.SQL)                                       [user DB]
    └── appdb.DB.Exec(UPDATE approval_requests SET status='executed') [db/db.go]
```

### GET/POST /api/data-scripts
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermDataScriptsManage)
    ├── handlers.ListDataScripts()                                    [handlers/data_scripts.go]
    │   └── appdb.DB.Query(SELECT FROM data_scripts)                  [db/db.go]
    └── handlers.CreateDataScript()                                   [handlers/data_scripts.go]
        ├── json.NewDecoder(r.Body).Decode()
        └── appdb.DB.Exec(INSERT INTO data_scripts ...)               [db/db.go]
```

### GET /api/data-scripts/{id}
```
└── handlers.GetDataScript()                                          [handlers/data_scripts.go]
    └── appdb.DB.QueryRow(SELECT FROM data_scripts WHERE id=?)        [db/db.go]
```

### GET/POST /api/data-scripts/{id}/versions
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermDataScriptsManage)
    ├── handlers.ListDataScriptVersions()                             [handlers/data_scripts.go]
    │   └── appdb.DB.Query(SELECT FROM data_script_versions WHERE script_id=?) [db/db.go]
    └── handlers.CreateDataScriptVersion()                            [handlers/data_scripts.go]
        └── appdb.DB.Exec(INSERT INTO data_script_versions ...)       [db/db.go]
```

### POST /api/data-scripts/{id}/preview
```
└── handlers.PreviewDataScript()                                      [handlers/data_scripts.go]
    ├── GetDB(script.ConnID)                                          [handlers/pool.go]
    ├── data_scripts_runtime — template rendering
    └── db.QueryContext(rendered SQL)                                 [user DB]
```

### POST /api/data-scripts/{id}/submit
```
└── handlers.SubmitDataScript()                                       [handlers/data_scripts.go]
    ├── appdb.DB.Exec(INSERT INTO data_change_plans ...)              [db/db.go]
    └── EmitNotificationEvent(...)                                    [handlers/notifications.go]
```

### GET /api/data-scripts/{id}/plans
```
└── handlers.ListDataScriptPlans()                                    [handlers/data_scripts.go]
    └── appdb.DB.Query(SELECT FROM data_change_plans WHERE script_id=?) [db/db.go]
```

### GET/POST /api/data-change-plans
```
└── handlers.ListAllDataChangePlans() / handlers.SubmitDataChangePlan() [handlers/data_scripts.go]
    └── appdb.DB.Query/Exec(data_change_plans)                        [db/db.go]
```

### POST /api/data-change-plans/{id}/review
```
└── handlers.ReviewDataChangePlan()                                   [handlers/data_scripts.go]
    └── appdb.DB.Exec(UPDATE data_change_plans SET status=?)          [db/db.go]
```

### POST /api/data-change-plans/{id}/execute
```
└── handlers.ExecuteDataChangePlan()                                  [handlers/data_scripts.go]
    ├── GetDB(plan.ConnID)                                            [handlers/pool.go]
    ├── data_scripts_runtime.ExecutePlan(...)                         [handlers/data_scripts_runtime.go]
    │   └── db.ExecContext(rendered SQL statements)                   [user DB]
    └── appdb.DB.Exec(UPDATE data_change_plans SET status='executed') [db/db.go]
```

---

## Frontend

### Route: /change-sets
```
router/index.ts
└── { path: 'change-sets', meta: { requiredPermissionsAny: ['changesets.manage', 'query.approve'] } }
    └── views/ChangeSetsView.vue                                      (27 KB)
        ├── axios.get('/api/change-sets')
        ├── axios.post('/api/change-sets', body)
        ├── axios.get('/api/change-sets/{id}')
        ├── axios.put('/api/change-sets/{id}', body)
        ├── axios.post('/api/change-sets/{id}/validate')
        ├── axios.post('/api/change-sets/{id}/submit')
        ├── axios.get('/api/change-sets/{id}/approval-progress')
        ├── axios.post('/api/change-sets/{id}/approve-step')
        ├── axios.post('/api/change-sets/{id}/execute')
        └── useConnections()
```

### Route: /approvals
```
router/index.ts
└── { path: 'approvals', meta: { requiredPermissionsAny: ['approvals.view', 'query.approve'] } }
    └── views/ApprovalRequestsView.vue                                (42 KB)
        ├── axios.get('/api/approval-requests')
        ├── axios.post('/api/approval-requests', body)
        ├── axios.get('/api/approval-requests/{id}')
        ├── axios.get('/api/approval-requests/{id}/approval-progress')
        ├── axios.post('/api/approval-requests/{id}/approve-step')
        └── axios.post('/api/approval-requests/{id}/execute')
```

### Route: /workflows
```
router/index.ts
└── { path: 'workflows', meta: { requiredPermissionsAny: ['workflows.manage'] } }
    └── views/ApprovalWorkflowsView.vue                               (19 KB)
        ├── axios.get('/api/workflows')
        ├── axios.post('/api/workflows', body)
        ├── axios.get('/api/workflows/{id}')
        ├── axios.put('/api/workflows/{id}', body)
        ├── axios.delete('/api/workflows/{id}')
        └── axios.put('/api/workflows/{id}/active', body)
```

### Route: /data-scripts
```
router/index.ts
└── { path: 'data-scripts', meta: { requiredPermissionsAny: ['datascripts.manage', 'query.approve'] } }
    └── views/DataScriptsView.vue                                     (37 KB)
        ├── axios.get('/api/data-scripts')
        ├── axios.post('/api/data-scripts', body)
        ├── axios.get('/api/data-scripts/{id}')
        ├── axios.get('/api/data-scripts/{id}/versions')
        ├── axios.post('/api/data-scripts/{id}/versions')
        ├── axios.post('/api/data-scripts/{id}/preview')
        └── axios.post('/api/data-scripts/{id}/submit')
```

### Route: /data-script-requests
```
router/index.ts
└── { path: 'data-script-requests', meta: { requiredPermissionsAny: ['scriptrequests.view', 'query.approve'] } }
    └── views/DataScriptRequestsView.vue                              (18 KB)
        ├── axios.get('/api/data-change-plans')
        ├── axios.post('/api/data-change-plans/{id}/review')
        └── axios.post('/api/data-change-plans/{id}/execute')
```
