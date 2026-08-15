import assert from "node:assert/strict";
import { test } from "node:test";

import {
  committedAmountExceedsOneSubmit,
  committedTransactionsExceedSubmits,
  countSubmitsInWindow,
  homeTxnCountsOf,
  parseAccountBalance,
  parseTypedAmount,
  readAccountBalance,
  readHomeCards,
} from "../../../examples/folio/sanderling/predicates.ts";
import type {
  ObservedAction,
  TxnCount,
} from "../../../examples/folio/sanderling/predicates.ts";

// The per-account balance the ledger and the add-transaction screen both show.
// Its window closes on every frame of the transaction flow, where the Home
// total's closes only when the walk goes back to Home: the iOS run in #78 went
// 117 steps between two Home readings and accumulated 37 submits against a rise
// of 15, so the double tap at step 32 sat in a window far too wide to judge.

const submit: ObservedAction = {
  kind: "Tap",
  on: "testTag:AddTransactionScreen > testTag:TxnSubmit",
  applied: true,
};
const doubleSubmit: ObservedAction = { ...submit, kind: "DoubleTap" };
const openLedger: ObservedAction = {
  kind: "Tap",
  on: "testTag:HomeScreen > testTag:AccountCard",
  applied: true,
};
const openAddTxn: ObservedAction = {
  kind: "Tap",
  on: "testTag:LedgerScreen > testTag:AddTransactionButton",
  applied: true,
};
const typeAmount: ObservedAction = {
  kind: "InputText",
  on: "testTag:AddTransactionScreen > testTag:TxnAmountField",
  applied: true,
};
const goBack: ObservedAction = { kind: "Tap", on: "testTag:BackButton", applied: true };

test("the ledger writes the balance bare and the add-transaction header labels it", () => {
  assert.equal(parseAccountBalance("$196.00"), 19600);
  assert.equal(parseAccountBalance("Balance: $196.00"), 19600);
  assert.equal(parseAccountBalance("-$1,234.56"), -123456);
  assert.equal(parseAccountBalance("Balance: -$1,234.56"), -123456);
  assert.equal(parseAccountBalance("$0.00"), 0);
});

test("a balance that is not a complete amount is unknown, not zero", () => {
  assert.equal(parseAccountBalance(undefined), null);
  assert.equal(parseAccountBalance(""), null);
  assert.equal(parseAccountBalance("Balance:"), null);
  assert.equal(parseAccountBalance("$1,23.00"), null);
});

// Which account these numbers belong to is never asked, because inside a run of
// these two routes it cannot change: Route.Ledger is pushed only by tapping a
// card on Home, Route.AddTransaction only by the ledger's own button for its
// own account, and an accepted submit pops back to that same ledger. Reaching
// another account means passing through Home, so every frame that is not one of
// the two routes drops the carrier.
test("a frame off the account's own screens drops the carrier", () => {
  for (const route of ["home", "login", "add-account", null]) {
    assert.deepEqual(
      readAccountBalance({ route, balanceText: "$196.00", previousCarrier: 10000 }),
      { value: null, carrier: null, fresh: false },
      `route ${route} kept a carrier that may belong to another account`,
    );
  }
});

test("a readable balance on either of the two screens closes the window", () => {
  assert.deepEqual(
    readAccountBalance({ route: "ledger", balanceText: "$196.00", previousCarrier: 10000 }),
    { value: 19600, carrier: 19600, fresh: true },
  );
  assert.deepEqual(
    readAccountBalance({
      route: "add-transaction",
      balanceText: "Balance: $196.00",
      previousCarrier: 10000,
    }),
    { value: 19600, carrier: 19600, fresh: true },
  );
});

// The balance node scrolled out of the viewport is unknown, not a new value.
// The account still cannot have changed, so the carrier survives and the window
// stays open across the frame.
test("an unreadable balance keeps the carrier and does not close the window", () => {
  assert.deepEqual(
    readAccountBalance({ route: "ledger", balanceText: undefined, previousCarrier: 10000 }),
    { value: 10000, carrier: 10000, fresh: false },
  );
});

test("a double submit moves the account balance by twice what was typed", () => {
  assert.equal(
    committedAmountExceedsOneSubmit({
      route: "ledger",
      lastAction: doubleSubmit,
      submitsInWindow: 1,
      typedAmount: 19600,
      prevAccountBalance: 10000,
      currAccountBalance: 49200,
    }),
    true,
  );
});

test("a double-submitted debit is caught by the same bound", () => {
  assert.equal(
    committedAmountExceedsOneSubmit({
      route: "ledger",
      lastAction: doubleSubmit,
      submitsInWindow: 1,
      typedAmount: 19600,
      prevAccountBalance: 10000,
      currAccountBalance: -29200,
    }),
    true,
  );
});

test("one submit moving the balance by exactly the typed amount is the app working", () => {
  for (const after of [29600, -9600]) {
    assert.equal(
      committedAmountExceedsOneSubmit({
        route: "ledger",
        lastAction: submit,
        submitsInWindow: 1,
        typedAmount: 19600,
        prevAccountBalance: 10000,
        currAccountBalance: after,
      }),
      false,
    );
  }
});

