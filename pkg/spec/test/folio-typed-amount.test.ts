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

test("leading plus sign tolerated as positive", () => {
  assert.equal(parseTypedAmount("+50"), 5000);
});

test("leading minus sign tolerated as positive", () => {
  assert.equal(parseTypedAmount("-50"), 5000);
});

test("comma-separated thousands accepted", () => {
  assert.equal(parseTypedAmount("1,234.56"), 123456);
});
