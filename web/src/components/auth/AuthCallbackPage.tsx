import { useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { toast } from 'sonner'
import { useAuth } from '../../contexts/AuthContext'
import { useLanguage } from '../../contexts/LanguageContext'
import { t } from '../../i18n/translations'
import { ROUTES } from '../../router/paths'

export function AuthCallbackPage() {
  const { loginWithToken } = useAuth()
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const { language } = useLanguage()
  const [done, setDone] = useState(false)

  useEffect(() => {
    if (done) return

    const token = searchParams.get('token')
    const redirect = searchParams.get('redirect') || ROUTES.dashboard

    if (!token) {
      navigate(ROUTES.login, { replace: true })
      setDone(true)
      return
    }

    const ok = loginWithToken(token)
    // #region agent log
    fetch('http://127.0.0.1:7776/ingest/c66f92c8-2d4b-4a06-b990-d87fc4f644cc',{method:'POST',headers:{'Content-Type':'application/json','X-Debug-Session-Id':'e70047'},body:JSON.stringify({sessionId:'e70047',hypothesisId:'H3',location:'AuthCallbackPage.tsx',message:'auth_callback',data:{ok,redirect},timestamp:Date.now()})}).catch(()=>{});
    // #endregion
    if (!ok) {
      toast.error(t('loginFailed', language))
      navigate(ROUTES.login, { replace: true })
      setDone(true)
      return
    }

    navigate(redirect.startsWith('/') ? redirect : ROUTES.dashboard, {
      replace: true,
    })
    setDone(true)
  }, [done, language, loginWithToken, navigate, searchParams])

  return (
    <div
      className="min-h-screen flex items-center justify-center"
      style={{ background: '#F1ECE2' }}
    >
      <p style={{ color: '#1A1813' }}>{t('loading', language)}</p>
    </div>
  )
}
