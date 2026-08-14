import assert from "node:assert/strict";
import { test } from "node:test";

import {
  cardTxnCount,
  committedTransactionsExceedSubmits,
  homeTxnCountsOf,
} from "../../../examples/folio/sanderling/predicates.ts";

// Android and iOS give the count its own node; web merges the card into one
// string, where the count sits between the name and the balance. The reading
// carries which of the two it came from: a number is a count nothing else could
// have leaked into, a string is a digit run that may have.
test("a dedicated count node reads as a number, not a digit run", () => {
  assert.equal(
    cardTxnCount({ childText: "12 transactions", cardText: "INInvestments12 transactions$2,589.00" }),
    12,
  );
});

test("merged card text: the count is taken from in front of the balance", () => {
  assert.equal(
    cardTxnCount({ childText: undefined, cardText: "INInvestments12 transactions$2,589.00" }),
    "12",
  );
});

test("merged card text: the singular label parses too", () => {
  assert.equal(
    cardTxnCount({ childText: undefined, cardText: "SASavings1 transaction$118.00" }),
    "1",
  );
});

// The balance has to come off first, or a name ending in digits would be read
// as the count.
test("a card with no readable count is unknown, not zero", () => {
  assert.equal(cardTxnCount({ childText: undefined, cardText: undefined }), undefined);
  assert.equal(cardTxnCount({ childText: undefined, cardText: "no digits here" }), undefined);
  assert.equal(cardTxnCount({ childText: "", cardText: "AA" + "a".repeat(198) }), undefined);
});

// Measured on a real web run: the account named "-1" holding 2 transactions
// merges to "-1-12 transactions-$119.00", and the maximal digit run reads 12.
test("merged text runs a digit-ending name into the count", () => {
  assert.equal(
    cardTxnCount({ childText: undefined, cardText: "-1-12 transactions-$119.00" }),
    "12",
  );
});

const before = { Checking: "3", Savings: "1" };

test("healthy window: three submits, three transactions", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: before,
      countsAfter: { Checking: "5", Savings: "2" },
      submitsInWindow: 3,
    }),
    false,
  );
});

test("rejected submits commit nothing, which is under the bound", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: before,
      countsAfter: { Checking: "3", Savings: "1" },
      submitsInWindow: 4,
    }),
    false,
  );
});

// The bug, stated directly: one tap, two rows.
test("double submit: one action commits two transactions", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: before,
      countsAfter: { Checking: "5", Savings: "1" },
      submitsInWindow: 1,
    }),
    true,
  );
});

// The point of counting actions against transactions rather than gating on a
// one-submit window: a wide window is still a sound comparison.
test("wide window: five submits committing six transactions still fires", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: before,
      countsAfter: { Checking: "8", Savings: "4" },
      submitsInWindow: 5,
    }),
    true,
  );
});

test("boundary: committed equal to the submit count is not a violation", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: before,
      countsAfter: { Checking: "4", Savings: "1" },
      submitsInWindow: 1,
    }),
    false,
  );
});

test("boundary: one transaction past the submit count is", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: before,
      countsAfter: { Checking: "4", Savings: "2" },
      submitsInWindow: 1,
    }),
    true,
  );
});

// Only accounts in both readings count. A card that scrolled out of the
// viewport, or one whose count was unreadable, drops out of the sum, so the
// result is a lower bound on what committed. Losing a card can only cost a
// detection; it must never manufacture one.
test("an account missing from the later reading is not counted", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { Checking: "3", Savings: "1" },
      countsAfter: { Checking: "3" },
      submitsInWindow: 0,
    }),
    false,
  );
});

test("an account appearing only in the later reading is not counted", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { Checking: "3" },
      countsAfter: { Checking: "3", Travel: "9" },
      submitsInWindow: 0,
    }),
    false,
  );
});

test("a card that scrolled away and back is not double counted", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { Checking: "3" },
      countsAfter: { Checking: "4", Savings: "40" },
      submitsInWindow: 1,
    }),
    false,
  );
});

test("an unknown reading on either side is not evidence", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: null,
      countsAfter: { Checking: "99" },
      submitsInWindow: 0,
    }),
    false,
  );
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { Checking: "0" },
      countsAfter: null,
      submitsInWindow: 0,
    }),
    false,
  );
});

