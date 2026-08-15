---
name: sanderling-spec-authoring
description: Write a sanderling spec for an app: test hooks, extractors, selectors, properties, and an action tree that reaches the states the properties read. Use when adopting sanderling for a new app, when adding a property to an existing spec, and when a run is green because it never reached the state the property was written for.
---

# Writing a sanderling spec

A spec is a TypeScript module the runner evaluates once per step. It exports
`properties` and `actionsRoot`, plus an optional `setup` and `generator`.
Writing one is easy. Writing one that would catch a real bug is not, because a
spec that checks nothing looks exactly like a spec that checks everything: both
are a green run.

So the order below is arranged around getting evidence early that each piece
reads what you think it reads. When the spec is written, audit it with
`sanderling-spec-review` before trusting a green run from it.

## Write it in this order

Hooks, then extractors, then **one** property, then run it and read the witness,
then everything else. Writing six properties before the first run is how people
end up with six that cannot fire, and nothing in the output tells you which.

## Hooks first

Your app needs stable handles or nothing can name what it is asserting on. The
hooks the replay UI's spec drives (`data-testid`, `data-step`) were added to the
UI for that spec, and its header says why: a UI with no stable handles is a UI
nothing can assert on, and that is as true for a person writing a test as it is
for a fuzzer.

Put a hook on the screen or route markers, on every container you will scope a
lookup to, and on every fact you will read. `testTag` is the portable one: it
surfaces as resource-id on Android and accessibilityIdentifier on iOS, and on
web it resolves to `data-testid` or `id`.

## Extractors

`extract(name, fn)` reads one fact off `state.ax` per step. `.current` is this
step's value, `.previous` the last step's, `undefined` on the first step.

Name every one. The name is what you get back later: `extractor_changes` in
`trace.jsonl` carries the prev/curr pair for each extractor whose value moved,
and the witness recorded at a violation carries the extractor values behind it,
by name. An unnamed extractor shows up as `extractor_3`, which tells you nothing
at the point you most need to know what the property was looking at.

The rule that decides whether the spec is worth anything:

> **Return `null` when the element is absent. Never `0`, `""`, or `[]`.**

An unreadable fact is unknown, and a default turns unknown into a claim. Both
directions bite. Folio parsed a missing balance as `0` and its property became
`Math.abs(0 - 0) === typedAmount`, false at every healthy submit. Read a missing
panel's row count as `0` and the property says the cart is empty when the truth
is that the cart is not on screen. Where the ambiguity is real, call it unknown:
an empty `findAll` is both "no rows" and "not drawn yet", and folio treats it as
unknown, which costs the very first account of a run and buys back every card
that arrived late.

Extractors run before properties and action generators, and they may not read
each other. If two readings must come off one parse, put the parse in a helper
both call: `examples/folio/sanderling/predicates.ts` does this with
`oncePerFrame`, keyed on the state object, since both hosts build a new state
object per step.

## Selectors

`ax.find` and `ax.findAll` take a string (`"id:CartBadge"`), an object
(`{id: "CartBadge"}`), or an array of objects for a path. Element handles carry
their own `.find` / `.findAll` scoped to their subtree.

The two forms resolve identically: an object key is matched by the same rule its
string form uses, so `{id: "X"}` and `"id:X"` can never pick different elements.
What differs is the rule per key. Measured against an Android dump holding
`com.app:id/CartBadge`, whose content-desc is `Cart, 3 items`, alongside
`AddAccountSubmit`:

| Selector | Resolves to |
|---|---|
| `{id: "CartBadge"}` | the badge: `id` matches the whole resource-id, or the part after `:id/` |
| `{id: "Sub"}` | nothing: `id` wants a whole name, not a fragment |
| `{testTag: "CartBadge"}` | the badge: `testTag` reaches resource-id on Android and accessibilityIdentifier on iOS |
| `{testTag: "Sub"}` | `AddAccountSubmit`, because every key outside the `id` / `desc` / `descPrefix` special cases is a **substring** match |
| `{desc: "Cart"}` | the badge: `desc` takes the whole description, or an iOS merged label starting `Cart, ` |

`testTag` is the portable key and the one to reach for, but name the element in
full. A substring match on `Sub` is not a match, it is a coincidence, and it
will one day pick a different control.

