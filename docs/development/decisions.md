---
title: Decisions
---

# Decisions

Architectural and organizational decisions worth recording. Each entry states the decision and the reasoning.

---

## Directory and Package Organization

### `web/` renamed to `replay-ui/`

The directory containing the React/TypeScript frontend is `replay-ui/`, not `web/`. The name `web/` was ambiguous (the project also has a web/Chrome driver target). `replay-ui/` makes the purpose explicit: this is the UI for the `sanderling replay` command.

### Keep `internal/`

Go's `internal/` directory restriction prevents any code outside this module from importing these packages. Sanderling is a CLI tool today, but the restriction costs nothing to keep and prevents accidental coupling if the module is ever used as a Go dependency. All implementation packages live under `internal/`.

### `internal/driver/` is an interface + subdirectory implementations

The `driver.go` file defines the `DeviceDriver` interface. Concrete implementations live in subdirectories: `sidecar/` (gRPC to the native sidecar), `chrome/` (CDP), `mock/` (tests). This pattern keeps the runner and verifier decoupled from any specific platform.

### `internal/verifier/marshal.go` stays in `internal/verifier/`

This was recorded as a move to `internal/replay/` on the grounds that serializing LTL formulas for the replay UI is a replay concern. The move never happened, and the reason it should not is that `marshal.go` is now the single decoder both hosts read the action wire through, which is a verifier concern: splitting it would put the wire contract and the evaluator that depends on it in different packages.

### `internal/verifier/bindings.go` splits into `types.go` + `bindings.go`

`bindings.go` currently holds shared types (`Action`, `ActionKind`, `LogEntry`, `Exception`) alongside JavaScript runtime wiring. The types half moves to `types.go` so the two concerns are separately navigable.

### `internal/permissions/` is deleted

Dead code with zero importers. Removed. Reintroduce a permission helper only when a platform actually needs one.

---

### `cmd/sanderling/android_env.go` moves to `internal/android/`

Android device enumeration, AVD selection, and emulator boot logic moves to `internal/android/`. This keeps `cmd/sanderling/` as a thin CLI wrapper and makes the Android logic independently testable.

### `cmd/sanderling/test_run.go` logic moves to `internal/testrun/`

Driver setup, agent connection, verifier init, trace setup, and runner orchestration extract to `internal/testrun/`. `cmd/sanderling/` wires CLI flags to `testrun` calls and nothing more.

### `internal/replay/runs.go` splits into multiple files

Done. The mixed concerns (cache, file I/O, JSON decoding, summary types) now sit in `runs_cache.go` and `runs_decode.go` beside `runs.go`.

### `cmd/internal-tools/` stays in `cmd/`

`bundle-check` and `hier-check` are dev/debug binaries. Leave them under `cmd/` for now.

### `pkg/spec-api/` renamed to `pkg/spec/`

Aligns the directory name with the npm package name `@sanderling/spec`.