// The real trace this came from: at the violating step of seeds 3 and 5 the
// window held exactly one submit action and the account's count moved by two.
test("the measured web witness: submits 1, count delta 2", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { INInvestments: "12", Aa: "0" },
      countsAfter: { INInvestments: "14", Aa: "0" },
      submitsInWindow: 1,
    }),
    true,
  );
});

// The length rule, which is what keeps the merged-text prefix honest. The
// account named "-1" reads 19 at nine transactions and 110 at ten: same account,
// a delta of 91 out of a true delta of 1. Different run lengths are dropped.
test("a count crossing a digit boundary is dropped, not convicted on", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { "-1-": "19" },
      countsAfter: { "-1-": "110" },
      submitsInWindow: 1,
    }),
    false,
  );
});

test("same run length keeps the prefixed delta exact", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { "-1-": "110" },
      countsAfter: { "-1-": "112" },
      submitsInWindow: 1,
    }),
    true,
  );
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { "-1-": "110" },
      countsAfter: { "-1-": "111" },
      submitsInWindow: 1,
    }),
    false,
  );
});

test("an unreadably long run is not evidence", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { Checking: "1".repeat(21) },
      countsAfter: { Checking: "9".repeat(21) },
      submitsInWindow: 0,
    }),
    false,
  );
});

// The other side of that rule, and the reason it is scoped to merged text: a
// count read off its own node has no account name in front of it, so its digits
// ARE the count and a decade crossing is just a number getting longer. Both
// windows below are real android seed-9 readings that the unscoped length rule
// threw away, in runs that then finished clean.
test("a dedicated node's count crossing a decade is usable evidence", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { Checking: 7 },
      countsAfter: { Checking: 12 },
      submitsInWindow: 1,
    }),
    true,
  );
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { Savings: 4 },
      countsAfter: { Savings: 10 },
      submitsInWindow: 1,
    }),
    true,
  );
});

// Recovering the window is only worth anything if it still acquits the healthy
// case, so the same crossing under a submit that earned it must not fire.
test("a dedicated node's healthy decade crossing does not convict", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { Checking: 9 },
      countsAfter: { Checking: 10 },
      submitsInWindow: 1,
    }),
    false,
  );
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { Checking: 9 },
      countsAfter: { Checking: 11 },
      submitsInWindow: 1,
    }),
    true,
  );
});

// The same numbers off merged text, where the digits may not be the count at
// all: still dropped.
test("the merged-text equivalent of that crossing is still dropped", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { Checking: "9" },
      countsAfter: { Checking: "10" },
      submitsInWindow: 0,
    }),
    false,
  );
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { Checking: "7" },
      countsAfter: { Checking: "12" },
      submitsInWindow: 1,
    }),
    false,
  );
});

// The boundary itself. A pair whose two readings came from different sources is
// vouched for by neither rule: the string may carry a name prefix the number
// does not, so subtracting them is not a transaction count.
test("a pair straddling the two sources is not comparable", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { Checking: 7 },
      countsAfter: { Checking: "12" },
      submitsInWindow: 1,
    }),
    false,
  );
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: { Checking: "7" },
      countsAfter: { Checking: 12 },
      submitsInWindow: 1,
    }),
    false,
  );
});

// End to end from the two accessibility shapes, which is where the distinction
// is actually made: the same account, the same true counts, read once off a
// dedicated node and once off merged card text.
const dedicated = (name: string, count: number) => ({
  name,
  balance: 0,
  count: cardTxnCount({ childText: `${count} transactions`, cardText: undefined }),
});

const merged = (initials: string, name: string, count: number) => ({
  name,
  balance: 0,
  count: cardTxnCount({
    childText: undefined,
    cardText: `${initials}${name}${count} transactions$0.00`,
  }),
});

test("a dedicated-node card list convicts across a decade", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: homeTxnCountsOf([dedicated("Checking", 9)]),
      countsAfter: homeTxnCountsOf([dedicated("Checking", 11)]),
      submitsInWindow: 1,
    }),
    true,
  );
});

test("the merged-text card list drops the same pair", () => {
  assert.equal(
    committedTransactionsExceedSubmits({
      countsBefore: homeTxnCountsOf([merged("CH", "Checking", 9)]),
      countsAfter: homeTxnCountsOf([merged("CH", "Checking", 11)]),
      submitsInWindow: 1,
    }),
    false,
  );
});
