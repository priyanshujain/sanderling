---
name: sanderling-property-patterns
description: Decide what a sanderling spec should assert. A catalogue of property shapes that are sound (cross-panel agreement, bounds on an effect, counting actions against effects, input and navigation invariants), each with the tempting unsound version beside it. Use when starting a spec, when adding a property to one, or when a property keeps convicting an app that behaved.
---

# Choosing what to assert

You have sanderling driving your app and now you have to say what must be true.
This is the hard part, and it fails in two directions: you freeze, or you write
six properties none of which can ever be false.

One rule orders everything below. **Soundness outranks detection.** A property
that convicts more often and is sometimes wrong is strictly worse than one that
convicts less and is never wrong, because a false conviction costs someone a day
and then costs the whole suite its credibility. When a property cannot establish
what it needs, it declines.

Each shape below gives the sound form and the tempting form next to it, because
the tempting one is usually what gets written first. The examples are from the
two specs in this repo: `replay-ui/sanderling/spec.ts` (sanderling fuzzing its
own trace browser) and `examples/folio/sanderling/spec.ts` with
`examples/folio/sanderling/predicates.ts` (a KMP finance app).

Once you have written properties, run `sanderling-spec-review` over them. It
audits what this file helps you build.

## 1. Two parts of the UI derive the same fact and must agree

Reach for this first, always. If your app shows the same number in two places,
or shows a thing and a count of that thing, or renders a list and a selection
into that list, you have a property and you do not have to think about windows,
calibration, or attribution to write it.

It is the strongest shape available. It holds on any run against any data, so
nothing needs recalibrating when a fixture changes; it needs no reasoning about
which action caused what; and an app that drifted on one of the two paths cannot
satisfy it. It is the backbone of `replay-ui/sanderling/spec.ts`, which states it
three times over: the toolbar's step count against the number of rows the list
renders, the toolbar's step against the step the screenshot panel built its URL
from, and the tab badge's violation count against the number of rows the
violations panel shows.

```ts
const stepCountMatchesTheList = always(() => {
  const current = toolbar.current;
  const rows = stepRows.current;
  if (!current || current.stepCount === null || rows.length === 0) return true;
  return current.stepCount === rows.length;
});
```

**What goes wrong: reading the second value off the wrong element.** Scope each
reading to the panel you mean, by name, not by position in the tree.

```ts
// tempting: the first screenshot on the page
s.ax.find({ "data-testid": "screenshot" })
// sound: the before panel's screenshot
s.ax.find([{ "data-testid": "state-before" }, { "data-testid": "screenshot" }])
```

Both versions pass most of the time. The fuzzer put the before panel on another
tab, which left the after panel's image first on the page, and the first version
fired against a UI that was behaving correctly.

**What else goes wrong: never getting both panels on screen at once.** This
shape's failure mode is vacuity, not false conviction, which makes it quiet. An
undirected run over replay-ui went 40 steps without switching a single tab out
of roughly 15 clickable elements, leaving both tab-facing properties vacuously
true. The fix is in the action tree, not the property: give the action that
brings the second panel into view its own weight.

```ts
const switchATab = actions(() => {
  const tabs = tabElements.current;
  return tabs.length === 0 ? [] : [Tap({ on: from(tabs).generate() })];
});

export const actionsRoot = weighted(
  [25, switchATab],
  [25, defaultActions],
);
```

## 2. An effect must not exceed what the actions could have caused

When the app has an effect you can measure (money moved, rows added, a counter
climbed), state a bound on it rather than a prediction of it.

**Prefer an upper bound to an equality.** This is the single most valuable
sentence in this file.

Both of these must hold at a step where exactly one submit is in the window:

```ts
// sound: no more moved than that one submit could account for
Math.abs(currTotalBalance - prevTotalBalance) <= typedAmount
// tempting: it moved by exactly what I typed
Math.abs(currTotalBalance - prevTotalBalance) === typedAmount
```

The equality catches the same bug, a double submit moving the balance by twice
the typed amount, and it also convicts an app that behaved: a commit still in
flight when the reading was taken, a submit the app refused, a tap that never
landed. Each of those is a legitimate way for the balance not to have moved, and
under an equality each one obliges you to write a guard for it. You will miss
one. Under a bound none of them are violations in the first place, because the
app doing less than you expected is not the bug you are hunting.

`submitChangesBalanceByTypedAmount` in `examples/folio/sanderling/predicates.ts`
is written as the equality, and you can read its guard stack as the price of
that choice: a route gate, a confirmed-dispatch gate, a window gate, a
typed-amount-is-parseable gate, two null gates and three
`Number.isSafeInteger` gates, all before the comparison.

