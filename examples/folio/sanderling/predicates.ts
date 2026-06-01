// Computes the Home-screen total balance from the visible AccountCard balances.
// When no Home cards are visible (Ledger, AddTransaction, etc.) the carrier
// value is returned so the property compares apples to apples across screen
// transitions. The Ledger LedgerBalance is intentionally ignored because it is
// a single-account number on a different scale than the Home multi-account sum.
export function computeHomeTotalBalance(args: {
  cardBalanceTexts: (string | undefined)[];
  previousCarrier: number;
}): number {
  const { cardBalanceTexts, previousCarrier } = args;
  if (cardBalanceTexts.length === 0) return previousCarrier;
  return cardBalanceTexts.reduce((sum, text) => sum + parseDollarCents(text), 0);
}

// Parses formatCents output like "$5.00", "-$1,234.56", "+$0.50" back to integer cents.
function parseDollarCents(text: string | undefined): number {
  if (!text) return 0;
  const sign = text.startsWith("-") ? -1 : 1;
  const digits = text.replace(/[^0-9]/g, "");
  return digits ? sign * parseInt(digits, 10) : 0;
}

// Parses raw user input in the transaction amount field into integer cents.
// Mirrors the Folio app's parseCents: whole numbers like "50" become 5000
// cents, decimals like "5.50" become 550, more than 2 decimals or non-numeric
// input return 0. Leading +/- signs are tolerated and treated as positive.
export function parseTypedAmount(text: string | undefined | null): number {
  if (!text) return 0;
  let trimmed = text.trim().replace(/,/g, "");
  if (trimmed.startsWith("+") || trimmed.startsWith("-")) trimmed = trimmed.slice(1);
  if (!/^\d+(\.\d{1,2})?$/.test(trimmed)) return 0;
  const dot = trimmed.indexOf(".");
  const whole = dot < 0 ? trimmed : trimmed.slice(0, dot);
  const frac = dot < 0 ? "" : trimmed.slice(dot + 1);
  const fracPadded = (frac + "00").slice(0, 2);
  return parseInt(whole, 10) * 100 + parseInt(fracPadded, 10);
}

// When the last action is a tap (or double-tap) on the transaction Submit
// button, the absolute change in total balance must equal the amount the
// user typed. A double-submit lands two transactions and shifts the balance
// by 2x the typed amount, tripping this check. The route gate skips steps
// whose landing screen is not Home: totalBalance is only freshly computed
// from visible AccountCards on Home, so off-Home comparisons would read a
// stale carrier value and false-fire.
export function submitChangesBalanceByTypedAmount(args: {
  route: string | null;
  lastAction: { kind?: string; on?: string | object } | null;
  typedAmount: number;
  prevTotalBalance: number;
  currTotalBalance: number;
}): boolean {
  const { route, lastAction, typedAmount, prevTotalBalance, currTotalBalance } = args;
  if (route !== "home") return true;
  if (lastAction == null) return true;
  if (lastAction.kind !== "Tap" && lastAction.kind !== "DoubleTap") return true;
  const on = lastAction.on;
  const onString = typeof on === "string" ? on : on != null ? JSON.stringify(on) : "";
  if (!onString.includes("TxnSubmit")) return true;
  if (typedAmount === 0) return true;
  return Math.abs(currTotalBalance - prevTotalBalance) === typedAmount;
}
