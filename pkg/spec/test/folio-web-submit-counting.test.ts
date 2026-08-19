import assert from "node:assert/strict";
import { test } from "node:test";

import {
  committedTransactionsExceedSubmits,
  countSubmitsInWindow,
  homeTxnCountsOf,
  isTxnSubmitTap,
  readHomeCards,
  submitCouldCommit,
} from "../../../examples/folio-web/sanderling/predicates.ts";
import type {
  CardReading,
  ObservedAction,
} from "../../../examples/folio-web/sanderling/predicates.ts";

const CHECKING = "acct-checking";
const SAVINGS = "acct-savings";

const submitTap = (): ObservedAction => ({ kind: "Tap", on: "id:txn-submit", applied: true });
const submitDoubleTap = (): ObservedAction => ({
  kind: "DoubleTap",
  on: "id:txn-submit",
  applied: true,
});
const otherTap = (on: string): ObservedAction => ({ kind: "Tap", on, applied: true });

test("a submit tap and a submit double tap are both one submit action", () => {
  assert.equal(isTxnSubmitTap(submitTap()), true);
  assert.equal(isTxnSubmitTap(submitDoubleTap()), true);
});

test("taps on other controls are not submits", () => {
  assert.equal(isTxnSubmitTap(otherTap("id:add-txn")), false);
  assert.equal(isTxnSubmitTap(otherTap("id:add-account-submit")), false);
  assert.equal(isTxnSubmitTap({ kind: "InputText", on: "id:txn-amount" }), false);
  assert.equal(isTxnSubmitTap(null), false);
});

// The disabled button and parseCents (src/format.ts) between them refuse these,
// so they raise no bound. Of 108 add-transaction frames in the recalibration
// run, 65 carried an empty amount field.
test("amounts the app must have refused do not count as submits", () => {
  assert.equal(submitCouldCommit(""), false);
  assert.equal(submitCouldCommit("   "), false);
  assert.equal(submitCouldCommit("0"), false);
  assert.equal(submitCouldCommit("0.00"), false);
  assert.equal(submitCouldCommit("-5"), false);
  assert.equal(submitCouldCommit("abc"), false);
  assert.equal(submitCouldCommit("1.234"), false);
  assert.equal(submitCouldCommit("999999999999999999999"), false);
});

test("amounts the app accepts count as submits", () => {
  assert.equal(submitCouldCommit("1"), true);
  assert.equal(submitCouldCommit("12.34"), true);
  assert.equal(submitCouldCommit("0.01"), true);
  assert.equal(submitCouldCommit("99999"), true);
  assert.equal(submitCouldCommit("1,234"), true);
});

// Off the transaction screen there is no field to read, and unknown counts:
// the bound has to hold every submit the window could contain.
test("an unreadable amount counts, because the tap may have committed", () => {
  assert.equal(submitCouldCommit(undefined), true);
});

const before = { [CHECKING]: 3, [SAVINGS]: 1 };

test("healthy window: three submits, three transactions", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: before,
      countsAfter: { [CHECKING]: 5, [SAVINGS]: 2 },
      submitsInWindow: 3,
    }),
    false,
  );
});

test("refused submits commit nothing, which is under the bound", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: before,
      countsAfter: before,
      submitsInWindow: 4,
    }),
    false,
  );
});

// The defect, stated directly: one action, two rows.
test("double submit: one action commits two transactions", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: before,
      countsAfter: { [CHECKING]: 5, [SAVINGS]: 1 },
      submitsInWindow: 1,
    }),
    true,
  );
});

test("boundary: committed equal to the submit count is not a violation", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: before,
      countsAfter: { [CHECKING]: 4, [SAVINGS]: 1 },
      submitsInWindow: 1,
    }),
    false,
  );
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: before,
      countsAfter: { [CHECKING]: 4, [SAVINGS]: 2 },
      submitsInWindow: 1,
    }),
    true,
  );
});

// Counting actions against transactions rather than gating on a one-submit
// window is what lets a wide window stay evidence.
test("wide window: five submits committing six transactions still fires", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: before,
      countsAfter: { [CHECKING]: 8, [SAVINGS]: 4 },
      submitsInWindow: 5,
    }),
    true,
  );
});

