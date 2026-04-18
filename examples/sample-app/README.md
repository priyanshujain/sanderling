# uatu sample app

Tiny Android app wired to the uatu SDK, plus a TypeScript spec that drives it.
Use it as a reference for integrating uatu into your own app.

## Prerequisites

- `uatu` CLI on `PATH` (see [getting started](https://priyanshujain.github.io/uatu/manual/getting-started.html))
- Android SDK with `adb` and `emulator` on `PATH`
- An emulator/device (API 24+) running, or an AVD name to boot one
- `just` task runner

## Install the app

```sh
just install
```

## Run a test

With a device already connected:

```sh
just test
```

Or have uatu boot an AVD for you:

```sh
just AVD=Pixel_7 test
```

Traces land in `./runs/<timestamp>/`.

## How the pieces connect

- `android/build.gradle.kts` depends on `io.github.priyanshujain:sdk-android` from
  Maven Central
- `android/src/main/kotlin/.../SampleApplication.kt` calls `Uatu.start(this)` and
  registers snapshot extractors (`app_state`, `click_count`)
- `spec.ts` imports `@uatu/spec` (see `package.json`), reads those snapshots,
  asserts properties on them, and weights the actions the fuzzer picks from
- `just test` invokes `uatu test` against the installed APK on the named AVD
