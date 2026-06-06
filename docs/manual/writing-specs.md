---
title: Writing specs
---

# Writing specs

This page teaches spec writing by building one. The subject is folio, the example app from [getting started](../getting-started/). We start from an empty file and end at the finished spec that ships in the repo at `examples/folio/sanderling/spec.ts`. Each section adds one concept.

## The app

folio is a small personal ledger:

- **Login** asks for email and password. The demo account is `demo@folio.app` / `ledger123`.
- **Home** lists account cards, each with a name and balance.
- Tapping a card opens the **Ledger** for that account, listing its transactions.
- From the ledger, **Add transaction** opens a form: amount, type (credit or debit), note.
- From home, **Add account** opens a form: account name.

Every screen and control in folio carries a `testTag`. In Compose this is `Modifier.testTag("LoginScreen")`, which sanderling reads as the resource-id on Android and the accessibility identifier on iOS. Tags are the most reliable way to find elements, and folio's spec uses them throughout:

| Tag | What it marks |
|---|---|
| `LoginScreen`, `LoginEmail`, `LoginPassword`, `LoginSubmit` | login screen and its controls |
| `HomeScreen`, `AccountCard`, `AccountName`, `AccountBalance`, `AddAccountButton` | home screen |
| `AddAccountScreen`, `AccountNameField`, `AddAccountSubmit` | add-account form |
| `LedgerScreen`, `AddTransactionButton` | ledger |
| `AddTransactionScreen`, `TxnAmountField`, `TxnSubmit` | add-transaction form |

## Start with the defaults

The smallest spec that does something:

```ts
import { defaultActions } from "@sanderling/spec/defaults";
import { noUncaughtExceptions } from "@sanderling/spec/defaults/properties";

export const properties = { noUncaughtExceptions };
export const actionsRoot = defaultActions;
```

A spec has two exports. `properties` is a map of named rules that must hold at every step. `actionsRoot` is the pool of actions the explorer may take.

Both come from the standard library here. `noUncaughtExceptions` fails the moment the app throws an exception nobody caught. `defaultActions` is a weighted mix of the built-in generators: random taps, typed text, scrolls, swipes, and double taps on whatever is on screen. There is also `noLogcatErrors`, which fails on error-level logcat lines; it applies on Android and silently holds elsewhere.

Run this spec and watch what happens. The explorer taps the login fields, types garbage into them, taps Sign in, and gets rejected. It never reaches the rest of the app, because random typing does not produce valid credentials. Already useful (a crash on the login screen would be caught), but the app behind the login stays untested.

The explorer needs to be taught how to get past the gate.

## Reading the screen: extractors

Before the spec can act on the app's state, it has to read it. That is what extractors do.

```ts
import { extract } from "@sanderling/spec";

const loggedIn = extract("loggedIn", s => s.ax.find({ testTag: "LoginScreen" }) == null);
```

`extract` takes a callback that runs once per step, against that step's state. `s.ax` is the UI tree: every element on screen with its text, position, and state. `find` returns the first element matching a selector, or `undefined`. Here the selector is an object: match any element whose `testTag` is `LoginScreen`. If no such element exists, we are logged in.

The string name is optional but worth setting. Named extractors show up by name in the replay UI, so you can watch their values change step by step.

`extract` returns a handle with two fields:

- `loggedIn.current` is the value at this step.
- `loggedIn.previous` is the value at the step before, or `undefined` on the first step.

One rule to remember: the state argument `s` exists only inside the `extract` callback. Properties and action generators never see the state directly. They read extractor handles. This keeps every read of the screen in one place, visible in replay, and evaluated exactly once per step.

## Getting past login

To log in, the spec needs the login controls. Elements are state too, so they come from extractors. Passing an array of selectors matches a path: find `LoginScreen`, then find `LoginEmail` inside it.