// A card that could not be read drops out of the sum, so the result is a lower
// bound on what committed. Losing a card can cost a detection; it must never
// manufacture one.
test("an account missing from either reading is not counted", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { [CHECKING]: 3, [SAVINGS]: 1 },
      countsAfter: { [CHECKING]: 3 },
      submitsInWindow: 0,
    }),
    false,
  );
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { [CHECKING]: 3 },
      countsAfter: { [CHECKING]: 3, [SAVINGS]: 9 },
      submitsInWindow: 0,
    }),
    false,
  );
});

test("an unknown reading on either side is not evidence", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: null,
      countsAfter: { [CHECKING]: 99 },
      submitsInWindow: 0,
    }),
    false,
  );
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { [CHECKING]: 0 },
      countsAfter: null,
      submitsInWindow: 0,
    }),
    false,
  );
});

test("a home frame whose cards have not rendered is unknown, not zero accounts", () => {
  assert.equal(homeTxnCountsOf([]), null);
  assert.equal(homeTxnCountsOf([{ accountId: CHECKING, count: undefined }]), null);
  assert.deepEqual(homeTxnCountsOf([{ accountId: CHECKING, count: 0 }]), { [CHECKING]: 0 });
});

test("a card with no readable id is left out rather than guessed at", () => {
  assert.deepEqual(
    homeTxnCountsOf([
      { accountId: "", count: 4 },
      { accountId: SAVINGS, count: 1 },
    ]),
    { [SAVINGS]: 1 },
  );
});

// One step of a run, as the spec sees it: whether this frame is Home, the cards
// on it, the action that landed on it, and the amount field it shows.
interface Frame {
  onHome: boolean;
  cards?: readonly CardReading[];
  lastAction?: ObservedAction;
  amountText?: string;
}

// Replays frames through the same three predicates the spec wires together,
// returning the verdict of submitCommitsOneTransactionPerAction at each step.
function replay(frames: readonly Frame[]): boolean[] {
  let carrier: Record<string, number> | null = null;
  let previousCounts: Record<string, number> | null = null;
  let submits = 0;
  return frames.map((frame) => {
    const reading = readHomeCards({
      onHome: frame.onHome,
      reading: frame.cards ? homeTxnCountsOf(frame.cards) : null,
      previousCarrier: carrier,
    });
    carrier = reading.carrier;
    const counted = countSubmitsInWindow({
      previousCount: submits,
      lastAction: frame.lastAction ?? null,
      amountText: frame.amountText,
      fresh: reading.fresh,
    });
    submits = counted.next;
    const violated = committedTransactionsExceedSubmits({
      countsBefore: previousCounts,
      countsAfter: reading.value,
      submitsInWindow: counted.reported,
    });
    previousCounts = reading.value;
    return violated;
  });
}

const home = (count: number): Frame => ({
  onHome: true,
  cards: [{ accountId: CHECKING, count }],
});

// The walk the fuzzer takes: Home, into the account, into the form, type, tap
// submit, back out, Home again. The two compared readings are eight steps
// apart and the property has to stay quiet the whole way.
const walkToSubmit: readonly Frame[] = [
  home(3),
  { onHome: false, lastAction: otherTap("desc:Checking, $0.00") },
  { onHome: false, lastAction: otherTap("id:add-txn") },
  { onHome: false, lastAction: { kind: "InputText", on: "id:txn-amount" }, amountText: "12.34" },
];

test("one submit committing one transaction is quiet across an eight-step window", () => {
  const verdicts = replay([
    ...walkToSubmit,
    { onHome: false, lastAction: submitTap(), amountText: "12.34" },
    { onHome: false, lastAction: otherTap("id:back") },
    { onHome: false, lastAction: otherTap("id:back") },
    home(4),
  ]);
  assert.deepEqual(verdicts, [false, false, false, false, false, false, false, false]);
});

// The planted defect: the same walk, one double tap, two rows.
test("a double tap committing two transactions fires at the next home reading", () => {
  const verdicts = replay([
    ...walkToSubmit,
    { onHome: false, lastAction: submitDoubleTap(), amountText: "12.34" },
    { onHome: false, lastAction: otherTap("id:back") },
    { onHome: false, lastAction: otherTap("id:back") },
    home(5),
  ]);
  assert.deepEqual(verdicts, [false, false, false, false, false, false, false, true]);
});

