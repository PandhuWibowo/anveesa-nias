import { ref } from 'vue'

export interface PendingAIAnalyticsRequest {
  connId: number
  title?: string
  question?: string
  sql?: string
  source?: 'saved_query' | 'query_result'
}

export const pendingAIAnalytics = ref<PendingAIAnalyticsRequest | null>(null)