The bound has one precondition, and it is the same one as shape 3: it bounds the
effect by what the actions in the window could have caused, so the window has to
count every action that could cause the effect. Miss one and the bound is not a
bound.

## 3. Count the actions, not the amounts

The same bound, stated in counts. One action must not produce two effects.

```ts
const submitCommitsOneTransactionPerAction = always(
  next(() =>
    !committedTransactionsExceedSubmits({
      countsBefore: homeTxnCounts.previous ?? null,
      countsAfter: homeTxnCounts.current,
      submitsInWindow: submitsSinceCounts.current,
    }),
  ),
);
```

Reach for this whenever the effect is countable, because it beats the amount
version on every axis. No arithmetic on values the UI formatted and you parsed
back, no float precision to reason about, and it stays sound however wide the
window between two readings gets, since both sides accumulate over the same
window. In folio it is the property that does most of the detecting.

It has exactly two failure modes and both are about the window. Neither makes it
unsound. Both make it useless, quietly.

**The window has to close often enough to attribute anything.** The window opens
when you last read the fact and closes when you read it again, so a run that
wanders away from that screen accumulates actions on one side of the bound
without accumulating evidence on the other. Measured on a real run: it went from
step 19 to step 136 without returning to the screen the property reads, giving a
transaction rise of 15 against a window of 37 actions. 15 is not more than 37,
so nothing was reported. The same run also gave 4 against 7, 6 against 13, and 1
against 1. Sound throughout, detected nothing.

The fix is to read the fact somewhere the run visits often, and to weight the
actions that return there. A bound whose budget always exceeds its evidence is a
property you can delete.

**The window must not be spent on actions that provably could not cause the
effect.** Folio's transaction submit button is declared
`enabled = state.amount.isNotBlank()` (`AddTransactionScreen.kt`), so a tap on
it with an empty amount field commits nothing and must not consume budget.
Counting those taps was over half the window on real runs: 35 taps against a
real budget of 16, and 42 against 17. `countSubmitsInWindow` in
`predicates.ts` still counts them, which is why that number is worth checking
before you trust a green run of this property.

Establishing that an action could not have had an effect is app knowledge, not
something the runner can tell you. Here the field's own text at the moment of
the tap settles it, and the spec already reads it as
`txnAmountField.previous?.text`. `element.enabled` is on every
`AccessibilityElement` for the general case, though whether your platform
populates it honestly is something to verify on a real tree rather than assume.

The mirror of this rule matters just as much: an action whose effect you cannot
rule out **must** be counted. Leaving out submits whose dispatch the runner
could not confirm is what once convicted a healthy app here, when the property
saw a transaction rise of one against a window of zero.

## 4. A value the user can reach must stay inside its legal range

Anything the user can type, or a URL can carry, or a deep link can set, is
attacker-controlled input to your app even when the attacker is a fuzzer. The
property is that it stays legal, and it is cheap: one reading, no window, no
attribution.

```ts
const selectedStepIsInRange = always(() => {
  const current = toolbar.current;
  if (!current || current.step === null || current.stepCount === null) return true;
  return current.step >= 1 && current.step <= current.stepCount;
});
```

Two things make that sound. The bound comes from the app's own reading of how
many steps the run has, not from a number you typed after looking at a fixture.
And it asserts legality rather than a prediction:

```ts
// tempting: I tapped next, so it must now be on step n + 1
toolbar.current.step === (toolbar.previous?.step ?? 0) + 1
```

which is false at the end of the run, false when the tap did not land, and false
whenever the app is within its rights to clamp. Assert what must not happen.

## 5. State machine and navigation invariants

Every screen with a selection, a mode, or a route has invariants that are true
by construction and therefore worth stating, because "by construction" is
exactly what breaks.

**Exactly one, not at least one.** The looser version is the tempting one and it
gives up the interesting half of the bug.

```ts
const exactlyOneStepIsSelected = always(() => {
  const rows = stepRows.current;
  if (rows.length === 0) return true;
  return rows.filter((row) => row.active).length === 1;
});
```

Two selected rows is a stuck selection. Zero is the toolbar showing a step the
list has no row for, which is what an off-by-one or a failed clamp looks like
from the list's side, and `>= 1` would never see it.

**A view change must not be a navigation.** Switching a tab, opening a menu or
toggling a theme must leave the app where it was.

