# code audit, august 2026

Eleven parallel audits of the tree at `9b4ff5f`, one per subsystem, each required to cite
`file:line` and to verify claims against the repository rather than recall. Detailed
per-slice findings sit alongside this file's source notes; what follows is the reconciled
result, including the places where the audit contradicted the brief it was given.

## What has been acted on

The branch `action-wire-and-test-scaffolding` finished the action-wire handshake, fixed the
rest of the vacuous-pass class, corrected the load-bearing false comments, and landed the
first shared test scaffolding. What it changed is marked through the sections below; three
findings were corrected in the process and say so where they sit. What it deliberately did
not touch, each still owed its own change: relocating `cmd/internal-tools`, collapsing the
four DOM fact producers, narrowing `DeviceDriver`, trimming the vendored protocol, the
remaining false comments outside the load-bearing set, and the 51 table clusters.

One correctness finding was added to the list during the work rather than found by the audit.
Chrome's `Metrics` filled `CPUPercent` with a zero, which is the same answer an idle app
gives, so every web step recorded a CPU reading nobody took. It is the same defect as the
three iOS reads and it is fixed the same way: `driver.Metrics.CPUPercent` and
`trace.Metrics.CPUPercent` are now optional, and absence is how a driver says it did not look.

Two findings were reported to this file rather than fixed, because both are decisions rather
than defects.

**The campaign record still has three declarations.** Collapsing them was considered and
rejected as too large for that change: they are not one type with drift. The writer emits
value ints with `omitempty`; `analyze/load.go` deliberately declares `Actions` and
`UnattributedActions` as `*int` so a `runs.jsonl` written before those counts existed is
refused rather than read as an arm that acted zero times, and two of its tests assert that
refusal; `confusion-matrix` reads a different subset again, including `RunDirectory`. One
shared type has to break one of those. If it is done, the shape is a type in a package all
three import, carrying pointers for every field whose absence has to be distinguishable from
zero, with the writer always emitting them, so the `duration_millis` shim moves in with it as
a named accessor rather than being rediscovered per reader.

**The two settle primitives now disagree, on purpose on one side.**
`sidecar/.../DriverBackend.kt` no longer charges a read's own duration to the stability
streak, because its doc said it did not and the code did. `internal/driver/ioscompanion/settle.go:58-64`
charges it deliberately and says so, arguing that a traversal-based read would show mid-read
churn as a byte difference. That argument is not unreasonable and it is an argument for a
weaker guarantee, so it should be settled on the evidence rather than adopted by drift.

## Verdict

The complaint that prompted this audit was that the repository is padded with unnecessary
code and worthless tests, and that a peer project solving a similar problem does it in a
third of the lines. The size gap is real and the causes are not what the complaint assumed.

Three conclusions, in order of how much they change what should be done next:

1. **The test suite is not the problem.** It is large, 1.42:1 over source in the runner and
   driver packages and 1.65:1 in the verifier core, but only about 5 to 10 percent of it is
   genuinely deletable. Most of it asserts on `trace.jsonl` bytes on disk, carries explicit
   control arms, and names the bug class in the test name. Measured rather than assumed:
   dropping the ten most redundant-looking `internal/hierarchy` tests moves statement
   coverage from 91.9 percent to 91.9 percent, and the three next-worst clusters lose seven
   statement blocks between them, five of which are one-line getters. The waste is
   structural, the fifth copy of a pattern that was never folded back into the first, and
   tests that were never deleted after a stronger test subsumed them.

2. **The bulk is in research tooling and a vendored protocol, not the product.** `cmd/`
   is 20,565 lines, of which the shipped CLI is 1,475. The other 19,090 sit in
   `cmd/internal-tools`, which is 14.6 times the size of the product CLI and 30 percent of
   the whole engine. Eight of its eleven binaries were created in the HEAD commit, have a
   one-commit history, and five of them cite specifications that live in `paper/`, a
   separate git repository. The Makefile builds none of them. Separately, the vendored iOS
   protocol generates 12,283 lines from 97 messages, of which 14 are ever called.

3. **Where the two projects do the same job, they cost the same.** The browser target is
   5,927 lines against the peer project's 5,777, and the peer's does more inside that
   budget. None of the size difference is this tool being worse at the work it exists to do.

The rules in `CLAUDE.md` were not uniformly ignored, and the pattern in how they failed is
the most useful finding in the audit. Rules an agent can check while typing a single line
were followed, completely: zero em dashes in the entire tracked source tree, zero `TODO`,
zero `FIXME`, zero `HACK`, zero emoji headings, no badges and no contributing boilerplate
in any of the five READMEs. Rules that require standing back from a finished artefact were
not followed at all: is this comment needed, is this commit too large, does this README hold
more than a description. That is a review-gate problem, not a discipline problem, and it is
fixable with checks rather than with resolve.

## Numbers

Non-blank lines, identical exclusions applied to both repositories, counted against the
tree rather than recalled.

| | this repository | peer project | ratio |
|---|---:|---:|---:|
| source, non-test | 56,708 | 19,065 | 3.0x |
| test | 49,277 | 6,693 | 7.4x |
| total | 105,985 | 25,758 | 4.1x |
| comparable product code only | 68,060 | 23,597 | 2.9x |
| files | 506 | 226 | 2.2x |
| test to source ratio | 0.87, or 1.11 excluding generated | 0.35 | |
| doc comments in non-test Go | 3,512, 15.9% of code | | |
| inline comments in non-test Go | 1,164, 5.3% of code | | |
| test functions | 1,280 | 136 | 9.4x |
| driver interface width | 18 methods plus 6 capability interfaces | 6 methods, 3 associated types | 4.5x |
| non-test files touched by one action verb | 34 | 8 | 4.3x |

Static analysis is clean and the suite is green. `go vet ./...` reports nothing across all
38 packages. `staticcheck ./...` reports one finding in source. The full suite runs in 24.9
seconds wall, 1,530 tests passed, 0 failed, 6 skipped, all six env-guarded rather than
broken. Average cyclomatic complexity is 4.26 with 7.2 percent of functions over 10, and
the excess is concentrated: `internal/runner/runner.go`, `internal/ltl/evaluator.go` and
`internal/verifier/worker.go` own 8 of the top 30.

Duplication runs opposite to the assumption. Over 12-line normalised windows, source-only
duplication is 1,036 lines across 23 cross-file blocks, 4.7 percent. Including tests it is
1,831 lines across 48 blocks, 3.1 percent. Tests are 62 percent of the code and produce 43
percent of the duplicated lines, so per line they are about half as duplicated as source.
18 of the 23 source blocks are one pair of packages.

## Where the size difference actually goes

