# Folio

A minimal Kotlin Multiplatform personal-ledger app: login with demo
credentials, create accounts, add credits and debits. Shared UI across
Android and iOS via Compose Multiplatform. Doubles as the example
sanderling runs its property-based specs against.

## Stack

- Kotlin Multiplatform + Compose Multiplatform (shared UI)
- SQLDelight for the data layer (unified across platforms)
- kotlinx.coroutines for state flows
- kotlinx.serialization for `@Serializable` route types

## Prerequisites

- `just`
- JDK 17
- Android SDK (auto-discovered under `$ANDROID_HOME`, `~/Library/Android/sdk`,
  or the Homebrew cask)
- Xcode 16+ and `xcodegen` (`brew install xcodegen`) for iOS

## Android

```sh
just install      # build + install on a booted emulator / device
just uninstall
just clean
```

## iOS

```sh
just ios                          # default device: iPhone 17 Pro
IOS_DEVICE="iPhone 15" just ios   # pick a different simulator
```

`just ios` regenerates `app/iosApp/iosApp.xcodeproj` from `app/iosApp/project.yml`,
builds the KMP framework (`Shared.framework` from `:app:shared`), links it
into the SwiftUI host, installs, and launches.

## Demo credentials

```
email:    demo@folio.app
password: ledger123
```

## Run a sanderling test (Android)

```sh
just test
```

If no device is connected, sanderling boots the single AVD it finds. With multiple
AVDs, pick one:

```sh
AVD=Pixel_7 just test
```

Persistent settings can live in `.env` alongside the justfile:

```
AVD=Pixel_7
DURATION=5m
```

Traces land in `./sanderling/runs/<timestamp>/`.

## How it connects to sanderling

- Each screen sets a stable Compose `testTag` (`HomeScreen`, `AccountCard`,
  `LedgerRow`, `TxnAmount`, ...). The Sanderling SDK resolves `testTag` to
  `resource-id` on Android and `accessibilityIdentifier` on iOS.
- Identity for list items is the visible text content (account name; txn
  note + amount). No synthetic IDs encoded in semantics.
- `contentDescription` is reserved for real accessibility labels, never as a
  data carrier.
- `sanderling/spec.ts` imports `@sanderling/spec`, reads state via `s.ax.*`,
  asserts properties, and weights the actions the fuzzer picks from.
- `just test` invokes `sanderling test` against the installed APK.
