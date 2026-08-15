---
name: sanderling-spec-review
description: Review a sanderling spec for properties that cannot fail, cannot pass, or convict a healthy app. Use before trusting any spec, after any spec change, and whenever a run is green but you are not sure it checked anything.
---

# Reviewing a sanderling spec

A spec that is wrong does not look wrong. It looks like a passing run. Every
failure below was found in a real spec that had been green for weeks, and each
was caught by reading a witness rather than an exit code.

Work through the checks in order. Report what you actually verified and what you
could not; a review that says "I could not establish this" is worth more than one
that implies coverage it did not check.

## 1. Can each property ever fail?

For every property, find the input that makes it return false, and say what it is.
If you cannot name one, the property is decoration.

The common shape is a guard that short-circuits on absent elements:

```ts
const badgeMatchesPanel = always(() => {
  const badges = violationBadges.current;
  const panels = panelCounts.current;
  if (badges.length === 0 || panels.length === 0) return true;  // declines
  return panels.every((c) => c === badges[0]);
});
```

That guard is correct in isolation: with nothing on screen there is nothing to
disagree about. It is also how a property judges zero steps in an eighty step run
and reports success. Measured on a real run, that exact property judged **0 of 80
steps** while the job went green.

So counting matters. For each property, count the steps where its guard passed
and it actually compared values (**judged**) against the steps where it returned
true without comparing anything (**declined**). A property that judged nothing
proved nothing, whatever the exit code said.

You can reconstruct this from a trace: fold `extractor_changes` forward per step
to recover each extractor's value, then apply the property's own guard. Do not
try to read it from `residuals`: `always(p)` residuals back to `{"op":"true"}`
whether `p` compared real values or short-circuited, so the two are
indistinguishable there.

## 2. Can each property ever pass?

The mirror failure. A missing fact read as a value instead of as unknown turns a
property into one that fires on every healthy run.

```ts
// balances parse to 0 when the element is missing
Math.abs(currBalance - prevBalance) === typedAmount   // 0 - 0 === typed, always
```

Check every extractor: does it return `null` when the element is absent, or does
it return `0`, `""`, or `[]`? An unreadable fact is unknown, never a default.

## 3. Would it convict an app that behaved correctly?

This is the only unforgivable failure. A property that convicts more often and is
sometimes wrong is strictly worse than one that convicts less and is never wrong,
because a false conviction costs someone a day and then costs the whole suite its
credibility.

Test both directions for every property, always:

- it fires on the bug it exists to catch
- it stays silent on a run where the app behaved

The second test is the one that matters and the one people skip. Build a fixture
where the effect happens legitimately and assert silence.

## 4. Is the attribution sound?

When a property blames an effect on an action, check it cannot blame the wrong one.

**Identity keys must be injective.** If two distinct objects can produce the same
key, a value can jump between unrelated series without anything noticing. Merged
UI text is the usual culprit: an account named `Travel1` with 25 transactions and
one named `Travel12` with 5 can both render `TRTravel125 transactions`. No
function of that string can separate them.

**Match whole keys, not endings or substrings.** `endsWith` attribution judges an
older account named `Emergency Fund` when the user typed `Fund`.

Selector matching has the same trap and it is easy to miss which keys carry it.
`id`, `text`, `desc` and `descPrefix` resolve by their own rules, so `{id: "Sub"}`
correctly matches nothing. Every other key, `testTag` included, falls through to a
substring compare, so `{testTag: "Sub"}` matches `AddAccountSubmit`. That is not a
match, it is a coincidence, and a property built on it judges whichever element
happens to contain the fragment.

**Drop the carrier when the screen changes.** A value carried across a route
change is a value read from a screen that is no longer there.

## 5. Are the windows bounded?

A property that compares two readings and counts actions between them is only as
good as how often it closes the window.

Real numbers from a real leg: a run went from step 19 to step 136 without
returning to the screen the property read, so it saw a rise of 15 against a window
of 37 actions. 15 is not more than 37, so nothing was reported, and the same run
also gave 4 against 7, 6 against 13, and 1 against 1. The property was sound the
whole time and detected nothing.

Two fixes, and prefer the first:

- **Close the window more often.** Read the fact somewhere the run visits often,
  not somewhere it visits rarely.
- **Do not spend budget on actions that cannot have caused anything.** If the
  submit button is disabled when the field is empty, a tap on it committed
  nothing and must not count. That one change halved the window on a real spec.

Prefer an **upper bound** to an equality. `|delta| > typedAmount` is sound where
`|delta| === typedAmount` convicts a commit still in flight, a refused submit, and
a tap that never landed.

## 6. Does every selector actually resolve?

A selector that matches nothing makes every property over it vacuous, and nothing
reports it. Verify by finding a step whose witness holds a real value for it, not
by reading the selector and believing it.

## 7. Does the property know what the runner could not promise?

`state.lastAction` distinguishes three things, and a property that collapses them
is unsound:

- `null` means no action ran
- `applied: true` means the runner saw the dispatch succeed
- `applied: null` means it was dispatched and nobody knows whether it landed

An action of unknown fate still counts toward **bounds on what the app could have
done**, and never licenses attributing an effect **to** it. `relaunched: true`
says the app restarted between two readings, so a property assuming continuous
state must decline.

## Reporting

For each property give: can it fail, can it pass, does it convict a healthy app,
how many steps it judged on a real run, and what you could not check. Name the
step and the witness values behind any claim that a property works.

A green run is evidence only if you can point at a step where a property actually
fired. Read the witness, not the exit code.
