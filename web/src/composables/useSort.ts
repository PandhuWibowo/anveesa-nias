import { ref } from 'vue'

export type SortDir = 'asc' | 'desc'

function compare(av: unknown, bv: unknown, dir: SortDir): number {
  const mul = dir === 'asc' ? 1 : -1
  if (typeof av === 'number' && typeof bv === 'number') return (av - bv) * mul
  return String(av ?? '').localeCompare(String(bv ?? ''), undefined, { numeric: true }) * mul
}

/**
 * Client-side sort state for a list of objects/records. `getter` picks the
 * comparable value for a given sort key off an item — pass the field
 * directly if items are already flat records.
 */
export function useSort<T>(getter: (item: T, key: string) => unknown, defaultKey = '') {
  const sortKey = ref(defaultKey)
  const sortDir = ref<SortDir>('asc')

  function toggleSort(key: string) {
    if (sortKey.value === key) {
      sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc'
    } else {
      sortKey.value = key
      sortDir.value = 'asc'
    }
  }

  function sort(items: T[]): T[] {
    if (!sortKey.value) return items
    return [...items].sort((a, b) => compare(getter(a, sortKey.value), getter(b, sortKey.value), sortDir.value))
  }

  return { sortKey, sortDir, toggleSort, sort }
}

/**
 * Sorts DataTable-style matrix rows (`unknown[][]`, one column-label per
 * index) by a column label — the shape needed by read-only DataTable
 * consumers (e.g. SchemaView.vue) that receive its `@sort(col, dir)` event
 * and must reorder the rows themselves, since DataTable never sorts
 * `props.rows` on its own (server-paginated consumers sort by re-querying).
 */
export function sortMatrixRows(columns: string[], rows: unknown[][], sortCol: string, sortDir: SortDir): unknown[][] {
  const idx = columns.indexOf(sortCol)
  if (idx < 0) return rows
  return [...rows].sort((a, b) => compare(a[idx], b[idx], sortDir))
}
