#!/usr/bin/env bash
# Runs examples/folio/sanderling/spec.ts against one platform and checks the
# exit code the job expects. Kept out of the workflow YAML so it can be run by
# hand, which is how it was calibrated:
#
#   SEED=9 MAX_STEPS=200 .github/scripts/folio-run.sh android
#
# web and ios expect a conviction: folio's double-submit bug is still there, and
# a run that no longer finds it is a regression in the fuzzer, not a pass. The
# exit code alone does not say that much, so this script reads the trace: see
# GATED_PROPERTIES below. android is a health gate instead, because it convicts
# in four runs out of five rather than five; see docs/development/ci.md.
set -uo pipefail

platform="${1:?usage: folio-run.sh android|ios|web}"
seed="${SEED:-1}"
max_steps="${MAX_STEPS:-240}"
duration="${DURATION:-20m}"
sanderling="${SANDERLING:-./bin/sanderling}"
output="runs/folio-$platform"
spec="examples/folio/sanderling/spec.ts"
summary="${GITHUB_STEP_SUMMARY:-/dev/null}"

folio_args=(--bundle-id app.folio)
case "$platform" in
  android)
    folio_args+=(--android-app-path
      examples/folio/app/androidApp/build/outputs/apk/debug/androidApp-debug.apk)
    ;;
  ios)
    # Clear state is left at its default, and no --ios-app-path is passed, so
    # the driver wipes the app's data container rather than reinstalling. That
    # is what the calibrated numbers were measured from, and it keeps the run
    # away from the `simctl uninstall` + `install` path that races FrontBoard
    # ("app.folio is unknown to FrontBoard"), which needs an app path to reach.
    folio_args+=(--platform ios
      --ios-device "${IOS_DEVICE:-iPhone 16 Pro}")
    ;;
  web)
    dist="examples/folio/app/webApp/build/dist/wasmJs/developmentExecutable"
    port="${PORT:-8791}"
    # The stock static servers do not set COOP/COEP, and without cross-origin
    # isolation the app's sqlite worker never starts, so folio loads to a blank
    # canvas and every step observes an empty accessibility tree.
    python3 - "$dist" "$port" <<'PY' &
import functools, http.server, sys

class Isolated(http.server.SimpleHTTPRequestHandler):
    def end_headers(self):
        self.send_header("Cross-Origin-Opener-Policy", "same-origin")
        self.send_header("Cross-Origin-Embedder-Policy", "require-corp")
        self.send_header("Cross-Origin-Resource-Policy", "cross-origin")
        super().end_headers()

    def log_message(self, *args):
        pass

directory, port = sys.argv[1], int(sys.argv[2])
handler = functools.partial(Isolated, directory=directory)
http.server.HTTPServer(("127.0.0.1", port), handler).serve_forever()
PY
    server_pid=$!
    trap 'kill "$server_pid" 2>/dev/null' EXIT
    ready=""
    for _ in $(seq 1 30); do
      curl -sf "http://127.0.0.1:$port/index.html" >/dev/null && { ready=1; break; }
      sleep 1
    done
    if [ -z "$ready" ]; then
      echo "folio/web: the app server never served index.html on 127.0.0.1:$port from $dist" >&2
      exit 1
    fi
    folio_args=(--platform web --bundle-id "http://127.0.0.1:$port/index.html")
    ;;
  *)
    echo "unknown platform: $platform" >&2
    exit 64
    ;;
esac

"$sanderling" test \
  --spec "$spec" \
  "${folio_args[@]}" \
  --duration "$duration" \
  --max-steps "$max_steps" \
  --seed "$seed" \
  --exit-on-violation \
  --output "$output"
code=$?

run_dir="$(ls -d "$output"/*/ 2>/dev/null | tail -1)"
# No run directory means no trace. Defaulting the directory to `.` here reads a
# stray ./trace.jsonl and reports it as this run's evidence, which is how a run
# that wrote nothing at all reached "found the submit bug" and exit 0.
trace=""
[ -n "$run_dir" ] && trace="${run_dir}trace.jsonl"
steps=0
[ -f "$trace" ] && steps=$(wc -l < "$trace" | tr -d ' ')

# The two properties that state folio's double-submit. Anything else the spec
# proves false is a different finding, and this leg has nothing to say about it.
GATED_PROPERTIES="submitMovesBalanceByAtMostTypedAmount,submitCommitsOneTransactionPerAction"

