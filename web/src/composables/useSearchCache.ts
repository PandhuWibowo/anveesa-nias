interface CacheEntry {
  data: any
  expiresAt: number
}

const store = new Map<string, CacheEntry>()

const TTL = {
  info: 30_000,       // 30s  — cluster info + latency
  indices: 60_000,    // 60s  — index list
  policies: 300_000,  // 5min — ILM, templates
  settings: 120_000,  // 2min — shard/index settings
  appPolicies: 30_000, // 30s — app policies (change frequently after run)
}

export type SearchCacheTTL = keyof typeof TTL

export function useSearchCache() {
  function key(connId: number, resource: string) {
    return `search:${connId}:${resource}`
  }

  function get<T>(connId: number, resource: string): T | null {
    const entry = store.get(key(connId, resource))
    if (!entry) return null
    if (Date.now() > entry.expiresAt) {
      store.delete(key(connId, resource))
      return null
    }
    return entry.data as T
  }

  function set(connId: number, resource: string, data: any, ttl: SearchCacheTTL | number) {
    const ttlMs = typeof ttl === 'number' ? ttl : TTL[ttl]
    store.set(key(connId, resource), { data, expiresAt: Date.now() + ttlMs })
  }

  function invalidate(connId: number, ...resources: string[]) {
    for (const resource of resources) {
      store.delete(key(connId, resource))
    }
  }

  function invalidateAll(connId: number) {
    const prefix = `search:${connId}:`
    for (const k of store.keys()) {
      if (k.startsWith(prefix)) store.delete(k)
    }
  }

  function isFresh(connId: number, resource: string): boolean {
    const entry = store.get(key(connId, resource))
    return !!entry && Date.now() <= entry.expiresAt
  }

  return { get, set, invalidate, invalidateAll, isFresh, TTL }
}
