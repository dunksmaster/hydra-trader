import { MessageCircle } from 'lucide-react'
import { useAuth } from '../../contexts/AuthContext'
import { useSystemConfig } from '../../hooks/useSystemConfig'

export function OwnerAccountBanner() {
  const { user } = useAuth()
  const { config } = useSystemConfig()
  const ownerId = config?.owner_user_id

  if (!user || !ownerId || user.id === ownerId) {
    return null
  }

  // #region agent log
  fetch('http://127.0.0.1:7776/ingest/c66f92c8-2d4b-4a06-b990-d87fc4f644cc',{method:'POST',headers:{'Content-Type':'application/json','X-Debug-Session-Id':'e70047'},body:JSON.stringify({sessionId:'e70047',hypothesisId:'H2',location:'OwnerAccountBanner.tsx',message:'secondary_account_banner',data:{userId:user.id,ownerId},timestamp:Date.now()})}).catch(()=>{});
  // #endregion

  return (
    <div className="mb-4 rounded-lg border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-sm text-amber-900">
      <div className="flex items-start gap-2">
        <MessageCircle className="mt-0.5 h-4 w-4 shrink-0" />
        <div>
          <p className="font-semibold">Secondary account detected</p>
          <p className="mt-1 text-amber-900/90">
            You are signed in as <span className="font-mono">{user.email}</span>,
            not your main NOFX account. Send{' '}
            <span className="font-mono font-semibold">/weblogin</span> to{' '}
            <span className="font-semibold">@duacrypto_bot</span> and tap the
            link to open your full dashboard (both traders + wallet config).
          </p>
        </div>
      </div>
    </div>
  )
}
