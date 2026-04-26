import {
  InputText,
  Tap,
  actions,
  always,
  extract,
  from,
  next,
  now,
  weighted,
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

// Account cards on Home: identified by visible name (the first Text node inside).
// Each card carries an AccountBalance Text with the formatted dollar value.
const accounts = extract<Account[]>(s => {
  const home = s.ax.find({ testTag: "HomeScreen" });
  if (!home) return [];
  return home.findAll({ testTag: "AccountCard" }).map(card => {
    const texts = card.findAll({}).map(c => c.text).filter((t): t is string => !!t);
    const balance = parseDollarCents(card.find({ testTag: "AccountBalance" })?.text);
    const name = texts.find(t => !t.startsWith("$") && !/^\d/.test(t) && t !== "transaction" && t !== "transactions") ?? "";
    return { name, balance };
  });
});

// Ledger rows: identified by the row's text contents joined together.
const ledgerRows = extract<LedgerRow[]>(s => {
  const ledger = s.ax.find({ testTag: "LedgerScreen" });
  if (!ledger) return [];
  return ledger.findAll({ testTag: "LedgerRow" }).map(row => {
    const texts = row.findAll({}).map(c => c.text).filter((t): t is string => !!t);
    const signed = parseDollarCents(row.find({ testTag: "TxnAmount" })?.text);
    return { key: texts.join("|"), signed };
  });
});

const ledgerBalance = extract(s =>
  parseDollarCents(s.ax.find({ testTag: "LedgerBalance" })?.text));

// Focus uses the native focused="true" attribute. We surface whichever
// stable identifier the focused element carries (testTag, label, etc).
const focusedFieldTag = extract(s => {
  const f = s.ax.find({ focused: "true" });
  if (!f) return null;
  return f.attrs?.["resource-id"] ?? f.attrs?.["accessibilityIdentifier"] ?? f.attrs?.["identifier"] ?? null;
});

const loginEmailField = extract(s =>
  s.ax.find({ testTag: "LoginScreen" })?.find({ testTag: "LoginEmail" }));
const loginPasswordField = extract(s =>
  s.ax.find({ testTag: "LoginScreen" })?.find({ testTag: "LoginPassword" }));
const loginSubmit = extract(s =>
  s.ax.find({ testTag: "LoginScreen" })?.find({ testTag: "LoginSubmit" }));
const addAccountButton = extract(s =>
  s.ax.find({ testTag: "HomeScreen" })?.find({ testTag: "AddAccountButton" }));
const accountNameField = extract(s =>
  s.ax.find({ testTag: "AddAccountScreen" })?.find({ testTag: "AccountNameField" }));
const addAccountSubmit = extract(s =>
  s.ax.find({ testTag: "AddAccountScreen" })?.find({ testTag: "AddAccountSubmit" }));
const addTxnButton = extract(s =>
  s.ax.find({ testTag: "LedgerScreen" })?.find({ testTag: "AddTransactionButton" }));
const txnAmountField = extract(s =>
  s.ax.find({ testTag: "AddTransactionScreen" })?.find({ testTag: "TxnAmountField" }));
const txnSubmit = extract(s =>
  s.ax.find({ testTag: "AddTransactionScreen" })?.find({ testTag: "TxnSubmit" }));
const accountCards = extract(s =>
  s.ax.find({ testTag: "HomeScreen" })?.findAll({ testTag: "AccountCard" }) ?? []);
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

const addAccount = actions(() => {
  if (!loggedIn.current) return [];
  if (route.current === "home") {
    const btn = addAccountButton.current;
    return btn ? [Tap({ on: btn })] : [];
  }
  if (route.current === "add-account") {
    const field = accountNameField.current;
    const submit = addAccountSubmit.current;
    const opts = [];
    if (field) opts.push(InputText({ into: field, text: accountNames.generate() }));
    if (submit) opts.push(Tap({ on: submit }));
    return opts;
  }
  return [];
});

const amounts = from(["10", "50", "25", "100", "5"]);

const addTxn = actions(() => {
  if (!loggedIn.current) return [];
  if (route.current === "home") {
    const cards = accountCards.current;
    if (cards.length === 0) return [];
    return [Tap({ on: cards[Math.floor(Math.random() * cards.length)] })];
  }
  if (route.current === "ledger") {
    const btn = addTxnButton.current;
    return btn ? [Tap({ on: btn })] : [];
  }
  if (route.current === "add-transaction") {
    const field = txnAmountField.current;
    const submit = txnSubmit.current;
    const opts = [];
    if (field) opts.push(InputText({ into: field, text: amounts.generate() }));
    if (submit) opts.push(Tap({ on: submit }));
    return opts;
  }
  return [];
});

const back = actions(() => {
  const btn = backButton.current;
  return btn ? [Tap({ on: btn })] : [];
});

export const properties = {
  newAccountBalanceIsZero,
  newTxnChangesBalance,
};

export const actionsRoot = weighted(
  [50, login],
  [30, addAccount],
  [30, addTxn],
  [5, back],
);

(globalThis as { actions?: unknown; properties?: unknown }).actions = actionsRoot;
(globalThis as { properties?: unknown }).properties = properties;
