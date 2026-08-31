// Splits SQL text into individual statements on unquoted semicolons —
// tracks single/double-quoted strings and skips "-- line comments" so a ';'
// inside either never counts as a statement boundary. Same core algorithm as
// QueryEditor.vue's findStatementRanges() (which needs offsets for its
// active-statement highlight), factored out here as a plain string splitter
// so SQLPanel.vue can cheaply answer "does this resolved SQL contain one
// statement or several" before deciding whether to hit the single-result
// /query endpoint or the multi-statement /script endpoint.
//
// This doesn't need to be as strict as the backend's splitter (which also
// has to be dollar-quote/block-comment aware to safely execute the split
// statements) — worst case here is just picking the less-specific endpoint
// for an edge case the backend then correctly re-merges, not a correctness
// bug in what actually runs.
export function splitSQLStatements(full: string): string[] {
  const ranges: Array<{ from: number; to: number }> = []
  let inSingle = false
  let inDouble = false
  let stmtStart = 0
  for (let i = 0; i < full.length; i++) {
    const ch = full[i]
    if (!inSingle && !inDouble && ch === '-' && full[i + 1] === '-') {
      const nl = full.indexOf('\n', i)
      i = nl === -1 ? full.length : nl
      continue
    }
    if (ch === "'" && !inDouble) inSingle = !inSingle
    else if (ch === '"' && !inSingle) inDouble = !inDouble
    else if (ch === ';' && !inSingle && !inDouble) {
      ranges.push({ from: stmtStart, to: i })
      stmtStart = i + 1
    }
  }
  ranges.push({ from: stmtStart, to: full.length })
  return ranges
    .map(r => full.slice(r.from, r.to).trim())
    .filter(hasSQLContent)
}

// True unless s is empty or nothing but comments — e.g. a trailing "-- note"
// left after the last real statement's ';'. Without this, "SELECT 1; --
// done" counts as two statements and gets routed through the multi-result
// /script endpoint instead of the normal single-result /query endpoint for
// what is, in effect, still just one statement.
function hasSQLContent(stmt: string): boolean {
  let s = stmt.trim()
  for (;;) {
    if (s.startsWith(';')) {
      s = s.slice(1).trim()
    } else if (s.startsWith('/*')) {
      const end = s.indexOf('*/')
      if (end < 0) return false
      s = s.slice(end + 2).trim()
    } else if (s.startsWith('--')) {
      const end = s.search(/[\r\n]/)
      if (end < 0) return false
      s = s.slice(end + 1).trim()
    } else {
      return s.length > 0
    }
  }
}
