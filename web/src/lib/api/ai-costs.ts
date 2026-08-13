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

const emptyDashboard = (): AICostDashboard => ({
  spent_today: 0,
  spent_week: 0,
  estimated_daily: 0,
  projected_7d: 0,
  wallet_balance_usdc: 0,
  runway_days: 0,
  call_count_today: 0,
  call_count_week: 0,
  by_source: {},
})

/** Sum spend metrics across traders sharing one claw402 wallet/model. */
export function sumSpendDashboards(
  items: AICostDashboard[]
): AICostDashboard {
  if (items.length === 0) return emptyDashboard()
  return items.reduce(
    (acc, item) => ({
      spent_today: acc.spent_today + item.spent_today,
      spent_week: acc.spent_week + item.spent_week,
      estimated_daily: acc.estimated_daily + item.estimated_daily,
      projected_7d: acc.projected_7d + item.projected_7d,
      wallet_balance_usdc: Math.max(
        acc.wallet_balance_usdc,
        item.wallet_balance_usdc
      ),
      runway_days: Math.max(acc.runway_days, item.runway_days),
      call_count_today: acc.call_count_today + item.call_count_today,
      call_count_week: acc.call_count_week + item.call_count_week,
      by_source: acc.by_source,
    }),
    emptyDashboard()
  )
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

  /** Aggregate spend for all traders bound to one AI model (claw402). */
  async getDashboardForTraders(
    traderIds: string[],
    silent?: boolean
  ): Promise<AICostDashboard> {
    if (traderIds.length === 0) return emptyDashboard()
    const results = await Promise.all(
      traderIds.map((id) =>
        aiCostsApi.getDashboard(id, silent).catch(() => emptyDashboard())
      )
    )
    return sumSpendDashboards(results)
  },
}
