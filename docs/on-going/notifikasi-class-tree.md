# Notifikasi — Class Tree

## Backend

### GET /api/notifications
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermNotificationsView)
    └── handlers.ListNotifications()                                  [handlers/notifications.go]
        ├── currentUserFromHeaders(r)
        ├── r.URL.Query() — limit, event_type, severity, read
        ├── appdb.DB.Query(SELECT FROM notifications WHERE target_user_id=? OR target_user_id=0 ORDER BY id DESC LIMIT ?) [db/db.go]
        └── json.NewEncoder(w).Encode()
```

### PUT /api/notifications
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermNotificationsView)
    └── handlers.MarkNotificationsRead()                              [handlers/notifications.go]
        ├── currentUserFromHeaders(r)
        ├── json.NewDecoder(r.Body).Decode() — {ids: [...]}
        ├── appdb.DB.Exec(UPDATE notifications SET read=1 WHERE id IN (?) AND target_user_id=?) [db/db.go]
        └── invalidateNotificationCountCache(userID)                  [handlers/notifications.go]
            └── cache.Default().Delete(ctx, "notif:unread:{userID}")  [cache/]
```

### GET /api/notifications/unread
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermNotificationsView)
    └── handlers.UnreadCount()                                        [handlers/notifications.go]
        ├── currentUserFromHeaders(r)
        ├── cache.Default().Get(ctx, "notif:unread:{userID}")         [cache/]
        ├── [cache miss] appdb.DB.QueryRow(SELECT COUNT(*) FROM notifications WHERE read=0 AND (target_user_id=? OR target_user_id=0)) [db/db.go]
        └── json.NewEncoder(w).Encode({count})
```

### GET /api/notification-events
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermNotificationsManage)
    └── handlers.ListNotificationEvents()                             [handlers/notifications.go]
        ├── r.URL.Query().Get("limit")
        ├── appdb.DB.Query(SELECT FROM notification_events ORDER BY id DESC LIMIT ?) [db/db.go]
        └── json.NewEncoder(w).Encode()
```

### GET/POST /api/notification-targets
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermNotificationsManage)
    ├── handlers.ListNotificationTargets()                            [handlers/notifications.go]
    │   └── appdb.DB.Query(SELECT FROM notification_targets ORDER BY updated_at DESC) [db/db.go]
    └── handlers.CreateNotificationTarget()                           [handlers/notifications.go]
        ├── currentUserFromHeaders(r)
        ├── json.NewDecoder(r.Body).Decode()
        ├── validateNotificationTargetPayload(body, true)             [handlers/notifications.go]
        │   └── encryptCredential(secret)                             [handlers/connections.go — AES-GCM]
        ├── appdb.DB.Exec(INSERT INTO notification_targets ...)        [db/db.go]
        └── getNotificationTargetByID(id)                             [handlers/notifications.go]
            └── appdb.DB.QueryRow(SELECT FROM notification_targets WHERE id=?) [db/db.go]
```

### PUT/DELETE /api/notification-targets/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermNotificationsManage)
    ├── handlers.UpdateNotificationTarget()                           [handlers/notifications.go]
    │   ├── getNotificationTargetRowByID(id)                         [handlers/notifications.go]
    │   ├── validateNotificationTargetPayload(body, false)
    │   └── appdb.DB.Exec(UPDATE notification_targets SET ...)        [db/db.go]
    └── handlers.DeleteNotificationTarget()                           [handlers/notifications.go]
        └── appdb.DB.Exec(DELETE FROM notification_targets WHERE id=?) [db/db.go]
```

### POST /api/notification-targets/{id}/test
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermNotificationsManage)
    └── handlers.TestNotificationTarget()                             [handlers/notifications.go]
        ├── getNotificationTargetByID(id)
        ├── decryptTargetSecrets(target)                              [handlers/notifications.go]
        └── deliverToTarget(ctx, target, testPayload)                 [handlers/notifications.go]
            ├── [type=slack]    HTTP POST to Slack webhook URL         [net/http]
            ├── [type=discord]  HTTP POST to Discord webhook URL       [net/http]
            ├── [type=webhook]  HTTP POST to custom URL + HMAC-SHA256 signature [net/http + crypto/hmac]
            └── [type=email]    SMTP send via net/smtp                 [net/smtp — if configured]
