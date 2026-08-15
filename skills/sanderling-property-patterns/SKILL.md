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

**What else goes wrong: never getting both readings onto one step.** This
shape's failure mode is vacuity, not false conviction, which makes it quiet. An
undirected run over replay-ui went 40 steps without switching a single tab out
of roughly 15 clickable elements, leaving both tab-facing properties vacuously
true. The fix is in the action tree, not the property: give the action that
brings the second reading into view its own weight.

```ts
const switchATab = actions(() => {
  const tabs = tabElements.current;
  return tabs.length === 0 ? [] : [Tap({ on: from(tabs).generate() })];
});

export const actionsRoot = weighted(
  [25, switchATab],
  [20, showAViolatingStepWithItsPanel],
  [25, defaultActions],
);
```

Weighting one half is usually not enough, and this is the part that surprises
people. `badgeCountMatchesThePanel` needs a badge, which a tab strip renders
only for a step that has a violation, and a panel to compare it against, which
exists only while a particular tab is selected. Undirected actions put both on
the same step 0 times in the 80 steps of replay-ui's first dogfood run. Aiming
at the violating step alone just moved the misses to the other side: still 0
judged. `showAViolatingStepWithItsPanel` in that spec aims at both halves in
sequence, selecting a violating row and then opening a panel if none is up.

It also opens the *after* panel deliberately, because the before panel's
screenshot is what `screenshotShowsTheSelectedStep` reads, and covering it up
would buy one property's evidence with another's. When two properties read the
same screen, an action tree can starve one to feed the other, and nothing in the
run output will say so.

## 2. An effect must not exceed what the actions could have caused

When the app has an effect you can measure (money moved, rows added, a counter
climbed), state a bound on it rather than a prediction of it.

**Prefer an upper bound to an equality.** This is the single most valuable
sentence in this file.

Folio shipped one of these both ways and the equality lost, so the two are worth
reading side by side. Each line is the last line of a predicate in
`examples/folio/sanderling/predicates.ts`, after the guards, at a step where
exactly one submit sits in the window:

```ts
// what folio's total-balance property demanded, until 6e8e6d5
Math.abs(currTotalBalance - prevTotalBalance) === typedAmount
// what it demands now
Math.abs(currTotalBalance - prevTotalBalance) <= typedAmount
// and the same bound stated as the violation, over the account's own balance
Math.abs(currAccountBalance - prevAccountBalance) > typedAmount
```

