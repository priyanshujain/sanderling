import assert from "node:assert/strict";
import { test } from "node:test";

import { readHomeTotalBalance } from "../../../examples/folio/sanderling/predicates.ts";

test("on Home the app's own total is the reading, the carrier and fresh", () => {
  assert.deepEqual(
    readHomeTotalBalance({ onHome: true, totalText: "$30.00", previousCarrier: 0 }),
    { value: 3000, carrier: 3000, fresh: true },
  );
});

test("off Home there is nothing to read, so the carrier is reported unchanged", () => {
  assert.deepEqual(
    readHomeTotalBalance({ onHome: false, totalText: undefined, previousCarrier: 3000 }),
    { value: 3000, carrier: 3000, fresh: false },
  );
});

test("off Home before any Home visit reports the null carrier, still not fresh", () => {
  assert.deepEqual(
    readHomeTotalBalance({ onHome: false, totalText: undefined, previousCarrier: null }),
    { value: null, carrier: null, fresh: false },
  );
});

test("a negative total parses with its sign", () => {
  assert.deepEqual(
    readHomeTotalBalance({ onHome: true, totalText: "-$1,234.56", previousCarrier: 0 }),
    { value: -123456, carrier: -123456, fresh: true },
  );
});

test("a fresh Home total overrides whatever the carrier held", () => {
  assert.deepEqual(
    readHomeTotalBalance({ onHome: true, totalText: "$7.50", previousCarrier: 9999 }),
    { value: 750, carrier: 750, fresh: true },
  );
});

// The poisoned carrier. An unreadable Home total is UNKNOWN for that step, so
// null is reported and the property goes vacuous, but the carrier must keep the
// last total we actually read. Writing null into the carrier is what used to end
// the run: off-Home steps hand the carrier straight back, so a single
// unreadable Home left every later step null.
test("an unreadable Home total reports null but leaves the carrier intact", () => {
  assert.deepEqual(
    readHomeTotalBalance({ onHome: true, totalText: undefined, previousCarrier: 3000 }),
    { value: null, carrier: 3000, fresh: false },
  );
});

test("a garbled Home total is unknown, not zero", () => {
  assert.deepEqual(
    readHomeTotalBalance({ onHome: true, totalText: "$", previousCarrier: 3000 }),
    { value: null, carrier: 3000, fresh: false },
  );
});

test("an unreadable Home no longer poisons the steps after it", () => {
  let carrier: number | null = null;
  const seen: (number | null)[] = [];
  const step = (onHome: boolean, totalText: string | undefined) => {
    const reading = readHomeTotalBalance({ onHome, totalText, previousCarrier: carrier });
    carrier = reading.carrier;
    seen.push(reading.value);
  };

  step(true, "$30.00");
  step(true, undefined);
  step(false, undefined);
  step(false, undefined);
  step(true, "$50.00");

  assert.deepEqual(seen, [3000, null, 3000, 3000, 5000]);
});

// The clipped fifth account card that started this: it is not a card reading
// any more, and the footer total the app renders is unaffected by which cards
// the viewport happens to fit.
test("Home total is one node, so an off-screen account cannot change it", () => {
  const withFiveCards = readHomeTotalBalance({
    onHome: true,
    totalText: "$2,589.00",
    previousCarrier: 0,
  });
  assert.deepEqual(withFiveCards, { value: 258900, carrier: 258900, fresh: true });
});
