import { useMemo } from 'react'
import { Copy, ExternalLink, Layers, PauseCircle, PlayCircle } from 'lucide-react'
import useSWR from 'swr'
import { Link, useSearchParams } from 'react-router-dom'
import { api } from '../lib/api'
import type { CopyBotRow } from '../types/copy'
import type { DecisionRecord, PositionHistoryResponse } from '../types'
import { DeepVoidBackground } from '../components/common/DeepVoidBackground'
import { buildDashboardPath, ROUTES } from '../router/paths'
import { useLanguage } from '../contexts/LanguageContext'

function fmtUsd(value?: number) {
  if (value == null || Number.isNaN(value)) return '—'
  const sign = value >= 0 ? '+' : ''
  return `${sign}$${value.toFixed(2)}`
}

function fmtPct(value?: number) {
  if (value == null || Number.isNaN(value)) return '—'
  const sign = value >= 0 ? '+' : ''
  return `${sign}${value.toFixed(1)}%`
}

function shortAddr(addr?: string) {
  if (!addr) return '—'
  if (addr.length <= 14) return addr
  return `${addr.slice(0, 6)}…${addr.slice(-4)}`
}

function layerLabel(layer?: number) {
  if (!layer || layer <= 0) return 'L2'
  return `L${layer}`
}

function botStatus(bot: CopyBotRow) {
  if (bot.copy_config?.copy_paused || (bot.copy_config?.copy_layer ?? 0) >= 3) {
    return 'PAUSED'
  }
  return bot.is_running ? 'RUNNING' : 'STOPPED'
}

function CopyBotCard({
  bot,
  selected,
  onSelect,
}: {
  bot: CopyBotRow
  selected: boolean
  onSelect: () => void
}) {
  const cfg = bot.copy_config
  const unrealized = bot.positions.reduce((sum, p) => sum + (p.unrealized_pnl || 0), 0)
  const status = botStatus(bot)

  return (
    <button
      type="button"
      onClick={onSelect}
      className={`w-full text-left rounded-xl border p-4 transition-colors ${
        selected
          ? 'border-nofx-gold bg-nofx-bg-lighter/80'
          : 'border-nofx-gold/20 bg-nofx-bg-lighter/40 hover:border-nofx-gold/40'
      }`}
    >
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2 flex-wrap">
            <span className="font-semibold text-nofx-text">{bot.trader_name}</span>
            <span className="text-xs px-2 py-0.5 rounded-full bg-nofx-gold/15 text-nofx-gold">
              {layerLabel(cfg?.copy_layer)}
            </span>
            <span
              className={`text-xs px-2 py-0.5 rounded-full ${
                status === 'RUNNING'
                  ? 'bg-emerald-500/15 text-emerald-300'
                  : status === 'PAUSED'
                    ? 'bg-amber-500/15 text-amber-300'
                    : 'bg-zinc-500/15 text-zinc-300'
              }`}
            >
              {status}
            </span>
          </div>
          <p className="text-xs text-nofx-text-muted mt-1 font-mono break-all">
            Leader {shortAddr(cfg?.leader_address)}
          </p>
        </div>
        <div className="text-right text-sm shrink-0">
          <div className={unrealized >= 0 ? 'text-emerald-300' : 'text-red-300'}>
            {fmtUsd(unrealized)} uPnL
          </div>
          <div className="text-nofx-text-muted text-xs mt-1">
            {bot.positions.length} open · ${cfg?.notional_usd ?? '?'} × {cfg?.max_leverage ?? '?'}x
          </div>
        </div>
      </div>
      {bot.stats && (
        <div className="mt-3 grid grid-cols-3 gap-2 text-xs text-nofx-text-muted">
          <div>Trades {bot.stats.trade_count}</div>
          <div>Win {fmtPct(bot.stats.win_rate)}</div>
          <div>Realized {fmtUsd(bot.stats.total_pnl)}</div>
        </div>
      )}
      {bot.last_decision && (
        <p className="mt-2 text-xs text-nofx-text-muted line-clamp-2">{bot.last_decision}</p>
      )}
    </button>
  )
}

