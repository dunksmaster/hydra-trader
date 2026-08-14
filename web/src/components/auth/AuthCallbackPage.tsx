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
    if (!ok) {
      toast.error(t('loginFailed', language))
      navigate(ROUTES.login, { replace: true })
      setDone(true)
      return
    }

    const target = redirect.startsWith('/') ? redirect : ROUTES.dashboard
    try {
      const url = new URL(target, window.location.origin)
      url.searchParams.delete('trader')
      navigate(`${url.pathname}${url.search}`, { replace: true })
    } catch {
      navigate(ROUTES.dashboard, { replace: true })
    }
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