| bucket | here | peer | delta | legitimate |
|---|---:|---:|---:|---|
| research and experiment tooling | 18,424 | 885 | +17,539 | yes, but it is not the tool |
| generated protobuf and the `.proto` | 13,292 | 0 | +13,292 | half |
| core engine | 20,092 | 8,859 | +11,233 | partly |
| iOS target | 10,502 | n/a | +10,502 | yes |
| Android target | 6,296 | n/a | +6,296 | yes |
| browser target | 5,927 | 5,777 | +150 | yes |
| terminal target, peer only | 0 | 2,143 | -2,143 | |
| web UI and replay server | 6,333 | 2,477 | +3,856 | no |
| spec language | 6,122 | 1,407 | +4,715 | partly |
| fixture apps and example specs | 5,868 | 322 | +5,546 | mostly |
| LLM action policy | 4,036 | 0 | +4,036 | yes |
| second JS runtime and its parity tests | 3,785 | 0 | +3,785 | no |
| CLI and orchestration | 3,349 | 1,066 | +2,283 | partly |
| E2E harness and page fixtures | 1,618 | 1,868 | -250 | yes |

Roughly 44,500 lines of the gap is legitimate scope: two mobile targets the peer does not
have, an LLM policy it does not have, and two fixture applications. Roughly 8,500 is
mechanically removable generated code. Roughly 27,200 is structural.

## Correctness findings

These are defects, not style. They come first because several of them cause the tool to
report success while checking nothing, which is the worst failure mode a property-based
checker can have.

**A run that spent half its budget outside the app enters the survival analysis as clean
data.** `cmd/internal-tools/campaign/summary.go:50` writes `precondition_failures`, and
neither reader declares the field: `analyze/load.go:32` and `confusion-matrix/checker.go:56`
each redeclare the campaign record independently. `summary.go:188` counts those steps into
`Steps`, and the exclusion logic at `analyze/load.go:190-204` filters on four conditions,
none of which is this one. `summary.go:45-49` states the requirement in its own words,
"counting it as a run that explored and found nothing puts a harness failure in the same
column as evidence", and then the pipeline does exactly that. The record schema being
declared three times is why: `analyze/load.go:45` already carries a `duration_millis`
back-compat shim for an earlier break that one shared type would have prevented.

**iOS answers three contract methods with fabricated success.**
`internal/driver/ioscompanion/driver.go:1115-1125` returns `[]` from `RecentLogs`, zero from
`Metrics`, and a hardcoded `Ready: true` from `Health`. The preflight at
`internal/runner/runner.go:1576` therefore checks nothing on iOS, and every `state.logs`
property holds vacuously with no machine-visible signal that it never looked. The fix is
not to implement them, it is to move them off the mandatory interface so that silence is
representable.

**A truncated page is served as a 200.** `internal/replay/server.go:245` reimplements
`io.ReadAll` and swallows every error except `fs.ErrInvalid`, returning a partial buffer
with a nil error. One caller, 18 lines, and a half-rendered `index.html` is indistinguishable
from a whole one.

**Actions render as raw text in the replay UI.** `replay-ui/src/lib/action-format.ts:9-19`
lists `textPrefix` and `classPrefix`, which are not in the canonical key list at
`pkg/spec/test/fixtures/selector-keys.json`, and omits `testTag`, `data-testid`, `identifier`
and `accessibilityIdentifier`. `internal/verifier/worker.go:893-915` writes `testTag:` labels
and `pkg/spec/src/web-runtime.ts:1124-1135` writes `data-testid:`. `testTag` is the primary
Android identification route, so the most common case is the one that renders wrong.

**A failed CDP round trip is indistinguishable from a page without the API.**
`internal/driver/chrome/driver.go:983-985` swallows its own error and returns zero metrics
with a nil error.

**541 lines of tag-gated tests never run in CI, and 167 of those run nowhere at all.**
Four test files sit behind build tags. `internal/sidecarassets/embed_withsidecar_test.go`,
167 lines including a four-goroutine torn-write race test, is gated on `withsidecar`, and no
target anywhere runs `go test` with that tag: `withsidecar` appears at `Makefile:59,71,80,93`
under `go build`, `go install` and `go run` only. It has never been compiled into a test
binary. The other three, `internal/driver/ioscompanion/smoke_test.go` at 85 lines and the two
`embed_withcompanion_test.go` files at 146 and 143, total 374 lines and are reachable only
through `make test-companion` at `Makefile:148`, which CI never invokes: `ci.yml` runs `make
test`, `make test-folio` and `make test-browser` and nothing else that tests Go.
Separately and less severely, 671 lines are compiled but skipped at runtime for want of an
environment variable, 105 in `internal/driver/ioscompanion/transport/integration_test.go`
needing `SANDERLING_IOS_INTEGRATION`, which is set nowhere in the repository, and 566 in a
subject-specific draft needing a spec directory named by an environment variable. Those two are guarded
by design; the 541 are not. This is a coverage gap wearing bloat's clothing and it outranks
every deletion in this document.

**One test does not test its subject.**
`internal/runner/runner_test.go:2104`, `TestRunner_InternalApplyErrorMarksTransitional`,
asserts only `err != nil` and `summary.Steps < 2`. Deleting the entire transitional-marking
branch leaves it green. Its helper at `:2084` also carries a comment saying the first
`InputText` call fails, while the method it overrides is `TapSelector`.

**One test is racy by construction.**
`internal/driver/ioscompanion/supervision_test.go:47` sleeps 100ms hoping a shell has
installed `trap "" TERM`. If it has not, plain SIGTERM works, the escalate-to-SIGKILL path
the test name promises is never exercised, and the test passes anyway.

## Structural findings, ranked by payoff

**Move the research tooling out of the product module.** 16,491 lines leave `cmd/`, and
`internal/tracecorpus` at 235 lines goes with its only two consumers. They compile and test
today only because `go test ./...` sweeps them up, at a measured 23.3 seconds per run.
`docs/development/decisions.md:49-53` is the only written policy governing this directory,
it says the directory holds two dev and debug binaries and should stay "for now", and it now
holds eleven. Two of the eleven should not simply move: `bundle-check` is the only one a
spec author wants and should fold into the product as `sanderling spec check`, since it
already runs the product code path; `hier-check` should be deleted, as nothing references
it, its usage string at `main.go:12` promises `<dump.xml>` while it parses JSON, and its
`main_test.go` never calls its own code.

