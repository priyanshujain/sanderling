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
} from "@uatu/spec";
import { noLogcatErrors, noUncaughtExceptions } from "@uatu/spec/defaults/properties";

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
const authStatus = extract<string>(
  (state) => (state.snapshots.auth_status as string) ?? "",
);
const route = extract<string>(
  (state) => (state.snapshots.route as string) ?? "",
);
const accounts = extract<AccountSnapshot[]>(
  (state) => (state.snapshots.accounts as AccountSnapshot[]) ?? [],
);
const totalBalance = extract<number>(
  (state) => (state.snapshots.total_balance as number) ?? 0,
);
const accountCount = extract<number>(
  (state) => (state.snapshots.account_count as number) ?? 0,
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
const txnFormAccountId = extract<string | null>(
  (state) => (state.snapshots.txn_form_account_id as string | null) ?? null,
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
const loginEmailValue = extract<string>(
  (state) => (state.snapshots.login_email_value as string) ?? "",
);
const loginPasswordLength = extract<number>(
  (state) => (state.snapshots.login_password_length as number) ?? 0,
);
const accountNameInput = extract<string>(
  (state) => (state.snapshots.account_name_input as string) ?? "",
);
const txnAmountInput = extract<string>(
  (state) => (state.snapshots.txn_amount_input as string) ?? "",
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
const anyAccountCard = extract((state) => state.ax.find("descPrefix:account_card:"));

const accountCountNonNegative = always(() => accountCount.current >= 0);

const onHome = () => route.current === "home";
const onLedger = () =>
  route.current === "ledger" || route.current === "add-transaction";
const isInteger = (n: number) => Number.isFinite(n) && Math.floor(n) === n;

const totalBalanceMatchesAccounts = always(
  now(onHome).implies(
    now(() => {
      const sum = accounts.current.reduce((acc, a) => acc + a.balance, 0);
      return sum === totalBalance.current;
    }),
  ),
);

const ledgerBalanceMatchesRows = always(
  now(onLedger).implies(
    now(() => {
      const sum = ledgerRows.current.reduce((acc, r) => acc + r.signed, 0);
      return sum === ledgerBalance.current;
    }),
  ),
);

const ledgerRowsWellFormed = always(() => {
  for (const row of ledgerRows.current) {
    if (row.type !== "credit" && row.type !== "debit") return false;
    if (!(row.amount > 0)) return false;
    const expected = row.type === "credit" ? row.amount : -row.amount;
    if (row.signed !== expected) return false;
  }
  return true;
});

const balancesAreIntegerCents = always(() => {
  if (!isInteger(totalBalance.current)) return false;
  if (!isInteger(ledgerBalance.current)) return false;
  for (const a of accounts.current) if (!isInteger(a.balance)) return false;
  for (const r of ledgerRows.current) {
    if (!isInteger(r.amount) || !isInteger(r.signed)) return false;
  }
  return true;
});

const accountCountMatchesList = always(
  () => accountCount.current === accounts.current.length,
);

const ledgerCountMatchesRows = always(
  now(onLedger).implies(
    now(() => {
      const active = activeAccountId.current;
      if (active === null) return true;
      const fromAccounts = accounts.current.find((a) => a.id === active);
      if (!fromAccounts) return true;
      return fromAccounts.txnCount === ledgerRows.current.length;
    }),
  ),
);

const zeroTxnsMeansZeroBalance = always(() => {
  for (const a of accounts.current) {
    if (a.txnCount === 0 && a.balance !== 0) return false;
  }
  return true;
});

const noOrphanTransactions = always(() => {
  const active = activeAccountId.current;
  if (active === null) return ledgerRows.current.length === 0;
  return ledgerRows.current.every((r) => r.accountId === active);
});

const uniqueAccountNames = always(() => {
  const seen = new Set<string>();
  for (const a of accounts.current) {
    const key = a.name.trim().toLowerCase();
    if (seen.has(key)) return false;
    seen.add(key);
  }
  return true;
});

const accountingInvariants = {
  totalBalanceMatchesAccounts,
  ledgerBalanceMatchesRows,
  ledgerRowsWellFormed,
  balancesAreIntegerCents,
  accountCountMatchesList,
  ledgerCountMatchesRows,
  zeroTxnsMeansZeroBalance,
  noOrphanTransactions,
  uniqueAccountNames,
};

const accountsOnlyGrow = always(
  now(() => true).implies(
    next(() => accounts.current.length >= (accounts.previous?.length ?? 0)),
  ),
);

const ledgerOnlyGrowsPerAccount = always(
  now(() => activeAccountId.current !== null).implies(
    next(() => {
      if (activeAccountId.current !== activeAccountId.previous) return true;
      return ledgerRows.current.length >= (ledgerRows.previous?.length ?? 0);
    }),
  ),
);

const authStatusIsKnown = always(
  () => authStatus.current === "logged-in" || authStatus.current === "logged-out",
);

const routeIsKnown = always(() => {
  const r = route.current;
  return (
    r === "login" ||
    r === "home" ||
    r === "add-account" ||
    r === "ledger" ||
    r === "add-transaction"
  );
});

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

const stateMachine = {
  accountsOnlyGrow,
  ledgerOnlyGrowsPerAccount,
  authStatusIsKnown,
  routeIsKnown,
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

const DEMO_EMAIL = "demo@ledger.app";
const DEMO_PASSWORD = "ledger123";

const loginHelper = actions(() => {
  if (loggedIn.current) return [];
  const focus = focusedInput.current;
  const email = loginEmailField.current;
  const password = loginPasswordField.current;
  const submit = loginSubmitButton.current;
  const emailFilled = loginEmailValue.current === DEMO_EMAIL;
  const passwordFilled = loginPasswordLength.current >= DEMO_PASSWORD.length;

  if (emailFilled && passwordFilled) {
    return submit ? [Tap({ on: submit })] : [];
  }
  if (focus === "login_password") {
    if (!passwordFilled && password) {
      return [InputText({ into: password, text: DEMO_PASSWORD })];
    }
    return submit ? [Tap({ on: submit })] : [];
  }
  if (focus === "login_email") {
    if (!emailFilled && email) {
      return [InputText({ into: email, text: DEMO_EMAIL })];
    }
    return password ? [Tap({ on: password })] : [];
  }
  if (!emailFilled) return email ? [Tap({ on: email })] : [];
  return password ? [Tap({ on: password })] : [];
});

const badCredentials = from([
  { email: "nobody@nowhere.dev", password: "wrong" },
  { email: "demo@ledger.app", password: "not-the-password" },
  { email: "", password: "" },
  { email: "   ", password: "x" },
]);

const adversarialLogin = actions(() => {
  if (loggedIn.current) return [];
  const email = loginEmailField.current;
  const password = loginPasswordField.current;
  const submit = loginSubmitButton.current;
  if (!email || !password || !submit) return [];
  const creds = badCredentials.generate();
  return [
    InputText({ into: email, text: creds.email }),
    InputText({ into: password, text: creds.password }),
    Tap({ on: submit }),
  ];
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
  if (accountNameInput.current.trim().length > 0) return [];
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
  const card = anyAccountCard.current;
  return card ? [Tap({ on: card })] : [];
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
  if (txnAmountInput.current.length > 0) return [];
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
  accountCountNonNegative,
  ...accountingInvariants,
  ...stateMachine,
  ...liveness,
  noUncaughtExceptions,
  noLogcatErrors,
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
