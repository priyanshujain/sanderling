---
title: Writing specs
---

# Writing specs

A spec has three parts: extractors, properties, and actions.

```ts
import { extract, always, now, actions, weighted, Tap, taps, swipes } from "@sanderling/spec";

const loggedIn = extract((s) => !!s.ax.find("id:home-tab-bar"));

export const properties = {
  cartNeverNegative: always(() => cartCount.current >= 0),
};

export const actionsRoot = weighted(
  [10, taps],
  [2, swipes],
);
```

The Go runner calls into the JS runtime each step. Extractors re-read the current state. Properties re-evaluate with their residual formulas. The action generator returns a tree, and one leaf is sampled by weight and dispatched.

## The `State` object

What extractors receive:

```ts
interface State {
  ax: AccessibilityTree;
  snapshots: Record<string, unknown>;
  lastAction: Action | null;
  logs: readonly LogEntry[];
  exceptions: readonly ExceptionRecord[];
  time: number;   // ms since run start
}
```

`ax` is the live UI hierarchy. `snapshots` carries any key-value data pushed by the app SDK. `logs` and `exceptions` contain entries collected since the previous step.

## Extractors

`extract()` wraps a getter that runs against every new state. The returned object exposes `.current` (this step's value) and `.previous` (last step's, or `undefined` on the first step).

```ts
const loggedIn = extract((s) => !!s.ax.find("id:home-tab-bar"));
const balance = extract<number>((s) => s.snapshots["account.balance"] as number ?? 0);

// Inside a property or action:
loggedIn.current     // boolean
loggedIn.previous    // boolean | undefined
```

Extractors are cheap. Prefer one extractor per concept and reuse it across properties and action generators.

## Finding elements

`ax.find(selector)` returns the first matching `AccessibilityElement`, or `undefined`. `ax.findAll(selector)` returns all matches. Both are available on the tree root and on any element (scoped to its subtree).

**String selectors:**

| Form | Match rule |
|---|---|
| `id:<value>` | Exact match on resource-id, or suffix after `:id/` (Android) |
| `text:<value>` | Substring match on text content |
| `desc:<value>` | Exact match on accessibility description, or starts-with for iOS merged labels |
| `descPrefix:<prefix>` | Starts-with on accessibility description |
| `<attr>:<value>` | Substring match on any raw attribute by name |

**Object selectors** (AND of all given attributes):

```ts
s.ax.find({ accessibilityText: "LoginScreen" })
s.ax.find({ accessibilityText: "login_email" })
```

**Path queries** (global only):

```ts
s.ax.find("id:HomeScreen > descPrefix:account_card:")
```

Each segment is matched within the subtree of the previous match.

**Cross-platform aliases** are resolved automatically. `label` and `accessibilityLabel` both resolve to `accessibilityText`; `content-desc` and `accessibilityText` are interchangeable; `identifier` and `accessibilityIdentifier` resolve to `resource-id`.

See the [Spec language reference](./spec-language/) for the complete selector grammar and per-platform field availability.

## Properties

Properties are named LTL formulas exported from the spec. The verifier evaluates each one every step and fails the run when a formula is violated.

```ts
export const properties = {
  balanceNeverNegative: always(() => balance.current >= 0),
  loginReachable: eventually(() => loggedIn.current).within(30, "seconds"),
};
```

**Operators:**

- `always(f)` - `f` must hold at every step.
- `eventually(f).within(n, unit)` - `f` must hold at some step within `n` milliseconds, seconds, or steps.
- `now(f)` - evaluates `f` at the current step (used for implication antecedents).
- `next(f)` - evaluates `f` at the next step.

**Combinators** - available on any formula:

```ts
now(() => loggedIn.current).implies(now(() => cartCount.current !== undefined))
formulaA.and(formulaB)
formulaA.or(formulaB)
formulaA.not()
```

`implies`, `and`, `or`, and `not` compose freely.

## Actions

Action generators return a list of actions to perform. The runner samples one from the weighted tree and dispatches it through the driver.

**Built-in generators** (pass directly to `weighted`):

