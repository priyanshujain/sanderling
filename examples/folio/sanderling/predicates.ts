// Reads the Home screen's own TOTAL BALANCE node and advances the carrier the
// spec holds between Home visits.
//
// The app computes that number over ALL accounts, so it does not care which
// account cards happen to be laid out inside the viewport. Summing the visible
// AccountCard balances did: a card clipped at the bottom edge exposes no
// AccountBalance child, and a partial sum looks exactly like money moving.
//
// Three cases, and the difference between the reported value and the carrier is
// the whole point:
//   - off Home there is nothing to read, so the last total we actually read is
//     reported and carried on unchanged;
//   - on Home with an unreadable total the reading is UNKNOWN, so null is
//     reported (the property treats null as vacuous) while the carrier keeps
//     the last value we did read. Writing null into the carrier is what used to
//     poison every later step: off-Home steps hand the carrier back, so one
//     unreadable Home turned the property vacuous for the rest of the run;
//   - on Home with a readable total, that total is both the reading and the new
//     carrier, and `fresh` says the comparison window closes here.
export interface HomeTotalReading {
  value: number | null;
  carrier: number | null;
  fresh: boolean;
}

export function readHomeTotalBalance(args: {
  onHome: boolean;
  totalText: string | undefined;
  previousCarrier: number | null;
}): HomeTotalReading {
  const { onHome, totalText, previousCarrier } = args;
  if (!onHome) return { value: previousCarrier, carrier: previousCarrier, fresh: false };
  const total = parseDollarCents(totalText);
  if (total === null) return { value: null, carrier: previousCarrier, fresh: false };
  return { value: total, carrier: total, fresh: true };
}

// Is this the action that commits a transaction? Nothing else in Folio reaches
// Repository.createTransaction: AddTransactionViewModel.submit() is the only
// caller, AddTransactionEvent.Submit is the only thing that runs it, and the
// TxnSubmit button's onClick is the only thing that sends that event.
export function isTxnSubmitTap(
  lastAction: { kind?: string; on?: string | object } | null,
): boolean {
  if (lastAction == null) return false;
  if (lastAction.kind !== "Tap" && lastAction.kind !== "DoubleTap") return false;
  const on = lastAction.on;
  const onString = typeof on === "string" ? on : on != null ? JSON.stringify(on) : "";
  return onString.includes("TxnSubmit");
}