```ts
const switchingTabsKeepsTheStep = always(
  next(() => {
    const previousTabs = activeTabs.previous;
    const previousToolbar = toolbar.previous;
    const currentToolbar = toolbar.current;
    if (previousTabs === undefined || previousTabs === activeTabs.current) return true;
    if (!previousToolbar || !currentToolbar) return true;
    return previousToolbar.step === currentToolbar.step;
  }),
);
```

Note the guard: it declines unless the tab strip actually changed. A property
about an event must first establish that the event happened.

The tempting unsound version of a navigation property is asserting the route you
were hoping for, `route.current === "home"` after tapping submit. The app is
within its rights to show a validation error and stay, and folio does exactly
that for an amount of zero. State what must not happen, not what you wanted to.

Deriving the route at all deserves care, and folio's `routeOfFrame` is the
pattern: it returns the screen only when exactly one screen marker is in the
tree, and null otherwise. Android's hierarchy dump carries the outgoing and the
incoming screen together on 425 of 1879 steps measured across 17 runs, better
than one frame in five. Such a frame is evidence about neither screen, and
ranking the markers to pick one is how a spec convicts itself on an animation.

## 6. The ones you get for free

```ts
import { noUncaughtExceptions, noLogcatErrors } from "@sanderling/spec/defaults";

export const properties = { noUncaughtExceptions, /* yours */ };
```

Export `noUncaughtExceptions` before you write anything of your own. It costs a
line, it needs no app knowledge, and a fuzzer typing `'; DROP TABLE--` and a
4096-character string into every field it finds will surface real breakage
through it. `noLogcatErrors` is stricter and Android-only; it holds trivially
elsewhere, so it is worth turning on once you know your app's log hygiene can
support it.

They do not substitute for the shapes above. An app can be thoroughly wrong
about money without throwing once.

## The rules that cut across all of them

**Absence is unknown, never a default.** Extractors return null when the element
is not there, and a property handed null declines. `0`, `""` and `[]` are the
values that turn a property into one that fires on healthy runs: folio's
balances once parsed as `0` on web, so the check became `|0 - 0| === typed` and
was false at every healthy submit. An empty list has the same problem in the
other direction, and it is worse because it looks reasonable. Android renders
Home's own node a frame or two before its list, so `findAll` over the cards
comes back empty while the screen already claims to be Home. That is unknown,
not "no accounts", and reading it as zero accounts killed folio's counting
invariant outright: `countsBefore` was `{}` at every evaluation point of all 17
runs measured.

**Attribution needs injective keys.** If two distinct objects can produce the
same identity key, a value silently jumps between unrelated series. Merged UI
text is the usual culprit: web collapses an account card into a single node
whose text runs the name into the count, so an account named `Travel1` with 25
transactions and one named `Travel12` with 5 both render `TRTravel125
transactions`. No function of that string can separate them. Where a key can
collide, drop the reading rather than guess: `homeTxnCountsOf` leaves out any
name carried by more than one card, because subtracting two different accounts'
counts convicts a healthy app of double-submitting.

**Match whole keys, not endings.** `endsWith` attribution judges an older
account named `Emergency Fund` when the user typed `Fund`, and substring
matching is looser still.

**What the runner could not promise.** `state.lastAction` distinguishes three
things and collapsing them is unsound:

- `null` means no action ran
- `applied: true` means the runner saw the dispatch succeed
- `applied: null` means it was dispatched and nobody can find out whether it
  landed, because an RPC deadline can fire after the tap arrived

The rule follows the shape of the property. An action of unknown fate still
counts toward a **bound on what the app could have done**, and it never licenses
attributing an effect **to** it. So a bound counts it and an equality must
decline on it, which is the same reason shape 2 prefers bounds: a property
demanding the effect of an action that may never have run convicts the app of
the runner's own uncertainty.

Note the one event that removes an action from a window without removing its
effect: when the app leaves the foreground the runner relaunches it and drops
the pending `lastAction`, while any effect that action already committed to
persisted storage survives the restart. A carrier you hold across steps in a
module-level variable does not know a restart happened either. If a property
compares readings across an interval that a relaunch can sit inside, that is the
gap to think about.

**Testing a property means both directions, every time.**

- it fires on the bug it exists to catch
- it stays silent on a run where the app behaved

The second is the one people skip and the one that catches unsoundness. Build
the fixture where the effect happens legitimately, at the boundary the property
draws, and assert silence: the commit that is still settling, the submit the app
refused, the card that scrolled into view rather than being created. A property
you have only ever seen go red is a property you have half tested.

Then hand it to `sanderling-spec-review`, which will ask how many steps it
actually judged on a real run.
