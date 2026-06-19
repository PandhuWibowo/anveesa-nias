# Redis Explorer

The Redis Explorer is a built-in Redis workbench for any connection whose driver is `redis`. It lets you browse keys, read and edit values of every Redis data type, run commands and scripts, inspect server health, and tail pub/sub channels in real time — all from the web UI, with every action gated by permissions and recorded in the audit log.

Route:
- `/redis`

Source:
- Frontend view: `web/src/views/RedisView.vue`
- API client: `web/src/composables/useRedis.ts`
- Backend handlers: `server/handlers/redis.go`
- Route registration: `server/main.go` (`case sub == "redis" ...`)

## How It Works (Architecture)

- The backend speaks the **raw RESP protocol** directly over TCP. There is no third-party Redis client library — commands are serialized by hand (`writeRedisCommand`) and replies are parsed by `readRedisRESP`.
- TLS is used when the connection has SSL enabled.
- A short 3-second timeout applies to each command; the pub/sub Monitor uses a long-lived connection capped at 10 minutes.
- Connection credentials are read from the app database and AES-decrypted with `NIAS_ENCRYPTION_KEY` for each request. Connections are not pooled for Redis — each command dials, authenticates (`AUTH`), optionally selects the DB (`SELECT`), runs, and closes.
- Every operation calls `writeRedisAudit`, so reads, writes, commands, and scripts all appear in the audit log with the acting user, connection, and target key.

## Permissions

Each endpoint is gated with `requireAny(...)`, meaning the user needs **at least one** of the listed app permissions:

| Operation group | Required (any of) |
| --- | --- |
| Read: ping, scan keys, read value, generate script, info, slowlog, monitor | `connections.view` **or** `schema.browse` |
| Write: save, delete, rename, move, command, execute script, set TTL | `connections.edit` **or** `schema.browse` |

Read-only users (with only `connections.view`) can browse and inspect but cannot mutate keys.

## Connecting

Purpose:
- Establish and verify a live Redis session for a selected connection and database.

Typical workflow:
1. Open `/redis`. If exactly one Redis connection exists, it is selected automatically; otherwise pick one from the dropdown.
2. The Explorer pings the server (`PING`) and shows `Connected / <latency>ms` when healthy.
3. Choose a database from the **DB** selector (DB 0–15). Each option shows its key count from `INFO keyspace`.
4. Use **Disconnect** to drop the local session and **Reconnect** to re-ping and reload keys.

Notes:
- A Redis connection's `database` field holds the numeric DB index (default `0`), not a database name. The default port is `6379`.
- The connection card shows red when offline; the workspace disables write actions until reconnected.

## Left Sidebar — Key Explorer

Purpose:
- Browse and locate keys without scanning the entire keyspace at once.

Features:
- **Filter / pattern**: a glob pattern (default `*`) passed to Redis `SCAN ... MATCH`. Press Enter or click the scan button to run.
- **Cursor paging**: keys load 100 at a time; **Load more** continues from the SCAN cursor. The Explorer never runs `KEYS *`, so it stays safe on large databases.
- **Tree vs Flat**: Tree mode groups keys by the prefix before the first `:` (for example `app:user:1` is grouped under `app`). Flat mode lists raw key names.
- **Type strip**: shows the count of currently loaded keys per type.
- Each key row shows its **type** and **TTL** (`no expiry`, `missing`, or a human duration such as `45s`, `10m`, `2h`).

## Main Tabs

The main panel has six tabs. The active tab is disabled visually when Redis is disconnected.

### 1. Data (`▦`)

Purpose:
- Read and display the value of the selected key using type-appropriate commands.

How values are read (`readRedisValue`):

| Type | Read commands | Displayed as |
| --- | --- | --- |
| string | `GET` | text |
| hash | `HLEN`, `HGETALL` | field → value rows |
| list | `LLEN`, `LRANGE 0 limit` | indexed rows |
| set | `SCARD`, `SSCAN` | member rows |
| zset (sorted set) | `ZCARD`, `ZRANGE ... WITHSCORES` | member + score rows |
| stream | `XLEN`, `XRANGE` | entry id + fields |
| json (RedisJSON) | `JSON.GET` | parsed JSON |

Features:
- Header shows type, TTL, item count, and memory usage (`MEMORY USAGE`).
- A row filter narrows displayed rows by key or value text.
- Clicking a row opens a JSON modal with pretty-printed value and a **Copy** button. Laravel queue job payloads are detected and shown as a readable job card (job name, UUID, queue, attempts, timeout).
- Large collections are previewed and truncated; **Load more rows** raises the limit (up to 5000).
- Per-key actions: **Copy Key**, **Copy Value**, **TTL**, **Edit**, **Rename**, **Move**, **Script**, **Delete**.

### 2. Edit (`✎`)

Purpose:
- Create or modify keys, and host the Rename and Move sub-forms.

Supported writable types: `string`, `hash`, `list`, `set`, `zset`, `stream`, `json`.

