---
title: Introduction
---

# Introduction

sanderling tests mobile and web apps by exploring them on its own and checking rules you write. You describe what must always be true about your app. sanderling drives the app for minutes or hours, performing thousands of taps, swipes, and text inputs, and records every moment a rule breaks.

This is property-based testing, applied to UIs.

## What is property-based testing?

Most UI tests are example-based. You script one path through the app and assert what happens:

1. Open the login screen.
2. Type `demo@folio.app` and the password.
3. Tap Sign in.
4. Assert the home screen appears.

This proves one path works. It says nothing about the paths you did not script. What happens when a user taps Submit twice in a row? Navigates away mid-form and comes back? Types `999999999` into the amount field? Each scripted test covers exactly one sequence of inputs, so each weird sequence needs its own test. Nobody writes them all, and the bugs live in the ones nobody wrote.

Property-based testing inverts this. Instead of scripting paths, you state rules that must hold on every path:

- An account never shows a negative balance.
- A new account always starts at zero.
- The app never throws an uncaught exception.

These rules are called properties. sanderling explores the app at random, guided by weights you choose, and checks every property at every step. One property covers every path the explorer finds, including the ones you did not think of.

## Why sanderling?

Scripted UI suites are expensive to write and expensive to maintain. Every flow needs its own script, every screen change breaks scripts, and the suite still only covers the flows someone wrote down.

With sanderling you write one spec per app. The spec is small: a handful of rules and a description of what the explorer is allowed to do. The explorer does the rest. A 30-minute run executes thousands of steps through combinations of screens, inputs, and timings that no human would script.

The same spec runs against Android, iOS, and web builds of the same app. Element lookups resolve across platforms, so a spec written once tests all three.

## How it works

You give sanderling two things: an app and a spec.

The spec is a TypeScript file with two exports:

- `properties`: named rules that must hold. For example, "a new account starts with a zero balance".
- `actionsRoot`: a weighted tree of actions sanderling may take. For example, "mostly add transactions, sometimes create accounts, occasionally tap things at random".

sanderling launches the app and runs a loop. Each pass through the loop is one step:

1. **Read the screen.** Capture the UI tree (every element with its text, position, and state), plus logs and exceptions since the last step.
2. **Check every property** against the new state. A broken property is recorded as a violation and the run continues, so one run can surface many bugs.
3. **Pick one action** from the weighted tree and perform it: a tap, a swipe, typed text, a key press.

```
   ┌────────────────────────────────┐
   │ read screen state              │
   │ (UI tree, logs, exceptions)    │
   └───────────────┬────────────────┘
                   ▼
   ┌────────────────────────────────┐
   │ check every property           │──▶ record violations
   └───────────────┬────────────────┘
                   ▼
   ┌────────────────────────────────┐
   │ pick an action by weight,      │
   │ perform it                     │
   └───────────────┬────────────────┘
                   ▼
                repeat
```

The loop runs until the duration you set elapses. Every step is written to a trace: one JSON line and one screenshot per step. After the run, `sanderling replay` opens the trace in a local web UI where you can walk through the run step by step, see exactly what the screen showed when a property broke, and which action caused it.

## What it runs on

| Platform | How sanderling drives the app |
|---|---|
| Android | A native sidecar reads the UI through UIAutomator and injects input. Works on emulators and devices. |
| iOS | A native sidecar reads the UI through XCTest and injects input. Works on simulators. |
| Web | Chrome, driven directly over the Chrome DevTools Protocol. No sidecar. |

Kotlin Multiplatform apps need nothing special: the Android build is tested through the Android driver and the iOS build through the iOS driver.

## Reading this manual

- [Getting started](./getting-started/) installs the CLI and runs a first test against folio, the example app.
- [Writing specs](./writing-specs/) builds the folio spec from scratch and teaches every concept along the way.
- [Spec language reference](./spec-language/) lists every selector, operator, and action.
- [Runs](./runs/) explains what happens during a run and why runs are long.
- [Replay](./replay/) covers the trace browser.
- [CLI reference](./cli/) lists every command and flag.
