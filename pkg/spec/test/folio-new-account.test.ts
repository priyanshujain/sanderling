import assert from "node:assert/strict";
import { test } from "node:test";

import { createdAccountHasNonZeroBalance } from "../../../examples/folio/sanderling/predicates.ts";

const created = {
  kind: "Tap",
  on: "testTag:AddAccountScreen > testTag:AddAccountSubmit",
  applied: true as const,
};
const idle = { kind: "Tap", on: "testTag:HomeScreen > testTag:AccountCard", applied: true as const };

const account = (name: string, balance: number | null) => ({ name, balance });

// The property still has teeth: the account the fuzzer just asked for, holding
// money on the step its creation landed on Home, is a real violation.
test("an account created holding money is a violation", () => {
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "home",
      lastAction: created,
      typedName: "Travel",
      before: [account("Checking", 0)],
      after: [account("Checking", 0), account("Travel", 5000)],
    }),
    true,
  );
});

test("a double-tapped create is judged the same way", () => {
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "home",
      lastAction: { kind: "DoubleTap", on: "id:AddAccountSubmit", applied: true },
      typedName: "Travel",
      before: [account("Checking", 0)],
      after: [account("Checking", 0), account("Travel", 5000)],
    }),
    true,
  );
});

test("an account created empty is what the app is supposed to do", () => {
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "home",
      lastAction: created,
      typedName: "Travel",
      before: [account("Checking", 0)],
      after: [account("Checking", 0), account("Travel", 0)],
    }),
    false,
  );
});

test("a balance that could not be read is not evidence", () => {
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "home",
      lastAction: created,
      typedName: "Travel",
      before: [account("Checking", 0)],
      after: [account("Checking", 0), account("Travel", null)],
    }),
    false,
  );
});

// The false positives this replaces. Home lists the accounts that fit the
// viewport, so an account arrives in a later reading for reasons that have
// nothing to do with being created: the list scrolled, a clipped card finished
// laying out, or (before the route fix) the earlier reading came off a
// half-rendered Home mid-transition. Measured on android: an existing Travel
// holding $24,112.00 and an existing Savings holding $429,585.00, convicted for
// coming into view.
test("an account scrolling into view is not an account being created", () => {
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "home",
      lastAction: idle,
      typedName: "Travel",
      before: [account("Emergency Fund", 461012300), account("Checking", 0)],
      after: [
        account("Emergency Fund", 461012300),
        account("Checking", 0),
        account("Travel", 2411200),
        account("Savings", 0),
      ],
    }),
    false,
  );
});

test("nor is one that appears with no action at all behind it", () => {
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "home",
      lastAction: null,
      typedName: "Travel",
      before: [account("Checking", 0)],
      after: [account("Checking", 0), account("Travel", 2411200)],
    }),
    false,
  );
});

// Even on the creation step, the only card judged is the one that answers to
// the name the fuzzer typed. A card that came into view alongside it is still
// just a card that came into view.
test("a funded account arriving beside the created one is not judged", () => {
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "home",
      lastAction: created,
      typedName: "Savings",
      before: [account("Checking", 0)],
      after: [account("Checking", 0), account("Savings", 0), account("Travel", 2411200)],
    }),
    false,
  );
});

test("off Home there is no reading to judge", () => {
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "ledger",
      lastAction: created,
      typedName: "Travel",
      before: [account("Checking", 0)],
      after: [account("Checking", 0), account("Travel", 5000)],
    }),
    false,
  );
});

test("a transition frame names no route, so nothing is judged there either", () => {
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: null,
      lastAction: created,
      typedName: "Travel",
      before: [account("Checking", 0)],
      after: [account("Checking", 0), account("Travel", 5000)],
    }),
    false,
  );
});

test("an unknown reading on either side is not evidence", () => {
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "home",
      lastAction: created,
      typedName: "Travel",
      before: null,
      after: [account("Travel", 5000)],
    }),
    false,
  );
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "home",
      lastAction: created,
      typedName: "Travel",
      before: [account("Checking", 0)],
      after: null,
    }),
    false,
  );
});

// defaultActions types edge-case text into the name field, and an empty name is
// not a name we can find a card by.
test("an empty typed name attributes nothing", () => {
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "home",
      lastAction: created,
      typedName: "   ",
      before: [account("Checking", 0)],
      after: [account("Checking", 0), account("Travel", 5000)],
    }),
    false,
  );
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "home",
      lastAction: created,
      typedName: undefined,
      before: [account("Checking", 0)],
      after: [account("Checking", 0), account("Travel", 5000)],
    }),
    false,
  );
});

// Web merges the card into one node whose text opens with the avatar initials,
// so the identity key carries them: "TRTravel" is the card for "Travel".
test("the merged web key still matches the name that was typed", () => {
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "home",
      lastAction: created,
      typedName: "Travel",
      before: [account("CHChecking", 0)],
      after: [account("CHChecking", 0), account("TRTravel", 5000)],
    }),
    true,
  );
});

// Two cards answering to one typed name leave the appearance unattributable:
// the fuzzer creates duplicates from a five-name list, and the tree has been
// seen exposing the same card twice on a transition frame.
test("two cards matching the typed name are not attributable to the creation", () => {
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "home",
      lastAction: created,
      typedName: "Travel",
      before: [account("Checking", 0)],
      after: [account("Checking", 0), account("Travel", 5000), account("MyTravel", 900)],
    }),
    false,
  );
});

test("a card that was already there is not a card that was just created", () => {
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "home",
      lastAction: created,
      typedName: "Travel",
      before: [account("Checking", 0), account("Travel", 2411200)],
      after: [account("Checking", 0), account("Travel", 2411200)],
    }),
    false,
  );
});

// The apply call failed with the gesture possibly already delivered, so nobody
// knows whether that account was created. The card carrying the typed name may
// be an older one that scrolled into view, and attributing it to a creation
// that may never have happened is a conviction built on a guess.
test("a create the runner could not confirm attributes nothing", () => {
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "home",
      lastAction: { ...created, applied: null },
      typedName: "Travel",
      before: [account("Checking", 0)],
      after: [account("Checking", 0), account("Travel", 5000)],
    }),
    false,
  );
});