**Delete the page-side JavaScript and keep one producer of DOM facts.** DOM fact derivation
and selector resolution exist four times: `pkg/spec/src/web-runtime.ts`; roughly 350 lines of
untyped JavaScript inside a Go raw string literal at `internal/driver/chrome/driver.go:578-934`
where prettier, `tsc` and `noUnusedLocals` are all blind to it; `internal/hierarchy/hierarchy.go`
with its own key list at line 302; and the replay-UI copy above. The evidence that this is a
standing hazard rather than a tolerable copy: every one of the nine commits that has ever
touched `web-runtime.ts` also touched `driver.go` or `hierarchy.go`. Nine out of nine. It is
currently held together by roughly 2,500 lines of parity tests, four probe entry points and
about 40 cross-reference comments. The drift postmortems in the file all report the same
outcome, at `web-runtime.ts:389-393`, `:555-559` and `:590-594`: the two sides disagreed and
properties passed vacuously rather than failing. `:161-166` records that the worked example in
`docs/manual/spec-language.md` named a control by a key that matched nothing on web, and passed
having checked nothing. Having the Go dump call the already-injected bundle removes the copy
and retires most of that parity suite.

**Trim the vendored protocol.** 43 RPCs are declared, 9 are called, and 2 of those 9 are
dead: `internal/driver/ioscompanion/runner.go:302` says so in its own comment, "the driver
does not call these today; they complete the Companion interface". Generating only the 14
messages actually used removes roughly 8,500 lines with no behaviour change, and dropping
install and uninstall removes 133 more including `tarGzipDirectory`.

**Collapse the twin sweep binaries.** `corpus-sweep` and `implementation-sweep` share 331
exactly-identical meaningful production lines and 163 more between their two end-to-end
tests, which are 301 and 276 lines respectively, the two longest functions in the
repository. Ten of eleven flags are identical. The entire difference is whether an
implementation needs a build step first, which is a boolean.

**Narrow `DeviceDriver` from 18 mandatory methods to 12.** `Screenshot` has zero callers:
all three `Snapshot` implementations use their own path at `chrome/driver.go:807`,
`ioscompanion/driver.go:1057` and `sidecar/client.go:269`. It exists for symmetry with
`Hierarchy`. `RecentLogs`, `Metrics` and `Health` become optional, which is what stops iOS
having to lie. `Launch`'s `env` map becomes a capability that only the sidecar implements,
since iOS rejects it and chrome silently drops it. The six optional capability interfaces
are the right shape and should not be collapsed: each has a real consumer in the runner and
a real non-implementer, which is that pattern working correctly.

**Stop maintaining two ASTs for one language.** `internal/verifier/bindings.go:28-58`
declares a 10-value `specKind` enum and an 8-field `formulaSpec` with integer child edges
that mirror `ltl.Formula` one for one, and `internal/verifier/worker.go:259-341` is an
83-line translator back, containing eleven copies of the same three-line child-and-error
block. Storing `ltl.Formula` directly deletes around 120 lines and, more importantly, takes
adding one operator from eleven edit sites to one. The isolation argument does not apply:
`worker.go:287-294` already constructs `ltl.EventuallyFormula` literals by hand, so the
boundary is already crossed.

**Replace the hand-rolled ordered-JSON encoder.** `internal/verifier/marshal.go:392-570` is
179 lines guaranteeing field order and unescaped HTML across the two hosts. `encoding/json`
already emits declaration order, `omitempty` already handles presence, and
`SetEscapeHTML(false)` is already in use at `marshal.go:520`. Tagged structs plus
`goja.TagFieldNameMapper` do the same job in roughly 90 fewer lines.

**Merge the two asset packages.** `companionassets` and `runnerassets` are the same 140-line
package twice, differing in 8 cosmetic hunks, and four dead exports fall out of the merge.
Both also validate tar member paths through `safeJoin` but do not validate `header.Linkname`
before `os.Symlink`.

**Deduplicate the smaller copies.** `decodeDump`'s body is pasted byte for byte inside
`MapHierarchy` in the same package, `input.go:420-433` against `hierarchymap.go:77-88`.
`Bundle` at `internal/bundler/bundler.go:36` and `BundleWeb` at `web_bundle.go:30` are 132
code lines differing in three expressions, and `web_bundle.go:37` calls `filepath.Abs` and
discards the result before calling it again at line 44. The route-transition rule exists
three times with three different key sets, at `hierarchy.go:654` with one key, `settle.go:123`
with five, and `chrome/driver.go:902` with one, and they already disagree.
`Tree.FindBySelectorPath` and `Node.FindBySelectorPath` differ in three lines, all `t` against
`n`, as do the `FindAll` pair. `parseBounds` at `hierarchy.go:1029-1053` contains the same
11-line body twice, once per regex.

**Retire the dead instrument.** `Tree.UnreadableFlags` at `internal/hierarchy/hierarchy.go:106`
is computed at `:554`, serialized twice, and read by zero production callers. Its two JSON
keys disagree with each other, `unreadableFlags` against `unreadable_flags` at `:120`, and
every `json` tag on `Tree` is dead anyway because `Tree` has a custom `MarshalJSON`.

**Undocumented escape hatch.** `SANDERLING_SIMULATOR_COMPANION=legacy` at
`internal/driver/ioscompanion/driver.go:404` appears in no document, Makefile or CI config,
and gates roughly 313 lines: `pasteText`, `findAllowPasteButton`, `pasteLanded`,
`grantPasteboardAccess` and `resolveInputField`. Document it or delete it. Leaving it
undocumented and live is the worst of the three options.

**Dead code, tool-backed.** `deadcode -test=false ./...` reports 48 unreachable first-party
functions; with test roots included that falls to 1. So 47 of 48 are reachable only from test
binaries, which is over-exported test scaffolding rather than dead product code. The single
genuinely dead one is `Cache.Root` at `internal/replay/runs_cache.go:29`. `staticcheck
-tests=false` reports 12 `U1000`, eleven of which are one statistics island in
`cmd/internal-tools/analyze` that `deadcode` independently flags, so both tools agree.
Confirmed zero-caller exports in the product: `ltl.Describe` at `formula.go:235`,
`ltl.EventuallyBefore` at `:158`, `ltl.EventuallyWithin` at `:162`, `ltl.Thunk`,
`ltl.Eventually`, `Evaluator.Observe` at `evaluator.go:81`, and
`trace.ActionSourceSeeded` at `writer.go:118`. The `ErrorFormula` arms in `reduce`
(`evaluator.go:395-399`) and `pushNot` (`nnf.go:49-50`) are unreachable, since `ErrorFormula`
is built only at `worker.go:771` for serialization and never re-enters an evaluator.

