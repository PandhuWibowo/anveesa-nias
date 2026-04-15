import { ref } from 'vue'

// Cross-view handoff: SavedQueriesView → DataView SQL tab
export const pendingSQL = ref<string | null>(null)
