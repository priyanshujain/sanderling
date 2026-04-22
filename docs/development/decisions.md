---
title: Decisions
---

# Decisions

Architectural and organizational decisions worth recording. Each entry states the decision and the reasoning.

---

## Directory and Package Organization

### `web/` renamed to `inspect-ui/`

The directory containing the React/TypeScript frontend is `inspect-ui/`, not `web/`. The name `web/` was ambiguous (the project also has a web/Chrome driver target). `inspect-ui/` makes the purpose explicit: this is the UI for the `sanderling inspect` command.

### Keep `internal/`

Go's `internal/` directory restriction prevents any code outside this module from importing these packages. Sanderling is a CLI tool today, but the restriction costs nothing to keep and prevents accidental coupling if the module is ever used as a Go dependency. All implementation packages live under `internal/`.

### `internal/driver/` is an interface + subdirectory implementations

The `driver.go` file defines the `DeviceDriver` interface. Concrete implementations live in subdirectories: `sidecar/` (Maestro gRPC), `chrome/` (CDP), `mock/` (tests). This pattern keeps the runner and verifier decoupled from any specific platform.

### `internal/verifier/marshal.go` moves to `internal/inspect/`

`marshal.go` serializes LTL formulas to JSON for the inspect UI. That is an inspect concern, not a verifier concern. Verifier should not know inspect exists.

### `internal/verifier/bindings.go` splits into `types.go` + `bindings.go`

`bindings.go` currently holds shared types (`Action`, `ActionKind`, `LogEntry`, `Exception`) alongside JavaScript runtime wiring. The types half moves to `types.go` so the two concerns are separately navigable.

### `internal/permissions/` stays as-is

Android-only package but there is no iOS equivalent yet. Revisit if iOS gets similar permission setup.

---

### `cmd/sanderling/android_env.go` moves to `internal/android/`

Android device enumeration, AVD selection, and emulator boot logic moves to `internal/android/`. This keeps `cmd/sanderling/` as a thin CLI wrapper and makes the Android logic independently testable.

### `cmd/sanderling/test_run.go` logic moves to `internal/testrun/`

Driver setup, agent connection, verifier init, trace setup, and runner orchestration extract to `internal/testrun/`. `cmd/sanderling/` wires CLI flags to `testrun` calls and nothing more.

### `internal/inspect/runs.go` splits into multiple files

429 LOC with mixed concerns (cache, file I/O, JSON decoding, summary types) splits into at least `runs_cache.go` and `runs_decode.go` within the same package.

### `cmd/internal-tools/` stays in `cmd/`

`bundle-check` and `hier-check` are dev/debug binaries. Leave them under `cmd/` for now.

### `pkg/spec-api/` renamed to `pkg/spec/`

Aligns the directory name with the npm package name `@sanderling/spec`.
