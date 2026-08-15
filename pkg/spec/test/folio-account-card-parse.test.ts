import assert from "node:assert/strict";
import { test } from "node:test";

import {
  cardAccountName,
  cardBalanceText,
  parseDollarCents,
} from "../../../examples/folio/sanderling/predicates.ts";

// Android and iOS expose AccountName/AccountBalance as their own nodes. Web
// merges the card into one node whose text is initials + name +
// "N transaction(s)" + balance, with no separator between the parts. Both
// shapes have to land on the same balance, and the name has to stay usable as
// an identity key.

const balanceOf = (cardText: string) =>
  parseDollarCents(cardBalanceText({ childText: undefined, cardText }));

// Renders a card the way HomeScreen.kt does, so a test states the account and
// lets the fixture do the concatenating.
const card = (initials: string, name: string, count: number, balance: string) =>
  `${initials}${name}${count === 1 ? "1 transaction" : `${count} transactions`}${balance}`;

test("structured child wins over the card text", () => {
  assert.equal(
    cardBalanceText({ childText: "$118.00", cardText: "SASavings1 transaction$118.00" }),
    "$118.00",
  );
  assert.equal(
    cardAccountName({ childText: "Savings", cardText: "SASavings1 transaction$118.00" }),
    "Savings",
  );
});

test("merged card text: balance is the amount at the end, not scraped digits", () => {
  // The naive reading, text.replace(/[^0-9]/g, ""), absorbs the 12 of
  // "12 transactions" and returns 12258900.
  assert.equal(balanceOf("INInvestments12 transactions$2,589.00"), 258900);
});

test("merged card text: singular transaction label", () => {
  assert.equal(balanceOf("SASavings1 transaction$118.00"), 11800);
});

test("merged card text: negative balance keeps its sign", () => {
  assert.equal(cardBalanceText({ childText: undefined, cardText: "TRTravel3 transactions-$45.50" }), "-$45.50");
  assert.equal(balanceOf("TRTravel3 transactions-$45.50"), -4550);
});

test("structured child: negative balance keeps its sign", () => {
  assert.equal(parseDollarCents(cardBalanceText({ childText: "-$45.50", cardText: undefined })), -4550);
});

test("zero-balance card is 0, not unknown", () => {
  assert.equal(balanceOf("EFEmergency Fund0 transactions$0.00"), 0);
  assert.equal(balanceOf("Aa0 transactions$0.00"), 0);
});

// The trap a lazy balance regex falls into: a name ending in digits runs
// straight into the transaction count, so only anchoring the amount at the end
// of the string gets it right.
test("name ending in digits does not leak into the balance", () => {
  assert.equal(balanceOf(card("T2", "Travel 2024", 12, "$75.00")), 7500);
  assert.equal(balanceOf(card("T2", "Travel 2024", 0, "$0.00")), 0);
  assert.equal(balanceOf(card("20", "2024", 3, "-$1,234.56")), -123456);
});

// The limit of a key read off merged text, and the reason homeTxnCountsOf
// guards the ambiguity rather than resolving it: two DIFFERENT accounts render
// the same card, character for character. Names are unique (Accounts.name is
// UNIQUE, checked NOCASE) but the count runs straight into a name that ends in
// digits, so nothing computed from this string can say which account it is.
test("two accounts can render one card, so no key off it can be injective", () => {
  const travel1 = card("TR", "Travel1", 25, "$120.00");
  const travel12 = card("TR", "Travel12", 5, "$120.00");
  assert.equal(travel1, travel12);
  assert.equal(cardAccountName({ childText: undefined, cardText: travel1 }), "TRTravel");
});

// The account key only has to be stable and per-account. newAccountBalanceIsZero
// reads it as a set member: a key that drifted as an account's transaction
// count grew would make an existing account look brand new, and the property
// would fire on it for holding the balance it just earned.
test("account key is stable as the transaction count and balance move", () => {
  const cards: [string, string][] = [
    ["CH", "Checking"],
    ["T2", "Travel 2024"],
    ["A2", "Account 2"],
    ["Aa", "a"],
    ["?", ""],
    ["5T", "5 transactions"],
    ["X9", "x9"],
  ];
  for (const [initials, name] of cards) {
    const keys = new Set<string>();
    for (let count = 0; count <= 130; count++) {
      keys.add(cardAccountName({
        childText: undefined,
        cardText: card(initials, name, count, `$${count * 7}.50`),
      }));
    }
    assert.equal(keys.size, 1, `key for ${JSON.stringify(name)} drifted: ${[...keys].join(", ")}`);
  }
});

test("account keys are distinct across the accounts a run creates", () => {
  const names: [string, string][] = [
    ["CH", "Checking"],
    ["SA", "Savings"],
    ["TR", "Travel"],
    ["EF", "Emergency Fund"],
    ["IN", "Investments"],
    ["T2", "Travel 2024"],
    ["A2", "Account 2"],
    ["A1", "Account 12"],
    ["Aa", "a"],
  ];
  const keys = names.map(([initials, name]) =>
    cardAccountName({ childText: undefined, cardText: card(initials, name, 4, "$9.00") }));
  assert.equal(new Set(keys).size, names.length);
});

// elementHandle in the web runtime truncates node text at 200 characters, so a
// long account name (the input corpus types 4096 "a"s) pushes the balance off
// the end of the string. That balance is unknown, and unknown must not read as
// zero: newAccountBalanceIsZero passes an unknown balance rather than
// convicting a card it could not read.
test("card text truncated past the balance reads as unknown, not zero", () => {
  const cardText = "AA" + "a".repeat(198);
  assert.equal(cardBalanceText({ childText: undefined, cardText }), undefined);
  assert.equal(balanceOf(cardText), null);
});

test("empty and missing text are unknown, not zero", () => {
  assert.equal(parseDollarCents(undefined), null);
  assert.equal(parseDollarCents(""), null);
  assert.equal(parseDollarCents("no digits here"), null);
  assert.equal(parseDollarCents("$12"), null);
  assert.equal(cardBalanceText({ childText: undefined, cardText: undefined }), undefined);
  assert.equal(cardAccountName({ childText: undefined, cardText: undefined }), "");
});

// The two accessibility shapes have to read the same per-card balance, which is
// what the accounts extractor compares. The Home total is no longer a sum of
// these: it is the app's own TOTAL BALANCE node (see folio-total-balance.test.ts).
test("merged card text and a structured child give the same balance", () => {
  const merged = ["INInvestments12 transactions$2,589.00", "Aa0 transactions$0.00"].map(cardText =>
    cardBalanceText({ childText: undefined, cardText }));
  assert.deepEqual(merged, ["$2,589.00", "$0.00"]);
  assert.deepEqual(merged.map(parseDollarCents), [258900, 0]);
});
