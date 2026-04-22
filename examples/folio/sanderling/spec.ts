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
  pressKey,
  swipes,
  taps,
  waitOnce,
  weighted,
} from "@sanderling/spec";
import { noUncaughtExceptions } from "@sanderling/spec/defaults/properties";

interface AccountSnapshot {
  id: string;
  name: string;
  balance: number;
  txnCount: number;
}

interface LedgerRow {
  id: string;
  accountId: string;
  type: "credit" | "debit";
  amount: number;
  signed: number;
}

const loggedIn = extract<boolean>(
  (state) => (state.snapshots.logged_in as boolean) ?? false,
);
const route = extract<string>(
  (state) => (state.snapshots.screen as string) ?? "",
);
const accounts = extract<AccountSnapshot[]>(
  (state) => (state.snapshots.accounts as AccountSnapshot[]) ?? [],
);
const totalBalance = extract<number>(
  (state) => (state.snapshots.total_balance as number) ?? 0,
);
const activeAccountId = extract<string | null>(
  (state) => (state.snapshots.active_account_id as string | null) ?? null,
);
const ledgerRows = extract<LedgerRow[]>(
  (state) => (state.snapshots.ledger_rows as LedgerRow[]) ?? [],
);
const ledgerBalance = extract<number>(
  (state) => (state.snapshots.ledger_balance as number) ?? 0,
);
const focusedInput = extract<string | null>(
  (state) => (state.snapshots.focused_input as string | null) ?? null,
);
const txnFormType = extract<string | null>(
  (state) => (state.snapshots.txn_form_type as string | null) ?? null,
);
const loginError = extract<string>(
  (state) => (state.snapshots.login_error as string) ?? "",
);
const addAccountError = extract<string>(
  (state) => (state.snapshots.add_account_error as string) ?? "",
);
const txnError = extract<string>(
  (state) => (state.snapshots.txn_error as string) ?? "",
);
const loginEmailField = extract((state) => state.ax.find("desc:login_email"));
const loginPasswordField = extract((state) => state.ax.find("desc:login_password"));
const loginSubmitButton = extract((state) => state.ax.find("desc:login_submit"));
const addAccountButton = extract((state) => state.ax.find("desc:add_account_button"));
const logoutButton = extract((state) => state.ax.find("desc:logout_button"));
const accountNameField = extract((state) => state.ax.find("desc:account_name_field"));
const addAccountSubmit = extract((state) => state.ax.find("desc:add_account_submit"));
const addTxnButton = extract((state) => state.ax.find("desc:add_txn_button"));
const txnAmountField = extract((state) => state.ax.find("desc:txn_amount"));
const txnNoteField = extract((state) => state.ax.find("desc:txn_note"));
const txnCredit = extract((state) => state.ax.find("desc:txn_credit"));
const txnDebit = extract((state) => state.ax.find("desc:txn_debit"));
const txnSubmit = extract((state) => state.ax.find("desc:txn_submit"));
const backButton = extract((state) => state.ax.find("desc:Back"));
const allAccountCards = extract((state) =>
  state.ax.findAll("descPrefix:account_card:"),
);

const balanceMatchesTransactionDelta = always(
  now(() => activeAccountId.current !== null).implies(
    next(() => {
      const prevActive = activeAccountId.previous;
      if (prevActive === null || prevActive === undefined) return true;
      if (prevActive !== activeAccountId.current) return true;
      const prevRows = ledgerRows.previous ?? [];
      const curRows = ledgerRows.current;
      if (curRows.length !== prevRows.length + 1) return true;
      const prevIds = new Set(prevRows.map((r) => r.id));
      const added = curRows.filter((r) => !prevIds.has(r.id));
      if (added.length !== 1) return true;
      const delta = ledgerBalance.current - (ledgerBalance.previous ?? 0);
      return delta === added[0].signed;
    }),
  ),
);

const totalEqualsSumOfAccounts = always(() => {
  const sum = accounts.current.reduce((acc, a) => acc + a.balance, 0);
  return sum === totalBalance.current;
});

const balanceChangeRequiresActiveAccount = always(
  now(() => true).implies(
    next(() => {
      const prevAccounts = accounts.previous ?? [];
      const prevActive = activeAccountId.previous ?? null;
      for (const cur of accounts.current) {
        const prev = prevAccounts.find((a) => a.id === cur.id);
        if (!prev) continue;
        if (cur.balance !== prev.balance && prevActive !== cur.id) return false;
      }
      return true;
    }),
  ),
);

const duplicateAccountNamesRejected = always(() => {
  const seen = new Set<string>();
  for (const a of accounts.current) {
    const key = a.name.trim().toLowerCase();
    if (seen.has(key)) return false;
    seen.add(key);
  }
  return true;
});

const domainInvariants = {
  balanceMatchesTransactionDelta,
  totalEqualsSumOfAccounts,
  balanceChangeRequiresActiveAccount,
  duplicateAccountNamesRejected,
};

