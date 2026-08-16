---
title: Spec language reference
---

# Spec language reference

Lookup reference for everything importable from `@sanderling/spec`. For a worked example, read the [case study](../case-study/) first.

## Module structure

A spec is a TypeScript module evaluated by the Go runner each step. It exports `properties` and `actionsRoot`, plus an optional `setup` and `generator`:

```ts
import { ... } from "@sanderling/spec";

export const properties = { ... };
export const actionsRoot = weighted(...);
export const setup = login;        // optional
export const generator = llm(...); // optional, see below
```

`setup` is an `ActionGenerator` the runner consults before `actionsRoot` each step. While it returns actions, they run; when it returns an empty list, the runner falls through to `actionsRoot`. Use it for preconditions like login and onboarding. If the app later regresses across the precondition (a logout mid-run), `setup` re-engages on its own.

## State

Every extractor callback receives a `State`:

```ts
interface State {
  ax: AccessibilityTree;
  snapshots: Record<string, unknown>;
  lastAction: (Action & { applied: true | null }) | null;
  logs: readonly LogEntry[];
  exceptions: readonly ExceptionRecord[];
  time: number;   // ms since run start
}
```

| Field | Description |
|---|---|
| `ax` | Live UI hierarchy for this step |
| `snapshots` | Key-value data pushed by the app SDK (empty if SDK not integrated) |
| `lastAction` | The action dispatched in the previous step, or `null` on the first step and on any step that dispatched nothing |
| `logs` | Log entries collected since the previous step |
| `exceptions` | Uncaught exceptions and unhandled promise rejections captured in the page. Web only: nothing fills this on Android or iOS, where it is always empty |
| `time` | Milliseconds elapsed since the run started |

`lastAction.applied` is `true` when the runner saw the dispatch succeed and `null` when the apply call failed with the action possibly already delivered: an RPC deadline can fire after the tap reached the app, and nothing can find out afterwards. So there are three states, not two. `state.lastAction === null` means no action ran; `applied === null` means one ran whose fate is unknown. A property that attributes an effect to the action ("this submit must move the balance by the typed amount") has to decline unless `applied` is `true`, or a timeout convicts a healthy app. A property that counts what the app COULD have done should include it: an unconfirmed submit belongs in an upper bound on how many submits a window holds.

## Selectors

Selectors are passed to `ax.find()`, `ax.findAll()`, and element-scoped `.find()` / `.findAll()`. A tree-level lookup scans the whole hierarchy, the root element included; an element-scoped one scans that element's descendants. Both selector forms scan the same set, so `ax.find("id:page")` and `ax.find({ id: "page" })` return the same element.

### String selectors

| Form | Matches |
|---|---|
| `id:<value>` | Exact match on resource-id, or element whose resource-id ends with `:id/<value>` (Android) |
| `idPrefix:<prefix>` | Starts-with match on resource-id, matched against the whole id and against the local name after `:id/` (Android) |
| `text:<value>` | Substring match on text content, innermost match only |
| `desc:<value>` | Exact match on accessibility description; also matches when description starts with `<value>, ` (iOS merged labels) |
| `descPrefix:<prefix>` | Starts-with match on accessibility description |
| `<attr>:<value>` | Substring match on any raw attribute by name |

Boolean attributes (`"true"` / `"false"`) use exact match rather than substring.

`text:` names the innermost match. An element's text is its whole subtree's text on web and on iOS, so a badge reading "Sent ✓" makes every ancestor of it read as a match too, up to the root. A match a descendant of it also makes is dropped, which leaves the deepest element carrying the value: `ax.find("text:Sent")` lands on the badge, and `ax.findAll("text:Sent")` returns the badges without their ancestors. An ancestor whose own text carries the value where no descendant of it does keeps its match. `{ text: "Sent" }` means the same thing.

### Object selectors

Pass an object to apply multiple attribute filters with AND semantics:

```ts
s.ax.find({ accessibilityText: "LoginScreen" })
s.ax.find({ testTag: "AccountCard", clickable: true })
```

Every key-value pair must match. A key means the same thing here as in the string form: `id`, `desc`, `idPrefix` and `descPrefix` keep their matching rules, and every other key is an attribute name, with substring and boolean rules per attribute.

