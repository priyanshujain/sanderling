import assert from "node:assert/strict";
import { test } from "node:test";

import {
  committedTransactionsExceedSubmits,
  countSubmitsInWindow,
  isTxnSubmitTap,
  readHomeTotalBalance,
} from "../../../examples/folio/sanderling/predicates.ts";

const submitOn = "testTag:AddTransactionScreen > testTag:TxnSubmit";

test("a tap on TxnSubmit is a commit", () => {
  assert.equal(isTxnSubmitTap({ kind: "Tap", on: submitOn }), true);
});

test("a double-tap on TxnSubmit is ONE commit action, not two", () => {
  const window = countSubmitsInWindow({
    previousCount: 0,
    lastAction: { kind: "DoubleTap", on: submitOn },
    fresh: true,
  });
  assert.equal(window.reported, 1);
});

test("a selector object naming TxnSubmit is a commit", () => {
  assert.equal(isTxnSubmitTap({ kind: "Tap", on: { testTag: "TxnSubmit" } }), true);
});

test("typing into the amount field is not a commit", () => {
  assert.equal(isTxnSubmitTap({ kind: "InputText", on: submitOn }), false);
});

test("tapping some other button is not a commit", () => {
  assert.equal(isTxnSubmitTap({ kind: "Tap", on: "testTag:AddAccountSubmit" }), false);
});

test("no action at all is not a commit", () => {
  assert.equal(isTxnSubmitTap(null), false);
});

test("a fresh Home reading closes the window and the next one starts empty", () => {
  assert.deepEqual(
    countSubmitsInWindow({ previousCount: 0, lastAction: { kind: "Tap", on: submitOn }, fresh: true }),
    { reported: 1, next: 0 },
  );
});

test("landing off Home keeps the submit in the window for the next step", () => {
  assert.deepEqual(
    countSubmitsInWindow({ previousCount: 0, lastAction: { kind: "Tap", on: submitOn }, fresh: false }),
    { reported: 1, next: 1 },
  );
});

test("a non-submit step neither adds to nor forgets the window", () => {
  assert.deepEqual(
    countSubmitsInWindow({ previousCount: 1, lastAction: { kind: "Tap", on: "testTag:AccountCard" }, fresh: false }),
    { reported: 1, next: 1 },
  );
});

test("a second submit with no Home reading between them counts two", () => {
  assert.deepEqual(
    countSubmitsInWindow({ previousCount: 1, lastAction: { kind: "DoubleTap", on: submitOn }, fresh: true }),
    { reported: 2, next: 0 },
  );
});

// The two traces the freshness rule exists to tell apart, driven step by step
// through the same pair of carriers the spec holds.
function run(steps: { route: string | null; totalText?: string; lastAction: unknown }[]) {
  let carrier: number | null = null;
  let submits = 0;
  const out: { total: number | null; submits: number }[] = [];
  for (const step of steps) {
    const reading = readHomeTotalBalance({
      route: step.route,
      totalText: step.totalText,
      previousCarrier: carrier,
    });
    carrier = reading.carrier;
    const window = countSubmitsInWindow({
      previousCount: submits,
      lastAction: step.lastAction as { kind?: string; on?: string } | null,
      fresh: reading.fresh,
    });
    submits = window.next;
    out.push({ total: reading.value, submits: window.reported });
  }
  return out;
}

const idle = { kind: "Tap", on: "testTag:AccountCard" };
const submit = { kind: "Tap", on: submitOn };
const doubleSubmit = { kind: "DoubleTap", on: submitOn };

// A double-submit pops the back stack twice (each Submit calls
// navigator.back), so unlike a healthy single submit it lands back on Home,
// which is why the property can see it at all.
test("clean double submit: one action in the window, delta is 2x", () => {
  const trace = run([
    { route: "home", totalText: "$0.00", lastAction: null },
    { route: "ledger", lastAction: idle },
    { route: "ledger", lastAction: idle },
    { route: "home", totalText: "$100.00", lastAction: doubleSubmit },
  ]);
  assert.equal(trace[3]?.submits, 1);
  assert.equal(trace[0]?.total, 0);
  assert.equal(trace[3]?.total, 10000);
});

// The contaminated window from the android run: an unrelated submit committed
// while we were off Home, then the double-submit landed. The delta spans three
// transactions, so it is not evidence about either typed amount.
test("two submits between Home visits: the window is not evidence", () => {
  const trace = run([
    { route: "home", totalText: "$0.00", lastAction: null },
    { route: "ledger", lastAction: idle },
    { route: "ledger", lastAction: submit },
    { route: "ledger", lastAction: idle },
    { route: "home", totalText: "-$130.00", lastAction: doubleSubmit },
  ]);
  assert.equal(trace[4]?.submits, 2);
});

// Freshness is restored by seeing Home, not by time passing.
test("a Home visit between two submits restores a one-action window", () => {
  const trace = run([
    { route: "home", totalText: "$0.00", lastAction: null },
    { route: "ledger", lastAction: submit },
    { route: "home", totalText: "$262.00", lastAction: idle },
    { route: "ledger", lastAction: idle },
    { route: "home", totalText: "$66.00", lastAction: doubleSubmit },
  ]);
  assert.equal(trace[1]?.submits, 1);
  assert.equal(trace[2]?.submits, 1);
  assert.equal(trace[4]?.submits, 1);
});

// An unreadable Home is not a Home reading: it must not close the window, or
// the count would go back to zero against a total nobody read.
test("an unreadable Home does not close the window", () => {
  const trace = run([
    { route: "home", totalText: "$0.00", lastAction: null },
    { route: "ledger", lastAction: submit },
    { route: "home", totalText: undefined, lastAction: idle },
    { route: "home", totalText: "$66.00", lastAction: doubleSubmit },
  ]);
  assert.equal(trace[2]?.total, null);
  assert.equal(trace[3]?.submits, 2);
});

// The window is an upper bound on the submits it holds, so a submit whose
// dispatch the runner could not confirm belongs in it: the tap may well have
// landed, and a bound that leaves it out is one the transaction it committed
// exceeds. That is the false conviction, a rise of one against a window of
// zero, on the property carrying most of the detection on android.
test("a submit the runner could not confirm still counts toward the window", () => {
  const window = countSubmitsInWindow({
    previousCount: 0,
    lastAction: { kind: "Tap", on: submitOn, applied: null },
    fresh: true,
  });
  assert.equal(window.reported, 1);
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { Travel: 3 },
      countsAfter: { Travel: 4 },
      submitsInWindow: window.reported,
    }),
    false,
  );
});
