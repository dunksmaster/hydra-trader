import { httpClient, API_BASE } from './helpers'

export interface AICostDashboard {
  spent_today: number
  spent_week: number
  estimated_daily: number
  projected_7d: number
  wallet_balance_usdc: number
  runway_days: number
  call_count_today: number
  call_count_week: number
  by_source: Record<string, number>
}

export const aiCostsApi = {
  async getDashboard(
    traderId: string,
    silent?: boolean
  ): Promise<AICostDashboard> {
    const params = new URLSearchParams({ trader_id: traderId })
    const result = await httpClient.request<AICostDashboard>(
      `${API_BASE}/ai-costs/dashboard?${params}`,
      { silent }
    )
    if (!result.success) {
      throw new Error('Failed to fetch AI spend dashboard')
    }
    return result.data!
  },
}
