import { MessageCircle } from 'lucide-react'
import { useAuth } from '../../contexts/AuthContext'
import { useSystemConfig } from '../../hooks/useSystemConfig'

export function OwnerAccountBanner() {
  const { user, logout } = useAuth()
  const { config } = useSystemConfig()
  const ownerId = config?.owner_user_id

  if (!user || !ownerId || user.id === ownerId) {
    return null
  }

  return (
    <div className="mb-4 rounded-lg border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-sm text-amber-900">
      <div className="flex items-start gap-2">
        <MessageCircle className="mt-0.5 h-4 w-4 shrink-0" />
        <div className="space-y-2">
          <p className="font-semibold">Secondary account detected</p>
          <p className="text-amber-900/90">
            You are signed in as <span className="font-mono">{user.email}</span>,
            not your main NOFX account. Equity, positions, and AI spend will stay
            empty until you switch accounts.
          </p>
          <p className="text-amber-900/90">
            Send <span className="font-mono font-semibold">/weblogin</span> to{' '}
            <span className="font-semibold">@duacrypto_bot</span> and tap the
            link, or log out and sign in again with the same email after the
            latest deploy.
          </p>
          <button
            type="button"
            onClick={() => logout()}
            className="rounded px-3 py-1.5 text-xs font-semibold"
            style={{
              background: 'rgba(214, 67, 58, 0.12)',
              color: '#D6433A',
            }}
          >
            Log out
          </button>
        </div>
      </div>
    </div>
  )
}
