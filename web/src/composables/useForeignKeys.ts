import { ref } from 'vue'
import axios from 'axios'

interface FKInfo {
  fromTable: string
  fromCol: string
  toTable: string
  toCol: string
}

const cache = new Map<string, FKInfo[]>()

export function useForeignKeys() {
  const fks = ref<FKInfo[]>([])
  const loading = ref(false)

  async function fetchFKs(connId: number, database?: string) {
    const key = `${connId}::${database ?? ''}`
    if (cache.has(key)) {
      fks.value = cache.get(key)!
      return
    }
    loading.value = true
    try {
      const { data } = await axios.get<{ tables: Array<{
        name: string
        foreign_keys: Array<{ column: string; references_table: string; references_column: string }>
      }> }>(`/api/connections/${connId}/er`)
      const result: FKInfo[] = []
      for (const table of data.tables ?? []) {
        for (const fk of table.foreign_keys ?? []) {
          result.push({
            fromTable: table.name,
            fromCol: fk.column,
            toTable: fk.references_table,
            toCol: fk.references_column,
          })
        }
      }
      cache.set(key, result)
      fks.value = result
    } catch {
      fks.value = []
    } finally {
      loading.value = false
    }
  }

  function isFKColumn(table: string, column: string): FKInfo | null {
    return fks.value.find((f) => f.fromTable === table && f.fromCol === column) ?? null
  }

  return { fks, loading, fetchFKs, isFKColumn }
}