const loggedInLeavesLogin = always(
  now(() => loggedIn.current).implies(
    eventually(() => route.current !== "login").within(3, "seconds"),
  ),
);

const loggedOutReachesLogin = always(
  now(() => !loggedIn.current).implies(
    eventually(() => route.current === "login").within(3, "seconds"),
  ),
);

const authRouting = {
  loggedInLeavesLogin,
  loggedOutReachesLogin,
};

const loginReachable = eventually(() => loggedIn.current).within(90, "seconds");
const accountCreationReachable = eventually(
  () => accounts.current.length > 0,
).within(180, "seconds");
const someTransactionExists = eventually(() =>
  accounts.current.some((a) => a.txnCount > 0),
).within(300, "seconds");

const loginErrorClears = always(
  now(() => loginError.current !== "").implies(
    eventually(() => loginError.current === "").within(30, "seconds"),
  ),
);
const addAccountErrorClears = always(
  now(() => addAccountError.current !== "").implies(
    eventually(() => addAccountError.current === "").within(30, "seconds"),
  ),
);
const txnErrorClears = always(
  now(() => txnError.current !== "").implies(
    eventually(() => txnError.current === "").within(30, "seconds"),
  ),
);

const liveness = {
  loginReachable,
  accountCreationReachable,
  someTransactionExists,
  loginErrorClears,
  addAccountErrorClears,
  txnErrorClears,
};

const DEMO_EMAIL = "demo@folio.app";
const DEMO_PASSWORD = "ledger123";

const loginHelper = actions(() => {
  if (loggedIn.current) return [];
  const focus = focusedInput.current;
  const email = loginEmailField.current;
  const password = loginPasswordField.current;
  const submit = loginSubmitButton.current;

  if (focus === "login_password") {
    return submit ? [Tap({ on: submit })] : [];
  }
  if (focus === "login_email") {
    return password ? [InputText({ into: password, text: DEMO_PASSWORD })] : [];
  }
  return email ? [InputText({ into: email, text: DEMO_EMAIL })] : [];
});

const adversarialLogin = actions(() => {
  if (loggedIn.current) return [];
  if (focusedInput.current !== null) return [];
  const submit = loginSubmitButton.current;
  if (!submit) return [];
  return [Tap({ on: submit })];
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
  if (route.current !== "add-account") return [];
  const field = accountNameField.current;
  if (!field) return [];
  return [InputText({ into: field, text: accountNameSampler.generate() })];
});

const submitAddAccount = actions(() => {
  if (route.current !== "add-account") return [];
  const submit = addAccountSubmit.current;
  return submit ? [Tap({ on: submit })] : [];
});

const openAddAccount = actions(() => {
  if (route.current !== "home") return [];
  const button = addAccountButton.current;
  return button ? [Tap({ on: button })] : [];
});

const openRandomAccount = actions(() => {
  if (route.current !== "home") return [];
  const cards = allAccountCards.current;
  if (cards.length === 0) return [];
  const card = cards[Math.floor(Math.random() * cards.length)];
  return [Tap({ on: card })];
});

const logoutAction = actions(() => {
  if (route.current !== "home") return [];
  const button = logoutButton.current;
  return button ? [Tap({ on: button })] : [];
});

const goBack = actions(() => {
  const button = backButton.current;
  return button ? [Tap({ on: button })] : [];
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
  if (route.current !== "add-transaction") return [];
  const field = txnAmountField.current;
  if (!field) return [];
  return [InputText({ into: field, text: amountSampler.generate() })];
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
  if (route.current !== "add-transaction") return [];
  const field = txnNoteField.current;
  if (!field) return [];
  return [InputText({ into: field, text: noteSampler.generate() })];
});

const toggleTxnType = actions(() => {
  if (route.current !== "add-transaction") return [];
  const current = txnFormType.current;
  const target = current === "credit" ? txnDebit.current : txnCredit.current;
  return target ? [Tap({ on: target })] : [];
});

const submitTxn = actions(() => {
  if (route.current !== "add-transaction") return [];
  const submit = txnSubmit.current;
  return submit ? [Tap({ on: submit })] : [];
});

const openAddTxn = actions(() => {
  if (route.current !== "ledger") return [];
  const button = addTxnButton.current;
  return button ? [Tap({ on: button })] : [];
});

export const properties = {
  ...domainInvariants,
  ...authRouting,
  ...liveness,
  noUncaughtExceptions,
};

export const actionsRoot = weighted(
  [30, loginHelper],
  [2, adversarialLogin],
  [18, typeAccountName],
  [14, submitAddAccount],
  [18, typeAmount],
  [8, typeNote],
  [6, toggleTxnType],
  [16, submitTxn],
  [14, openAddAccount],
  [14, openRandomAccount],
  [12, openAddTxn],
  [6, goBack],
  [1, logoutAction],
  [4, taps],
  [2, swipes],
  [2, waitOnce],
  [2, pressKey],
);

(globalThis as { actions?: unknown; properties?: unknown }).actions = actionsRoot;
(globalThis as { properties?: unknown }).properties = properties;
