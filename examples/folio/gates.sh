#!/usr/bin/env bash
# Scripted conformance gate for the iOS simulator driver. Runs five serial,
# non-overlapping 3-minute fuzz runs against the folio app and scores five
# gates (G1..G5) over the captured traces and output. Exits non-zero if any
# gate fails.
#
# Backends:
#   BACKEND=simulator (default)  drive the booted iOS simulator
#   BACKEND=device               drive an attached physical iPhone via the
#                                native sidecar; select it with
#                                IOS_DEVICE="<name>" (passed as --ios-device)
#
# Usage:
#   ./gates.sh                          run the simulator gates
#   BACKEND=device IOS_DEVICE="iPhone" ./gates.sh
#   ./gates.sh --self-test              run the offline analyzer tests only
#
# Tunables (environment):
#   RUNS=5            number of serial runs
#   DURATION=3m       per-run fuzz duration
#   SEED=0            fuzz seed
#   P95_LIMIT_MS=2500 G5 p95 step-latency ceiling in milliseconds
#   SANDERLING=sanderling  binary to invoke

set -euo pipefail

script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

BACKEND="${BACKEND:-simulator}"
RUNS="${RUNS:-5}"
DURATION="${DURATION:-3m}"
SEED="${SEED:-0}"
P95_LIMIT_MS="${P95_LIMIT_MS:-2500}"
SANDERLING="${SANDERLING:-sanderling}"
IOS_DEVICE="${IOS_DEVICE:-iPhone 17 Pro}"

bundle_id="app.folio"
spec_path="${script_directory}/sanderling/spec.ts"
ios_app="${script_directory}/app/iosApp/build/Build/Products/Debug-iphonesimulator/iosApp.app"

# The companion binary, embedded for simulator runs. Referenced by file name
# only for the orphan-process check; prose elsewhere says "the companion".
companion_process_name="idb_companion"

# Known-benign stderr lines the companion always prints. These are matched as
# fixed substrings and excluded from the G2 ERROR scan. Keep this list tight:
# only lines that are provably harmless and emitted on every healthy run.
benign_stderr_substrings=(
  # The dynamic linker reports the same Objective-C class registered by two
  # loaded images. The companion runs fine; this is cosmetic.
  "objc[*]: Class"
  "is implemented in both"
  "One of the two will be used. Which one is undefined."
  # gRPC and absl emit informational banner lines on startup.
  "WARNING: All log messages before absl::InitializeLog()"
)

# ---- gate analyzers (pure, operate on a single run directory) --------------

# G2 helper: strip benign companion noise, then report sanderling ERROR lines.
# sanderling's progress logger renders error-level records as lines beginning
# "error:" (see internal/testrun/progress.go). We also catch upper-case ERROR
# for safety against future handlers.
error_lines_in() {
  local output_file="$1"
  local filtered
  filtered="$(cat "$output_file")"
  local pattern
  for pattern in "${benign_stderr_substrings[@]}"; do
    filtered="$(printf '%s\n' "$filtered" | grep -vF "$pattern" || true)"
  done
  printf '%s\n' "$filtered" | grep -E '(^error:|ERROR|level=ERROR)' || true
}

# G1: process exit status recorded by the runner loop.
gate_exit_zero() {
  local run_directory="$1"
  [[ "$(cat "${run_directory}/exit_status")" == "0" ]]
}

# G2: no sanderling ERROR lines after filtering benign companion noise.
gate_no_error_lines() {
  local run_directory="$1"
  local found
  found="$(error_lines_in "${run_directory}/output.log")"
  [[ -z "$found" ]]
}

