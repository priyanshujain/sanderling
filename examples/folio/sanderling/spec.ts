import {
  InputText,
  Tap,
  actions,
  always,
  extract,
  from,
  keyedBy,
  next,
  now,
  weighted,
  whenRoute,
} from "@sanderling/spec";

interface Account {
  name: string;
  balance: number;
}

interface LedgerRow {
  key: string;
  signed: number;
}

// Parses formatCents output like "$5.00", "-$1,234.56", "+$0.50" back to integer cents.
function parseDollarCents(text: string | undefined): number {
  if (!text) return 0;
  const sign = text.startsWith("-") ? -1 : 1;
  const digits = text.replace(/[^0-9]/g, "");
  return digits ? sign * parseInt(digits, 10) : 0;
}

// Route detection via testTag (resource-id on Android, accessibilityIdentifier on iOS)
const loggedIn = extract(s => s.ax.find({ testTag: "LoginScreen" }) == null);
const route = extract<string | null>(s => {
  if (s.ax.find({ testTag: "LoginScreen" })) return "login";
  if (s.ax.find({ testTag: "AddAccountScreen" })) return "add-account";
  if (s.ax.find({ testTag: "AddTransactionScreen" })) return "add-transaction";
  if (s.ax.find({ testTag: "LedgerScreen" })) return "ledger";
  if (s.ax.find({ testTag: "HomeScreen" })) return "home";
  return null;
});

// Account cards on Home: identity is the AccountName text; balance comes from AccountBalance.
const accounts = extract<Account[]>(s =>
  s.ax.findAll([{ testTag: "HomeScreen" }, { testTag: "AccountCard" }]).map(card => ({
    name: card.find({ testTag: "AccountName" })?.text ?? "",
    balance: parseDollarCents(card.find({ testTag: "AccountBalance" })?.text),
  })));

// Ledger rows: identity composed from the row's stable testTag'd cells.
const ledgerRows = extract<LedgerRow[]>(s =>
  s.ax.findAll([{ testTag: "LedgerScreen" }, { testTag: "LedgerRow" }]).map(row => ({
    key: keyedBy(row, ["TxnDate", "TxnNote", "TxnAmount"]),
    signed: parseDollarCents(row.find({ testTag: "TxnAmount" })?.text),
  })));

const ledgerBalance = extract(s =>
  parseDollarCents(s.ax.find({ testTag: "LedgerBalance" })?.text));

const focusedFieldTag = extract(s => s.ax.find({ focused: "true" })?.id ?? null);

const loginEmailField = extract(s =>
  s.ax.find([{ testTag: "LoginScreen" }, { testTag: "LoginEmail" }]));
const loginPasswordField = extract(s =>
  s.ax.find([{ testTag: "LoginScreen" }, { testTag: "LoginPassword" }]));
const loginSubmit = extract(s =>
  s.ax.find([{ testTag: "LoginScreen" }, { testTag: "LoginSubmit" }]));
const addAccountButton = extract(s =>
  s.ax.find([{ testTag: "HomeScreen" }, { testTag: "AddAccountButton" }]));
const accountNameField = extract(s =>
  s.ax.find([{ testTag: "AddAccountScreen" }, { testTag: "AccountNameField" }]));
const addAccountSubmit = extract(s =>
  s.ax.find([{ testTag: "AddAccountScreen" }, { testTag: "AddAccountSubmit" }]));
const addTxnButton = extract(s =>
  s.ax.find([{ testTag: "LedgerScreen" }, { testTag: "AddTransactionButton" }]));
const txnAmountField = extract(s =>
  s.ax.find([{ testTag: "AddTransactionScreen" }, { testTag: "TxnAmountField" }]));
const txnSubmit = extract(s =>
  s.ax.find([{ testTag: "AddTransactionScreen" }, { testTag: "TxnSubmit" }]));
const accountCards = extract(s =>
  s.ax.findAll([{ testTag: "HomeScreen" }, { testTag: "AccountCard" }]));
const backButton = extract(s => s.ax.find({ testTag: "BackButton" }));

// Property 1: every newly-appearing account starts with balance === 0.
// Identity is by visible name. Guard against navigation transitions where
// accounts vanish from the visible tree.
const newAccountBalanceIsZero = always(
  next(() => {
    const prev = accounts.previous ?? [];
    const curr = accounts.current;
    if (prev.length === 0 || curr.length === 0) return true;
    const prevNames = new Set(prev.map(a => a.name));
    return curr.filter(a => !prevNames.has(a.name)).every(a => a.balance === 0);
  })
);

// Property 2: a newly-added ledger row changes the ledger balance by exactly its signed amount.
const newTxnChangesBalance = always(
  now(() => route.current === "ledger").implies(
    next(() => {
      const prev = ledgerRows.previous ?? [];
      const curr = ledgerRows.current;
      if (curr.length !== prev.length + 1) return true;
      const prevKeys = new Set(prev.map(r => r.key));
      const added = curr.find(r => !prevKeys.has(r.key));
      if (!added) return true;
      const delta = ledgerBalance.current - (ledgerBalance.previous ?? 0);
      return delta === added.signed && delta !== 0;
    })
  )
);

const DEMO_EMAIL = "demo@folio.app";
const DEMO_PASSWORD = "ledger123";

// Login: drive the form via focus state read from the native focused="true" attr.
const login = actions(() => {
  if (loggedIn.current) return [];
  const focus = focusedFieldTag.current;
  if (focus === "LoginPassword") {
    const submit = loginSubmit.current;
    return submit ? [Tap({ on: submit })] : [];
  }
  if (focus === "LoginEmail") {
    const pwd = loginPasswordField.current;
    return pwd ? [InputText({ into: pwd, text: DEMO_PASSWORD })] : [];
  }
  const email = loginEmailField.current;
  return email ? [InputText({ into: email, text: DEMO_EMAIL })] : [];
});

const accountNames = from(["Checking", "Savings", "Travel", "Emergency Fund", "Investments"]);

const addAccount = whenRoute(route, ["home", "add-account"], () => {
  if (route.current === "home") {
    const btn = addAccountButton.current;
    return btn ? [Tap({ on: btn })] : [];
  }
  const field = accountNameField.current;
  const submit = addAccountSubmit.current;
  const opts = [];
  if (field) opts.push(InputText({ into: field, text: accountNames.generate() }));
  if (submit) opts.push(Tap({ on: submit }));
  return opts;
});

const amounts = from(["10", "50", "25", "100", "5"]);

const addTxn = whenRoute(route, ["home", "ledger", "add-transaction"], () => {
  if (route.current === "home") {
    const cards = accountCards.current;
    if (cards.length === 0) return [];
    return [Tap({ on: from(cards).generate() })];
  }
  if (route.current === "ledger") {
    const btn = addTxnButton.current;
    return btn ? [Tap({ on: btn })] : [];
  }
  const field = txnAmountField.current;
  const submit = txnSubmit.current;
  const opts = [];
  if (field) opts.push(InputText({ into: field, text: amounts.generate() }));
  if (submit) opts.push(Tap({ on: submit }));
  return opts;
});

const back = actions(() => {
  const btn = backButton.current;
  return btn ? [Tap({ on: btn })] : [];
});

export const properties = {
  newAccountBalanceIsZero,
  newTxnChangesBalance,
};

export const setup = login;

export const actionsRoot = weighted(
  [50, addAccount],
  [40, addTxn],
  [10, back],
);

(globalThis as { actions?: unknown; properties?: unknown; setup?: unknown }).actions = actionsRoot;
(globalThis as { properties?: unknown }).properties = properties;
(globalThis as { setup?: unknown }).setup = setup;
