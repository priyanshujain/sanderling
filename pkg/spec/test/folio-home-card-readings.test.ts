import assert from "node:assert/strict";
import { test } from "node:test";

import {
  committedTransactionsExceedSubmits,
  countSubmitsInWindow,
  homeAccountsOf,
  homeTxnCountsOf,
  readHomeCards,
} from "../../../examples/folio/sanderling/predicates.ts";
import type {
  CardReading,
  HomeCardReading,
  TxnCount,
} from "../../../examples/folio/sanderling/predicates.ts";

const card = (name: string, balance: number | null, count: TxnCount | undefined) => ({
  name,
  balance,
  count,
});

test("a laid-out card list reads as an account list and a count map", () => {
  const cards = [card("Checking", 0, "0"), card("Travel", 2411200, "1")];
  assert.deepEqual(homeAccountsOf(cards), [
    { name: "Checking", balance: 0 },
    { name: "Travel", balance: 2411200 },
  ]);
  assert.deepEqual(homeTxnCountsOf(cards), { Checking: "0", Travel: "1" });
});

// Android draws Home's own node a frame or two before its list, so `findAll`
// over the cards comes back empty while the screen already claims to be Home.
// "No cards on screen" is not "no accounts".
test("Home with nothing laid out yet is unknown, not empty", () => {
  assert.equal(homeAccountsOf([]), null);
  assert.equal(homeTxnCountsOf([]), null);
});

test("a card with no readable name or count is left out of the map", () => {
  assert.deepEqual(
    homeTxnCountsOf([card("", 0, "3"), card("Checking", 0, undefined), card("Savings", 0, "2")]),
    { Savings: "2" },
  );
});

test("every card unreadable leaves nothing to compare, which is unknown", () => {
  assert.equal(homeTxnCountsOf([card("", 0, "3"), card("Checking", 0, undefined)]), null);
});

test("off Home the carrier is reported unchanged", () => {
  const carried = { Checking: "3" };
  assert.deepEqual(readHomeCards({ route: "ledger", reading: null, previousCarrier: carried }), {
    value: carried,
    carrier: carried,
    fresh: false,
  });
});

test("a readable list replaces the carrier and closes the window", () => {
  assert.deepEqual(
    readHomeCards({ route: "home", reading: { Checking: "5" }, previousCarrier: { Checking: "3" } }),
    { value: { Checking: "5" }, carrier: { Checking: "5" }, fresh: true },
  );
});

// The poisoned carrier, the same defect readHomeTotalBalance was fixed for and
// this reading was not. An empty reading written into the carrier is handed
// straight back on every later off-Home step, so one un-laid-out Home turns the
// comparison into {} against {} for the rest of the run. Measured on android:
// counts_prev was {} at EVERY evaluation point of all 17 runs, which is a
// counting invariant that cannot fire at all.
test("an un-laid-out Home reports unknown but leaves the carrier intact", () => {
  const carried = { Checking: "3", Savings: "1" };
  assert.deepEqual(readHomeCards({ route: "home", reading: null, previousCarrier: carried }), {
    value: null,
    carrier: carried,
    fresh: false,
  });
});

// The trace the fix has to survive, stepped through the carrier and the window
// the spec holds. A double-submit commits two rows against one action, the
// Home it lands on has not drawn its list yet, and the counting invariant must
// still be able to see the pair once a real Home comes back.
function run(steps: { route: string | null; cards: CardReading[]; lastAction: unknown }[]) {
  let carrier: Record<string, TxnCount> | null = null;
  let submits = 0;
  const out: { counts: Record<string, TxnCount> | null; submits: number }[] = [];
  for (const step of steps) {
    const reading: HomeCardReading<Record<string, TxnCount>> = readHomeCards({
      route: step.route,
      reading: homeTxnCountsOf(step.cards),
      previousCarrier: carrier,
    });
    carrier = reading.carrier;
    const window = countSubmitsInWindow({
      previousCount: submits,
      lastAction: step.lastAction as { kind?: string; on?: string } | null,
      fresh: reading.fresh,
    });
    submits = window.next;
    out.push({ counts: reading.value, submits: window.reported });
  }
  return out;
}