A selector that matches nothing makes every property over it vacuous, and
nothing anywhere reports that. This is why the selector you verify is the one
you found a real value for in a witness, not the one that looked right when you
wrote it.

**Scope the lookup to a container instead of taking the first match on the
page.** From `replay-ui/sanderling/spec.ts`: an earlier draft of
`screenshotShowsTheSelectedStep` took the first screenshot on the page, the
fuzzer put the "before" panel on another tab, which left the "after" panel's
image first, and the property fired against a UI that was behaving correctly. It
now reads `s.ax.find([{ "data-testid": "state-before" }, { "data-testid": "screenshot" }])`
and is scoped to the panel it means.

A screen marker is not enough scope on its own during a navigation. Android's
hierarchy dump carries the outgoing and the incoming screen together on 425 of
1879 steps measured across 17 runs, better than one frame in five, so a find
scoped to a screen the app has already left still resolves. Decide the route once
per frame, return `null` when more than one screen marker is present, and have
every reading take its answer from there. `routeOfFrame` in folio's
`predicates.ts` is that rule and carries the measurements.

## Properties

`always(f)` requires `f` at every step. `next(f)` inside it compares this step
to the next, which is how you state "this action had that effect". `now(f)`
evaluates at the current step inside a formula body.
`eventually(f).within(n, "steps" | "seconds" | "milliseconds")` requires `f`
before the window closes and convicts at the step it does not. Unbounded, it
does not stop being a liveness obligation: one that never fires is violated when
the run ends, with the reason `eventually never satisfied`. So an `eventually`
over a state your run may not reach fires on every run that does not reach it,
and that is the usual way a first spec ends up red for no reason. At the top
level an `eventually` is one goal for the whole run, armed once and discharged
for good the first time it holds; written inside `always` it re-arms at every
step, which asks for the window to be met from everywhere. Every formula has
`.implies`, `.and`, `.or`, `.not`.

The stock properties are in `@sanderling/spec/defaults`. Both are cheap and both
are narrower than their names suggest, so know which platform yours runs on.

`noUncaughtExceptions` fails when `state.exceptions` is non-empty, and today only
the web runtime fills it: `pkg/spec/src/web-runtime.ts` installs `error` and
`unhandledrejection` listeners in the page. On Android and iOS nothing populates
the field, so it holds at every step whatever the app does. Export it on web,
where it is free and real; on native, understand that a green run says nothing
about crashes.

`noLogcatErrors` fails on any log line the driver reports at level `E`, which is
where an uncaught Java or Kotlin throwable lands, so on Android it is the closest
thing to `noUncaughtExceptions`. It holds vacuously on web and iOS. Neither
platform has an equivalent today: an iOS crash is invisible to both properties.

## What makes a good first property

Prefer a **cross-panel agreement**: two parts of the UI that derive the same fact
by different paths must say the same thing. The toolbar prints a step count and
the list renders rows; a badge counts violation records and the panel counts the
rows it can show for them. Those hold on any run, so they never need
recalibrating against a fixture, and an app that gets the fact wrong in one of
the two places cannot satisfy them however it was driven there.
Three of the seven properties in `replay-ui/sanderling/spec.ts` are this shape:
`stepCountMatchesTheList`, `screenshotShowsTheSelectedStep` and
`badgeCountMatchesThePanel`. The rest of that spec shows what to write when no
second panel derives the fact: a range invariant on user input
(`selectedStepIsInRange`), a counting invariant inside one panel
(`exactlyOneStepIsSelected`), a no-effect property across an action
(`switchingTabsKeepsTheStep`), and the stock `noUncaughtExceptions`. All of them
still hold on any run, which is the property worth keeping.

Contrast a property that needs the fuzzer to reach a specific state, like
folio's "a submit moves the balance by no more than the amount typed". That is where
the real bugs are, and it is the harder thing to keep honest: it needs an action
tree that reaches the state, a window that closes often enough to bound what
happened inside it, and attribution that cannot blame the wrong action. Folio's
counting form went 117 steps between two readings on one iOS run and gathered 37
submits against a rise of 15 transactions, which is perfectly sound and says
nothing at all; the fix was to state the same rule over a number the app redraws
on nearly every frame, so the window is usually one action wide. Write these
second, and read `sanderling-spec-review` before you believe one.

