#!/usr/bin/env bash
# Scripted conformance gate for the mobile drivers. Runs five serial,
# non-overlapping 3-minute fuzz runs against the folio example app
# (examples/folio) and scores five gates (G1..G5) over the captured traces and
# output. Exits non-zero if any gate fails.
#
# Backends:
#   BACKEND=simulator (default)  drive the booted iOS simulator
#   BACKEND=device               drive an attached physical iPhone via the
#                                driver's runner-only device path; select it
#                                with IOS_DEVICE="<name>" (passed as --ios-device)
#   BACKEND=android              drive an Android device/emulator over the JVM
#                                sidecar; select a specific device with
#                                ANDROID_DEVICE="<adb serial>" (passed as --device)
#
# Usage:
#   ./gates.sh                          run the simulator gates
#   BACKEND=device IOS_DEVICE="iPhone" ./gates.sh
#   BACKEND=android ANDROID_DEVICE="663c91b1" ./gates.sh
#   RUNS=1 SEEDS=303 ./gates.sh         re-run just the seed that failed
#   ./gates.sh --self-test              run the offline analyzer tests only
#
# Tunables (environment):
#   RUNS=5            number of serial runs
#   DURATION=3m       per-run fuzz duration
#   SEEDS="101 202 303 404 505"
#                     one fuzz seed per run, whitespace separated, consumed in
#                     order. The values differ so the runs explore different
#                     action streams, and they are fixed so a failing gate can
#                     be re-run exactly; the results table prints the seed each
#                     run used. A 0 is rejected: sanderling test reads --seed 0
#                     as "derive the seed from the clock", which is the one
#                     thing a gate cannot reproduce.
#   P95_LIMIT_MS      G5 p95 step-latency ceiling in ms (default 2500 for iOS,
#                     5500 for the android backend's higher and more variable
#                     per-step USB cost, especially cold right after a reboot)
#   SANDERLING=sanderling  binary to invoke

set -euo pipefail

script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
folio_directory="$(cd "${script_directory}/../examples/folio" && pwd)"

BACKEND="${BACKEND:-simulator}"
RUNS="${RUNS:-5}"
DURATION="${DURATION:-3m}"
SEEDS="${SEEDS:-101 202 303 404 505}"
# The p95 ceiling is backend-specific: a physical Android device drives every
# step over USB (snapshot + settle + adb round-trips), so its per-step floor is
# several times the iOS simulator's in-process cost. 2500ms was calibrated on
# the simulator; holding a physical device to it would force ripping out the
# settle/retry logic the correctness gates depend on. Override with P95_LIMIT_MS.
if [[ "$BACKEND" == "android" ]]; then
  P95_LIMIT_MS="${P95_LIMIT_MS:-5500}"
else
  P95_LIMIT_MS="${P95_LIMIT_MS:-2500}"
fi
SANDERLING="${SANDERLING:-sanderling}"
IOS_DEVICE="${IOS_DEVICE:-iPhone 17 Pro}"
ANDROID_DEVICE="${ANDROID_DEVICE:-}"

if [[ -n "${SEED:-}" ]]; then
  echo "gates.sh: SEED is not read any more; set SEEDS to one seed per run" >&2
  exit 2
fi

bundle_id="app.folio"
spec_path="${folio_directory}/sanderling/spec.ts"
android_apk="${folio_directory}/app/androidApp/build/outputs/apk/debug/androidApp-debug.apk"
# The built app bundle differs by SDK: the simulator build lands under
# Debug-iphonesimulator, the device build under Debug-iphoneos.
if [[ "$BACKEND" == "device" ]]; then
  ios_app="${folio_directory}/app/iosApp/build/Build/Products/Debug-iphoneos/iosApp.app"
else
  ios_app="${folio_directory}/app/iosApp/build/Build/Products/Debug-iphonesimulator/iosApp.app"
fi

# Run adb against the selected Android device, or the only one if unset.
adb_target() {
  if [[ -n "$ANDROID_DEVICE" ]]; then adb -s "$ANDROID_DEVICE" "$@"; else adb "$@"; fi
}

# The companion binary, embedded for simulator runs. Referenced by file name
# only for the orphan-process check; prose elsewhere says "the companion".
companion_process_name="idb_companion"

# Known-benign stderr lines the companion always prints. These are matched as
# fixed substrings and excluded from the G2 ERROR scan. Keep this list tight:
# only lines that are provably harmless and emitted on every healthy run.
benign_stderr_substrings=(
  # The dynamic linker reports the same Objective-C class registered by two
  # loaded images. The companion runs fine; this is cosmetic. The real line
  # reads "objc[<pid>]: Class ...", so the fixed substring is "]: Class".
  "]: Class"
  "is implemented in both"
  "One of the two will be used. Which one is undefined."
  # gRPC and absl emit informational banner lines on startup.
  "WARNING: All log messages before absl::InitializeLog()"
)

