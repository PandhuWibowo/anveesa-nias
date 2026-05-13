# Integrasi Ekosistem — Class Tree

## Backend — Redis Integration

All Redis routes: `/api/connections/{id}/redis/...`

### GET /api/connections/{id}/redis/ping
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermRedisView)
    └── handlers.RedisPing()                                          [handlers/redis.go]
        ├── loadRedisConn(connID)                                     [handlers/redis.go]
        │   ├── appdb.DB.QueryRow(SELECT host,port,username,password,ssl FROM connections WHERE id=?) [db/db.go]
        │   └── decryptCredential(encPassword)                        [handlers/connections.go]
        ├── redisClient.ping(ctx)                                     [handlers/redis.go — raw TCP RESP]
        └── json.NewEncoder(w).Encode({ok, latency_ms})
```

### GET /api/connections/{id}/redis/keys
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermRedisView)
    └── handlers.RedisKeys()                                          [handlers/redis.go]
        ├── loadRedisConn(connID)
        ├── r.URL.Query() — pattern, cursor, count, db
        ├── redisClient.scan(ctx, cursor, pattern, count, db)         [handlers/redis.go — SCAN command]
        ├── per key: redisClient.type(ctx, key, db)                   [handlers/redis.go — TYPE command]
        ├── per key: redisClient.ttl(ctx, key, db)                    [handlers/redis.go — TTL command]
        └── json.NewEncoder(w).Encode(redisKeysResponse)
```

### GET /api/connections/{id}/redis/key
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermRedisView)
    └── handlers.RedisGetKey()                                        [handlers/redis.go]
        ├── loadRedisConn(connID)
        ├── r.URL.Query() — key, db
        ├── redisClient.getKeyValue(ctx, key, db)                     [handlers/redis.go]
        │   ├── TYPE → dispatch to GET/LRANGE/SMEMBERS/HGETALL/ZRANGE
        └── json.NewEncoder(w).Encode(redisValueResponse)
```

### POST /api/connections/{id}/redis/key (write / set)
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermRedisView)
    └── handlers.RedisSetKey()                                        [handlers/redis.go]
        ├── loadRedisConn(connID)
        ├── json.NewDecoder(r.Body).Decode(redisWriteRequest)
        ├── redisClient.setKeyValue(ctx, req)                         [handlers/redis.go — SET/LPUSH/SADD/HSET/ZADD]
        └── json.NewEncoder(w).Encode({ok})
```

### POST /api/connections/{id}/redis/rename
```
└── handlers.RedisRenameKey()                                         [handlers/redis.go]
    └── redisClient.sendCommand(ctx, db, "RENAME", oldKey, newKey)    [handlers/redis.go]
```

### POST /api/connections/{id}/redis/move
```
└── handlers.RedisMoveKey()                                           [handlers/redis.go]
    └── redisClient.sendCommand(ctx, db, "MOVE", key, targetDB)       [handlers/redis.go]
```

### POST /api/connections/{id}/redis/command
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermRedisView)
    └── handlers.RedisCommand()                                       [handlers/redis.go]
        ├── json.NewDecoder(r.Body).Decode(redisCommandRequest)
        ├── blockDangerousRedisCommand(command)                       [handlers/redis.go — block FLUSHALL etc.]
        ├── redisClient.sendRawCommand(ctx, db, command)              [handlers/redis.go]
        └── json.NewEncoder(w).Encode({result})
```

### POST /api/connections/{id}/redis/script
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermRedisView)
    └── handlers.RedisScript()                                        [handlers/redis.go]
        ├── json.NewDecoder(r.Body).Decode(redisScriptRequest)
        ├── parseRedisScript(req.Script)                              [handlers/redis.go — split by lines]
        ├── per command: redisClient.sendRawCommand(ctx, db, cmd)     [handlers/redis.go]
        └── json.NewEncoder(w).Encode([]redisScriptResult)
```

---

## Backend — Kafka Integration

All Kafka routes: `/api/connections/{id}/kafka/...`

### GET /api/connections/{id}/kafka/topics
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermKafkaView)
    └── handlers.KafkaTopics()                                        [handlers/kafka.go]
        ├── loadKafkaConn(connID)                                     [handlers/kafka.go]
        │   ├── appdb.DB.QueryRow(SELECT host,port,username,password,ssl FROM connections WHERE id=?) [db/db.go]
        │   └── decryptCredential()
        ├── readKafkaTopics(ctx, in)                                  [handlers/kafka.go]
        │   └── kafka.NewReader() / kafka-go DescribeTopics           [github.com/segmentio/kafka-go]
        └── json.NewEncoder(w).Encode([]KafkaTopicInfo)
