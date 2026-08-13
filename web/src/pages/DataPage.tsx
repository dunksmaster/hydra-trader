import useSWR from 'swr'
import { Database, ExternalLink } from 'lucide-react'
import { Link } from 'react-router-dom'
import { useLanguage } from '../contexts/LanguageContext'
import { useAuth } from '../contexts/AuthContext'
import { t } from '../i18n/translations'
import { api } from '../lib/api'
import type { SignalRankItem } from '../lib/api/data'
import { SignalMatrix } from '../components/terminal/SignalMatrix'
import { ROUTES } from '../router/paths'

const VERGEX_TRENDING_URL = 'https://vergex.trade/trending'

function toSignalRankItems(
  items: NonNullable<
    Awaited<ReturnType<typeof api.getVergexSignalRanking>>['items']
  >
): SignalRankItem[] {
  return items.map((item, index) => ({
    rank: item.rank ?? index + 1,
    symbol: item.symbol,
    market_type: item.market_type ?? '',
    bias: item.bias ?? 'neutral',
    score: item.score ?? 0,
    category: item.category,
  }))
}

export function DataPage() {
  const { language } = useLanguage()
  const { user, token } = useAuth()
  const isAuthenticated = !!user && !!token

  const {
    data: ranking,
    error,
    isLoading,
  } = useSWR(
    isAuthenticated ? 'data-center-signals' : null,
    () => api.getVergexSignalRanking(30),
    {
      refreshInterval: 60000,
      revalidateOnFocus: false,
    }
  )

  if (!isAuthenticated) {
    return (
      <div className="container mx-auto max-w-2xl px-4 py-16 text-center">
        <div className="mx-auto mb-6 flex h-16 w-16 items-center justify-center rounded-2xl border border-nofx-gold/30 bg-nofx-bg-lighter">
          <Database className="h-8 w-8 text-nofx-gold" />
        </div>
        <h1 className="mb-3 text-2xl font-bold text-nofx-text">
          {t('dataCenter', language)}
        </h1>
        <p className="mb-8 text-sm text-nofx-text-muted">
          Sign in to view live Claw402 signal rankings inside NOFX. Vergex
          blocks embedding on this domain, so the data board loads from your
          account instead of an iframe.
        </p>
        <div className="flex flex-col items-center justify-center gap-3 sm:flex-row">
          <Link
            to={ROUTES.login}
            className="rounded-lg bg-nofx-gold px-5 py-2.5 text-sm font-semibold text-white"
          >
            Sign in
          </Link>
          <a
            href={VERGEX_TRENDING_URL}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-2 rounded-lg border border-nofx-gold/30 px-5 py-2.5 text-sm font-semibold text-nofx-text"
          >
            Open on Vergex
            <ExternalLink className="h-4 w-4" />
          </a>
        </div>
      </div>
    )
  }

  return (
    <div className="container mx-auto max-w-7xl px-4 py-6 md:px-8">
      <div className="mb-6 flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <div>
          <h1 className="text-2xl font-bold text-nofx-text">
            {t('dataCenter', language)}
          </h1>
          <p className="text-sm text-nofx-text-muted">
            Live Claw402 / Vergex signal ranking via your NOFX wallet.
          </p>
        </div>
        <a
          href={VERGEX_TRENDING_URL}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-2 self-start rounded-lg border border-nofx-gold/30 px-4 py-2 text-sm font-semibold text-nofx-text"
        >
          Full board on Vergex
          <ExternalLink className="h-4 w-4" />
        </a>
      </div>

      {isLoading ? (
        <div className="rounded-xl border border-nofx-gold/20 bg-nofx-bg-lighter p-8 text-center text-sm text-nofx-text-muted">
          Loading signal board...
        </div>
      ) : error ? (
        <div className="rounded-xl border border-red-300/40 bg-red-50 p-8 text-center">
          <p className="mb-4 text-sm text-red-700">
            {error instanceof Error
              ? error.message
              : 'Failed to load data board'}
          </p>
          <a
            href={VERGEX_TRENDING_URL}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-2 text-sm font-semibold text-nofx-gold"
          >
            Open Vergex trending instead
            <ExternalLink className="h-4 w-4" />
          </a>
        </div>
      ) : (
        <div className="rounded-xl border border-nofx-gold/20 bg-nofx-bg-lighter p-4">
          <SignalMatrix items={toSignalRankItems(ranking?.items ?? [])} max={30} />
        </div>
      )}
    </div>
  )
}