// A balance that has not moved is a commit still in flight (createTransaction
// runs in a coroutine), a submit the app rejected, or a tap that never landed.
// None of those is evidence, and an equality would convict all three.
test("a balance that has not moved yet is not evidence", () => {
  assert.equal(
    committedAmountExceedsOneSubmit({
      route: "ledger",
      lastAction: submit,
      submitsInWindow: 1,
      typedAmount: 19600,
      prevAccountBalance: 10000,
      currAccountBalance: 10000,
    }),
    false,
  );
});

test("a window holding anything other than one submit is not attributable", () => {
  for (const submitsInWindow of [0, 2, 37]) {
    assert.equal(
      committedAmountExceedsOneSubmit({
        route: "ledger",
        lastAction: submit,
        submitsInWindow,
        typedAmount: 19600,
        prevAccountBalance: 10000,
        currAccountBalance: 49200,
      }),
      false,
    );
  }
});

test("an amount this reading cannot represent is vacuous, not a violation", () => {
  assert.equal(
    committedAmountExceedsOneSubmit({
      route: "ledger",
      lastAction: submit,
      submitsInWindow: 1,
      typedAmount: parseTypedAmount("not an amount"),
      prevAccountBalance: 10000,
      currAccountBalance: 49200,
    }),
    false,
  );
  assert.equal(
    committedAmountExceedsOneSubmit({
      route: "ledger",
      lastAction: submit,
      submitsInWindow: 1,
      typedAmount: Number.MAX_SAFE_INTEGER + 2,
      prevAccountBalance: 10000,
      currAccountBalance: 49200,
    }),
    false,
  );
});

test("a balance too large to hold exactly is not compared", () => {
  assert.equal(
    committedAmountExceedsOneSubmit({
      route: "ledger",
      lastAction: submit,
      submitsInWindow: 1,
      typedAmount: 19600,
      prevAccountBalance: Number.MAX_SAFE_INTEGER + 2,
      currAccountBalance: 0,
    }),
    false,
  );
  assert.equal(
    committedAmountExceedsOneSubmit({
      route: "ledger",
      lastAction: submit,
      submitsInWindow: 1,
      typedAmount: 19600,
      prevAccountBalance: 0,
      currAccountBalance: Number.MAX_SAFE_INTEGER + 2,
    }),
    false,
  );
});

test("an unknown balance on either side is not evidence", () => {
  assert.equal(
    committedAmountExceedsOneSubmit({
      route: "ledger",
      lastAction: submit,
      submitsInWindow: 1,
      typedAmount: 19600,
      prevAccountBalance: null,
      currAccountBalance: 49200,
    }),
    false,
  );
  assert.equal(
    committedAmountExceedsOneSubmit({
      route: "ledger",
      lastAction: submit,
      submitsInWindow: 1,
      typedAmount: 19600,
      prevAccountBalance: 10000,
      currAccountBalance: null,
    }),
    false,
  );
});

test("a step whose action was not a submit attributes nothing", () => {
  for (const lastAction of [openLedger, openAddTxn, typeAmount, goBack, null]) {
    assert.equal(
      committedAmountExceedsOneSubmit({
        route: "ledger",
        lastAction,
        submitsInWindow: 1,
        typedAmount: 19600,
        prevAccountBalance: 10000,
        currAccountBalance: 49200,
      }),
      false,
    );
  }
});

test("Home shows every account's money, so it is not this comparison's scale", () => {
  for (const route of ["home", "login", "add-account", null]) {
    assert.equal(
      committedAmountExceedsOneSubmit({
        route,
        lastAction: doubleSubmit,
        submitsInWindow: 1,
        typedAmount: 19600,
        prevAccountBalance: 10000,
        currAccountBalance: 49200,
      }),
      false,
    );
  }
});

// A step of the walk: the frame it landed on, the balance node that frame
// carried, what was in the amount field the step before, and the action that
// got there. Driven through the same carrier and window the spec holds.
interface Frame {
  route: string | null;
  balanceText?: string;
  typed?: string;
  lastAction: ObservedAction | null;
}

function walk(frames: readonly Frame[]) {
  let carrier: number | null = null;
  let submits = 0;
  const verdicts: { violated: boolean; balance: number | null; submits: number }[] = [];
  let previous: number | null = null;
  let typedBefore = "";
  for (const frame of frames) {
    const reading = readAccountBalance({
      route: frame.route,
      balanceText: frame.balanceText,
      previousCarrier: carrier,
    });
    carrier = reading.carrier;
    const window = countSubmitsInWindow({
      previousCount: submits,
      lastAction: frame.lastAction,
      fresh: reading.fresh,
    });
    submits = window.next;
    verdicts.push({
      violated: committedAmountExceedsOneSubmit({
        route: frame.route,
        lastAction: frame.lastAction,
        submitsInWindow: window.reported,
        typedAmount: parseTypedAmount(typedBefore),
        prevAccountBalance: previous,
        currAccountBalance: reading.value,
      }),
      balance: reading.value,
      submits: window.reported,
    });
    previous = reading.value;
    typedBefore = frame.typed ?? "";
  }
  return verdicts;
}

