import {
  InputText,
  Tap,
  actions,
  always,
  doubleTaps,
  eventually,
  extract,
  llm,
  next,
  now,
  taps,
  typing,
  waitOnce,
  weighted,
} from "@sanderling/spec";
import type { State } from "@sanderling/spec";
import { noUncaughtExceptions } from "@sanderling/spec/defaults/properties";
import {
  committedTransactionsExceedSubmits,
  countSubmitsInWindow,
  homeTxnCountsOf,
  readHomeCards,
} from "./predicates";

// Page-presence checks via stable element ids.
const isHomeFrame = (s: State) => !!s.ax.find({ id: "add-account" });

const onLoginPage = extract((s) => !!s.ax.find({ id: "email" })).named("onLoginPage");
const onHomePage = extract(isHomeFrame).named("onHomePage");
const onAddAccountPage = extract((s) => !!s.ax.find({ id: "account-name" })).named("onAddAccountPage");
const onLedgerPage = extract((s) => !!s.ax.find({ id: "ledger" })).named("onLedgerPage");
const onAddTxnPage = extract((s) => !!s.ax.find({ id: "txn-amount" })).named("onAddTxnPage");

// Auth state: true on any authenticated page, false only on login page.
const loggedIn = extract((s) => {
  if (s.ax.find({ id: "email" })) return false;
  return !!(
    s.ax.find({ id: "logout" }) ||
    s.ax.find({ id: "add-account" }) ||
    s.ax.find({ id: "ledger" }) ||
    s.ax.find({ id: "account-name" }) ||
    s.ax.find({ id: "txn-amount" }) ||
    s.ax.find({ id: "add-txn" })
  );
}).named("loggedIn");

// Read raw cents off explicit data-cents attributes; no aria-label parsing.
function readCents(value: string | undefined): number {
  if (!value) return 0;
  const parsed = parseInt(value, 10);
  return isNaN(parsed) ? 0 : parsed;
}

// A count of 0 is a real reading and a missing one is not, so this cannot fall
// back to readCents.
function readCount(value: string | undefined): number | undefined {
  if (value === undefined) return undefined;
  const parsed = parseInt(value, 10);
  return Number.isSafeInteger(parsed) ? parsed : undefined;
}

// Account cards expose `data-account-id` + `data-balance` so the spec reads
// structured data without parsing aria-label.
const accountCards = extract((s) => {
  return s.ax.findAll({ "data-testid": "account-card" }).map((el) => ({
    element: el,
    id: el.attrs?.["data-account-id"] ?? "",
    balance: readCents(el.attrs?.["data-balance"]),
  }));
}).named("accountCards");

const ledgerTxnCount = extract((s) => {
  const el = s.ax.find({ id: "ledger" });
  return readCents(el?.attrs?.["data-txn-count"]);
}).named("ledgerTxnCount");

// Transactions committed per account, off Home's own cards, carried across
// off-Home steps: the pair a property compares has to be two Home readings, not
// a Home reading and whatever happened to be on screen. See readHomeCards.
const homeCardReadings = (s: State) =>
  s.ax.findAll({ "data-testid": "account-card" }).map((el) => ({
    accountId: el.attrs?.["data-account-id"] ?? "",
    count: readCount(el.attrs?.["data-txn-count"]),
  }));

let lastHomeTxnCounts: Record<string, number> | null = null;
const homeTxnCounts = extract((s) => {
  const reading = readHomeCards({
    onHome: isHomeFrame(s),
    reading: homeTxnCountsOf(homeCardReadings(s)),
    previousCarrier: lastHomeTxnCounts,
  });
  lastHomeTxnCounts = reading.carrier;
  return reading.value;
}).named("homeTxnCounts");

// Submit actions inside the window `homeTxnCounts.previous` and
// `homeTxnCounts.current` span. Recomputed rather than shared with the
// extractor above because extractor getters may not read one another; both
// derive freshness from the same reading, so they reset on the same step.
let submitsSinceHomeCards = 0;
const submitsSinceCounts = extract((s) => {
  const fresh = readHomeCards({
    onHome: isHomeFrame(s),
    reading: homeTxnCountsOf(homeCardReadings(s)),
    previousCarrier: null,
  }).fresh;
  const counted = countSubmitsInWindow({
    previousCount: submitsSinceHomeCards,
    lastAction: s.lastAction,
    amountText: s.ax.find({ id: "txn-amount" })?.attrs?.["value"],
    fresh,
  });
  submitsSinceHomeCards = counted.next;
  return counted.reported;
}).named("submitsSinceCounts");

