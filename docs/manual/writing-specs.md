---
title: Writing specs
---

# Writing specs

A spec has three parts: extractors, properties, and actions.

```ts
import { extract, always, now, actions, weighted, Tap, taps, swipes } from "@sanderling/spec";

// 1. Extractors pull values from each observed state.
const loggedIn = extract((s) => !!s.ax.find("id:home-tab-bar"));
const cartCount = extract<number>((s) => (s.snapshots.cart_count as number) ?? 0);

// 2. Properties are LTL formulas evaluated every step.
export const properties = {
  cartNeverNegative: always(() => cartCount.current >= 0),
};

// 3. Actions are a weighted tree of what sanderling is allowed to do.
export const actions = weighted(
  [10, taps],
  [2, swipes],
);
```

The Go runner calls into the JS runtime each step. Extractors re-read the current state. Properties re-evaluate with their residual formulas. The action generator returns a tree, and one leaf is sampled by weight and dispatched.

## The `State` object

What extractors see:

```ts
interface State {
  ax: AccessibilityTree;               // view hierarchy
  snapshots: Record<string, unknown>;  // values registered by the in-app SDK
  screen: { id: string; hash: string };
  lastAction: Action | null;
  logs: LogEntry[];                    // since previous state
  exceptions: Exception[];
  time: number;                        // ms since run start
}
```

`ax.find("text:Click me")`, `ax.find("id:login-form")`, `ax.findAll("role:todo-row")` are the common accessors. Prefer stable testID-style identifiers over positional selectors, for the same reason you would in Espresso or XCUITest.

`snapshots` is populated by the in-app SDK via `Sanderling.extract("name") { value }`. Use it when the UI does not expose a value you need, such as business-logic state or hidden fields.

## Pattern: preconditions (login, onboarding)

sanderling has no setup phase and no fixtures. Preconditions are action generators with two properties:

1. High weight, so they fire whenever applicable.
2. Gated on a state extractor, so they return an empty tree when not applicable and self-disable once the precondition is met.

```ts
const onLoginScreen = extract((s) => !!s.ax.find("id:login-form"));

const doLogin = actions(() => {
  if (!onLoginScreen.current) return [];
  const emailField = state.ax.find("id:email-field");
  const signInButton = state.ax.find("id:sign-in-button");
  if (!emailField || !signInButton) return [];
  return [
    InputText({ into: emailField, text: "test@example.com" }),
    Tap({ on: signInButton }),
  ];
});
```

Stack these for onboarding, consent dialogs, cold-start flows:

```ts
const dismissOnboarding = actions(() => {
  const skip = state.ax.find("text:Skip");
  return skip ? [Tap({ on: skip })] : [];
});

export const actions = weighted(
  [100, dismissOnboarding],  // clear the path first
  [50,  doLogin],            // log in when the login screen appears
  [10,  taps],               // exploration
  [2,   swipes],
);
```

Lifecycle of a run:

```
Step 1:   fresh install, onboarding visible
          eligible: dismissOnboarding (weight 100)
          picks: Tap "Skip"

Step 2-3: login screen visible
          eligible: doLogin (weight 50)
          picks: InputText / Tap to sign in

Step 4+:  home screen, onboarding and login generators return []
          eligible: taps, swipes
          picks: autonomous exploration
```

Session state (tokens, keychain, prefs) persists through the rest of the run. If the app logs the user out mid-run, `doLogin` re-fires automatically. No retry logic, no special-casing.

## Pattern: conditional properties

Use gating extractors the same way inside properties. Express "only check X when Y holds" with `now(...).implies(...)`:

```ts
const loggedIn = extract((s) => !!s.ax.find("id:home-tab-bar"));

export const properties = {
  cartPersistsWhenLoggedIn: always(
    now(() => loggedIn.current).implies(now(() => cartCount.current !== undefined)),
  ),
};
```

`implies`, `and`, `or`, and `not` are methods on any formula. Combine them freely.

## Pattern: eventually

`always` asserts something holds at every step. `eventually` asserts it holds at some step, usually with a time bound:

```ts
loginSucceedsWithin30s: eventually(() => loggedIn.current).within(30, "seconds"),
```

`within` takes `"milliseconds"`, `"seconds"`, or `"steps"`. Useful for liveness checks: the loading spinner eventually goes away, the deep link eventually lands on `/home`.

## Pattern: snapshot-backed properties

When the UI does not expose a value but the app knows it, use the SDK's extractor registry:

```kotlin
// in the app (Android)
Sanderling.extract("cart_count") { store.cart.size }
```

```ts
// in the spec
const cartCount = extract<number>((s) => (s.snapshots.cart_count as number) ?? 0);

export const properties = {
  cartMonotonicAfterAdd: always(() => {
    const previous = cartCount.previous;
    return previous === undefined || cartCount.current >= previous;
  }),
};
```

This pattern lets you write properties against business logic that no UI element exposes.

## Pattern: weighted exploration sub-trees

Nest `weighted` to group related actions and tune their collective rate:

```ts
export const actions = weighted(
  [100, dismissOnboarding],
  [50,  doLogin],
  [10,  taps],
  [2,   swipes],
  [1, weighted(
    [3, openLink("todos://home")],
    [1, openLink("todos://settings")],
    [1, openLink("todos://item/42/edit")],
  )],
);
```

Weights are relative within a tree, so nested trees get their own local budget. This is how you keep low-frequency but high-value actions (deep links, background/foreground, rotate) from drowning out normal tapping.

## Anti-patterns

**Positional taps.** `Tap({ on: { x: 100, y: 200 } })` works for a demo but breaks on any layout change. Always prefer an `ax.find("id:...")` reference.

**Sleep or wait-for-time.** `Wait(3000)` inside an action generator is a smell. If you need to wait for a condition, use an extractor and gate the next action on it.

**Retry logic inside generators.** Generators should be pure: given the same state they produce the same actions. Retry is the runner's responsibility.

**Unbounded `eventually`.** Without a `.within(...)`, `eventually` never fails within a finite run. It just stays residual. Almost always you want a bound.