```ts
const loginEmailField = extract("loginEmailField", s =>
  s.ax.find([{ testTag: "LoginScreen" }, { testTag: "LoginEmail" }]));
const loginPasswordField = extract("loginPasswordField", s =>
  s.ax.find([{ testTag: "LoginScreen" }, { testTag: "LoginPassword" }]));
const loginSubmit = extract("loginSubmit", s =>
  s.ax.find([{ testTag: "LoginScreen" }, { testTag: "LoginSubmit" }]));
```

Now the action. `actions` wraps a callback that returns a list of actions the explorer may take right now. Returning an empty list means "nothing to do here".

```ts
import { InputText, Tap, actions } from "@sanderling/spec";

const DEMO_EMAIL = "demo@folio.app";
const DEMO_PASSWORD = "ledger123";

const login = actions(() => {
  if (loggedIn.current) return [];
  const email = loginEmailField.current;
  const pwd = loginPasswordField.current;
  if (email && !email.text) return [InputText({ into: email, text: DEMO_EMAIL })];
  if (pwd && !pwd.text) return [InputText({ into: pwd, text: DEMO_PASSWORD })];
  const submit = loginSubmit.current;
  return submit ? [Tap({ on: submit })] : [];
});

export const setup = login;
```

The generator reads what is on screen and offers the next sensible step: fill the email if it is empty, then the password, then tap submit. It decides by reading the fields, not by tracking what it did before. Generators must work this way. They are pure functions of the current state, called fresh every step, with no memory of their own. If the app throws the user back to a half-filled login form, this generator picks up from whatever the form actually contains.

The `setup` export gives the generator priority. Each step, the runner asks `setup` first; only when it returns an empty list does the main `actionsRoot` pool get used. Once logged in, `loggedIn.current` is true, `login` yields nothing, and exploration proceeds. If a stray action ever logs the user out mid-run, `loggedIn.current` flips back and `setup` takes over again. No retry logic needed.

Run the spec now and the explorer logs in within a few steps, then fuzzes everything behind the login.

## Knowing where you are: routes

Random taps will wander into the add-account and add-transaction forms, but rarely complete them. To exercise the flows that matter, the spec needs deliberate actions, and those need to know which screen is showing.

folio tags each screen, so a route extractor is one chain of lookups:

```ts
const route = extract<string | null>("route", s => {
  if (s.ax.find({ testTag: "LoginScreen" })) return "login";
  if (s.ax.find({ testTag: "AddAccountScreen" })) return "add-account";
  if (s.ax.find({ testTag: "AddTransactionScreen" })) return "add-transaction";
  if (s.ax.find({ testTag: "LedgerScreen" })) return "ledger";
  if (s.ax.find({ testTag: "HomeScreen" })) return "home";
  return null;
});
```

`whenRoute` builds a generator that is only eligible on the given routes. Here is the account-creation flow:

```ts
import { from, weighted, whenRoute } from "@sanderling/spec";

const addAccountButton = extract("addAccountButton", s =>
  s.ax.find([{ testTag: "HomeScreen" }, { testTag: "AddAccountButton" }]));
const accountNameField = extract("accountNameField", s =>
  s.ax.find([{ testTag: "AddAccountScreen" }, { testTag: "AccountNameField" }]));
const addAccountSubmit = extract("addAccountSubmit", s =>
  s.ax.find([{ testTag: "AddAccountScreen" }, { testTag: "AddAccountSubmit" }]));

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
```

On home it offers one action: tap the add-account button. On the form it offers two: type a name, or tap submit. Everything a generator returns is eligible, and the runner samples one. That sampling is deliberate. Sometimes the explorer types then submits; sometimes it submits an empty form. Both are things real users do, and the second is exactly the kind of input that finds validation bugs.

`from(...)` is a sampler: `.generate()` picks from the list. For numbers there is `integers().between(lo, hi)`, used the same way in the transaction flow:

```ts
import { integers } from "@sanderling/spec";

const addTxnButton = extract("addTxnButton", s =>
  s.ax.find([{ testTag: "LedgerScreen" }, { testTag: "AddTransactionButton" }]));
const txnAmountField = extract("txnAmountField", s =>
  s.ax.find([{ testTag: "AddTransactionScreen" }, { testTag: "TxnAmountField" }]));
const txnSubmit = extract("txnSubmit", s =>
  s.ax.find([{ testTag: "AddTransactionScreen" }, { testTag: "TxnSubmit" }]));
const accountCards = extract("accountCards", s =>
  s.ax.findAll([{ testTag: "HomeScreen" }, { testTag: "AccountCard" }]));

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
```

