#!/usr/bin/env bash
# Drives folio-run.sh through a stubbed `sanderling` binary and checks the
# verdict it reaches from each shape of trace. What is under test is the
# classification, not the fuzzer: the stub writes the trace the run would have
# written and exits the code the run would have exited.
#
# folio-run.sh is invoked as `bash -eo pipefail -c <script>`, which is what a
# `run:` block does: -e is set on the shell that calls the script, and does not
# cross the shebang into it. Running the script itself under -e would kill it at
# the first non-zero `sanderling test`, which is the exit code it exists to read.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/folio-run.sh"
spec="$here/../../examples/folio/sanderling/spec.ts"
testdata="$here/testdata"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

failed=0
case_dir=""
status=0
spec_override=""

run() { # <case> <platform> <stub exit code> [none] ; trace lines on stdin
  local name="$1" platform="$2" code="$3" trace_mode="${4:-file}"
  case_dir="$work/$name"
  mkdir -p "$case_dir"
  # the web leg serves this directory and waits for index.html before it runs
  mkdir -p "$case_dir/examples/folio/app/webApp/build/dist/wasmJs/developmentExecutable"
  echo ok > "$case_dir/examples/folio/app/webApp/build/dist/wasmJs/developmentExecutable/index.html"
  cat > "$case_dir/trace.jsonl"
  printf '#!/usr/bin/env bash\n' > "$case_dir/sanderling"
  cat >> "$case_dir/sanderling" <<'STUB'
printf '%s\n' "$@" > "$STUB_ARGV"
out="" ; prev=""
for arg in "$@"; do
  [ "$prev" = "--output" ] && out="$arg"
  prev="$arg"
done
if [ "$STUB_TRACE_MODE" = file ]; then
  mkdir -p "$out/20260815-101500"
  cp "$STUB_TRACE" "$out/20260815-101500/trace.jsonl"
fi
exit "$STUB_CODE"
STUB
  chmod +x "$case_dir/sanderling"
  status=0
  cd "$case_dir"
  STUB_ARGV="$case_dir/argv" STUB_TRACE="$case_dir/trace.jsonl" \
  STUB_TRACE_MODE="$trace_mode" STUB_CODE="$code" \
  SANDERLING="$case_dir/sanderling" SPEC="${spec_override:-$spec}" \
  GITHUB_STEP_SUMMARY="$case_dir/summary.md" PORT=8796 \
  SEED=7 MAX_STEPS=240 DURATION=20m \
    bash -eo pipefail -c "'$script' '$platform'" \
    > "$case_dir/out" 2> "$case_dir/err" || status=$?
  cd "$here"
  spec_override=""
}

fail() { echo "FAIL: $*" >&2; failed=1; }

expect_status() { # <want> <case>
  [ "$status" = "$1" ] || fail "$2: exit $status, want $1"
}

# grep reads both files directly: piping `cat` into `grep -q` returns 141 under
# pipefail, because grep leaves on the first match and cat takes the SIGPIPE.
expect_says() { # <text> <case>
  grep -qF -- "$1" "$case_dir/out" "$case_dir/err" || fail "$2: nothing said '$1'"
}

expect_silent() { # <text> <case>
  if grep -qF -- "$1" "$case_dir/out" "$case_dir/err"; then
    fail "$2: should not have said '$1'"
  fi
}

expect_summary() { # <text> <case>
  grep -qF -- "$1" "$case_dir/summary.md" || fail "$2: summary has no '$1'"
}

expect_argv() { # <text> <case>
  grep -qxF -- "$1" "$case_dir/argv" || fail "$2: '$1' never reached the binary"
}

