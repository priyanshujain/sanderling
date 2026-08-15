#!/usr/bin/env bash
# Reads the replay-ui dogfood trace and reports, per property, how many steps
# that property actually judged. Kept out of the workflow YAML so it can be run
# by hand against a local run:
#
#   GITHUB_STEP_SUMMARY=/dev/stdout .github/scripts/replay-ui-summary.sh runs/dogfood
#
# `sanderling test` exiting 0 says only that no property returned false. Every
# property in replay-ui/sanderling/spec.ts declines to judge when a reading it
# needs is absent, which is right individually and useless in aggregate: a run
# that never rendered the step page returns false nowhere and exits 0, so
# checked-and-clean and checked-nothing arrive at the same green tick. Section 8
# of docs/development/design-principles.md is the rule this leg was breaking.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
output="${1:-runs/dogfood}"
spec="${SPEC:-$root/replay-ui/sanderling/spec.ts}"
summary="${GITHUB_STEP_SUMMARY:-/dev/null}"

shopt -s nullglob
run_dirs=("$output"/*/)
shopt -u nullglob

{
  echo "### replay-ui dogfood"
  echo
  echo "- seed \`${SEED:-unset}\`, budget ${MAX_STEPS:-unset} steps"
} >> "$summary"

if [ ${#run_dirs[@]} -eq 0 ]; then
  echo "- **nothing under \`$output/\`**: the run never started, so no property was ever evaluated" >> "$summary"
  echo "replay-ui: no run directory under $output/, so there is nothing to judge" >&2
  exit 1
fi

trace="${run_dirs[${#run_dirs[@]} - 1]}trace.jsonl"
if [ ! -f "$trace" ]; then
  echo "- **no trace at \`$trace\`**: the run wrote no steps, so no property was ever evaluated" >> "$summary"
  echo "replay-ui: $trace does not exist, so there is nothing to judge" >&2
  exit 1
fi

TRACE="$trace" SPEC="$spec" python3 - >> "$summary" <<'PY'
import json
import os
import re
import sys

trace_path = os.environ["TRACE"]
spec_path = os.environ["SPEC"]


def toolbar(values):
    reading = values.get("toolbar")
    return reading if isinstance(reading, dict) else None


def numbered(reading, key):
    return reading is not None and reading.get(key) is not None


def listing(values, name):
    reading = values.get(name)
    return reading if isinstance(reading, list) else []


# One entry per property in replay-ui/sanderling/spec.ts, holding the guard that
# property opens with. The trace records extractor values, not verdicts, so
# counting the steps a property really judged means restating its guard here,
# and that restatement is the risk this file carries: a guard that drifts from
# its property would report evidence that does not exist. The two checks at the
# bottom make the two drifts that CAN be caught loud rather than silent.
#
# noUncaughtExceptions carries no guard on purpose: it compares a count, not an
# element, so it judges every step the verifier accepted.
GUARDS = {
    "noUncaughtExceptions": [],
    "selectedStepIsInRange": [
        ("a toolbar reporting a step and a step count",
         lambda c, p: numbered(toolbar(c), "step") and numbered(toolbar(c), "stepCount")),
    ],
    "exactlyOneStepIsSelected": [
        ("a row in the step list", lambda c, p: len(listing(c, "stepRows")) > 0),
    ],
    "stepCountMatchesTheList": [
        ("a toolbar reporting a step count", lambda c, p: numbered(toolbar(c), "stepCount")),
        ("a row in the step list", lambda c, p: len(listing(c, "stepRows")) > 0),
    ],
    "screenshotShowsTheSelectedStep": [
        ("a toolbar reporting a step", lambda c, p: numbered(toolbar(c), "step")),
        ("a screenshot in the before panel",
         lambda c, p: c.get("beforeScreenshotStep") is not None),
    ],
    "switchingTabsKeepsTheStep": [
        ("a tab selection that changed from the step before",
         lambda c, p: p is not None and p.get("activeTabs") != c.get("activeTabs")),
        ("a toolbar on both steps",
         lambda c, p: p is not None and toolbar(p) is not None and toolbar(c) is not None),
    ],
    "badgeCountMatchesThePanel": [
        ("a violations badge on screen",
         lambda c, p: len(listing(c, "violationBadges")) > 0
         and listing(c, "violationBadges")[0] is not None),
        ("a violations panel on screen",
         lambda c, p: len(listing(c, "violationPanelCounts")) > 0),
    ],
}

# A zero here is the run failing to render, not the fuzzer getting unlucky:
# every one of these needs only that the step page came up, which it does on the
# first step of any working run. The other three are left to be reported. Two of
# them are trajectory-dependent - switchingTabsKeepsTheStep needs a tab switch
# between consecutive steps, badgeCountMatchesThePanel needs the fuzzer to land
# on a violating step AND open the violations tab there - and a gate that
# convicts on an unlucky seed reports a regression it has not found.
MUST_RENDER = (
    "selectedStepIsInRange",
    "exactlyOneStepIsSelected",
    "stepCountMatchesTheList",
    "screenshotShowsTheSelectedStep",
)

steps = 0
verified = 0
violating = 0
malformed = 0
property_names = set()
judged = {name: 0 for name in GUARDS}
met_once = {name: [0] * len(conditions) for name, conditions in GUARDS.items()}

# The trace records only the extractors that changed at a step, so an
# extractor's value at any step is the last change recorded for it, starting
# from null. Steps the verifier skipped advance nothing and are not evaluations.
values = {}
previous = None
with open(trace_path, encoding="utf-8", errors="replace") as lines:
    for line in lines:
        if not line.strip():
            continue
        try:
            step = json.loads(line)
        except ValueError:
            malformed += 1
            continue
        steps += 1
        property_names |= set((step.get("residuals") or {}).keys())
        if step.get("violations"):
            violating += 1
        if step.get("skipped_verification") or step.get("transitional"):
            continue
        for name, change in (step.get("extractor_changes") or {}).items():
            values[name] = change.get("curr")
        verified += 1
        for name, conditions in GUARDS.items():
            met = [condition(values, previous) for _, condition in conditions]
            if all(met):
                judged[name] += 1
            for index, held in enumerate(met):
                met_once[name][index] += 1 if held else 0
        previous = dict(values)

report = []
report.append("- %d steps recorded, %d verified, %d with violations"
              % (steps, verified, violating))
if malformed:
    report.append("- **%d unreadable line(s)** in %s" % (malformed, trace_path))
report.append("")
report.append("- judged: the property compared real values. declined: a reading it "
              "needs was absent, so it returned true without checking anything.")
report.append("")
report.append("| property | judged | declined |")
report.append("| --- | --- | --- |")
for name in GUARDS:
    count = judged[name]
    report.append("| %s | %s | %d |"
                  % (name, count if count else "**0**", verified - count))

report.append("")
for name, conditions in GUARDS.items():
    if judged[name] or not verified:
        continue
    absent = [label for index, (label, _) in enumerate(conditions) if not met_once[name][index]]
    report.append("- `%s` judged nothing on this run: no step had %s"
                  % (name, ", nor ".join(absent) if absent else "what its guard needs"))

blind = None
if not verified:
    blind = "%s records %d step(s) and not one of them was verified" % (trace_path, steps)
else:
    silent = [name for name in MUST_RENDER if not judged[name]]
    if silent:
        blind = ("%s declined on every step, so the step page never rendered and the "
                 "exit code is not evidence about the replay UI" % ", ".join(silent))

drift = []
if property_names and property_names != set(GUARDS):
    drift.append("the spec's properties are %s but this summary counts %s"
                 % (", ".join(sorted(property_names)), ", ".join(sorted(GUARDS))))
try:
    with open(spec_path, encoding="utf-8") as handle:
        declared = set(re.findall(r'extract\(\s*"([^"]+)"', handle.read()))
except OSError as error:
    drift.append("could not read %s to check its readings still exist: %s" % (spec_path, error))
    declared = None
if declared is not None:
    gone = sorted({"toolbar", "stepRows", "beforeScreenshotStep", "activeTabs",
                   "violationBadges", "violationPanelCounts"} - declared)
    if gone:
        drift.append("%s no longer declares %s, so the counts above describe readings "
                     "that do not exist" % (spec_path, ", ".join(gone)))

if blind:
    report.append("- **this run checked nothing**: %s" % blind)
for reason in drift:
    report.append("- **these counts cannot be trusted**: %s" % reason)

print("\n".join(report))
for reason in ([blind] if blind else []) + drift:
    print("replay-ui: %s" % reason, file=sys.stderr)
sys.exit(1 if blind or drift else 0)
PY