// UI element handles.
const emailField = extract((s) => s.ax.find({ id: "email" })).named("emailField");
const passwordField = extract((s) => s.ax.find({ id: "password" })).named("passwordField");
const loginSubmit = extract((s) => s.ax.find({ id: "login-submit" })).named("loginSubmit");
const logoutButton = extract((s) => s.ax.find({ id: "logout" })).named("logoutButton");
const addAccountButton = extract((s) => s.ax.find({ id: "add-account" })).named("addAccountButton");
const accountNameField = extract((s) => s.ax.find({ id: "account-name" })).named("accountNameField");
const addAccountSubmit = extract((s) => s.ax.find({ id: "add-account-submit" })).named("addAccountSubmit");
const addTxnButton = extract((s) => s.ax.find({ id: "add-txn" })).named("addTxnButton");
const txnAmountField = extract((s) => s.ax.find({ id: "txn-amount" })).named("txnAmountField");
const txnNoteField = extract((s) => s.ax.find({ id: "txn-note" })).named("txnNoteField");
const txnCreditButton = extract((s) => s.ax.find({ id: "txn-credit" })).named("txnCreditButton");
const txnDebitButton = extract((s) => s.ax.find({ id: "txn-debit" })).named("txnDebitButton");
const txnSubmit = extract((s) => s.ax.find({ id: "txn-submit" })).named("txnSubmit");
const backButton = extract((s) => s.ax.find({ id: "back" })).named("backButton");

// -- Properties --

const loggedInLeavesLogin = always(
  now(() => loggedIn.current).implies(
    eventually(() => !onLoginPage.current).within(3, "seconds"),
  ),
);

const loggedOutReachesLogin = always(
  now(() => !loggedIn.current).implies(
    eventually(() => onLoginPage.current).within(3, "seconds"),
  ),
);

// One action commits at most one transaction, measured over the window between
// two Home card readings rather than over a single step transition.
//
// The window is what makes it work. A commit always routes through
// /transactions/new and pops back to the ledger it came from, so the fuzzer can
// add transactions all day without Home being redrawn: any rule phrased over
// two consecutive steps on one screen never gets a pair to judge. This one
// stays sound however wide the gap between two Home readings gets, because the
// submit count is taken over exactly the same interval as the two counts it
// compares.
//
// `next` because the first step of a run has no previous reading to compare
// against.
const submitCommitsOneTransactionPerAction = always(
  next(
    () =>
      !committedTransactionsExceedSubmits({
        countsBefore: homeTxnCounts.previous ?? null,
        countsAfter: homeTxnCounts.current,
        submitsInWindow: submitsSinceCounts.current,
      }),
  ),
);

// The three reachability goals are bounded in steps, not seconds, because they
// are compared across action-selection policies and the model policy spends a
// provider call per step. A 300-step seeded run of this app takes about 47
// seconds, so the wall-clock windows these replace are 6.38 steps per second:
// 90 s is 575 steps, 180 s is 1150, and 300 s is 1915. The window now costs
// the same whatever is driving.
const loginReachable = eventually(() => loggedIn.current).within(575, "steps");
const accountCreationReachable = eventually(
  () => accountCards.current.length > 0,
).within(1150, "steps");
const someTransactionExists = eventually(
  () => ledgerTxnCount.current > 0,
).within(1915, "steps");

export const properties = {
  loggedInLeavesLogin,
  loggedOutReachesLogin,
  submitCommitsOneTransactionPerAction,
  loginReachable,
  accountCreationReachable,
  someTransactionExists,
  noUncaughtExceptions,
};

// -- Actions --

const DEMO_EMAIL = "demo@ledger.app";
const DEMO_PASSWORD = "ledger123";

function focusedField(): string | null {
  const email = emailField.current;
  const password = passwordField.current;
  if (email && (email as { focused?: boolean }).focused) return "email";
  if (password && (password as { focused?: boolean }).focused) return "password";
  return null;
}

const loginHelper = actions(() => {
  if (loggedIn.current) return [];
  const email = emailField.current;
  const password = passwordField.current;
  const submit = loginSubmit.current;
  if (!email || !password || !submit) return [];
  const focused = focusedField();
  if (focused === "password") return [Tap({ on: submit })];
  if (focused === "email") return [InputText({ into: password, text: DEMO_PASSWORD })];
  return [InputText({ into: email, text: DEMO_EMAIL })];
});

const adversarialLogin = actions(() => {
  if (loggedIn.current) return [];
  const submit = loginSubmit.current;
  if (!submit) return [];
  return [Tap({ on: submit })];
});

