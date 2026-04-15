<script setup lang="ts">
import { ref, computed } from 'vue'
import type { Connection } from '@/composables/useConnections'

const props = defineProps<{
  show: boolean
  sql: string
  connection: Connection | null
}>()
const emit = defineEmits<{ close: [] }>()

type Lang = 'python' | 'javascript' | 'go' | 'curl' | 'php'
const lang = ref<Lang>('python')

const langs: Array<{ key: Lang; label: string }> = [
  { key: 'python', label: 'Python' },
  { key: 'javascript', label: 'JavaScript' },
  { key: 'go', label: 'Go' },
  { key: 'curl', label: 'cURL' },
  { key: 'php', label: 'PHP' },
]

const conn = computed(() => props.connection)

const code = computed(() => {
  const sql = props.sql.trim()
  const c = conn.value
  if (!c) return '// No connection selected'

  switch (lang.value) {
    case 'python':
      return generatePython(sql, c)
    case 'javascript':
      return generateJS(sql, c)
    case 'go':
      return generateGo(sql, c)
    case 'curl':
      return generateCurl(sql)
    case 'php':
      return generatePHP(sql, c)
    default:
      return ''
  }
})

function generatePython(sql: string, c: Connection) {
  const escaped = sql.replace(/"""/g, '\\"\\"\\"')
  switch (c.driver) {
    case 'postgres':
      return `import psycopg2

conn = psycopg2.connect(
    host="${c.host}",
    port=${c.port || 5432},
    dbname="${c.database}",
    user="${c.username}",
    password="YOUR_PASSWORD"
)
cur = conn.cursor()
cur.execute("""
${escaped}
""")
rows = cur.fetchall()
for row in rows:
    print(row)
cur.close()
conn.close()`
    case 'mysql':
      return `import mysql.connector

conn = mysql.connector.connect(
    host="${c.host}",
    port=${c.port || 3306},
    database="${c.database}",
    user="${c.username}",
    password="YOUR_PASSWORD"
)
cursor = conn.cursor()
cursor.execute("""${escaped}""")
for row in cursor.fetchall():
    print(row)
cursor.close()
conn.close()`
    case 'sqlite':
      return `import sqlite3

conn = sqlite3.connect("${c.database}")
cursor = conn.cursor()
cursor.execute("""${escaped}""")
for row in cursor.fetchall():
    print(row)
conn.close()`
    default:
      return `# Driver: ${c.driver}\nimport pyodbc\n# Configure your connection string\ncursor.execute("""${escaped}""")`
  }
}

function generateJS(sql: string, c: Connection) {
  const escaped = sql.replace(/`/g, '\\`')
  switch (c.driver) {
    case 'postgres':
      return `import { Pool } from 'pg'

const pool = new Pool({
  host: '${c.host}',
  port: ${c.port || 5432},
  database: '${c.database}',
  user: '${c.username}',
  password: process.env.DB_PASSWORD,
})

const { rows } = await pool.query(\`${escaped}\`)
console.log(rows)
await pool.end()`
    case 'mysql':
      return `import mysql from 'mysql2/promise'

const conn = await mysql.createConnection({
  host: '${c.host}',
  port: ${c.port || 3306},
  database: '${c.database}',
  user: '${c.username}',
  password: process.env.DB_PASSWORD,
})

const [rows] = await conn.execute(\`${escaped}\`)
console.log(rows)
await conn.end()`
    default:
      return `// Driver: ${c.driver}\nconst rows = await db.query(\`${escaped}\`)`
  }
}

function generateGo(sql: string, c: Connection) {
  let importPkg = ''
  let dsn = ''
  switch (c.driver) {
    case 'postgres':
      importPkg = '_ "github.com/lib/pq"'
      dsn = `"host=${c.host} port=${c.port || 5432} dbname=${c.database} user=${c.username} password=YOUR_PASSWORD sslmode=disable"`
      break
    case 'mysql':
      importPkg = '_ "github.com/go-sql-driver/mysql"'
      dsn = `"${c.username}:YOUR_PASSWORD@tcp(${c.host}:${c.port || 3306})/${c.database}"`
      break
    case 'sqlite':
      importPkg = '_ "modernc.org/sqlite"'
      dsn = `"${c.database}"`
      break
    default:
      importPkg = '// import your driver'
      dsn = '"your-dsn"'
  }
  return `package main

import (
    "database/sql"
    "fmt"
    "log"
    ${importPkg}
)

func main() {
    db, err := sql.Open("${c.driver === 'sqlite' ? 'sqlite' : c.driver}", ${dsn})
    if err != nil { log.Fatal(err) }
    defer db.Close()

    rows, err := db.Query(\`${sql}\`)
    if err != nil { log.Fatal(err) }
    defer rows.Close()

    cols, _ := rows.Columns()
    fmt.Println(cols)
    for rows.Next() {
        vals := make([]interface{}, len(cols))
        ptrs := make([]interface{}, len(cols))
        for i := range vals { ptrs[i] = &vals[i] }
        rows.Scan(ptrs...)
        fmt.Println(vals)
    }
}`
}

function generateCurl(sql: string) {
  return `curl -X POST http://localhost:8080/api/connections/1/query \\
  -H "Content-Type: application/json" \\
  -d '{"sql": ${JSON.stringify(sql)}}'`
}

function generatePHP(sql: string, c: Connection) {
  const escaped = sql.replace(/'/g, "\\'")
  switch (c.driver) {
    case 'postgres':
      return `<?php
$conn = pg_connect("host=${c.host} port=${c.port || 5432} dbname=${c.database} user=${c.username} password=YOUR_PASSWORD");
$result = pg_query($conn, '${escaped}');
while ($row = pg_fetch_assoc($result)) {
    print_r($row);
}
pg_close($conn);`
    case 'mysql':
      return `<?php
$mysqli = new mysqli("${c.host}", "${c.username}", "YOUR_PASSWORD", "${c.database}", ${c.port || 3306});
$result = $mysqli->query('${escaped}');
while ($row = $result->fetch_assoc()) {
    print_r($row);
}
$mysqli->close();`
    default:
      return `<?php\n// Driver: ${c.driver}\n$pdo = new PDO('dsn', 'user', 'pass');\n$stmt = $pdo->query('${escaped}');\nprint_r($stmt->fetchAll(PDO::FETCH_ASSOC));`
  }
}

async function copy() {
  await navigator.clipboard.writeText(code.value)
}
</script>

<template>
  <Teleport to="body">
    <div v-if="show" class="ec-overlay" @click.self="emit('close')">
      <div class="ec-modal">
        <div class="ec-header">
          <span class="ec-title">Export as Code</span>
          <div class="ec-lang-tabs">
            <button
              v-for="l in langs" :key="l.key"
              class="ec-lang-btn"
              :class="{ 'ec-lang-btn--active': lang === l.key }"
              @click="lang = l.key"
            >{{ l.label }}</button>
          </div>
          <div style="flex:1"/>
          <button class="ec-btn" @click="copy">Copy</button>
          <button class="ec-close" @click="emit('close')">×</button>
        </div>
        <div class="ec-body">
          <pre class="ec-pre"><code>{{ code }}</code></pre>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.ec-overlay {
  position: fixed; inset: 0; background: rgba(0,0,0,0.55);
  display: flex; align-items: center; justify-content: center; z-index: 1100;
}
.ec-modal {
  background: var(--bg-elevated); border: 1px solid var(--border);
  border-radius: 10px; width: min(760px, 94vw); max-height: 82vh;
  display: flex; flex-direction: column;
  box-shadow: 0 24px 64px rgba(0,0,0,0.55);
}
.ec-header {
  display: flex; align-items: center; gap: 8px;
  padding: 12px 16px; border-bottom: 1px solid var(--border);
  flex-wrap: wrap;
}
.ec-title { font-size: 13px; font-weight: 700; color: var(--text-primary); }
.ec-lang-tabs { display: flex; gap: 4px; }
.ec-lang-btn {
  padding: 3px 10px; border-radius: 5px; border: 1px solid var(--border);
  background: transparent; color: var(--text-muted); font-size: 11.5px;
  cursor: pointer; transition: all 0.12s;
}
.ec-lang-btn--active { background: var(--brand); color: #fff; border-color: var(--brand); }
.ec-lang-btn:not(.ec-lang-btn--active):hover { background: var(--bg-hover); color: var(--text-primary); }
.ec-btn {
  padding: 4px 12px; border-radius: 5px; border: 1px solid var(--border);
  background: transparent; color: var(--text-secondary); font-size: 12px;
  cursor: pointer; transition: all 0.12s;
}
.ec-btn:hover { background: var(--bg-hover); }
.ec-close {
  background: transparent; border: none; font-size: 20px;
  color: var(--text-muted); cursor: pointer; padding: 0 4px; line-height: 1;
}
.ec-body { flex: 1; min-height: 0; overflow: auto; padding: 0; }
.ec-pre {
  margin: 0; padding: 20px 24px;
  font-family: "JetBrains Mono", "Fira Mono", monospace;
  font-size: 12.5px; line-height: 1.65;
  color: var(--text-primary);
  white-space: pre; overflow: visible;
}
</style>
