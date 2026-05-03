import { useEffect, useMemo, useState } from 'react'
import {
  listAccounts,
  listAllTransactions,
  type Account,
  type Transaction,
} from '../db'
import { formatCents, initialsOf, signedAmount } from '../format'
import { navigate } from '../router'
import { Header, Screen } from '../components/Screen'

export function HomePage(props: {
  user: string
  onLogout: () => void
  reloadKey: number
}) {
  const [accounts, setAccounts] = useState<Account[] | null>(null)
  const [txns, setTxns] = useState<Transaction[]>([])

  useEffect(() => {
    let alive = true
    Promise.all([listAccounts(), listAllTransactions()]).then(([a, t]) => {
      if (!alive) return
      setAccounts(a)
      setTxns(t)
    })
    return () => {
      alive = false
    }
  }, [props.reloadKey])

  const balanceByAccount = useMemo(() => {
    const m = new Map<string, number>()
    for (const t of txns) {
      m.set(t.accountId, (m.get(t.accountId) ?? 0) + signedAmount(t))
    }
    return m
  }, [txns])

  const totalBalance = useMemo(() => {
    let sum = 0
    for (const v of balanceByAccount.values()) sum += v
    return sum
  }, [balanceByAccount])

  return (
    <Screen
      header={
        <Header
          title="Accounts"
          subtitle={props.user}
          right={
            <button
              id="logout"
              type="button"
              className="icon-btn"
              onClick={props.onLogout}
              aria-label="Sign out"
              title="Sign out"
            >
              <svg
                width="16"
                height="16"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2.2"
                strokeLinecap="round"
                strokeLinejoin="round"
                aria-hidden="true"
              >
                <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
                <polyline points="16 17 21 12 16 7" />
                <line x1="21" y1="12" x2="9" y2="12" />
              </svg>
            </button>
          }
        />
      }
      footer={
        <>
          <div
            className="row"
            style={{ justifyContent: 'space-between', alignItems: 'flex-end' }}
          >
            <div
              id="total-balance"
              className="balance-display"
              data-cents={totalBalance}
            >
              <span className="balance-label">Total balance</span>
              <span
                className={
                  'balance-value' +
                  (totalBalance > 0
                    ? ' credit'
                    : totalBalance < 0
                      ? ' debit'
                      : '')
                }
              >
                {formatCents(totalBalance)}
              </span>
            </div>
            <span
              id="account-count"
              className="faint"
              style={{ fontSize: 12 }}
              data-value={accounts?.length ?? 0}
            >
              {accounts?.length ?? 0} account
              {(accounts?.length ?? 0) === 1 ? '' : 's'}
            </span>
          </div>
          <button
            id="add-account"
            className="btn primary"
            type="button"
            onClick={() => navigate('/accounts/new')}
          >
            + Add account
          </button>
        </>
      }
    >
      {accounts === null ? (
        <div className="empty">
          <div className="empty-sub">Loading…</div>
        </div>
      ) : accounts.length === 0 ? (
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
              <rect x="3" y="6" width="18" height="13" rx="2" />
              <path d="M3 10h18" />
            </svg>
          </div>
          <div className="empty-title">No accounts yet</div>
          <div className="empty-sub">
            Create your first account to start tracking transactions.
          </div>
        </div>
      ) : (
        <div className="list" data-testid="account-list">
          {accounts.map((a) => {
            const bal = balanceByAccount.get(a.id) ?? 0
            return (
              <button
                key={a.id}
                type="button"
                className="account-card"
                data-testid="account-card"
                data-account-id={a.id}
                data-name={a.name}
                data-balance={bal}
                data-txn-count={countTxns(txns, a.id)}
                aria-label={`${a.name}, ${formatCents(bal)}`}
                onClick={() => navigate(`/accounts/${a.id}`)}
              >
                <span className="account-avatar" aria-hidden="true">
                  {initialsOf(a.name)}
                </span>
                <span className="account-meta">
                  <span className="account-name">{a.name}</span>
                  <span className="account-sub">
                    {countTxns(txns, a.id)} transactions
                  </span>
                </span>
                <span className="account-amount">{formatCents(bal)}</span>
              </button>
            )
          })}
        </div>
      )}
    </Screen>
  )
}

function countTxns(all: Transaction[], accountId: string): number {
  let n = 0
  for (const t of all) if (t.accountId === accountId) n++
  return n
}
