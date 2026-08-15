import assert from "node:assert/strict";
import { test } from "node:test";

import {
  parseTypedAmount,
  submitChangesBalanceByTypedAmount,
} from "../../../examples/folio/sanderling/predicates.ts";

const submitOn = "testTag:LedgerScreen > testTag:TxnSubmit";

test("single submit: delta matches typed amount", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: 500,
      prevTotalBalance: 1000,
      currTotalBalance: 1500,
    }),
    true,
  );
});

test("double submit: delta is twice the typed amount, fires", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: 500,
      prevTotalBalance: 1000,
      currTotalBalance: 2000,
    }),
    false,
  );
});

test("DoubleTap kind also caught when delta exceeds typed amount", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "DoubleTap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: 500,
      prevTotalBalance: 0,
      currTotalBalance: 1000,
    }),
    false,
  );
});

test("wrong action kind: vacuous true even with mismatch", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "InputText", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: 500,
      prevTotalBalance: 1000,
      currTotalBalance: 1000,
    }),
    true,
  );
});

test("wrong target: vacuous true even with mismatch", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: "testTag:LoginScreen > testTag:LoginSubmit", applied: true },
      submitsInWindow: 1,
      typedAmount: 500,
      prevTotalBalance: 1000,
      currTotalBalance: 1000,
    }),
    true,
  );
});

test("null lastAction: vacuous true", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: null,
      submitsInWindow: 1,
      typedAmount: 500,
      prevTotalBalance: 1000,
      currTotalBalance: 2000,
    }),
    true,
  );
});

test("zero typedAmount: vacuous true", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: 0,
      prevTotalBalance: 1000,
      currTotalBalance: 1500,
    }),
    true,
  );
});

test("selector as object: coerced safely and TxnSubmit detected", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: { testTag: "TxnSubmit" }, applied: true },
      submitsInWindow: 1,
      typedAmount: 500,
      prevTotalBalance: 0,
      currTotalBalance: 1000,
    }),
    false,
  );
});

test("selector as object without TxnSubmit: vacuous true", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: { testTag: "LoginSubmit" }, applied: true },
      submitsInWindow: 1,
      typedAmount: 500,
      prevTotalBalance: 0,
      currTotalBalance: 1000,
    }),
    true,
  );
});

test("raw whole-dollar input: single submit clears", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: parseTypedAmount("50"),
      prevTotalBalance: 5000,
      currTotalBalance: 10000,
    }),
    true,
  );
});

test("raw whole-dollar input: double submit fires", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: parseTypedAmount("50"),
      prevTotalBalance: 5000,
      currTotalBalance: 15000,
    }),
    false,
  );
});

test("decimal input from empty prior balance clears", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: parseTypedAmount("5.50"),
      prevTotalBalance: 0,
      currTotalBalance: 550,
    }),
    true,
  );
});

test("DoubleTap kind with raw whole-dollar input fires", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "DoubleTap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: parseTypedAmount("100"),
      prevTotalBalance: 0,
      currTotalBalance: 20000,
    }),
    false,
  );
});

test("route gate: ledger landing with stale carrier is skipped", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "ledger",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: 5000,
      prevTotalBalance: 0,
      currTotalBalance: 0,
    }),
    true,
  );
});

test("route gate: add-transaction landing with double-submit delta is skipped", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "add-transaction",
      lastAction: { kind: "DoubleTap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: 5000,
      prevTotalBalance: 0,
      currTotalBalance: 10000,
    }),
    true,
  );
});

test("route gate: null route is skipped", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: null,
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: 5000,
      prevTotalBalance: 0,
      currTotalBalance: 0,
    }),
    true,
  );
});

test("route gate: home landing with matching delta passes", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: 5000,
      prevTotalBalance: 0,
      currTotalBalance: 5000,
    }),
    true,
  );
});

test("route gate: home landing with double-insert delta fires", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: 5000,
      prevTotalBalance: 0,
      currTotalBalance: 10000,
    }),
    false,
  );
});

// Precision. Cents are integers in float64 here, so the equality only means
// something while every number involved is exactly representable. The app takes
// any amount that fits a Kotlin Long, and an iOS run reached a balance around
// 1e18 cents, where representable values sit 128 apart: the delta of a
// perfectly healthy single submit no longer reads back as the typed amount.
const HUGE_BALANCE = 999999999999999900;

test("above 2^53 the arithmetic itself is wrong, which is why the guard exists", () => {
  assert.notEqual(Math.abs(HUGE_BALANCE + 1600 - HUGE_BALANCE), 1600);
});

test("above 2^53 a healthy single submit is not reported", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: 1600,
      prevTotalBalance: HUGE_BALANCE,
      currTotalBalance: HUGE_BALANCE + 1600,
    }),
    true,
  );
});

