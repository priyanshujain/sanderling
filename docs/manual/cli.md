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
| `--avd` | required (android) | Android AVD name. |
| `--duration` | `5m` | Total test duration (`30s`, `5m`, `2h`, `1d`). |
| `--seed` | `0` | PRNG seed. `0` uses a random seed and records it in `meta.json`. |
| `--output` | `./runs` | Output directory for traces. |

## `uatu doctor`

Check the host environment for a working uatu setup: Go toolchain, JDK, Maestro availability, emulator reachability, SDK linkage hints.

## `uatu version`

Print the CLI version.

## Flags coming in v0.1.0

- `--permissions` to pre-set OS-level permissions (for example `--permissions location=allow,notifications=deny`).
- `--max-steps` hard cap on step count.
- `--exit-on-violation` stop the run on the first property violation.
- `uatu inspect` command for browsing traces in the built-in UI.

Tracked in [issue #4](https://github.com/priyanshujain/uatu/issues/4).
