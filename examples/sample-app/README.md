# uatu sample app

Tiny Android app wired to the uatu SDK, plus a TypeScript spec that drives it.
Use it as a reference for integrating uatu into your own app.

## Prerequisites

- `uatu` CLI on `PATH` (see [getting started](https://priyanshujain.github.io/uatu/manual/getting-started.html))
- Android SDK installed (uatu auto-discovers `adb` and `emulator` under
  `$ANDROID_HOME`, `~/Library/Android/sdk`, or the Homebrew cask; nothing to
  export if you use a standard install)
- `just` task runner

## Install the app

```sh
just install
```

## Run a test

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

## How the pieces connect

- `android/build.gradle.kts` depends on `io.github.priyanshujain:sdk-android` from
  Maven Central
- `android/src/main/kotlin/.../SampleApplication.kt` calls `Uatu.start(this)` and
  registers snapshot extractors (`app_state`, `click_count`)
- `spec.ts` imports `@uatu/spec` (see `package.json`), reads those snapshots,
  asserts properties on them, and weights the actions the fuzzer picks from
- `just test` invokes `uatu test` against the installed APK on the connected device (or the AVD named via `AVD=`)