# is_error is absent, not false: internal/trace/writer.go tags it omitempty, so a
# predicate that simply returned false never writes the key. The real traces
# pinned under testdata/ carry exactly this shape.
convicting='{"violations":["submitCommitsOneTransactionPerAction"],"witnesses":{"submitCommitsOneTransactionPerAction":{"reason":"predicate false"}}}'
unrelated='{"violations":["newAccountBalanceIsZero"],"witnesses":{"newAccountBalanceIsZero":{"reason":"predicate false"}}}'
threw='{"violations":["submitMovesBalanceByAtMostTypedAmount"],"witnesses":{"submitMovesBalanceByAtMostTypedAmount":{"is_error":true,"reason":"TypeError: cannot read text of undefined"}}}'
# The spec's `route` extractor, which is what the health gate reads. Only the
# extractors that changed at a step are recorded, hence the prev/curr shape.
on_txn='{"extractor_changes":{"route":{"prev":"ledger","curr":"add-transaction"}},"violations":[]}'
off_txn='{"extractor_changes":{"route":{"prev":null,"curr":"home"}},"violations":[]}'
on_ledger='{"extractor_changes":{"route":{"prev":"home","curr":"ledger"}},"violations":[]}'

# --- the flags the calibrated runs were measured with reach the binary --------

run argv-ios ios 2 <<TRACE
$on_txn
$convicting
TRACE
expect_status 0 argv-ios
expect_argv "--exit-on-violation" argv-ios
expect_argv "--platform" argv-ios
expect_argv "ios" argv-ios
expect_argv "--seed" argv-ios
expect_argv "7" argv-ios
expect_argv "240" argv-ios
expect_argv "20m" argv-ios
expect_argv "iPhone 16 Pro" argv-ios

run argv-android android 2 <<TRACE
$on_txn
$convicting
TRACE
expect_status 0 argv-android
expect_argv "--exit-on-violation" argv-android
expect_argv "app.folio" argv-android
expect_argv "examples/folio/app/androidApp/build/outputs/apk/debug/androidApp-debug.apk" argv-android

# --- ios and web: a conviction is required -----------------------------------

run convicted ios 2 <<TRACE
$on_txn
$convicting
TRACE
expect_status 0 convicted
expect_says "found the submit bug in 2 steps (submitCommitsOneTransactionPerAction)" convicted
expect_summary "- convicted on: submitCommitsOneTransactionPerAction" convicted

# Exit 2 with a violation of a real but ungated property is not this leg's bug.
run other-violation ios 2 <<TRACE
$on_txn
$unrelated
TRACE
expect_status 1 other-violation
expect_says "not the double-submit this leg gates on" other-violation
expect_summary "- also violated: newAccountBalanceIsZero" other-violation

# A predicate that threw reaches exit 2 by the identical path, and is not a
# verdict about folio at all.
run threw-ios ios 2 <<TRACE
$on_txn
$threw
TRACE
expect_status 1 threw-ios
expect_says "a predicate threw, so exit 2 is not a verdict about folio" threw-ios
expect_says "TypeError: cannot read text of undefined" threw-ios
expect_silent "found the submit bug" threw-ios
expect_summary "**a predicate threw**" threw-ios

# The bug is still in folio, so a clean run is the fuzzer no longer reaching it.
run clean-ios ios 0 <<TRACE
$on_txn
$on_txn
TRACE
expect_status 1 clean-ios
expect_says "the double-submit bug was NOT found in 2 steps" clean-ios

run harness-ios ios 3 <<TRACE
$on_txn
TRACE
expect_status 3 harness-ios
expect_says "the harness failed with exit 3" harness-ios

# --- android: a health gate, with a conviction as a bonus --------------------

run android-convicted android 2 <<TRACE
$on_txn
$convicting
TRACE
expect_status 0 android-convicted
expect_says "found the submit bug in 2 steps (a bonus, not required)" android-convicted

run android-other android 2 <<TRACE
$on_txn
$unrelated
TRACE
expect_status 0 android-other
expect_says "judging health only" android-other

run android-healthy android 0 <<TRACE
$on_txn
$on_txn
TRACE
expect_status 0 android-healthy
expect_says "healthy run over 2 steps, reached the transaction screen" android-healthy

