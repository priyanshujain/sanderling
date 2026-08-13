import {
  InputText,
  Tap,
  actions,
  always,
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