const idle = { kind: "Tap", on: "testTag:AccountCard" };
const doubleSubmit = { kind: "DoubleTap", on: "testTag:AddTransactionScreen > testTag:TxnSubmit" };
const submit = { kind: "Tap", on: "testTag:AddTransactionScreen > testTag:TxnSubmit" };

test("an un-laid-out Home no longer kills the counting invariant", () => {
  const trace = run([
    { route: "home", cards: [card("Checking", 0, "3")], lastAction: null },
    { route: "ledger", cards: [], lastAction: idle },
    { route: "home", cards: [], lastAction: doubleSubmit },
    { route: "ledger", cards: [], lastAction: idle },
    { route: "home", cards: [card("Checking", 0, "5")], lastAction: idle },
  ]);

  // The un-laid-out Home is unknown for its own step, and the two steps after
  // it get the last list anyone actually read rather than an empty one.
  assert.deepEqual(trace[2]?.counts, null);
  assert.deepEqual(trace[3]?.counts, { Checking: "3" });
  assert.deepEqual(trace[4]?.counts, { Checking: "5" });

  // The window it did not close still holds the double-submit, so one action
  // against two committed rows is visible at the step that can compare them.
  assert.equal(trace[4]?.submits, 1);
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: trace[3]?.counts ?? null,
      countsAfter: trace[4]?.counts ?? null,
      submitsInWindow: trace[4]?.submits ?? 0,
    }),
    true,
  );
});

// The other half of the pairing: the counts window has to close on the counts
// reading, not on the total's. A Home frame can render its footer total while
// its list is still empty, and a window that reset there would compare a pair of
// readings spanning submits it had already forgotten.
test("an un-laid-out Home does not close the counting window", () => {
  const trace = run([
    { route: "home", cards: [card("Checking", 0, "3")], lastAction: null },
    { route: "ledger", cards: [], lastAction: doubleSubmit },
    { route: "home", cards: [], lastAction: doubleSubmit },
    { route: "home", cards: [card("Checking", 0, "7")], lastAction: idle },
  ]);
  assert.equal(trace[3]?.submits, 2);
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { Checking: "3" },
      countsAfter: trace[3]?.counts ?? null,
      submitsInWindow: trace[3]?.submits ?? 0,
    }),
    true,
  );
});

// What keeps a healthy submit from ever arriving as a rise nobody paid for. The
// reading banked here also resets the submit window, so a Home card list drawn
// before the store caught up with a commit would bank stale counts, start the
// next window empty, and leave the rise turning up with no budget to cover it.
// The app cannot put that frame in front of the spec: submit() pops one entry,
// so a commit lands back on the ledger it came from and the first Home reading
// is a whole action later, with the submit still in the window when the rise
// does show up.
test("a submit landing on the ledger is still in the window when Home reads it", () => {
  const trace = run([
    { route: "home", cards: [card("Checking", 0, "3")], lastAction: idle },
    { route: "ledger", cards: [], lastAction: submit },
    { route: "home", cards: [card("Checking", 5000, "4")], lastAction: idle },
  ]);
  assert.equal(trace[2]?.submits, 1);
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: trace[1]?.counts ?? null,
      countsAfter: trace[2]?.counts ?? null,
      submitsInWindow: trace[2]?.submits ?? 0,
    }),
    false,
  );
});

test("and a double submit down that same path still convicts", () => {
  const trace = run([
    { route: "home", cards: [card("Checking", 0, "3")], lastAction: idle },
    { route: "ledger", cards: [], lastAction: doubleSubmit },
    { route: "home", cards: [card("Checking", 10000, "5")], lastAction: idle },
  ]);
  assert.equal(trace[2]?.submits, 1);
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: trace[1]?.counts ?? null,
      countsAfter: trace[2]?.counts ?? null,
      submitsInWindow: trace[2]?.submits ?? 0,
    }),
    true,
  );
});
