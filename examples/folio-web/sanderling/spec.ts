import {
  InputText,
  Tap,
  actions,
  always,
  edgeCaseText,
  eventually,
  extract,
  from,
  integers,
  next,
  now,
  taps,
  waitOnce,
  weighted,
} from "@sanderling/spec";
import { noUncaughtExceptions } from "@sanderling/spec/defaults/properties";

// Page-presence checks via stable element ids.
const onLoginPage = extract((s) => !!s.ax.find({ id: "email" })).named("onLoginPage");
const onHomePage = extract((s) => !!s.ax.find({ id: "add-account" })).named("onHomePage");
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

const totalBalance = extract((s) => {
  const el = s.ax.find({ id: "total-balance" });
  return readCents(el?.attrs?.["data-cents"]);
}).named("totalBalance");

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

const ledgerBalance = extract((s) => {
  const el = s.ax.find({ id: "ledger-balance" });
  return readCents(el?.attrs?.["data-cents"]);
}).named("ledgerBalance");

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

const totalBalanceMatchesAccounts = always(() => {
  if (!onHomePage.current) return true;
  const cards = accountCards.current;
  if (cards.length === 0) return true;
  const sum = cards.reduce((acc, c) => acc + c.balance, 0);
  return sum === totalBalance.current;
});

const balanceMatchesTransactionDelta = always(
  now(() => onLedgerPage.current && ledgerTxnCount.current > 0).implies(
    next(() => {
      if (!onLedgerPage.current) return true;
      const prevCount = ledgerTxnCount.previous ?? 0;
      const curCount = ledgerTxnCount.current;
      if (curCount !== prevCount + 1) return true;
      const prevBal = ledgerBalance.previous ?? 0;
      const curBal = ledgerBalance.current;
      return curBal !== prevBal;
    }),
  ),
);

const loginReachable = eventually(() => loggedIn.current).within(90, "seconds");
const accountCreationReachable = eventually(
  () => accountCards.current.length > 0,
).within(180, "seconds");
const someTransactionExists = eventually(
  () => ledgerTxnCount.current > 0,
).within(300, "seconds");

export const properties = {
  loggedInLeavesLogin,
  loggedOutReachesLogin,
  totalBalanceMatchesAccounts,
  balanceMatchesTransactionDelta,
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
// exercise duplicate-name handling.
const accountNames = from([
  "Checking",
  "Savings",
  "Travel",
  "Rent",
  "Emergency Fund",
  "Investments",
  "Groceries",
  "Petty Cash",
]);

function typeAccountNameWith(sampler: { generate(): string }) {
  return actions(() => {
    if (!onAddAccountPage.current) return [];
    const field = accountNameField.current;
    return field ? [InputText({ into: field, text: sampler.generate() })] : [];
  });
}

const typeAccountName = typeAccountNameWith(accountNames);
const typeAccountNameEdge = typeAccountNameWith(edgeCaseText());

const submitAddAccount = actions(() => {
  if (!onAddAccountPage.current) return [];
  const btn = addAccountSubmit.current;
  return btn ? [Tap({ on: btn })] : [];
});

const openAccount = actions(() => {
  if (!onHomePage.current) return [];
  const cards = accountCards.current;
  if (cards.length === 0) return [];
  return [Tap({ on: from(cards).generate().element })];
});

const openAddTxn = actions(() => {
  if (!onLedgerPage.current) return [];
  const btn = addTxnButton.current;
  return btn ? [Tap({ on: btn })] : [];
});

// Valid happy-path amounts keep the balance properties exercised; the edge
// branch (weighted in actionsRoot) stresses parsing with the adversarial corpus.
const validAmounts = integers().between(1, 99999);

function typeAmountWith(sampler: { generate(): string }) {
  return actions(() => {
    if (!onAddTxnPage.current) return [];
    const field = txnAmountField.current;
    return field ? [InputText({ into: field, text: sampler.generate() })] : [];
  });
}

const typeAmount = typeAmountWith({ generate: () => String(validAmounts.generate()) });
const typeAmountEdge = typeAmountWith(edgeCaseText());

const noteSampler = from([
  "Coffee",
  "Paycheck",
  "Gas",
  "Refund",
  "",
  "Groceries for the week",
]);

const typeNote = actions(() => {
  if (!onAddTxnPage.current) return [];
  const field = txnNoteField.current;
  return field ? [InputText({ into: field, text: noteSampler.generate() })] : [];
});

const toggleTxnType = actions(() => {
  if (!onAddTxnPage.current) return [];
  const targets = [txnCreditButton.current, txnDebitButton.current].filter(Boolean);
  if (targets.length === 0) return [];
  return [Tap({ on: from(targets).generate() })];
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

export const actionsRoot = weighted(
  [30, loginHelper],
  [2, adversarialLogin],
  [14, openAddAccount],
  [14, typeAccountName],
  [4, typeAccountNameEdge],
  [14, submitAddAccount],
  [14, openAccount],
  [12, openAddTxn],
  [14, typeAmount],
  [4, typeAmountEdge],
  [8, typeNote],
  [6, toggleTxnType],
  [16, submitTxn],
  [6, goBack],
  [1, logoutAction],
  [4, taps],
  [2, waitOnce],
);
