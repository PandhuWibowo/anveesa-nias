import { ref, computed, watch, type Ref } from 'vue'

/**
 * Client-side pagination — slices an already-loaded array. For server-driven
 * pagination (the view fetches one page at a time), don't use this; just
 * track `page`/`totalPages` yourself and drive <Pagination> directly.
 *
 * Resets to page 1 whenever the underlying `items` array is replaced (e.g. a
 * filter changed) — if you need to preserve the current page across an
 * in-place refresh, keep the array reference stable.
 */
export function usePagination<T>(items: Ref<T[]>, initialPageSize = 25) {
  const page = ref(1)
  const pageSize = ref(initialPageSize)
  const total = computed(() => items.value.length)
  const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))

  watch(items, () => { page.value = 1 })
  watch(pageSize, () => { page.value = 1 })
  watch(totalPages, (tp) => { if (page.value > tp) page.value = tp })

  const paged = computed(() => {
    const start = (page.value - 1) * pageSize.value
    return items.value.slice(start, start + pageSize.value)
  })

  function setPage(p: number) {
    page.value = Math.min(Math.max(1, p), totalPages.value)
  }

  return { page, pageSize, total, totalPages, paged, setPage }
}
