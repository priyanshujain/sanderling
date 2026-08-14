import assert from "node:assert/strict";
import { test } from "node:test";

import { parseTypedAmount } from "../../../examples/folio/sanderling/predicates.ts";

test("empty string returns 0", () => {
  assert.equal(parseTypedAmount(""), 0);
});

test("undefined returns 0", () => {
  assert.equal(parseTypedAmount(undefined), 0);
});

test("null returns 0", () => {
  assert.equal(parseTypedAmount(null), 0);
});

test("whole number 50 parses as 5000 cents", () => {
  assert.equal(parseTypedAmount("50"), 5000);
});

test("decimal 5.50 parses as 550 cents", () => {
  assert.equal(parseTypedAmount("5.50"), 550);
});

test("decimal 5.05 parses as 505 cents", () => {
  assert.equal(parseTypedAmount("5.05"), 505);
});

test("one-decimal 0.5 parses as 50 cents", () => {
  assert.equal(parseTypedAmount("0.5"), 50);
});

test("non-numeric returns 0", () => {
  assert.equal(parseTypedAmount("abc"), 0);
});

test("too many decimals returns 0", () => {
  assert.equal(parseTypedAmount("5.123"), 0);
});

test("zero returns 0", () => {
  assert.equal(parseTypedAmount("0"), 0);
});

// The app's parseCents matches ^\d+(\.\d{1,2})?$ against the trimmed input, so
// a sign is rejected and no transaction is created. Reading "-50" as 5000 cents
// would make the balance property demand a move the app never made.
test("leading plus sign rejected, like the app", () => {
  assert.equal(parseTypedAmount("+50"), 0);
});

test("leading minus sign rejected, like the app", () => {
  assert.equal(parseTypedAmount("-50"), 0);
});

test("comma-separated thousands accepted", () => {
  assert.equal(parseTypedAmount("1,234.56"), 123456);
});

// The input corpus types this 21-digit run into every field. parseCents calls
// toLongOrNull on the whole part, which is null past Long.MAX, so the app
// refuses the submit; float64 would have read it as 1e23 and asked the property
// to find a balance move of 1e23 cents that never happened.
test("21-digit corpus amount returns 0: the app rejects it", () => {
  assert.equal(parseTypedAmount("999999999999999999999"), 0);
});

test("amount too large for exact cents returns 0", () => {
  assert.equal(parseTypedAmount("100000000000000"), 0);
});

// 9007199254740991 cents is Number.MAX_SAFE_INTEGER: the last amount whose
// cents survive the multiply intact.
test("largest exactly representable amount is kept", () => {
  assert.equal(parseTypedAmount("90071992547409.91"), 9007199254740991);
});

test("one cent past the safe range returns 0", () => {
  assert.equal(parseTypedAmount("90071992547409.92"), 0);
});