Two more the tools did not reach, both found while correcting the comments beside them.
`isWDADrop` in `internal/runner/runner.go` and its two call sites classify an error string
nothing can produce any more: `WdaRecovery` is constructed only in its own test
(`DriverBackend.kt:1557`, `WdaRecoveryTest.kt:16`), so the sidecar no longer throws it.
`testrun.preflightDevice` was dead in a way that also made it wrong, and is deleted: it was
called only after web and both iOS branches had returned, so it never saw a platform other
than android, and it returns nil for anything but ios. The comment over its call site said it
was Android's java check, which is in `runPreflight`, and its own premise was wrong because a
physical iOS device is runner-only over usbmux and needs no JVM.

**One dead feature spans four layers.** `trace.Step.Snapshots` at `internal/trace/writer.go:27`
has no producer, yet `replay-ui/src/panels/SnapshotTable.tsx` renders a tab for it, wired at
`RunDetail.tsx:141`. Every step of every run shows an empty panel. Separately,
`replay-ui/src/panels/Timeline.tsx`, 142 lines, is never rendered: its only importers use
`import type`, and the `RunHistory.lanes` tail it feeds has zero readers.

**`VERB_SUPPORT` is a no-op.** `pkg/spec/src/verbs.ts:18` maps every verb to all three
platforms, so `supports()` cannot return false, which makes `warnUnsupportedOnce`,
`resetWarnings`, `Host.reportUnsupported` at three call sites, one `pick.ts:103-106` branch
and the whole of `verbs.test.ts` unreachable, along with 26 pointless `resetWarnings()` calls
in `pick.test.ts`.

**`runner.go` is not mostly ceremony.** At 1,818 lines it is 1,233 code and 500 comment, and
it does five jobs in one file rather than 500 lines of work in 1,818. It splits along its
existing seams into runner, foreground, dispatch, observe and trace encoding, largest piece
around 420 lines, with no logic change. `Run` is 473 lines with a cyclomatic complexity of
60, the highest in the repository.

**One abstraction is justified by a false statement.** `internal/runner/source.go:41` says the
interface is declared there "rather than folded into driver.WebDriver so the mobile drivers
stay untouched", but `WebDriver` is web-only and no mobile driver implements it, so the error
branches at `source.go:102-116` are unreachable. `idleTimeoutFloor` at `runner.go:630` cites
the same false reason. Similarly, `ltl.PredicateLabel` at `internal/ltl/formula.go:17-22`
exists so that `formula.go:394` can type-assert a `ThunkFormula` that cannot fail the
assertion, replacing a single field read.

**One correction to an audit finding.** The `{"op":"now"}` serialized node was reported as
having no consumer. It has one: `replay-ui/src/types.ts:106` declares it in the `ResidualNode`
union and `replay-ui/src/components/ResidualNode.tsx:41` renders it. The agent searched for
`web/src`, which does not exist here, and concluded the UI did not either. `NowFormula` is
still the identity function at every evaluator site, but the wire node is load-bearing and
must not be dropped.

## The test suite, measured

| package group | test | source | ratio |
|---|---:|---:|---:|
| runner, drivers, testrun, cmd | 32,886 | 23,166 | 1.42 |
| `internal/runner` alone | 7,270 | 2,589 | 2.81 |
| `internal/verifier` | 5,218 | 3,179 | 1.64 |
| `internal/hierarchy` | 1,886 | 1,063 | 1.77 |
| `internal/ltl` | 1,595 | 1,045 | 1.53 |

Genuinely deletable across both audited scopes is roughly 2,600 lines, about 5 percent, not
the 40 percent the size ratio suggests.

Every one of the 1,281 test functions was then classified mechanically by the condition
guarding its assertions. 47 functions, 658 lines, assert only on nil, on `err`, or on a length
against zero. 896 functions compare actual values and 315 more compare against a golden file
or by deep equality. The residual "no assertion" bucket is 23 functions and is almost entirely
an artefact of the classifier, since these tests embed spec source in raw string literals
containing a brace at column zero; the genuine case is `mock_test.go:123`. So the mechanically
detectable garbage is roughly 800 lines, which reproduces the reading-based estimate from a
different direction.

That figure is a floor, not a ceiling. The method cannot see tautology:
`internal/driver/ioscompanion/keymap_test.go:202` compares two values and is worthless,
because the values are constants declared four lines away in `keymap.go:32-33`. Tests of that
shape score as healthy. The honest statement is that the suite's assertions are overwhelmingly
real, and that an unmeasured remainder above 800 lines is tautological.

The bloat is not in the tests. Go test files hold 43,435 lines, of which 29,770 sit inside a
test function. The other 13,665 lines, 31 percent of the suite, are scaffolding: 256 test-only
helpers, 65 test-only types, 26 one-off wrapper drivers in a single package, `traceLine`
redeclared fifteen times, and four separate spec bundlers in one package's tests. Consolidating
the harness is worth several times what deleting tests is worth, and it costs no coverage. What is actually wrong is repetition of shape:

- 26 one-off wrapper drivers live in one package, ten of them in `runner_test.go`, six of
  which override `TapSelector` to return a different error on call N. `mock.go` already has
  `Failures map[ActionKind]error`; one per-call hook retires all 26, about 280 lines.
- `type traceLine struct` is redeclared inline ten times inside `runner_test.go` and five
  more times across the package, while `readTraceLines` sits at `runner_test.go:1595` and
  `trace.jsonl` is opened by hand in 24 places. Two fields on `traceStepLine` collapse all
  fifteen, about 250 lines.
- 57 of 98 `internal/hierarchy` tests have bodies of 12 lines or fewer, nearly all one row of
  an unwritten table. Nine clusters, roughly 650 lines, collapse into tables. Four properly
  table-driven tests already sit in the same files, so the pattern was known and not applied.
- 72 of 98 hierarchy tests write `tree, _ := Parse(...)`, so a fixture that stops parsing
  panics on a nil dereference inside `Find` instead of reporting a parse failure.
- Four separate spec bundlers exist in `internal/verifier`'s tests, differing only in the
  alias map.
- Confirmed subsumed: `chrome/driver_test.go:140`, `:202` and `:264` are superseded by
  `fact_parity_test.go:92`, which reads the same fields with forced both-polarity coverage
  and cross-checks the real web runtime, while the middle of the three does not call the web
  runtime at all despite its name. 163 lines. Also `runner_test.go:351` by `:374`,
  `runner_test.go:240` by `llm_source_test.go:837`, and `runner_test.go:1226` by
  `scroll_distance_test.go:52`.

Three tests cannot go red at all. `internal/driver/mock/mock_test.go:123` is a compile-time
assertion wearing a `func Test` hat, with no assertion and no use of `t`.
`internal/driver/ioscompanion/keymap_test.go:202` asserts two constants equal the literals
they are declared with four lines away in `keymap.go:32-33`.
`internal/verifier/llm_test.go:674` installs a stub returning `"sampled"` and asserts the
function returns `"sampled"`, under a name claiming it draws from a corpus that is never
reached.