- `taps` - autonomous random taps on clickable elements.
- `swipes` - autonomous random swipe gestures.
- `waitOnce` - idles one step.
- `pressKey` - presses a random supported key.

**Action constructors:**

```ts
Tap({ on: element })                         // tap an element or selector string
InputText({ into: element, text: "hello" })  // clear and type into a field
Swipe({ from: elementOrPoint, to: elementOrPoint, durationMillis?: number })
PressKey({ key: "back" | "home" | "enter" | "tab" | "up" | "down" | "left" | "right" })
Wait({ durationMillis: number })
```

**Samplers** - cycle over a fixed list:

```ts
const names = from(["Checking", "Savings", "Travel"]);
names.generate()  // picks from the list
```

**Custom generators:**

```ts
const doLogin = actions(() => {
  if (loggedIn.current) return [];
  const emailField = loginEmail.current;
  const submit = loginSubmit.current;
  if (!emailField || !submit) return [];
  return [InputText({ into: emailField, text: "test@example.com" }), Tap({ on: submit })];
});
```

**Weighted trees:**

```ts
export const actionsRoot = weighted(
  [100, dismissOnboarding],
  [50,  doLogin],
  [10,  taps],
  [2,   swipes],
  [1, weighted(
    [3, openDeepLink("app://home")],
    [1, openDeepLink("app://settings")],
  )],
);
```

Weights are relative within each tree. Nested trees get their own local budget.

## Default properties

`@sanderling/spec/defaults/properties` exports ready-made properties:

```ts
import { noUncaughtExceptions, noLogcatErrors } from "@sanderling/spec/defaults/properties";

export const properties = {
  noUncaughtExceptions,  // fails if the app throws an uncaught exception
  noLogcatErrors,        // fails if logcat emits any error-level lines
};
```

## Pattern: preconditions

sanderling has no setup phase. Preconditions are action generators with high weight that self-disable once the condition is satisfied.

```ts
const onLoginScreen = extract((s) => !!s.ax.find("id:login-form"));
const loginEmailField = extract((s) => s.ax.find("id:email-field"));
const loginSubmit = extract((s) => s.ax.find("id:sign-in-button"));

const doLogin = actions(() => {
  if (!onLoginScreen.current) return [];
  const email = loginEmailField.current;
  const submit = loginSubmit.current;
  if (!email || !submit) return [];
  return [
    InputText({ into: email, text: "test@example.com" }),
    Tap({ on: submit }),
  ];
});
```

Stack these for onboarding, consent dialogs, and cold-start flows:

```ts
export const actionsRoot = weighted(
  [100, dismissOnboarding],
  [50,  doLogin],
  [10,  taps],
  [2,   swipes],
);
```

Once `onLoginScreen.current` is false, `doLogin` returns `[]` and drops out of the eligible set automatically.

## Pattern: conditional properties

Gate a property so it only applies when a precondition holds:

```ts
export const properties = {
  cartPersistsWhenLoggedIn: always(
    now(() => loggedIn.current).implies(now(() => cartCount.current !== undefined)),
  ),
};
```

## Pattern: step-to-step invariants

Use `next()` to express invariants that span two consecutive steps:

```ts
const newAccountBalanceIsZero = always(
  next(() => {
    const prev = accounts.previous ?? [];
    const curr = accounts.current;
    if (prev.length === 0 || curr.length === 0) return true;
    const prevIds = new Set(prev.map((a) => a.id));
    return curr.filter((a) => !prevIds.has(a.id)).every((a) => a.balance === 0);
  }),
);
```

## Anti-patterns

**Accessing `state` outside of `extract`.** The `state` argument exists only inside the `extract()` callback. Use extractors and `.current` everywhere else.

**Positional taps.** `Tap({ on: { x: 100, y: 200 } })` breaks on any layout change. Always prefer an `ax.find("id:...")` reference.

**Unbounded `eventually`.** Without `.within(...)`, `eventually` never fails within a finite run. Almost always you want a bound.

**`Wait()` inside generators.** Waiting for a condition belongs in an extractor guard, not inside a generator.

**Retry logic inside generators.** Generators must be pure. Given the same state they produce the same actions. Retry is the runner's responsibility.
