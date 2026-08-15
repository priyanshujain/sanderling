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
import type { AccessibilityElement, State } from "@sanderling/spec";
import { defaultActions, doubleTaps } from "@sanderling/spec/defaults";
import {
  cardAccountName,
  cardBalanceText,
  cardTxnCount,
  committedTransactionsExceedSubmits,
  countSubmitsInWindow,
  createdAccountHasNonZeroBalance,
  homeAccountsOf,
  homeTxnCountsOf,
  oncePerFrame,
  parseDollarCents,
  parseTypedAmount,
  readHomeCards,
  readHomeTotalBalance,
  routeOfFrame,
  submitChangesBalanceByTypedAmount,
} from "./predicates";
import type { Account, CardReading, TxnCount } from "./predicates";

// Screen markers, and the route each one names. Detection is by testTag
// (resource-id on Android, accessibilityIdentifier on iOS).
const SCREENS = {
  login: "LoginScreen",
  "add-account": "AddAccountScreen",
  "add-transaction": "AddTransactionScreen",
  ledger: "LedgerScreen",
  home: "HomeScreen",
} as const;
type Route = keyof typeof SCREENS;

// The screen this frame shows, or null when it does not show exactly one: see
// routeOfFrame, which owns that rule and the reason for it. Everything below
// takes its answer from here, so no two readings can disagree about which
// screen the app is on. Every extractor asks, so the answer is read once per
// frame: see oncePerFrame for why that stays fresh.
const routeOf = oncePerFrame(
  (s: State): Route | null =>
    routeOfFrame<Route>(SCREENS, tag => s.ax.find({ testTag: tag }) != null),
);

// An element is a reading, and a target, only when the route says we are on its
// screen. Scoping a find to the screen's own node is not enough on a transition
// frame: both screens are in the tree, so the one the app has already left
// still resolves and the tap goes to whatever now occupies those pixels.
const on =
  (route: Route, tag: string) =>
  (s: State): AccessibilityElement | undefined =>
    routeOf(s) === route ? s.ax.find([{ testTag: SCREENS[route] }, { testTag: tag }]) : undefined;

const allOn =
  (route: Route, tag: string) =>
  (s: State): AccessibilityElement[] =>
    routeOf(s) === route ? s.ax.findAll([{ testTag: SCREENS[route] }, { testTag: tag }]) : [];

const loggedIn = extract("loggedIn", s => routeOf(s) !== "login");
const route = extract<Route | null>("route", routeOf);

// One parse of Home's account cards. Identity comes from AccountName, balance
// from AccountBalance, the transaction count from AccountTxnCount. Web exposes
// none of those children (the card is one merged node there), so every reading
// goes through predicates.ts, which falls back to parsing the card's own text.
//
// Everything that comes off the card list shares this parse so the readings
// cannot disagree with each other about what was on screen.
const homeCards = oncePerFrame((s: State): CardReading[] =>
  allOn("home", "AccountCard")(s).map(card => ({
    name: cardAccountName({
      childText: card.find({ testTag: "AccountName" })?.text,
      cardText: card.text,
    }),
    balance: parseDollarCents(
      cardBalanceText({
        childText: card.find({ testTag: "AccountBalance" })?.text,
        cardText: card.text,
      })),
    count: cardTxnCount({
      childText: card.find({ testTag: "AccountTxnCount" })?.text,
      cardText: card.text,
    }),
  })));

// Total balance: Home's own TOTAL BALANCE node, which the app computes over
// every account rather than over the cards that happen to be laid out inside
// the viewport. The carrier deliberately tracks only that Home total. Ledger's
// LedgerBalance is a single-account number on a different scale and would
// corrupt cross-screen comparisons if mixed in. Off-Home steps carry forward
// the last-read Home total so `previous` and `current` stay on the same scale.
const homeTotalText = (s: State) => on("home", "TotalBalance")(s)?.text;

let lastHomeTotal: number | null = null;
const totalBalance = extract<number | null>("totalBalance", s => {
  const reading = readHomeTotalBalance({
    route: routeOf(s),
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
    route: routeOf(s),
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

// The account list, carried across off-Home steps exactly like totalBalance and
// for the same reason: the pair a property compares has to be two Home
// readings, not a Home reading and whatever happened to be on screen.
let lastHomeAccounts: Account[] | null = null;
const accounts = extract<Account[] | null>("accounts", s => {
  const reading = readHomeCards({
    route: routeOf(s),
    reading: homeAccountsOf(homeCards(s)),
    previousCarrier: lastHomeAccounts,
  });
  lastHomeAccounts = reading.carrier;
  return reading.value;
});

// Transactions committed per account, same carrier rule.
let lastHomeTxnCounts: Record<string, TxnCount> | null = null;
const homeTxnCounts = extract<Record<string, TxnCount> | null>("homeTxnCounts", s => {
  const reading = readHomeCards({
    route: routeOf(s),
    reading: homeTxnCountsOf(homeCards(s)),
    previousCarrier: lastHomeTxnCounts,
  });
  lastHomeTxnCounts = reading.carrier;
  return reading.value;
});

// The counting invariant gets its own window because its carrier advances on a
// different event than the total's: a Home frame can render the footer total
// while its card list is still empty. Sharing submitsInWindow would let the
// count reset without the counts pair moving, and a window whose submit count
// is smaller than the interval its two readings span is a false conviction
// waiting to happen.
let submitsSinceHomeCards = 0;
const submitsSinceCounts = extract("submitsSinceCounts", s => {
  const fresh = readHomeCards({
    route: routeOf(s),
    reading: homeTxnCountsOf(homeCards(s)),
    previousCarrier: null,
  }).fresh;
  const window = countSubmitsInWindow({
    previousCount: submitsSinceHomeCards,
    lastAction: s.lastAction,
    fresh,
  });
  submitsSinceHomeCards = window.next;
  return window.reported;
});

const lastAction = extract("lastAction", s => s.lastAction);

const loginEmailField = extract("loginEmailField", on("login", "LoginEmail"));
const loginPasswordField = extract("loginPasswordField", on("login", "LoginPassword"));
const loginSubmit = extract("loginSubmit", on("login", "LoginSubmit"));
const addAccountButton = extract("addAccountButton", on("home", "AddAccountButton"));
const accountNameField = extract("accountNameField", on("add-account", "AccountNameField"));
const addAccountSubmit = extract("addAccountSubmit", on("add-account", "AddAccountSubmit"));
const addTxnButton = extract("addTxnButton", on("ledger", "AddTransactionButton"));
const txnAmountField = extract("txnAmountField", on("add-transaction", "TxnAmountField"));
const txnSubmit = extract("txnSubmit", on("add-transaction", "TxnSubmit"));
const accountCards = extract("accountCards", allOn("home", "AccountCard"));

// Property 1: an account starts life holding nothing. The account it judges is
// the one the fuzzer just created, on the step that creation landed on Home:
// a card that merely turns up in a later reading is a card that came into view,
// not an account that came into existence. See createdAccountHasNonZeroBalance.
const newAccountBalanceIsZero = always(
  next(() =>
    !createdAccountHasNonZeroBalance({
      route: route.current,
      lastAction: lastAction.current,
      typedName: accountNameField.previous?.text,
      before: accounts.previous ?? null,
      after: accounts.current,
    }),
  ),
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
      submitsInWindow: submitsSinceCounts.current,
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
