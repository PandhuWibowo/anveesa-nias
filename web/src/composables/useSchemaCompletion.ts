import { ref } from 'vue'
import axios from 'axios'
import type { CompletionSource, Completion } from '@codemirror/autocomplete'

interface Column {
  name: string
  type: string
}

interface TableInfo {
  name: string
  columns: Column[]
}

const schemaCache = new Map<string, TableInfo[]>()
const loading = ref(false)

async function fetchSchema(connId: number, db: string): Promise<TableInfo[]> {
  const key = `${connId}:${db}`
  if (schemaCache.has(key)) return schemaCache.get(key)!

  loading.value = true
  try {
    const { data: tables } = await axios.get<string[]>(`/api/connections/${connId}/schema/${db}`)
    const infos: TableInfo[] = []
    await Promise.all(
      tables.map(async (t) => {
        try {
          const { data: cols } = await axios.get<Column[]>(
            `/api/connections/${connId}/schema/${db}/tables/${t}/columns`,
          )
          infos.push({ name: t, columns: cols })
        } catch {
          infos.push({ name: t, columns: [] })
        }
      }),
    )
    schemaCache.set(key, infos)
    return infos
  } catch {
    return []
  } finally {
    loading.value = false
  }
}

function buildCompletionSource(tables: TableInfo[]): CompletionSource {
  const tableNames: Completion[] = tables.map((t) => ({
    label: t.name,
    type: 'type',
    detail: 'table',
    boost: 5,
  }))

  const colCompletions: Completion[] = []
  for (const t of tables) {
    for (const c of t.columns) {
      colCompletions.push({
        label: c.name,
        type: 'property',
        detail: `${t.name}.${c.type}`,
      })
    }
  }

  return (ctx) => {
    const word = ctx.matchBefore(/\w*/)
    if (!word || (word.from === word.to && !ctx.explicit)) return null
    return {
      from: word.from,
      options: [...tableNames, ...colCompletions],
    }
  }
}

export function useSchemaCompletion() {
  async function getCompletionSource(
    connId: number | null,
    db: string,
  ): Promise<CompletionSource | null> {
    if (!connId || !db) return null
    const tables = await fetchSchema(connId, db)
    if (!tables.length) return null
    return buildCompletionSource(tables)
  }

  function invalidateCache(connId: number) {
    for (const key of schemaCache.keys()) {
      if (key.startsWith(`${connId}:`)) schemaCache.delete(key)
    }
  }

  return { getCompletionSource, invalidateCache, loading }
}
