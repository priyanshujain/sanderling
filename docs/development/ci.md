---
title: CI
---

# CI

`ci.yml` runs on every pull request: it builds, unit-tests, and drives three
small web fixtures through headless Chrome (`test/browser/testdata`). It never
runs sanderling against a real app.

Two other workflows do, and both are `workflow_dispatch` only. They boot devices,
build apps and take minutes, which is not what you want on every push, and
neither is a merge gate.

## folio

Actions -> folio -> Run workflow. Inputs pick the legs (`all`, `android`, `ios`,
`web`), and override the seed, the step budget and the wall-clock budget. Seed
and step budget take `0` to mean "use the calibrated value in the workflow"; the
wall-clock budget has no such sentinel and is passed through as written, so
every leg gets whatever you type there.

Each leg builds `examples/folio` for its platform, builds the CLI with only the
tags that platform needs (`make sanderling-android` and friends), and runs
`examples/folio/sanderling/spec.ts` through `.github/scripts/folio-run.sh`. That
script is plain bash so you can reproduce a job locally, with that leg's pinned
numbers:

```
SEED=9 MAX_STEPS=200 .github/scripts/folio-run.sh android
```

**web and ios expect the bug.** Folio double-submits a transaction when the
submit button is double-tapped, and two properties catch it:
`submitMovesBalanceByTypedAmount`, which demands the total balance move by
exactly the amount typed, and `submitCommitsOneTransactionPerAction`, which
demands no more transactions committed over a window than there were submit
actions in it. A double tap is one action committing two transactions, so it
breaks both.

The runs pass `--exit-on-violation`, which exits 2 when the run recorded a
violation and 1 when something went wrong. Telling those apart is the whole
point of the exit code: a job that only knew "non-zero" could not tell a working
fuzzer from a broken emulator.

Exit 2 on its own is not a conviction, though, so the script reads the trace
before it decides:

| what the trace says | job |
|---|---|
| a violation of one of the two properties above, with no `is_error` on its witness | green |
| a violation whose witness carries `is_error` | red: a predicate threw, and a thrown predicate is recorded as a violation like any other |
| a violation of any other property (`newAccountBalanceIsZero` fires on one android seed) | red: a real finding, but not the one this leg gates on |
| no violation and exit 0 | red: the fuzzer stopped finding a bug that is still there |
| exit 1, or any other code | red: the harness broke, and the code propagates |

The first two rows are why the check is worth the code it takes. A `TypeError`
in `predicates.ts` and a fuzzer that no longer reaches the bug both used to
print "found the submit bug" and exit 0, and they need opposite responses: one
is a spec to fix, the other is a seed to recalibrate. A thrown predicate fails
android as well, where a conviction is otherwise only a bonus, because a spec
that stopped running is not evidence about the app.

The balance property only judges a window holding exactly one submit action.
Without that rule the recorded delta covers every transaction since the last Home
visit, and the property convicts on arithmetic it cannot attribute: an early
version of this gate went green on a witness whose delta was 3.16x the typed
amount. Honest windows are rare, so the counting invariant carries most of the
detection: it needs no amount and survives a wide window.

**android is a health gate**, not a conviction gate, because it convicts in four
runs out of five rather than five. The leg asserts that the run stayed healthy
and reached the transaction screen, and reports the conviction it usually gets as
a bonus. A gate that fails one run in five would be useless here: its failure
message, "the double-submit bug was NOT found", is indistinguishable from the
regression the gate exists to catch.

It used to be two runs in five. The android backend now waits out a route
cross-fade before it snapshots, the way the ios companion and the chrome driver
do, because a dump holding two screens at once makes the runner refuse to act on
it and a quarter of android steps therefore applied no action at all. The number
of those wasted steps varied run to run, so the same seed never walked the same
trajectory. With the wait, four runs of one seed produced byte-identical decision
sequences over 179 steps.

The fifth diverged for the remaining reason: a route can settle before its
content composes, so the tree holds one screen and almost nothing in it, and the
fuzzer acts on a screen that is still filling in. That happened once in about a
thousand steps. Catching it needs structural stability polling on every snapshot,
which costs roughly 2.8s per mutating step and was removed for that reason, so it
is the open item between android and a real conviction gate.

The wasmJs app is served with `Cross-Origin-Opener-Policy` and
`Cross-Origin-Embedder-Policy` headers, because its sqlite worker needs
cross-origin isolation. Served without them the app loads a blank canvas and
every step observes an empty accessibility tree.

The seeds are calibrated, not guessed. On an M-series mac, web seed 3 convicts at
step 185-187 and ios seed 7 at step 97-101, each 3 runs out of 3 and each with a
delta of exactly twice the typed amount. Both run a 240-step budget. Keep them
pinned: honest evidence is rare, and across 2261 ios steps only one submit tap
landing on Home had a single-submit window.

Android runs seed 9 over 200 steps: its conviction lands at step 178, so a
shorter budget would never see the bonus. A full run costs about five minutes.

Repeating the ios leg by hand is not the same as running it in CI: with
`--clear-data=false` a second local run inherits the first one's accounts, so
`simctl uninstall` before each repeat or the numbers drift.

The ios leg passes `--clear-data=false`, because the job installs a fresh build
immediately before the run and a freshly installed app is already clear state.
The in-run reinstall is worth avoiding: `simctl uninstall` + `install` followed
straight away by the XCTest runner's own launch fails with `app.folio is unknown
to FrontBoard` maybe half the time. That used to hang the run outright; the
launch RPC is bounded now, so it fails in about 90 seconds with a real error
instead, but a failing leg is still a failing leg. The job timeouts are the
backstop if it happens anyway.

Only one sanderling run may drive a given simulator at a time. The driver takes
an advisory lock on the target's UDID and a second run is refused with the lock
path in the message, because two runs interleaving app lifecycle leave the first
run's automation session bound to a bundle the simulator no longer knows.

## replay-ui

Actions -> replay-ui -> Run workflow. This one is dogfooding: it records a trace
from `test/browser/testdata/throwing` (violations and uncaught exceptions, so
every panel has something to render), serves it with `sanderling replay`, and
fuzzes that UI with `replay-ui/sanderling/spec.ts`.

Six of the seven properties there are cross-panel agreements - two panels
deriving the same fact by different paths have to say the same thing - so they
hold for any trace and need no recalibrating when the fixture changes. The
seventh is the stock `noUncaughtExceptions`, which asks nothing of the panels
and only fails if the UI throws. Any violation fails the job.

## Reading a failure

Both workflows upload their run directories as artifacts, and write the step
count, seed and violations to the job summary. To replay a failure:

```
gh run download <run-id> -n folio-android
sanderling replay <the downloaded runs directory>
```

That is the same UI the replay-ui workflow fuzzes. Open the step the summary
named, and the Violations tab shows the witness: the property, the reason, and
the extractor values at the step that caused it.

## When a device leg flakes

Expecting a violation from a single seed is timing-sensitive, most of all on an
emulator. Calibration reduces that; it does not remove it. If a platform starts
failing across repeated dispatches with "the double-submit bug was NOT found",
do not raise the step budget blindly - run a seed sweep with the campaign tool
(`cmd/internal-tools/campaign`), which exists for exactly this, and pin a seed
that finds the bug with room to spare. A leg failing with "a predicate threw" is
a different problem entirely and no seed will fix it.
