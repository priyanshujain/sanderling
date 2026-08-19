// A Home reading and the carrier it advances.
//
// A frame that is not Home, and a Home frame whose card list has not rendered,
// are the same case: nothing was read, so the last value we did read is
// reported and carried on unchanged. An empty list is equally "the app has no
// accounts" and "the app has not drawn them yet", and taking it as a reading is
// what would bank an empty window and leave the next rise arriving with no
// budget to cover it.
//
// `fresh` marks the one event that closes a comparison window, so it also
// resets the submit count taken over that window.
export interface HomeReading<T> {
  value: T | null;
  carrier: T | null;
  fresh: boolean;
}

export function readHomeCards<T>(args: {
  onHome: boolean;
  reading: T | null;
  previousCarrier: T | null;
}): HomeReading<T> {
  const { onHome, reading, previousCarrier } = args;
  if (!onHome || reading === null) {
    return { value: previousCarrier, carrier: previousCarrier, fresh: false };
  }
  return { value: reading, carrier: reading, fresh: true };
}

export interface CardReading {
  accountId: string;
  count: number | undefined;
}

// Transactions committed per account, keyed by the account id the card carries.
// A card whose id or count is unreadable is left out rather than guessed at,
// and a reading with nothing left in it is unknown.
export function homeTxnCountsOf(cards: readonly CardReading[]): Record<string, number> | null {
  const counts: Record<string, number> = {};
  for (const card of cards) {
    if (card.accountId === "" || card.count === undefined) continue;
    if (!Number.isSafeInteger(card.count)) continue;
    counts[card.accountId] = card.count;
  }
  return Object.keys(counts).length === 0 ? null : counts;
}

// state.lastAction as the runner builds it (internal/verifier/marshal.go
// lastActionFields), read defensively: every field is what a Go struct decided
// to emit, not something this file can trust a compile-time shape for.
export interface ObservedAction {
  kind?: string;
  on?: string | object;
  applied?: true | null;
}

// Is this the action that commits a transaction? Nothing else in the app
// reaches createTransaction: the add-transaction form's onSubmit is the only
// caller, and the txn-submit button is the only control that submits it. A
// double tap counts once, because it is one action.
export function isTxnSubmitTap(lastAction: ObservedAction | null): boolean {
  if (lastAction == null) return false;
  if (lastAction.kind !== "Tap" && lastAction.kind !== "DoubleTap") return false;
  const on = lastAction.on;
  const onText = typeof on === "string" ? on : on != null ? JSON.stringify(on) : "";
  return onText.includes("txn-submit");
}

// Could the app have committed anything for that submit? The amount field as
// the LANDING frame shows it is the form state the tap read: one action runs
// per step and a tap changes no field, so nothing else could have. Off the
// transaction screen there is no field to read, and undefined is unknown,
// which counts.
//
// False only where the app's own code must have refused. This mirrors
// parseCents (src/format.ts) plus the handler's own `cents <= 0` rejection, and
// an empty field never reaches either: the submit button is disabled while the
// amount is blank, so no click fires at all.
//
// This is the difference between a bound and a useless one. The window is an
// upper bound on the transactions its interval could hold, and a bound inflated
// by taps that commit nothing is a bound a double submit hides behind: of 108
// add-transaction frames in the recalibration run, 65 showed the amount field
// empty.
export function submitCouldCommit(amountText: string | undefined): boolean {
  if (amountText === undefined) return true;
  const trimmed = amountText.trim().replace(/,/g, "");
  if (!/^\d+(\.\d{1,2})?$/.test(trimmed)) return false;
  const dot = trimmed.indexOf(".");
  const whole = dot < 0 ? trimmed : trimmed.slice(0, dot);
  const fraction = dot < 0 ? "" : trimmed.slice(dot + 1);
  const cents = Number(whole) * 100 + Number((fraction + "00").slice(0, 2));
  return Number.isSafeInteger(cents) && cents > 0;
}

// Counts the submit actions inside the window the counting property compares
// over: from the last Home reading we took to this step, inclusive of this
// step's action.
//
// Counting ACTIONS rather than transactions is the whole point. A double tap is
// one action that commits two transactions, which is precisely the defect, so a
// rule phrased in transactions could not tell it apart from two healthy
// submits.
//
// A submit whose dispatch the runner could not confirm counts, because this
// number is an upper bound and the tap may well have landed.
export function countSubmitsInWindow(args: {
  previousCount: number;
  lastAction: ObservedAction | null;
  amountText: string | undefined;
  fresh: boolean;
}): { reported: number; next: number } {
  const { previousCount, lastAction, amountText, fresh } = args;
  const counts = isTxnSubmitTap(lastAction) && submitCouldCommit(amountText);
  const reported = previousCount + (counts ? 1 : 0);
  return { reported, next: fresh ? 0 : reported };
}

// Every accepted submit commits exactly one transaction, so over any window the
// number of transactions committed cannot exceed the number of submit actions
// taken. A double submit is one action committing two, which is the only way to
// break it. Refused submits commit nothing and are not counted, so the healthy
// case sits at or under the bound rather than at it.
//
// Only accounts present in BOTH readings are counted, and only upward movement.
// A card that could not be read drops out of the sum, which makes the result a
// LOWER BOUND on the transactions committed; a lower bound that already exceeds
// the submit count is still a real violation. Missing cards can cost a
// detection, they cannot manufacture one.
//
// Transactions are never deleted, so a per-account count only ever rises.
export function committedTransactionsExceedSubmits(args: {
  countsBefore: Record<string, number> | null;
  countsAfter: Record<string, number> | null;
  submitsInWindow: number;
}): boolean {
  const { countsBefore, countsAfter, submitsInWindow } = args;
  if (countsBefore === null || countsAfter === null) return false;
  if (!Number.isSafeInteger(submitsInWindow)) return false;
  let committed = 0;
  for (const accountId of Object.keys(countsAfter)) {
    const before = countsBefore[accountId];
    const after = countsAfter[accountId];
    if (before === undefined || after === undefined) continue;
    if (after > before) committed += after - before;
  }
  return committed > submitsInWindow;
}
