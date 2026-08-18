# Folio

A minimal Kotlin Multiplatform personal-ledger app: login with demo
credentials, create accounts, add credits and debits. Shared UI across
Android, iOS, and web (wasmJs via Compose for Web). Doubles as the
example sanderling runs its property-based specs against.

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
into the SwiftUI host, uninstalls any previous copy, installs, and launches.
The uninstall matters: folio's signed-in session survives an install over the
top, so without it a run opens on the last run's Home screen.

## Web

```sh
just web         # webpack dev server with COOP/COEP headers
just web-build   # produce a webpack distributable bundle
```

`just web` runs `:app:webApp:wasmJsBrowserDevelopmentRun --continuous`, so
edits to shared code reload in the browser.

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

The device does not have to be attached to this machine. `ADB_SERVER_SOCKET`
aims adb at another host's adb server, and `ANDROID_DEVICE` names the serial
that server reports, which is also what routes the install when the server has
more than one device online:

```
ADB_SERVER_SOCKET=tcp:10.0.0.5:5037
ANDROID_DEVICE=emulator-5556
```

Gradle only assembles the APK. The install goes through adb, which reads those
variables, so a remote server needs nothing else. Gradle's own `installDebug`
cannot be used here: its adb client only ever dials loopback.

Traces land in `./sanderling/runs/<timestamp>/`.

## Run with the LLM action generator

The same `sanderling/spec.ts` runs under either generator: `--generator seeded`
(the default weighted fuzzer) or `--generator llm`, where a vision model picks
from the SAME weighted candidate set (reading the screenshot plus a numbered,
weight-annotated list of concrete actions) and returns one number. The spec's
`generator = llm({ model, instructions })` export configures it.

```sh
export OPENROUTER_API_KEY=sk-or-...   # or OPENAI_API_KEY=sk-... for OpenAI direct
just test-llm                         # or: sanderling test --generator llm --spec sanderling/spec.ts --bundle-id app.folio
```

OpenRouter wins when both keys are set. With a plain OpenAI key, drop the vendor
prefix from the model id in `spec.ts` (`gpt-5.4-nano`, not
`openai/gpt-5.4-nano`). The model must support image input **and** strict
`json_schema` structured outputs. Each step is one multimodal call, so keep the
duration / step budget modest. The trace records the model's reasoning, the
chosen number, and `source: "llm"` on each action, so the replay UI shows why
each pick was made.

## Run a sanderling test (iOS)

```sh
just test-ios                          # default simulator: iPhone 17 Pro
IOS_DEVICE="iPhone 15" just test-ios   # pick a different simulator
```

`just test-ios` boots the simulator if needed, runs `just ios` to install
and launch the app, then invokes `sanderling test --platform ios`. Same
`DURATION`, `SEED`, and `OUTPUT` env vars as the Android target.

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
