import assert from "node:assert/strict";
import { test } from "node:test";

import {
  createdAccountHasNonZeroBalance,
  initialsOf,
} from "../../../examples/folio/sanderling/predicates.ts";

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

// The avatar the merged web key opens with, hand-computed off Format.kt rather
// than off the mirror, because a mirror checked against itself checks nothing.
// A single word gives its first two characters, several give the first letter
// of the first and of the last, and an empty name gives "?".
test("the initials a merged key opens with are the app's", () => {
  const named: [string, string][] = [
    ["CH", "Checking"],
    ["SA", "Savings"],
    ["TR", "Travel"],
    ["EF", "Emergency Fund"],
    ["IN", "Investments"],
    ["FU", "Fund"],
    ["T2", "Travel 2024"],
    ["A", "a"],
    ["X9", "x9"],
    ["-1", "-1"],
    ["?", ""],
    ["?", "   "],
  ];
  for (const [initials, name] of named) {
    assert.equal(initialsOf(name), initials, `initials for ${JSON.stringify(name)}`);
  }
});

// The attribution used to be a suffix test, and a suffix test hands the verdict
// to whichever OTHER account happens to end with the typed name. Home lists
// what fits the viewport, so the card that was just created is clipped out of
// the reading exactly as easily as any other, and the older account left in it
// is then judged for money it has held all along.
test("an older account whose name ends with the typed one is not the created one", () => {
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "home",
      lastAction: created,
      typedName: "Fund",
      before: [account("Checking", 0)],
      after: [account("Checking", 0), account("Emergency Fund", 461012300)],
    }),
    false,
  );
});

test("the merged web key is matched whole too, not by its ending", () => {
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "home",
      lastAction: created,
      typedName: "Fund",
      before: [account("CHChecking", 0)],
      after: [account("CHChecking", 0), account("EFEmergency Fund", 461012300)],
    }),
    false,
  );
});

// The card that was actually asked for is still judged, standing next to the
// account that merely ends with its name.
test("the created card is judged beside an account whose name ends with it", () => {
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "home",
      lastAction: created,
      typedName: "Fund",
      before: [account("Emergency Fund", 461012300)],
      after: [account("Emergency Fund", 461012300), account("Fund", 5000)],
    }),
    true,
  );
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "home",
      lastAction: created,
      typedName: "Fund",
      before: [account("EFEmergency Fund", 461012300)],
      after: [account("EFEmergency Fund", 461012300), account("FUFund", 5000)],
    }),
    true,
  );
});

// Two cards answering to one typed name leave the appearance unattributable.
// Accounts.name is UNIQUE and Repository.createAccount rejects a name already
// taken, so the pair is one card the tree exposed twice on a transition frame,
// or two names the merged web key cannot tell apart.
test("two cards matching the typed name are not attributable to the creation", () => {
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "home",
      lastAction: created,
      typedName: "Travel",
      before: [account("Checking", 0)],
      after: [account("Checking", 0), account("Travel", 5000), account("Travel", 900)],
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

// The runner's foreground guard restarted the app after the create. A fresh
// launch draws Home from the top, so the visible set is whatever the new layout
// fits rather than what was there a step ago, and "appeared in the reading" is
// even less like "was created" than usual. The process may also have died
// before the write landed, which makes the card that carries the typed name an
// older account of that name coming into view.
test("a create the runner relaunched across attributes nothing", () => {
  assert.equal(
    createdAccountHasNonZeroBalance({
      route: "home",
      lastAction: { ...created, relaunched: true },
      typedName: "Travel",
      before: [account("Checking", 0)],
      after: [account("Checking", 0), account("Travel", 5000)],
    }),
    false,
  );
});

test("no relaunch reported still judges the account that was created", () => {
  for (const relaunched of [null, undefined]) {
    assert.equal(
      createdAccountHasNonZeroBalance({
        route: "home",
        lastAction: { ...created, relaunched },
        typedName: "Travel",
        before: [account("Checking", 0)],
        after: [account("Checking", 0), account("Travel", 5000)],
      }),
      true,
    );
  }
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
