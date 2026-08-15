---
name: sanderling-run-triage
description: Work out what a finished sanderling run actually proves. Use before trusting a green run, before filing the bug a red run seems to show, and any time the exit code is the only thing anyone has looked at.
---

# Reading a run honestly

A run produces one number that is easy to read and several that are worth
reading. The easy one says whether a process finished. It does not say whether
anything was checked, whether what was checked was your app, or whether the
violation it reports is about the app at all.

Work through the sections in order and report what you established and what you
could not. "This run is not evidence, and here is the signal that says so" is a
complete and useful answer.

## 1. The exit codes

- **0** means the run completed. It does **not** mean no violations. Without
  `--exit-on-violation` a run that recorded violations still exits 0: measured
  on a ten step web run that recorded two, `run complete: 10 steps` and
  `2 violation record(s)`, exit code 0.
- **2** means the run recorded a violation under `--exit-on-violation` and
  stopped there. The same ten step run with the flag exits 2 after four steps.
- **1** means the harness broke. A bad target gives
  `error: launch app: page load error net::ERR_UNSAFE_PORT` and exit 1, and
  writes no run directory at all, because the trace is created after the launch
  succeeds.

Anything other than 0 and 2 means the run did not complete, and a missing
`trace.jsonl` under a 0 or a 2 means there is nothing to judge rather than
nothing to report.

Exit 2 is not a conviction. `.github/scripts/folio-run.sh` is the worked example
worth reading in full: it exists because a thrown predicate reaches exit 2 by the
identical path a real conviction does, and so does a violation of a real but
unrelated property in the same spec. It sorts a trace's violations three ways,
by name and by `is_error`: convictions of the properties the leg gates on,
predicates that threw, and other real violations the leg has nothing to say
about. Do the same sort by hand before you call a 2 a finding.

## 2. Reading a witness

Witnesses live in `trace.jsonl`, one object per step under `witnesses`, keyed by
property name. A real conviction and a real throw from the same run:

```json
{"step": 4, "violations": ["countStaysUnderThree"],
 "witnesses": {"countStaysUnderThree": {
   "reason": "predicate false", "step": 4, "detected_step": 4,
   "extractors": {"count": 3}}}}

{"step": 5, "violations": ["throwsOnceCountIsFour"],
 "witnesses": {"throwsOnceCountIsFour": {
   "reason": "Error: boom: no reading for this screen at <eval>:501:37(14)",
   "is_error": true, "step": 5, "detected_step": 5,
   "extractors": {"count": 4}}}}
```

`step` is where the failed obligation was armed and `detected_step` is where the
evaluation produced the violation; for a deferred obligation (a `next`, an
`eventually`) they differ, and `extractors` is `detected_step`'s state, not
`step`'s.

The discipline is one sentence: open the witness and confirm those values could
actually produce that verdict. An iOS witness read `typedAmount = 0`, and
`submitChangesBalanceByTypedAmount` in `examples/folio/sanderling/predicates.ts`
returns true at `typedAmount === 0` before it compares anything. So the trace
appeared to show a conviction that could not have happened. The verdict was real
and the artifact was lying, and until that was resolved neither the bug report
nor the fix could be trusted.

When a witness value looks impossible, suspect the reading before you suspect
the property. Values reach a witness through the driver, and the driver can be
wrong in ways the spec cannot see: erasing a text field used to leave characters
behind, because a backspace only deletes to the left of the cursor and the
runner taps the field's centre, and 7 of 19 measured `InputText` observations
left residue that the spec then reasoned about as if it were the typed value.

Two more things the witness tells you, if the spec extracts them. folio declares
`extract("lastAction", s => s.lastAction)` precisely so they land in the trace:
`applied: true` means the runner saw the dispatch succeed, `applied: null` means
it was dispatched and nobody knows whether it landed, and `relaunched: true`
means the app restarted between the two readings. A property attributing an
effect to an action of unknown fate is unsound; see `sanderling-spec-review`.

Finally, a property violates once. After it fires, its residual stays `false`
(or `{"op": "error", ...}`) for every remaining step and it is never evaluated
again. Measured across steps 4 to 10 of that run, `countStaysUnderThree` reads
`{"op": "false"}` at every step after the first. So the violation count is a
count of distinct properties, not of occurrences, and everything after a
property's first violation is unchecked by that property.

## 3. A green run fails in two ways

Either it checked nothing, or it checked and the fuzzer never reached the bug.
These need opposite responses (fix the spec or the hooks; spend more budget or
better actions) and the exit code distinguishes neither.