// Counts the submit actions inside the window the balance property compares
// over: from the last Home total we read to this step, inclusive of this step's
// action.
//
// Counting ACTIONS rather than transactions is deliberate. A double-tap is one
// action that commits two transactions, which is precisely the bug, so a rule
// phrased in transactions could not tell the bug apart from two healthy
// submits. A rule phrased in actions can: one action in the window means the
// whole balance delta belongs to that action, and comparing it against the
// amount typed for it is fair.
//
// The reset lands on `fresh`, the same event that advances the carrier, so the
// count always describes exactly the interval the two compared totals span.
export function countSubmitsInWindow(args: {
  previousCount: number;
  lastAction: { kind?: string; on?: string | object } | null;
  fresh: boolean;
}): { reported: number; next: number } {
  const { previousCount, lastAction, fresh } = args;
  const reported = previousCount + (isTxnSubmitTap(lastAction) ? 1 : 0);
  return { reported, next: fresh ? 0 : reported };
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

// The transaction-count label sits between the name and the balance. The group
// captures the digit run so the count can be read from the same match.
const TRAILING_TXN_COUNT = /(\d+)\s*transactions?\s*$/;

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

// The digit run in front of a card's "transaction(s)" label, kept as TEXT.
//
// A string rather than a number because web merges the card into one node and
// an account whose name ends in digits runs them into the count: the account
// named "-1" holding 2 transactions merges to "-1-12 transactions", whose
// maximal digit run reads 12. That prefix is fixed for a given account, so two
// readings whose runs are the SAME LENGTH still differ by exactly the true
// difference (19 to 120 is impossible; 19 to 110 is a length change). Two runs
// of different lengths do not, and 9 to 10 would read as 19 to 110, a delta of
// 91 out of a delta of 1. Keeping the run as text is what lets the predicate
// see the length change and drop the pair instead of convicting on it.
//
// Android and iOS expose AccountTxnCount as its own node, where the run is just
// the count and the length rule costs nothing but a window per decade.
export function cardTxnCountDigits(args: {
  childText: string | undefined;
  cardText: string | undefined;
}): string | undefined {
  const { childText, cardText } = args;
  const source = childText ?? cardText?.replace(TRAILING_BALANCE, "");
  return source?.match(TRAILING_TXN_COUNT)?.[1];
}

// Every accepted submit commits exactly one transaction, so over any window the
// number of transactions committed cannot exceed the number of submit actions
// taken. A double-submit is one action committing two, which is the only way to
// break it. Rejected submits (parseCents refuses the amount) commit nothing, so
// the normal case sits comfortably under the bound.
//
// Only accounts present in BOTH readings are counted, and only upward movement.
// A card that scrolled out of the viewport, or one whose count could not be
// read, simply drops out of the sum. That makes the result a LOWER BOUND on the
// transactions committed, and a lower bound that already exceeds the submit
// count is still a real violation. Missing cards can cost a detection; they
// cannot manufacture one.
//
// Transactions are never deleted (Ledger.sq has no DELETE for them), so a
// per-account count only ever rises; the max(0, ...) is defensive, not load
// bearing.
export function committedTransactionsExceedSubmits(args: {
  countsBefore: Record<string, string> | null;
  countsAfter: Record<string, string> | null;
  submitsInWindow: number;
}): boolean {
  const { countsBefore, countsAfter, submitsInWindow } = args;
  if (countsBefore === null || countsAfter === null) return false;
  if (!Number.isSafeInteger(submitsInWindow)) return false;
  let committed = 0;
  for (const name of Object.keys(countsAfter)) {
    const before = countsBefore[name];
    const after = countsAfter[name];
    if (before === undefined || after === undefined) continue;
    // Different run lengths are not comparable: see cardTxnCountDigits.
    if (before.length !== after.length) continue;
    const from = parseInt(before, 10);
    const to = parseInt(after, 10);
    if (!Number.isSafeInteger(from) || !Number.isSafeInteger(to)) continue;
    if (to > from) committed += to - from;
  }
  return committed > submitsInWindow;
}

// Parses raw user input in the transaction amount field into integer cents.
// Mirrors the Folio app's parseCents (app/shared/.../util/Format.kt): whole
// numbers like "50" become 5000 cents, decimals like "5.50" become 550, more
// than 2 decimals or non-numeric input return 0. A sign is NOT tolerated,
// because parseCents does not tolerate one either: it rejects "-50" outright,
// so the app creates no transaction, and reading the field as a real amount
// would make the property demand a balance move that never happened.
//
// 0 also means "an amount this reading cannot represent". parseCents returns
// null once the whole part no longer fits a Kotlin Long, and its cents fit a
// Long where ours are float64 doubles, exact only to Number.MAX_SAFE_INTEGER.
// Past that the digits we compute are not the digits the app applied, so the
// reading is unknown rather than large, and 0 routes it to the property's
// vacuous branch.
export function parseTypedAmount(text: string | undefined | null): number {
  if (!text) return 0;
  const trimmed = text.trim().replace(/,/g, "");
  if (!/^\d+(\.\d{1,2})?$/.test(trimmed)) return 0;
  const dot = trimmed.indexOf(".");
  const whole = dot < 0 ? trimmed : trimmed.slice(0, dot);
  const frac = dot < 0 ? "" : trimmed.slice(dot + 1);
  const fracPadded = (frac + "00").slice(0, 2);
  const cents = parseInt(whole, 10) * 100 + parseInt(fracPadded, 10);
  return Number.isSafeInteger(cents) ? cents : 0;
}

// When the last action is a tap (or double-tap) on the transaction Submit
// button, the absolute change in total balance must equal the amount the
// user typed. A double-submit lands two transactions and shifts the balance
// by 2x the typed amount, tripping this check. The route gate skips steps
// whose landing screen is not Home: totalBalance is only freshly read from
// Home's own TOTAL BALANCE node, so off-Home comparisons would read a stale
// carrier value and false-fire.
//
// submitsInWindow is what keeps the comparison honest. prevTotalBalance is the
// last total we READ, not the total as of the previous transaction, so the two
// compared numbers can straddle any number of commits: a real run produced a
// 13000 delta against a typed 19600 because the window held a double-submit's
// two 19600 debits AND an unrelated 26200 credit. A delta like that is not
// evidence about the amount typed into any one submit, so anything other than
// exactly one submit action in the window is vacuous. Exactly one still catches
// the bug: the double-tap is a single action.
export function submitChangesBalanceByTypedAmount(args: {
  route: string | null;
  lastAction: { kind?: string; on?: string | object } | null;
  submitsInWindow: number;
  typedAmount: number;
  prevTotalBalance: number | null;
  currTotalBalance: number | null;
}): boolean {
  const { route, lastAction, submitsInWindow, typedAmount } = args;
  const { prevTotalBalance, currTotalBalance } = args;

  if (route !== "home") return true;
  if (!isTxnSubmitTap(lastAction)) return true;
  if (submitsInWindow !== 1) return true;
  if (typedAmount === 0) return true;
  // An unknown total on either side is not evidence of anything. Comparing one
  // would turn every unreadable Home into a violation.
  if (prevTotalBalance === null || currTotalBalance === null) return true;
  // Same rule for a total we cannot hold exactly. These are integer cents in
  // float64, exact only up to Number.MAX_SAFE_INTEGER. The app caps a
  // transaction at whatever fits a Kotlin Long, so a balance of ~1e18 cents is
  // one accepted amount away, and up there the gap between representable
  // values is 128 cents: a real 1600-cent move reads back as something else
  // entirely. The equality below is then false for a healthy single submit
  // exactly as readily as for a double one, and a check that cannot pass is not
  // a check that failed.
  //
  // The three guarded quantities are the ones that lose precision on their own:
  // each balance, because the number parsed out of the card text stops being
  // the number the card shows, and the typed amount, because parseTypedAmount
  // multiplies by 100. Their difference needs no guard of its own: IEEE
  // subtraction of two exact integers is correctly rounded, so if the real
  // difference equals a safe typedAmount it is itself safe and comes out exact,
  // and if it does not, it cannot round INTO a safe typedAmount either. Adding
  // a magnitude check there would only silence honest mismatches.
  if (!Number.isSafeInteger(prevTotalBalance)) return true;
  if (!Number.isSafeInteger(currTotalBalance)) return true;
  if (!Number.isSafeInteger(typedAmount)) return true;
  return Math.abs(currTotalBalance - prevTotalBalance) === typedAmount;
}
