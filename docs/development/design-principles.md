---
title: Design principles
---

# Design principles

## 1. The app owns introspection; the driver owns input

On native platforms, the in-app SDK knows the state: view hierarchy, coverage, logs, exceptions, custom extractors. Maestro causes the state to change through taps, swipes, typed text, and deep links. The Go runner decides what to do.

Splitting these responsibilities is what makes the system work across iOS and Android with one spec surface. Neither Maestro nor the SDK alone is sufficient.

- Maestro can read a coarse accessibility tree, but not the real `UIView` or `View` hierarchy, not coverage, not in-process logs.
- The SDK can see everything inside the app, but cannot dispatch UI events the way the OS would. Touch injection through Maestro goes through XCTest or UIAutomator, which the OS treats as real input.

On web, Chrome DevTools Protocol handles both input and introspection. No in-app SDK is needed.

## 2. One TypeScript surface across platforms

Spec authors write against `state.ax`, `state.logs`, `state.snapshots`, and so on, regardless of iOS, Android, or web. Platform differences (back button semantics, system alerts, coverage format) are absorbed in the Go runner and the drivers.

Corollary: if a concept only exists on one platform, it does not belong in the spec API. It belongs behind a feature flag or an extractor.

## 3. The driver is an interface

`DeviceDriver` has two production implementations: `sidecar` (Maestro gRPC, for native) and `chrome` (CDP, for web), plus a `mock` for tests. The runner never knows which is wired in. Adding a new platform means adding a new implementation; nothing else changes.

## 4. Hot loops bypass Maestro (native)

On native, per-step introspection (hierarchy dump, coverage read, pause and resume) goes over a local Unix socket directly to the SDK. Only physical UI events go through Maestro's gRPC.

A 30-minute run is about 10,000 steps. Every step has at least one hierarchy dump and one coverage read. If those went through Maestro, the JVM sidecar would be the bottleneck. Instead the hot path is a 2 ms round-trip to an in-process Swift or Kotlin SDK.

On web, CDP is fast enough that a separate introspection channel is not needed.

## 5. Deterministic where it can be

A seeded PRNG drives action selection. Spec evaluation is pure given state and snapshots. The bundle hash and seed are recorded in `meta.json`.

sanderling does not attempt byte-exact replay. Animation timing, keyboard popup timing, and system daemons are non-deterministic on mobile, and the cost of trying to suppress that is not worth the payoff. Same seed produces a similar trajectory, which is usually enough to reproduce the bug.

## 6. Fail honest

If coverage is not available (release build, instrumentation off), tell the user. Do not pretend exploration is guided when it is random. If the SDK is not linked, say so. If a property is unparseable, fail the run at startup, not step 1000.

The alternative, graceful degradation that silently weakens guarantees, is how testing tools lose trust.

## 7. Specs are authoritative; no hidden setup

There is exactly one authoring surface: the TypeScript spec. There is no separate YAML for login, no fixtures directory, no `setup.sh`. Login, onboarding, permission prompts, and teardown are all expressed as action generators or extractors, evaluated in the same loop as the rest of the spec.

This is intentional. A test harness with two authoring languages (YAML plus code, JSON plus code) splits concerns in a way that always drifts. Something works in one surface and not the other, and debugging requires holding both in your head.

