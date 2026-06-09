---
title: Getting started
---

# Getting started

Install the CLI, run a spec.

## Prerequisites

**Android / iOS:**

- An Android emulator with API level 30 or newer (or a connected device).
- `adb` on your PATH.

**Web:**

- Chrome installed. sanderling drives it via CDP; no other setup required.

Run `sanderling doctor` to check the host environment.

## Install

### CLI

```sh
curl -fsSL https://raw.githubusercontent.com/priyanshujain/sanderling/master/install.sh | bash
```

### Spec package ([npm](https://www.npmjs.com/package/@sanderling/spec))

```sh
npm install --save-dev @sanderling/spec
```

## Your first run

The repo ships two sample apps. `examples/folio` is a Kotlin Multiplatform personal-ledger app that covers Android, iOS, and web (wasmJs) from one shared codebase. `examples/folio-web` is a smaller React + Vite app that covers only the web path. Both carry a TypeScript spec under `sanderling/spec.ts`. Install `just`, then pick a target below.

### Android

From `examples/folio`:

```sh
just install   # build and install the folio APK on a booted emulator or device
just test      # run the spec
```

With no device connected and multiple AVDs, pick one:

```sh
AVD=Pixel_7 just test
```

Persistent settings can live in a `.env` alongside the justfile (`AVD=Pixel_7`, `DURATION=5m`, and so on).

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

### Web

From either example. For the KMP wasmJs build, use `examples/folio`:

```sh
just web       # serve the wasmJs app on a webpack dev server
```

For the React + Vite build, use `examples/folio-web`:

```sh
just test      # starts the Vite dev server, then sanderling drives Chrome via CDP
```

No emulator or SDK setup needed for either web path.

### Trace output

When the run ends, the trace lands in `sanderling/runs/<timestamp>/`:

```
runs/2026-04-18T12-34-56/
├── trace.jsonl
├── screenshots/
└── meta.json
```

Browse it with `sanderling replay` (see [replay](./replay/)), or read `trace.jsonl` step by step.

Next: [writing specs](./writing-specs/).