This one spans three screens: pick an account card on home, tap the add button on the ledger, fill and submit the form. The explorer chains these into complete flows on its own, because after each action the route changes and the generator offers the next link.

## The first property

The spec can now drive the app. Time to state what must be true.

A good property describes an outcome the app guarantees, regardless of how the user got there. folio guarantees that a freshly created account starts at zero. To check it, the spec needs the list of accounts as data:

```ts
interface Account {
  name: string;
  balance: number;
}

function parseDollarCents(text: string | undefined): number {
  if (!text) return 0;
  const sign = text.startsWith("-") ? -1 : 1;
  const digits = text.replace(/[^0-9]/g, "");
  return digits ? sign * parseInt(digits, 10) : 0;
}

const accounts = extract<Account[]>("accounts", s =>
  s.ax.findAll([{ testTag: "HomeScreen" }, { testTag: "AccountCard" }]).map(card => ({
    name: card.find({ testTag: "AccountName" })?.text ?? "",
    balance: parseDollarCents(card.find({ testTag: "AccountBalance" })?.text),
  })));
```

`findAll` returns every match. Each card element supports `.find` scoped to its own subtree, so name and balance come from inside the card. The balance on screen is text like `"$5.00"`; a plain helper function parses it to cents. Specs are ordinary TypeScript, so helpers like this are fine anywhere.

Now the property:

```ts
import { always, next } from "@sanderling/spec";

const newAccountBalanceIsZero = always(
  next(() => {
    const prev = accounts.previous ?? [];
    const curr = accounts.current;
    if (prev.length === 0 || curr.length === 0) return true;
    const prevNames = new Set(prev.map(a => a.name));
    return curr.filter(a => !prevNames.has(a.name)).every(a => a.balance === 0);
  })
);

export const properties = {
  noUncaughtExceptions,
  newAccountBalanceIsZero,
};
```

Read it inside out. The callback compares two consecutive steps: any account present now but absent before is new, and every new account must show a zero balance. `next` makes the formula span a step boundary, so `.previous` and `.current` are both defined inside it. `always` applies the check at every step of the run.

The early return matters. When the explorer navigates away from home, the card list goes empty, and when it comes back, every account looks "new". Without the guard, the property would fire on ordinary navigation. Writing a property is mostly this: stating the rule, then excluding the states where the rule does not apply.

## The second property

The deepest flow in folio is adding a transaction, so it deserves the strongest property: submitting a transaction must move the total balance by exactly the typed amount.

The total balance needs care. Only home shows all account cards, so the sum is only fresh there. On other screens the spec carries the last home value forward, keeping `.previous` and `.current` comparable across screen changes:

```ts
let lastHomeTotal = 0;
const totalBalance = extract("totalBalance", s => {
  const cards = s.ax.findAll([{ testTag: "HomeScreen" }, { testTag: "AccountCard" }]);
  if (cards.length === 0) return lastHomeTotal;
  lastHomeTotal = cards.reduce(
    (sum, c) => sum + parseDollarCents(c.find({ testTag: "AccountBalance" })?.text), 0);
  return lastHomeTotal;
});

const lastAction = extract("lastAction", s => s.lastAction);
```

`s.lastAction` is the action the explorer performed in the previous step. The property uses it to recognize "the user just tapped Submit":

