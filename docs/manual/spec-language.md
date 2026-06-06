---
title: Spec language reference
---

# Spec language reference

Lookup reference for everything importable from `@sanderling/spec`. For a guided walkthrough, read [writing specs](../writing-specs/) first.

## Module structure

A spec is a TypeScript module evaluated by the Go runner each step. It exports `properties` and `actionsRoot`, plus an optional `setup`:

```ts
import { ... } from "@sanderling/spec";

export const properties = { ... };
export const actionsRoot = weighted(...);
export const setup = login;   // optional
```

`setup` is an `ActionGenerator` the runner consults before `actionsRoot` each step. While it returns actions, they run; when it returns an empty list, the runner falls through to `actionsRoot`. Use it for preconditions like login and onboarding. If the app later regresses across the precondition (a logout mid-run), `setup` re-engages on its own.

## State

Every extractor callback receives a `State`:

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

| Field | Description |
|---|---|
| `ax` | Live UI hierarchy for this step |
| `snapshots` | Key-value data pushed by the app SDK (empty if SDK not integrated) |
| `lastAction` | The action dispatched in the previous step, or `null` on the first step |
| `logs` | Log entries collected since the previous step |
| `exceptions` | Uncaught exceptions or `Sanderling.reportError()` calls since the previous step |
| `time` | Milliseconds elapsed since the run started |

## Selectors

Selectors are passed to `ax.find()`, `ax.findAll()`, and element-scoped `.find()` / `.findAll()`.

### String selectors

| Form | Matches |
|---|---|
| `id:<value>` | Exact match on resource-id, or element whose resource-id ends with `:id/<value>` (Android) |
| `text:<value>` | Substring match on text content |
| `desc:<value>` | Exact match on accessibility description; also matches when description starts with `<value>, ` (iOS merged labels) |
| `descPrefix:<prefix>` | Starts-with match on accessibility description |
| `<attr>:<value>` | Substring match on any raw attribute by name |

Boolean attributes (`"true"` / `"false"`) use exact match rather than substring.

### Object selectors

Pass an object to apply multiple attribute filters with AND semantics:

```ts
s.ax.find({ accessibilityText: "LoginScreen" })
s.ax.find({ testTag: "AccountCard", clickable: true })
```

Every key-value pair must match. Substring and boolean rules apply per attribute.

Known attribute names are typed; you get autocomplete on `testTag`, `text`, `content-desc`, the boolean states (`clickable`, `enabled`, `focused`, `checked`, `selected`), and the cross-platform aliases (`identifier`, `accessibilityIdentifier`, `accessibilityText`, `accessibilityLabel`, `label`, `resource-id`, `class`, `elementType`, `package`, `placeholderValue`, `hintText`). Boolean state attributes accept a native `true` / `false`. Other attribute keys still type-check as a string-valued fallback so raw driver attributes remain reachable.

### Path selectors

An array of object selectors matches a path: each segment is matched within the subtree of the previous match. Arrays work on the tree root and on element-scoped `.find`/`.findAll`.

```ts
s.ax.find([{ testTag: "LoginScreen" }, { testTag: "LoginEmail" }])
s.ax.findAll([{ testTag: "HomeScreen" }, { testTag: "AccountCard" }])
```

String selectors chain the same way with ` > `, but only on the tree root (`ax.find`, `ax.findAll`):

```ts
s.ax.find("id:HomeScreen > descPrefix:account_card:")
s.ax.find("id:LedgerScreen > desc:ledger_balance_display")
```

### Cross-platform aliases

These key aliases are resolved automatically so selectors work across platforms without changes:

| Write this | Also checks |
|---|---|
| `content-desc` | `accessibilityText` |
| `accessibilityText` | `content-desc` |
| `label` | `accessibilityText` |
| `accessibilityLabel` | `accessibilityText` |
| `identifier` | `resource-id` |
| `accessibilityIdentifier` | `resource-id` |

## AccessibilityElement fields

Fields available on every element returned by `find` / `findAll`:

| Field | Type | Description |
|---|---|---|
| `id` | `string` | resource-id (Android) or accessibility identifier (iOS) |
| `text` | `string` | Visible text content |
| `desc` | `string` | Accessibility description (`content-desc` / `accessibilityText`) |
| `class` | `string` | View class (Android), element type (iOS), or HTML tag (web) |
| `clickable` | `boolean` | Element is interactive |
| `enabled` | `boolean` | Element is enabled |
| `checked` | `boolean` | Checkbox or toggle state |
| `focused` | `boolean` | Element has input focus |
| `selected` | `boolean` | Selection state |
| `bounds` | `{ left, top, right, bottom }` | Bounding box in device pixels |
| `x` | `number` | Center X (derived from bounds) |
| `y` | `number` | Center Y (derived from bounds) |
| `attrs` | `Record<string, string>` | All raw attributes from the driver |

## Platform notes

### Android

- `id` maps to the Android resource-id (e.g., `com.example:id/button`). The `id:<value>` selector matches by suffix after `:id/`, so `id:button` matches `com.example:id/button`.
- `desc` maps to `content-desc`.
- `class` is the Java view class name (e.g., `android.widget.TextView`).
- `attrs` contains raw UIAutomator attributes: `package`, `scrollable`, `checkable`, etc.

### iOS

