export interface SQLFunctionSpec {
  label: string
  snippet: string
  detail: string
  signature: string
}

export const SQL_FUNCTIONS: SQLFunctionSpec[] = [
  { label: 'COUNT', snippet: 'COUNT(*)', detail: 'aggregate', signature: 'COUNT(expr | *)' },
  { label: 'SUM', snippet: 'SUM(column)', detail: 'aggregate', signature: 'SUM(column)' },
  { label: 'AVG', snippet: 'AVG(column)', detail: 'aggregate', signature: 'AVG(column)' },
  { label: 'MIN', snippet: 'MIN(column)', detail: 'aggregate', signature: 'MIN(column)' },
  { label: 'MAX', snippet: 'MAX(column)', detail: 'aggregate', signature: 'MAX(column)' },
  { label: 'COALESCE', snippet: 'COALESCE(expr, fallback)', detail: 'null handling', signature: 'COALESCE(expr, fallback)' },
  { label: 'DATE_TRUNC', snippet: "DATE_TRUNC('day', timestamp_column)", detail: 'time bucket', signature: "DATE_TRUNC('grain', timestamp)" },
  { label: 'ROUND', snippet: 'ROUND(value, 2)', detail: 'numeric', signature: 'ROUND(value, digits)' },
  { label: 'CAST', snippet: 'CAST(expr AS type)', detail: 'conversion', signature: 'CAST(expr AS type)' },
  { label: 'LOWER', snippet: 'LOWER(text_value)', detail: 'text', signature: 'LOWER(text)' },
  { label: 'UPPER', snippet: 'UPPER(text_value)', detail: 'text', signature: 'UPPER(text)' },
  { label: 'NOW', snippet: 'NOW()', detail: 'datetime', signature: 'NOW()' },
]

const functionMap = new Map(SQL_FUNCTIONS.map((fn) => [fn.label, fn]))

export function getFunctionSignature(name: string): string | null {
  return functionMap.get(name.toUpperCase())?.signature ?? null
}

export function getActiveFunctionHint(sql: string, cursorPos: number): string | null {
  const before = sql.slice(0, cursorPos)
  let depth = 0

  for (let i = before.length - 1; i >= 0; i--) {
    const ch = before[i]
    if (ch === ')') {
      depth++
      continue
    }
    if (ch === '(') {
      if (depth > 0) {
        depth--
        continue
      }

      let j = i - 1
      while (j >= 0 && /\s/.test(before[j])) j--
      let end = j + 1
      while (j >= 0 && /[A-Za-z_]/.test(before[j])) j--
      const fnName = before.slice(j + 1, end).toUpperCase()
      return getFunctionSignature(fnName)
    }
  }

  return null
}