# Health means it got to the screen the double-submit lives on. Anything less is
# a leg that proved nothing, however green the exit code.
run android-stalled android 0 <<TRACE
$off_txn
$on_ledger
TRACE
expect_status 1 android-stalled
expect_says "never reached the add-transaction route over 2 steps" android-stalled
expect_says "routes the trace does record: home, ledger" android-stalled

# The gate used to grep the hierarchy dump for the "AddTransactionScreen"
# marker, so a trace carrying that marker without ever reporting the route
# passed. It reads the route the spec computed now, and routeOf answers null on
# a frame showing two screens, which is exactly when the marker is present and
# the app is not on that screen.
run android-marker-without-route android 0 <<TRACE
$off_txn
{"hierarchy":{"resourceIds":["AddTransactionScreen"]},"violations":[]}
TRACE
expect_status 1 android-marker-without-route
expect_says "never reached the add-transaction route over 2 steps" android-marker-without-route
expect_says "routes the trace does record: home" android-marker-without-route

# And the converse: the route alone is enough, with no hierarchy in the trace.
run android-route-without-hierarchy android 0 <<TRACE
$off_txn
$on_txn
TRACE
expect_status 0 android-route-without-hierarchy
expect_says "healthy run over 2 steps, reached the transaction screen" android-route-without-hierarchy

# The thrown-predicate check has to bite on android too: this is the leg whose
# gate is loose enough to swallow it.
run threw-android android 2 <<TRACE
$on_txn
$threw
TRACE
expect_status 1 threw-android
expect_says "a predicate threw" threw-android
expect_silent "judging health only" threw-android

run harness-android android 4 <<TRACE
$on_txn
TRACE
expect_status 4 harness-android
expect_says "the harness failed with exit 4" harness-android

# --- traces that are not there, or are there and say nothing -----------------

# Exit 2 and no run directory at all: the glob matches nothing, and an empty
# trace must not be judged as a folio that behaved.
run no-trace ios 2 none </dev/null
expect_status 1 no-trace
expect_says "wrote no trace" no-trace
expect_silent "Traceback" no-trace

run no-trace-android android 0 none </dev/null
expect_status 1 no-trace-android
expect_says "wrote no trace" no-trace-android

# A zero-byte trace: the file is there, so the missing-trace check passes and
# the classification has to survive reading nothing out of it.
run zero-byte ios 0 file </dev/null
expect_status 1 zero-byte
expect_says "NOT found in 0 steps" zero-byte
expect_silent "Traceback" zero-byte

