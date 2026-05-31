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
      lastAction: { kind: "DoubleTap", on: submitOn },
      typedAmount: parseTypedAmount("100"),
      prevTotalBalance: 0,
      currTotalBalance: 20000,
    }),
    false,
  );
});
