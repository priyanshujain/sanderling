# Folio

A minimal Kotlin Multiplatform personal-ledger app: login with demo
credentials, create accounts, add credits and debits. Shared UI across
Android, iOS, and Web via Compose Multiplatform. Doubles as the example
sanderling runs its property-based specs against.

## Stack

- Kotlin Multiplatform + Compose Multiplatform (shared UI)
- kotlinx.serialization for file-backed persistence
- kotlinx.coroutines for state flows
- sanderling `sdk-android` for harness integration on Android

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

`just ios` regenerates `iosApp/iosApp.xcodeproj` from `iosApp/project.yml`,
builds the KMP framework, links it into the SwiftUI host, installs, and
launches.

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

- `composeApp/src/androidMain/.../FolioApplication.kt` calls `Sanderling.start(this)`
  and registers snapshot extractors (`logged_in`, `account_count`,
  `total_balance`, `route`)
- `sanderling/spec.ts` imports `@sanderling/spec`, reads those snapshots, asserts
  properties, and weights the actions the fuzzer picks from
- `just test` invokes `sanderling test` against the installed APK
