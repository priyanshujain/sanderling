---
title: CLI reference
---

# CLI reference

```
uatu <command> [flags]
```

## `uatu test`

Run a spec against an app for a fixed duration.

| Flag | Default | Description |
|---|---|---|
| `--spec` | required | Path to the TypeScript spec. |
| `--bundle-id` | required | Target app bundle ID (Android: applicationId). |
| `--launcher-activity` | resolved | Optional `<pkg>/<activity>` to launch. Overrides default resolution. |
| `--platform` | `android` | Target platform. Only `android` in the current alpha. |
| `--avd` | optional (android) | Android AVD name to boot if no device is connected. Required only when no device is connected and multiple AVDs exist. |
| `--duration` | `5m` | Total test duration (`30s`, `5m`, `2h`, `1d`). |
| `--seed` | `0` | PRNG seed. `0` uses a random seed and records it in `meta.json`. |
| `--output` | `./runs` | Output directory for traces. |

## `uatu inspect [run-or-runs-dir]`

Serve a local web UI for browsing traces. The positional argument is optional and may point at either a runs directory (the parent of many runs) or a single run directory (auto-detected by the presence of `meta.json`). Defaults to `./runs`.

| Flag | Default | Description |
|---|---|---|
| `--port` | `0` (ephemeral) | TCP port to listen on. |
| `--no-open` | `false` | Skip opening the default browser on startup. |
| `--dev` | `false` | Reverse-proxy non-API requests to the Vite dev server on `127.0.0.1:5173`. |

See [the inspect UI page](inspect.md) for the panel reference and keyboard shortcuts.

## `uatu doctor`

Check the host environment for a working uatu setup: Go toolchain, JDK, Maestro availability, emulator reachability, SDK linkage hints.

## `uatu version`

Print the CLI version.

## Flags coming in v0.1.0

- `--permissions` to pre-set OS-level permissions (for example `--permissions location=allow,notifications=deny`).
- `--max-steps` hard cap on step count.
- `--exit-on-violation` stop the run on the first property violation.

Tracked in [issue #4](https://github.com/priyanshujain/uatu/issues/4).