Known attribute names are typed; you get autocomplete on `testTag`, `text`, `content-desc`, the boolean states (`clickable`, `enabled`, `focused`, `checked`, `selected`), and the cross-platform aliases (`identifier`, `accessibilityIdentifier`, `accessibilityText`, `accessibilityLabel`, `label`, `resource-id`, `class`, `elementType`, `package`, `placeholderValue`, `hintText`). Boolean state attributes accept a native `true` / `false`. Other attribute keys still type-check as a string-valued fallback so raw driver attributes remain reachable.

A key that names neither an accepted selector key nor an attribute some element on screen carries fails the run, naming the key and the accepted list. Such a key can never match, and an empty result is indistinguishable from a screen with no matching element: the generator declines to act, the runner waits out the step, and the run ends clean having explored nothing. The string form keeps its open kind space, since `<attr>:<value>` is the documented way to reach a raw driver attribute.

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
- `attrs` contains all HTML attributes available to CDP, keyed by the name the
  markup writes (`attrs["data-cents"]`, not `attrs.cents`).
- `attrs.hintText` names an editable field the way a user reads it: its
  `aria-label`, the `<label>` bound to it, its `placeholder`, then its `name`.
  The `hintText` selector key still matches the `placeholder` attribute alone.

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

An extractor may return elements. The trace records their data (id, text, desc, class, the flags, bounds, attrs) and not their `find`/`findAll` methods; a cycle, a NaN or a branch nested deeper than 32 levels is recorded as null. A value with no JSON form at all fails the step and names the extractor, so nothing goes unrecorded without the author hearing about it.

## LTL operators

| Function | Meaning |
|---|---|
| `always(f)` | `f` must hold at every step |
| `eventually(f).within(n, unit)` | `f` must hold at some step within `n` `"milliseconds"`, `"seconds"`, or `"steps"` |
| `now(f)` | `f` evaluated at the current step (for use inside `always`/`next` bodies) |
| `next(f)` | `f` evaluated at the step immediately after the current one |

A property that is an `eventually` at the top level is one goal for the whole run: it is armed at the first step and discharged for good the first time it holds. Written inside `always(...)` the same formula is armed again at every step, which asks for the window to be met from every step in the run.

### Choosing a bound unit

`"milliseconds"` and `"seconds"` bound the window in wall-clock time, measured from
the step that armed the obligation. Use them when the deadline is about what a user
would sit through: a spinner that has to clear, a screen that has to appear.

`"steps"` bounds the window in observed steps instead, so the same window costs the
same however long each step took. Use it for anything compared across runs that move
at different speeds, such as two action-selection policies given the same step budget,
where a wall-clock window fails the slower one on elapsed time rather than on the app
misbehaving.

An observed step is a step the verifier evaluated. A step it skipped, a transitional
tree or an empty hierarchy, never reached the property and so does not consume the
window; the trace marks those steps with `skippedVerification`. The observed-step
count is therefore at most the runner's step number, and a step-bounded residual
records both: `within` carries the window the spec authored plus
`expiresAtObservation`, the observed step the obligation comes due at.

The unit does not change what an undischarged obligation means. An `eventually` that
never fires is a violation at the end of the run whichever unit bounded it, and a
window that never closed before the run ended is still a broken promise.

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

`Key` values: `"back"`, `"home"`, `"enter"`, `"tab"`, `"escape"`, `"up"`, `"down"`, `"left"`, `"right"`.

Android sends all nine. Web sends every key except `"back"` and `"home"`, which have no in-page meaning. iOS sends `"enter"` and `"escape"`. A key the platform cannot send fails the action with an error rather than pressing nothing.

### Built-in generators

| Generator | Behaviour |
|---|---|
| `taps` | Random tap on a clickable element |
| `doubleTaps` | Random double tap on a clickable element |
| `longPresses` | Random long press on a clickable element |
| `typing` | Types a value from the edge-case corpus into a random editable field |
| `scrolls` | Scrolls a random scrollable container up or down |
| `swipes` | Random up, down, left, or right swipe from any visible element |
| `waitOnce` | Idles one step |
| `pressKeys` | Presses a random supported key |