const openAddAccount = actions(() => {
  if (!onHomePage.current) return [];
  const btn = addAccountButton.current;
  return btn ? [Tap({ on: btn })] : [];
});

// Readable enumeration keeps the demo legible; repeats over a run still
// exercise duplicate-name handling. One action per name rather than one action
// for a drawn name: a draw reads the seeded picker's stream, which the model
// policy never enters, so the two would explore different action spaces.
const accountNames = [
  "Checking",
  "Savings",
  "Travel",
  "Rent",
  "Emergency Fund",
  "Investments",
  "Groceries",
  "Petty Cash",
];

const typeAccountName = actions(() => {
  if (!onAddAccountPage.current) return [];
  const field = accountNameField.current;
  if (!field) return [];
  return accountNames.map((name) => InputText({ into: field, text: name }));
});

const submitAddAccount = actions(() => {
  if (!onAddAccountPage.current) return [];
  const btn = addAccountSubmit.current;
  return btn ? [Tap({ on: btn })] : [];
});

const openAccount = actions(() => {
  if (!onHomePage.current) return [];
  return accountCards.current.map((card) => Tap({ on: card.element }));
});

const openAddTxn = actions(() => {
  if (!onLedgerPage.current) return [];
  const btn = addTxnButton.current;
  return btn ? [Tap({ on: btn })] : [];
});

// Valid happy-path amounts keep the balance properties exercised; the `typing`
// branch in actionsRoot stresses parsing with the adversarial corpus. Four
// authored values rather than a range, because a range cannot be enumerated:
// the smallest accepted amount, one with cents, an everyday one, and one wide
// enough to format with a thousands separator.
const validAmounts = ["1", "12.34", "250", "99999"];

const typeAmount = actions(() => {
  if (!onAddTxnPage.current) return [];
  const field = txnAmountField.current;
  if (!field) return [];
  return validAmounts.map((amount) => InputText({ into: field, text: amount }));
});

const notes = ["Coffee", "Paycheck", "Gas", "Refund", "", "Groceries for the week"];

const typeNote = actions(() => {
  if (!onAddTxnPage.current) return [];
  const field = txnNoteField.current;
  if (!field) return [];
  return notes.map((note) => InputText({ into: field, text: note }));
});

const toggleTxnType = actions(() => {
  if (!onAddTxnPage.current) return [];
  const credit = txnCreditButton.current;
  const debit = txnDebitButton.current;
  const options = [];
  if (credit) options.push(Tap({ on: credit }));
  if (debit) options.push(Tap({ on: debit }));
  return options;
});

const submitTxn = actions(() => {
  if (!onAddTxnPage.current) return [];
  const btn = txnSubmit.current;
  return btn ? [Tap({ on: btn })] : [];
});

const goBack = actions(() => {
  const btn = backButton.current;
  return btn ? [Tap({ on: btn })] : [];
});

const logoutAction = actions(() => {
  if (!onHomePage.current) return [];
  const btn = logoutButton.current;
  return btn ? [Tap({ on: btn })] : [];
});

// `typing` carries the adversarial input the two authored edge-case leaves used
// to: it names the field and leaves the value to whichever policy is driving,
// so the seeded picker draws from the edge-case corpus and the model writes its
// own text. It holds the 8 weight those two leaves shared, so every other
// branch keeps the share it had.
//
// `doubleTaps` gets explicit weight because rapid double-submission is a
// failure mode these forms must be idempotent under, and no other branch
// provokes it: every authored leaf taps once.
export const actionsRoot = weighted(
  [30, loginHelper],
  [2, adversarialLogin],
  [14, openAddAccount],
  [14, typeAccountName],
  [14, submitAddAccount],
  [14, openAccount],
  [12, openAddTxn],
  [14, typeAmount],
  [8, typeNote],
  [6, toggleTxnType],
  [16, submitTxn],
  [6, goBack],
  [1, logoutAction],
  [4, taps],
  [8, typing],
  [5, doubleTaps],
  [2, waitOnce],
);

// The LLM generator is orthogonal to actionsRoot: with `--generator llm` a model
// picks from the SAME weighted candidate set above, reading the screenshot and a
// numbered, weight-annotated list; the default `--generator seeded` ignores it.
// instructions describe only WHAT the app is, never HOW to test it.
export const generator = llm({
  model: "gpt-5.4-nano",
  instructions:
    "Folio is a personal-finance ledger app in the browser. Sign in with the demo credentials shown on the login screen. The home screen lists accounts, each with a balance. You can create accounts, open an account to see its ledger, and add credit or debit transactions; each transaction has an amount and changes that account's balance and the overall total.",
});
