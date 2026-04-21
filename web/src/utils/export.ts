export function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

export function downloadCSV(columns: string[], rows: unknown[][], name = 'export') {
  const escape = (v: unknown): string => {
    if (v === null || v === undefined) return ''
    const s = String(v)
    if (s.includes(',') || s.includes('"') || s.includes('\n')) {
      return '"' + s.replace(/"/g, '""') + '"'
    }
    return s
  }
  const lines = [columns.map(escape).join(',')]
  for (const row of rows) {
    lines.push((row as unknown[]).map(escape).join(','))
  }
  const blob = new Blob([lines.join('\n')], { type: 'text/csv;charset=utf-8;' })
  downloadBlob(blob, `${name}.csv`)
}

export function downloadJSON(columns: string[], rows: unknown[][], name = 'export') {
  const data = rows.map((row) => {
    const obj: Record<string, unknown> = {}
    ;(row as unknown[]).forEach((v, i) => { obj[columns[i]] = v })
    return obj
  })
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
  downloadBlob(blob, `${name}.json`)
}
