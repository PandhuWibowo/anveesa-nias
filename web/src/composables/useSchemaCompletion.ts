import { ref } from 'vue'
import axios from 'axios'
import type { CompletionSource, Completion, CompletionContext } from '@codemirror/autocomplete'
import { SQL_FUNCTIONS } from '@/utils/sqlFunctionHelp'

interface Column {
  name: string
  type: string
}

interface TableInfo {
  name: string
  columns: Column[]
}

interface SchemaTableSummary {
  name: string
  type: 'table' | 'view'
}

interface SchemaDatabase {
  name: string
  tables: SchemaTableSummary[]
}

interface ERForeignKey {
  constraint_name: string
  table_name: string
  column_name: string
  ref_table_name: string
  ref_column_name: string
}

interface ERResponse {
  foreign_keys: ERForeignKey[]
}

interface SchemaBundle {
  tables: TableInfo[]
  foreignKeys: ERForeignKey[]
}

const schemaCache = new Map<string, SchemaBundle>()
const loading = ref(false)

const SQL_KEYWORDS = [
  'SELECT', 'FROM', 'WHERE', 'JOIN', 'LEFT JOIN', 'RIGHT JOIN', 'INNER JOIN', 'OUTER JOIN',
  'GROUP BY', 'ORDER BY', 'HAVING', 'LIMIT', 'OFFSET', 'INSERT INTO', 'UPDATE', 'DELETE FROM',
  'VALUES', 'SET', 'CREATE TABLE', 'ALTER TABLE', 'DROP TABLE', 'DISTINCT', 'CASE', 'WHEN',
  'THEN', 'ELSE', 'END', 'AND', 'OR', 'NOT', 'NULL', 'IS NULL', 'IS NOT NULL', 'IN', 'EXISTS',
  'BETWEEN', 'LIKE', 'ILIKE', 'UNION', 'WITH', 'AS', 'ON', 'DESC', 'ASC',
]

const SQL_SNIPPETS = [
  {
    label: 'SELECT template',
    apply: 'SELECT *\nFROM table_name\nWHERE 1=1\nLIMIT 100;',
    detail: 'query skeleton',
  },
  {
    label: 'Aggregation template',
    apply: 'SELECT dimension, COUNT(*) AS total\nFROM table_name\nGROUP BY dimension\nORDER BY total DESC;',
    detail: 'group by skeleton',
  },
  {
    label: 'CTE template',
    apply: 'WITH base AS (\n  SELECT *\n  FROM table_name\n)\nSELECT *\nFROM base\nLIMIT 100;',
    detail: 'common table expression',
  },
]

async function fetchSchema(connId: number, db: string): Promise<SchemaBundle> {
  const key = `${connId}:${db}`
  if (schemaCache.has(key)) return schemaCache.get(key)!

  loading.value = true
  try {
    const [{ data: schemaDatabases }, { data: er }] = await Promise.all([
      axios.get<SchemaDatabase[]>(`/api/connections/${connId}/schema`),
      axios.get<ERResponse>(`/api/connections/${connId}/er`),
    ])

    const tables = (schemaDatabases ?? []).find((item) => item.name === db)?.tables ?? []

    const infos: TableInfo[] = []
    await Promise.all(
      (tables ?? []).map(async (t) => {
        try {
          const { data: cols } = await axios.get<Column[]>(
            `/api/connections/${connId}/schema/${encodeURIComponent(db)}/tables/${encodeURIComponent(t.name)}/columns`,
          )
          infos.push({ name: t.name, columns: cols })
        } catch {
          infos.push({ name: t.name, columns: [] })
        }
      }),
    )

    infos.sort((a, b) => a.name.localeCompare(b.name))
    const bundle = {
      tables: infos,
      foreignKeys: er?.foreign_keys ?? [],
    }
    schemaCache.set(key, bundle)
    return bundle
  } catch {
    return { tables: [], foreignKeys: [] }
  } finally {
    loading.value = false
  }
}