Two structural gaps matter more than any of the above. The two-host parity tests at
`internal/verifier/marshal_test.go:166` and `:299` assert that the two encoders agree with
each other, with no golden anywhere, so a field rename landing on both encoders at once,
which is the likely change, passes silently. The same gap exists at `redaction_test.go:174`
and `:193`. And `cmd/internal-tools/hier-check/main_test.go:11-21` tests `hierarchy.Parse`
and `tree.FindAll` directly, so if hier-check's own formatting or exit codes break, nothing
notices.

## Rule compliance

| rule | verdict | count |
|---|---|---|
| zero comments first, WHY never WHAT | ignored on volume, followed on quality | 7,534 comment lines under no linter mandate, 35% WHAT in a 60-block sample |
| never use an em dash | followed in source | 0 tracked, 7 in commit messages, 43 in gitignored `research/` |
| never write a directory tree | one violation | `docs/manual/runs.md:23-27` |
| README under 15 lines | partly | 3 of 5 over budget, 277 lines total |
| lowercase kebab-case doc names | partly | 11 flagged, 1 committed and discretionary |
| no abbreviated identifiers | ignored | 683 occurrences, 51 distinct identifiers |
| commits 1-3 files, under 20 lines | ignored on lines, followed on files | 1,216 of 1,864 commits over 20 lines, 194 over 3 files |
| no filler, every line load-bearing | partly | 123 of 1,565 comment blocks restate the code |

Two of these need care rather than a sweep. `AttrFilter`, `AttrSelector` and the `Attr`
field are baked into the public selector API and leak into `pkg/spec/src/types.ts`, so that
abbreviation cannot be removed without a breaking change; it should be scheduled, not
grepped away. And `ctx context.Context` is idiomatic Go and was excluded from the count.

Go convention does not ask for fewer comments in general, it asks for doc comments on
exported symbols and for inline comments to be rare. Measured against that, this repository is
close to correct: 3,512 doc-comment lines against 22,155 lines of code, and only 1,164 inline
comment lines, 5.3 percent. Three quarters of the comment volume is the convention being
followed. Comparing a single total against a project that writes few doc comments overstated
the problem, and the earlier 20.8 percent figure conflated the two kinds.

The comment rule failed hardest outside the engine.
`examples/folio/sanderling/predicates.ts` is 61.7 percent comment, 472 comment lines to 293
code, with two inline blocks of 13 and 17 lines wrapped around about 12 lines of guard
clauses. `pkg/spec/src/action-tree.ts` is 47 percent comment. Inside the drivers the
opposite holds: nearly every comment records a measured symptom, and the genuine
WHAT-comments there total only 60 to 80 lines, most of them on the legacy paste path that
should be deleted anyway.

Comment rot is the single largest defect in the repository's prose, and the first pass of
this audit understated it badly. A per-block classification of 1,565 comment blocks, 95
percent of every comment in non-test source, finds **78 that are false**: they disagree with
the code beside them. The first pass named two. Detail is in the comment section below.
`internal/runner/runner.go:185` claims a returned error propagates to every sibling read,
while all three goroutines return nil at lines 198, 202 and 207, and line 209 admits the
`Wait` error is always nil, so the errgroup is a `sync.WaitGroup` plus a dependency plus an
untrue comment. `internal/testrun/testrun.go:93-99` stacks three doc comments over the wrong
function, leaving `Execute` and `buildRunMeta` undocumented. The worst single instance is
`internal/runner/runner.go:760-768`, nine comment lines above one constant, eight of them
narrating a rejected design and two emulator measurements. The measurement is worth two
lines; the rest is a lab notebook checked into a runner.

## Repository hygiene

Nothing untoward is tracked at HEAD. 674 files, 5.32 MB, no secrets, no build artefacts, no
vendored bundles, no `dist`, no `node_modules`. The problems are all around it.

`.git` is 659 MB for 5.32 MB of content, a ratio of 118 to 1. The largest object in the
repository is a 138.9 MB compiled binary carrying the dead pre-rename project name, blob
`9788374f`, reachable only from stash commit `6bb190f`, "untracked files on feat-inspect-ui".
A `git stash` swept up a compiled binary and `refs/stash` has pinned it ever since.
`.git/lost-found/other` holds a further 152 MB of `git fsck` salvage that no ref points at,
and there are 12 packs, 479 prune-packable objects and 65 local branches against 42 remote.
Separately, a 35.4 MB compiled binary is permanent in `master` history, added in `88db0cb`
and removed in `2a1b263`, along with 426 KB of UI bundles: 35.8 MB total, about 6.4 times the
entire legitimate tree and roughly 70 percent of what a fresh clone downloads. Ignoring these
afterwards stopped recurrence but every clone still pays.

`keys/` holds live Apple distribution secrets: an `AuthKey` `.p8`, a distribution private key
PEM, a `.p12` and a mobileprovision. History is clean and nothing has leaked. The only thing
standing between them and a commit is one line, `keys/*`, appended at `.gitignore:87` with no
section comment, and `git check-ignore keys` shows the directory itself unmatched. Signing
material belongs in the keychain, not in a git working tree. `scripts/` is currently
untracked and unignored, so it is one `git add -A` away from being committed, which is one
reason the project bans that command.

The working tree holds 9.16 GB of untracked output: 4.3 GB under `conformance/runs`, 2.7 GB
of run artefacts under `examples`, 92 MB of Gradle jars under `sidecar/build`, 141 MB under
`companion`, 37 MB each for `bin` and a stray `sanderling` binary, 25 MB in `runs`. Five
empty `idb-*` directories and an empty `memory/` directory have sat there since April and
June and are not ignored.

Twelve of 33 make targets are dead or undocumented, `conformance/gates.sh` at 574 lines with
a self-test mode and committed fixtures is referenced by nothing, and there is no CI format
check despite five `fmt-*` targets existing.

Documentation is accurate where it counts and stale in three places. Twelve of fifteen
spot-checked claims verified against the code, including every one of the 22 CLI flags, which
are all documented in `docs/manual/cli.md` with no undocumented and no dead flag. The three
failures: `docs/manual/action-space.md:15` says `PressKey` has 8 keys where
`pkg/spec/src/types.ts:209-218` has 9, missing `escape`; `action-space.md:14` documents a
`durationMillis?` field on `Scroll` that does not exist; and `docs/development/decisions.md:25-27`
says `marshal.go` moves to `internal/replay/`, which never happened, while `:47-49` describes
a `runs.go` split as pending that has already been done. `README.md` is 40 lines against its
own 15-line standard, with lines 36-40 byte-identical to `docs/index.md:25-29`, the manual
link list repeated four times and the pitch three. `examples/folio/README.md` is 150 lines
with nine headings that are all `docs/` content. `decisions.md` and `action-space.md` are
orphaned, stale and linked from nowhere.

