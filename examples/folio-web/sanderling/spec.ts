import {
  InputText,
  Tap,
  actions,
  always,
  eventually,
  extract,
  from,
  next,
  now,
  swipes,
  taps,
  waitOnce,
  weighted,
} from "@sanderling/spec";
import { noUncaughtExceptions } from "@sanderling/spec/defaults/properties";

// Page-presence checks via stable element ids.
const onLoginPage = extract((s) => !!s.ax.find({ id: "email" }));
const onHomePage = extract((s) => !!s.ax.find({ id: "add-account" }));
const onAddAccountPage = extract((s) => !!s.ax.find({ id: "account-name" }));
const onLedgerPage = extract((s) => !!s.ax.find({ id: "ledger" }));
const onAddTxnPage = extract((s) => !!s.ax.find({ id: "txn-amount" }));

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
});

// Read raw cents off explicit data-cents attributes; no aria-label parsing.
function readCents(value: string | undefined): number {
  if (!value) return 0;
  const parsed = parseInt(value, 10);
  return isNaN(parsed) ? 0 : parsed;
}

const totalBalance = extract((s) => {
  const el = s.ax.find({ id: "total-balance" });
  return readCents(el?.attrs?.["data-cents"]);
});

// Account cards expose `data-account-id` + `data-balance` so the spec reads
// structured data without parsing aria-label.
const accountCards = extract((s) => {
  return s.ax.findAll({ "data-testid": "account-card" }).map((el) => ({
    element: el,
    id: el.attrs?.["data-account-id"] ?? "",
    balance: readCents(el.attrs?.["data-balance"]),
  }));
});

const ledgerTxnCount = extract((s) => {
  const el = s.ax.find({ id: "ledger" });
  return readCents(el?.attrs?.["data-txn-count"]);
});

const ledgerBalance = extract((s) => {
  const el = s.ax.find({ id: "ledger-balance" });
  return readCents(el?.attrs?.["data-cents"]);
});

// UI element handles.
const emailField = extract((s) => s.ax.find({ id: "email" }));
const passwordField = extract((s) => s.ax.find({ id: "password" }));
const loginSubmit = extract((s) => s.ax.find({ id: "login-submit" }));
const logoutButton = extract((s) => s.ax.find({ id: "logout" }));
const addAccountButton = extract((s) => s.ax.find({ id: "add-account" }));
const accountNameField = extract((s) => s.ax.find({ id: "account-name" }));
const addAccountSubmit = extract((s) => s.ax.find({ id: "add-account-submit" }));
const addTxnButton = extract((s) => s.ax.find({ id: "add-txn" }));
const txnAmountField = extract((s) => s.ax.find({ id: "txn-amount" }));
const txnNoteField = extract((s) => s.ax.find({ id: "txn-note" }));
const txnCreditButton = extract((s) => s.ax.find({ id: "txn-credit" }));
const txnDebitButton = extract((s) => s.ax.find({ id: "txn-debit" }));
const txnSubmit = extract((s) => s.ax.find({ id: "txn-submit" }));
const backButton = extract((s) => s.ax.find({ id: "back" }));

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

const accountNameSampler = from([
  "Checking",
  "Savings",
  "Travel",
  "Rent",
  "Emergency Fund",
  "Investments",
  "Groceries",
  "  ",
  "Checking",
  "A".repeat(41),
  "Petty Cash",
]);

const typeAccountName = actions(() => {
  if (!onAddAccountPage.current) return [];
  const field = accountNameField.current;
  return field ? [InputText({ into: field, text: accountNameSampler.generate() })] : [];
});

const submitAddAccount = actions(() => {
  if (!onAddAccountPage.current) return [];
  const btn = addAccountSubmit.current;
  return btn ? [Tap({ on: btn })] : [];
});

const openAccount = actions(() => {
  if (!onHomePage.current) return [];
  const cards = accountCards.current;
  if (cards.length === 0) return [];
  const card = cards[Math.floor(Math.random() * cards.length)];
  return [Tap({ on: card.element })];
});

const openAddTxn = actions(() => {
  if (!onLedgerPage.current) return [];
  const btn = addTxnButton.current;
  return btn ? [Tap({ on: btn })] : [];
});

const amountSampler = from([
  "12.34",
  "100",
  "0.01",
  "999.99",
  "5.5",
  "42",
  "0",
  "",
  "1e4",
  "0.001",
  "-5",
]);

const typeAmount = actions(() => {
  if (!onAddTxnPage.current) return [];
  const field = txnAmountField.current;
  return field ? [InputText({ into: field, text: amountSampler.generate() })] : [];
});

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
  const credit = txnCreditButton.current;
  const debit = txnDebitButton.current;
  const target = Math.random() < 0.5 ? credit : debit;
  return target ? [Tap({ on: target })] : [];
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
  [18, typeAccountName],
  [14, submitAddAccount],
  [14, openAccount],
  [12, openAddTxn],
  [18, typeAmount],
  [8, typeNote],
  [6, toggleTxnType],
  [16, submitTxn],
  [6, goBack],
  [1, logoutAction],
  [4, taps],
  [2, swipes],
  [2, waitOnce],
);

(globalThis as { actions?: unknown; properties?: unknown }).actions = actionsRoot;
(globalThis as { properties?: unknown }).properties = properties;
