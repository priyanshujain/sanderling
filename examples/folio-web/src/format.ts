import type { Transaction } from './db'

const CURRENCY = '$'

export function formatCents(
  cents: number,
  opts: { signed?: boolean } = {},
): string {
  const abs = Math.abs(cents)
  const dollars = Math.floor(abs / 100)
  const rem = abs % 100
  const dollarsStr = dollars.toLocaleString('en-US')
  const formatted = `${CURRENCY}${dollarsStr}.${rem.toString().padStart(2, '0')}`
  if (cents < 0) return `-${formatted}`
  if (opts.signed && cents > 0) return `+${formatted}`
  return formatted
}

export function parseCents(input: string): number | null {
  const trimmed = input.trim().replace(/,/g, '')
  if (trimmed === '') return null
  if (!/^\d+(\.\d{1,2})?$/.test(trimmed)) return null
  const [whole, frac = ''] = trimmed.split('.')
  const fracPadded = (frac + '00').slice(0, 2)
  const total = Number(whole) * 100 + Number(fracPadded)
  if (!Number.isFinite(total) || !Number.isInteger(total)) return null
  if (total > Number.MAX_SAFE_INTEGER) return null
  return total
}

export function signedAmount(t: Transaction): number {
  return t.type === 'credit' ? t.amount : -t.amount
}

export function balanceOf(txns: Transaction[]): number {
  let total = 0
  for (const t of txns) total += signedAmount(t)
  return total
}

export function initialsOf(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '?'
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase()
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase()
}

const DATE_FMT = new Intl.DateTimeFormat('en-US', {
  month: 'short',
  day: 'numeric',
  hour: 'numeric',
  minute: '2-digit',
})

export function formatDate(ts: number): string {
  return DATE_FMT.format(new Date(ts))
}

const CLOCK_FMT = new Intl.DateTimeFormat('en-US', {
  hour: 'numeric',
  minute: '2-digit',
  hour12: false,
})

export function formatClock(ts: number): string {
  return CLOCK_FMT.format(new Date(ts))
}
