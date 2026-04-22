import { useState, type FormEvent } from 'react'
import { checkCredentials, DEMO_EMAIL, DEMO_PASSWORD } from '../auth'
import { setSession } from '../db'
import { navigate } from '../router'
import { Screen } from '../components/Screen'

export function LoginPage(props: { onLoggedIn: (user: string) => void }) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (busy) return
    if (!email.trim() || !password) {
      setErr('Enter email and password')
      return
    }
    if (!checkCredentials(email, password)) {
      setErr('Invalid email or password')
      return
    }
    setBusy(true)
    try {
      await setSession(email.trim().toLowerCase())
      props.onLoggedIn(email.trim().toLowerCase())
      navigate('/', { replace: true })
    } catch {
      setErr('Could not sign in. Try again.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Screen>
      <form
        onSubmit={onSubmit}
        className="list"
        style={{ gap: 14, marginTop: 24 }}
        noValidate
      >
        <div className="field">
          <label className="field-label" htmlFor="email">
            Email
          </label>
          <input
            id="email"
            className={'input' + (err ? ' invalid' : '')}
            type="email"
            autoComplete="email"
            inputMode="email"
            autoCapitalize="none"
            placeholder={DEMO_EMAIL}
            value={email}
            onChange={(e) => {
              setEmail(e.target.value)
              if (err) setErr(null)
            }}
          />
        </div>

        <div className="field">
          <label className="field-label" htmlFor="password">
            Password
          </label>
          <input
            id="password"
            className={'input' + (err ? ' invalid' : '')}
            type="password"
            autoComplete="current-password"
            placeholder="••••••••"
            value={password}
            onChange={(e) => {
              setPassword(e.target.value)
              if (err) setErr(null)
            }}
          />
        </div>

        <div id="login-error" className="error" role="alert">
          {err ?? ''}
        </div>

        <button
          id="login-submit"
          className="btn primary"
          type="submit"
          disabled={busy}
        >
          {busy ? 'Signing in…' : 'Sign in'}
        </button>

        <div
          className="card"
          style={{
            marginTop: 8,
            background: 'var(--surface-2)',
            borderStyle: 'dashed',
          }}
        >
          <div className="muted" style={{ fontSize: 12 }}>
            Demo credentials
          </div>
          <div style={{ fontSize: 13 }}>
            <div>
              email: <span style={{ fontWeight: 600 }}>{DEMO_EMAIL}</span>
            </div>
            <div>
              password:{' '}
              <span style={{ fontWeight: 600 }}>{DEMO_PASSWORD}</span>
            </div>
          </div>
        </div>
      </form>
    </Screen>
  )
}
