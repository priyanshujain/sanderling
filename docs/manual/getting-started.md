---
title: Getting started
---

# Getting started

This page installs sanderling and runs a first test against folio, the example app that ships with the repo. By the end you will have a trace of a real run open in your browser.

If you have not read the [introduction](../introduction/), start there. It explains what sanderling does and how it works.

## Install

Install the CLI:

```sh
curl -fsSL https://raw.githubusercontent.com/priyanshujain/sanderling/master/install.sh | bash
```

Specs import from the `@sanderling/spec` package on [npm](https://www.npmjs.com/package/@sanderling/spec). Install it in the project where your spec lives:

```sh
npm install --save-dev @sanderling/spec
```

The example apps already depend on it, so for this page the CLI is enough.

## Check your environment

```sh
sanderling doctor
```

`doctor` checks each platform and tells you what is missing. What each platform needs:

- **Web**: Chrome. Nothing else.
- **Android**: `adb` on your PATH, and an emulator (API level 30 or newer) or a connected device.
- **iOS**: Xcode 16 or newer, with a simulator.

Web is the easiest place to start. If you have Chrome, you can run a test right now.

## Your first run

Clone the repo and pick an example app. Both are small personal-ledger apps: log in, create accounts, add transactions.

- `examples/folio` is a Kotlin Multiplatform app. One codebase builds for Android, iOS, and web.
- `examples/folio-web` is a React + Vite app. Web only.

Both carry a spec at `sanderling/spec.ts`. The examples use [`just`](https://github.com/casey/just) as a command runner, so install it first.

### Web

The quickest path. From `examples/folio-web`:

```sh
just test
```

This starts the Vite dev server, then runs `sanderling test`. sanderling launches Chrome and starts exploring: logging in, creating accounts, adding transactions, and tapping things at random. Let it run, or stop it early with Ctrl+C.

For the Kotlin Multiplatform web build instead, run `just web` from `examples/folio`.

### Android

From `examples/folio`, with an emulator booted or a device connected:

```sh
just install   # build and install the folio APK
just test      # run the spec
```

If no device is connected and you have several AVDs, pick one:

```sh
AVD=Pixel_7 just test
```

Settings you use every time can live in a `.env` file next to the justfile (`AVD=Pixel_7`, `DURATION=5m`, and so on).

### iOS

From `examples/folio` (requires Xcode 16+ and `xcodegen`):

```sh
just test-ios                          # default simulator: iPhone 17 Pro
IOS_DEVICE="iPhone 15" just test-ios   # pick a different simulator
```

`just test-ios` boots the simulator if needed, builds and installs the app, then runs `sanderling test --platform ios`.

#### Physical device

A connected iPhone is driven over a usbmux tunnel by a runner the driver builds and signs at run time. The tunnel talks to macOS's own `usbmuxd`, so nothing extra is installed beyond Xcode. It needs App Store Connect signing credentials in the environment (a gitignored `.env` is loaded by `just`):

```sh
SANDERLING_IOS_TEAM=<10-char team id>
ASC_API_KEY_ID=<key id>
ASC_API_ISSUER_ID=<issuer id>
ASC_API_KEY_PATH=<absolute path to AuthKey_*.p8>

IOS_DEVICE="iPhone" just test-ios-device   # name, UDID, or CoreDevice id
```

Run `sanderling doctor --platform ios-device` to check `devicectl`, the `usbmuxd` socket, a connected and paired device, and the signing credentials before a run.

## What you get

When the run ends, the trace lands in `sanderling/runs/<timestamp>/`:

```
runs/2026-04-18T12-34-56/
├── trace.jsonl       one JSON line per step
├── screenshots/      one screenshot per step
└── meta.json         seed, duration, run metadata
```

The trace is written as the run goes, so a run you interrupt is still complete up to the point you stopped it.

## Look at the run

```sh
sanderling replay
```

This opens a web UI for the trace. Step through the run with `j` and `k`. Each step shows the screenshot, the action taken, the state of every property, and the full UI hierarchy. If a property broke, press `.` to jump to the violation and see exactly what the screen showed when it happened.

The folio spec gives extra weight to rapid double taps, because a form that submits twice on a double tap is a classic bug. folio's transaction form has exactly that flaw. When the explorer hits it, the balance moves by twice the typed amount, the `submitMovesBalanceByTypedAmount` property fires, and the timeline shows the violation.

See [replay](../replay/) for the full panel and shortcut reference.

## Next

You have run sanderling against an app with a finished spec. The next page builds that spec from scratch, one concept at a time: [writing specs](../writing-specs/).