function BotDetailPanel({ bot }: { bot: CopyBotRow }) {
  const { data: history } = useSWR<PositionHistoryResponse>(
    bot ? `copy-history-${bot.trader_id}` : null,
    () => api.getPositionHistory(bot.trader_id, 30),
    { refreshInterval: 30000 }
  )
  const { data: decisions } = useSWR<DecisionRecord[]>(
    bot ? `copy-decisions-${bot.trader_id}` : null,
    () => api.getLatestDecisions(bot.trader_id, 8),
    { refreshInterval: 30000 }
  )
  const slug = `${bot.trader_name}-${bot.trader_id.slice(0, 4)}`
  const cfg = bot.copy_config

  return (
    <div className="rounded-xl border border-nofx-gold/20 bg-nofx-bg-lighter/50 p-5 space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-xl font-semibold text-nofx-text">{bot.trader_name}</h2>
          <p className="text-sm text-nofx-text-muted mt-1">{bot.strategy_name}</p>
          <p className="text-xs font-mono text-nofx-gold mt-2 break-all">{cfg?.leader_address}</p>
        </div>
        <Link
          to={buildDashboardPath(slug)}
          className="inline-flex items-center gap-1 text-sm text-nofx-gold hover:underline"
        >
          Open terminal <ExternalLink className="w-4 h-4" />
        </Link>
      </div>

      <div className="grid grid-cols-2 md:grid-cols-4 gap-3 text-sm">
        <Metric label="Mode" value={cfg?.copy_mode ?? 'fills'} />
        <Metric label="Notional" value={`$${cfg?.notional_usd ?? '—'}`} />
        <Metric label="Max legs" value={String(cfg?.max_positions ?? '—')} />
        <Metric label="Slots" value={String(cfg?.wallet_copy_slots ?? '—')} />
        <Metric label="Layer" value={layerLabel(cfg?.copy_layer)} />
        <Metric label="Overflow" value={cfg?.overflow_enabled ? 'On' : 'Off'} />
        <Metric label="Dry run" value={cfg?.dry_run ? 'Yes' : 'No'} />
        <Metric label="Status" value={botStatus(bot)} />
      </div>

      <section>
        <h3 className="text-sm font-semibold text-nofx-text mb-2">Open positions</h3>
        {bot.positions.length === 0 ? (
          <p className="text-sm text-nofx-text-muted">No open legs on shared wallet for this bot view.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-nofx-text-muted border-b border-nofx-gold/10">
                  <th className="py-2 pr-3">Symbol</th>
                  <th className="py-2 pr-3">Side</th>
                  <th className="py-2 pr-3">Size</th>
                  <th className="py-2 pr-3">Entry</th>
                  <th className="py-2 pr-3">Mark</th>
                  <th className="py-2">uPnL</th>
                </tr>
              </thead>
              <tbody>
                {bot.positions.map((p) => (
                  <tr key={`${p.symbol}-${p.side}`} className="border-b border-nofx-gold/5">
                    <td className="py-2 pr-3">{p.symbol}</td>
                    <td className="py-2 pr-3 capitalize">{p.side}</td>
                    <td className="py-2 pr-3">{p.quantity?.toFixed?.(4) ?? p.quantity}</td>
                    <td className="py-2 pr-3">{p.entry_price?.toFixed?.(2)}</td>
                    <td className="py-2 pr-3">{p.mark_price?.toFixed?.(2)}</td>
                    <td className={`py-2 ${p.unrealized_pnl >= 0 ? 'text-emerald-300' : 'text-red-300'}`}>
                      {fmtUsd(p.unrealized_pnl)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section>
        <h3 className="text-sm font-semibold text-nofx-text mb-2">Closed trades</h3>
        {!history?.positions?.length ? (
          <p className="text-sm text-nofx-text-muted">No closed trades yet.</p>
        ) : (
          <div className="overflow-x-auto max-h-64">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-nofx-text-muted border-b border-nofx-gold/10">
                  <th className="py-2 pr-3">Symbol</th>
                  <th className="py-2 pr-3">Side</th>
                  <th className="py-2 pr-3">PnL</th>
                  <th className="py-2">Closed</th>
                </tr>
              </thead>
              <tbody>
                {history.positions.slice(0, 20).map((p) => (
                  <tr key={p.id} className="border-b border-nofx-gold/5">
                    <td className="py-2 pr-3">{p.symbol}</td>
                    <td className="py-2 pr-3 capitalize">{p.side}</td>
                    <td className={`py-2 pr-3 ${p.realized_pnl >= 0 ? 'text-emerald-300' : 'text-red-300'}`}>
                      {fmtUsd(p.realized_pnl)}
                    </td>
                    <td className="py-2 text-nofx-text-muted">
                      {p.exit_time ? new Date(p.exit_time).toLocaleString() : '—'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section>
        <h3 className="text-sm font-semibold text-nofx-text mb-2">Recent copy activity</h3>
        {!decisions?.length ? (
          <p className="text-sm text-nofx-text-muted">No recent decisions logged.</p>
        ) : (
          <ul className="space-y-2 max-h-48 overflow-y-auto">
            {decisions.map((d, idx) => {
              const action = d.decisions?.[0]?.reasoning || d.execution_log?.[0] || d.error_message
              return (
                <li key={`${d.timestamp}-${idx}`} className="text-xs border border-nofx-gold/10 rounded-lg p-2">
                  <div className="text-nofx-text-muted">{new Date(d.timestamp).toLocaleString()}</div>
                  <div className="text-nofx-text mt-1 line-clamp-3">{action || 'Copy cycle'}</div>
                </li>
              )
            })}
          </ul>
        )}
      </section>
    </div>
  )
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-nofx-gold/10 bg-nofx-bg-deeper/40 p-3">
      <div className="text-xs text-nofx-text-muted">{label}</div>
      <div className="text-sm font-medium text-nofx-text mt-1">{value}</div>
    </div>
  )
}

export function CopyBotsPage() {
  const { language } = useLanguage()
  const [searchParams, setSearchParams] = useSearchParams()
  const selectedBotId = searchParams.get('bot') ?? ''

  const { data, error, isLoading } = useSWR('copy-bots', api.getCopyBots, {
    refreshInterval: 15000,
    revalidateOnFocus: false,
  })

  const grouped = useMemo(() => {
    const bots = data?.bots ?? []
    const layers: Record<number, CopyBotRow[]> = { 1: [], 2: [], 3: [] }
    for (const bot of bots) {
      const layer = bot.copy_config?.copy_layer ?? 2
      const key = layer >= 3 ? 3 : layer <= 1 ? 1 : 2
      layers[key].push(bot)
    }
    return layers
  }, [data?.bots])

  const selectedBot =
    data?.bots.find((b) => b.trader_id === selectedBotId) ?? data?.bots[0] ?? null

  const title = language === 'zh' ? 'Copy 机器人' : 'Copy Bots'

  if (error) {
    return (
      <DeepVoidBackground className="py-8" disableAnimation>
        <div className="container mx-auto max-w-7xl px-4 md:px-8">
          <div className="rounded-xl border border-red-300/40 bg-red-50 p-8 text-center text-red-700">
            {error instanceof Error ? error.message : 'Failed to load copy bots'}
          </div>
        </div>
      </DeepVoidBackground>
    )
  }

  return (
    <DeepVoidBackground className="py-8" disableAnimation>
      <div className="container mx-auto max-w-7xl px-4 md:px-8 space-y-6">
        <div className="flex items-center gap-3">
          <Copy className="w-7 h-7 text-nofx-gold" />
          <div>
            <h1 className="text-2xl font-bold text-nofx-text">{title}</h1>
            <p className="text-sm text-nofx-text-muted">
              Leader wallets, layers, open legs, and PnL for every copy bot.
            </p>
          </div>
        </div>

        {isLoading || !data ? (
          <div className="animate-pulse h-32 rounded-xl bg-nofx-bg-lighter border border-nofx-gold/20" />
        ) : (
          <>
            <div className="grid grid-cols-2 md:grid-cols-5 gap-3">
              <Metric label="Profile" value={data.profile} />
              <Metric
                label="Wallet equity"
                value={fmtUsd(data.wallet.equity).replace('+', '')}
              />
              <Metric label="Available" value={fmtUsd(data.wallet.available).replace('+', '')} />
              <Metric
                label="Slots used"
                value={`${data.wallet.open_legs} / ${data.wallet.wallet_slots}`}
              />
              <Metric
                label="Bots live / paused"
                value={`${data.summary.live_count} / ${data.summary.paused_count}`}
              />
            </div>

            {data.bots.length === 0 ? (
              <div className="rounded-xl border border-nofx-gold/20 bg-nofx-bg-lighter/40 p-8 text-center">
                <p className="text-nofx-text-muted">No copy bots found.</p>
                <Link to={ROUTES.strategy} className="text-nofx-gold text-sm mt-2 inline-block hover:underline">
                  Configure copy trading in Strategy Studio
                </Link>
              </div>
            ) : (
              <div className="grid lg:grid-cols-5 gap-6">
                <div className="lg:col-span-2 space-y-5">
                  {[1, 2, 3].map((layer) => {
                    const list = grouped[layer]
                    if (!list.length) return null
                    return (
                      <section key={layer}>
                        <div className="flex items-center gap-2 mb-3">
                          <Layers className="w-4 h-4 text-nofx-gold" />
                          <h2 className="text-sm font-semibold text-nofx-text">
                            Layer {layer}
                            {layer === 3 && (
                              <PauseCircle className="inline w-4 h-4 ml-2 text-amber-300" />
                            )}
                            {layer === 1 && (
                              <PlayCircle className="inline w-4 h-4 ml-2 text-emerald-300" />
                            )}
                          </h2>
                        </div>
                        <div className="space-y-3">
                          {list.map((bot) => (
                            <CopyBotCard
                              key={bot.trader_id}
                              bot={bot}
                              selected={selectedBot?.trader_id === bot.trader_id}
                              onSelect={() =>
                                setSearchParams({ bot: bot.trader_id })
                              }
                            />
                          ))}
                        </div>
                      </section>
                    )
                  })}
                </div>
                <div className="lg:col-span-3">
                  {selectedBot ? (
                    <BotDetailPanel bot={selectedBot} />
                  ) : (
                    <div className="rounded-xl border border-nofx-gold/20 p-8 text-center text-nofx-text-muted">
                      Select a copy bot to see details.
                    </div>
                  )}
                </div>
              </div>
            )}
          </>
        )}
      </div>
    </DeepVoidBackground>
  )
}
