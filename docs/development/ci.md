---
title: CI
---

# CI

`ci.yml` runs on every pull request and every push to master. The `Check`
jobs build, unit-test, and drive three small web fixtures through headless
Chrome (`test/browser/testdata`). The `Folio` and `Replay UI` jobs in the same
workflow do run sanderling against real apps, on emulators and simulators, and
they are what make a run take the better part of an hour.

`All checks passed` is the one status check to point branch protection at, and
it is what gates a release: `release.yml` cuts one only after a whole ci run
went green. See [Releases](#releases) at the bottom.

## folio

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
`submitMovesBalanceByAtMostTypedAmount`, which demands the total balance move by
no more than the amount typed, and `submitCommitsOneTransactionPerAction`, which
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
| no trace at all, on exit 0 or 2 | red: the run recorded nothing, so there is no verdict to read |
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

The seeds are calibrated, not guessed, and every number here says which host it
was measured on, because the hosts do not agree. On an M3 mac driving iOS 26.1
simulators, ios seed 7 convicts at step 97-101, 11 runs out of 11 from a cleared
install, each on both properties and each with the balance moving by exactly
twice the typed amount: 199 typed, 39800 cents moved, one account's transaction
count rising by two against a window holding one submit. Web seed 3 convicts at
step 185-187 on that mac and at step 192 on the ubuntu runner. Both legs run a
240-step budget.

What those numbers assume is a cleared starting state, and that is the only thing
that moved them. Measured four ways on one simulator, seed 7 convicts at step 97
from a fresh install with clear-state on, at 100 from a fresh install with it
off, and at 97 from a dirty container with it on. It walks 240 steps clean
exactly once: dirty container, clear-state off, where the app opens already
signed in on the previous run's accounts and the walk diverges at step 1. The leg
therefore clears state for itself rather than relying on how it was called.

Do not read a mac number as a statement about CI, but the ios leg does now
convict there. It had never done so before 2026-08-16, through every dispatch,
and no seed was ever the reason. `submitCommitsOneTransactionPerAction` counted
every tap on TxnSubmit toward its window, including taps the app refuses because
the amount field is empty, and more than half of a typical window was those.
Measured on recorded android runs: 35 taps against a real budget of 16, and 42
against 17. A window that wide cannot attribute anything, which is why seed 28
reached the bug at the step it convicts at locally and was still not judged.

`submitCouldCommit` stopped counting them, and the numbers moved a long way:

    leg      before                          after (run 31898888205)
    ios      240 steps clean, every time     convicts step 59, detected 60
    web      step 192 on the ubuntu runner   convicts step 185, detected 186
    android  never reached AddTransaction    healthy over 200 steps, reached it

The ios witness at that conviction reads one account's transaction count rising
from 0 to 7 against a window holding 6 submits, with `applied: true` on the
action and `is_error` unset. Seven transactions from six submits is one double
submit, which is the bug the leg exists to find.

On the calibration mac the same seed now convicts around step 48, twice in a row,
where it used to convict at 97-101. Treat both as approximate: the point is that
the window is now tight enough to attribute a submit, not that any particular
step number is pinned. A run that fails is worth reading before it is worth
recalibrating.

Android runs seed 9 over 200 steps. Its conviction lands around step 178, and a
shorter budget would never see the bonus. A full run costs about five minutes.

That step number was measured on a local emulator with animations ON, and the CI
job sets `disable-animations: true`, so it does not describe the CI leg. The
worry that follows is that zeroing the 700ms Compose fade would stop the leg
exercising the cross-fade wait entirely, and the traces say that worry is
largely right. The first dispatch, whose run was stuck on one screen, carried 4
`transitional` steps in 200. The healthy runs since carry **zero**. So on CI the
wait almost never fires, and a leg that is green there is not evidence the wait
works. Local runs with animations on are where that gets exercised. Treat the
android number as an order of magnitude, not a pin. It is a health gate, so
nothing keys on it.

Repeating the ios leg by hand needs nothing special now, because the run clears
the app's state itself. It used to: `just ios` installs over the top without
uninstalling and folio's signed-in session survives that, so a repeat under the
old `--clear-data=false` opened on the previous run's Home screen and diverged at
step 1. That is how the leg came to look dead while the app and the seed were
both fine, and it is worth recognising: a leg that reports "the double-submit bug
was NOT found" from a machine that has been running the app all day is describing
the machine.

The ios leg clears state and passes no `--ios-app-path`, which is deliberate:
without an app path the driver wipes the app's data container instead of
reinstalling, and the reinstall is the path that races FrontBoard. `simctl
uninstall` + `install` followed straight away by the XCTest runner's own launch
has failed with `app.folio is unknown to FrontBoard` about half the time on the
host that reported it. That race is untouched and still open; the leg simply
does not take that path. It did not reproduce here at all, in 20 consecutive
reinstall-and-launch cycles on iOS 26.1, 10 of them reinstalling on top of a
live app, so any fix for it has to be developed on a host that can still show it
failing.

The leg names a device and a runtime, `iPhone 17 Pro` on `iOS 26.2`, and boots
by the UDID that pair resolves to. Both halves matter: one runner image carries
the same phone under several runtimes, so booting by name alone is booting on
whichever one `simctl` lists first, and a seed that is only calibrated against a
runtime it did not run on says nothing. A runner image that stops carrying the
pair fails the boot step naming what it does carry, which is the cue to pick a
new pair and recalibrate rather than a `bootstatus` error to read backwards.

Only one sanderling run may drive a given simulator at a time. The driver takes
an advisory lock on the target's UDID and a second run is refused with the lock
path in the message, because two runs interleaving app lifecycle leave the first
run's automation session bound to a bundle the simulator no longer knows.

## replay-ui

This one is dogfooding: it records a trace
from `test/browser/testdata/throwing` (violations and uncaught exceptions, so
every panel has something to render), serves it with `sanderling replay`, and
fuzzes that UI with `replay-ui/sanderling/spec.ts`.

Three of the seven properties there are cross-panel agreements - two panels
deriving the same fact by different paths have to say the same thing. The other
four are a range invariant on the step in the URL, a count of selected rows
inside the list, a no-effect property across a tab switch, and the stock
`noUncaughtExceptions`, which asks nothing of the panels and only fails if the
UI throws. All seven hold for any trace and need no recalibrating when the
fixture changes. Any violation fails the job.

So does a run that judged nothing. Exit 0 says no property returned false, which
is not the same as any property having been evaluated: each one declines to
judge when the elements it reads are absent, so a run where the trace failed to
serve, or where the fuzzer sat on the run list, renders nothing and passes.
`.github/scripts/replay-ui-summary.sh` reads the trace and puts a per-property
count of judged against declined steps in the job summary. Run it by hand with
`GITHUB_STEP_SUMMARY=/dev/stdout .github/scripts/replay-ui-summary.sh runs/dogfood`.

It fails the job when any of the four properties that need nothing beyond the
step page having rendered - `selectedStepIsInRange`, `exactlyOneStepIsSelected`,
`stepCountMatchesTheList`, `screenshotShowsTheSelectedStep` - judged nothing at
all. The other three are reported and not gated, because a zero on them is a
seed getting unlucky rather than a broken leg: `switchingTabsKeepsTheStep` needs
a tab switch between consecutive steps, and `badgeCountMatchesThePanel` needs the
fuzzer to land on a violating step and open the violations tab in that same
step. On the first run measured this way (seed 3, 80 steps) that last one judged
nothing at all, so the fixture reaches it far too rarely to be worth gating on.

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

Sweep in the leg's own configuration, though. The campaign tool and the ios leg
now clear state the same way, so a swept seed means what the leg means, but the
starting frame is not a detail you can skip checking: while the leg still passed
`--clear-data=false`, seed 14 convicted at step 17 in 2 campaign runs out of 2
and in 0 leg-shaped runs out of 3. Prefer the earliest conviction on offer over
the first one found, too. A run reproduces its trajectory on another host only
for as long as every snapshot agrees, and every step of prefix is another chance
for it not to: seeds convicting at steps 33, 60, 114, 187 and 189 all turned up
within the first 30, so an early one is usually there to be found.

A short prefix is necessary and not sufficient, though, and ios is the standing
counter-example: seed 28 has the shortest prefix on offer, reproduced its walk on
the runner exactly, reached the bug at step 32, and still did not convict,
because the window the counting invariant had to judge it in was 117 steps wide.
Sweeping selects for a seed that reaches the bug. It cannot select for one whose
walk also closes the window, so when a property needs a window, check what the
window looked like and not only that the conviction happened.


## Releases

**Every merge to master cuts a patch.** The `Tag`, `Release (npm)` and
`Release (cli)` jobs sit in `ci.yml` alongside everything else, waiting on
`Checks`, `Folio` and `Replay UI`, so nothing reaches a registry that the
emulators and the simulator have not agreed on. `0.1.4` becomes `0.1.5`:
published to npm, and to GitHub Releases with the CLI binaries.

**A milestone consolidates them.** Actions -> ci -> Run workflow, set `promote`
to `minor` or `major`, and the patches you have been shipping become `0.2.0`.
Leaving `promote` on `none` is an ordinary ci run that publishes nothing, which
is what stops a dispatch meant to re-run the tests from cutting a release.

A promotion runs the whole suite, device legs included. It is the same pipeline
either way, and a release that skipped the checks would be the only release
nobody checked. Both paths release the commit the run tested rather than
whatever master drifted to while it ran. Afterwards the patch line continues
from the milestone: the next merge counts off `0.2.0` and cuts `0.2.1`.

**The tags are the version.** Nothing in the tree holds it:
`pkg/spec/package.json` stays at `0.0.0-dev` and CI stamps the real version in
before it publishes. So there is no version-bump commit to land on master,
nothing to conflict on, and no second record to hold in step with the tags.
`.github/scripts/next-version.sh` is the whole rule, and it counts off stable
tags only, because `v0.0.1-rc4` is a candidate for `0.0.1` and a patch counted
off it would skip the version it was a candidate for. Run it anywhere to see
what the next release would be:

```
BUMP=minor .github/scripts/next-version.sh
```

The tag is pushed before anything is published, because npm is the half of a
release that cannot be taken back and a tag is the half that can.

### How far back the notes reach

GoReleaser builds its changelog from the commits between the previous tag and
this one, and works that previous tag out on its own. For a patch that is
exactly right. For a milestone it is not: the notes on a `0.2.0` consolidating
six patches would describe the one merge that happened to be last.

So the resolver also emits `previous_tag`, which the release passes as
`GORELEASER_PREVIOUS_TAG`: the last release at the level being cut. A minor
reaches back to the last `vX.Y.0`, counting a major as one, and a major reaches
back to the last `vX.0.0`. The first milestone of its kind has nothing at its own
level, so it reaches back to the first release there has ever been. A patch emits
nothing, and an empty value leaves GoReleaser on the default that was already
right for it.

The boundary is exclusive, the way a changelog always is: the notes cover what
landed *after* that tag. So the one release this shortchanges is the first
milestone of its kind, whose notes start after the first release rather than at
it. That is one merge, once, and it is not worth a special case.

### Why the release is not its own workflow

It reads like it should be. The reason it is not is npm.

npm publishes over OIDC here, against a trusted publisher configured for
`@sanderling/spec`, so CI holds no npm credential at all. That is not a
preference: npm disabled classic token creation in November 2025, revoked every
classic token on 9 December 2025, and caps a granular token at 90 days. A token
in CI would now expire quarterly, which is exactly the failure this replaced.
The August 2026 outage was an expired granular token, and npm answers a publish
it will not authorise with `404`, so it read as "package does not exist" while
`@sanderling/spec` sat in the registry the whole time.

A package carries exactly one trusted publisher, and npm matches it against the
filename of the workflow that *starts* the run. A reusable workflow does not
help, because npm sees the caller's name, not the callee's. So every publish has
to enter through one file, and since a merge's release has to run inside ci, that
file is `ci.yml`.

Setting it up again, or moving the package, means npmjs.com -> the package ->
trusted publisher: repository `priyanshujain/sanderling`, workflow `ci.yml`. Or
from a shell, which needs an interactive 2FA challenge:

```
npm trust github @sanderling/spec --file ci.yml --repo priyanshujain/sanderling --allow-publish
npm trust list @sanderling/spec
```

The job installs npm 11.5.1 or newer before publishing, because
`actions/setup-node` writes an empty `_authToken` line into `.npmrc` and an older
npm reads that as "auth is configured" and never asks for an OIDC token.
