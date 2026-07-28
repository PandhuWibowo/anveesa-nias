import { ref, computed, type Ref } from 'vue'

/**
 * Client-side text filter over an already-loaded list — the `search` ref +
 * `computed` matcher shape most views already hand-roll (SftpView,
 * DockerView, KubernetesView, …). `matcher` receives the lower-cased,
 * trimmed query and decides which fields on `item` to test.
 */
export function useListFilter<T>(items: Ref<T[]>, matcher: (item: T, query: string) => boolean) {
  const search = ref('')
  const filtered = computed(() => {
    const q = search.value.trim().toLowerCase()
    if (!q) return items.value
    return items.value.filter((item) => matcher(item, q))
  })
  return { search, filtered }
}