// A window can hold several submits and several visits to the form. Every
// transaction is accounted for by an action, so the bound holds.
test("many submits across a wide window stay under the bound", () => {
  const verdicts = replay([
    home(3),
    { onHome: false, lastAction: otherTap("desc:Checking, $0.00") },
    { onHome: false, lastAction: otherTap("id:add-txn") },
    { onHome: false, lastAction: submitTap(), amountText: "1" },
    { onHome: false, lastAction: otherTap("id:add-txn") },
    { onHome: false, lastAction: submitTap(), amountText: "250" },
    { onHome: false, lastAction: otherTap("id:add-txn") },
    { onHome: false, lastAction: submitTap(), amountText: "99999" },
    { onHome: false, lastAction: otherTap("id:back") },
    home(6),
  ]);
  assert.equal(verdicts.some((violated) => violated), false);
});

// The same wide window with one of the three taps doubled. The rise of four
// against a budget of three is what convicts.
test("a double tap hidden among healthy submits still fires", () => {
  const verdicts = replay([
    home(3),
    { onHome: false, lastAction: otherTap("desc:Checking, $0.00") },
    { onHome: false, lastAction: otherTap("id:add-txn") },
    { onHome: false, lastAction: submitTap(), amountText: "1" },
    { onHome: false, lastAction: otherTap("id:add-txn") },
    { onHome: false, lastAction: submitDoubleTap(), amountText: "250" },
    { onHome: false, lastAction: otherTap("id:add-txn") },
    { onHome: false, lastAction: submitTap(), amountText: "99999" },
    { onHome: false, lastAction: otherTap("id:back") },
    home(7),
  ]);
  assert.deepEqual(verdicts[9], true);
});

// Submits the app refused raise no bound, which is what keeps the window tight
// enough to convict. The same trace with the three empty-field taps counted
// would acquit a double submit.
test("taps on the disabled submit button do not pad the budget", () => {
  const padded: readonly Frame[] = [
    home(3),
    { onHome: false, lastAction: otherTap("desc:Checking, $0.00") },
    { onHome: false, lastAction: otherTap("id:add-txn") },
    { onHome: false, lastAction: submitTap(), amountText: "" },
    { onHome: false, lastAction: submitTap(), amountText: "" },
    { onHome: false, lastAction: submitTap(), amountText: "" },
    { onHome: false, lastAction: { kind: "InputText", on: "id:txn-amount" }, amountText: "1" },
    { onHome: false, lastAction: submitDoubleTap(), amountText: "1" },
    { onHome: false, lastAction: otherTap("id:back") },
    home(5),
  ];
  assert.deepEqual(replay(padded)[9], true);
});

// A fresh Home reading closes one window and opens the next, so a submit
// already accounted for cannot be spent twice.
test("the submit budget resets on every home reading", () => {
  const verdicts = replay([
    home(3),
    { onHome: false, lastAction: otherTap("id:add-txn") },
    { onHome: false, lastAction: submitTap(), amountText: "1" },
    home(4),
    { onHome: false, lastAction: otherTap("id:add-txn") },
    { onHome: false, lastAction: submitDoubleTap(), amountText: "1" },
    home(6),
  ]);
  assert.deepEqual(verdicts, [false, false, false, false, false, false, true]);
});

// A submit that lands back on Home belongs to the window that ends there, so
// the rise it caused is covered rather than convicted.
test("a submit whose landing frame is home is counted in that window", () => {
  const verdicts = replay([
    home(3),
    { onHome: false, lastAction: otherTap("id:add-txn") },
    { onHome: true, cards: [{ accountId: CHECKING, count: 4 }], lastAction: submitTap() },
  ]);
  assert.deepEqual(verdicts, [false, false, false]);
});

// Home renders its own node before its card list, and an empty list is
// UNKNOWN, not "no accounts". Writing it into the carrier is what would leave
// the next real reading comparing against nothing.
test("a home frame with no cards yet neither convicts nor closes the window", () => {
  const verdicts = replay([
    home(3),
    { onHome: false, lastAction: otherTap("id:add-txn") },
    { onHome: false, lastAction: submitDoubleTap(), amountText: "1" },
    { onHome: true, cards: [] },
    home(5),
  ]);
  assert.deepEqual(verdicts, [false, false, false, false, true]);
});
