---
title: "Case study: Folio"
---

# Case study: Folio

Folio is a personal-ledger app: log in, create accounts, record credits and debits. It is built once in Kotlin Multiplatform and ships to Android, iOS, and web from a single Compose codebase. The repo carries it under `examples/folio`, with its spec at `examples/folio/sanderling/spec.ts`.

It also carries a bug, the kind that survives manual testing and example-based suites and ships to production. This page follows how sanderling finds it.

## The bug

Folio's add-transaction form saves on submit and navigates home. The submit handler does not disable the button while the save is in flight:

```kotlin
AddTransactionEvent.Submit -> submit()
```

Tap submit twice fast and two transactions post. The balance moves by twice what you typed.

Nobody writes this test. A manual tester taps submit once, sees the right number, moves on. A scripted test encodes the same single tap. The bug lives in a sequence no one thought to script: the double tap, on this screen, with a pending save. That is the class of bug sanderling exists to catch.

## Telling sanderling what is true

You do not script the double tap. You state the invariant and let sanderling find the inputs that break it.

The amount the user types must equal the amount the balance moves:

```ts
const submitMovesBalanceByTypedAmount = always(
  next(() => {
    if (route.current !== "home") return true;
    const action = lastAction.current;
    if (action?.kind !== "Tap" && action?.kind !== "DoubleTap") return true;
    if (!JSON.stringify(action.on ?? "").includes("TxnSubmit")) return true;
    const typed = parseTypedAmount(txnAmountField.previous?.text);
    if (typed === 0) return true;
    const before = totalBalance.previous;
    if (before === null || totalBalance.current === null) return true;
    return Math.abs(totalBalance.current - before) === typed;
  })
);
```

`always` checks the formula at every step; `next` lets it compare the step before a submit to the step after. The guards narrow it to the one transition that matters, a submit that lands back on home, and the last line states the rule: the balance moved by exactly the typed amount. Double-submit moves it by twice that, and the formula is false.

The null guard is not defensive clutter, it is the difference between a property and a false alarm. Read a balance you could not parse as `0` and the comparison becomes `0 - 0 === typed`, which is false at every healthy submit. A reading you do not have is not evidence, so the property declines to judge. The real spec guards the same way against a balance too large for exact integer arithmetic.

The values it reads come from extractors, which pull state out of the UI tree once per step:

```ts
const route = extract<string | null>("route", s => {
  if (s.ax.find({ testTag: "AddTransactionScreen" })) return "add-transaction";
  if (s.ax.find({ testTag: "HomeScreen" })) return "home";
  // ...other screens
  return null;
});

const totalBalance = extract("totalBalance", s =>
  s.ax.findAll([{ testTag: "HomeScreen" }, { testTag: "AccountCard" }])
    .reduce((sum, c) => sum + parseDollarCents(c.find({ testTag: "AccountBalance" })?.text), 0));
```

Every Folio screen and control carries a `testTag`. Compose exposes it as the resource-id on Android and the accessibility identifier on iOS, so one selector resolves on both. `extract` runs against the live tree each step; properties and actions read `.current` and `.previous`, never the raw state.

A second property states what new accounts must look like: a freshly created account starts at zero.

```ts
const newAccountBalanceIsZero = always(
  next(() => {
    const before = new Set((accounts.previous ?? []).map(a => a.name));
    return accounts.current
      .filter(a => !before.has(a.name))
      .every(a => a.balance === 0);
  })
);
```

## Reaching the screens that matter

A fuzzer that pokes at random never logs in, and never reaches a transaction form. The spec gives sanderling enough to drive the real flows, no more.

Login is a precondition, exported as `setup`. The runner runs it before anything else and falls through to the main pool once it yields nothing:

```ts
const login = actions(() => {
  if (loggedIn.current) return [];
  const email = loginEmailField.current;
  if (email && !email.text) return [InputText({ into: email, text: DEMO_EMAIL })];
  const pwd = loginPasswordField.current;
  if (pwd && !pwd.text) return [InputText({ into: pwd, text: DEMO_PASSWORD })];
  const submit = loginSubmit.current;
  return submit ? [Tap({ on: submit })] : [];
});

export const setup = login;
```

It reads the form and offers the next move, never tracking what it did before. If a stray tap logs the user out mid-run, `loggedIn` flips and login re-engages on its own.

The transaction flow spans three screens. `whenRoute` keeps it eligible only where it applies, and returns every reasonable next action, so sometimes the form is filled and submitted, sometimes submitted empty:

```ts
const addTxn = whenRoute(route, ["home", "ledger", "add-transaction"], () => {
  if (route.current === "home") {
    const cards = accountCards.current;
    return cards.length ? [Tap({ on: from(cards).generate() })] : [];
  }
  if (route.current === "ledger") {
    const btn = addTxnButton.current;
    return btn ? [Tap({ on: btn })] : [];
  }
  const field = txnAmountField.current, submit = txnSubmit.current;
  const out = [];
  if (field) out.push(InputText({ into: field, text: String(amounts.generate()) }));
  if (submit) out.push(Tap({ on: submit }));
  return out;
});
```

The action pool weights the flows by how much they are worth testing. The transaction chain dominates, account creation stays in the mix so new accounts keep appearing, and `defaultActions` keeps a quarter of the budget on untargeted exploration: random taps, edge-case text in every field, scrolls, swipes, double taps.

```ts
export const actionsRoot = weighted(
  [45, addTxn],
  [25, addAccount],
  [25, defaultActions],
  [5, doubleTaps],
);
```

That is the whole input. Two invariants, a way in, and a weighted sense of where to spend time. Nothing here names the bug.

## What the run does

sanderling launches Folio, logs in, and starts exploring. Most steps are unremarkable: open an account, add a transaction, watch the balance move by exactly what was typed, `submitMovesBalanceByTypedAmount` holds.

Then a step lands two taps on submit before the first save settles. Two transactions post. The balance jumps by twice the typed amount. At that step the formula evaluates false and the run records a violation: the step, the screenshot, the offending action, and the residual formula that failed.

The run does not stop. It keeps exploring and keeps checking, so one run surfaces every violation it can reach, not just the first. Open the trace with [`sanderling replay`](../replay/), press `.` to jump to the violation, and step across the boundary to watch the balance double.

sanderling did not know about this bug. It was given what the app guarantees and a realistic way to use it, and it found the sequence that breaks the guarantee. That is the point: you describe what must always be true, and the explorer finds what you would not have thought to try.

## From here

The complete spec is `examples/folio/sanderling/spec.ts`. Folio uses [`just`](https://github.com/casey/just) as a runner: from `examples/folio`, `just install` then `just test` drives it on an Android emulator or device, `just test-ios` on an iOS simulator, and `just web` in Chrome. Then `sanderling replay` and press `.` to land on the violation.

To point sanderling at your own app, see [getting started](../getting-started/). For every selector, operator, action, and sampler, see the [spec language reference](../spec-language/).
