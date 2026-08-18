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

Which values that covers differs by platform, because the platforms differ in what they report. iOS and web state on every editable field whether it masks its input, so only the fields that do are redacted and the rest of the memory keeps its values. Android reports nothing: uiautomator's password attribute is dropped by the native tree mapper before the driver sees it, a password field is indistinguishable from a search box there, and so every typed value on Android is redacted.

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