// The trajectory of #78: open an account, open the transaction form, type,
// double tap. Not one frame of it is Home, so the Home readings the counting
// invariant compares never advance and it has nothing to say about any of it.
// This is what a 117-step stretch of that run looked like, and it is why the
// double tap at step 32 went unconvicted.
test("the Home window cannot judge a walk that never goes Home", () => {
  const cards = [{ name: "Checking", balance: 10000, count: 3 as TxnCount }];
  let carrier: Record<string, TxnCount> | null = homeTxnCountsOf(cards);
  let submits = 0;
  for (const lastAction of [openLedger, openAddTxn, typeAmount, doubleSubmit]) {
    const reading = readHomeCards({ route: "ledger", reading: null, previousCarrier: carrier });
    const previous = carrier;
    carrier = reading.carrier;
    const window = countSubmitsInWindow({ previousCount: submits, lastAction, fresh: reading.fresh });
    submits = window.next;
    assert.equal(
      committedTransactionsExceedSubmits({
        countsBefore: previous,
        countsAfter: reading.value,
        submitsInWindow: window.reported,
      }),
      false,
    );
  }
});

// The same trajectory, judged where the app actually is. The frame the double
// tap lands on is the account's own ledger, so the window that closes there
// holds exactly the one action.
test("the double tap is convicted on the frame it lands on", () => {
  const verdicts = walk([
    { route: "ledger", balanceText: "$100.00", lastAction: openLedger },
    { route: "add-transaction", balanceText: "Balance: $100.00", lastAction: openAddTxn },
    { route: "add-transaction", balanceText: "Balance: $100.00", typed: "196", lastAction: typeAmount },
    { route: "ledger", balanceText: "$492.00", lastAction: doubleSubmit },
  ]);
  assert.deepEqual(
    verdicts.map(v => v.violated),
    [false, false, false, true],
  );
  assert.equal(verdicts[3]?.submits, 1);
});

test("the same walk with one transaction committed is silent throughout", () => {
  const verdicts = walk([
    { route: "ledger", balanceText: "$100.00", lastAction: openLedger },
    { route: "add-transaction", balanceText: "Balance: $100.00", lastAction: openAddTxn },
    { route: "add-transaction", balanceText: "Balance: $100.00", typed: "196", lastAction: typeAmount },
    { route: "ledger", balanceText: "$296.00", lastAction: submit },
    { route: "add-transaction", balanceText: "Balance: $296.00", lastAction: openAddTxn },
    { route: "add-transaction", balanceText: "Balance: $296.00", typed: "50", lastAction: typeAmount },
    { route: "ledger", balanceText: "$346.00", lastAction: submit },
  ]);
  assert.deepEqual(
    verdicts.map(v => v.violated),
    [false, false, false, false, false, false, false],
  );
});

// The reading a healthy app must survive: transactions arriving between two
// readings that the window can no longer attribute to one action. The balance
// node is off the viewport for a stretch, so the two numbers the property would
// compare straddle two commits, and the balance moves by 296.00 against a typed
// 50.00. Two submits in the window is not one, so there is nothing to judge.
test("transactions arriving between two readings do not convict a healthy app", () => {
  const verdicts = walk([
    { route: "ledger", balanceText: "$100.00", lastAction: openLedger },
    { route: "add-transaction", lastAction: openAddTxn },
    { route: "add-transaction", typed: "196", lastAction: typeAmount },
    { route: "ledger", lastAction: submit },
    { route: "add-transaction", lastAction: openAddTxn },
    { route: "add-transaction", typed: "100", lastAction: typeAmount },
    { route: "ledger", balanceText: "$396.00", lastAction: submit },
  ]);
  assert.deepEqual(
    verdicts.map(v => v.violated),
    [false, false, false, false, false, false, false],
  );
  assert.equal(verdicts[6]?.submits, 2);
  assert.equal(verdicts[6]?.balance, 39600);
});

// Attribution across accounts, which is the whole reason the carrier is dropped
// rather than carried. A $500.00 account is left behind for an empty one whose
// screens have not drawn their balance yet, and the submit into the new account
// lands with exactly one submit in the window: every gate this property has is
// open, and only the dropped carrier keeps it quiet. Carrying $500.00 across
// that frame reads as 30400 committed against 19600 typed, on an app that did
// nothing wrong.
test("a ledger opened for another account never inherits the old balance", () => {
  for (const between of ["home", null]) {
    const verdicts = walk([
      { route: "ledger", balanceText: "$500.00", lastAction: openAddTxn },
      { route: between, lastAction: goBack },
      { route: "ledger", lastAction: openLedger },
      { route: "add-transaction", typed: "196", lastAction: openAddTxn },
      { route: "ledger", balanceText: "$196.00", lastAction: submit },
    ]);
    assert.deepEqual(
      verdicts.map(v => v.violated),
      [false, false, false, false, false],
      `an account switch through ${between} was compared across accounts`,
    );
    assert.equal(verdicts[4]?.submits, 1);
    assert.equal(verdicts[4]?.balance, 19600);
  }
});
