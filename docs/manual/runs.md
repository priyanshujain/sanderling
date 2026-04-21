---
title: Runs
---

# What is a run?

One `sanderling test` invocation. Fresh install, spec-driven exploration, then the trace lands in `runs/<timestamp>/`. Typically minutes to hours, not seconds.

A run is not analogous to a unit test. A closer framing is: boot a fuzzer for an hour and see what breaks. Violations are recorded in the trace and exploration continues, so one run can surface many bugs.

## Lifecycle

```
sanderling test --spec spec.ts --bundle-id com.example.app --duration 30m
  │
  ├── uninstall and reinstall the app (clean slate, every run)
  ├── boot the sidecar, connect the agent socket
  ├── bundle the spec, load it into goja
  │
  ├── step 0..N:  pause, capture state, evaluate properties, pick action, resume, dispatch
  │
  └── terminate when --duration elapses (or SIGINT)
        └── trace written to ./runs/<timestamp>/
              ├── trace.jsonl
              ├── screenshots/
              └── meta.json
```

## Why runs are long and linear

sanderling does not restart the app every N steps. Each restart throws away two things.

**Novelty and coverage signal.** The exploration strategy weights actions by whether they reach previously unseen state. Restarting resets that history.

**Deep app states.** Many screens take many actions to reach: nested settings, a loaded cart, post-checkout flows. A 50-step prefix to reach "cart with 3 items" does not happen if every run starts cold.

Long-linear trajectories find bugs that restart-based testing structurally cannot.

## Setup cost amortizes

Preconditions (login, onboarding, consent dialogs) are written as weighted action generators gated on extractors. See [writing specs](./writing-specs.html#pattern-preconditions-login-onboarding). They fire only when applicable, so login happens once per run, not per step.

| Run length | Login cost | % of run |
|---|---|---|
| 5 min | ~15s | 5% |
| 30 min | ~15s | 0.8% |
| 1 hour | ~15s | 0.4% |
| CI: 3 seeds × 10 min | ~45s total | 2.5% |

At any non-trivial run length, preconditions are a rounding error.

## Session state

Session tokens, keychain, shared prefs, cookies, and other app-managed persistence survive the full run. If the app logs the user out mid-run, the `doLogin` generator re-fires automatically because its gating extractor (`onLoginScreen`) becomes true again. No retry logic. No special-casing.

## Termination

A run ends when either of these happens.

- `--duration` elapses.
- The process is interrupted (SIGINT).

The trace is written incrementally, so an interrupted run is still fully inspectable.

Additional termination conditions (`--max-steps`, `--exit-on-violation`, hard crash handling) land in [v0.1.0](https://github.com/priyanshujain/sanderling/issues/4).
