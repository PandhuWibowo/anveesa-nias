import { ref } from 'vue'

export interface PendingDashboardBlock {
  savedQueryId: number
  title: string
}

export const pendingDashboardBlock = ref<PendingDashboardBlock | null>(null)
