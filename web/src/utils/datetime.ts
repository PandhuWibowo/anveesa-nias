const dbTimestampPattern = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)?$/

export function parseServerTimestamp(value: string): Date {
  if (!value) return new Date(NaN)
  if (dbTimestampPattern.test(value)) {
    return new Date(value.replace(' ', 'T') + 'Z')
  }
  return new Date(value)
}

export function formatServerTimestamp(value: string): string {
  const dt = parseServerTimestamp(value)
  if (Number.isNaN(dt.getTime())) return value || '—'
  return dt.toLocaleString()
}
