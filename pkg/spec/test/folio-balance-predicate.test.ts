import assert from "node:assert/strict";
import { test } from "node:test";

import {
  balanceMatchesAddedSum,
  type LedgerRow,
} from "../../../examples/folio/sanderling/predicates.ts";

function row(key: string, signed: number): LedgerRow {
  return { key, signed };
}

test("single new row whose signed amount matches the balance delta holds", () => {
  const previous: LedgerRow[] = [row("a", 100)];
  const current: LedgerRow[] = [row("a", 100), row("b", 500)];
  assert.equal(balanceMatchesAddedSum(previous, current, 100, 600), true);
});

test("two new rows whose sum matches the balance delta holds", () => {
  const previous: LedgerRow[] = [];
  const current: LedgerRow[] = [row("a", 500), row("b", 300)];
  assert.equal(balanceMatchesAddedSum(previous, current, 0, 800), true);
});

test("double-submit case: two identical rows with sum exceeding the delta violates", () => {
  const previous: LedgerRow[] = [];
  const current: LedgerRow[] = [row("a", 500), row("b", 500)];
  assert.equal(balanceMatchesAddedSum(previous, current, 0, 500), false);
});

test("partial-credit case: two new rows whose sum falls short of the delta violates", () => {
  const previous: LedgerRow[] = [];
  const current: LedgerRow[] = [row("a", 200), row("b", 100)];
  assert.equal(balanceMatchesAddedSum(previous, current, 0, 500), false);
});

test("no new rows holds vacuously regardless of balance delta", () => {
  const previous: LedgerRow[] = [row("a", 100)];
  const current: LedgerRow[] = [row("a", 100)];
  assert.equal(balanceMatchesAddedSum(previous, current, 100, 100), true);
  assert.equal(balanceMatchesAddedSum(previous, current, 100, 999), true);
});
