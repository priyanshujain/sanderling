export type Account = {
  id: string
  name: string
  createdAt: number
}

export type TxnType = 'credit' | 'debit'

export type Transaction = {
  id: string
  accountId: string
  type: TxnType
  amount: number
  note: string
  createdAt: number
}

export type Session = {
  key: 'current'
  user: string
  loggedInAt: number
}

const DB_NAME = 'ledger'
const DB_VERSION = 1
const STORE_ACCOUNTS = 'accounts'
const STORE_TXNS = 'transactions'
const STORE_SESSION = 'session'

let dbPromise: Promise<IDBDatabase> | null = null

function openDb(): Promise<IDBDatabase> {
  if (dbPromise) return dbPromise
  dbPromise = new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION)
    req.onupgradeneeded = () => {
      const db = req.result
      if (!db.objectStoreNames.contains(STORE_ACCOUNTS)) {
        const s = db.createObjectStore(STORE_ACCOUNTS, { keyPath: 'id' })
        s.createIndex('createdAt', 'createdAt')
        s.createIndex('name', 'name', { unique: true })
      }
      if (!db.objectStoreNames.contains(STORE_TXNS)) {
        const s = db.createObjectStore(STORE_TXNS, { keyPath: 'id' })
        s.createIndex('accountId', 'accountId')
        s.createIndex('createdAt', 'createdAt')
      }
      if (!db.objectStoreNames.contains(STORE_SESSION)) {
        db.createObjectStore(STORE_SESSION, { keyPath: 'key' })
      }
    }
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
  return dbPromise
}

function tx(
  db: IDBDatabase,
  stores: string | string[],
  mode: IDBTransactionMode,
) {
  return db.transaction(stores, mode)
}

function reqAsPromise<T>(req: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
}

export function makeId(): string {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return crypto.randomUUID()
  }
  return (
    Date.now().toString(36) + Math.random().toString(36).slice(2, 10)
  )
}

export async function listAccounts(): Promise<Account[]> {
  const db = await openDb()
  const store = tx(db, STORE_ACCOUNTS, 'readonly').objectStore(STORE_ACCOUNTS)
  const all = await reqAsPromise(store.getAll() as IDBRequest<Account[]>)
  return all.sort((a, b) => a.createdAt - b.createdAt)
}

export async function getAccount(id: string): Promise<Account | undefined> {
  const db = await openDb()
  const store = tx(db, STORE_ACCOUNTS, 'readonly').objectStore(STORE_ACCOUNTS)
  return reqAsPromise(store.get(id) as IDBRequest<Account | undefined>)
}

export async function accountNameExists(name: string): Promise<boolean> {
  const db = await openDb()
  const idx = tx(db, STORE_ACCOUNTS, 'readonly')
    .objectStore(STORE_ACCOUNTS)
    .index('name')
  const key = await reqAsPromise(idx.getKey(name))
  return key !== undefined
}

export async function createAccount(name: string): Promise<Account> {
  const trimmed = name.trim()
  if (!trimmed) throw new Error('Name is required')
  if (trimmed.length > 40) throw new Error('Name is too long (max 40)')
  if (await accountNameExists(trimmed)) {
    throw new Error('An account with that name already exists')
  }
  const account: Account = {
    id: makeId(),
    name: trimmed,
    createdAt: Date.now(),
  }
  const db = await openDb()
  const t = tx(db, STORE_ACCOUNTS, 'readwrite')
  await reqAsPromise(t.objectStore(STORE_ACCOUNTS).add(account))
  return account
}

export async function listTransactions(
  accountId: string,
): Promise<Transaction[]> {
  const db = await openDb()
  const idx = tx(db, STORE_TXNS, 'readonly')
    .objectStore(STORE_TXNS)
    .index('accountId')
  const all = await reqAsPromise(
    idx.getAll(IDBKeyRange.only(accountId)) as IDBRequest<Transaction[]>,
  )
  return all.sort((a, b) => b.createdAt - a.createdAt)
}

export async function listAllTransactions(): Promise<Transaction[]> {
  const db = await openDb()
  const store = tx(db, STORE_TXNS, 'readonly').objectStore(STORE_TXNS)
  return reqAsPromise(store.getAll() as IDBRequest<Transaction[]>)
}

export async function createTransaction(input: {
  accountId: string
  type: TxnType
  amount: number
  note: string
}): Promise<Transaction> {
  if (!Number.isInteger(input.amount)) {
    throw new Error('Amount must be a whole number of cents')
  }
  if (input.amount <= 0) {
    throw new Error('Amount must be greater than zero')
  }
  if (input.type !== 'credit' && input.type !== 'debit') {
    throw new Error('Invalid transaction type')
  }
  const acc = await getAccount(input.accountId)
  if (!acc) throw new Error('Account not found')

  const txn: Transaction = {
    id: makeId(),
    accountId: input.accountId,
    type: input.type,
    amount: input.amount,
    note: input.note.trim().slice(0, 80),
    createdAt: Date.now(),
  }
  const db = await openDb()
  const t = tx(db, STORE_TXNS, 'readwrite')
  await reqAsPromise(t.objectStore(STORE_TXNS).add(txn))
  return txn
}

export async function getSession(): Promise<Session | undefined> {
  const db = await openDb()
  const store = tx(db, STORE_SESSION, 'readonly').objectStore(STORE_SESSION)
  return reqAsPromise(store.get('current') as IDBRequest<Session | undefined>)
}

export async function setSession(user: string): Promise<Session> {
  const session: Session = {
    key: 'current',
    user,
    loggedInAt: Date.now(),
  }
  const db = await openDb()
  const t = tx(db, STORE_SESSION, 'readwrite')
  await reqAsPromise(t.objectStore(STORE_SESSION).put(session))
  return session
}

export async function clearSession(): Promise<void> {
  const db = await openDb()
  const t = tx(db, STORE_SESSION, 'readwrite')
  await reqAsPromise(t.objectStore(STORE_SESSION).delete('current'))
}