# ---- gate analyzers (pure, operate on a single run directory) --------------

# G2 helper: strip benign companion noise, then report sanderling ERROR lines.
# sanderling's progress logger renders error-level records as lines beginning
# "error:" (see internal/testrun/progress.go). We also catch the upper-case
# ERROR token (word-bounded, so ERRORS/ERRORLESS and path fragments do not
# trip the gate) for safety against future handlers.
error_lines_in() {
  local output_file="$1"
  local filtered
  filtered="$(cat "$output_file")"
  local pattern
  for pattern in "${benign_stderr_substrings[@]}"; do
    filtered="$(printf '%s\n' "$filtered" | grep -vF "$pattern" || true)"
  done
  printf '%s\n' "$filtered" | grep -E '(^error:|\bERROR\b)' || true
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

# G5 orphan check: report any lingering companion, runner session (the hybrid
# simulator driver hosts an in-simulator runner), and, on the device backend,
# the device runner session. The usbmux tunnel is an in-process forwarder that
# dies with sanderling, so it leaves no process to check. Empty output is clean.
orphan_processes() {
  local found=""
  if [[ "$BACKEND" == "android" ]]; then
    # sanderling SIGTERMs the JVM sidecar on shutdown; a survivor is an orphan.
    if pgrep -f "sanderling-sidecar.*\.jar" >/dev/null 2>&1; then found+="sidecar "; fi
    printf '%s' "$found"
    return
  fi
  if pgrep -f "$companion_process_name" >/dev/null 2>&1; then
    found+="companion "
  fi
  if pgrep -f "sanderling-runner.*xctestrun" >/dev/null 2>&1; then
    found+="runner-session "
  fi
  if pgrep -f "CompanionRunnerUITests-Runner" >/dev/null 2>&1; then
    found+="runner-app "
  fi
  if [[ "$BACKEND" == "device" ]]; then
    # The device test session that hosts the runner. Its destination carries
    # platform=iOS,id=<udid>.
    if pgrep -f "xctestrun.*platform=iOS,id=" >/dev/null 2>&1; then
      found+="device-session "
    fi
  fi
  printf '%s' "$found"
}

# ---- run orchestration -----------------------------------------------------

# Split a seed list into the global seed_list, one entry per requested run.
# sanderling test reads --seed 0 as "derive the seed from the clock", so a 0
# here would make that run unreproducible and is refused.
seed_list=()
select_seeds() {
  local requested="$1"
  local -a candidates=()
  read -r -a candidates <<<"$2"
  if [[ "${#candidates[@]}" -lt "$requested" ]]; then
    echo "gates.sh: SEEDS has ${#candidates[@]} value(s) but RUNS=${requested}; give one seed per run" >&2
    return 1
  fi
  local index seed
  for ((index = 0; index < ${#candidates[@]}; index++)); do
    seed="${candidates[index]}"
    case "$seed" in
      '' | *[!0-9]*)
        echo "gates.sh: SEEDS takes whitespace-separated positive integers; got \"${seed}\"" >&2
        return 1
        ;;
    esac
    if [[ "$((10#$seed))" -eq 0 ]]; then
      echo "gates.sh: SEEDS may not contain 0; sanderling test derives a clock seed from 0, so a gate configured with it cannot be reproduced" >&2
      return 1
    fi
  done
  seed_list=("${candidates[@]:0:requested}")
}

invoke_sanderling() {
  local output_directory="$1"
  local output_log="$2"
  local exit_status_file="$3"
  local seed="$4"

  local platform target_flags=()
  if [[ "$BACKEND" == "android" ]]; then
    # pm clear is blocked on some OEM ROMs, so a fresh install (which wipes
    # /data/data) provides the clean clear-state start; --clear-data=false
    # then skips the sidecar's pm clear. Mirrors the iOS per-run reinstall.
    platform=android
    adb_target uninstall "$bundle_id" >/dev/null 2>&1 || true
    # A transient install hiccup must score this run as a failure, not abort the
    # whole harness under `set -e` and discard the other runs' data.
    if ! adb_target install "$android_apk" >"$output_log" 2>&1; then
      echo "adb install failed for ${android_apk}; recording run as a failure" >>"$output_log"
      printf '1' >"$exit_status_file"
      return
    fi
    target_flags=(--clear-data=false)
    [[ -n "$ANDROID_DEVICE" ]] && target_flags+=(--device "$ANDROID_DEVICE")
  else
    # The iOS backends pass --ios-app-path so each run reinstalls the current
    # build for a clean start (device via devicectl, simulator via simctl).
    platform=ios
    target_flags=(--ios-device "$IOS_DEVICE" --ios-app-path "$ios_app")
  fi

  local status=0
  "$SANDERLING" test \
    --platform "$platform" \
    --spec "$spec_path" \
    --bundle-id "$bundle_id" \
    "${target_flags[@]}" \
    --duration "$DURATION" \
    --seed "$seed" \
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
  select_seeds "$RUNS" "$SEEDS" || exit 2

  if [[ "$BACKEND" == "android" ]]; then
    # Build only; invoke_sanderling reinstalls per run via adb (gradle's ddmlib
    # install is flaky on some physical devices).
    echo "preparing folio android build"
    ( cd "$folio_directory" && just build >/dev/null )
    # A physical device, unlike an emulator, lets system UI steal the foreground
    # from the app the fuzzer is exploring. Keep the screen on so it never
    # re-locks, silence the autofill save-password prompt that pops over the
    # login form, and stop Play Protect from intercepting the per-run reinstall.
    # The device must already be unlocked (a secure lock cannot be opened here).
    adb_target shell svc power stayon true >/dev/null 2>&1 || true
    adb_target shell settings put secure autofill_service null >/dev/null 2>&1 || true
    adb_target shell settings put global verifier_verify_adb_installs 0 >/dev/null 2>&1 || true
  elif [[ "$BACKEND" == "simulator" ]]; then
    echo "preparing folio build for the simulator backend"
    ( cd "$folio_directory" && just ios >/dev/null )
  else
    echo "preparing folio device build"
    ( cd "$folio_directory" && just ios-device >/dev/null )
  fi

  local timestamp
  timestamp="$(date +%Y%m%d-%H%M%S)"
  local gate_root="${script_directory}/runs/${timestamp}"
  mkdir -p "$gate_root"
  echo "gate root: ${gate_root}"
  echo "backend=${BACKEND} runs=${RUNS} duration=${DURATION} seeds=${seed_list[*]} p95_limit_ms=${P95_LIMIT_MS}"

  local all_latencies="${gate_root}/all-latencies.txt"
  : >"$all_latencies"

  local -a g1 g2 g3 g4 g5
  local run_index
  for ((run_index = 1; run_index <= RUNS; run_index++)); do
    local run_directory="${gate_root}/run-${run_index}"
    local seed="${seed_list[run_index - 1]}"
    mkdir -p "$run_directory"
    printf '%s' "$seed" >"${run_directory}/seed"
    echo "run ${run_index}/${RUNS} seed=${seed} -> ${run_directory}"

    invoke_sanderling "$run_directory" "${run_directory}/output.log" "${run_directory}/exit_status" "$seed"
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
  printf 'run  seed   G1    G2    G3    G4    G5\n'
  local verdict="PASS"
  for ((run_index = 1; run_index <= RUNS; run_index++)); do
    printf '%-4s %-6s %-5s %-5s %-5s %-5s %-5s\n' \
      "$run_index" "${seed_list[run_index - 1]}" \
      "${g1[run_index]}" "${g2[run_index]}" "${g3[run_index]}" "${g4[run_index]}" "${g5[run_index]}"
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
  echo "reproduce one run with: RUNS=1 SEEDS=<seed from the table> BACKEND=${BACKEND} $0"
  return 1
}

# ---- offline self-test -----------------------------------------------------
# Exercises every analyzer against canned passing/failing run directories under
# testdata/ so the parsing logic can be checked without a device.

self_test() {
  local testdata="${script_directory}/testdata"
  local failures=0
  # The self-test fixtures (g5-slow-p95 = 4000ms) were calibrated against the
  # 2500ms ceiling, so pin it here. Without this the backend-dependent default
  # (5500ms under BACKEND=android) would rate the slow fixture as a PASS and the
  # offline, device-free analyzer check would fail purely from an env var.
  local P95_LIMIT_MS=2500

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

  assert "seeds default to one distinct value per run" "101 202 303 404 505" \
    "$(select_seeds 5 "101 202 303 404 505" >/dev/null 2>&1 && echo "${seed_list[*]}")"
  assert "seeds override honoured, extras ignored" "11 22" \
    "$(select_seeds 2 "11 22 33" >/dev/null 2>&1 && echo "${seed_list[*]}")"
  assert "seed 0 rejected" FAIL \
    "$(select_seeds 1 "0" >/dev/null 2>&1 && echo PASS || echo FAIL)"
  assert "padded seed 00 rejected" FAIL \
    "$(select_seeds 1 "00" >/dev/null 2>&1 && echo PASS || echo FAIL)"
  assert "non-numeric seed rejected" FAIL \
    "$(select_seeds 1 "abc" >/dev/null 2>&1 && echo PASS || echo FAIL)"
  assert "seed list shorter than RUNS rejected" FAIL \
    "$(select_seeds 5 "101 202" >/dev/null 2>&1 && echo PASS || echo FAIL)"

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