test("above 2^53 a double-submit delta is not reported either", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: 1600,
      prevTotalBalance: HUGE_BALANCE,
      currTotalBalance: HUGE_BALANCE + 3200,
    }),
    true,
  );
});

test("an unreadable previous balance above 2^53 is not evidence", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: 1600,
      prevTotalBalance: HUGE_BALANCE,
      currTotalBalance: 5000,
    }),
    true,
  );
});

// A typed amount past the safe range cannot be compared either. parseTypedAmount
// returns 0 for those now, but the predicate takes the number from its caller
// and must not convict on one it cannot hold.
test("typed amount above 2^53 is not evidence", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: 1e23,
      prevTotalBalance: 0,
      currTotalBalance: 0,
    }),
    true,
  );
});

// The boundary, from both sides. MAX_SAFE_INTEGER still gets judged; one cent
// more is where counting stops being exact.
test("boundary: a double submit landing exactly on MAX_SAFE_INTEGER still fires", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: 4503599627370495,
      prevTotalBalance: 0,
      currTotalBalance: 9007199254740990,
    }),
    false,
  );
});

test("boundary: a single submit landing exactly on MAX_SAFE_INTEGER passes", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: 9007199254740991,
      prevTotalBalance: 0,
      currTotalBalance: 9007199254740991,
    }),
    true,
  );
});

test("boundary: one cent past MAX_SAFE_INTEGER stops being evidence", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: 4503599627370496,
      prevTotalBalance: 0,
      currTotalBalance: 9007199254740992,
    }),
    true,
  );
});

// The guard covers the balances and the typed amount, not their difference: two
// safe balances subtract exactly whenever the result could have matched a safe
// typed amount, so a mismatch here is real and must still be reported.
test("a large but exact difference between safe balances still fires", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: 500,
      prevTotalBalance: -9007199254740991,
      currTotalBalance: 9007199254740991,
    }),
    false,
  );
});

// The 21-digit corpus amount end to end: the app refuses it, so nothing moves,
// and the property must stay quiet rather than demand a 1e23-cent move.
test("21-digit typed amount with an unmoved balance is not a violation", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: parseTypedAmount("999999999999999999999"),
      prevTotalBalance: 220900,
      currTotalBalance: 220900,
    }),
    true,
  );
});

// Freshness. prevTotalBalance is the last total we READ, so the window between
// it and now can hold more than one submit's transactions. A delta measured
// over such a window is not evidence about the amount typed into any one of
// them, and the android run that produced a 13000 delta against a typed 19600
// is what that looks like: the window held a double-submit's two 19600 debits
// and an unrelated 26200 credit.
test("freshness: two submits in the window is vacuous, not a conviction", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "DoubleTap", on: submitOn, applied: true },
      submitsInWindow: 2,
      typedAmount: 19600,
      prevTotalBalance: 0,
      currTotalBalance: -13000,
    }),
    true,
  );
});

test("freshness: two submits cannot convict even on a clean 2x delta", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 2,
      typedAmount: 500,
      prevTotalBalance: 1000,
      currTotalBalance: 2000,
    }),
    true,
  );
});

// The boundary of the rule, from both sides. One submit is the only window the
// property judges: zero means the total moved without a submit landing in it
// (nothing to attribute the move to), and two or more means the move is shared.
test("freshness boundary: exactly one submit is the window that convicts", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "DoubleTap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: 19600,
      prevTotalBalance: 0,
      currTotalBalance: -39200,
    }),
    false,
  );
});

test("freshness boundary: one submit with a healthy 1x delta still passes", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 1,
      typedAmount: 19600,
      prevTotalBalance: 0,
      currTotalBalance: -19600,
    }),
    true,
  );
});

test("freshness boundary: three submits is vacuous", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 3,
      typedAmount: 500,
      prevTotalBalance: 0,
      currTotalBalance: 2500,
    }),
    true,
  );
});

// A zero count would mean the step's own action was not counted as a submit,
// which contradicts the action gate above it. Guard it anyway: a window with no
// submit in it explains no balance move.
test("freshness boundary: a window with no submit in it is vacuous", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: true },
      submitsInWindow: 0,
      typedAmount: 500,
      prevTotalBalance: 1000,
      currTotalBalance: 2000,
    }),
    true,
  );
});

// applied: null is the runner saying it dispatched the tap and never learned
// whether it landed. A submit that committed nothing leaves the balance where
// it was, so demanding the typed amount of movement for it convicts an app that
// did exactly what it should have.
test("a submit the runner could not confirm demands no balance move", () => {
  assert.equal(
    submitChangesBalanceByTypedAmount({
      route: "home",
      lastAction: { kind: "Tap", on: submitOn, applied: null },
      submitsInWindow: 1,
      typedAmount: 500,
      prevTotalBalance: 1000,
      currTotalBalance: 1000,
    }),
    true,
  );
});
