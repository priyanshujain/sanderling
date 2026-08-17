---
title: Driver history
---

# Driver history

The driver layer is the part of sanderling that touches the device: it reads the UI tree, dispatches taps and text, and reports back what happened. It has been rebuilt more times than anything else in the project, and its current shape (a JVM sidecar for Android, no JVM at all for iOS, Chrome driven directly for web) is not what was designed on day one. Two of the original pieces have been deleted outright.

This page records how it got here: what was tried, why each approach was abandoned, and the bugs that forced each move. Commit shas, PR numbers and issue numbers are given throughout so a claim can be checked rather than believed. Dates are merge dates on master.

One theme runs through nearly all of it. The failure was silent, and the run reported success. A tap that never dispatched, a hierarchy read that came back empty, a property that could not fire: none of these turned a run red. They made a green run mean nothing. Most of the work below is the work of finding that out.

## Two paths from day one, 17 April

The first design split introspection from input, deliberately. Issue #30 states the split retrospectively, but it is in the code from the first commits.

Introspection was an in-app SDK: a Kotlin library under `sdk/android/`, linked into the app under test. It paused the app on a Choreographer frame callback, ran extractors on the paused main thread, and shipped length-prefixed JSON over an Android `LocalSocket` in the abstract namespace. Go reached it with `adb reverse` to `localabstract:uatu-agent` and spoke a HELLO/PAUSE/STATE/RESUME/EXTRACT_RESULT/GOODBYE protocol from `internal/agent/` (`ca92bb14`, `b9511c07`, `ac498182`, `52944b1a`).

Input was a JVM sidecar over gRPC, whose `StubDriverBackend` shelled out to raw adb: `uiautomator dump /data/local/tmp/window_dump.xml` for the tree, `adb shell input tap` for the touch (`125a060a`, `6d81e24e`). `e99c85c3` wired the pipeline end to end: bundle the spec, extract the embedded jar, spawn `java -jar`, adb reverse, launch the app, accept the SDK's HELLO, run the loop.

