# AI — Class Tree

## Backend

### GET /api/ai/settings
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermAIUse | PermAIManage)
    └── handlers.GetAISettings()                                          [handlers/ai.go]
        ├── currentUserID(r)                                              [handlers/ai.go — X-User-ID header]
        ├── readUserAISettings(userID)                                    [handlers/ai.go]
        │   └── appdb.DB.QueryRow(SELECT api_key, base_url, model FROM user_ai_settings WHERE user_id=?) [db/db.go]
        ├── readGlobalAISettings()                                        [handlers/ai.go]
        │   ├── appdb.DB.QueryRow(SELECT value FROM settings WHERE key='ai_api_key') [db/db.go]
        │   ├── appdb.DB.QueryRow(SELECT value FROM settings WHERE key='ai_base_url') [db/db.go]
        │   ├── appdb.DB.QueryRow(SELECT value FROM settings WHERE key='ai_model')   [db/db.go]
        │   └── [env override] globalAIOverride (set by SetGlobalAIConfig via AI_API_KEY, AI_BASE_URL, AI_MODEL env vars)
        ├── maskAPIKey(userSettings.APIKey)                               [handlers/ai.go — masks all but last 4 chars]
        └── json.NewEncoder(w).Encode({api_key, base_url, model, source, fallback_available})
```

### POST /api/ai/settings
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermAIUse | PermAIManage)
    └── handlers.SaveAISettings()                                         [handlers/ai.go]
        ├── currentUserID(r)
        ├── json.NewDecoder(r.Body).Decode(AISettings)
        ├── readUserAISettings(userID)                                    [handlers/ai.go — load current to merge]
        ├── isSafeAIModel(model)                                          [handlers/ai.go — alphanumeric + - . _ / : only]
        ├── url.Parse(baseURL) — scheme must be http or https
        └── saveUserAISettings(userID, merged)                            [handlers/ai.go]
            └── appdb.DB.Exec(INSERT or UPDATE user_ai_settings SET api_key=?, base_url=?, model=?) [db/db.go]
```

### POST /api/ai/chat
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermAIUse)
    └── handlers.AIChat()                                                 [handlers/ai.go]
        ├── json.NewDecoder(r.Body).Decode({messages: [...]})
        ├── [guard] len(messages) > 50 → reject
        ├── [guard] total content size > 100 KB → reject
        ├── resolveAISettings(r)                                          [handlers/ai.go]
        │   ├── readUserAISettings(userID)                                [handlers/ai.go — appdb.DB]
        │   └── [fallback] readGlobalAISettings()                        [handlers/ai.go — appdb.DB + env vars]
        ├── prepend aiSafetyPrompt as system message                      [handlers/ai.go — const]
        │   └── [user system msgs] appended as "Additional context:" suffix on safety prompt
        ├── HTTP POST {baseURL}/chat/completions                          [net/http — OpenAI-compatible API]
        │   ├── Authorization: Bearer {apiKey}
        │   ├── max_tokens: 2048
        │   └── 60s timeout; 1 MB response limit
        └── io.Copy(w, limitedReader) — proxies raw AI response