```

### GET /api/connections/{id}/kafka/messages
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermKafkaView)
    └── handlers.KafkaMessages()                                      [handlers/kafka.go]
        ├── loadKafkaConn(connID)
        ├── r.URL.Query() — topic, partition, offset, limit
        ├── kafka.NewReader({Brokers, Topic, Partition, Offset})      [kafka-go]
        ├── reader.ReadMessage(ctx) — per message
        └── json.NewEncoder(w).Encode([]KafkaMessageInfo)
```

### POST /api/connections/{id}/kafka/produce
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermKafkaProduce)
    └── handlers.KafkaProduce()                                       [handlers/kafka.go]
        ├── loadKafkaConn(connID)
        ├── json.NewDecoder(r.Body).Decode(KafkaProduceInput)
        ├── kafka.NewWriter({Brokers})                                [kafka-go]
        ├── writer.WriteMessages(ctx, kafka.Message{Key, Value, Headers})
        └── json.NewEncoder(w).Encode({ok})
```

### POST /api/connections/{id}/kafka/consume-test
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermKafkaView)
    └── handlers.KafkaConsumeTest()                                   [handlers/kafka.go]
        ├── loadKafkaConn(connID)
        ├── json.NewDecoder(r.Body).Decode(KafkaConsumeInput)
        ├── kafka.NewReader({Brokers, Topic, GroupID})                [kafka-go]
        └── reader.ReadMessage(ctx) — up to limit messages
```

### GET /api/connections/{id}/kafka/groups
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermKafkaView)
    └── handlers.KafkaGroups()                                        [handlers/kafka.go]
        ├── loadKafkaConn(connID)
        └── kafka client DescribeGroups                               [kafka-go]
```

### GET /api/connections/{id}/kafka/groups-detail
```
└── handlers.KafkaGroupsDetail()                                      [handlers/kafka.go]
    └── kafka client ListGroups + DescribeGroups                      [kafka-go]
```

---

## Backend — Laravel Queue Integration

All Laravel Queue routes: `/api/connections/{id}/laravel-queue/...`

### GET /api/connections/{id}/laravel-queue/queues
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermQueuesView)
    └── handlers.LaravelQueueQueues()                                 [handlers/laravel_queue.go]
        ├── loadLaravelQueueConn(connID)                              [handlers/laravel_queue.go]
        │   ├── check driver is redis
        │   └── loadRedisConn(connID)                                 [handlers/redis.go]
        ├── r.URL.Query() — prefix, db
        ├── redisClient.scan(ctx, 0, prefix+"*", 1000, db)
        └── per queue: LLEN, ZCARD commands → json.NewEncoder(w).Encode([]laravelQueueSummary)
```

### GET /api/connections/{id}/laravel-queue/jobs
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermQueuesView)
    └── handlers.LaravelQueueJobs()                                   [handlers/laravel_queue.go]
        ├── loadLaravelQueueConn(connID)
        ├── r.URL.Query() — queue, prefix, state, db
        ├── [state=ready]    LRANGE queue 0 99                        [redis RESP]
        ├── [state=delayed]  ZRANGE queue:delayed 0 99                [redis RESP]
        ├── [state=reserved] ZRANGE queue:reserved 0 99               [redis RESP]
        ├── json.Unmarshal per job payload → laravelQueueJob
        └── json.NewEncoder(w).Encode(laravelQueueJobsResponse)
```

### GET /api/connections/{id}/laravel-queue/failed-jobs
```
└── handlers.LaravelQueueFailedJobs()                                 [handlers/laravel_queue.go]
    ├── check if driver is mysql/postgres (DB driver for failed jobs table)
    ├── GetDB(connID)                                                 [handlers/pool.go]
    ├── db.Query(SELECT FROM failed_jobs ORDER BY failed_at DESC)     [user DB]
    └── json.NewEncoder(w).Encode([]laravelFailedJob)
```

### GET /api/connections/{id}/laravel-queue/horizon
```
└── handlers.LaravelQueueHorizon()                                    [handlers/laravel_queue.go]
    ├── loadLaravelQueueConn(connID) → redis
    ├── scan for horizon:* keys
    ├── HGETALL horizon:supervisors:*
    └── json.NewEncoder(w).Encode(laravelHorizonSummary)