```ts
function parseTypedAmount(text: string | undefined | null): number {
  if (!text) return 0;
  const trimmed = text.trim().replace(/,/g, "").replace(/^[+-]/, "");
  if (!/^\d+(\.\d{1,2})?$/.test(trimmed)) return 0;
  const [whole, frac = ""] = trimmed.split(".");
  return parseInt(whole, 10) * 100 + parseInt((frac + "00").slice(0, 2), 10);
}

const submitMovesBalanceByTypedAmount = always(
  next(() => {
    if (route.current !== "home") return true;
    const action = lastAction.current;
    if (!action || (action.kind !== "Tap" && action.kind !== "DoubleTap")) return true;
    if (!JSON.stringify(action.on ?? "").includes("TxnSubmit")) return true;
    const typed = parseTypedAmount(txnAmountField.previous?.text);
    if (typed === 0) return true;
    const moved = Math.abs(totalBalance.current - (totalBalance.previous ?? 0));
    return moved === typed;
  })
);
```

The first four lines are guards: only check when the last action was a tap on the transaction submit button and the app landed back on home. After the guards, the rule is one line: the balance moved by exactly what was typed.

This property catches a real bug in folio. The transaction form does not disable its submit button while saving, so a fast double tap lands two transactions. The balance moves by twice the typed amount, `moved === typed` is false, and the violation is recorded with the screenshot and the action that caused it.

The shipped spec factors these checks into pure functions in `predicates.ts` and unit-tests them separately. The property stays a thin wrapper. Worth copying once your predicates grow past a few lines: a property with a bug in it reports violations that are not there, or misses the ones that are.

## Weights

The last piece is the action mix. `weighted` builds a tree of generators; weights are relative within the tree.

```ts
import { weighted } from "@sanderling/spec";
import { defaultActions, doubleTaps } from "@sanderling/spec/defaults";

export const actionsRoot = weighted(
  [25, addAccount],
  [45, addTxn],
  [5, doubleTaps],
  [25, defaultActions],
);
```

Weights declare testing intent:

- `addTxn` gets the most weight because the transaction chain is the deepest flow and both balance properties watch it.
- `addAccount` stays in the mix because `newAccountBalanceIsZero` needs fresh accounts to check.
- `doubleTaps` gets explicit weight because rapid double submission is a failure mode these forms must survive. This is the line that flushes out the double-submit bug.
- `defaultActions` keeps a quarter of the budget on random exploration, so the explorer still wanders everywhere and types edge-case values into every field. Without it, the spec only tests the flows you thought of, which defeats the point.

When a sampled generator returns an empty list, the runner re-draws, up to 16 times per step. So weights state preference, and the step still lands on a generator that currently has something to offer.

## The finished spec

The complete file is `examples/folio/sanderling/spec.ts`, about 190 lines including comments. Run it:

```sh
cd examples/folio
just test        # android
just test-ios    # ios
```

Then `sanderling replay` and press `.` to jump to the first violation.

## Finding properties for your own app

Properties follow a few recurring shapes. When staring at a new app, look for:

- **Conserved quantities.** Money, item counts, scores. State the arithmetic: a transfer leaves the total unchanged, a deletion drops the count by one.
- **Initial states.** New account, empty cart, fresh document. State what they must contain.
- **Idempotence.** Submitting once and submitting twice must have the same effect. Give `doubleTaps` weight and let the property judge.
- **Reachability.** `eventually(() => loggedIn.current).within(30, "seconds")` says login must succeed within bounds. Useful as a smoke check that setup works.
- **Never-states.** No uncaught exceptions, no error toasts, no negative balances. Cheap to write, always on.

Start with the defaults plus one property. A spec with one sharp property and good action coverage beats a spec with ten vague ones.

## Anti-patterns

**Accessing state outside `extract`.** The `s` argument exists only inside the `extract()` callback. Everywhere else, read `.current` and `.previous` on extractor handles.

**Positional taps.** `Tap({ on: { x: 100, y: 200 } })` breaks on any layout change. Find the element instead.

**Unbounded `eventually`.** Without `.within(...)`, `eventually` can never fail within a finite run. Always bound it.

**Memory in generators.** Generators must derive everything from the current state. Counters, flags, and retry logic inside a generator make its behavior depend on history, which breaks reproducibility and breaks recovery when the app moves unexpectedly.

**Properties without guards.** A rule that is only meaningful on one screen must check the route first. Unguarded properties fire on navigation noise and train you to ignore violations.
