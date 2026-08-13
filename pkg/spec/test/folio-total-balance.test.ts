import assert from "node:assert/strict";
import { test } from "node:test";

import { computeHomeTotalBalance } from "../../../examples/folio/sanderling/predicates.ts";

test("on Home with two cards ($10, $20): returns $30", () => {
  assert.equal(
    computeHomeTotalBalance({
      cardBalanceTexts: ["$10.00", "$20.00"],
      previousCarrier: 0,
    }),
    3000,
  );
});

test("off Home (no cards) after a Home visit of $30: returns carrier $30", () => {
  assert.equal(
    computeHomeTotalBalance({
      cardBalanceTexts: [],
      previousCarrier: 3000,
    }),
    3000,
  );
});

test("off Home (no cards) with carrier still 0: returns 0", () => {
  assert.equal(
    computeHomeTotalBalance({
      cardBalanceTexts: [],
      previousCarrier: 0,
    }),
    0,
  );
});

test("sequence: Home $30, off-Home, Home $50 tracks new Home totals", () => {
  let carrier: number | null = 0;
  carrier = computeHomeTotalBalance({
    cardBalanceTexts: ["$10.00", "$20.00"],
    previousCarrier: carrier,
  });
  assert.equal(carrier, 3000);
  carrier = computeHomeTotalBalance({
    cardBalanceTexts: [],
    previousCarrier: carrier,
  });
  assert.equal(carrier, 3000);
  carrier = computeHomeTotalBalance({
    cardBalanceTexts: ["$20.00", "$30.00"],
    previousCarrier: carrier,
  });
  assert.equal(carrier, 5000);
});

test("Ledger step (no Home cards) holds the carrier, ignores Ledger balance", () => {
  let carrier: number | null = 0;
  carrier = computeHomeTotalBalance({
    cardBalanceTexts: ["$10.00", "$20.00"],
    previousCarrier: carrier,
  });
  assert.equal(carrier, 3000);
  carrier = computeHomeTotalBalance({
    cardBalanceTexts: [],
    previousCarrier: carrier,
  });
  assert.equal(carrier, 3000);
});

test("negative card balance parses with sign and sums correctly", () => {
  assert.equal(
    computeHomeTotalBalance({
      cardBalanceTexts: ["-$5.00", "$10.00"],
      previousCarrier: 0,
    }),
    500,
  );
});

test("single card on Home overrides any previous carrier", () => {
  assert.equal(
    computeHomeTotalBalance({
      cardBalanceTexts: ["$7.50"],
      previousCarrier: 9999,
    }),
    750,
  );
});

test("undefined card balance text makes the total unknown, not a partial sum", () => {
  assert.equal(
    computeHomeTotalBalance({
      cardBalanceTexts: [undefined, "$10.00"],
      previousCarrier: 0,
    }),
    null,
  );
});