```

### GET/PUT /api/connections/{id}/laravel-queue/ops-settings
```
└── handlers.LaravelQueueOpsSettings() / handlers.LaravelQueueSaveOpsSettings() [handlers/laravel_queue_ops.go]
    ├── appdb.DB.QueryRow(SELECT FROM laravel_queue_settings WHERE conn_id=?) [db/db.go]
    └── appdb.DB.Exec(UPSERT laravel_queue_settings ...)              [db/db.go]
```

### GET /api/connections/{id}/laravel-queue/audit
```
└── handlers.LaravelQueueAudit()                                      [handlers/laravel_queue_ops.go]
    └── appdb.DB.Query(SELECT FROM laravel_queue_audit WHERE conn_id=?) [db/db.go]
```

### GET/POST /api/connections/{id}/laravel-queue/quarantine
```
└── handlers.LaravelQueueQuarantine() / handlers.LaravelQueueQuarantineAction() [handlers/laravel_queue_ops.go]
    └── appdb.DB.Query/Exec(laravel_queue_quarantine)                 [db/db.go]
```

### GET/POST /api/connections/{id}/laravel-queue/alerts
```
└── handlers.LaravelQueueAlerts() / handlers.LaravelQueueSaveAlert() [handlers/laravel_queue_ops.go]
    └── appdb.DB.Query/Exec(laravel_queue_alerts)                     [db/db.go]
```

### GET /api/connections/{id}/laravel-queue/agent
```
└── handlers.LaravelQueueAgent()                                      [handlers/laravel_queue_ops.go]
    ├── loadLaravelQueueConn(connID) → redis
    ├── scan for agent state keys
    └── json.NewEncoder(w).Encode(agentStatus)
```

---

## Frontend

### Route: /redis
```
router/index.ts
└── { path: 'redis', meta: { requiredPermissionsAny: ['redis.view'] } }
    └── views/RedisView.vue                                           (42 KB)
        └── useRedis()                                                [composables/useRedis.ts]
            ├── axios.get('/api/connections/{id}/redis/ping')
            ├── axios.get('/api/connections/{id}/redis/keys?pattern=...&cursor=...&db=...')
            ├── axios.get('/api/connections/{id}/redis/key?key=...&db=...')
            ├── axios.post('/api/connections/{id}/redis/key', body)
            ├── axios.post('/api/connections/{id}/redis/rename', body)
            ├── axios.post('/api/connections/{id}/redis/move', body)
            ├── axios.post('/api/connections/{id}/redis/command', body)
            └── axios.post('/api/connections/{id}/redis/script', body)
```

### Route: /kafka
```
router/index.ts
└── { path: 'kafka', meta: { requiredPermissionsAny: ['kafka.view'] } }
    └── views/KafkaView.vue                                           (82 KB)
        ├── axios.get('/api/connections/{id}/kafka/topics')
        ├── axios.get('/api/connections/{id}/kafka/messages?topic=...&partition=...&offset=...')
        ├── axios.post('/api/connections/{id}/kafka/produce', body)
        ├── axios.post('/api/connections/{id}/kafka/consume-test', body)
        ├── axios.get('/api/connections/{id}/kafka/groups')
        └── axios.get('/api/connections/{id}/kafka/groups-detail')
```

### Route: /laravel-queue
```
router/index.ts
└── { path: 'laravel-queue', meta: { requiredPermissionsAny: ['queues.view'] } }
    └── views/LaravelQueueView.vue                                    (93 KB)
        └── useLaravelQueue()                                         [composables/useLaravelQueue.ts]
            ├── axios.get('/api/connections/{id}/laravel-queue/queues')
            ├── axios.get('/api/connections/{id}/laravel-queue/jobs?queue=...&state=...')
            ├── axios.get('/api/connections/{id}/laravel-queue/failed-jobs')
            ├── axios.get('/api/connections/{id}/laravel-queue/horizon')
            ├── axios.get/put('/api/connections/{id}/laravel-queue/ops-settings')
            ├── axios.get('/api/connections/{id}/laravel-queue/audit')
            ├── axios.get/post('/api/connections/{id}/laravel-queue/quarantine')
            ├── axios.get/post('/api/connections/{id}/laravel-queue/alerts')
            └── axios.get('/api/connections/{id}/laravel-queue/agent')
```
