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
import { defaultActions } from "@sanderling/spec/defaults";

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

// Android's IME (soft keyboard) ships a focused FrameLayout in its own window
// that traverses ahead of the app's EditText; constrain to editable so the
// login flow sees LoginEmail / LoginPassword, not the keyboard chrome.
const focusedFieldTag = extract(s =>
  s.ax.findAll({ focused: true }).find(n => n.editable)?.id ?? null);

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

// Property 2: no single user action grows the ledger by more than one row.
// The ledger is only observable on LedgerScreen, so carry the last seen count
// across non-ledger steps. Otherwise a plain home -> ledger navigation would
// look like a delta of N from 0 and false-trigger the property.
const ledgerRowsSeen = extract<number>((s): number => {
  // Only refresh when the ledger screen owns the foreground; transitional
  // states (AddTransaction overlay during navigation) hide LedgerRow nodes
  // and would falsely zero the count.
  const onLedgerOnly =
    s.ax.find({ testTag: "LedgerScreen" }) != null &&
    s.ax.find({ testTag: "AddTransactionScreen" }) == null;
  if (!onLedgerOnly) return ledgerRowsSeen.previous ?? 0;
  return s.ax.findAll([{ testTag: "LedgerScreen" }, { testTag: "LedgerRow" }]).length;
});

const noDuplicateTxnPerStep = always(
  next(() => {
    const prev = ledgerRowsSeen.previous ?? 0;
    const curr = ledgerRowsSeen.current;
    return curr - prev <= 1;
  })
);

// Property 3: a newly-added ledger row changes the ledger balance by exactly its signed amount.
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

export const properties = {
  newAccountBalanceIsZero,
  noDuplicateTxnPerStep,
  newTxnChangesBalance,
};

export const setup = login;

// Targeted depth (addAccount / addTxn) drives the deep flows; defaultActions
// adds breadth so the fuzzer wanders the whole app and types edge-case values
// into every field, stressing the balance invariants above.
export const actionsRoot = weighted(
  [50, addAccount],
  [30, addTxn],
  [20, defaultActions],
);

(globalThis as { actions?: unknown; properties?: unknown; setup?: unknown }).actions = actionsRoot;
(globalThis as { properties?: unknown }).properties = properties;
(globalThis as { setup?: unknown }).setup = setup;
