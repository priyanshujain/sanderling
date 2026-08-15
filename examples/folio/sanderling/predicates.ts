// The screen a frame shows, or null when it does not show exactly one.
//
// `present` answers whether a screen's marker node is in the accessibility
// tree. Android puts two markers there constantly: its hierarchy dump carries
// the outgoing and the incoming screen together on 425 of 1879 steps measured
// across 17 runs, better than one frame in five, occasionally three at once.
// iOS produced none in 2558 steps and web one in 560, so this is not a rule
// invented for a hypothetical.
//
// Such a frame is a navigation transition, and it is evidence about neither
// screen. Ranking the markers and taking the first is what convicted folio's
// spec in 11 android runs: `route` answered add-transaction off the screen
// being torn down while a separate unscoped find answered "on Home" off the one
// being built, so a half-rendered Home counted as a fresh balance reading and
// reset the submit window under a tap that had not landed yet. Every reading
// below takes the route as its only input, so there is no second answer left
// for it to disagree with.
//
// The engine already draws this line: a transitional tree yields no builtin
// targets at all (see targets() in internal/verifier), so a spec that keeps
// picking one of the two screens is the only thing still handing out
// coordinates from an animation.
export function routeOfFrame<R extends string>(
  screens: Record<R, string>,
  present: (tag: string) => boolean,
): R | null {
  let shown: R | null = null;
  for (const route of Object.keys(screens) as R[]) {
    if (!present(screens[route])) continue;
    if (shown !== null) return null;
    shown = route;
  }
  return shown;
}

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
//   - anywhere but Home there is nothing to read, so the last total we actually
//     read is reported and carried on unchanged. A transition frame is one of
//     those places: its route is null, which is not "home";
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
  route: string | null;
  totalText: string | undefined;
  previousCarrier: number | null;
}): HomeTotalReading {
  const { route, totalText, previousCarrier } = args;
  if (route !== "home") return { value: previousCarrier, carrier: previousCarrier, fresh: false };
  const total = parseDollarCents(totalText);
  if (total === null) return { value: null, carrier: previousCarrier, fresh: false };
  return { value: total, carrier: total, fresh: true };
}

// The same reading, taken off Home's account cards instead of its total, for
// the readings the spec carries between Home visits (the account list and the
// per-account transaction counts).
//
// The empty reading is the whole point. Android renders Home's own node a frame
// or two before its list, so `findAll` over the cards comes back empty while the
// screen is already claiming to be Home. That is UNKNOWN, not "no accounts":
// writing it into the carrier is the poisoning readHomeTotalBalance was already
// fixed for. It killed the counting invariant outright on android, where
// counts_prev was {} at every evaluation point of all 17 runs measured.
//
// Callers pass null for a reading they could not take, so an empty list and an
// empty map are one case here rather than two.
export interface HomeCardReading<T> {
  value: T | null;
  carrier: T | null;
  fresh: boolean;
}

export function readHomeCards<T>(args: {
  route: string | null;
  reading: T | null;
  previousCarrier: T | null;
}): HomeCardReading<T> {
  const { route, reading, previousCarrier } = args;
  if (route !== "home") return { value: previousCarrier, carrier: previousCarrier, fresh: false };
  if (reading === null) return { value: null, carrier: previousCarrier, fresh: false };
  return { value: reading, carrier: reading, fresh: true };
}

function isTapOn(
  lastAction: { kind?: string; on?: string | object } | null,
  target: string,
): boolean {
  if (lastAction == null) return false;
  if (lastAction.kind !== "Tap" && lastAction.kind !== "DoubleTap") return false;
  const on = lastAction.on;
  const onString = typeof on === "string" ? on : on != null ? JSON.stringify(on) : "";
  return onString.includes(target);
}