# G3: the first hierarchy snapshot shows the login screen with empty email and
# password fields (clear-state proof). Reads the first trace line carrying a
# hierarchy and asserts LoginEmail/LoginPassword carry no text.
gate_clear_state() {
  local run_directory="$1"
  local trace_file="${run_directory}/trace.jsonl"
  [[ -f "$trace_file" ]] || return 1
  local verdict
  verdict="$(jq -s -r '
    [ .[] | select(.hierarchy != null) ] as $withHierarchy
    | if ($withHierarchy | length) == 0 then "fail:no-hierarchy"
      else ($withHierarchy[0].hierarchy.elements // []) as $elements
      | ($elements | map(select(.resourceId == "LoginEmail"))    | first) as $email
      | ($elements | map(select(.resourceId == "LoginPassword")) | first) as $password
      | if $email == null or $password == null then "fail:no-login-fields"
        elif (($email.attrs.text // $email.text // "") != "") then "fail:email-not-empty"
        elif (($password.attrs.text // $password.text // "") != "") then "fail:password-not-empty"
        else "pass" end
      end
  ' "$trace_file")"
  [[ "$verdict" == "pass" ]]
}

# G4: no doubled text after InputText. For each InputText action targeting a
# field, the field's value in the NEXT hierarchy must not contain the input
# concatenated with itself (catches append-vs-replace and double-paste bugs).
# The action chosen at step N is applied before step N+1 is observed, so the
# effect lands in the following snapshot.
gate_no_doubled_text() {
  local run_directory="$1"
  local trace_file="${run_directory}/trace.jsonl"
  [[ -f "$trace_file" ]] || return 1
  local verdict
  verdict="$(jq -s -r '
    # Map a selector like "testTag:LoginScreen > testTag:LoginEmail" to its
    # target field id: the token after the final ":" of the last segment.
    def target_field(selector):
      (selector | split(">") | last | gsub("^\\s+|\\s+$";"")) as $last
      | ($last | split(":") | last);

    [ .[] | select(.hierarchy != null) ] as $steps
    | reduce range(0; ($steps | length)) as $i ([];
        ($steps[$i]) as $current
        | (if $i + 1 < ($steps | length) then $steps[$i + 1] else null end) as $next
        | if ($current.next_action.kind == "InputText") and ($next != null)
          then
            (target_field($current.next_action.selector // "")) as $field
            | ($current.next_action.text // "") as $typed
            | (($next.hierarchy.elements // [])
               | map(select(.resourceId == $field)) | first) as $element
            | if $element != null and $typed != ""
              then
                (($element.attrs.text // $element.text // "")) as $value
                | if ($value | contains($typed + $typed))
                  then . + [{field: $field, typed: $typed, value: $value}]
                  else . end
              else . end
          else . end)
    | if length == 0 then "pass"
      else "fail:" + (.[0].field) + ":" + (.[0].value) end
  ' "$trace_file")"
  [[ "$verdict" == "pass" ]]
}

# Emit one step-latency sample per consecutive trace-step pair, in
# milliseconds, on stdout. The trace records a wall-clock timestamp per step;
# the latency of a step is the gap to the next step's observation. Used by G5,
# which aggregates samples across all runs before computing the p95. Timestamps
# are RFC3339 with fractional seconds and a numeric offset, so they are parsed
# with python3 rather than jq's UTC-only fromdateiso8601.
emit_step_latencies() {
  local trace_file="$1"
  [[ -f "$trace_file" ]] || return 0
  jq -r 'select(.timestamp != null) | .timestamp' "$trace_file" \
    | python3 -c '
import sys
from datetime import datetime
stamps = [datetime.fromisoformat(line.strip()) for line in sys.stdin if line.strip()]
for earlier, later in zip(stamps, stamps[1:]):
    print(int((later - earlier).total_seconds() * 1000))
'
}

# Compute the p95 (nearest-rank) of the latency samples on stdin, in ms.
p95_of() {
  python3 -c '
import sys, math
samples = sorted(int(float(line)) for line in sys.stdin if line.strip())
if not samples:
    print(0)
    sys.exit(0)
rank = max(1, math.ceil(0.95 * len(samples)))
print(samples[rank - 1])
'
}

# G5 orphan check: report any lingering companion (and, on the device backend,
# the XCTest runner java sidecar) processes. Empty output means clean.
orphan_processes() {
  local found=""
  if pgrep -f "$companion_process_name" >/dev/null 2>&1; then
    found+="companion "
  fi
  if [[ "$BACKEND" == "device" ]]; then
    # The native sidecar that drives the XCTest runner for physical devices.
    if pgrep -f "sanderling.*sidecar.jar" >/dev/null 2>&1; then
      found+="sidecar "
    fi
  fi
  printf '%s' "$found"
}

# ---- run orchestration -----------------------------------------------------

invoke_sanderling() {
  local output_directory="$1"
  local output_log="$2"
  local exit_status_file="$3"

  local target_flags=()
  if [[ "$BACKEND" == "device" ]]; then
    target_flags=(--ios-device "$IOS_DEVICE")
  else
    # Simulator: build + install the current app so each run starts from the
    # current build, matching the test-ios recipe. clear-state reinstall uses
    # --ios-app-path. just ios boots IOS_DEVICE if nothing is booted.
    target_flags=(--ios-device "$IOS_DEVICE" --ios-app-path "$ios_app")
  fi

  local status=0
  "$SANDERLING" test \
    --platform ios \
    --spec "$spec_path" \
    --bundle-id "$bundle_id" \
    "${target_flags[@]}" \
    --duration "$DURATION" \
    --seed "$SEED" \
    --output "$output_directory" \
    >"$output_log" 2>&1 || status=$?
  printf '%s' "$status" >"$exit_status_file"
}

# Locate the run directory sanderling created under output_directory (it nests
# a timestamped subdirectory) and normalise its artifacts up one level.
collect_run_artifacts() {
  local output_directory="$1"
  [[ -f "${output_directory}/trace.jsonl" ]] && return 0
  local produced
  produced="$(find "$output_directory" -mindepth 2 -name 'trace.jsonl' 2>/dev/null | head -1 || true)"
  if [[ -n "$produced" ]]; then
    cp "$produced" "${output_directory}/trace.jsonl" 2>/dev/null || true
  fi
}

run_gates() {
  if [[ "$BACKEND" == "simulator" ]]; then
    echo "preparing folio build for the simulator backend"
    ( cd "$script_directory" && just ios >/dev/null )
  fi

  local timestamp
  timestamp="$(date +%Y%m%d-%H%M%S)"
  local gate_root="${script_directory}/sanderling/gates/${timestamp}"
  mkdir -p "$gate_root"
  echo "gate root: ${gate_root}"
  echo "backend=${BACKEND} runs=${RUNS} duration=${DURATION} seed=${SEED} p95_limit_ms=${P95_LIMIT_MS}"

  local all_latencies="${gate_root}/all-latencies.txt"
  : >"$all_latencies"

  local -a g1 g2 g3 g4 g5
  local run_index
  for ((run_index = 1; run_index <= RUNS; run_index++)); do
    local run_directory="${gate_root}/run-${run_index}"
    mkdir -p "$run_directory"
    echo "run ${run_index}/${RUNS} -> ${run_directory}"

    invoke_sanderling "$run_directory" "${run_directory}/output.log" "${run_directory}/exit_status"
    collect_run_artifacts "$run_directory"

    g1[run_index]=$(gate_exit_zero      "$run_directory" && echo PASS || echo FAIL)
    g2[run_index]=$(gate_no_error_lines "$run_directory" && echo PASS || echo FAIL)
    g3[run_index]=$(gate_clear_state    "$run_directory" && echo PASS || echo FAIL)
    g4[run_index]=$(gate_no_doubled_text "$run_directory" && echo PASS || echo FAIL)

    emit_step_latencies "${run_directory}/trace.jsonl" >>"$all_latencies"

    local orphans
    orphans="$(orphan_processes)"
    if [[ -n "$orphans" ]]; then
      g5[run_index]="FAIL"
      echo "  orphaned processes after run ${run_index}: ${orphans}"
    else
      g5[run_index]="PENDING"
    fi
  done

  local p95
  p95="$(p95_of <"$all_latencies")"
  local final_orphans
  final_orphans="$(orphan_processes)"

  # G5 is global: p95 is computed over every run's samples and an orphan after
  # any single run fails the whole gate. Decide once, then stamp every row.
  local g5_global="PASS"
  if [[ "$p95" -ge "$P95_LIMIT_MS" ]]; then
    g5_global="FAIL"
  fi
  if [[ -n "$final_orphans" ]]; then
    g5_global="FAIL"
    echo "orphaned processes at end: ${final_orphans}"
  fi
  for ((run_index = 1; run_index <= RUNS; run_index++)); do
    if [[ "${g5[run_index]}" == "FAIL" ]]; then
      g5_global="FAIL"
    fi
  done
  for ((run_index = 1; run_index <= RUNS; run_index++)); do
    g5[run_index]="$g5_global"
  done

  echo
  printf 'run  G1    G2    G3    G4    G5\n'
  local verdict="PASS"
  for ((run_index = 1; run_index <= RUNS; run_index++)); do
    printf '%-4s %-5s %-5s %-5s %-5s %-5s\n' \
      "$run_index" "${g1[run_index]}" "${g2[run_index]}" "${g3[run_index]}" "${g4[run_index]}" "${g5[run_index]}"
    for cell in "${g1[run_index]}" "${g2[run_index]}" "${g3[run_index]}" "${g4[run_index]}" "${g5[run_index]}"; do
      [[ "$cell" == "PASS" ]] || verdict="FAIL"
    done
  done
  echo
  echo "p95 step latency: ${p95}ms (limit ${P95_LIMIT_MS}ms)"
  if [[ "$verdict" == "PASS" ]]; then
    echo "GATES PASS"
    return 0
  fi
  echo "GATES FAIL"
  return 1
}

# ---- offline self-test -----------------------------------------------------
# Exercises every analyzer against canned passing/failing run directories under
# gates-testdata/ so the parsing logic can be checked without a device.

self_test() {
  local testdata="${script_directory}/gates-testdata"
  local failures=0

  assert() {
    local label="$1" expected="$2" actual="$3"
    if [[ "$expected" == "$actual" ]]; then
      printf 'ok   %s\n' "$label"
    else
      printf 'FAIL %s (expected %s, got %s)\n' "$label" "$expected" "$actual"
      failures=$((failures + 1))
    fi
  }

  assert "G1 pass run exits zero" PASS \
    "$(gate_exit_zero "${testdata}/pass" && echo PASS || echo FAIL)"
  assert "G1 fail run nonzero exit" FAIL \
    "$(gate_exit_zero "${testdata}/g1-nonzero-exit" && echo PASS || echo FAIL)"

  assert "G2 pass run no errors" PASS \
    "$(gate_no_error_lines "${testdata}/pass" && echo PASS || echo FAIL)"
  assert "G2 benign noise tolerated" PASS \
    "$(gate_no_error_lines "${testdata}/g2-benign-only" && echo PASS || echo FAIL)"
  assert "G2 real error caught" FAIL \
    "$(gate_no_error_lines "${testdata}/g2-real-error" && echo PASS || echo FAIL)"

  assert "G3 pass clear state" PASS \
    "$(gate_clear_state "${testdata}/pass" && echo PASS || echo FAIL)"
  assert "G3 dirty fields caught" FAIL \
    "$(gate_clear_state "${testdata}/g3-dirty-field" && echo PASS || echo FAIL)"

  assert "G4 pass no doubling" PASS \
    "$(gate_no_doubled_text "${testdata}/pass" && echo PASS || echo FAIL)"
  assert "G4 doubled text caught" FAIL \
    "$(gate_no_doubled_text "${testdata}/g4-doubled-text" && echo PASS || echo FAIL)"

  local pass_p95 slow_p95
  pass_p95="$(emit_step_latencies "${testdata}/pass/trace.jsonl" | p95_of)"
  slow_p95="$(emit_step_latencies "${testdata}/g5-slow-p95/trace.jsonl" | p95_of)"
  assert "G5 pass p95 under limit" PASS \
    "$([[ "$pass_p95" -lt "$P95_LIMIT_MS" ]] && echo PASS || echo FAIL)"
  assert "G5 slow p95 over limit" FAIL \
    "$([[ "$slow_p95" -lt "$P95_LIMIT_MS" ]] && echo PASS || echo FAIL)"

  echo
  if [[ "$failures" -eq 0 ]]; then
    echo "SELF-TEST PASS"
    return 0
  fi
  echo "SELF-TEST FAIL (${failures} failed)"
  return 1
}

main() {
  case "${1:-}" in
    --self-test) self_test ;;
    "") run_gates ;;
    *) echo "usage: $0 [--self-test]" >&2; exit 2 ;;
  esac
}

main "$@"
