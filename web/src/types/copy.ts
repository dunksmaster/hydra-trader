import type { CopyStrategyConfig } from './strategy'

export interface CopyBotPosition {
  symbol: string
  side: string
  quantity: number
  entry_price: number
  mark_price: number
  unrealized_pnl: number
  leverage: number
}

export interface CopyBotStats {
  trade_count: number
  win_rate: number
  profit_factor: number
  total_pnl: number
  total_pnl_pct: number
}

export interface CopyBotRow {
  trader_id: string
  trader_name: string
  exchange: string
  exchange_id: string
  is_running: boolean
  strategy_id: string
  strategy_name: string
  strategy_type: string
  copy_config?: CopyStrategyConfig
  account?: Record<string, number>
  positions: CopyBotPosition[]
  stats?: CopyBotStats
  last_decision?: string
}

export interface CopyBotsWalletSummary {
  equity: number
  available: number
  unrealized_pnl: number
  total_pnl: number
  open_legs: number
  wallet_slots: number
  margin_used_pct: number
}

export interface CopyBotsResponse {
  profile: 'current' | 'layer1' | string
  wallet: CopyBotsWalletSummary
  summary: {
    copy_bot_count: number
    live_count: number
    paused_count: number
  }
  bots: CopyBotRow[]
}