```

### GET/POST /api/notification-rules
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermNotificationsManage)
    ├── handlers.ListNotificationRules()                              [handlers/notifications.go]
    │   └── appdb.DB.Query(SELECT FROM notification_rules ORDER BY updated_at DESC) [db/db.go]
    └── handlers.CreateNotificationRule()                             [handlers/notifications.go]
        ├── json.NewDecoder(r.Body).Decode(notificationRulePayload)
        └── appdb.DB.Exec(INSERT INTO notification_rules ...)          [db/db.go]
```

### PUT/DELETE /api/notification-rules/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermNotificationsManage)
    ├── handlers.UpdateNotificationRule()                             [handlers/notifications.go]
    │   └── appdb.DB.Exec(UPDATE notification_rules SET ...)          [db/db.go]
    └── handlers.DeleteNotificationRule()                             [handlers/notifications.go]
        └── appdb.DB.Exec(DELETE FROM notification_rules WHERE id=?)  [db/db.go]
```

### GET /api/notification-deliveries
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermNotificationsManage)
    └── handlers.ListNotificationDeliveries()                        [handlers/notifications.go]
        ├── r.URL.Query() — limit, status, target_id
        ├── appdb.DB.Query(SELECT nd.*, nt.name, ne.* FROM notification_deliveries JOIN notification_targets JOIN notification_events ORDER BY id DESC LIMIT ?) [db/db.go]
        └── json.NewEncoder(w).Encode([]NotificationDelivery)
```

### Background: StartNotificationWorker() / StopNotificationWorker()
```
handlers.StartNotificationWorker()                                    [handlers/notifications.go]
└── go goroutine: ticker 15 seconds
    └── processNotificationWorkerTick()                               [handlers/notifications.go]
        ├── appdb.DB.QueryRow(SELECT instance_id FROM notification_worker_lock WHERE expires_at > NOW()) [db/db.go — distributed lock]
        ├── [acquire lock] appdb.DB.Exec(UPSERT notification_worker_lock)
        ├── appdb.DB.Query(SELECT FROM notification_deliveries WHERE status='pending' AND next_attempt_at <= NOW() LIMIT 20) [db/db.go]
        ├── per delivery: getNotificationTargetByID(delivery.TargetID)
        ├── per delivery: deliverToTarget(ctx, target, payload)       [handlers/notifications.go]
        │   ├── [slack/discord/webhook] HTTP POST via net/http
        │   └── [HMAC signature] crypto/hmac.New(sha256.New, secret)
        ├── [success] appdb.DB.Exec(UPDATE notification_deliveries SET status='delivered')  [db/db.go]
        └── [failure] appdb.DB.Exec(UPDATE notification_deliveries SET attempts=attempts+1, next_attempt_at=expBackoff) [db/db.go]
```

### Internal: EmitNotification(input NotificationEventInput)
```
handlers.EmitNotification(input)                                      [handlers/notifications.go]
├── appdb.DB.Exec(INSERT INTO notification_events ...)                [db/db.go]
├── per targetUserID: appdb.DB.Exec(INSERT INTO notifications ...)    [db/db.go]
├── invalidateNotificationCountCache(targetUserIDs...)
└── queueNotificationDeliveries(eventID, ...)                        [handlers/notifications.go]
    ├── appdb.DB.Query(SELECT FROM notification_rules WHERE is_active=1 AND event_type matches) [db/db.go]
    └── per matching rule: appdb.DB.Exec(INSERT INTO notification_deliveries ...) [db/db.go]
```

---

## Frontend

### Route: /notifications
```
router/index.ts
└── { path: 'notifications', meta: { requiredPermissionsAny: ['notifications.view'] } }
    └── views/NotificationsView.vue                                   (38 KB)
        ├── axios.get('/api/notifications?limit=...&event_type=...&severity=...')
        ├── axios.put('/api/notifications', {ids: [...]})             [mark as read]
        ├── axios.get('/api/notifications/unread')
        ├── axios.get('/api/notification-events?limit=...')
        ├── axios.get('/api/notification-targets')
        ├── axios.post('/api/notification-targets', body)
        ├── axios.put('/api/notification-targets/{id}', body)
        ├── axios.delete('/api/notification-targets/{id}')
        ├── axios.post('/api/notification-targets/{id}/test')
        ├── axios.get('/api/notification-rules')
        ├── axios.post('/api/notification-rules', body)
        ├── axios.put('/api/notification-rules/{id}', body)
        ├── axios.delete('/api/notification-rules/{id}')
        └── axios.get('/api/notification-deliveries?limit=...&status=...')
```

### Notification badge (global — AppLayout)
```
layouts/AppLayout.vue
└── setInterval → axios.get('/api/notifications/unread')              [polling unread count]
```