The first is not a hypothetical. Against an empty page, six steps, exit 0, `no
violations`, and `countNeverNegative` judged **0 of 6**: its extractor returned
null every step, its guard short-circuited, and its residual read `{"op":
"true"}` at every step, exactly as it reads when it compares real values.

So count, per property, the steps where its guard passed and it compared
something (**judged**) against the steps where it returned true without
comparing anything (**declined**). `.github/scripts/replay-ui-summary.sh` does
this for the replay-ui spec and prints a judged/declined table for exactly this
reason. To do it by hand from a trace:

- fold `extractor_changes` forward per step. Only extractors whose value changed
  are recorded, so a step with no entry for an extractor means unchanged, not
  absent. Measured: `{"count": {"prev": 0, "curr": 2}}` at one step and no
  `count` entry at the next.
- skip steps carrying `skipped_verification` or `transitional`. They advance
  nothing.
- apply each property's own guard to the folded values and count.

That script also carries the honest warning about this technique: restating a
property's guard outside the property is a second copy that can drift, so it
checks that the trace's property names still match the ones it counts and that
the spec still declares the extractors it reads, and it fails loudly when either
moves.

Do not try to read judged-versus-declined off `residuals`. `always(p)` residuals
to `{"op": "true"}` whether `p` compared real values or short-circuited, so the
two are indistinguishable there.

## 4. A red run fails in two ways

Either a property was proved false about the app, or a predicate threw.
`is_error` in the witness separates them and they mean opposite things.

A conviction is a claim about the app. A throw is a claim about the spec, and it
is worse than an unhelpful result: the property is violated from that step on
whatever the app does, so it checks nothing for the rest of the run, and under
`--exit-on-violation` the run ended there so nothing past it was checked by
anything. The `reason` carries the JavaScript error and its location, which is
usually enough to find it: `Error: boom: no reading for this screen at
<eval>:501:37(14)`.

The third case is a real violation of a property that is not the one you are
asking about. It is a finding, and it is somebody's bug, but the run has nothing
to say about the question you asked it. Name the property before you claim the
result.

## 5. When a run is not evidence at all

Some runs never got far enough for any of the above to matter, and every one of
them exits 0 and reports no violations.

**It never reached the app.** A launch flake left a fuzzer on the device
launcher for 200 steps in 65 seconds, two nodes per snapshot, exit 0, no
violations (issue #81). The check is that the app's own marker appears in the
trace at all: the folio CI leg greps for `"AddTransactionScreen"` and fails the
run when it is absent, which is more honest than any exit code it could read.
The run's stdout also carries `app left foreground; relaunching` with the
package it found instead.

**The hierarchy is a handful of nodes.** `nodes=` in each step line is the
cheapest signal there is. Measured: 6 on a four-element page, 2 on an empty one.
A run whose `nodes` never leaves single digits is looking at a launcher, a
crash screen, or a page that failed to boot.

**It never left one screen.** `screen=` constant for the whole trace, or a
`route` extractor that never changes value.

**Steps far faster than the run's own median.** Take the per-step deltas from
each step's `timestamp` and compare them against the run's median. A stretch of
steps at a fraction of it is a driver that is not waiting for an app, because
there is no app to wait for: 200 steps in 65 seconds is 325 ms a step, against
seconds a step for a run that is driving something real.

**It spent its budget on one action.** Count `next_action` by kind and selector.
A run whose actions are one selector explored nothing, whatever its step count.

None of these change the exit code. All of them change what the run proves,
which is nothing.

## 6. `skipped_verification`, `transitional`, and the judged count

The runner skips the verifier for a step whose hierarchy was still moving: an
Android NavHost mid cross-fade after the retry budget, or a hierarchy fetch that
failed or came back empty. Pushing such a tree would poison the previous/current
extractor advance and make the next clean step convict a healthy app, so the
step is recorded for replay and judged by nothing. `transitional` marks the
tree; `skipped_verification` is set exactly when the verifier was skipped.

The run says so itself:

```
7 step(s) judged by nothing: the screen was still moving when it was read
```

Subtract it. `run complete: 240 steps` with that line is a 233 step run for
every purpose that matters, and `replay-ui-summary.sh` reports the pair as
"N steps recorded, M verified" for the same reason. A run with many of these is
telling you the driver could not get a clean read of your app, which is a
finding about the setup and worth chasing rather than quietly accepting the
smaller number.

## Reporting

For any run, report: the exit code and whether `--exit-on-violation` was set;
steps recorded against steps verified; per property, judged against declined;
for every violation, its `is_error` and the witness values you actually opened;
and which of the section 5 signals you checked. Name the step behind any claim.

A run is evidence only for the properties that judged, and only for the app it
was actually looking at. Everything else it produced is a log.
