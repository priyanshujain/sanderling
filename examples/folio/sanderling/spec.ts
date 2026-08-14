import {
  InputText,
  Tap,
  actions,
  always,
  extract,
  from,
  integers,
  llm,
  next,
  weighted,
  whenRoute,
} from "@sanderling/spec";
import type { State } from "@sanderling/spec";
import { defaultActions, doubleTaps } from "@sanderling/spec/defaults";
import {
  cardAccountName,
  cardBalanceText,
  cardTxnCountDigits,
  committedTransactionsExceedSubmits,
  countSubmitsInWindow,
  parseDollarCents,
  parseTypedAmount,
  readHomeTotalBalance,
  submitChangesBalanceByTypedAmount,
} from "./predicates";

interface Account {
  // Identity key, not a display name: on web it carries the card's initials.
  name: string;
  // null when the card's balance could not be read at all (see cardBalanceText).
  balance: number | null;
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

// Account cards on Home: identity comes from AccountName, balance from
// AccountBalance. Web exposes neither child (the card is one merged node
// there), so both readings go through predicates.ts, which falls back to
// parsing the card's own text.
const accounts = extract<Account[]>("accounts", s =>
  s.ax.findAll([{ testTag: "HomeScreen" }, { testTag: "AccountCard" }]).map(card => ({
    name: cardAccountName({ childText: card.find({ testTag: "AccountName" })?.text, cardText: card.text }),
    balance: parseDollarCents(
      cardBalanceText({ childText: card.find({ testTag: "AccountBalance" })?.text, cardText: card.text })),
  })));

// Total balance: Home's own TOTAL BALANCE node, which the app computes over
// every account rather than over the cards that happen to be laid out inside
// the viewport. The carrier deliberately tracks only that Home total. Ledger's
// LedgerBalance is a single-account number on a different scale and would
// corrupt cross-screen comparisons if mixed in. Off-Home steps carry forward
// the last-read Home total so `previous` and `current` stay on the same scale.
const homeTotalText = (s: State) =>
  s.ax.find([{ testTag: "HomeScreen" }, { testTag: "TotalBalance" }])?.text;
const onHome = (s: State) => s.ax.find({ testTag: "HomeScreen" }) != null;

let lastHomeTotal: number | null = null;
const totalBalance = extract<number | null>("totalBalance", s => {
  const reading = readHomeTotalBalance({
    onHome: onHome(s),
    totalText: homeTotalText(s),
    previousCarrier: lastHomeTotal,
  });
  lastHomeTotal = reading.carrier;
  return reading.value;
});

// Submit actions inside the window `totalBalance.previous` and
// `totalBalance.current` span. Recomputed rather than shared with the extractor
// above because extractor getters may not read one another; both derive
// freshness from the same reading, so they reset on the same step.
let submitsSinceHomeTotal = 0;
const submitsInWindow = extract("submitsInWindow", s => {
  const fresh = readHomeTotalBalance({
    onHome: onHome(s),
    totalText: homeTotalText(s),
    previousCarrier: null,
  }).fresh;
  const window = countSubmitsInWindow({
    previousCount: submitsSinceHomeTotal,
    lastAction: s.lastAction,
    fresh,
  });
  submitsSinceHomeTotal = window.next;
  return window.reported;
});

// Transactions committed per account, read off the Home cards. Carried across
// off-Home steps exactly like totalBalance, and for the same reason: the pair
// the property compares has to be two Home readings, not a Home reading and
// whatever happened to be on screen. A card whose count is unreadable is left
// out of the map rather than guessed at; the predicate treats a missing account
// as no evidence.
let lastHomeTxnCounts: Record<string, string> | null = null;
const homeTxnCounts = extract<Record<string, string> | null>("homeTxnCounts", s => {
  if (!onHome(s)) return lastHomeTxnCounts;
  const counts: Record<string, string> = {};
  for (const card of s.ax.findAll([{ testTag: "HomeScreen" }, { testTag: "AccountCard" }])) {
    const name = cardAccountName({
      childText: card.find({ testTag: "AccountName" })?.text,
      cardText: card.text,
    });
    const digits = cardTxnCountDigits({
      childText: card.find({ testTag: "AccountTxnCount" })?.text,
      cardText: card.text,
    });
    if (name !== "" && digits !== undefined) counts[name] = digits;
  }
  lastHomeTxnCounts = counts;
  return counts;
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
    return curr
      .filter(a => !prevNames.has(a.name))
      .every(a => a.balance === null || a.balance === 0);
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
      submitsInWindow: submitsInWindow.current,
      typedAmount: parseTypedAmount(txnAmountField.previous?.text),
      prevTotalBalance: totalBalance.previous ?? null,
      currTotalBalance: totalBalance.current,
    }),
  ),
);

// Property 3: one submit action commits at most one transaction. Counting
// actions against transactions needs no amounts and no float arithmetic, and it
// stays sound however wide the window between two Home readings gets, because
// both sides of the comparison accumulate over the same window. It is the
// double-submit stated directly: one tap, two rows.
const submitCommitsOneTransactionPerAction = always(
  next(() =>
    !committedTransactionsExceedSubmits({
      countsBefore: homeTxnCounts.previous ?? null,
      countsAfter: homeTxnCounts.current,
      submitsInWindow: submitsInWindow.current,
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
  submitCommitsOneTransactionPerAction,
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
  [5, doubleTaps],
  [25, defaultActions],
);

// The LLM generator is orthogonal to actionsRoot: with `--generator llm` a model
// picks from the SAME weighted candidate set above, reading the screenshot and a
// numbered, weight-annotated list; the default `--generator seeded` ignores it.
// instructions describe only WHAT the app is, never HOW to test it. The model
// figures out how to surface bugs on its own. With a plain OpenAI key, drop the
// vendor prefix from the model id.
export const generator = llm({
  model: "gpt-5.4-nano",
  instructions:
    "Folio is a personal-finance ledger app. After signing in, the home screen lists accounts, each with a balance. You can create accounts, open an account to see its ledger, and add transactions; each transaction has an amount and changes that account's balance and the overall total.",
});
