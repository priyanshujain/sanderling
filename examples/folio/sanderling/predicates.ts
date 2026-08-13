// Computes the Home-screen total balance from the visible AccountCard balances.
// When no Home cards are visible (Ledger, AddTransaction, etc.) the carrier
// value is returned so the property compares apples to apples across screen
// transitions. The Ledger LedgerBalance is intentionally ignored because it is
// a single-account number on a different scale than the Home multi-account sum.
// A single unreadable card makes the whole total null: a partial sum looks
// exactly like money moving, and the balance property would fire on a healthy
// step.
export function computeHomeTotalBalance(args: {
  cardBalanceTexts: (string | undefined)[];
  previousCarrier: number | null;
}): number | null {
  const { cardBalanceTexts, previousCarrier } = args;
  if (cardBalanceTexts.length === 0) return previousCarrier;
  let sum = 0;
  for (const text of cardBalanceTexts) {
    const cents = parseDollarCents(text);
    if (cents === null) return null;
    sum += cents;
  }
  return sum;
}

// Parses formatCents output like "$5.00", "-$1,234.56", "+$0.50" back to
// integer cents. Anything that is not a complete amount is null, not 0: a
// balance we could not read is unknown, and reading it as zero silently moves
// the Home total.
export function parseDollarCents(text: string | undefined): number | null {
  if (!text) return null;
  const match = text.trim().match(/^([-+]?)\$?(\d{1,3}(?:,\d{3})*|\d+)\.(\d{2})$/);
  if (!match) return null;
  const [, sign, dollars, cents] = match;
  if (dollars === undefined || cents === undefined) return null;
  return (sign === "-" ? -1 : 1) * (parseInt(dollars.replace(/,/g, ""), 10) * 100 + parseInt(cents, 10));
}

// Compose Multiplatform for Web merges an AccountCard's whole subtree into one
// accessibility node: the AccountName and AccountBalance children that Android
// and iOS expose do not exist there, and the card's own text is
// initials + name + "N transaction(s)" + balance run together with no
// separator, e.g. "INInvestments12 transactions$2,589.00". The two helpers
// below prefer the structured child and only parse the merged text when the
// platform did not give us one.

// The balance is the amount at the very END of the card text. Anchoring there
// is the whole trick: digit-scraping the merged string would swallow the "12"
// of "12 transactions" into the amount.
const TRAILING_BALANCE = /[-+]?\$[\d,]+\.\d{2}\s*$/;

// The transaction-count label sits between the name and the balance.
const TRAILING_TXN_COUNT = /\d+\s*transactions?\s*$/;

export function cardBalanceText(args: {
  childText: string | undefined;
  cardText: string | undefined;
}): string | undefined {
  const { childText, cardText } = args;
  if (childText) return childText;
  const match = cardText?.match(TRAILING_BALANCE);
  return match ? match[0].trim() : undefined;
}

// The result is an identity key, not a display name: off web it is the
// AccountName text, on web it is whatever the merged card text leaves in front
// of the count label, initials and all ("T2Travel" for "Travel 2024"). Its only
// consumer compares it for set membership. Keep it stable, not pretty: the
// count label always starts at the first digit of the run before it, so the
// key does not move as an account's transaction count grows.
export function cardAccountName(args: {
  childText: string | undefined;
  cardText: string | undefined;
}): string {
  const { childText, cardText } = args;
  if (childText) return childText;
  if (!cardText) return "";
  const head = cardText.replace(TRAILING_BALANCE, "");
  const label = head.match(TRAILING_TXN_COUNT);
  if (!label || label.index === undefined) return head.trim();
  return head.slice(0, label.index).trim();
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
  prevTotalBalance: number | null;
  currTotalBalance: number | null;
}): boolean {
  const { route, lastAction, typedAmount, prevTotalBalance, currTotalBalance } = args;
  if (route !== "home") return true;
  if (lastAction == null) return true;
  if (lastAction.kind !== "Tap" && lastAction.kind !== "DoubleTap") return true;
  const on = lastAction.on;
  const onString = typeof on === "string" ? on : on != null ? JSON.stringify(on) : "";
  if (!onString.includes("TxnSubmit")) return true;
  if (typedAmount === 0) return true;
  // An unknown total on either side is not evidence of anything. Comparing one
  // would turn every unreadable Home into a violation.
  if (prevTotalBalance === null || currTotalBalance === null) return true;
  return Math.abs(currTotalBalance - prevTotalBalance) === typedAmount;
}