All three catch the bug, because a double submit moves the balance by twice the
typed amount and twice x exceeds x. Only the equality also convicts an app that
behaved. A balance that has not moved is a commit still in flight (folio's
`createTransaction` runs in a coroutine, and Home's total re-renders on the
store's own schedule), a submit the app rejected, or a tap that never landed,
and none of those is evidence of anything.

The asymmetry is the point. Moving by more than one submit's worth is not
something a correct app can do, so the bound needs no case for any of the three.
The equality needs a case for each, and every one you forget is a false
conviction. Two of those cases are facts the runner cannot promise you (see
below): an action it could not confirm was applied, and an action it had to
relaunch the app after. Both leave the balance under the bound and both break an
equality, so a bound counts them and an equality has to decline on them.

You do give something up, so make the trade deliberately. A bound cannot see a
balance that moved by *less* than the typed amount, and for a ledger that is a
real bug. The question to settle before giving it up is whether your readings are
tight enough to tell "moved by less" from "has not finished moving yet". If they
are not, the equality was never detecting that bug either; it was reporting it at
random.

The bound has one precondition, and it is the same one as shape 3: it bounds the
effect by what the actions in the window could have caused, so the window has to
count every action that could cause the effect. Miss one and the bound is not a
bound.

## 3. Count the actions, not the amounts

The same bound, stated in counts. One action must not produce two effects.

```ts
!committedTransactionsExceedSubmits({
  countsBefore: homeTxnCounts.previous ?? null,
  countsAfter: homeTxnCounts.current,
  submitsInWindow: submitsSinceCounts.current,
})
```

Reach for this whenever the effect is countable. No arithmetic on values the UI
formatted and you parsed back, no float precision to reason about, and it stays
sound however wide the window between two readings gets, since both sides
accumulate over the same window.

It has exactly two failure modes and both are about the window. Neither makes it
unsound. Both make it useless, quietly.

**The window has to close often enough to attribute anything.** The window opens
when you last read the fact and closes when you read it again, so a run that
wanders away from that screen accumulates budget on one side of the bound
without accumulating evidence on the other. Measured on a real iOS run: it went
from step 19 to step 136 without returning to the screen the property reads,
giving a transaction rise of 15 against a window of 37 submits. 15 is not more
than 37, so nothing was reported. The same run also gave 4 against 7, 6 against
13, and 1 against 1. Sound throughout, detected nothing.

The obvious fix is to read the fact somewhere the run visits often. The better
one, when the wide window is the app's own shape rather than an accident, is to
**state the same rule a second time over a narrower window**, which is what
folio's spec now does:

```ts
const submitCommitsOneTransactionPerAction = always(
  next(
    () =>
      !committedTransactionsExceedSubmits({ /* Home's counts, wide window */ }) &&
      !committedAmountExceedsOneSubmit({ /* this account's balance, narrow window */ }),
  ),
);
```

The counting form can only close its window on a Home reading, and a walk that
stays inside the transaction flow leaves it hundreds of steps and dozens of
submits wide. The second conjunct says the same thing in money about the one
account whose screen the walk is already on, and the transaction flow redraws
that balance on nearly every frame, so its window is usually a single action
wide, narrow enough to tell one commit from two. One rule, two windows, and the
narrow one is where the detection actually comes from.

That only works because the two readings are kept from spanning two accounts:
`readAccountBalance` drops its carrier on every route that is not the ledger or
the transaction screen, transition frames included. A narrow window buys nothing
if the pair it compares straddles two different subjects.

**The window must not be spent on actions that provably could not cause the
effect.** A bound inflated by taps that commit nothing is a bound the app can
never exceed, which is slack a real double submit hides behind. Folio's
transaction submit is `clickable(enabled = amount.isNotBlank())`, so a tap with
an empty field never fires at all, and the app's own `parseCents` refuses
anything outside `^\d+(\.\d{1,2})?$` or parsing to zero. Measured over four
recorded Android runs, 19, 11, 25 and 25 of 35, 26, 42 and 42 submit taps landed
with the amount field empty, which is roughly half the budget in every one.

`submitCouldCommit` in `predicates.ts` is that rule, and note how narrowly it is
drawn. It returns false only where folio's own code **must** have refused, and
returns true for anything it cannot rule out, including an undefined reading and
an amount too large for the app to hold. Over-counting costs a detection;
under-counting convicts a healthy app.

Establishing that an action could not have had an effect is app knowledge, not
something the runner can tell you, and it has to come from the frame the tap
read. Folio reads the amount field on the landing frame, which is sound because
the tap changes nothing about it and one action runs per step.
`element.enabled` is on every `AccessibilityElement` for the general case,
though whether your platform populates it honestly is worth checking on a real
tree rather than assuming.

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

## 6. The ones you get for free, on one platform each

```ts
import { noUncaughtExceptions, noLogcatErrors } from "@sanderling/spec/defaults";

export const properties = { noUncaughtExceptions, /* yours */ };
```

Both read a field the driver fills, and each field is filled on one platform, so
check which one is yours before counting either as coverage. Folio's spec
exports neither, and that is the tell: folio's primary target is Android and iOS.

`noUncaughtExceptions` fails when `state.exceptions` is non-empty. Only the web
runtime fills it, from `error` and `unhandledrejection` listeners installed in
the page by `pkg/spec/src/web-runtime.ts`. On web it is worth the line: a fuzzer
typing `'; DROP TABLE--` and a 4096-character string into every field it finds
will surface real breakage through it. On Android and iOS the field is never
populated, so the property holds at every step of a run that crashed.

`noLogcatErrors` fails on a log line at level `E`. An uncaught Java or Kotlin
throwable is logged there, so on Android it is the nearest equivalent and worth
turning on once you know your app's log hygiene can support it. It holds
vacuously on web and iOS.

That leaves iOS with neither, and it leaves both platforms uncovered for the
thing that matters most anyway. An app can be thoroughly wrong about money
without throwing once.

## The rules that cut across all of them

**Absence is unknown, never a default.** Extractors return null when the element
is not there, and a property handed null declines. `0`, `""` and `[]` are the
values that turn a property into one that fires on healthy runs: folio's
balances once parsed as `0` on web, so the check, an equality at the time,
became `|0 - 0| === typed` and was false at every healthy submit. Under today's
bound the same `0` reads as `|0 - 0| <= typed` and passes at every submit
instead, which is the same defect wearing green. An empty list has the same problem in the
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
account named `Emergency Fund` when the user typed `Fund`: have the new card
clipped out of the reading, the way a list clips any card, and the old account
is convicted for money it has held all along. Substring matching is looser
still. Build every form of the key the platforms can produce and compare each
one whole, which is what `createdAccountHasNonZeroBalance` does with
`account.name === typed || account.name === initialsOf(typed) + typed`. Note
that this bought detections as well as soundness: under the suffix test, a name
that two cards ended with was thrown away as unattributable rather than matched
to the one card that actually carried it.

**What the runner could not promise.** `state.lastAction` is
`Action & { applied: true | null; relaunched: true | null }`, and collapsing any
of its states is unsound:

- `null`, the whole field, means no action ran
- `applied: true` means the runner saw the dispatch succeed
- `applied: null` means it was dispatched and nobody can find out whether it
  landed, because an RPC deadline can fire after the tap arrived
- `relaunched: true` means the runner had to bring the app back to the
  foreground after this action, so the two readings straddle a restart

One rule covers the last two, and it is the rule that decides shape 2 for you.
An action the runner cannot fully vouch for **still counts toward a bound on
what the app could have done**, and it **never licenses attributing an effect to
it**. So a bound counts it and a property demanding an effect has to decline on
it. That is why `committedAmountExceedsOneSubmit`, which only bounds how far the
balance could have moved, needs no `confirmedApplied` guard and no
`acrossRelaunch` guard, while `createdAccountHasNonZeroBalance`, which demands
that a card appear, needs both. Demanding the effect of an action that may never
have run, or that a restart may have swallowed, convicts the app of the runner's
own uncertainty.

`relaunched` is the same shape of fact as `applied`, applied to app state rather
than to dispatch. The action itself did happen. What nobody can promise across
it is that the process ran continuously, that the commit survived, or that the
screen is showing the same slice of the same list it was. So a property assuming
continuous state declines, via `acrossRelaunch(lastAction)`.
`createdAccountHasNonZeroBalance` declines because Home redraws from the top and
the card carrying the typed name may be an older account laid out where the new
one used to be. `countSubmitsInWindow` uses the same call to **stop trusting its
own refusal evidence**, since a relaunch is the one thing that can put a form
state on screen other than the one the tap read.

Both fields are `true | null` rather than booleans, and that is deliberate: only
the positive report is a fact the runner can vouch for, so `null` is "not
reported" rather than "did not happen". `relaunched` shows why it has to be that
way. Web and iOS cannot read the foreground at all, so they never relaunch the
app and equally cannot promise it never restarted, and a `false` there would be
a claim nobody is in a position to make. Read the absence as a guarantee and you
have made the same mistake as reading a missing value as zero, one level up.

**Testing a property means both directions, every time.**

- it fires on the bug it exists to catch
- it stays silent on a run where the app behaved

The second is the one people skip and the one that catches unsoundness. Build
the fixture where the effect happens legitimately, at the boundary the property
draws, and assert silence: the commit that is still settling, the submit the app
refused, the card that scrolled into view rather than being created, the pair of
readings taken either side of a relaunch. A property you have only ever seen go
red is a property you have half tested.

Then hand it to `sanderling-spec-review`, which will ask how many steps it
actually judged on a real run.
