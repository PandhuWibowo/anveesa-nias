export interface ReadableErrorOptions {
  fallback?: string
  action?: string
}

type ErrorPayload = {
  error?: unknown
  message?: unknown
  detail?: unknown
  details?: unknown
  code?: unknown
  reason?: unknown
  hint?: unknown
  field?: unknown
  fields?: unknown
  column?: unknown
  columns?: unknown
  attribute?: unknown
  attributes?: unknown
  path?: unknown
  param?: unknown
  parameter?: unknown
  constraint?: unknown
}

function stringifyMessage(value: unknown): string {
  if (typeof value === 'string') return value.trim()
  if (Array.isArray(value)) return value.map(stringifyMessage).filter(Boolean).join(', ')
  if (value && typeof value === 'object') {
    try {
      return JSON.stringify(value)
    } catch {
      return ''
    }
  }
  return value == null ? '' : String(value).trim()
}

function payloadMessage(payload: unknown): string {
  if (!payload) return ''
  if (typeof payload === 'string') return payload.trim()
  if (typeof payload === 'object') {
    const data = payload as ErrorPayload
    const parts = [
      stringifyMessage(data.error),
      stringifyMessage(data.message),
      stringifyMessage(data.detail),
      stringifyMessage(data.details),
      stringifyMessage(data.reason),
      stringifyMessage(data.hint),
    ].filter(Boolean)
    if (parts.length) return [...new Set(parts)].join('\n')
    return stringifyMessage(payload)
  }
  return stringifyMessage(payload)
}

function payloadFieldContext(payload: unknown): string {
  if (!payload || typeof payload !== 'object') return ''
  const data = payload as ErrorPayload
  const values = [
    stringifyMessage(data.field),
    stringifyMessage(data.fields),
    stringifyMessage(data.column),
    stringifyMessage(data.columns),
    stringifyMessage(data.attribute),
    stringifyMessage(data.attributes),
    stringifyMessage(data.path),
    stringifyMessage(data.param),
    stringifyMessage(data.parameter),
  ].filter(Boolean)
  return [...new Set(values)].join(', ')
}

function payloadConstraint(payload: unknown): string {
  if (!payload || typeof payload !== 'object') return ''
  return stringifyMessage((payload as ErrorPayload).constraint)
}

function inferFieldContext(message: string): string {
  const patterns = [
    /column ["'`]?([^"'`\n]+?)["'`]?(?:\s|$)/i,
    /field ["'`]?([^"'`\n]+?)["'`]?(?:\s|$)/i,
    /attribute ["'`]?([^"'`\n]+?)["'`]?(?:\s|$)/i,
    /key \(([^)]+)\)=/i,
    /unknown column ["'`]([^"'`]+)["'`]/i,
    /duplicate entry .* for key ["'`]([^"'`]+)["'`]/i,
  ]
  for (const pattern of patterns) {
    const match = message.match(pattern)
    if (match?.[1]) return match[1].trim()
  }
  return ''
}

function inferConstraint(message: string): string {
  const match = message.match(/constraint ["'`]?([^"'`\n]+?)["'`]?(?:\s|$)/i)
  return match?.[1]?.trim() ?? ''
}

function statusLine(status?: number, statusText?: string) {
  return status ? `HTTP ${status}${statusText ? ` ${statusText}` : ''}` : ''
}

function withContext(
  message: string,
  status?: number,
  statusText?: string,
  action?: string,
  code?: string,
  fieldContext?: string,
  constraint?: string,
) {
  const statusLabel = status ? `HTTP ${status}${statusText ? ` ${statusText}` : ''}` : ''
  const title = action ? `${action} failed` : 'Request failed'
  const lines = [title]
  if (statusLabel) lines.push(`Status: ${statusLabel}`)
  if (code) lines.push(`Code: ${code}`)
  if (fieldContext) lines.push(`Field/column: ${fieldContext}`)
  if (constraint) lines.push(`Constraint: ${constraint}`)
  if (message) lines.push(`Detail: ${message}`)
  if (!message && !statusLabel && !code) lines.push('Detail: Request failed')
  return lines.join('\n')
}

export function readableError(error: unknown, options: ReadableErrorOptions | string = {}): string {
  const opts: ReadableErrorOptions = typeof options === 'string' ? { fallback: options } : options
  const fallback = opts.fallback ?? 'Request failed'

  if (!error) return withContext(fallback, undefined, undefined, opts.action)

  const err = error as {
    code?: string
    name?: string
    message?: string
    response?: { status?: number; statusText?: string; data?: unknown }
    request?: unknown
  }

  if (err.code === 'ERR_CANCELED' || err.name === 'AbortError') {
    return withContext('Request was cancelled.', undefined, undefined, opts.action)
  }

  const responseMessage = payloadMessage(err.response?.data)
  if (responseMessage || err.response?.status) {
    const payload = err.response?.data as ErrorPayload | undefined
    const code = stringifyMessage(payload?.code)
    const fieldContext = payloadFieldContext(payload) || inferFieldContext(responseMessage)
    const constraint = payloadConstraint(payload) || inferConstraint(responseMessage)
    return withContext(responseMessage || fallback, err.response?.status, err.response?.statusText, opts.action, code, fieldContext, constraint)
  }

  if (err.request) {
    return withContext('Server did not respond. Check that the API is running and the connection is reachable.', undefined, undefined, opts.action, err.code)
  }

  const message = err.message || stringifyMessage(error) || fallback
  return withContext(message, undefined, undefined, opts.action, err.code, inferFieldContext(message), inferConstraint(message))
}

export async function readableFetchError(response: Response, fallback = 'Request failed'): Promise<string> {
  let payload: unknown = null
  const text = await response.text().catch(() => '')
  if (text) {
    try {
      payload = JSON.parse(text)
    } catch {
      payload = text
    }
  }
  const message = payloadMessage(payload) || fallback
  return withContext(message, response.status, response.statusText, undefined, undefined, payloadFieldContext(payload) || inferFieldContext(message), payloadConstraint(payload))
}
