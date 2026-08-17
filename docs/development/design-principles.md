---
title: Design principles
---

# Design principles

## 1. The driver both reads and drives

Introspection and input come through the same interface. On Android and iOS the state is the platform accessibility tree (an on-device AccessibilityService, XCTest's `XCUIApplication.snapshot()`); on web it is the DOM, read over CDP. The same driver dispatches taps, swipes, typed text, key presses, and launches, and the Go runner decides what to do.

There is no in-app SDK and no second channel into the app's process. On native that is a hard limit on what a spec can know: the tree carries no real `UIView` or `View` hierarchy and nothing the platform declines to expose to accessibility, and an extractor cannot recover a fact the tree left out. On web, extractors run inside the page and read the DOM directly.

## 2. One TypeScript surface across platforms

Spec authors write against `state.ax`, `state.logs`, and so on, regardless of iOS, Android, or web. Platform differences (back button semantics, keyboard dismissal) are absorbed in the Go runner and the drivers.

Corollary: if a concept only exists on one platform, it does not belong in the spec API. It belongs behind a feature flag or an extractor.

`state.snapshots` is the one place this surface lies. It was the in-app SDK's extractor output, the SDK is gone, and nothing populates it now: a spec that reads it gets an empty object on every platform.

## 3. The driver is an interface

`DeviceDriver` has three production implementations: `sidecar` (gRPC to the JVM sidecar, for Android), `ioscompanion` (Go-native, for the iOS simulator and for physical devices), and `chrome` (CDP, for web), plus a `mock` for tests. The runner never knows which is wired in. Adding a new platform means adding a new implementation; nothing else changes.

## 4. The step loop pays for every round trip

Every step reads the hierarchy and a screenshot before anything can be evaluated, and there is no cheaper channel to read them from: introspection and input share one driver. Per-step latency is therefore the run's budget, and it is why the iOS simulator is driven Go-natively rather than through the JVM sidecar, whose p95 step latency was about twice as high ([driver history](./driver-history/)).

On web the per-element work stays in the page. Extractors and the action picker run in V8 and only coordinates and values cross back to Go, because a CDP round-trip per element would dominate the step.

## 5. Deterministic where it can be

A seeded PRNG drives action selection. Spec evaluation is pure given state. The bundle hash and seed are recorded in `meta.json`.

sanderling does not attempt byte-exact replay. Animation timing, keyboard popup timing, and system daemons are non-deterministic on mobile, and the cost of trying to suppress that is not worth the payoff. Same seed produces a similar trajectory, which is usually enough to reproduce the bug.

## 6. Fail honest

If a property is unparseable, fail the run at startup, not step 1000. If a probe cannot be read, do not read it as the reassuring answer: an unparseable count of animating windows is not "nothing is animating", and an action whose outcome the driver could not observe is not an action that landed, which is why `state.lastAction.applied` is three-valued.

The alternative, graceful degradation that silently weakens guarantees, is how testing tools lose trust.

## 7. Specs are authoritative; no hidden setup

There is exactly one authoring surface: the TypeScript spec. There is no separate YAML for login, no fixtures directory, no `setup.sh`. Login, onboarding, permission prompts, and teardown are all expressed as action generators or extractors, evaluated in the same loop as the rest of the spec.

This is intentional. A test harness with two authoring languages (YAML plus code, JSON plus code) splits concerns in a way that always drifts. Something works in one surface and not the other, and debugging requires holding both in your head.


## 8. A property that cannot fail is worse than one that fails

A spec fails loudly when it is wrong about the app. It fails silently when it is wrong about
itself, and that is the failure this project has to design against.

Two shapes recur. A property is *vacuously true* when a fact it reads is missing, so it never
fires and every run is green: `state.lastAction` was hardcoded null on web, `ax.findAll` on a
selector path returned nothing there, and no element on iOS carried `text` because the mapping
only ever read `AXValue`. In each case a correct property proved nothing. A property is
*degenerately false* when a missing fact is read as a value instead: folio's balances parsed as
`0` on web, so the check became `|0 - 0| === typedAmount` and fired on every healthy submit.
Both shapes look exactly like a working property from the outside.

So an unreadable fact is unknown, never a default. Extractors return null rather than zero or
empty string, and a property given an unknown declines to judge instead of convicting. The same
rule covers arithmetic the representation cannot hold: integer cents in a float64 stop being
exact past 2^53, and a comparison that cannot pass is not a comparison that failed.

Which means a green run is evidence only if you can point at a step where the property actually
fired, and a red one only if the witness holds real values. Read the witness, not the exit code.
Every bug listed above was found that way, and each had already survived a run that looked fine.
