import {
  actions,
  always,
  extract,
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

const noopAction = actions(() => []);

export const properties = {
  accountCountNonNegative,
  noUncaughtExceptions,
  noLogcatErrors,
};

export const actionsRoot = weighted(
  [1, noopAction],
  [4, taps],
  [2, swipes],
  [2, waitOnce],
  [2, pressKey],
);

(globalThis as { actions?: unknown; properties?: unknown }).actions = actionsRoot;
(globalThis as { properties?: unknown }).properties = properties;
