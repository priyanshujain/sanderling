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

One submit commits one transaction, so the balance cannot move by more than the
amount that submit typed:

```ts
const submitMovesBalanceByAtMostTypedAmount = always(
  next(() => {
    if (route.current !== "home") return true;
    const action = lastAction.current;
    if (action?.kind !== "Tap" && action?.kind !== "DoubleTap") return true;
    if (!JSON.stringify(action.on ?? "").includes("TxnSubmit")) return true;
    if (submitsInWindow.current !== 1) return true;
    const typed = parseTypedAmount(txnAmountField.previous?.text);
    if (typed === 0) return true;
    const before = totalBalance.previous;
    if (before === null || totalBalance.current === null) return true;
    return Math.abs(totalBalance.current - before) <= typed;
  })
);
```

`always` checks the formula at every step; `next` lets it compare the step before a submit to the step after. The guards narrow it to the one transition that matters, a submit that lands back on home, and the last line states the rule: the balance moved by no more than the typed amount. Double-submit moves it by twice that, and the formula is false.

The obvious version of that last line is `=== typed`, and it is the version this spec used to ship. It was wrong. Folio's `createTransaction` runs in a coroutine and Home's total re-renders on the store's own schedule, so a total that has not caught up yet is what a healthy app looks like a frame after a submit, and an equality convicts it. So does a submit the app rejected, and so does a tap that never landed. The bound declines on all three without needing a case for any of them, and it still catches the bug, because twice the typed amount is more than the typed amount. What it gives up is worth naming: a transaction committed for less than the amount typed is a real ledger bug this property no longer sees. It cannot be told apart from a total one frame behind, and a check that fires on both is evidence about neither.

The window guard is what keeps the comparison about one submit. `totalBalance.previous` is the last total we read, not the total as of the last transaction, so the two numbers being compared can straddle any number of commits: a real run produced a delta of 13000 against a typed 19600, because the window held a double-submit's two 19600 debits and an unrelated 26200 credit. A delta over a window like that is not evidence about the amount typed into any one submit, whichever side of the bound it falls on. Exactly one submit action in the window still catches the bug, because the double tap is a single action.

The null guard is not defensive clutter either, and under a bound its failure mode is the quiet one. Read a balance you could not parse as `0` and the comparison becomes `|0 - 0| <= typed`, which is true at every submit: the property stops judging and never says so. A reading you do not have is not evidence, so it has to decline in the open rather than pass by accident. The real spec guards the same way against a balance too large for exact integer arithmetic.

The values it reads come from extractors, which pull state out of the UI tree once per step:

```ts
const SCREENS = {
  login: "LoginScreen",
  "add-account": "AddAccountScreen",
  "add-transaction": "AddTransactionScreen",
  ledger: "LedgerScreen",
  home: "HomeScreen",
} as const;
type Route = keyof typeof SCREENS;

// The screen this frame shows, or null when it does not show exactly one.
// Android's hierarchy dump carries the outgoing and the incoming screen
// together on better than one frame in five, and such a frame is a navigation
// transition: evidence about neither screen. Ranking the markers and returning
// the first one found is how a spec convicts itself on a half-drawn screen.
const routeOf = (s: State): Route | null => {
  let shown: Route | null = null;
  for (const [name, tag] of Object.entries(SCREENS) as [Route, string][]) {
    if (!s.ax.find({ testTag: tag })) continue;
    if (shown !== null) return null;
    shown = name;
  }
  return shown;
};
const route = extract<Route | null>("route", routeOf);

// Home's own TOTAL BALANCE node, which the app computes over every account.
// Summing the AccountCard balances reads only the cards laid out inside the
// viewport, and a card clipped at the bottom edge looks exactly like money
// moving. Off Home there is nothing to read, so the last total we did read is
// carried forward and `previous` and `current` stay on the same scale.
let lastHomeTotal: number | null = null;
const totalBalance = extract<number | null>("totalBalance", s => {
  if (routeOf(s) !== "home") return lastHomeTotal;
  const total = parseDollarCents(
    s.ax.find([{ testTag: "HomeScreen" }, { testTag: "TotalBalance" }])?.text);
  if (total === null) return null; // unreadable is unknown; the carrier keeps its value
  lastHomeTotal = total;
  return total;
});
```

Every Folio screen and control carries a `testTag`. Compose exposes it as the resource-id on Android and the accessibility identifier on iOS, so one selector resolves on both. `extract` runs against the live tree each step; properties and actions read `.current` and `.previous`, never the raw state. An extractor may not read another extractor's handle, which is why `totalBalance` calls `routeOf` rather than `route.current`.

A second property states what new accounts must look like: a freshly created account starts at zero. The work is in naming the account it judges.

```ts
const newAccountBalanceIsZero = always(
  next(() => {
    if (route.current !== "home") return true;
    if (!isAddAccountSubmitTap(lastAction.current)) return true;
    const typed = accountNameField.previous?.text?.trim();
    const before = accounts.previous ?? null;
    const after = accounts.current;
    if (!typed || before === null || after === null) return true;
    // The only card attributable to a creation is the one named what the fuzzer
    // typed, on the step its submit landed. Anything else that turned up is a
    // card that scrolled into view, not an account that came into existence.
    const matches = after.filter(a => a.name.endsWith(typed));
    if (matches.length !== 1) return true;
    const created = matches[0];
    if (before.some(a => a.name === created.name)) return true;
    return created.balance === null || created.balance === 0;
  })
);
```

Diffing the two account lists and judging whatever is new is the version that reads better and does not work: Home lists the accounts that fit the viewport, so a card that scrolls in is indistinguishable from an account that was just created. That version convicted this property on android over a Travel account holding $24,112.00.

A third property, `submitCommitsOneTransactionPerAction`, states the same bug without arithmetic: over any window, no more transactions may be committed than there were submit actions. It needs no amount and no float comparison, so it survives a window of any width, and it does most of the detecting in practice.

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

That is the whole input. Three invariants, a way in, and a weighted sense of where to spend time. Nothing here names the bug.

## What the run does

sanderling launches Folio, logs in, and starts exploring. Most steps are unremarkable: open an account, add a transaction, watch the balance move by the amount that was typed, `submitMovesBalanceByAtMostTypedAmount` holds.

Then a step lands two taps on submit before the first save settles. Two transactions post. The balance jumps by twice the typed amount. At that step the formula evaluates false and the run records a violation: the step, the screenshot, the offending action, and the residual formula that failed.

The run does not stop. It keeps exploring and keeps checking, so one run surfaces every violation it can reach, not just the first. Open the trace with [`sanderling replay`](../replay/), press `.` to jump to the violation, and step across the boundary to watch the balance double.

sanderling did not know about this bug. It was given what the app guarantees and a realistic way to use it, and it found the sequence that breaks the guarantee. That is the point: you describe what must always be true, and the explorer finds what you would not have thought to try.

## From here

The complete spec is `examples/folio/sanderling/spec.ts`. Folio uses [`just`](https://github.com/casey/just) as a runner: from `examples/folio`, `just install` then `just test` drives it on an Android emulator or device, `just test-ios` on an iOS simulator, and `just web` in Chrome. Then `sanderling replay` and press `.` to land on the violation.

To point sanderling at your own app, see [getting started](../getting-started/). For every selector, operator, action, and sampler, see the [spec language reference](../spec-language/).
