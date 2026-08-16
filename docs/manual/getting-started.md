---
title: Getting started
---

# Getting started

Install sanderling, write a spec for your app, run it, and open the trace.

## Install

The CLI:

```sh
curl -fsSL https://raw.githubusercontent.com/priyanshujain/sanderling/master/install.sh | bash
```

The spec package, in your project:

```sh
npm install --save-dev @sanderling/spec
```

Both come from the same release tag, and the CLI bundles the package's TypeScript sources when it evaluates your spec, so upgrade them together. Pre-releases are published under npm's `next` tag; `npm install @sanderling/spec` gives you the current stable one.

## Check your environment

```sh
sanderling doctor
```

`doctor` reports what the target platform needs and what is missing:

- **Android**: `adb` and `emulator`, on your PATH or under the Android SDK; Java 17 or newer; a real (not placeholder) sidecar JAR in the binary.
- **iOS**: `xcrun` and `simctl`. For a connected iPhone, run `sanderling doctor --platform ios-device`, which also wants `devicectl`, a paired device, and signing credentials.
- **Web**: a Chromium that launches headless.

## Write a spec

A spec exports two things: `properties` that must always hold, and `actionsRoot`, the actions sanderling may take. The smallest spec that does something useful imports both from the defaults:

```ts
import { defaultActions } from "@sanderling/spec/defaults";
import { noUncaughtExceptions } from "@sanderling/spec/defaults/properties";

export const properties = { noUncaughtExceptions };
export const actionsRoot = defaultActions;
```

This taps, types, double-taps, scrolls, and swipes at random. Check the property matches your platform before you trust a green run: `noUncaughtExceptions` reads exceptions the web runtime captures in the page, so it fires on `--platform web` and holds unconditionally on Android and iOS. On Android use `noLogcatErrors` instead, which fails on any error-level log line and so catches an uncaught throwable. iOS has neither today, so an iOS run is only worth as much as the properties you write yourself. From here you add extractors to read your screens, properties that state what your app guarantees, and actions that drive its real flows. The [case study](../case-study/) walks a complete spec, and the [spec language reference](../spec-language/) lists every primitive.

## Run it

Point sanderling at your app:

```sh
sanderling test --spec spec.ts --bundle-id com.example.app --platform android
```

Use `--platform ios` or `--platform web` for the other targets. By default a run lasts five minutes and starts from a fresh install; `--clear-data=false` resumes prior state, and `--duration 30m` runs longer. The [CLI reference](../cli/) lists every flag.

## See what it found

```sh
sanderling replay
```

This opens the trace in a local web UI. Step through with `j` and `k`; press `.` to jump to a property violation and see the screenshot, the action, and the failed formula at that step. The [replay page](../replay/) covers the panels and shortcuts.
