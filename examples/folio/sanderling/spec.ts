import {
  InputText,
  Tap,
  actions,
  always,
  extract,
  from,
  integers,
  next,
  weighted,
  whenRoute,
} from "@sanderling/spec";
import { defaultActions, doubleTaps } from "@sanderling/spec/defaults";
import {
  computeHomeTotalBalance,
  parseTypedAmount,
  submitChangesBalanceByTypedAmount,
} from "./predicates";

interface Account {
  name: string;
  balance: number;
}

// Parses formatCents output like "$5.00", "-$1,234.56", "+$0.50" back to integer cents.
function parseDollarCents(text: string | undefined): number {
  if (!text) return 0;
  const sign = text.startsWith("-") ? -1 : 1;
  const digits = text.replace(/[^0-9]/g, "");
  return digits ? sign * parseInt(digits, 10) : 0;
}

// Route detection via testTag (resource-id on Android, accessibilityIdentifier on iOS)
const loggedIn = extract("loggedIn", s => s.ax.find({ testTag: "LoginScreen" }) == null);
const route = extract<string | null>("route", s => {
  if (s.ax.find({ testTag: "LoginScreen" })) return "login";
  if (s.ax.find({ testTag: "AddAccountScreen" })) return "add-account";
  if (s.ax.find({ testTag: "AddTransactionScreen" })) return "add-transaction";
  if (s.ax.find({ testTag: "LedgerScreen" })) return "ledger";
  if (s.ax.find({ testTag: "HomeScreen" })) return "home";
  return null;
});

// Account cards on Home: identity is the AccountName text; balance comes from AccountBalance.
const accounts = extract<Account[]>("accounts", s =>
  s.ax.findAll([{ testTag: "HomeScreen" }, { testTag: "AccountCard" }]).map(card => ({
    name: card.find({ testTag: "AccountName" })?.text ?? "",
    balance: parseDollarCents(card.find({ testTag: "AccountBalance" })?.text),
  })));

// Total balance: sum of AccountCard balances visible on Home. The carrier
// deliberately tracks only the Home multi-account total. Ledger's
// LedgerBalance is a single-account number on a different scale and would
// corrupt cross-screen comparisons if mixed in. Off-Home steps carry forward
// the last-seen Home sum so `previous` and `current` stay on the same scale.
let lastHomeTotal = 0;
const totalBalance = extract("totalBalance", s => {
  const cards = s.ax.findAll([{ testTag: "HomeScreen" }, { testTag: "AccountCard" }]);
  const cardBalanceTexts = cards.map(c => c.find({ testTag: "AccountBalance" })?.text);
  lastHomeTotal = computeHomeTotalBalance({ cardBalanceTexts, previousCarrier: lastHomeTotal });
  return lastHomeTotal;
});

const lastAction = extract("lastAction", s => s.lastAction);

const loginEmailField = extract("loginEmailField", s =>
  s.ax.find([{ testTag: "LoginScreen" }, { testTag: "LoginEmail" }]));
const loginPasswordField = extract("loginPasswordField", s =>
  s.ax.find([{ testTag: "LoginScreen" }, { testTag: "LoginPassword" }]));
const loginSubmit = extract("loginSubmit", s =>
  s.ax.find([{ testTag: "LoginScreen" }, { testTag: "LoginSubmit" }]));
const addAccountButton = extract("addAccountButton", s =>
  s.ax.find([{ testTag: "HomeScreen" }, { testTag: "AddAccountButton" }]));
const accountNameField = extract("accountNameField", s =>
  s.ax.find([{ testTag: "AddAccountScreen" }, { testTag: "AccountNameField" }]));
const addAccountSubmit = extract("addAccountSubmit", s =>
  s.ax.find([{ testTag: "AddAccountScreen" }, { testTag: "AddAccountSubmit" }]));
const addTxnButton = extract("addTxnButton", s =>
  s.ax.find([{ testTag: "LedgerScreen" }, { testTag: "AddTransactionButton" }]));
const txnAmountField = extract("txnAmountField", s =>
  s.ax.find([{ testTag: "AddTransactionScreen" }, { testTag: "TxnAmountField" }]));
const txnSubmit = extract("txnSubmit", s =>
  s.ax.find([{ testTag: "AddTransactionScreen" }, { testTag: "TxnSubmit" }]));
const accountCards = extract("accountCards", s =>
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

// Property 2: a tap on TxnSubmit must move the total balance by exactly the
// typed amount. A double-submit lands two transactions, so the balance shifts
// by twice the typed amount and the check fires. The route gate inside the
// predicate skips off-Home landings where totalBalance.current is the carrier.
const submitMovesBalanceByTypedAmount = always(
  next(() =>
    submitChangesBalanceByTypedAmount({
      route: route.current,
      lastAction: lastAction.current,
      typedAmount: parseTypedAmount(txnAmountField.previous?.text),
      prevTotalBalance: totalBalance.previous ?? 0,
      currTotalBalance: totalBalance.current,
    }),
  ),
);

const DEMO_EMAIL = "demo@folio.app";
const DEMO_PASSWORD = "ledger123";

// Login: drive the form by reading what's currently in each field, not by
// inferring intent from which one happens to be focused. A focus-driven
// approach loops forever if we re-enter the login screen with the password
// field already focused (e.g. after an exception bounces us back from
// another screen): it would type the password then tap submit with an
// empty email field, see no progress, and repeat indefinitely.
const login = actions(() => {
  if (loggedIn.current) return [];
  const email = loginEmailField.current;
  const pwd = loginPasswordField.current;
  if (email && !email.text) return [InputText({ into: email, text: DEMO_EMAIL })];
  if (pwd && !pwd.text) return [InputText({ into: pwd, text: DEMO_PASSWORD })];
  const submit = loginSubmit.current;
  return submit ? [Tap({ on: submit })] : [];
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

const amounts = integers().between(1, 500);

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
  if (field) opts.push(InputText({ into: field, text: String(amounts.generate()) }));
  if (submit) opts.push(Tap({ on: submit }));
  return opts;
});

export const properties = {
  newAccountBalanceIsZero,
  submitMovesBalanceByTypedAmount,
};

export const setup = login;

// Weights declare testing intent. The transaction chain is the focus: it is
// the deepest flow and both balance properties observe it. Account creation
// stays in the mix because newAccountBalanceIsZero needs fresh accounts to
// fire. doubleTaps gets explicit weight on every screen because rapid
// double-submission is a failure mode these forms must be idempotent under.
// defaultActions adds breadth so the fuzzer wanders the whole app and types
// edge-case values into every field.
export const actionsRoot = weighted(
  [25, addAccount],
  [45, addTxn],
  [10, doubleTaps],
  [20, defaultActions],
);
