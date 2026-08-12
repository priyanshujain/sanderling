#!/usr/bin/env bash
# Runs examples/folio/sanderling/spec.ts against one platform and checks the
# exit code the job expects. Kept out of the workflow YAML so it can be run by
# hand, which is how it was calibrated:
#
#   SEED=3 MAX_STEPS=240 .github/scripts/folio-run.sh android
#
# android and ios expect exit 2: folio's double-submit bug is still there, and a
# run that no longer finds it is a regression in the fuzzer, not a pass. web
# expects a clean run that reached the transaction screen; it cannot observe the
# submit bug (see docs/development/ci.md).
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
    # --clear-data=false because the caller has just installed a fresh build (a
    # freshly installed app IS clear state). The in-run reinstall path is worth
    # avoiding here: `simctl uninstall` + `install` immediately followed by the
    # XCTest runner's own launch hits "app.folio is unknown to FrontBoard"
    # perhaps half the time, and the run then hangs rather than failing.
    folio_args+=(--platform ios
      --clear-data=false
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
    for _ in $(seq 1 30); do
      curl -sf "http://127.0.0.1:$port/index.html" >/dev/null && break
      sleep 1
    done
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
steps=0
[ -n "$run_dir" ] && steps=$(wc -l < "$run_dir/trace.jsonl" | tr -d ' ')
violated=$(grep -ho '"violations":\[[^]]*\]' "$run_dir/trace.jsonl" 2>/dev/null | head -1)

{
  echo "### folio on $platform"
  echo
  echo "- seed \`$seed\`, budget $max_steps steps / $duration"
  echo "- $steps steps recorded, exit $code"
  [ -n "$violated" ] && echo "- $violated"
} >> "$summary"

if [ "$platform" = "web" ]; then
  # The web leg is a health gate: the same spec has to log in and drive the app
  # as far as the transaction screen. Reaching it is the evidence; the submit
  # property cannot fire here.
  if [ "$code" -ne 0 ]; then
    echo "folio/web: expected a clean run, got exit $code" >&2
    exit 1
  fi
  if ! grep -q '"AddTransactionScreen"' "$run_dir/trace.jsonl"; then
    echo "folio/web: the run never reached AddTransactionScreen, so it never got past login" >&2
    exit 1
  fi
  echo "folio/web: clean run over $steps steps, reached the transaction screen"
  exit 0
fi

case "$code" in
  2) echo "folio/$platform: found the submit bug in $steps steps"; exit 0 ;;
  0) echo "folio/$platform: the run finished clean; the double-submit bug was NOT found in $steps steps (seed $seed)" >&2; exit 1 ;;
  *) echo "folio/$platform: the harness failed with exit $code" >&2; exit "$code" ;;
esac