function parseAliases(sql: string, tables: TableInfo[]): Map<string, TableInfo> {
  const map = new Map<string, TableInfo>()
  const byName = new Map(tables.map((t) => [t.name.toLowerCase(), t] as const))
  const regex = /\b(?:from|join)\s+([a-zA-Z0-9_"`.\-]+)(?:\s+(?:as\s+)?([a-zA-Z_][a-zA-Z0-9_]*))?/gi
  let match: RegExpExecArray | null
  while ((match = regex.exec(sql)) !== null) {
    const rawTable = match[1]?.replace(/["`]/g, '') ?? ''
    const alias = match[2]
    const table = byName.get(rawTable.toLowerCase())
    if (!table) continue
    map.set(table.name, table)
    if (alias) map.set(alias.toLowerCase(), table)
  }
  return map
}

function isAfterTableKeyword(beforeCursor: string): boolean {
  return /\b(?:from|join|update|into)\s+[a-zA-Z0-9_"`.\-]*$/i.test(beforeCursor)
}

function isAfterJoinKeyword(beforeCursor: string): boolean {
  return /\bjoin\s+[a-zA-Z0-9_"`.\-]*$/i.test(beforeCursor)
}

function isAfterAliasDot(beforeCursor: string): string | null {
  const match = beforeCursor.match(/([a-zA-Z_][a-zA-Z0-9_]*)\.\w*$/)
  return match?.[1]?.toLowerCase() ?? null
}

function nextAlias(tableName: string, aliasMap: Map<string, TableInfo>): string {
  const base = tableName
    .split(/[^a-zA-Z0-9]+/)
    .filter(Boolean)
    .map((part) => part[0]?.toLowerCase() ?? '')
    .join('') || tableName[0]?.toLowerCase() || 't'
  let alias = base
  let idx = 2
  while (aliasMap.has(alias)) {
    alias = `${base}${idx++}`
  }
  return alias
}

function keywordCompletions(): Completion[] {
  return SQL_KEYWORDS.map((keyword) => ({
    label: keyword,
    type: 'keyword',
    apply: keyword.includes(' ') ? `${keyword} ` : keyword,
    boost: 1,
  }))
}

function functionCompletions(): Completion[] {
  return SQL_FUNCTIONS.map((fn) => ({
    label: fn.label,
    type: 'function',
    detail: fn.detail,
    info: fn.signature,
    apply: fn.snippet,
    boost: 5,
  }))
}

function snippetCompletions(): Completion[] {
  return SQL_SNIPPETS.map((snippet) => ({
    label: snippet.label,
    type: 'snippet',
    detail: snippet.detail,
    apply: snippet.apply,
    boost: 2,
  }))
}

function tableCompletions(tables: TableInfo[]): Completion[] {
  return tables.map((table) => ({
    label: table.name,
    type: 'class',
    detail: 'table',
    boost: 6,
  }))
}

function columnCompletions(tables: TableInfo[]): Completion[] {
  const completions: Completion[] = []
  for (const table of tables) {
    for (const column of table.columns) {
      completions.push({
        label: column.name,
        type: 'property',
        detail: `${table.name}.${column.type}`,
        boost: 4,
      })
      completions.push({
        label: `${table.name}.${column.name}`,
        type: 'property',
        detail: column.type,
        boost: 3,
      })
    }
  }
  return completions
}

function aliasColumnCompletions(aliasMap: Map<string, TableInfo>): Completion[] {
  const completions: Completion[] = []
  for (const [alias, table] of aliasMap.entries()) {
    if (alias === table.name) continue
    for (const column of table.columns) {
      completions.push({
        label: `${alias}.${column.name}`,
        type: 'property',
        detail: `${table.name}.${column.type}`,
        boost: 8,
      })
    }
  }
  return completions
}

function aliasScopedColumns(table: TableInfo, alias: string): Completion[] {
  return table.columns.map((column) => ({
    label: column.name,
    type: 'property',
    detail: `${alias}.${column.type}`,
    boost: 10,
  }))
}

function joinCompletions(foreignKeys: ERForeignKey[], aliasMap: Map<string, TableInfo>): Completion[] {
  const completions: Completion[] = []
  const seenApply = new Set<string>()

  for (const [alias, table] of aliasMap.entries()) {
    for (const fk of foreignKeys) {
      if (fk.table_name === table.name) {
        const targetAlias = nextAlias(fk.ref_table_name, aliasMap)
        const apply = `${fk.ref_table_name} ${targetAlias} ON ${alias}.${fk.column_name} = ${targetAlias}.${fk.ref_column_name}`
        if (!seenApply.has(apply)) {
          seenApply.add(apply)
          completions.push({
            label: fk.ref_table_name,
            type: 'class',
            detail: `join via ${table.name}.${fk.column_name} = ${fk.ref_table_name}.${fk.ref_column_name}`,
            info: `JOIN ${fk.ref_table_name} ${targetAlias} ON ${alias}.${fk.column_name} = ${targetAlias}.${fk.ref_column_name}`,
            apply,
            boost: 12,
          })
        }
      }
      if (fk.ref_table_name === table.name) {
        const targetAlias = nextAlias(fk.table_name, aliasMap)
        const apply = `${fk.table_name} ${targetAlias} ON ${targetAlias}.${fk.column_name} = ${alias}.${fk.ref_column_name}`
        if (!seenApply.has(apply)) {
          seenApply.add(apply)
          completions.push({
            label: fk.table_name,
            type: 'class',
            detail: `join via ${fk.table_name}.${fk.column_name} = ${table.name}.${fk.ref_column_name}`,
            info: `JOIN ${fk.table_name} ${targetAlias} ON ${targetAlias}.${fk.column_name} = ${alias}.${fk.ref_column_name}`,
            apply,
            boost: 12,
          })
        }
      }
    }
  }

  return completions
}

function buildCompletionSource(bundle: SchemaBundle): CompletionSource {
  const { tables, foreignKeys } = bundle
  const tableOpts = tableCompletions(tables)
  const keywordOpts = keywordCompletions()
  const functionOpts = functionCompletions()
  const snippetOpts = snippetCompletions()
  const allColumnOpts = columnCompletions(tables)

  return (ctx: CompletionContext) => {
    const doc = ctx.state.doc.toString()
    const beforeCursor = doc.slice(0, ctx.pos)
    const aliasMap = parseAliases(beforeCursor, tables)
    const alias = isAfterAliasDot(beforeCursor)

    if (alias) {
      const table = aliasMap.get(alias)
      if (!table) return null
      const word = ctx.matchBefore(/\w*/)
      return {
        from: word ? word.from : ctx.pos,
        options: aliasScopedColumns(table, alias),
      }
    }

    if (isAfterJoinKeyword(beforeCursor)) {
      const word = ctx.matchBefore(/[a-zA-Z0-9_"`.\-]*/)
      const joinOpts = joinCompletions(foreignKeys, aliasMap)
      return {
        from: word ? word.from : ctx.pos,
        options: joinOpts.length ? joinOpts : tableOpts,
      }
    }

    if (isAfterTableKeyword(beforeCursor)) {
      const word = ctx.matchBefore(/[a-zA-Z0-9_"`.\-]*/)
      return {
        from: word ? word.from : ctx.pos,
        options: tableOpts,
      }
    }

    const word = ctx.matchBefore(/[\w."]*/)
    if (!word || (word.from === word.to && !ctx.explicit)) return null

    return {
      from: word.from,
      options: [
        ...snippetOpts,
        ...keywordOpts,
        ...functionOpts,
        ...tableOpts,
        ...joinCompletions(foreignKeys, aliasMap),
        ...aliasColumnCompletions(aliasMap),
        ...allColumnOpts,
      ],
    }
  }
}

export function useSchemaCompletion() {
  async function getCompletionSource(
    connId: number | null,
    db: string,
  ): Promise<CompletionSource | null> {
    if (!connId || !db) return null
    const bundle = await fetchSchema(connId, db)
    return buildCompletionSource(bundle)
  }

  function invalidateCache(connId: number) {
    for (const key of schemaCache.keys()) {
      if (key.startsWith(`${connId}:`)) schemaCache.delete(key)
    }
  }

  return { getCompletionSource, invalidateCache, loading }
}
