import useSWR from 'swr'
import { api } from '../../lib/api'

function fmtUsd(n: number | undefined): string {
  if (n == null || Number.isNaN(n)) return '—'
  if (Math.abs(n) < 0.01 && n !== 0) return `$${n.toFixed(4)}`
  return `$${n.toFixed(2)}`
}

interface AISpendStripProps {
  traderId?: string
}

export function AISpendStrip({ traderId }: AISpendStripProps) {
  const { data } = useSWR(
    traderId ? ['ai-spend-dashboard', traderId] : null,
    () => api.getDashboard(traderId!, true),
    { refreshInterval: 60000, shouldRetryOnError: false }
  )

  const cellBorder = '1px solid var(--tm-line)'

  const items = [
    { l: 'AI spent today', v: fmtUsd(data?.spent_today), tip: undefined },
    { l: 'AI spent (7d)', v: fmtUsd(data?.spent_week), tip: undefined },
    { l: 'Est. daily burn', v: fmtUsd(data?.estimated_daily), tip: undefined },
    {
      l: 'Est. 7-day spend',
      v: fmtUsd(data?.projected_7d),
      tip: 'Based on your average daily Claw402 spend over the last 7 days.',
    },
    { l: 'Fee wallet', v: data != null ? `${fmtUsd(data.wallet_balance_usdc)} USDC` : '—', tip: undefined },
  ]

  return (
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(5, 1fr)' }}>
      {items.map((m, i) => (
        <div
          key={m.l}
          title={m.tip}
          style={{ padding: '10px 14px', borderRight: i < 4 ? cellBorder : 'none' }}
        >
          <div className="tm-sc">{m.l}</div>
          <div
            className="tm-mono"
            style={{ fontSize: 15, fontWeight: 500, color: 'var(--tm-ink)', marginTop: 3 }}
          >
            {m.v}
          </div>
        </div>
      ))}
    </div>
  )
}

export default AISpendStrip