How writes work (`writeRedisValue`):
- The key is first `DEL`eted, then re-created from the submitted value, then `EXPIRE`d if a TTL > 0 was given. This makes a save fully replace the key.
- Value formats expected in the editor:
  - **string**: plain text.
  - **hash**: JSON object `{ "field": "value" }` → `HSET`.
  - **list / set**: JSON array of strings → `RPUSH` / `SADD`.
  - **zset**: JSON array `[{ "member": "x", "score": 1 }]` → `ZADD`.
  - **stream**: JSON object of fields → `XADD ... *`.
  - **json**: any valid JSON → `JSON.SET key $`.
- A live **Command Preview** shows the exact Redis commands that will run.

Sub-forms:
- **Rename**: uses `RENAMENX`, which fails (409 Conflict) if the target key already exists, so existing keys are never silently overwritten.
- **Move**: uses `MOVE key <toDb>` to move a key between databases. With **Overwrite** checked, the target key is `DEL`eted first; otherwise a clash returns a conflict.
- **TTL**: a TTL > 0 runs `EXPIRE`; a TTL of 0 runs `PERSIST` (removes expiry).

### 3. Script (`⌘`)

Purpose:
- Produce a replayable, human-readable set of Redis commands and run them.

Typical workflow:
1. Generate a script from a single key (key action **Script**) or from the sidebar pattern (**Script** in sidebar / **Generate Pattern**). The backend reads each matching key and emits `DEL` + re-create commands plus `EXPIRE`.
2. Review or edit the generated commands in the text area.
3. Click **Run Script**. Commands run in order, capped at 100; execution stops at the first error and reports the failing line.

Notes:
- Lines starting with `#` or `--` are treated as comments and skipped.
- Generated scripts are useful for copying data between environments or seeding a fresh instance.

### 4. Console (`>_`)

Purpose:
- Run a single ad-hoc Redis command.

Features:
- Accepts a raw command line (for example `GET "app:user:1"`). Arguments are tokenized with quote and escape handling.
- The result is pretty-printed below the input.

Blocked commands:
- For safety, the following are rejected server-side (`validateRedisCommand`) and cannot run from the console or scripts:
  `ACL`, `CONFIG`, `DEBUG`, `EVAL`, `EVALSHA`, `FLUSHALL`, `FLUSHDB`, `FUNCTION`, `MIGRATE`, `MODULE`, `REPLICAOF`, `SCRIPT`, `SHUTDOWN`, `SLAVEOF`.

### 5. Server (`◷`)

Purpose:
- Show parsed server diagnostics from `INFO` and the slow log.

Displays:
- **Stats grid**: Redis version, uptime, memory used/peak, connected clients, ops/sec, keyspace hits/misses, total commands, evicted keys.
- **Keyspace**: per-database key counts.
- **Slow Log**: recent slow commands (`SLOWLOG GET`) with id, time, duration in ms, and the command text.

### 6. Monitor (`📡`)

Purpose:
- Tail Redis pub/sub channels live.

How it works:
- Subscribing to a plain channel uses `SUBSCRIBE`; a pattern containing `*`, `?`, or `[` uses `PSUBSCRIBE`.
- Messages stream to the browser over Server-Sent Events (SSE). The backend keeps the connection open up to 10 minutes; the UI keeps the most recent 500 messages.
- Each message row shows time, channel, and payload; clicking opens the JSON modal.

Use cases:
- Watch application events as they fire (for example `events:*`).
- Confirm a publisher is emitting on the expected channel.

## API Reference

All routes are under `/api/connections/{id}/redis/...`. The optional `db` query parameter or body field overrides the connection's default database index.

| Method | Path | Handler | Purpose |
| --- | --- | --- | --- |
| GET | `/redis/ping` | `RedisPing` | Health check + latency |
| GET | `/redis/keys?pattern=&cursor=&count=` | `RedisKeys` | SCAN keys (paged) |
| GET | `/redis/key?key=&limit=` | `RedisKeyValue` | Read one key's value |
| POST/PUT | `/redis/key` | `RedisWriteKey` | Create/replace a key |
| DELETE | `/redis/key?key=` | `RedisDeleteKey` | Delete a key |
| POST | `/redis/rename` | `RedisRenameKey` | Rename (RENAMENX) |
| POST | `/redis/move` | `RedisMoveKey` | Move key between DBs |
| POST | `/redis/command` | `RedisCommand` | Run one command |
| GET | `/redis/script?key=\|pattern=` | `RedisGenerateScript` | Generate replay script |
| POST | `/redis/script` | `RedisExecuteScript` | Run a script |
| POST | `/redis/ttl` | `RedisSetTTL` | EXPIRE / PERSIST |
| GET | `/redis/info` | `RedisInfo` | Parsed INFO |
| GET | `/redis/slowlog?count=` | `RedisSlowlog` | SLOWLOG GET |
| GET | `/redis/monitor?channel=` | `RedisMonitor` | SSE pub/sub stream |

## Notes and Safety

- The Explorer never runs `KEYS *`; it always uses cursor-based `SCAN`, so it is safe against production databases.
- Destructive server commands are blocked; the Explorer cannot flush or reconfigure the server.
- Writes replace keys (`DEL` then re-create), so editing a key resets its TTL unless you set one explicitly.
- All Redis activity is audited, including the acting user and target key.
