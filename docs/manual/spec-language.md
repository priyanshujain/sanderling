---
title: Spec language reference
---

# Spec language reference

## Module structure

A spec is a TypeScript module evaluated by the Go runner each step. It must export `properties` and `actionsRoot` on `globalThis` (the bundler entry point does this automatically via the final two lines):

```ts
import { ... } from "@sanderling/spec";

export const properties = { ... };
export const actionsRoot = weighted(...);

(globalThis as { actions?: unknown }).actions = actionsRoot;
(globalThis as { properties?: unknown }).properties = properties;
```

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

### Path queries

Chains of string selectors separated by ` > ` scope each segment to the subtree of the previous match. Path queries are only supported on the tree root (`ax.find`, `ax.findAll`), not on element-scoped `.find`/`.findAll`.

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
- `desc` maps to `accessibilityText`, which Maestro builds by merging `accessibilityLabel` and the element's value (e.g., `"Close, icon description"`). The `desc:<value>` selector handles this by also matching when the description starts with `<value>, `.
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
loggedIn.current    // T - value from the current step
loggedIn.previous   // T | undefined - value from the previous step, undefined on first step
```

Extractors are evaluated before properties and action generators. Use `.previous` to detect transitions between steps.

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
InputText({ into: element | string, text: string })
Swipe({ from: element | Point, to: element | Point, durationMillis?: number })
PressKey({ key: Key })
Wait({ durationMillis: number })
```

`Key` values: `"back"`, `"home"`, `"enter"`, `"tab"`, `"up"`, `"down"`, `"left"`, `"right"`.

On web, `"back"` maps to Backspace and `"home"` is not supported. All other keys work on all platforms.

### Built-in generators

| Generator | Behaviour |
|---|---|
| `taps` | Random tap on a clickable element |
| `swipes` | Random swipe gesture |
| `waitOnce` | Idles one step |
| `pressKey` | Presses a random supported key |

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

### `from(items)`

Returns a `Sampler<T>` that cycles through a fixed list. Use `.generate()` to pick an item.

```ts
const names = from(["Checking", "Savings", "Travel"]);
// inside an actions() callback:
InputText({ into: nameField, text: names.generate() })
```

## Default properties

```ts
import { noUncaughtExceptions, noLogcatErrors } from "@sanderling/spec/defaults/properties";
```

| Property | Fails when |
|---|---|
| `noUncaughtExceptions` | An uncaught exception or `Sanderling.reportError()` call is captured |
| `noLogcatErrors` | Logcat emits any error-level (`E`) lines since the previous step |
