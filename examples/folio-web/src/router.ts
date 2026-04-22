import { useEffect, useState } from 'react'

export type Route =
  | { name: 'login' }
  | { name: 'home' }
  | { name: 'add-account' }
  | { name: 'ledger'; accountId: string }
  | { name: 'add-transaction'; accountId: string }
  | { name: 'not-found' }

function readHash(): string {
  const raw = window.location.hash.replace(/^#/, '')
  return raw === '' ? '/' : raw
}

export function parseHash(hash: string): Route {
  const path = hash.split('?')[0]
  if (path === '/' || path === '') return { name: 'home' }
  if (path === '/login') return { name: 'login' }
  if (path === '/accounts/new') return { name: 'add-account' }
  const ledgerMatch = /^\/accounts\/([^/]+)$/.exec(path)
  if (ledgerMatch) return { name: 'ledger', accountId: ledgerMatch[1] }
  const txnMatch = /^\/accounts\/([^/]+)\/transactions\/new$/.exec(path)
  if (txnMatch)
    return { name: 'add-transaction', accountId: txnMatch[1] }
  return { name: 'not-found' }
}

export function useRoute(): Route {
  const [hash, setHash] = useState<string>(() => readHash())
  useEffect(() => {
    function onChange() {
      setHash(readHash())
    }
    window.addEventListener('hashchange', onChange)
    return () => window.removeEventListener('hashchange', onChange)
  }, [])
  return parseHash(hash)
}

export function navigate(path: string, opts: { replace?: boolean } = {}) {
  const target = '#' + (path.startsWith('/') ? path : '/' + path)
  if (opts.replace) {
    window.location.replace(target)
  } else {
    window.location.hash = target.slice(1)
  }
}

export function back(fallback: string = '/') {
  if (window.history.length > 1) {
    window.history.back()
  } else {
    navigate(fallback, { replace: true })
  }
}
