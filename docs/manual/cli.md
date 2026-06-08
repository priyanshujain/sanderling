---
title: CLI reference
---

# CLI reference

```
sanderling <command> [flags]
```

## `sanderling test`

Run a spec against an app for a fixed duration.

| Flag | Default | Description |
|---|---|---|
| `--spec` | required | Path to the TypeScript spec. |
| `--bundle-id` | required | Target app bundle ID (Android: applicationId). |
| `--launcher-activity` | resolved | Optional `<pkg>/<activity>` to launch. Overrides default resolution. |
| `--platform` | `android` | Target platform: `android`, `ios`, or `web`. |
| `--avd` | optional (android) | Android AVD name to boot if no device is connected. Required only when no device is connected and multiple AVDs exist. |
| `--ios-device` | optional (ios) | iOS target: a simulator name/UDID to boot, or a connected device's name, UDID, or CoreDevice id. |
| `--ios-app-path` | optional (ios) | Path to the `.app` bundle for clear-state reinstall (simulator via `simctl`, device via `devicectl`). |
| `--duration` | `5m` | Total test duration (`30s`, `5m`, `2h`, `1d`). |
| `--seed` | `0` | PRNG seed. `0` uses a random seed and records it in `meta.json`. |
| `--output` | `./runs` | Output directory for traces. |
| `--clear-data` | `true` | Clear app data before launching so the run starts from a fresh install. Pass `--clear-data=false` to resume prior state. |

## `sanderling replay [run-or-runs-dir]`

Serve a local web UI for browsing traces. The positional argument is optional and may point at either a runs directory (the parent of many runs) or a single run directory (auto-detected by the presence of `meta.json`). Defaults to `./runs`.

| Flag | Default | Description |
|---|---|---|
| `--port` | `0` (ephemeral) | TCP port to listen on. |
| `--no-open` | `false` | Skip opening the default browser on startup. |
| `--dev` | `false` | Reverse-proxy non-API requests to the Vite dev server on `127.0.0.1:5173`. |

See [the replay UI page](./replay/) for the panel reference and keyboard shortcuts.

## `sanderling doctor`

Check the host environment for a working sanderling setup.

```
sanderling doctor [--platform web|android|ios|ios-device|all]
```

`--platform` defaults to `all`, which runs every platform's checks (deduped). Pass a specific platform to scope the output.

| Platform | Checks |
|---|---|
| `web` | headless Chromium can launch (the bundled CDP surface boots a real browser). |
| `android` | `adb` on PATH; `emulator` on PATH or under `ANDROID_HOME`; Java 17+; embedded native sidecar JAR is real. |
| `ios` | `xcrun` on PATH; `simctl` on PATH. The simulator path drives the native companion with no JVM. |
| `ios-device` | the `ios` checks plus `devicectl`; `iproxy` on PATH; a connected, paired device; App Store Connect signing credentials present. |

## `sanderling version`

Print the CLI version.

## Flags coming in v0.1.0

- `--permissions` to pre-set OS-level permissions (for example `--permissions location=allow,notifications=deny`).
- `--max-steps` hard cap on step count.
- `--exit-on-violation` stop the run on the first property violation.

Tracked in [issue #4](https://github.com/priyanshujain/sanderling/issues/4).
