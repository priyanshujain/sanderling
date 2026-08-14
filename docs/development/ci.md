---
title: CI
---

# CI

`ci.yml` runs on every pull request: it builds, unit-tests, and drives two small
web fixtures through headless Chrome. It never runs sanderling against a real
app.

Two other workflows do, and both are `workflow_dispatch` only. They boot devices,
build apps and take minutes, which is not what you want on every push, and
neither is a merge gate.

## folio

Actions -> folio -> Run workflow. Inputs pick the legs (`all`, `android`, `ios`,
`web`), and override the seed, the wall-clock budget, and the step budget; `0`
means "use the calibrated value in the workflow".

Each leg builds `examples/folio` for its platform, builds the CLI with only the
tags that platform needs (`make sanderling-android` and friends), and runs
`examples/folio/sanderling/spec.ts` through `.github/scripts/folio-run.sh`. That
script is plain bash so you can reproduce a job locally:

```
SEED=3 MAX_STEPS=240 .github/scripts/folio-run.sh android
```

**web and ios expect the bug.** Folio double-submits a transaction when the
submit button is double-tapped, and two properties catch it:
`submitMovesBalanceByTypedAmount`, which needs the balance to move by exactly
twice the typed amount, and `submitCommitsOneTransactionPerAction`, which needs
more transactions committed than there were submit actions. The runs pass
`--exit-on-violation`, so:

| exit | meaning | job |
|---|---|---|
| 2 | the run found a violation | green |
| 0 | the run finished clean | red: the fuzzer stopped finding a bug that is still there |
| 1 | something went wrong | red: the harness broke |

Distinguishing 0 from 1 is the whole point of the exit code: a job that only
knew "non-zero" could not tell a working fuzzer from a broken emulator.

The balance property only judges a window holding exactly one submit action.
Without that rule the recorded delta covers every transaction since the last Home
visit, and the property convicts on arithmetic it cannot attribute: an early
version of this gate went green on a witness whose delta was 3.16x the typed
amount. Honest windows are rare, so the counting invariant carries most of the
detection: it needs no amount and survives a wide window.

**android is a health gate**, not a conviction gate. A run there is not a
function of its seed. The hierarchy dump can show two screens at once during a
Compose cross-fade, such a step applies no action, and the count of those frames
varies run to run, so the same seed walks a different trajectory each time. The
bug turns up in about two runs in five, which is not a gate: the failure message
"the double-submit bug was NOT found" would be indistinguishable from the
regression it exists to catch. The leg asserts instead that the run stayed
healthy and reached the transaction screen, and reports a conviction as a bonus
when it gets one. Fixing this means suppressing transitional frames in the
android driver, the way the ios companion and the chrome driver already do.

The wasmJs app is served with `Cross-Origin-Opener-Policy` and
`Cross-Origin-Embedder-Policy` headers, because its sqlite worker needs
cross-origin isolation. Served without them the app loads a blank canvas and
every step observes an empty accessibility tree.

The seeds are calibrated, not guessed. On an M-series mac, web seed 3 convicts at
step 185-187 and ios seed 7 at step 97-101, each 3 runs out of 3 and each with a
delta of exactly twice the typed amount. Both run a 240-step budget. Keep them
pinned: honest evidence is rare, and across 2261 ios steps only one submit tap
landing on Home had a single-submit window.

Android runs seed 9 over 120 steps, but as a health gate the seed is not load
bearing; the budget is, since a run that finds nothing walks the whole thing and
costs seven minutes against ninety seconds for one that convicts.

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

Every property there is a cross-panel agreement - two panels deriving the same
fact by different paths have to say the same thing - so it holds for any trace
and needs no recalibrating when the fixture changes. Any violation fails the job.

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
failing across repeated dispatches with exit 0, do not raise the step budget
blindly - run a seed sweep with the campaign tool
(`cmd/internal-tools/campaign`), which exists for exactly this, and pin a seed
that finds the bug with room to spare.
