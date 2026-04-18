---
title: Getting started
---

# Getting started

Install the CLI, link the SDK into your debug build, run a spec.

## Prerequisites

- An Android emulator with API level 30 or newer.
- The app under test built as a debug variant with the uatu Android SDK linked in.
- `adb` on your PATH.

Run `uatu doctor` to check the host environment.

## Install

### CLI

macOS arm64:

```sh
curl -L https://github.com/priyanshujain/uatu/releases/latest/download/uatu_<version>_darwin_arm64.tar.gz | tar xz
./uatu version
```

Linux amd64:

```sh
curl -L https://github.com/priyanshujain/uatu/releases/latest/download/uatu_<version>_linux_amd64.tar.gz | tar xz
./uatu version
```

Pre-built for `darwin/arm64`, `darwin/amd64`, `linux/amd64`, `linux/arm64`.

### Spec package (npm)

```sh
npm install --save-dev @uatu/spec
```

### Android SDK (Maven Central)

```kotlin
dependencies {
    implementation("io.github.priyanshujain:sdk-android:<version>")
}
```

## Your first run

The repo ships a working sample at `examples/sample-app`. From that directory:

```sh
npm install
(cd android && ./gradlew installDebug)
uatu test \
  --spec spec.ts \
  --bundle-id dev.uatu.sample \
  --platform android \
  --duration 2m
```

Pass `--avd <name>` only when no device is connected and you have multiple AVDs; otherwise uatu uses the connected device or boots the single AVD it finds.

When the run ends, the trace lands in `runs/<timestamp>/`:

```
runs/2026-04-18T12-34-56/
├── trace.jsonl
├── screenshots/
└── meta.json
```

Open the screenshots directory to scrub visually, or read `trace.jsonl` step by step.

Next: [writing specs](./writing-specs.html).