```

### POST /api/ai/analytics
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermAIUse)
    └── handlers.AIAnalytics()                                            [handlers/ai.go]
        ├── json.NewDecoder(r.Body).Decode(AIAnalyticsRequest{conn_id, question, sql, title, compare_preset})
        ├── CheckReadPermission(r, connID)                                [handlers/permissions_legacy.go]
        ├── resolveAISettings(r)                                          [handlers/ai.go]
        ├── GetDB(connID)                                                 [handlers/pool.go — user DB connection]
        ├── buildAnalyticsSchemaContext(ctx, connID, dbConn, driver)      [handlers/ai.go]
        │   ├── cache.Default().Get(ctx, "ai:schema-context:{connID}:{driver}") [cache/]
        │   ├── [cache miss — postgres] dbConn.QueryRow(SELECT current_database()) [user DB]
        │   ├── [cache miss — mysql]    dbConn.QueryRow(SELECT DATABASE())         [user DB]
        │   ├── [cache miss — mssql]    dbConn.QueryRow(SELECT DB_NAME())          [user DB]
        │   └── dbConn.Query(SELECT table_schema, table_name, column_name, data_type FROM information_schema.columns LIMIT 240) [user DB]
        │
        ├── [if req.SQL provided — skip AI planner]
        │   └── normalizeAnalyticsSQL(req.SQL)                           [handlers/ai.go — strip trailing semicolon]
        │
        ├── [if req.Question provided — AI planner call]
        │   └── callAIText(ctx, apiKey, baseURL, model, plannerMessages, maxTokens=1600) [handlers/ai.go]
        │       ├── analyticsPlannerPrompt(driver, databaseName, schemaContext, comparePreset) [handlers/ai.go]
        │       ├── doAIChatCompletion(ctx, apiKey, baseURL, payload)    [handlers/ai.go — HTTP POST /chat/completions]
        │       │   └── [retry] if max_tokens param rejected → retry with max_completion_tokens → retry without
        │       └── parseAnalyticsPlan(content) → aiAnalyticsPlan{title, sql, chart_type, assumptions, follow_ups}
        │
        ├── validateAnalyticsSQL(plan.SQL)                               [handlers/ai.go — SELECT/WITH/SHOW only; blocks INSERT/UPDATE/DELETE/DROP/etc.]
        ├── executeAnalyticsQuery(ctx, dbConn, plan.SQL)                 [handlers/ai.go]
        │   └── dbConn.QueryContext(ctx+20s timeout, sqlText) → QueryResult{Columns, Rows (max 200), RowCount, DurationMs} [user DB]
        │
        ├── callAIText(ctx, ..., summaryMessages, maxTokens=900)         [handlers/ai.go — AI summarizer call]
        │   ├── analyticsSummaryPrompt(question, comparePreset, plan, result) [handlers/ai.go]
        │   └── parseAnalyticsSummary(content) → aiAnalyticsSummary{summary, chart_type, report_cards, follow_ups}
        │
        └── json.NewEncoder(w).Encode(AIAnalyticsResponse)
```

### POST /api/ai/analytics/stream
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermAIUse)
    └── handlers.AIAnalyticsStream()                                      [handlers/ai.go]
        ├── w.Header Set("Content-Type", "text/event-stream")
        ├── [same validation as AIAnalytics: conn_id, question/sql, read permission]
        ├── resolveAISettings(r)
        ├── GetDB(connID)                                                 [handlers/pool.go]
        ├── buildAnalyticsSchemaContext(ctx, connID, dbConn, driver)      [handlers/ai.go]
        │
        ├── emit("progress", {step: "planning", message: "Generating analytics query plan…"})
        ├── callAIText → parseAnalyticsPlan                              [handlers/ai.go — same as AIAnalytics]
        ├── emit("plan", {title, sql, chart_type, assumptions, follow_up_questions})
        │
        ├── validateAnalyticsSQL(plan.SQL)
        ├── emit("progress", {step: "executing", message: "Running database query…"})
        ├── executeAnalyticsQuery(ctx, dbConn, plan.SQL)                 [handlers/ai.go]
        ├── emit("query", {columns, rows, row_count, duration_ms})
        │
        ├── emit("progress", {step: "summarizing", message: "Generating AI summary…"})
        ├── callAIText → parseAnalyticsSummary                          [handlers/ai.go]
        ├── emit("summary", {summary, chart_type, follow_up_questions, report_cards})
        │
        └── emit("done", AIAnalyticsResponse{...full result...})
            └── flusher.Flush() after each event                         [http.Flusher]
```

### GET /api/ai/reports
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermAIUse)
    └── handlers.ListAIReports()                                          [handlers/ai.go]
        ├── currentUserID(r)
        ├── appdb.DB.Query(SELECT id,conn_id,title,question,summary,chart_type,sql_text,columns_json,rows_json,report_cards,follow_ups,created_at FROM ai_reports WHERE user_id=? ORDER BY created_at DESC) [db/db.go]
        ├── per row: json.Unmarshal(columns_json, rows_json, report_cards, follow_ups)
        └── json.NewEncoder(w).Encode([]AIReport)
```