Launching the app was the first thing that lied. `monkey -p <pkg> -c LAUNCHER 1` "reports no error but silently fails to start the activity" on API 36 and up, so the SDK never ran and the CLI sat waiting on a handshake that could not arrive. It was replaced with `cmd package resolve-activity --brief` plus `am start -W -n` (PR #8, `6b1c1732`).

Then the original sin, `16e55086` in PR #12: `InputText` swallowed the error from the focus tap that preceded it, so text typed into the wrong field, or into no field at all, reported success. Three of the most expensive bugs on this page are later and worse versions of exactly that.

## Raw adb was too slow, 22 April

`uiautomator dump` costs 300 to 600ms per hierarchy call, and the loop makes at least one call per step. Issue #30 put the alternatives side by side:

> Maestro's AndroidDriver uses a persistent socket to an on-device APK via AccessibilityService, which is 50-100ms. A purpose-built binary protocol could reach 10-30ms but that optimization is not the priority now.

PR #32 took the middle option. `MaestroDriverBackend` wraps `maestro.drivers.AndroidDriver`, which is dadb plus an on-device gRPC AccessibilityService server. The XML parser was replaced with Maestro's TreeNode JSON, `launcherActivity` was dropped from `Launch` because Maestro resolves it, and `driver.Driver` was renamed `driver.DeviceDriver`. Issue #30 is explicit about the scope of the dependency: "We do not use its YAML flows, Orchestra execution engine, or JS template engine." Maestro is a driver library here and nothing more.

The same PR added `ChromeDriver` over chromedp and CDP. Web has never touched the sidecar.

## Web will not link your SDK, 22 April

PR #36:

> The --platform web path was broken: testrun always waited for an in-app SDK TCP connection that a plain web app never establishes (60s timeout).

This is the first recorded case of the SDK assumption being load-bearing in a place it did not belong. Web became the first platform driven entirely from the accessibility tree, reading `state.ax` with no snapshots at all. Three days later that would be the only way anything worked.

## An iOS SDK that lived one day, 23 April

PR #38 ported the SDK to Kotlin/Native: `IosAgent`, `IosPauser`, a `TcpConnection` doing its own byte swapping, all four snapshot groups. The app was launched with `xcrun simctl` and `SIMCTL_CHILD_` environment injection, so it received `SANDERLING_PORT` before Maestro's XCTest session came up.

It shipped with its own death sentence in the PR body:

> hierarchy fetch failed: invalid character '<' -- Maestro's XCTest driver can only provide view hierarchy for apps it launched through XCTest. Since we launch via xcrun simctl (to inject SANDERLING_PORT), WDA does not have an XCUIApplication session, so Hierarchy() and TapSelector() return HTML error responses.

The detour taken to feed the SDK its port was the thing that broke the driver. It was dead within a day.

## iOS launches through Maestro instead, 23 April

PR #39 added `map<string, string> env = 3` to `LaunchRequest` so the sidecar could forward environment variables through XCTest, and dropped the simctl detour. PR #40 followed immediately with a WDA startup race: the `/status` health check passes before the XCUITest accept loop is ready. PR #41 pinned the iOS backend simulator-only, a limitation cited later as a reason to delete it.

## The in-app SDK is deleted, 25 April

PR #43 (`776becd`) removed both SDKs in one squash: `internal/agent/` on the Go side (about 770 lines), `sdk/android/` (about 1400 lines of Kotlin plus tests), and `IosAgent.kt`, `IosPauser.kt`, `SanderlingIos.kt`. The Maven Central release job went with them. Folio's spec was rewritten to read state from the accessibility hierarchy through `s.ax.*` instead of `s.snapshots.*`.

No reason for this pivot is recorded anywhere. PR #43's body is a pure changelist. There are no review comments, no issue comments, no linked issue, and no commit body explaining it. The only trace of intent is the README diff in the same commit:

```
-Install the CLI, link the SDK into your debug build, run a spec.
+Install the CLI, run a spec.
+- No in-app SDK needed.
```

The reason, recorded here for the first time, is that the accessibility tree turned out to be enough. Web had just been driven entirely from it, and once that was true the SDK's extra fidelity stopped being worth two SDK codebases, a Go protocol and a Maven release. That is the author's recollection rather than a contemporaneous record, which is why the paragraph above says only what the evidence at the time actually shows.

The timing is worth noting on its own. PR #34, three days earlier, had invested in a `Sanderling.snapshot { }` Kotlin property delegate purely to get string keys out of the SDK's API.

The consequence is still visible: `state.snapshots` exists in the spec surface and is permanently empty.

## Web extractors move into the page, 27 April to 3 May

PR #49 added a `driver.WebDriver` capability (`InstallBundle`, `EvaluateExtractors`, `NextActionFromV8`) and `pkg/spec/src/web-runtime.ts`, which installs `globalThis.__sanderling__` inside the page. The constraint recorded at the time is the one that matters: element references never cross the V8 and host boundary, so targets serialize to `{x, y}` from `getBoundingClientRect()`.

This is the origin of the dual-runtime split that `paper/system.md` calls "a real design cost and a source of parity bugs". LTL still evaluates host-side in goja on every platform.

## iOS through the sidecar reaches its ceiling, 5 to 6 June

PR #58 opens with the finding:

> iOS runs never actually drove the app. Every historical iOS trace shows null actions.

Five driver gaps were stacked on top of each other. Compose on iOS surfaces a `testTag` node as an empty leaf sibling of the content it labels, never an ancestor, so every structural descendant or scoped query returned null and no targeted action ever fired. The hierarchy mapping dropped the `XCUIElementType`, so no node carried clickable or editable and the tap and typing verbs found zero candidates. Static text and button strings live in `AXLabel` and not `AXValue`, so `text` came through empty, every balance extractor parsed to zero, and the folio properties were, in the PR's words, "silently disarmed": they evaluated 0 against 0 and passed. `InputText` appended instead of replacing, and `DoubleTap` was two client round trips. Every iOS run had been green because nothing was ever driving the app.

PR #60 found that the runs were poisoning each other. The runner killed the sidecar with SIGKILL, which orphaned the XCTest session the sidecar had spawned; the host process later restarted that dead session in the middle of the *next* run and hijacked the simulator's gesture daemon (`b30f7ce0`, `132b05c2`). Failures looked inexplicable in isolation because they had been caused by the run before.

The same PR fixed a reconnect wrapper that corrupted the app. A read timeout while the device was still typing made the sidecar re-run the whole block: text typed twice, taps double-fired, and login could never succeed because the email field held a doubled address. Non-idempotent actions now reconnect for the next RPC and surface UNAVAILABLE rather than replaying; idempotent reads keep the replay (`4f99af7d`). It also found that the Android `just` recipe never installed the current build, unlike the iOS one, so Android runs had been fuzzing whatever APK happened to be on the emulator already (`386cb12d`).

Both PRs end at the same wall, which is the transport itself:

> The prebuilt XCTest runner serializes touch requests at ~350ms per tap, so a sub-100ms double tap cannot be synthesized on the iOS simulator through this transport. The folio fixture's race window is therefore unreachable on iOS; reproduction is verified on Android.

A driver that cannot express the input the fixture bug needs is not a driver you can fix incrementally.

## The iOS simulator leaves the JVM, 8 June

PR #62 replaced the whole iOS simulator path with a Go-native hybrid under `internal/driver/ioscompanion/`, in two halves behind one transport seam (`internal/driver/ioscompanion/transport`). Meta's idb_companion 1.1.8 is vendored, embedded, sha-pinned and behind a macOS-only build tag, and provides HID gestures with millisecond-precise inter-event timing, screenshots and screen geometry. A purpose-built Swift XCTest runner (`companion/`, 755 lines) runs inside the simulator and speaks newline-delimited JSON over loopback TCP for accessibility snapshots, native unicode typing and app lifecycle.

Measured against the sidecar path: p95 step latency fell from 3365ms to 1693ms, collapsed accessibility dumps went from roughly 15% to none over 200 reads, and gate G4 (no doubled text) went from 2/5 to 5/5. `SANDERLING_SIMULATOR_COMPANION=legacy` still exists as an escape hatch.

Getting a genuine double tap out of XCTest took its own investigation (`a6052c67`, `companion/Sources/Gesture.swift`). `XCPointerEventPath` honours event offsets to the millisecond within a path, but nothing after the path's first `liftUp` is ever delivered:

> nothing after the path's first lift is delivered: a second press is silently dropped.

Across paths every event is delivered, but each path's timeline is normalised to its own first event, so a later absolute start offset collapses to zero; and two paths sharing a pointer index at the same point are coalesced by the system into a single multi-tap. The working shape is one path per tap, a distinct `path.index` per tap, each path anchored by a raw zero-offset move at the tap point so its timeline origin is pinned to the record's, and a trailing hover move stretching each path to the next press's offset. The raw move-event shape is read at runtime from a probe path rather than hardcoding private enum values.

Three other findings from the same PR are worth keeping. Roughly 15% of legacy accessibility reads contained no UI at all: the bridge briefly reports only the application element during cold start and screen transitions, and nothing distinguished that from a genuinely empty screen, so the fuzzer acted on an empty tree and the trace recorded a screenshot of a real screen beside a hierarchy of nothing (`a745ec79`, `41d80b77`). Unicode typing went through the pasteboard because HID cannot express it, and every external pasteboard write re-triggers the iOS paste-permission dialog, whose dismissal blacks out the accessibility bridge for about 2.5s; the original loop re-sent the paste chord on each failed verification, so a slow render was indistinguishable from a swallowed paste and the text pasted twice. That was fixed by sending once and polling on a budget sized to outlast the blackout, then by pre-granting `kTCCServicePasteboard` with a row written directly into the simulator's TCC database (`simctl privacy grant` does not expose the pasteboard service), and finally made moot by native `typeText` through the in-simulator runner (`432154ee`, `3ee26ca5`, `edd70b66`). And the automation server was listening on the LAN: the simulator shares the host's network stack, so an `NWListener` bound with default parameters was an unauthenticated remote control for taps, typing and screenshots, fixed with `parameters.requiredLocalEndpoint` (`fb6cfce3`, `93b62d4a`).

## The sidecar's iOS backend is deleted, 8 June

PR #65:

> The JVM sidecar's iOS backend was left in place but is now dead: it has been simulator-only since #41 and fails on physical devices, and the simulator path no longer routes through it.

The embedded jar went from 107.4 MB to 91.5 MB, and the sanderling binary from 173 MB to 149 MB.

## iOS on a physical device, 9 June

PR #66 drove a real device with the runner alone. idb_companion is not on the device path at all:

> The legacy idb_companion cannot drive a device on iOS 26 (Screenshot/AccessibilityInfo/HID all fail), so it is not on the device path.

The host-to-device tunnel is an in-process Go forwarder speaking the usbmux plist protocol to `/var/run/usbmuxd`, so a device run needs nothing installed beyond macOS and Xcode. The runner is built and code-signed at run time using an App Store Connect API key, cached on a hash of its sources.

## Android on a physical device, 11 June

PR #67 kept the Maestro sidecar and hardened it for real hardware, which mostly meant discovering how much of the emulator's good behaviour had been assumed.

Every by-selector tap on a physical device did nothing, silently. uiautomator reports bounds as `[left,top][right,bottom]`, and the sidecar's `parseBounds` only understood the flat `[l,t,r,b]` form that its own long-dead stub backend had emitted. The regex never matched, bounds resolution returned null, and `tapSelector` returned quietly instead of erroring, so whole runs walked the app without touching it (`433f786`, `a245194`). Returning quietly dispatched no tap at all and left the step reading as an action that landed.

Swipes pulled down the notification shade. A zero-bounds element centres at (0,0), and a downward swipe from the top-left corner is exactly the system gesture for the shade. That compounded with a second bug: the Android hierarchy root itself reports zero bounds, so the safe-area clamp computed a zero-sized screen and no-opped. The fix requires positive bounds for swipe candidates, derives the screen rect from the maximum element extent rather than from the root, clamps origins out of the top 7% or so, and forces 3-button navigation for the duration of the run so edge-back and swipe-up-home are off at the OS level (`f9aa44f7`, `9c9d3c93`, `542bf11a`).

`--clear-data` reported a clean start and ran on the previous run's data. ColorOS denies CLEAR_APP_USER_DATA even to the adb shell user, so `pm clear` fails, and the uninstall fallback hid its own failure: `adb uninstall` answers DELETE_FAILED_INTERNAL_ERROR both when the package was never installed and when it refuses to remove one. The failure text cannot say which happened, the old code installed over the top either way, and `install -r` keeps the data (`015f6196`, `921ecb91`).

The fuzzer kept tapping Gboard's Settings key and leaving the app, with package-scoped filtering on. That key is a bare `FrameLayout` with a content description, no package and no resource-id, so a per-element package check treats it as in-scope. A keyboard-region Y heuristic was tried and thrown away; the fix walks the window tree propagating each node's owning package, treating empty and the neutral `android` package as transparent, and calls a node in scope only when no concrete foreign package owns it (`41955d56`, then `505367f4`).

Two devices attached silently disabled app-scope enforcement entirely. `ForegroundPackage` and `FocusedWindowPackage` ran bare `adb shell` with no `-s`, which errors when more than one device is attached, and the guard read the error as "cannot tell" and stopped enforcing; `pickDevice` had been silently returning `connected[0]` as well (`c48b13f3`).

Android itself fights the driver. The cached-app freezer, the phantom process killer, Doze and OEM additions such as ColorOS OSense all suspend on-device instrumentation while the app under test is foreground, and device prep is hostile in its own right (HyperOS SIGKILLs `svc power stayon`). The settings writes have to go through `set_sync_disabled_for_tests`, otherwise server-side device_config sync reverts them (`300af130`).

Long text found its own way out of the app. A 4096-character string exceeds the fast `adb input text` cap and fell to the per-character path, which takes about 120s and cannot be interrupted, long enough for the app to lose foreground so that the remaining keystrokes landed in the launcher's search box. Shell-safe ASCII of any length now goes through `adb input text` in 512-character chunks with the foreground owner rechecked between chunks (`77aa6778`). The degraded case came later: an unreadable dumpsys passed a null owner, which switched the guard off entirely, so it now falls back to the launched bundle id (`8d7ef4d6`). An unreadable probe must never read as focus being fine.

p95 step latency went from about 6.5s to 3 or 4s, and gate G5's budget became backend-aware (2500ms on the simulator, 5500ms on physical Android).

## The keyboard is a window you cannot see

By this point the driver's shape was settled and the remaining work was about what happens on the device. Most of it was the IME, and the IME turned out to be the single most expensive object in the system.

The keyboard does not cover the submit button, it deletes it. The IME is its own window, and the Android hierarchy carries only what is visible, so every app node underneath the keyboard is absent from the tree the action picker enumerates from, submit control included. Typing raises the keyboard, so a form becomes an absorbing state: an IME left open hides a form's submit control for as long as the fuzzer keeps typing into that form, which is a state it cannot type its way out of. In one 178-step run, 19 steps were stuck there and add-transaction was structurally unreachable (issue #78, `b4cd9d5c`, PR #82).

Dismissing the keyboard means BACK, and BACK without a keyboard leaves the screen, so an unguarded dismissal turns every `InputText` into a screen exit. The obvious guard, `mInputShown`, trails the BACK it answers for by about 0.6s on API 36, so a second dismissal inside that window reads a stale true. The snapshot path guards on the IME's own view ids in the tree instead, precisely because the flag lags. The `mInputShown` guard is load-bearing rather than an optimisation (`276c05ac`, `4bbab3ff`).

One step spent 121 seconds deleting a field, and the run exited 0. The corpus types a 4096-character string, and the next `InputText` triggers `EraseText(4096)`, which delegated to maestro's `eraseAllText`: one delete keyevent per character, measured at 29.6, 29.6, 29.8 and 30.1 ms per character with jstack on the live sidecar JVM. Two of those in a 20-minute budget is a fifth of the run spent deleting characters. It is not a hang and not a timeout: nothing fires, nothing is abandoned, the step just does two minutes of real work on the device and the run carries on. `input keycombination 113 29` followed by keyevent 67 is two events at any length, and takes 0.15s (issue #80, `7fa2d71e`).

The erase had a second failure on top of that. The check for "did select-all work?" looked for an `editable` attribute that maestro's tree does not carry, so it always answered "cannot tell", and with the keyboard open the tree holds a second focused node, one of the IME's own keys, carrying no text. Taking the first focused node would read a field still holding 4096 characters as empty, which is the one answer that stops the erase early (`3df8a3b3`). Backspace also only deletes to the left of the cursor while the runner taps the field centre, so everything to the right survived and the next `InputText` typed into the residue: 7 of 19 measured `InputText` observations left characters behind, and the spec was reasoning about values that were not what got typed.

Two other IME findings are about the reads rather than the writes. The runner reads the hierarchy twice per step to detect a screen that changed while it was looking, but `Snapshot` settled the tree and closed a keyboard while `Hierarchy` was a bare `contentDescriptor` read that did neither; with an IME open on API 34 the two reads of the same still screen disagreed by 355 nodes (489 against 134). The comparison was measuring what the backend did between the reads, not what the app did (`1afe9c3a`). And idle detection is `dumpsys window -a | grep -c mAnimating=true`, whose parse defaulted an unreadable count to zero, which means "nothing animating, go ahead"; a degraded adb link therefore broke out of the settle early and handed the runner a frame caught mid-animation (`9ff82b15`). A count we could not read is not a count of zero.

## Hardening what was left

A wedged adb could hold a step for as long as it liked. `adbOutput` and `readLogcat` read to EOF and then waited, with no timeout on either, and a wedged adb never reaches EOF, so a bounded `waitFor` after the read is a line that never runs. Putting the wait first is worse: a process with more to say than the pipe buffer holds, which logcat and dumpsys both are, blocks writing while the waiter waits. The bound belongs on the read, not on `waitFor` (`23a24544`).

The Kotlin sidecar hardcoded dadb to `localhost:5037`, so `ADB_SERVER_SOCKET=tcp:host:5037 adb devices` could list three remote emulators that the run then could not reach; the Go side and the adb CLI both honour the variable and the sidecar did not (issue #79, `83e9d5fd`). It is the difference between pointing sanderling at a device farm and requiring the emulator to be on the same machine.

On iOS, a refused launch is never reported and wedges the session for four minutes. XCTest records the refusal as a test failure that the runner process cannot observe, then holds the session's main thread for about four minutes on a diagnostic chain: a 120s accessibility wait, a spindump, an idle wait. There is no error text to key a retry on, only the expired bound, and the launch RPC ran before the run's duration clock started so no deadline covered it. The lifecycle RPC is now bounded at 90s inside the driver, and a blown bound restarts the whole XCTest session and launches once more, because only a session that never served the refused launch can serve the retry (`0c249ec2`, `d64781ce`, `f073f2cc`).

The guard for a wedged session could never have gone red. It tested `errors.Is(err, context.DeadlineExceeded)`, but the same expiry arrives in three shapes: the context's error once cancellation lands, `os.ErrDeadlineExceeded` when the socket deadline armed from that context fires first, and a gRPC DeadlineExceeded status from the legacy transport. Only the first matched, and the unit test's fake answered with a raw `ctx.Err()`, which is the one shape the guard already matched (`f1a5db88`, `f911079e`).

iOS clear-data had three independent holes (`abc5e7db`, `6234bf5b`, `220c7fe1`, `6beea910`). `simctl install` over an installed app carries its data container across, and the uninstall's error was discarded, so a failed uninstall reported a clear-state that never happened; the device path had the same hole with devicectl. On a physical device there is no data-container wipe at all, so `--clear-data` with no `--ios-app-path` warned and then ran anyway. And the "did we clear?" guard was a bare bool, so `Launch(otherBundle, clearState: true)` passed a guard that had cleared a different bundle.

## What the driver owes the verifier

Three fixes belong to the seam between the driver and the spec rather than to any platform, and they are the ones that decide whether a trace can be trusted.

"No action happened" was an assumption, not a fact. When `applyAction` errored, the runner told the spec `lastAction = nil`, but a deadline that fires *after* the tap was dispatched leaves the effect committed on the device, and a property convicted a healthy app on exactly that. `lastAction` became three-valued: null for nothing dispatched, `applied:true` for a dispatch seen to succeed, and `applied:null` for dispatched with the outcome unknown. Demanding properties decline on unknown (PR #77, `6e52fa08`).

`applyAction` could also return nil without ever calling the driver, along six paths: a tap, double-tap or long-press whose coordinates did not resolve and carried no selector; a long-press with a stale selector; an empty key press; a zero-duration wait. The trace showed an action that looked executed and acted on nothing, so the verifier attributed the next observed state to an action that never ran (`bc4b9fb2`, PR #74).

And `InputText` still could not be sure where its text went. It tapped its target, slept, then typed, and Android injects into whatever holds focus. On an emulator with a floating keyboard panel parked over the password field, the tap hit the emoji key, focus stayed on email, and the password went into the email field on every step for the whole run, because the setup leaf is guarded on the password being empty. A different element held focus in 15.8% of 717 measured `InputText` steps (`9a2bbbf7`, PR #74). This is `16e55086` again, four months and one architecture later.

## Where it stands

One Go binary does everything: esbuild bundles the TypeScript spec, goja evaluates it in-process, the step loop dispatches through the `driver.DeviceDriver` interface, and the trace is written as JSONL and PNG. `buildDriver` in `internal/testrun/driver.go` is the single routing point, and it picks one of four paths.

Android, emulator and USB device alike, goes through the JVM sidecar: Go spawns `java -jar sidecar-all.jar --port N --platform android [--serial S]` and talks gRPC on 127.0.0.1, and `MaestroDriverBackend` uses Maestro 2.4.0's AndroidDriver over dadb, with the hierarchy coming from `driver.contentDescriptor(false)`. Short ASCII text takes the faster `adb shell input text` path. The iOS simulator has no JVM at all: the Go-native hybrid in `internal/driver/ioscompanion/` runs vendored idb_companion for HID gestures, screenshots and geometry, alongside the project's own Swift XCTest runner inside the simulator for accessibility snapshots, unicode typing and lifecycle. A physical iOS device uses the same Swift runner alone, built and signed at run time and reached over the in-process usbmux forwarder, with no idb_companion and no sidecar. Web is Chrome over CDP, with extractors and the action picker executing in V8 inside the page and only coordinates and values crossing back; LTL always evaluates host-side in goja.

`sanderling replay` is the same binary in read-only mode, serving HTTP and an embedded React SPA with an SSE file watcher over `runs/`. It has no driver and no evaluator.

Some of the wreckage is still in the tree. `WdaRecovery` and `reapOrphanIosRunners` in `DriverBackend.kt` have had no production callers since PR #65 and are reached only by tests, and `Main.kt` still routes any non-android platform to `StubDriverBackend`, which Go no longer spawns.

One bug is documented as open rather than fixed. On iOS, `simctl uninstall` plus `install` followed immediately by the XCTest runner's launch fails about half the time on one machine, with `app.folio` reported as unknown to FrontBoard: the clear-state reinstall races FrontBoard's app registration under a live XCTest session. On the machine used to work on it, 20 consecutive reinstall-and-launch cycles, 10 of them over a live app, never reproduced it once. Clear-state was moved into driver construction so it happens before any automation session attaches, and `Launch` now refuses a clear-state request the driver was not built for, which avoids the window; the race itself is untouched (`aed65976`, `67da9313`). Any fix for it has to be developed on a host that can still show it failing.
