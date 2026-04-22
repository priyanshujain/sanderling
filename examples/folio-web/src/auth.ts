export const DEMO_EMAIL = 'demo@ledger.app'
export const DEMO_PASSWORD = 'ledger123'

export function checkCredentials(email: string, password: string): boolean {
  return (
    email.trim().toLowerCase() === DEMO_EMAIL &&
    password === DEMO_PASSWORD
  )
}
