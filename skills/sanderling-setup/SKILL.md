---
name: sanderling-setup
description: Get sanderling running against an app that is not folio. Use before writing a spec for a new app, when deciding what test hooks the app needs, and when a run will not start or starts and sees nothing.
---

# Getting sanderling onto your app

The goal of setup is not a run that finishes. It is a run whose output you can
believe. Two things decide that, and both are usually treated as chores: the
handles the app exposes, and the state the app starts in. Everything else here
is plumbing.

Every flag below is one the binary accepts. `sanderling test -h` is the
authority, not this file and not the manual: the manual currently documents
`--launcher-activity`, which the binary answers with `flag provided but not
defined`. Check before you use a flag you have not seen work.

## 1. Install, then check the host

The CLI installs from the release script, and the spec package from npm:

```sh
curl -fsSL https://raw.githubusercontent.com/priyanshujain/sanderling/master/install.sh | bash
npm install --save-dev @sanderling/spec
```

Both come from the same release tag and the CLI bundles the package's TypeScript
when it evaluates your spec, so they move together.

`sanderling doctor` reports the host's readiness per platform and exits non-zero
if anything is missing. On a Mac with no Android SDK it says:

```
OK    adb on PATH
FAIL  emulator on PATH or under ANDROID_HOME: not on PATH and ANDROID_HOME is unset
OK    java 17+ on PATH
FAIL  sidecar JAR is real (not placeholder): placeholder JAR embedded; run `make sidecar && make sanderling` to embed the real fat JAR
error: 2 check(s) failed
```

Scope it with `--platform web|android|ios|ios-device|all` (default `all`). Web
needs a Chromium that launches headless. Android needs `adb`, an emulator on
PATH or under `ANDROID_HOME`, Java 17 or newer, and the embedded sidecar JAR.
iOS needs `xcrun` and `simctl`; `ios-device` adds `devicectl`, the macOS usbmuxd
socket, a connected paired device, and App Store Connect signing credentials.