Whichever you write, name the input that makes it return false before you move
on. If you cannot, it is decoration.

## Actions

`actions(() => Action[])` returns the candidate actions for this step and the
picker chooses one. The verbs are `Tap`, `DoubleTap`, `LongPress`, `InputText`,
`Scroll`, `Swipe`, `PressKey`, and `Wait`. The built-in generators are `taps`,
`doubleTaps`, `longPresses`, `typing`, `scrolls`, `swipes`, `pressKeys`, and
`waitOnce`. `defaultActions` bundles five of them: taps and typing at 100,
scrolls 50, swipes 25, double taps 10. `longPresses`, `pressKeys` and `waitOnce`
are not in it, so a spec that only exports `defaultActions` never presses android
back, never long-presses, and never waits. Weight those in yourself if the app
has behaviour behind them.

`weighted([n, generator], ...)` composes them with relative weights.
`whenRoute(routeExtractor, routes, body)` runs `body` only on the named screens.
The optional `setup` export runs before `actionsRoot` for as long as it returns
actions, which is where login and onboarding belong; it re-engages on its own if
the app logs itself out mid-run. Values come from `from(items)`,
`integers().between(min, max)`, `strings().length(min, max).alpha()`,
`emails().domain(host)`, and `edgeCaseText()`, all drawn from the run's seeded
PRNG so a seed replays exactly.

**The default enumeration explores, but reaching a specific interesting state
usually needs a weighted action of your own.** With about 15 clickable elements
on the replay UI's page, an undirected run went 40 steps without switching a
single tab, which left both tab-facing properties vacuously true. Its badge
agreement is worse: it needs two readings on one step, a badge, which only a
step that has a violation renders, and a violations panel to compare it against.
Undirected actions put both on the same step **0 times in the 80 steps of the
first dogfood run**. The property was reachable in principle and judged nothing
in practice, and aiming at the step alone just moved the misses to the other
side, 0 judged either way. It took an action that selects a violating step and
then opens a panel if none is up. Folio weights its transaction chain at 45 for
the same reason: both balance properties observe that flow and nothing else
reaches it.

So for every property, name the action in the tree that puts everything it reads
on screen at the same step. If there is none, add one, and give it enough weight
that a short run gets there.

## Soundness outranks everything else here

A property must never convict an app that behaved correctly. A property that
convicts more often and is sometimes wrong is strictly worse than one that
convicts less and is never wrong, because a false conviction costs someone a day
and then costs the whole suite its credibility. **When in doubt, a property
should decline to judge.**

Declining costs at most a detection. Convicting a healthy app costs the spec.
Concretely that means unknown stays `null`, a bound is preferred to an equality
where the window can hold more than one cause, and a value carried across a
screen change is dropped rather than compared.

It also means reading `state.lastAction` for what it actually promises, which is
three different things and not one:

- `state.lastAction === null`: no action ran.
- `applied: true`: the runner saw the dispatch succeed.
- `applied: null`: it was dispatched and nobody knows whether it landed.

The rule is short. **An action of unknown fate still counts toward bounds on
what the app could have done, but it never licenses attributing an effect to
it.** Leave it out of the bound and you convict a healthy app: folio saw a
transaction rise of one against a window of zero submits and called it a double
submit. Demand its effect and you convict the app of the runner's own
uncertainty.

`relaunched: true` says the runner had to bring the app back to the foreground
after the action, so the two readings straddle a restart. The action still
happened and still counts toward the bound, but nothing about state running
continuously between the two readings survives it, and a property demanding that
action's effect has to decline. Like `applied`, its null is "not reported", not
"the app never restarted": web and iOS cannot read the foreground at all, so only
an explicit `true` licenses declining.

## Run it, then read the witness

**Write one property, run it, and read the witness before you write the second.**
This is the step that gets skipped and it is the one that pays. A spec that has
never had its readings confirmed against a real run looks exactly like a spec
that has, right up until you find out that an extractor reads `null` on the
platform you care about, or that a selector matches nothing, or that the value
being compared is not the value you thought.

```sh
sanderling test --spec spec.ts --bundle-id com.example.app --platform android --duration 2m
sanderling replay
```

