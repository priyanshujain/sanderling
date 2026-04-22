import { useState, type FormEvent } from 'react'
import { createAccount } from '../db'
import { back, navigate } from '../router'
import { BackButton, Header, Screen } from '../components/Screen'

export function AddAccountPage(props: { onCreated: () => void }) {
  const [name, setName] = useState('')
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (busy) return
    const trimmed = name.trim()
    if (!trimmed) {
      setErr('Account name is required')
      return
    }
    if (trimmed.length > 40) {
      setErr('Name is too long (max 40 characters)')
      return
    }
    setBusy(true)
    try {
      await createAccount(trimmed)
      props.onCreated()
      navigate('/', { replace: true })
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Could not create account')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Screen
      header={
        <Header
          title="New account"
          left={<BackButton onClick={() => back('/')} />}
        />
      }
      footer={
        <button
          id="add-account-submit"
          className="btn primary"
          type="submit"
          form="add-account-form"
          disabled={busy || name.trim().length === 0}
        >
          {busy ? 'Creating…' : 'Create account'}
        </button>
      }
    >
      <form
        id="add-account-form"
        onSubmit={onSubmit}
        className="list"
        style={{ gap: 14 }}
        noValidate
      >
        <div className="field">
          <label className="field-label" htmlFor="account-name">
            Account name
          </label>
          <input
            id="account-name"
            className={'input' + (err ? ' invalid' : '')}
            type="text"
            autoFocus
            autoComplete="off"
            placeholder="e.g. Checking"
            value={name}
            maxLength={40}
            onChange={(e) => {
              setName(e.target.value)
              if (err) setErr(null)
            }}
          />
        </div>
        <div id="add-account-error" className="error" role="alert">
          {err ?? ''}
        </div>
        <div className="muted" style={{ fontSize: 12 }}>
          Use a short, recognizable name. You can create as many accounts as
          you need.
        </div>
      </form>
    </Screen>
  )
}
