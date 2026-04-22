import { useCallback, useEffect, useState } from 'react'
import { clearSession, getSession } from './db'
import { navigate, useRoute } from './router'
import { LoginPage } from './pages/Login'
import { HomePage } from './pages/Home'
import { AddAccountPage } from './pages/AddAccount'
import { LedgerPage } from './pages/Ledger'
import { AddTransactionPage } from './pages/AddTransaction'
import { StatusBar } from './components/Screen'

type AuthState =
  | { status: 'loading' }
  | { status: 'logged-out' }
  | { status: 'logged-in'; user: string }

export default function App() {
  const route = useRoute()
  const [auth, setAuth] = useState<AuthState>({ status: 'loading' })
  const [reloadKey, setReloadKey] = useState(0)

  useEffect(() => {
    let alive = true
    getSession().then((s) => {
      if (!alive) return
      setAuth(
        s ? { status: 'logged-in', user: s.user } : { status: 'logged-out' },
      )
    })
    return () => {
      alive = false
    }
  }, [])

  useEffect(() => {
    if (auth.status === 'loading') return
    if (auth.status === 'logged-out' && route.name !== 'login') {
      navigate('/login', { replace: true })
    } else if (auth.status === 'logged-in' && route.name === 'login') {
      navigate('/', { replace: true })
    }
  }, [auth, route.name])

  const bumpReload = () => setReloadKey((k) => k + 1)

  const onLogout = useCallback(async () => {
    await clearSession()
    setAuth({ status: 'logged-out' })
    navigate('/login', { replace: true })
  }, [])

  return (
    <div
      className="frame"
      data-route={route.name}
      data-logged-in={auth.status === 'logged-in'}
      data-auth-status={auth.status}
    >
      <StatusBar />
      {auth.status === 'loading' ? (
        <div className="screen">
          <div className="screen-body">
            <div className="empty">
              <div className="empty-sub">Loading…</div>
            </div>
          </div>
        </div>
      ) : auth.status === 'logged-out' ? (
        <LoginPage
          onLoggedIn={(user) => setAuth({ status: 'logged-in', user })}
        />
      ) : (
        renderRouteForLoggedIn(route, auth.user, bumpReload, onLogout, reloadKey)
      )}
    </div>
  )
}

function renderRouteForLoggedIn(
  route: ReturnType<typeof useRoute>,
  user: string,
  bumpReload: () => void,
  onLogout: () => void,
  reloadKey: number,
) {
  switch (route.name) {
    case 'home':
      return (
        <HomePage user={user} onLogout={onLogout} reloadKey={reloadKey} />
      )
    case 'add-account':
      return <AddAccountPage onCreated={bumpReload} />
    case 'ledger':
      return (
        <LedgerPage accountId={route.accountId} reloadKey={reloadKey} />
      )
    case 'add-transaction':
      return (
        <AddTransactionPage
          accountId={route.accountId}
          onCreated={bumpReload}
        />
      )
    case 'login':
      return null
    case 'not-found':
    default:
      return (
        <div className="screen">
          <div className="screen-body">
            <div className="empty">
              <div className="empty-title">Page not found</div>
              <button
                className="btn"
                type="button"
                style={{
                  marginTop: 12,
                  width: 'auto',
                  padding: '10px 16px',
                }}
                onClick={() => navigate('/', { replace: true })}
              >
                Go home
              </button>
            </div>
          </div>
        </div>
      )
  }
}