Confirm two things before adding anything. First, that each extractor holds a
real value at some step, by finding it in `extractor_changes` in `trace.jsonl`;
the replay UI's hierarchy panel separately tells you whether the element your
selector names is in the tree at all. Second, that the property actually
compared values on some step rather than short-circuiting on its own guard. A
green run is evidence only if you can point at a step where a property fired.

Only then write the next property.

Once a property is worth keeping, its logic is worth testing away from the
device. Folio keeps its predicates in a plain module and unit-tests them in
`pkg/spec/test/folio-*.test.ts`, run by `make test-spec-api`; the app's own
Kotlin tests are `make test-folio`. Those files are the model for testing a
property in isolation, including the direction people skip: a fixture where the
effect happens legitimately, asserting that the predicate stays silent.

## A spec to adapt

Complete and self-contained: a storefront whose header badge and cart panel both
know how many things are in the cart.

```ts
import { InputText, Tap, actions, always, extract, from, integers, weighted } from "@sanderling/spec";
import { defaultActions, noUncaughtExceptions } from "@sanderling/spec/defaults";

function wholeNumber(text: string | undefined): number | null {
  if (!text) return null;
  const parsed = Number(text.trim());
  return Number.isInteger(parsed) ? parsed : null;
}

// The header badge: the app's own count of what is in the cart.
const badgeCount = extract("badgeCount", s =>
  wholeNumber(s.ax.find({ testTag: "CartBadge" })?.text));

// The same fact by another path: the rows the cart panel renders. No panel is
// null rather than 0, because nothing on screen is a fact we do not have, and
// 0 would claim the cart is empty.
const cartRowCount = extract("cartRowCount", s => {
  const panel = s.ax.find({ testTag: "CartPanel" });
  return panel ? panel.findAll({ testTag: "CartRow" }).length : null;
});

const checkoutEnabled = extract("checkoutEnabled", s => {
  const button = s.ax.find({ testTag: "CheckoutButton" });
  return button ? button.enabled === true : null;
});

// Two parts of the UI count the cart by different routes through the app's own
// state, so they cannot disagree about how many things are in it.
const badgeMatchesTheCart = always(() => {
  const badge = badgeCount.current;
  const rows = cartRowCount.current;
  if (badge === null || rows === null) return true;
  return badge === rows;
});

// Checkout is offered exactly when there is something to check out.
const emptyCartCannotCheckOut = always(() => {
  const rows = cartRowCount.current;
  const enabled = checkoutEnabled.current;
  if (rows === null || enabled === null) return true;
  return rows > 0 || !enabled;
});

export const properties = {
  noUncaughtExceptions,
  badgeMatchesTheCart,
  emptyCartCannotCheckOut,
};

const productCards = extract("productCards", s => s.ax.findAll({ testTag: "ProductCard" }));
const cartButton = extract("cartButton", s => s.ax.find({ testTag: "CartButton" }));
const quantityField = extract("quantityField", s =>
  s.ax.find([{ testTag: "CartPanel" }, { testTag: "QuantityField" }]));

const addAProduct = actions(() => {
  const cards = productCards.current;
  return cards.length === 0 ? [] : [Tap({ on: from(cards).generate() })];
});

const openTheCart = actions(() => {
  const button = cartButton.current;
  return button ? [Tap({ on: button })] : [];
});

const quantities = integers().between(1, 5);

const changeAQuantity = actions(() => {
  const field = quantityField.current;
  return field ? [InputText({ into: field, text: String(quantities.generate()) })] : [];
});

// Both properties read the cart panel, so a run that never opens it judges
// nothing. defaultActions carries the rest of the app.
export const actionsRoot = weighted(
  [35, addAProduct],
  [25, openTheCart],
  [15, changeAQuantity],
  [25, defaultActions],
);
```

Both properties here decline whenever the panel is off screen, which is honest
and also the thing to measure first: if `openTheCart` never wins the draw, they
judge nothing, exactly like the replay UI's badge property did for 80 steps.

The two real specs in the repo are the fuller references.
`replay-ui/sanderling/spec.ts` is the cross-panel spec written the way this page
recommends. `examples/folio/sanderling/spec.ts` with its `predicates.ts` is the
harder kind, a spec that attributes effects to actions across screens, and every
comment in it records a way it was once wrong. `docs/manual/spec-language.md` is
the lookup reference for anything not covered here.
