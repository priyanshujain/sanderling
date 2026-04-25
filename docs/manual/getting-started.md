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

### Android

The repo ships a working sample at `examples/folio`, a Kotlin Multiplatform app with a TypeScript spec under `sanderling/spec.ts`. Install `just`, then from `examples/folio`:

```sh
just install   # build and install the folio APK on a booted emulator or device
just test      # run the spec
```

With no device connected and multiple AVDs, pick one:

```sh
AVD=Pixel_7 just test
```

Persistent settings can live in a `.env` alongside the justfile (`AVD=Pixel_7`, `DURATION=5m`, and so on).

### Web

The repo also ships a web sample at `examples/folio-web`, a React/Vite app with the same domain logic. From `examples/folio-web`:

```sh
just test      # starts Chrome, runs the spec
```

No emulator or SDK setup needed.

### Trace output

When the run ends, the trace lands in `sanderling/runs/<timestamp>/`:

```
runs/2026-04-18T12-34-56/
├── trace.jsonl
├── screenshots/
└── meta.json
```

Browse it with `sanderling inspect` (see [inspect](./inspect/)), or read `trace.jsonl` step by step.

Next: [writing specs](./writing-specs/).