## What must not be touched

A cleanup driven by line count would destroy the best work in the repository. The following
were examined and found to be load-bearing.

`internal/ltl/evaluator.go` in full: `reduce`, the three-valued `finalize`, the bounded
always and eventually duality at `:528-540`, `collapse`'s `describe()` key with per-thunk
identity, `nnf`'s implication rewrite and `isOneShotRoot`. It is a real standalone evaluator
carrying both step and wall-clock bounds, where the peer project's equivalent carries
duration only. It is the intellectual content of the project and it is dense because the
problem is.

`internal/verifier/redaction.go` in full, including the three-valued `secureFact` that treats
an unreported target as a credential, which is the correct default. The
`attributeAliases` and `selectorKeys` tables, `recordableValue`, `Node.scopedNodes`' spatial
fallback, `innermostMatches`, and the `ActionWireContract` handshake mechanism, which is
versioned precisely because the format already drifted once and every scroll silently
travelled zero distance.

In the runner: `clampGestureToSafeArea`, `structuralShape`, the transitional-skip and action
hold-back at `runner.go:254-356`, `confirmFocus` and `otherElementHoldsFocus`, the
extractor-override consistency checks at `:309-324`, `fetchSyncedState`'s unchanged-JSON
early exit, `resolveIdleTimeout`, `applyBound`, `runOutcome`'s three error types, and all of
`internal/seedspec`, `internal/llmclient` and `internal/replay/watcher.go`.

In the drivers, roughly 60 items flagged as battle scars: the 354 to 616 pixel fling range,
the 136ms Compose accessibility lag, the four-minute XCTest wedge, and the rest of the
measured device quirks. Chrome and iOS are dense because those platforms are hostile. The
optional capability interfaces are the right pattern for heterogeneous targets and should
survive the narrowing of the mandatory set.

In the tests: the cross-host golden tests, `parity_test.go`, `host_parity_test.go`,
`selector_keys_test.go`, `extractor_encoding_test.go` and `policy_parity_test.go`. They
assert wire bytes against committed goldens, they name the bug class, and several clearly
went red once.

## Order of work

Correctness first, because these make the tool lie: the `precondition_failures` exclusion in
the analysis pipeline, the three fabricated iOS health responses, the partial read in the
replay server, the selector vocabulary drift in the replay UI, the swallowed CDP error, and
wiring the 541 lines of tag-gated tests into CI.

Then the removals that carry no behaviour risk: move the research tooling out, regenerate the
protocol from the 14 messages actually used, delete the dead `Snapshots` panel and `Timeline`,
delete `VERB_SUPPORT` and its unreachable tail, merge the two asset packages, collapse the
twin sweep binaries.

Then the structural changes, each of which needs its own branch and its own green CI: one
producer of DOM facts, a 12-method mandatory driver interface, `ltl.Formula` stored directly
in the verifier handle, `encoding/json` in place of the hand-rolled encoder, and `runner.go`
split along its five existing seams.

The test suite comes last and is a consolidation, not a purge: one per-call failure hook on
the mock, one `traceStepLine`, nine table collapses in hierarchy, and goldens where two
encoders currently only agree with each other.

Doing all of it puts the tool at roughly 60,700 lines with a further 18,400 in a separate
research module. That is 2.4 times the peer project's own tool, which is earned by two mobile
targets it does not have, an LLM policy it does not have, two fixture applications, and a test
ratio deliberately kept above its 0.35.

## Second pass: how the tests are built, not what they assert

The first pass measured whether tests could be deleted without losing coverage and answered
"mostly no, about 5 to 10 percent". That answered the wrong question. Deletability and
engineering quality are independent axes, and on the second axis the suite fails its own
standard comprehensively. The rule says tests are first-class code held to the same or
higher standard than the code they cover. Measured against source, they are held to a
visibly lower one.

Go, 135 test files, 41,988 lines, 1,259 test functions:

| measure | value |
|---|---|
| test-only helpers | 457, totalling 4,560 lines |
| of those, pure duplication | about 1,860 lines, 41 percent of the helper layer |
| test-only types | 67 top-level plus 20 function-local |
| `t.Run` subtests across 1,281 test functions | 108 |
| `t.Parallel()` anywhere in the repository | 0 |
| `cmp.Diff` / testify / `reflect.DeepEqual` | 0 / 0 / 11 |
| hand-rolled comparison assertions | about 97.8 percent |
| clusters that should be tables | 51, covering 355 tests and 5,758 lines |

The specific duplications, each verified: five spec bundlers differing only in an alias map
(`verifier_test.go:42`, `setup_action_test.go:18`, `spec_integration_test.go:16`,
`action_encoding_test.go:36`, and `internal/runner/runner_test.go:131`, of which the first
and last are character-identical apart from the name; the last is in a different package, so
four of the five sit in `internal/verifier`); `traceLine` redeclared 14 times in
`internal/runner` while `readTraceLines` and `traceStepLine` sit at `runner_test.go:1586-1595`
and are a superset of nearly all of them; 27 wrapper driver types in one package, of which
`snapshotFailFirst:1925`, `tapSelectorFailFirst:1998` and `internalApplyErrorFailFirst:2087`
are the same seven-line method with a different error constant, while `mock.go:64` already
has `Failures map[ActionKind]error` and needs only a call-count field; 43 byte-identical
eight-line Chrome launch preambles across four files, 344 lines; 48 identical
`context.WithTimeout` plus `Run(ctx, Options{})` blocks, 576 lines, beside a `harness` struct
at `runner_test.go:78` that has four fields and no methods; and 347 identical lines shared
between `corpus-sweep/end_to_end_test.go` and `implementation-sweep/end_to_end_test.go`,
including six byte-identical helpers.

A shared `internal/testsupport` package holding file and run-directory writers, one trace
reader over one wide step type, a tree builder, a Chrome page helper, and the spec bundler
removes about 1,860 lines without touching a single assertion. Collapsing the 51 table
clusters removes about 1,700 more. Roughly 3,560 lines in Go, with the suite asserting
strictly more than it does now, because the next test becomes cheap instead of becoming
another copy.