# Exit 2 means "the run recorded a violation", and that is NOT the same as "the
# run convicted folio". A predicate that THROWS is recorded as a violation too,
# with is_error set and the thrown text as its reason, and it reaches exit 2 by
# the identical path. So does a violation of newAccountBalanceIsZero, a real but
# unrelated property in the same spec, which one android seed produces. Reading
# the exit code alone leaves the gate green with detection dead.
#
# So sort the trace's violations three ways, by name and by is_error:
#   line 1  convictions: a gated property that was proved false
#   line 2  thrown: a predicate that blew up, with the reason it gave
#   line 3  other: a real violation of some property this leg does not gate on
classified=$(TRACE="$trace" GATED="$GATED_PROPERTIES" python3 - <<'PY'
import json, os

gated = set(os.environ["GATED"].split(","))
convictions, thrown, other = [], [], []
try:
    lines = open(os.environ["TRACE"], encoding="utf-8", errors="replace")
except OSError:
    lines = []
for line in lines:
    try:
        step = json.loads(line)
    except ValueError:
        continue
    witnesses = step.get("witnesses") or {}
    for name in step.get("violations") or []:
        witness = witnesses.get(name) or {}
        if witness.get("is_error"):
            reason = " ".join(str(witness.get("reason") or "").split())
            thrown.append("%s: %s" % (name, reason) if reason else name)
        elif name in gated:
            convictions.append(name)
        else:
            other.append(name)
for names in (convictions, thrown, other):
    print(", ".join(names))
PY
) || {
  echo "folio/$platform: could not classify $trace, so exit $code cannot be read as a verdict" >&2
  exit 1
}
convicted=$(printf '%s\n' "$classified" | sed -n '1p')
thrown=$(printf '%s\n' "$classified" | sed -n '2p')
other=$(printf '%s\n' "$classified" | sed -n '3p')

{
  echo "### folio on $platform"
  echo
  echo "- seed \`$seed\`, budget $max_steps steps / $duration"
  echo "- $steps steps recorded, exit $code"
  [ -n "$convicted" ] && echo "- convicted on: $convicted"
  [ -n "$other" ] && echo "- also violated: $other"
  [ -n "$thrown" ] && echo "- **a predicate threw**: $thrown"
} >> "$summary"

# A thrown predicate fails every leg, android included. It is not evidence about
# folio: the property that threw is violated from that step on whatever the app
# does, and --exit-on-violation ends the run there, so nothing past it was
# checked. Reporting that as a conviction, or as android's bonus, is the hole
# this check exists to close.
if [ -n "$thrown" ]; then
  echo "folio/$platform: a predicate threw, so exit $code is not a verdict about folio: $thrown" >&2
  echo "folio/$platform: fix the spec (examples/folio/sanderling/) and run again" >&2
  exit 1
fi

# Only 0 and 2, the two codes that claim the run completed: any other code is a
# harness failure, which the branches below already report as one. Without this,
# a missing trace is judged as an empty trace and reported as a verdict on folio.
if [ ! -f "$trace" ] && { [ "$code" = 0 ] || [ "$code" = 2 ]; }; then
  echo "folio/$platform: the run exited $code but wrote no trace under $output/, so there is nothing to judge" >&2
  exit 1
fi

if [ "$platform" = "android" ]; then
  # A health gate, not a conviction gate: android convicts in four runs out of
  # five, and a gate that fails the fifth would report a regression it had not
  # found. Reaching the transaction screen is what this leg proves: the app
  # built, installed, launched and drove.
  case "$code" in
    0) ;;
    2)
      if [ -n "$convicted" ]; then
        echo "folio/android: found the submit bug in $steps steps (a bonus, not required)"
      else
        echo "folio/android: violated $other, which is not the double-submit; judging health only"
      fi
      ;;
    *) echo "folio/android: the harness failed with exit $code" >&2; exit "$code" ;;
  esac
  if ! grep -q '"AddTransactionScreen"' "$trace"; then
    # Where it stopped is a much longer question than this gate answers, so
    # name the routes the trace holds and leave the diagnosis to the reader.
    reached=$(grep -oE '"[A-Za-z0-9]+Screen"' "$trace" | tr -d '"' |
      awk '!seen[$0]++' | paste -sd, -) || reached=""
    echo "folio/android: the run never reached AddTransactionScreen over $steps steps" >&2
    echo "folio/android: routes the trace does record: ${reached:-none}" >&2
    exit 1
  fi
  echo "folio/android: healthy run over $steps steps, reached the transaction screen"
  exit 0
fi

case "$code" in
  2)
    if [ -z "$convicted" ]; then
      echo "folio/$platform: exit 2, but the violation was ${other:-nothing this trace records}, not the double-submit this leg gates on" >&2
      exit 1
    fi
    echo "folio/$platform: found the submit bug in $steps steps ($convicted)"
    exit 0
    ;;
  0)
    echo "folio/$platform: the run finished clean; the double-submit bug was NOT found in $steps steps (seed $seed)" >&2
    echo "folio/$platform: the spec ran without throwing, so this is the fuzzer no longer reaching the bug, not a broken spec" >&2
    exit 1
    ;;
  *) echo "folio/$platform: the harness failed with exit $code" >&2; exit "$code" ;;
esac
