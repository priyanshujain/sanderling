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
  id: string;
  balance: number;
}

interface LedgerRow {
  id: string;
  signed: number;
}

function parseAccount(desc: string): Account {
  const parts = desc.split(":");
  return { id: parts[1], balance: Number(parts[2]) };
}

function parseLedgerRow(desc: string): LedgerRow {
  const parts = desc.split(":");
  return { id: parts[1], signed: Number(parts[2]) };
}

function parseCents(desc: string | null | undefined): number {
  if (!desc) return 0;
  const parts = desc.split(":");
  return Number(parts[1]) || 0;
}

// Route and auth state derived from screen root nodes
const loggedIn = extract(s => s.ax.find("desc:LoginScreen") == null);
const route = extract<string | null>(s => {
  if (s.ax.find("desc:LoginScreen")) return "login";
  if (s.ax.find("desc:HomeScreen")) return "home";
  if (s.ax.find("desc:AddAccountScreen")) return "add-account";
  if (s.ax.find("desc:LedgerScreen")) return "ledger";
  if (s.ax.find("desc:AddTransactionScreen")) return "add-transaction";
  return null;
});

// All element lookups scoped through their screen root
const accounts = extract(s => s.ax.findAll("desc:HomeScreen > descPrefix:account:")
  .map(el => parseAccount(el.desc)));
const ledgerRows = extract(s => s.ax.findAll("desc:LedgerScreen > descPrefix:ledger_row:")
  .map(el => parseLedgerRow(el.desc)));
const ledgerBalance = extract(s =>
  parseCents(s.ax.find("desc:LedgerScreen > descPrefix:ledger_balance:")?.desc));
const activeAccountId = extract(s =>
  s.ax.find("desc:LedgerScreen > descPrefix:active_account:")?.desc?.split(":")[1] ?? null);

// focusedInput lives in the app root (not inside any screen), so unscoped
const focusedInput = extract(s =>
  s.ax.find("descPrefix:focused_input:")?.desc?.split(":")[1] ?? null);

const loginEmailField = extract(s => s.ax.find("desc:LoginScreen > desc:login_email"));
const loginPasswordField = extract(s => s.ax.find("desc:LoginScreen > desc:login_password"));
const loginSubmit = extract(s => s.ax.find("desc:LoginScreen > desc:login_submit"));
const addAccountButton = extract(s => s.ax.find("desc:HomeScreen > desc:add_account_button"));
const accountNameField = extract(s => s.ax.find("desc:AddAccountScreen > desc:account_name_field"));
const addAccountSubmit = extract(s => s.ax.find("desc:AddAccountScreen > desc:add_account_submit"));
const addTxnButton = extract(s => s.ax.find("desc:LedgerScreen > desc:add_txn_button"));
const txnAmountField = extract(s => s.ax.find("desc:AddTransactionScreen > desc:txn_amount"));
const txnSubmit = extract(s => s.ax.find("desc:AddTransactionScreen > desc:txn_submit"));
const accountCards = extract(s => s.ax.findAll("desc:HomeScreen > descPrefix:account:"));
const backButton = extract(s => s.ax.find("desc:Back"));

// Property 1: every new account starts with balance === 0
// Guard: only check when accounts were visible in the previous step too.
// Without this, navigating away from HomeScreen (accounts=[]) then back
// makes every account look "new", causing false positives on pre-existing balances.
const newAccountBalanceIsZero = always(
  next(() => {
    const prev = accounts.previous ?? [];
    const curr = accounts.current;
    if (prev.length === 0 || curr.length === 0) return true;
    const prevIds = new Set(prev.map(a => a.id));
    const newAccounts = curr.filter(a => !prevIds.has(a.id));
    return newAccounts.every(a => a.balance === 0);
  })
);

// Property 2: every new transaction changes the account ledger balance by exactly its signed amount
const newTxnChangesBalance = always(
  now(() => activeAccountId.current !== null).implies(
    next(() => {
      const prevRows = ledgerRows.previous ?? [];
      const curRows = ledgerRows.current;
      if (curRows.length !== prevRows.length + 1) return true;
      const prevIds = new Set(prevRows.map(r => r.id));
      const added = curRows.find(r => !prevIds.has(r.id));
      if (!added) return true;
      const delta = ledgerBalance.current - (ledgerBalance.previous ?? 0);
      return delta === added.signed && delta !== 0;
    })
  )
);

const DEMO_EMAIL = "demo@folio.app";
const DEMO_PASSWORD = "ledger123";

// Login if not already in - step by step based on which field has focus
const login = actions(() => {
  if (loggedIn.current) return [];
  const focus = focusedInput.current;
  if (focus === "login_password") {
    const submit = loginSubmit.current;
    return submit ? [Tap({ on: submit })] : [];
  }
  if (focus === "login_email") {
    const pwd = loginPasswordField.current;
    return pwd ? [InputText({ into: pwd, text: DEMO_PASSWORD })] : [];
  }
  const email = loginEmailField.current;
  return email ? [InputText({ into: email, text: DEMO_EMAIL })] : [];
});

const accountNames = from(["Checking", "Savings", "Travel", "Emergency Fund", "Investments"]);

// Add an account: home -> tap add -> type name -> submit
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

// Add a transaction: home -> tap account card -> tap add txn -> type amount -> submit
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