The same shape holds in the other two languages. In Kotlin, `StubDriverBackend` is declared
in production source at `sidecar/src/main/kotlin/dev/sanderling/sidecar/DriverBackend.kt:507`
and wired as a live backend fallback at `Main.kt:93`, carrying an injectable `commandRunner`
seam and five `@Volatile` spy fields that ship in the production jar. Eight test files import
it as their fake. The suite has no fake of its own because the fake was moved into `main`.
Alongside that, `sidecar/build.gradle.kts:56-62` declares kotlin.test, JUnit 4.13.2, JUnit
Jupiter 5.11.3 and the vintage engine to run 4 under 5, with 12 files importing
`org.junit.Test` against 3 importing `kotlin.test.Test`. `countRouteScreens` is tested from
two files with two separately invented tree builders, and 15 sidecar test files target two
production files and should be about 7.

In TypeScript, `.each` is used zero times while 26 files hand-roll `for (const x of [...])`,
and 61 percent of the 450 cases carry exactly one assertion.
`folio-submit-balance-predicate.test.ts` is 625 lines over 40 cases, all driving
`const submitOn = "testTag:LedgerScreen > testTag:TxnSubmit"` at line 9. `TxnSubmit` exists
in exactly one place in the application, `AddTransactionScreen.kt:88`, and `LedgerScreen` does
not contain it, so that selector cannot match anything real. Its sibling
`folio-submit-window.test.ts:11` declares the same constant correctly. A duplicated fixture
constant, copied rather than imported, edited to something fictional, with 625 lines built on
top, all green. The predicates under test are pure, so the arithmetic is still exercised; the
failure is that the fixture claims to model the application and does not.

Inline fixture bloat, which the first pass expected to find, is not a real problem in any of
the three languages. Kotlin has zero literals over 30 lines. TypeScript has zero standalone
data literals over 30 lines. Go has three raw strings over 30 lines totalling 120. The
committed `testdata` corpus is 88 files, 264 KB, 3,497 lines, and is the best-managed part of
the suite: `conformance/testdata` is 11 gate fixtures regenerated by a committed
`generate.py`, and `.github/scripts/testdata` is six captured golden runs consumed by CI.
Near-duplicates across all of it total about 90 lines. The growth is in test code, not test
data. What Go does have is 395 hand-written hierarchy JSON nodes across 28 files with no
shared builder, and `testdata/` in only 6 of 33 packages;
`internal/hierarchy/hierarchy_test.go` is 1,823 lines with the largest inline corpus in the
tree and no `testdata/` directory at all, while eight other packages already have one.

## Second pass: the growth pattern

Across the last 50 commits, test lines added 51,444 against 3,891 deleted, a ratio of 13.2 to
1. Source over the same window is 5.4 to 1. Tests are removed at roughly a third the relative
rate of source.

In the last 30 commits, **43 test files were added into a package that already had a file
testing the same unit**. `internal/runner` took 14 of them alongside the existing 3,181-line
`runner_test.go`, and they carry the duplication that decision implies: five near-identical
web-target fakes each redeclaring the same five methods, and five hand-rolled trace scanners
each doing `bufio.NewScanner` with an 8 MB buffer beside the existing `readTraceLines`.
`internal/driver/chrome` took four, carrying four byte-identical probe installers, four
matching probe entry files and four fixture pages. `test/browser` has four spellings of one
harness: `runFixture`, `executeFixture`, `launchConsoleFixture`, and `servePage` plus
`runBinary`. `pkg/spec/test` took eight folio files against three that existed, so
`countSubmitsInWindow` is now tested from five files and `readHomeCards` from four, each
redeclaring its own builders.

Only three commits in the last 60 have a negative test delta, and all three are subsystem
removals: the in-app SDK, the JVM iOS backend, and a rename. Exactly one commit in 60 deleted
test content because the tests were bad, `faebfe3`, which cut
`examples/folio/sanderling/spec.ts` by 37 added against 122 deleted, removing properties that
could not fail.

The commit named `test: full test-suite refactor sweep (#61)` is the clearest single case. It
is net plus 2,674 lines. Its body carries 34 bullets of which one is a deletion. Of its 125
deleted test lines, one is a genuine rewrite, `internal/ios/ios_test.go` at minus 53, beside
a real extraction of pure simctl parsers; the remaining seven files give up 8 lines or fewer
each. It also added eight new test files. A refactor that deletes nothing has not refactored
anything.

Two counterexamples belong on the record, because the discipline has been demonstrated here
and simply not repeated: `#52` rewrote `api.test.ts` at plus 125 against minus 134 and
`defaults.test.ts` at plus 23 against minus 93, both in place, both deleting more than they
added. `cmd/internal-tools/analyze` is properly layered, with one `writeCampaign` and five
thin scenario wrappers delegating to it. The pattern was available in this repository the
whole time.

One nuance argues against a naive purge. `internal/driver/chrome/fact_parity_test.go`
supersedes four tests in `driver_test.go`, but parity asserts that the two fact producers
agree, not that either is right, so those four still pin ground truth. The correct move was
to fold them into the parity fixture. Neither folding nor deletion happened, and
`element_state_test.go` then repeated the whole pattern a third time on a fourth fixture page.

## Second pass: compatibility code in an alpha project

`README.md:25` states the project is alpha. Genuinely deletable compatibility machinery is
454 source lines and 558 test lines, rising to 493 and 632 once a stale trace corpus goes.

The largest single item was reported as the `action-wire/2` revision check,
`internal/verifier/marshal.go:598` plus four files, on the grounds that it never reads
revision 1 and so buys only a better error message. **That reading was wrong and the entry is
withdrawn.** Refusing a pairing is not supporting it: the check exists because
`@sanderling/spec` 0.0.3 and earlier serialized an authored `Scroll` with the container's own
point as both endpoints, and this binary reads pre-computed endpoints as authoritative, so
every such scroll dispatched successfully as a 250ms press and hold that travelled zero
distance and no run reported it. Without the handshake the two halves run to completion and
the campaign is void. It stays, and this pull request finishes it. The first item is `SANDERLING_SIMULATOR_COMPANION=legacy`, gating about 330
lines and appearing in no document, Makefile or CI configuration. Third,
`cmd/internal-tools/analyze/load.go:43` carries a `duration_millis` shim for campaign records
written before two clocks were split: across the committed corpus, 0 of 308 campaign records
carry `duration_millis` and 308 of 308 carry `monotonic_millis`, so it defends against
nothing. `load.go:55` makes `Actions` a pointer to refuse older files whose field is present
on 308 of 308 records.