# A line the recorder truncated is skipped, not fatal, and the violation on the
# readable line is still found.
run malformed ios 2 <<TRACE
$on_txn
{"violations":["submitCommitsOne
$convicting
TRACE
expect_status 0 malformed
expect_says "found the submit bug" malformed
expect_silent "Traceback" malformed

run bad-platform windows 0 none </dev/null
expect_status 64 bad-platform
expect_says "unknown platform: windows" bad-platform

# --- the gate names still exist in the spec they gate ------------------------

# Nothing but this check ties GATED_PROPERTIES to the spec. Rename a gated
# property and the classification matches nothing: ios and web report a
# different bug, android reclassifies a conviction as health and stays green.
sed 's/submitCommitsOneTransactionPerAction/submitCommitsOneTxnPerAction/g' \
  "$spec" > "$work/renamed-spec.ts"
spec_override="$work/renamed-spec.ts"
run drift-renamed ios 2 <<TRACE
$on_txn
$convicting
TRACE
expect_status 1 drift-renamed
expect_says "no longer declares submitCommitsOneTransactionPerAction" drift-renamed
if [ -e "$case_dir/argv" ]; then
  fail "drift-renamed: the run started before the gate was checked"
fi

spec_override="$work/absent-spec.ts"
run drift-missing ios 2 none </dev/null
expect_status 1 drift-missing
expect_says "cannot read" drift-missing

echo "export const notProperties = { a };" > "$work/shapeless-spec.ts"
spec_override="$work/shapeless-spec.ts"
run drift-shapeless ios 2 none </dev/null
expect_status 1 drift-shapeless
expect_says "declares no \`export const properties" drift-shapeless

# The health gate reads two more names out of the spec: the `route` extractor
# and the SCREENS key naming the transaction screen. Renaming either would make
# every android run report a route it never failed to reach.
sed 's/extract<Route | null>("route"/extract<Route | null>("screen"/' \
  "$spec" > "$work/no-route-spec.ts"
spec_override="$work/no-route-spec.ts"
run drift-route-extractor android 0 none </dev/null
expect_status 1 drift-route-extractor
expect_says 'no longer declares extract("route"' drift-route-extractor

sed 's/"add-transaction": "AddTransactionScreen"/"add-txn": "AddTransactionScreen"/' \
  "$spec" > "$work/renamed-route-spec.ts"
spec_override="$work/renamed-route-spec.ts"
run drift-route-key android 0 none </dev/null
expect_status 1 drift-route-key
expect_says "waits for a route the app never reports" drift-route-key
expect_says "add-txn" drift-route-key

# And the same check against the spec as it stands: this is the assertion that
# fails at `make test` when someone renames a property without moving the gate.
run gate-matches-spec ios 2 <<TRACE
$on_txn
$convicting
TRACE
expect_status 0 gate-matches-spec
expect_silent "no longer declares" gate-matches-spec

# --- real traces, from actions run 31902501859 on this branch's code ---------

# The cases above are hand-written, so they only prove the classifier agrees
# with what this file assumes a trace looks like. These are what the binary
# actually wrote. Every step is kept; of each step only step, violations,
# witnesses and residuals survive, and hierarchy is replaced by the "...Screen"
# resource ids it held, in order, because that dump is 95% of the bytes and the
# only place the route gate's grep can match. Running the classifier over the
# originals and over these produced byte-identical verdicts.

run ios-real ios 2 file < "$testdata/folio-ios-convicted.jsonl"
expect_status 0 ios-real
expect_says "folio/ios: found the submit bug in 49 steps (submitCommitsOneTransactionPerAction)" ios-real
expect_summary "- 49 steps recorded, exit 2" ios-real
# a real conviction carries no is_error, so reading it as a thrown predicate is
# the mistake this asserts against
expect_silent "a predicate threw" ios-real

# Both gated properties fired on the same step, and both are named.
run web-real web 2 file < "$testdata/folio-web-convicted.jsonl"
expect_status 0 web-real
expect_says "folio/web: found the submit bug in 184 steps (submitCommitsOneTransactionPerAction, submitMovesBalanceByAtMostTypedAmount)" web-real
expect_summary "- convicted on: submitCommitsOneTransactionPerAction, submitMovesBalanceByAtMostTypedAmount" web-real

# 200 steps, no violation, and it reached the transaction screen: the health
# gate's passing case on real data.
run android-real android 0 file < "$testdata/folio-android-healthy.jsonl"
expect_status 0 android-real
expect_says "folio/android: healthy run over 200 steps, reached the transaction screen" android-real
expect_silent "never reached" android-real

# The same real trace cut to before it first reached the transaction screen.
# The route gate reads the accessibility hierarchy, so this is the one case that
# proves it reads real hierarchy dumps and not just the shape assumed above.
head -10 "$testdata/folio-android-healthy.jsonl" > "$work/android-early.jsonl"
run android-real-stalled android 0 file < "$work/android-early.jsonl"
expect_status 1 android-real-stalled
expect_says "never reached the add-transaction route over 10 steps" android-real-stalled
expect_says "routes the trace does record: login, home, add-account, ledger" android-real-stalled

if [ "$failed" = 0 ]; then
  echo "folio-run.sh: ok"
fi
exit "$failed"