- `id` maps to the `accessibilityIdentifier` set via `.accessibilityIdentifier` in SwiftUI/UIKit.
- `desc` maps to `accessibilityText`, which the iOS sidecar builds by merging `accessibilityLabel` and the element's value (e.g., `"Close, icon description"`). The `desc:<value>` selector handles this by also matching when the description starts with `<value>, `.
- `class` is the XCUITest element type (e.g., `XCUIElementTypeButton`).
- `attrs` contains raw XCUITest attributes: `title`, `placeholderValue`, `hasFocus`, etc.

### Web (Chrome)

- `id` maps to the HTML `id` attribute.
- `desc` is derived from `aria-label`, `alt`, or `title`.
- `class` is the lowercase HTML tag name (e.g., `button`, `input`).
- `attrs` contains all HTML attributes available to CDP.

### KMP (Kotlin Multiplatform)

KMP apps are tested identically to native apps. An Android KMP build uses the Android driver; an iOS KMP build uses the iOS driver. There is no separate KMP driver. The accessibility tree structure reflects the target platform, so the same selector portability rules apply.

## Extractors

```ts
const loggedIn = extract((s) => !!s.ax.find("id:home-tab-bar"));
const route = extract("route", (s) => ...);   // named form

loggedIn.current    // T - value from the current step
loggedIn.previous   // T | undefined - value from the previous step, undefined on first step
```

Extractors are evaluated before properties and action generators. Use `.previous` to detect transitions between steps. Named extractors appear by name in the replay UI and trace.

## LTL operators

| Function | Meaning |
|---|---|
| `always(f)` | `f` must hold at every step |
| `eventually(f).within(n, unit)` | `f` must hold at some step within `n` `"milliseconds"`, `"seconds"`, or `"steps"` |
| `now(f)` | `f` evaluated at the current step (for use inside `always`/`next` bodies) |
| `next(f)` | `f` evaluated at the step immediately after the current one |

**Formula combinators** - available on every `Formula`:

| Method | Meaning |
|---|---|
| `.implies(other)` | If `this` holds, `other` must also hold |
| `.and(other)` | Both must hold |
| `.or(other)` | At least one must hold |
| `.not()` | Negation |

## Actions

### Constructors

```ts
Tap({ on: element | string })
DoubleTap({ on: element | string })
LongPress({ on: element | string })
InputText({ into: element | string, text: string })
Swipe({ from: element | Point, to: element | Point, durationMillis?: number })
Scroll({ direction: "up" | "down" | "left" | "right", in?: element | string })
PressKey({ key: Key })
Wait({ durationMillis: number })
```

`Key` values: `"back"`, `"home"`, `"enter"`, `"tab"`, `"up"`, `"down"`, `"left"`, `"right"`.

On web, `"back"` maps to Backspace and `"home"` is not supported. All other keys work on all platforms.

### Built-in generators

| Generator | Behaviour |
|---|---|
| `taps` | Random tap on a clickable element |
| `doubleTaps` | Random double tap on a clickable element |
| `longPresses` | Random long press on a clickable element |
| `typing` | Types a value from the edge-case corpus into a random editable field |
| `scrolls` | Random scroll gesture |
| `swipes` | Random swipe gesture |
| `waitOnce` | Idles one step |
| `pressKeys` | Presses a random supported key |

### `actions(generator)`

Wraps a callback that returns `Action[]`. The callback runs each step the generator is eligible.

```ts
const doLogin = actions(() => {
  if (loggedIn.current) return [];
  const submit = loginSubmit.current;
  return submit ? [Tap({ on: submit })] : [];
});
```

### `weighted(...entries)`

Assembles a weighted tree. Each entry is `[weight, generator]`. Weights are relative within the tree.

```ts
export const actionsRoot = weighted(
  [50, doLogin],
  [10, taps],
  [2,  swipes],
);
```

### `whenRoute(routeExtractor, routes, body)`

Builds a generator that runs `body` only when the extractor's current value is in `routes` (a string or array of strings). Returns an empty list otherwise.

```ts
const addTxn = whenRoute(route, ["home", "ledger", "add-transaction"], () => {
  ...
  return [Tap({ on: btn })];
});
```

### Samplers

Every sampler has `.generate()`. Draws are seeded by the run's PRNG, so a run replays identically from its seed.

| Sampler | Produces |
|---|---|
| `from(items)` | An item from a fixed list |
| `integers().between(min, max)` | An integer in the range |
| `strings().length(min, max).alpha()` | A random string; `.alpha()` restricts to letters |
| `emails().domain("example.com")` | A random email address |
| `edgeCaseText()` | A value from the adversarial input corpus (empty and whitespace strings, emoji, numeric boundary values, very long strings, injection payloads) |

```ts
const names = from(["Checking", "Savings", "Travel"]);
const amounts = integers().between(1, 500);
// inside an actions() callback:
InputText({ into: nameField, text: names.generate() })
InputText({ into: amountField, text: String(amounts.generate()) })
```

## Defaults

```ts
import { defaultActions, doubleTaps } from "@sanderling/spec/defaults";
import { noUncaughtExceptions, noLogcatErrors } from "@sanderling/spec/defaults/properties";
```

`defaultActions` is a ready-made weighted tree of the built-in generators: taps and typing at weight 100, scrolls 50, swipes 25, double taps 10. Use it as a baseline pool or as one entry in your own tree.

| Property | Fails when |
|---|---|
| `noUncaughtExceptions` | An uncaught exception or `Sanderling.reportError()` call is captured |
| `noLogcatErrors` | Logcat emits any error-level (`E`) lines since the previous step (Android only; holds elsewhere) |
