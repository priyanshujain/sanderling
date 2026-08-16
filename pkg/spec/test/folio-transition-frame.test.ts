import assert from "node:assert/strict";
import { test } from "node:test";

import {
  countSubmitsInWindow,
  readHomeCards,
  readHomeTotalBalance,
  routeOfFrame,
  submitChangesBalanceByAtMostTypedAmount,
} from "../../../examples/folio/sanderling/predicates.ts";

// The spec's own screen table. A frame is the set of markers its accessibility
// tree carries, which is all routeOfFrame is allowed to look at.
const SCREENS = {
  login: "LoginScreen",
  "add-account": "AddAccountScreen",
  "add-transaction": "AddTransactionScreen",
  ledger: "LedgerScreen",
  home: "HomeScreen",
};

const frame =
  (...tags: string[]) =>
  (tag: string) =>
    tags.includes(tag);

test("a frame showing one screen names its route", () => {
  assert.equal(routeOfFrame(SCREENS, frame("LoginScreen")), "login");
  assert.equal(routeOfFrame(SCREENS, frame("AddAccountScreen")), "add-account");
  assert.equal(routeOfFrame(SCREENS, frame("AddTransactionScreen")), "add-transaction");
  assert.equal(routeOfFrame(SCREENS, frame("LedgerScreen")), "ledger");
  assert.equal(routeOfFrame(SCREENS, frame("HomeScreen")), "home");
});

test("a frame showing no screen at all is unknown", () => {
  assert.equal(routeOfFrame(SCREENS, frame()), null);
  assert.equal(routeOfFrame(SCREENS, frame("SomethingElse")), null);
});

// The android transition frame: 425 of 1879 steps across 17 measured runs carry
// two screens, in every combination the navigation graph allows. Ranking the
// markers and taking the first answers add-transaction for the first of these
// while a second, unscoped look answers "on Home" -- and two answers for one
// frame is the defect. There is one answer now, and on a transition frame it is
// "I do not know".
test("a transition frame showing two screens names neither", () => {
  assert.equal(routeOfFrame(SCREENS, frame("AddTransactionScreen", "HomeScreen")), null);
  assert.equal(routeOfFrame(SCREENS, frame("HomeScreen", "LedgerScreen")), null);
  assert.equal(routeOfFrame(SCREENS, frame("AddAccountScreen", "HomeScreen")), null);
  assert.equal(routeOfFrame(SCREENS, frame("HomeScreen", "LoginScreen")), null);
  assert.equal(routeOfFrame(SCREENS, frame("AddTransactionScreen", "LedgerScreen")), null);
});

test("the three-screen frames android also emits name nothing", () => {
  assert.equal(
    routeOfFrame(SCREENS, frame("AddTransactionScreen", "HomeScreen", "LedgerScreen")),
    null,
  );
});

// Everything read off Home takes the route as its only input, so a frame that
// is not Home cannot be read as Home by anything.
test("a transition frame's half-drawn Home total is not a reading", () => {
  const route = routeOfFrame(SCREENS, frame("AddTransactionScreen", "HomeScreen"));
  assert.deepEqual(
    readHomeTotalBalance({ route, totalText: "$86,911.00", previousCarrier: 8681600 }),
    { value: 8681600, carrier: 8681600, fresh: false },
  );
});

test("nor is its half-drawn card list", () => {
  const route = routeOfFrame(SCREENS, frame("AddAccountScreen", "HomeScreen"));
  const carried = { Travel: "8", Checking: "0" };
  const partial = { Travel: "8" };
  assert.deepEqual(readHomeCards<Record<string, string>>({ route, reading: partial, previousCarrier: carried }), {
    value: carried,
    carrier: carried,
    fresh: false,
  });
});

// The false conviction itself, android seed 3, steps 98-102 of the recorded
// trace. Five submits deep into the window the app sits on AddTransaction with
// "339" typed; a double-tap on Back starts the trip Home; the frame that comes
// back carries BOTH screens with Home's total already drawn behind the outgoing
// one. The old spec read that total as a fresh Home reading, reset the window to
// zero, and aimed the next tap at a TxnSubmit button that had stopped existing.
// The tap landed on Home, committed nothing, and the property demanded 33900 of
// movement for it. Nine of the eleven android convictions were this, all at
// delta 0.0x, all a single Tap where the real bug is a DoubleTap.
test("the measured android transition chain no longer convicts at delta 0", () => {
  let carrier: number | null = 8681600;
  let submits = 5;
  const step = (
    tags: string[],
    totalText: string | undefined,
    lastAction: { kind: string; on: string; applied: true } | null,
  ) => {
    const route = routeOfFrame(SCREENS, frame(...tags));
    const reading = readHomeTotalBalance({ route, totalText, previousCarrier: carrier });
    const window = countSubmitsInWindow({ previousCount: submits, lastAction, fresh: reading.fresh });
    carrier = reading.carrier;
    submits = window.next;
    return { route, total: reading.value, submits: window.reported };
  };

  const back = { kind: "DoubleTap", on: "id:BackButton", applied: true as const };
  const phantomSubmit = {
    kind: "Tap",
    on: "testTag:AddTransactionScreen > testTag:TxnSubmit",
    applied: true as const,
  };

  const transition = step(["AddTransactionScreen", "HomeScreen"], "$86,911.00", back);
  assert.equal(transition.route, null);
  assert.equal(transition.total, 8681600);
  assert.equal(transition.submits, 5);

  const landing = step(["HomeScreen"], "$86,911.00", phantomSubmit);
  assert.equal(landing.submits, 6);
  assert.equal(
    submitChangesBalanceByAtMostTypedAmount({
      route: landing.route,
      lastAction: phantomSubmit,
      submitsInWindow: landing.submits,
      typedAmount: 33900,
      prevTotalBalance: transition.total,
      currTotalBalance: landing.total,
    }),
    true,
  );

  // What the reset bought the old spec: the same landing, judged against a
  // window of one and a total the transition frame had already banked. It
  // convicted on a delta of zero, and that shape cannot convict any more even
  // with the window reset back to one, because the property is a bound rather
  // than an equality. A balance that did not move is under any typed amount,
  // whether nothing was submitted or the total has not caught up yet.
  assert.equal(
    submitChangesBalanceByAtMostTypedAmount({
      route: "home",
      lastAction: phantomSubmit,
      submitsInWindow: 1,
      typedAmount: 33900,
      prevTotalBalance: 8691100,
      currTotalBalance: 8691100,
    }),
    true,
  );

  // The double tap it was always meant to catch is untouched by that: two
  // 33900 debits against one action still exceed the amount typed for it.
  assert.equal(
    submitChangesBalanceByAtMostTypedAmount({
      route: "home",
      lastAction: { ...phantomSubmit, kind: "DoubleTap" },
      submitsInWindow: 1,
      typedAmount: 33900,
      prevTotalBalance: 8691100,
      currTotalBalance: 8691100 - 67800,
    }),
    false,
  );
});
