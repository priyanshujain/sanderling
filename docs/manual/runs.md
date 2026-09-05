---
title: Runs
---

# Runs

A run is one `sanderling test` invocation: launch the app, explore it under the spec for a fixed duration, write a trace. Runs typically last minutes to hours, not seconds.

A run is not a unit test. The closer picture is: boot a fuzzer for an hour and see what breaks. A violated property is recorded in the trace and exploration continues, so one run can surface many bugs.

## Lifecycle

```
sanderling test --spec spec.ts --bundle-id com.example.app --duration 30m
  │
  ├── launch the app under test (wipes app data first unless --clear-data=false)
  ├── boot the sidecar (or connect to Chrome on web)
  ├── bundle the spec, load it into the JS runtime
  │
  ├── step 0..N:  read state, check properties, pick and perform an action
  │
  └── stop when --duration elapses (or on Ctrl+C)
        └── trace written to ./runs/<timestamp>/
              ├── trace.jsonl
              ├── screenshots/
              ├── llm-calls.jsonl   (--generator llm only)
              └── meta.json
```

The trace is written incrementally. An interrupted run is complete up to the step where it stopped.

## Typed values in the record

A run types into whatever the app puts on screen, login forms included, and the trace and the model call record are both shared. So a typed value is written down as `[redacted]` whenever the target may be a credential entry: the trace action, the recent-action memory the prompt carries, the numbered candidate list, and the `state.lastAction` a spec reads (and can extract into the trace) all render it that way. The app still receives the real keystrokes; only the record is redacted, and the record still names the field that was typed into.

Which values that covers is decided per field, from what the platform says about it. iOS and web state on every editable field whether it masks its input. Android states it too, though the tree the sidecar gets from maestro does not: maestro's mapper copies a fixed attribute list off the device's view hierarchy and `password` is not on it, so the sidecar re-reads that hierarchy once per settled snapshot and puts the fact back on the text fields it can match. A field it cannot match is left unstated, and an unstated field is redacted.

The re-read is one more device round trip on every screen that has a text field. Measured on 2026-09-05 against `examples/folio` on an emulator behind a remote adb server, seed 1, 1m runs, two per binary: 20 and 20 steps at the merge base (9b4ff5f), 20 and 19 with the re-read (8dc4d08), and a mean gap between steps of 3.08 s and 3.02 s against 3.13 s and 3.31 s. Nearly every screen that seed visits has a text field, so the runs paid for the re-read on almost every step, and it cost at most one step a minute. Two runs each cannot separate the 2% and 10% differences from run-to-run noise.

On iOS and web the fact comes from the platform's own widget type, and a Compose Multiplatform app has none: iOS exposes a password field as a `TextArea` rather than a `SecureTextField` (`internal/driver/ioscompanion/hierarchymap.go`), and web renders it as a `contenteditable` div rather than an `<input type="password">` (`internal/driver/chrome/driver.go`). Both checks then state `secure: false` from a test that cannot say yes for such an app, and the value is written to the record in the clear. Measured on 2026-08-19 against `examples/folio` on both targets: the login password reaches `trace.jsonl` as `ledger123`, in the step's `next_action.text` and again in the `state.lastAction` the following step reports. Android is not affected; the fact it reads is the device's own `password` attribute. Until this is closed, treat an iOS or web trace of a Compose app as holding whatever the run typed.

## App state across runs

By default each run wipes app data before launch and starts cold. Pass `--clear-data=false` to resume whatever the previous run left behind (an account, cached responses, completed onboarding). See the [CLI reference](../cli/#sanderling-test).

## Why runs are long

sanderling does not restart the app every few steps. Restarting throws away two things.

**Accumulated data.** Accounts created, items added, caches warmed, settings changed. Interesting bugs live in apps with history, and a restart wipes it.

**Deep app states.** Many bugs live in states that take many actions to reach: nested settings, a loaded cart, the screen after the third transaction. A 50-step path to "cart with 3 items" never happens if every run starts cold.

Long runs reach states that restart-per-test approaches structurally cannot.

## Setup cost is paid once

Preconditions like login run through the spec's `setup` export (see the [case study](../case-study/#reaching-the-screens-that-matter)). They fire when their condition is unmet and go quiet after, so login costs a few seconds once per run, not once per test case.

| Run length | Login cost | Share of run |
|---|---|---|
| 5 min | ~15s | 5% |
| 30 min | ~15s | 0.8% |
| 1 hour | ~15s | 0.4% |

Those steps are still actions the app received, so the trace names them: every action it records carries a `source` saying whether `setup`, the seeded picker or the model produced it. Anything measured per action counts the last two.

A trace recorded before actions carried a `source` names no producer for any of them, so nothing can say whether its login is inside a per-action count. The analysis marks such a count unattributed and prints how much of it that is, rather than discarding the runs; what it refuses is testing that count against one the login was taken out of, since the two divide by different things.

## Session state

Session tokens, keychain entries, shared preferences, and cookies survive the whole run. If the app logs the user out mid-run, the gating extractor flips, `setup` re-engages, and the run logs back in. No retry logic needed in the spec.

## Termination

A run ends when:

- `--duration` elapses, or
- the process is interrupted (Ctrl+C).

- `--max-steps` is reached, or `--exit-on-violation` was passed and a property was violated.

Hard crash handling lands in the [v0.1.0 milestone](https://github.com/priyanshujain/sanderling/milestone/1).