The more important finding is the opposite of the brief. **Four comments label current,
documented, dominant behaviour as legacy or backward-compatible**, and acting on them deletes
live surface: `internal/verifier/bindings.go:217`, `internal/driver/chrome/translate.go:16`,
`internal/hierarchy/hierarchy.go:8` and `cmd/sanderling/doctor.go:27`. A fifth,
`cmd/internal-tools/analyze/load.go:62`, was listed and does not belong: it explains why
`UnattributedActions` is a pointer, which is a live refusal two tests assert, and it never
calls anything legacy. All four of the real ones are fixed. The clearest is `bindAlways`, whose comment calls a
predicate function the "legacy shape" and a formula handle the "new shape". **This was first
recorded as the predicate shape being used 8 times and the formula shape 0, which is wrong in
both directions.** Counted against the tree: all six properties in `examples/folio/sanderling/spec.ts`
and `examples/folio-web/sanderling/spec.ts` pass a formula, the two shipped default properties
in `pkg/spec/src/defaults/properties.ts` pass a predicate, and `pkg/spec/test/api.test.ts:189`
passes a predicate. Both shapes are current and both have real callers, so neither is legacy
and the comment was wrong to rank them at all.

Ordering matters for the trace-format items. 342 of 698 committed `trace.jsonl` files carry
no `trace_version` and no `depths`, and the gates currently refuse them. Deleting the version
machinery first turns those refusals into nil-root trees that resolve no selectors, which is
silent wrong data. Delete the stale corpus first, then the code.

## Second pass: comments, classified

1,565 comment blocks were classified against the code beside them, 95 percent of every
comment block in non-test source.

| bucket | blocks | share |
|---|---|---|
| godoc on an exported identifier, correct | 540 | 34.5% |
| why, carrying a fact not recoverable from the code | 751 | 48.0% |
| restates the code | 123 | 7.9% |
| compensating for a bad name | 8 | 0.5% |
| compensating for a function that is too long | 9 | 0.6% |
| design essay, notebook, or history | 56 | 3.6% |
| stale or false | 78 | 5.0% |

The volume complaint does not survive this. Four blocks in five are godoc or a
non-recoverable fact, which is what Go convention asks for, and the 20.8 percent line-share
figure overstates the problem. About 1,332 of 6,458 comment lines, 21 percent, should be
deleted or refactored away. `internal/driver/chrome` is the strongest package in the tree, 68
of its 123 blocks being measured browser quirks with no bad-name, too-long or essay findings
at all. `examples/folio/sanderling/predicates.ts` at 61.7 percent comment is the worst.

The real defect is falsity, and several of the 78 are load-bearing:

`internal/hierarchy/hierarchy.go:9` documents `id:<suffix>` as a substring match on
resource-id. The code at `:412-414` is `ResourceID == value` or `HasSuffix(ResourceID, ":id/"+value)`,
so `id:Button` does not match `com.x:id/saveButton`. `desc:` at `:417` is worse than reported:
it is exact-or-prefix-before `", "`, not a substring match at all. This is the package doc a
spec author reads first, and `docs/manual/spec-language.md:60,63` has it right, so the package
doc was the only wrong copy. Its "(backward compat)" label on `id:` was wrong too: `id:`
appears about 375 times across the tree against 16 for `idPrefix:`, so the form marked legacy
outnumbers the other by 23 to 1.

`hierarchy.go:43` declares `Bounds` an inclusive rectangle while `Width()` returns
`Right - Left`, so every width, height and centre in the tool was off by one against its own
stated semantics. Settled against the producers: all three emit exclusive right and bottom
(`sidecar/.../DriverBackend.kt:1624-1626` states it outright, `chrome/driver.go:696` writes
`getBoundingClientRect`'s `right` and `bottom`, `ioscompanion/hierarchymap.go:190` writes
`left + width`), and `runner.go` reads `screen.Width()` as the viewport width, which only
holds under the exclusive reading. The code was right, the comment was the only wrong copy,
and a test now pins the arithmetic on the abutting and one-pixel cases the two conventions
disagree on. Separately, the two `parseBounds` pattern comments named the wrong producers in
both directions: the paired `[x1,y1][x2,y2]` form is what both device backends emit, and the
flat one is chrome and the sidecar stub.

`internal/driver/driver.go` was documented from the Android sidecar and never revisited for
Chrome, producing six false statements on the interface every driver implements: `:52` says
an empty `minLevel` defaults to "E" while `chrome/driver.go:823` returns every level; `:47`
says `Snapshot` runs under a backend-side mutex while `chrome/driver.go:802-812` takes no
lock, and that claim has already been copied into `runner.go:1430`; `:58` calls `HeapBytes`
RSS while Chrome feeds it `usedJSHeapSize`; `:101` says the gate prefers `FocusedWindowChecker`
while `runner.go:836` requires `ForegroundChecker`; `:175` names a `WebAction` type that
exists nowhere.

iOS stopped routing through the JVM sidecar and 11 comments still say it does, including
three that name an `IosDriverBackend` type which does not exist. Eleven TypeScript comments
are anchored to Go symbols that were deleted. `verifier/types.go:22,26,29` deny Scroll its
endpoints and duration and deny DoubleTap and LongPress their coordinates. `marshal.go:95`
and `runner.go:1289` now state opposite rules for the same selector-and-coordinate conflict.
`DriverBackend.kt:141` documents `pollUntilStable` as not charging reads to the stability
streak, but `now` is sampled after `snapshot()` returns, so the code permitted exactly the
failure its own worked example presents as impossible, in the settle primitive every Android
step depends on. **This one turned out to be a code defect, not a comment defect.** On the
comment's own numbers (streak 750ms, interval 250ms, read 500ms) the poll returned having
watched 250ms of quiet. The comment arrived in `b02e86b` with the streak parameters and the
`now`-after-`snapshot` sampling was left as `88db965` wrote it. Fixed by sampling the read's
start; the existing test measured from the end of the first read, so it included an interval
plus a whole read and passed against the bug at any read length. `runner.go:95` says the caller is responsible for launching the app, and
`Run` launches it at `:913`.

The bad-name and too-long buckets are small but they are the ones where the comment is a
symptom rather than the disease. `runner.Run` at 473 lines carries five multi-paragraph
signposts, 62 lines, that exist only because there is no function boundary; extracting
`observeDevice`, `verifyStep`, `applyChosenAction`, `recordStep` and `settleAfterStep` deletes
all five. Four foreground readers with four failure policies carry 40 doc lines between them,
one of which is a 10-line comment over a single delegating line. `held := skippedVerification`
at `runner.go:352` is a pure alias that exists so that a 15-line comment has somewhere to
live. In `ioscompanion`, a type named `runner` collides with the XCUITest runner, the
xcodebuild session and `internal/runner`, which is why `driver.go:1211` has to write "the
text runner"; renaming it `textInputBackend` retires the comment.