Read the doctor's Android result as advisory rather than final: its emulator
check today looks only at PATH, `ANDROID_HOME` and `ANDROID_SDK_ROOT`, while a
run also searches `~/Library/Android/sdk`, `~/Android/Sdk` and the Homebrew
command-line-tools paths. The run's own error names every location it tried, so
that is the one to trust. In the other direction, a missing SDK can surface
during a run as `sidecar health check: context deadline exceeded` about thirty
seconds in, which names the symptom and not the cause (issue #69). If you see
it, go back to `sanderling doctor --platform android` before believing anything
about the sidecar.

Two traps if you build from source rather than installing a release. A plain
`go build ./cmd/sanderling` embeds a placeholder sidecar JAR, so every Android
run stops at `sidecar: binary built without -tags withsidecar`; `make sanderling`
(or `make sanderling-android`) embeds the real one. And `go run ./cmd/sanderling
test` collapses the process exit code: a run that exits 2 comes back from
`go run` as 1 with `exit status 2` printed. Use the built binary whenever the
exit code matters, which is always in CI.

## 2. Point it at the app

Android takes the applicationId, boots an AVD with `--avd`, and picks between
attached devices with `--device <serial>` as `adb devices` prints it:

```sh
sanderling test --spec spec.ts --bundle-id com.example.app --avd Pixel_7_API_34
```

iOS takes `--platform ios` and `--ios-device`, which accepts a simulator name or
UDID, or a connected device's name, UDID, or CoreDevice id. `--ios-app-path`
points at the `.app` bundle and is what makes clear-state real; see section 4.

Web takes a URL as the bundle id:

```sh
sanderling test --spec spec.ts --platform web --bundle-id http://127.0.0.1:8799/index.html
```

The web target has to genuinely load. A page that boots to a blank canvas still
produces steps, still exits 0, and proves nothing: folio's own web leg needs
COOP/COEP headers or its sqlite worker never starts, which is why
`.github/scripts/folio-run.sh` serves the build itself instead of using a stock
static server. Confirm the app rendered before you read anything else.

## 3. Test hooks are a prerequisite, not a polish step

This is the part that decides whether a spec is possible at all. The header of
`replay-ui/sanderling/spec.ts` states it as the lesson it is:

> The hooks it drives (data-testid, data-step, ...) were added to the UI for
> this spec. Needing them is the lesson: a UI with no stable handles is a UI
> nothing can assert on, and that is as true for a person writing a test as it
> is for a fuzzer.

A fuzzer is not asking for anything a human test author does not need. It is
only less able to squint at a screenshot and guess. Budget the hooks as part of
adopting sanderling, before the spec, not after the first vacuous run.

`testTag` is the portable name. `internal/hierarchy/hierarchy.go` aliases it to
`resource-id`, `identifier` and `accessibilityIdentifier`, so one selector
matches on every platform. What you have to add differs:

**Compose on Android.** `Modifier.testTag("AddAccountSubmit")` alone does not
reach the accessibility tree. The tree only carries it when a root composable
sets `semantics { testTagsAsResourceId = true }`. folio does this once, at the
app root, through an expect/actual bridge:
`examples/folio/app/shared/src/androidMain/kotlin/app/folio/ui/TestTagBridge.android.kt`.
Without it every `testTag` selector matches nothing, every property over it
declines, and the run goes green having checked nothing.

**Web.** `data-testid` is the hook. Every `data-*` attribute on the element
reaches the spec under `attrs`, camel-cased the way `dataset` does it, so
`data-step-count` reads as `attrs.stepCount`. That is how the replay-ui spec
reads a panel's own claim about which step it is showing rather than re-deriving
it. Hooks that carry a value, not just an identity, are what make cross-panel
agreement properties possible.

**iOS.** `accessibilityIdentifier`, set via `.accessibilityIdentifier` in
SwiftUI or UIKit. Compose Multiplatform maps `testTag` to it for you.

Two rules about the names themselves. A `testTag` selector falls through to a
substring compare, so `{testTag: "Sub"}` matches `AddAccountSubmit`: make each
hook a whole distinct name rather than a fragment of another. And give every
screen a marker of its own, because a route extractor is what lets a property
decline on the screens it has nothing to say about.

The check that a hook exists is not that you added it. It is that you can point
at a step in a real trace where a selector over it resolved to a value.
`sanderling-spec-authoring` covers which hooks a spec needs and in what order to
add them; this section is about what each platform requires before any of that
reaches the tree.

## 4. A run must start from a known state

`--clear-data` defaults to true and is the difference between a repeatable run
and a measurement of your own leftovers. A second run that inherits the first
one's accounts, cache and completed onboarding diverges at step 1: the seed
reproduces nothing, the two runs' step counts are not comparable, and any number
you quote from the pair is noise.

What "clear" reaches depends on the platform, and in two cases it silently
reaches less than you expect:

- Android wipes app data through the sidecar. On OEM builds that deny
  `pm clear`, pass `--android-app-path <apk>` and it uninstalls and reinstalls
  instead.
- iOS simulator without `--ios-app-path` resets the data container only and
  prints `clear-state requested without an app path: resetting the data
  container only`. With the path it does a full `simctl` uninstall and install.
  The container wipe is a real reset and folio's own iOS leg relies on it; the
  reinstall path is the one that races FrontBoard.
- iOS on a physical device without `--ios-app-path` does not clear at all. It
  prints `clear-state on a physical device requires --ios-app-path for a
  reinstall; skipping (state not cleared)` and carries on. A device run left on
  the default flag inherits every previous run's data.
- Web clears cookies and the target origin's storage. It cannot touch your
  backend. If your app's state lives on a server, reset it yourself between
  runs.

`--clear-data=false` is a legitimate choice in one situation: you have just
installed a fresh build, so the app is already in clear state and an in-run
reinstall would only add a failure mode. Outside that, a run that resumes is a
run you cannot repeat.

## 5. The device does not have to be local

Android talks to whatever adb server the environment names.
`ADB_SERVER_SOCKET=tcp:host:port` (or `tcp:port` for a server on this machine)
is read first, then the older `ANDROID_ADB_SERVER_ADDRESS` and
`ANDROID_ADB_SERVER_PORT` pair, then the loopback default. The CLI shells out to
`adb` and inherits it; the JVM sidecar resolves the same variables when it
attaches to a serial.

Two things to get right. Pass `--device <serial>` exactly as the remote server
reports it: with no serial the sidecar's target is a local `localhost:5555`, not
your remote device. And a serial that already looks like `host:port` is dialled
straight at adbd, bypassing any server, which is a different path with different
failure modes. A value the sidecar cannot parse fails the run rather than
falling back to loopback, and that is deliberate: emulator serials are numbered
per server, so a quiet fallback would drive whatever this machine calls
`emulator-5554` and report the results as the remote device's.

## 6. What a first run prints

A ten step web run, in full:

```
bundled spec: 16532 bytes (sha256=71375ed5bfc7)
bundled web spec: 33131 bytes (sha256=779bae3c8fee)
spec loaded into verifier
trace dir: runs/20260815-172356
running for 1m30s or 10 steps, whichever comes first (seed=7)
step index=1 screen="/index.html" nodes=6
...
step index=10 screen="/index.html" nodes=6

elapsed: 1.715s

run complete: 10 steps
no violations.
```

`nodes=` is the first number to read and the cheapest lie detector you have. On
that page, four elements plus html and body gave `nodes=6`. The same command
against an empty page gives `nodes=2` for every step, and still exits 0 with no
violations. If `nodes` is a handful and never grows, the run is looking at
something that is not your app.

`screen=` is the route marker your spec's screen hooks produce. A run where it
never changes never left one screen.

The summary can carry a third line you should never skim past:

```
7 step(s) judged by nothing: the screen was still moving when it was read
```

Those steps were recorded but no property judged them, so the run's step count
and its checked count are different numbers. `sanderling-run-triage` is about
what to do with that.

Set `--max-steps` whenever you intend to compare two runs: a step budget is what
makes them comparable, since duration alone does not. `--seed` fixes the PRNG,
and seed 0 draws a random one and records it in `meta.json`.

## 7. The run directory

Each run writes `<output>/<UTC timestamp>/`, containing `meta.json`,
`trace.jsonl`, and one PNG per step under `screenshots/`. `--output` defaults to
`./runs`.

`meta.json` is the run's identity: seed, spec path, bundled spec sha256,
platform, bundle id, start and end times, generator, `max_steps`,
`duration_millis`, host, and the `--arm` label if you set one. Two runs that
differ in any of those are different runs and cannot be pooled.

If the app never launched, there is no run directory at all: the launch error
comes before the trace is created. `error: launch app: ...` with nothing under
`./runs` means the run never began, which is a different thing from a run that
began and found nothing.

Open a run with `sanderling replay <dir>`, which accepts either the parent runs
directory or a single run directory.

## Reporting

Say what you actually ran and what came back: the `doctor` output you got rather
than the one you expected, the exact `sanderling test` command, the step count
and the `nodes=` figure from the first run, and for each hook you added, the step
in a real trace where a selector over it resolved. Name what you could not
establish, particularly any platform you did not run on.

Setup is finished when a property can be written that could fail. Write it with
`sanderling-spec-authoring`, review it with `sanderling-spec-review`, and read
the run it produces with `sanderling-run-triage`.