### POST /api/ai/reports
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermAIUse)
    └── handlers.SaveAIReport()                                           [handlers/ai.go]
        ├── currentUserID(r)
        ├── json.NewDecoder(r.Body).Decode(AIReport)
        ├── [guard] title and sql must be non-empty
        ├── json.Marshal(columns, rows, report_cards, follow_ups) → JSON strings
        ├── appdb.DB.Exec(INSERT INTO ai_reports (user_id, conn_id, title, question, summary, chart_type, sql_text, columns_json, rows_json, report_cards, follow_ups, created_at, updated_at)) [db/db.go]
        └── json.NewEncoder(w).Encode({id})
```

### DELETE /api/ai/reports/{id}
```
└── Middleware: mw.InjectUserContext → mw.RequireAnyAppPermission(PermAIUse)
    └── handlers.DeleteAIReport()                                         [handlers/ai.go]
        ├── currentUserID(r)
        ├── strconv.ParseInt(r.URL.Path — strip "/api/ai/reports/")
        └── appdb.DB.Exec(DELETE FROM ai_reports WHERE id=? AND user_id=?) [db/db.go]
```

### Internal Helpers
```
resolveAISettings(r)                                                      [handlers/ai.go]
├── readUserAISettings(userID) — appdb user_ai_settings table
├── [user has key] return user settings (baseURL/model fall back to defaults)
└── [no user key]  readGlobalAISettings()
    ├── appdb.DB.QueryRow(settings table — ai_api_key / ai_base_url / ai_model)
    └── globalAIOverride (set from AI_API_KEY / AI_BASE_URL / AI_MODEL env vars at startup)

callAIText(ctx, apiKey, baseURL, model, messages, maxTokens)              [handlers/ai.go]
└── doAIChatCompletion(ctx, apiKey, baseURL, payload)                    [handlers/ai.go]
    ├── HTTP POST {baseURL}/chat/completions — Authorization: Bearer {apiKey}
    ├── 60s timeout; 1 MB response limit
    └── [retry logic] max_tokens rejected → swap to max_completion_tokens → drop token limit

buildAnalyticsSchemaContext(ctx, connID, dbConn, driver)                  [handlers/ai.go]
├── cache.Default().Get(ctx, "ai:schema-context:{connID}:{driver}")      [cache/]
└── [cache miss] information_schema.columns query (240 row limit) → format as "schema.table(col type,...)" [user DB]

validateAnalyticsSQL(sql)                                                 [handlers/ai.go]
└── must start with SELECT/WITH/SHOW/DESCRIBE/EXPLAIN/PRAGMA; blocks INSERT/UPDATE/DELETE/DROP/ALTER/CREATE/TRUNCATE/MERGE/EXEC/COPY/GRANT/REVOKE

SetGlobalAIConfig(apiKey, baseURL, model)                                 [handlers/ai.go — called from main.go at startup]
└── sets globalAIOverride from cfg.AIAPIKey / cfg.AIBaseURL / cfg.AIModel [config/config.go]
```

---

## Frontend

### Route: /ai-analytics
```
router/index.ts
└── { path: 'ai-analytics', meta: { requiredPermissionsAny: ['ai.use'] } }
    └── views/AIAnalyticsView.vue
        ├── axios.get('/api/ai/reports')
        ├── axios.post('/api/ai/reports', body)
        ├── axios.delete('/api/ai/reports/{id}')
        ├── axios.post('/api/ai/analytics', {conn_id, question, sql, title, compare_preset})
        └── EventSource / fetch POST '/api/ai/analytics/stream'          [SSE — events: progress | plan | query | summary | done | error]
```

### Route: /settings (AI settings tab)
```
router/index.ts
└── { path: 'settings', meta: { requiredPermissionsAny: ['ai.use', 'ai.manage'] } }
    └── views/SettingsView.vue
        ├── axios.get('/api/ai/settings')
        └── axios.post('/api/ai/settings', {api_key, base_url, model})
```

### Chat (embedded in AIAnalyticsView / global panel)
```
views/AIAnalyticsView.vue (or shared chat component)
└── axios.post('/api/ai/chat', {messages: [{role, content}, ...]})
```