// Is this the action that commits a transaction? Nothing else in Folio reaches
// Repository.createTransaction: AddTransactionViewModel.submit() is the only
// caller, AddTransactionEvent.Submit is the only thing that runs it, and the
// TxnSubmit button's onClick is the only thing that sends that event.
export function isTxnSubmitTap(
  lastAction: { kind?: string; on?: string | object } | null,
): boolean {
  return isTapOn(lastAction, "TxnSubmit");
}

// Likewise the only action that creates an account.
export function isAddAccountSubmitTap(
  lastAction: { kind?: string; on?: string | object } | null,
): boolean {
  return isTapOn(lastAction, "AddAccountSubmit");
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

// One card's transaction count, in the strongest form its SOURCE supports. The
// two forms are the whole reason this is not just a number:
//
//   - a NUMBER when the count came from the card's own AccountTxnCount node.
//     That node's text is the count label and nothing else, so its digits are
//     the count and subtracting two readings is exact arithmetic;
//   - the digit RUN as TEXT when the count had to be recovered from merged card
//     text. Web merges the AccountCard subtree into one node, and an account
//     whose name ends in digits runs them into the count: the account named
//     "-1" holding 2 transactions merges to "-1-12 transactions", whose maximal
//     digit run reads 12. That prefix is fixed for a given account, so two
//     readings whose runs are the SAME LENGTH still differ by exactly the true
//     difference (19 to 120 is impossible; 19 to 110 is a length change), while
//     two of different lengths do not: 9 to 10 reads as 19 to 110, a delta of
//     91 out of a delta of 1. Keeping the run as text is what lets
//     committedTransactionsExceedSubmits see the length change and drop the
//     pair instead of convicting on it.
//
// Carrying the distinction is what keeps that length rule where it belongs.
// There is no name in front of a dedicated node's digits for it to protect
// against, so applied there it buys nothing and throws away real evidence every
// time an account crosses a decade: of the three android seed-9 runs that
// finished without convicting, two had dropped a window on this rule, one
// reading 7 against 12 and the other 4 against 10.
//
// This is a fact about the reading, not about the platform. A platform that
// starts exposing the node gets exact counts by exposing it, and one that stops
// falls back to the text rule on the same step it stops.
export type TxnCount = number | string;

export function cardTxnCount(args: {
  childText: string | undefined;
  cardText: string | undefined;
}): TxnCount | undefined {
  const { childText, cardText } = args;
  const source = childText ?? cardText?.replace(TRAILING_BALANCE, "");
  const digits = source?.match(TRAILING_TXN_COUNT)?.[1];
  if (digits === undefined) return undefined;
  return childText === undefined ? digits : parseInt(digits, 10);
}

export interface Account {
  // Identity key, not a display name: on web it carries the card's initials.
  name: string;
  // null when the card's balance could not be read at all (see cardBalanceText).
  balance: number | null;
}

// One Home card, already parsed by the three helpers above.
export interface CardReading extends Account {
  count: TxnCount | undefined;
}

// The two readings Home's card list yields, each null when there is nothing in
// it to read. Both feed readHomeCards, which is where null stops the carrier
// from being overwritten.
//
// An account list is empty for two reasons a frame cannot tell apart: the app
// has no accounts, or it has not drawn them yet. Calling both unknown costs one
// detection, the very first account of a run, and buys back every card that
// arrived late.
export function homeAccountsOf(cards: readonly CardReading[]): Account[] | null {
  if (cards.length === 0) return null;
  return cards.map(({ name, balance }) => ({ name, balance }));
}

// A card whose name or count is unreadable is left out rather than guessed at;
// committedTransactionsExceedSubmits treats a missing account as no evidence.
// Every card being unreadable leaves nothing to compare, which is unknown.
//
// A name carried by more than one card is left out for the same reason, the
// rule createdAccountHasNonZeroBalance applies with `matches.length === 1`:
// nothing here can say which of them a count came from. Folio accepts the same
// account name twice and Home lists whatever fits the viewport, so a reading
// that saw one Travel card and a later one that saw two would otherwise
// subtract two DIFFERENT accounts' counts and convict a healthy app of
// double-submitting. The twin does not have to be readable to spoil the
// identity, so duplicates are counted over every card, not just the usable
// ones. Dropping a card can only ever cost a detection.
export function homeTxnCountsOf(cards: readonly CardReading[]): Record<string, TxnCount> | null {
  const cardsPerName = new Map<string, number>();
  for (const card of cards) cardsPerName.set(card.name, (cardsPerName.get(card.name) ?? 0) + 1);
  const counts: Record<string, TxnCount> = {};
  for (const card of cards) {
    if (card.name === "" || card.count === undefined) continue;
    if (cardsPerName.get(card.name) !== 1) continue;
    counts[card.name] = card.count;
  }
  return Object.keys(counts).length === 0 ? null : counts;
}

// Did the account the fuzzer just created come into existence holding money?
//
// "Appeared in the visible set" is not "was created". Home lists the accounts
// that fit the viewport, so an existing account arrives in a later reading
// whenever the list scrolls, whenever a card that was clipped becomes laid out,
// and (before the route fix) whenever the earlier reading came off a
// half-rendered Home mid-transition. All three read as a brand new account, and
// the last two convicted this property on android over a Travel account holding
// $24,112.00 and a Savings account holding $429,585.00.
//
// The one appearance that IS attributable to a creation is the account the
// fuzzer asked for: the name it typed into AddAccountScreen, judged on the step
// where that screen's submit landed on Home. Everything else that shows up is a
// card that came into view, and this property has nothing to say about it.
//
// Returns true only for a violation it can attribute. Unattributable is not the
// same as fine, and both come back false here.
export function createdAccountHasNonZeroBalance(args: {
  route: string | null;
  lastAction: { kind?: string; on?: string | object } | null;
  typedName: string | undefined;
  before: Account[] | null;
  after: Account[] | null;
}): boolean {
  const { route, lastAction, before, after } = args;
  if (route !== "home") return false;
  if (!isAddAccountSubmitTap(lastAction)) return false;
  if (before === null || after === null) return false;
  const typed = (args.typedName ?? "").trim();
  if (typed === "") return false;
  // Web merges the card into one node whose text opens with the avatar
  // initials, so the identity key is "INInvestments" where android and iOS give
  // "Investments"; endsWith covers both. Two cards answering to the same typed
  // name (a second "Travel", or a card the tree exposed twice) leave the
  // appearance unattributable, so nothing is judged.
  const matches = after.filter(account => account.name.endsWith(typed));
  const created = matches.length === 1 ? matches[0] : undefined;
  if (created === undefined) return false;
  if (before.some(account => account.name === created.name)) return false;
  return created.balance !== null && created.balance !== 0;
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
  countsBefore: Record<string, TxnCount> | null;
  countsAfter: Record<string, TxnCount> | null;
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
    const rise = countRise(before, after);
    if (rise !== null && rise > 0) committed += rise;
  }
  return committed > submitsInWindow;
}

// How far one account's count rose between two readings, or null when the pair
// is not comparable. Not comparable is not zero: the account drops out of the
// sum entirely, which can only cost a detection.
function countRise(before: TxnCount, after: TxnCount): number | null {
  if (typeof before === "number" && typeof after === "number") {
    if (!Number.isSafeInteger(before) || !Number.isSafeInteger(after)) return null;
    return after - before;
  }
  // Recovered from merged card text, where the run may carry an account-name
  // prefix, so only equal-length runs subtract to the true difference: see
  // TxnCount. A pair whose two readings came from different sources is one
  // neither rule can vouch for, and it is dropped with them.
  if (typeof before !== "string" || typeof after !== "string") return null;
  if (before.length !== after.length) return null;
  const from = parseInt(before, 10);
  const to = parseInt(after, 10);
  if (!Number.isSafeInteger(from) || !Number.isSafeInteger(to)) return null;
  return to - from;
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
