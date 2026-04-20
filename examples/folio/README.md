# uatu sample app

A minimal Kotlin Multiplatform ledger app that mirrors the React reference:
login with demo credentials, create accounts, add credits and debits. Same
features, monospace look, and demo creds, shared across Android and iOS via
Compose Multiplatform.

## Stack

- Kotlin Multiplatform + Compose Multiplatform (shared UI)
- kotlinx.serialization for file-backed persistence
- kotlinx.coroutines for state flows
- uatu `sdk-android` for harness integration on Android

Everything in `composeApp/src/commonMain/kotlin/dev/uatu/sample/` is shared
between platforms. Platform-specific I/O (file storage, clock, UUID) lives
in `androidMain/` and `iosMain/` as `actual`s of the `Platform` expect object.

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
email:    demo@ledger.app
password: ledger123
```

## Run a uatu test (Android)

```sh
just test
```

If no device is connected, uatu boots the single AVD it finds. With multiple
AVDs, pick one:

```sh
just AVD=Pixel_7 test
```

Persistent settings can live in `.env` alongside the justfile:

```
AVD=Pixel_7
DURATION=5m
```

Traces land in `./runs/<timestamp>/`.

## Layout

```
composeApp/
  src/commonMain/kotlin/dev/uatu/sample/   shared domain, state, UI
  src/androidMain/                         Android Application + Activity
  src/iosMain/                             iOS UIViewController entry
iosApp/
  project.yml                              xcodegen spec
  iosApp/iOSApp.swift                      SwiftUI host
  iosApp/Info.plist
justfile
spec.ts                                    uatu test spec
```

## How it connects to uatu

- `composeApp/src/androidMain/.../FolioApplication.kt` calls `Uatu.start(this)`
  and registers snapshot extractors (`logged_in`, `account_count`,
  `total_balance`, `route`)
- `spec.ts` imports `@uatu/spec`, reads those snapshots, asserts properties,
  and weights the actions the fuzzer picks from
- `just test` invokes `uatu test` against the installed APK