`scrolls` anchors on a scrollable container and moves its content up or down. It never
scrolls sideways, because every scrollable container on screen gets a candidate and a
sideways pair doubles that list for little return; write `Scroll({ in, direction })` when
you need one.
`swipes` is a free drag of 200 to 600 px from any element with real bounds, in any of the
four directions. The sideways ones are what reach swipe-to-dismiss and swipe-to-delete on
a list row.

On a touch device the two verbs are one gesture: a Scroll is dispatched as a drag. A
browser is not a touch device, so `Scroll` there is a wheel over the point, which moves the
container under it by exactly the distance asked for, and `Swipe` is a touch drag, which
reaches the handlers only a finger reaches and carries a drag's momentum. Reach for `Swipe`
when a container scrolls by handling the drag itself rather than by overflowing.

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

## LLM generator

By default the run's PRNG picks each action. `--generator llm` swaps out the picker for a vision model and nothing else: same spec, same `actionsRoot`, same weights, same actions. Add the export and pick a model.

```ts
export const generator = llm({
  model: "gpt-5.4-nano",
  instructions: "Folio is a personal-finance ledger app. The home screen lists accounts with balances; you can open an account and add transactions.",
});
```

Set `OPENROUTER_API_KEY` or `OPENAI_API_KEY` (OpenRouter wins if both are set). With a plain OpenAI key, drop the vendor prefix from the model id. The model needs image input and strict `json_schema` structured output.

Each step it gets a screenshot plus a numbered list of the concrete actions your tree yields right now, each tagged with its weight, and picks one number. That list is the seeded picker's own candidate enumeration, so both modes explore the same action space and only the choice differs. `instructions` are appended to the prompt: say what the app is, not how to test it; the model works that part out. Everything else is unchanged. Setup actions still run first, typing still falls back to the edge-case corpus when the model supplies no text, and the trace records the reasoning, the chosen number, and `source: "llm"` so the replay UI can show why each pick happened.

One thing this mode refuses outright: a sampler drawn inside an `actions()` leaf. `from(...).generate()` draws from the seeded picker's stream, which the model never enters, so it would be offered the first item on every step while a seeded run reaches all of them. The run stops and names the leaf rather than degrade quietly. Return one action per item instead (`cards.map(card => Tap({ on: card }))`); a one-item list never draws and needs no change.

The value generators draw from that same stream, so they refuse there too: an authored `InputText({ into: field, text: String(amounts.generate()) })` would type one fixed value on every model step while the seeded arm varies it, which is a different experiment rather than a different action space. Pass a fixed value instead. A generator whose span is a single value (`integers().between(7, 7)`, `strings().length(0, 0)`) never draws and is left alone, and `setup` is untouched: it runs through the picker with its seed under both generators, so a sampler there keeps drawing.

Every step of a model-driven run also appends one record to `llm-calls.jsonl` in the run directory, keyed by the step index it shares with `trace.jsonl`: the system and user prompts as sent, the numbered candidate list as the model saw it, the path of the screenshot that went with the call, the raw response, token counts, latency, and an `outcome`. `outcome` is `selected` when the pick became an action; every other value (`echo_mismatch`, `choice_out_of_range`, `unparsable_response`, `request_failed`, `no_candidates`, `setup_action`, ...) names a step that ran no model-chosen action, so a step a guard threw away can never be mistaken for one where the model had nothing to pick. A seeded run writes no such file.

It is one model call per step, so keep `--duration` modest.

## Defaults

```ts
import { defaultActions, doubleTaps } from "@sanderling/spec/defaults";
import { noUncaughtExceptions, noLogcatErrors } from "@sanderling/spec/defaults/properties";
```

`defaultActions` is a ready-made weighted tree of five of the built-in generators: taps and typing at weight 100, scrolls 50, swipes 25, double taps 10. `longPresses`, `pressKeys` and `waitOnce` are not in it; weight them in yourself if you want them. Use it as a baseline pool or as one entry in your own tree.

| Property | Fails when |
|---|---|
| `noUncaughtExceptions` | The page captured an uncaught exception or an unhandled rejection (web only; holds on Android and iOS, where `state.exceptions` is never populated) |
| `noLogcatErrors` | Logcat emits any error-level (`E`) lines since the previous step (Android only; holds elsewhere) |
