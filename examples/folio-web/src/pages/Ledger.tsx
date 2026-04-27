import { useEffect, useMemo, useState } from 'react'
import {
  getAccount,
  listTransactions,
  type Account,
  type Transaction,
} from '../db'
import {
  balanceOf,
  formatCents,
  formatDate,
  signedAmount,
} from '../format'
import { back, navigate } from '../router'
import { BackButton, Header, Screen } from '../components/Screen'

export function LedgerPage(props: {
  accountId: string
  reloadKey: number
}) {
  const [account, setAccount] = useState<Account | null | undefined>(undefined)
  const [txns, setTxns] = useState<Transaction[] | null>(null)

  useEffect(() => {
    let alive = true
    Promise.all([
      getAccount(props.accountId),
      listTransactions(props.accountId),
    ]).then(([a, t]) => {
      if (!alive) return
      setAccount(a ?? null)
      setTxns(t)
    })
    return () => {
      alive = false
    }
  }, [props.accountId, props.reloadKey])

  const balance = useMemo(() => (txns ? balanceOf(txns) : 0), [txns])

  if (account === null) {
    return (
      <Screen
        header={
          <Header
            title="Account"
            left={<BackButton onClick={() => back('/')} />}
          />
        }
      >
        <div className="empty">
          <div className="empty-title">Account not found</div>
          <div className="empty-sub">It may have been deleted.</div>
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

  return (
    <Screen
      header={
        <Header
          title={account?.name ?? 'Loading…'}
          subtitle="Ledger"
          left={<BackButton onClick={() => back('/')} />}
        />
      }
      footer={
        <button
          id="add-txn"
          className="btn primary"
          type="button"
          disabled={!account}
          onClick={() =>
            navigate(`/accounts/${props.accountId}/transactions/new`)
          }
        >
          + Add transaction
        </button>
      }
    >
      <div
        id="ledger"
        className="card"
        data-account-id={props.accountId}
        data-account-name={account?.name ?? ''}
        data-txn-count={txns?.length ?? 0}
      >
        <div
          id="ledger-balance"
          className="balance-display"
          data-cents={balance}
        >
          <span className="balance-label">Balance</span>
          <span
            className={
              'balance-value' +
              (balance > 0 ? ' credit' : balance < 0 ? ' debit' : '')
            }
          >
            {formatCents(balance)}
          </span>
        </div>
      </div>

      <div className="section-label">Activity</div>

      {txns === null ? (
        <div className="empty">
          <div className="empty-sub">Loading…</div>
        </div>
      ) : txns.length === 0 ? (
        <div className="empty">
          <div className="empty-icon" aria-hidden="true">
            <svg
              width="22"
              height="22"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M3 6h18M3 12h18M3 18h12" />
            </svg>
          </div>
          <div className="empty-title">No transactions yet</div>
          <div className="empty-sub">
            Add your first credit or debit to see it here.
          </div>
        </div>
      ) : (
        <div className="card" style={{ padding: '0 16px' }}>
          {txns.map((t) => {
            const signed = signedAmount(t)
            return (
              <div
                key={t.id}
                className="txn-row"
                data-testid="txn-row"
                data-txn-id={t.id}
                data-account-id={t.accountId}
                data-type={t.type}
                data-amount={t.amount}
                data-signed={signed}
              >
                <div className={'txn-icon ' + t.type} aria-hidden="true">
                  {t.type === 'credit' ? '+' : '−'}
                </div>
                <div className="txn-meta">
                  <div className="txn-note">
                    {t.note || (t.type === 'credit' ? 'Credit' : 'Debit')}
                  </div>
                  <div className="txn-date">{formatDate(t.createdAt)}</div>
                </div>
                <div className={'txn-amount ' + t.type}>
                  {formatCents(signed, { signed: true })}
                </div>
              </div>
            )
          })}
        </div>
      )}
    </Screen>
  )
}
