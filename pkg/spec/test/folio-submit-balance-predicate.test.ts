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
      lastAction: { kind: "Tap", on: submitOn },
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
      lastAction: { kind: "Tap", on: submitOn },
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
      lastAction: { kind: "DoubleTap", on: submitOn },
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
      lastAction: { kind: "InputText", on: submitOn },
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
      lastAction: { kind: "Tap", on: "testTag:LoginScreen > testTag:LoginSubmit" },
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
      lastAction: { kind: "Tap", on: submitOn },
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
      lastAction: { kind: "Tap", on: { testTag: "TxnSubmit" } },
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
      lastAction: { kind: "Tap", on: { testTag: "LoginSubmit" } },
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
      lastAction: { kind: "Tap", on: submitOn },
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
      lastAction: { kind: "Tap", on: submitOn },
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
      lastAction: { kind: "Tap", on: submitOn },
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
      lastAction: { kind: "DoubleTap", on: submitOn },
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
      lastAction: { kind: "Tap", on: submitOn },
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
      lastAction: { kind: "DoubleTap", on: submitOn },
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
      lastAction: { kind: "Tap", on: submitOn },
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
      lastAction: { kind: "Tap", on: submitOn },
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
      lastAction: { kind: "Tap", on: submitOn },
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
      lastAction: { kind: "Tap", on: submitOn },
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
      lastAction: { kind: "Tap", on: submitOn },
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
      lastAction: { kind: "Tap", on: submitOn },
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
      lastAction: { kind: "Tap", on: submitOn },
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
      lastAction: { kind: "Tap", on: submitOn },
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
      lastAction: { kind: "Tap", on: submitOn },
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
      lastAction: { kind: "Tap", on: submitOn },
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
      lastAction: { kind: "Tap", on: submitOn },
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
      lastAction: { kind: "Tap", on: submitOn },
      typedAmount: parseTypedAmount("999999999999999999999"),
      prevTotalBalance: 220900,
      currTotalBalance: 220900,
    }),
    true,
  );
});
