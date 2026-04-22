import { useEffect, useState, type FormEvent } from 'react'
import {
  createTransaction,
  getAccount,
  type Account,
  type TxnType,
} from '../db'
import { parseCents } from '../format'
import { back, navigate } from '../router'
import { BackButton, Header, Screen } from '../components/Screen'

export function AddTransactionPage(props: {
  accountId: string
  onCreated: () => void
}) {
  const [account, setAccount] = useState<Account | null | undefined>(undefined)
  const [type, setType] = useState<TxnType>('credit')
  const [amount, setAmount] = useState('')
  const [note, setNote] = useState('')
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let alive = true
    getAccount(props.accountId).then((a) => {
      if (alive) setAccount(a ?? null)
    })
    return () => {
      alive = false
    }
  }, [props.accountId])

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (busy) return
    const cents = parseCents(amount)
    if (cents === null) {
      setErr('Enter a valid amount (e.g. 12.34)')
      return
    }
    if (cents <= 0) {
      setErr('Amount must be greater than zero')
      return
    }
    setBusy(true)
    try {
      await createTransaction({
        accountId: props.accountId,
        type,
        amount: cents,
        note,
      })
      props.onCreated()
      back(`/accounts/${props.accountId}`)
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Could not save transaction')
    } finally {
      setBusy(false)
    }
  }

  if (account === null) {
    return (
      <Screen
        header={
          <Header
            title="Add transaction"
            left={<BackButton onClick={() => back('/')} />}
          />
        }
      >
        <div className="empty">
          <div className="empty-title">Account not found</div>
          <button
            className="btn"
            type="button"
            style={{ marginTop: 12, width: 'auto', padding: '10px 16px' }}
            onClick={() => navigate('/', { replace: true })}
          >
            Back to accounts
          </button>
        </div>
      </Screen>
    )
  }

  const submitDisabled = busy || amount.trim() === ''

  return (
    <Screen
      header={
        <Header
          title="Add transaction"
          subtitle={account?.name}
          left={
            <BackButton
              onClick={() => back(`/accounts/${props.accountId}`)}
            />
          }
        />
      }
      footer={
        <button
          id="txn-submit"
          className="btn primary"
          type="submit"
          form="add-txn-form"
          disabled={submitDisabled}
        >
          {busy ? 'Saving…' : type === 'credit' ? 'Add credit' : 'Add debit'}
        </button>
      }
    >
      <form
        id="add-txn-form"
        onSubmit={onSubmit}
        className="list"
        style={{ gap: 16 }}
        data-account-id={props.accountId}
        data-type={type}
        noValidate
      >
        <div
          className="segmented"
          role="tablist"
          aria-label="Transaction type"
        >
          <button
            id="txn-credit"
            type="button"
            role="tab"
            className="credit"
            aria-pressed={type === 'credit'}
            onClick={() => setType('credit')}
          >
            Credit
          </button>
          <button
            id="txn-debit"
            type="button"
            role="tab"
            className="debit"
            aria-pressed={type === 'debit'}
            onClick={() => setType('debit')}
          >
            Debit
          </button>
        </div>

        <div className="field">
          <label className="field-label" htmlFor="txn-amount">
            Amount
          </label>
          <input
            id="txn-amount"
            className={'input amount-input' + (err ? ' invalid' : '')}
            type="text"
            inputMode="decimal"
            autoComplete="off"
            placeholder="0.00"
            value={amount}
            onChange={(e) => {
              const v = e.target.value
              if (v === '' || /^\d*(\.\d{0,2})?$/.test(v)) {
                setAmount(v)
                if (err) setErr(null)
              }
            }}
            autoFocus
          />
        </div>

        <div className="field">
          <label className="field-label" htmlFor="txn-note">
            Note (optional)
          </label>
          <input
            id="txn-note"
            className="input"
            type="text"
            placeholder="What's this for?"
            value={note}
            maxLength={80}
            onChange={(e) => setNote(e.target.value)}
          />
        </div>

        <div id="txn-error" className="error" role="alert">
          {err ?? ''}
        </div>
      </form>
    </Screen>
  )
}
