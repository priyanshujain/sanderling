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

**android and ios expect the bug.** Folio double-submits a transaction when the
submit button is double-tapped, and `submitMovesBalanceByTypedAmount` is the
property that catches it. The runs pass `--exit-on-violation`, so:

| exit | meaning | job |
|---|---|---|
| 2 | the run found a violation | green |
| 0 | the run finished clean | red: the fuzzer stopped finding a bug that is still there |
| 1 | something went wrong | red: the harness broke |

Distinguishing 0 from 1 is the whole point of the exit code: a job that only
knew "non-zero" could not tell a working fuzzer from a broken emulator.

**web is a health gate**, not an expect-the-bug leg. The same spec logs into the
wasmJs build and drives it to the transaction screen (the job asserts the trace
reached `AddTransactionScreen`), but the submit property cannot fire there: it
keys off `state.lastAction`, which the web runtime reports as `null`, and off the
action's selector, which the web picker does not carry, since it emits
coordinates and element identity never crosses the V8 boundary.

The wasmJs app is served with `Cross-Origin-Opener-Policy` and
`Cross-Origin-Embedder-Policy` headers, because its sqlite worker needs
cross-origin isolation. Served without them the app loads a blank canvas and
every step observes an empty accessibility tree.

The seeds in the workflow are calibrated, not guessed. On an M-series mac,
android seed 3 finds the bug at step 110-116 (seeds 1, 2 and 4 run 120 steps
clean) and ios seed 1 finds it at step 129-134, both against a 240-step budget.
The web leg ran five seeds x 200 steps clean.

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
