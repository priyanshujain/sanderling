#!/usr/bin/env bash
# Drives replay-ui-summary.sh over the traces in testdata/ and checks the
# summary it renders. Run under the flags GitHub Actions uses for a `run:`
# block, because that is where a swallowed failure hides.
#
# testdata/replay-ui-real-run.jsonl is the first 10 steps of the dogfood run in
# actions run 31873049857 on master, with the per-step `hierarchy` dumps and the
# rowElements/tabElements extractors removed so the file stays readable. Nothing
# else was touched. That run was green, and badgeCountMatchesThePanel judged
# nothing in all 80 of its steps. The other two traces are written by hand.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/replay-ui-summary.sh"
testdata="$here/testdata"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

failed=0
rendered=""
status=0

summarise() { # <case name> <output dir>
  rendered="$work/$1.md"
  : > "$rendered"
  status=0
  GITHUB_STEP_SUMMARY="$rendered" SEED=3 MAX_STEPS=80 \
    bash -eo pipefail "$script" "$2" >/dev/null 2>"$work/$1.err" || status=$?
}

fail() {
  echo "FAIL: $*" >&2
  failed=1
}

expect_status() { # <want> <case>
  [ "$status" = "$1" ] || fail "$2: exit $status, want $1"
}

expect_line() { # <line> <case>
  grep -qxF -- "$1" "$rendered" || fail "$2: summary has no line '$1'"
}

expect_absent() { # <pattern> <case>
  if grep -q -- "$1" "$rendered"; then fail "$2: summary should not mention '$1'"; fi
}

plant() { # <case name> <fixture> -> echoes the output dir
  local dir="$work/$1/runs/20260815-075347"
  mkdir -p "$dir"
  cp "$testdata/$2" "$dir/trace.jsonl"
  echo "$work/$1/runs"
}

# A green run of the real thing. Every count here was measured, not chosen.
summarise real "$(plant real replay-ui-real-run.jsonl)"
expect_status 0 real
expect_line "- 10 steps recorded, 10 verified, 0 with violations" real
expect_line "| selectedStepIsInRange | 10 | 0 |" real
expect_line "| exactlyOneStepIsSelected | 10 | 0 |" real
expect_line "| stepCountMatchesTheList | 10 | 0 |" real
expect_line "| screenshotShowsTheSelectedStep | 10 | 0 |" real
expect_line "| switchingTabsKeepsTheStep | 2 | 8 |" real
expect_line "| badgeCountMatchesThePanel | **0** | 10 |" real
expect_line "- \`badgeCountMatchesThePanel\` judged nothing on this run: no step had a violations badge on screen" real
expect_absent "checked nothing" real

# Every property judged at least once, including the one the real run never
# reached. Without this the counts above are consistent with a guard that can
# only ever return zero.
summarise every "$(plant every replay-ui-every-property.jsonl)"
expect_status 0 every
expect_line "- 4 steps recorded, 3 verified, 0 with violations" every
expect_line "| noUncaughtExceptions | 3 | 0 |" every
expect_line "| screenshotShowsTheSelectedStep | 3 | 0 |" every
expect_line "| switchingTabsKeepsTheStep | 2 | 1 |" every
expect_line "| badgeCountMatchesThePanel | 1 | 2 |" every

# The fuzzer sat on the run list: the step page never rendered, so nothing was
# ever compared. This is the run that used to pass.
summarise blind "$(plant blind replay-ui-nothing-rendered.jsonl)"
expect_status 1 blind
expect_line "| selectedStepIsInRange | **0** | 4 |" blind
expect_line "| badgeCountMatchesThePanel | **0** | 4 |" blind
expect_line "- \`exactlyOneStepIsSelected\` judged nothing on this run: no step had a row in the step list" blind
grep -q "this run checked nothing" "$rendered" || fail "blind: no checked-nothing verdict"
grep -q "the step page never rendered" "$work/blind.err" || fail "blind: nothing on stderr"

# The run directory exists but the trace does not, and the glob matches nothing
# at all. Both are the harness dying before it checked anything.
mkdir -p "$work/empty-run/runs/20260815-075347"
summarise empty-run "$work/empty-run/runs"
expect_status 1 empty-run
grep -q "no trace at" "$rendered" || fail "empty-run: no missing-trace line"

summarise no-glob "$work/no-glob/runs"
expect_status 1 no-glob
grep -q "nothing under" "$rendered" || fail "no-glob: no missing-directory line"

# A property this summary does not know about is a summary that silently counts
# six of seven properties, which is the bug one level up.
drifted="$(plant drift replay-ui-every-property.jsonl)"
sed 's/badgeCountMatchesThePanel/badgeAgreesWithThePanel/g' \
  "$testdata/replay-ui-every-property.jsonl" > "$drifted/20260815-075347/trace.jsonl"
summarise drift "$drifted"
expect_status 1 drift
grep -q "these counts cannot be trusted" "$rendered" || fail "drift: no untrusted verdict"

# Same for a reading the spec no longer declares: the count would quietly go to
# zero and read as a UI that stopped rendering.
sed 's/extract("violationBadges"/extract("violationCounters"/' \
  "$here/../../replay-ui/sanderling/spec.ts" > "$work/renamed-spec.ts"
rendered="$work/renamed.md"
: > "$rendered"
status=0
GITHUB_STEP_SUMMARY="$rendered" SPEC="$work/renamed-spec.ts" \
  bash -eo pipefail "$script" "$(plant renamed replay-ui-real-run.jsonl)" \
  >/dev/null 2>&1 || status=$?
expect_status 1 renamed
grep -q "no longer declares violationBadges" "$rendered" || fail "renamed: no drift verdict"

if [ "$failed" = 0 ]; then
  echo "replay-ui-summary.sh: ok"
fi
exit "$failed"
