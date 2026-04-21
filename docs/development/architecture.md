---
title: Architecture
---

# Architecture

Three processes, two transports.

```mermaid
flowchart TB
    subgraph Go["sanderling (Go)"]
        Bundler[Bundler<br/>esbuild] --> Verifier[Verifier<br/>goja + LTL]
        Verifier <--> Runner[Runner]
        Runner --> Trace[Trace writer<br/>JSONL + PNG]
        Runner <--> Driver[Driver iface]
    end

    subgraph Sidecar["Maestro Sidecar (JVM)"]
        Maestro[maestro-client]
    end

    subgraph Device["Emulator"]
        subgraph App["Android app (debug)"]
            SDK[sanderling-sdk<br/>pause / hierarchy<br/>logs / coverage]
        end
    end

    Driver -- gRPC --> Maestro
    Maestro -- UIAutomator --> App
    Runner -- Unix socket --> SDK
    Trace --> Runs[(runs/)]
```

## Processes

**sanderling (Go).** The top-level binary. Bundles the spec with esbuild, evaluates it in goja, runs the main loop, dispatches actions through the driver, writes the trace.

**Maestro sidecar (JVM).** A Kotlin process that wraps `maestro-client` and exposes a gRPC surface matching the `driver.Driver` interface. Handles UI input, screenshots, the system accessibility tree, and OS-level alerts.

**In-app SDK.** A Kotlin (or Swift for iOS) library linked into the app under test. Exposes a Unix socket to the runner. Provides pause and resume, view-hierarchy dumps, coverage reads, log capture, and user-registered state extractors.

## Transports

| Channel | Transport | Purpose |
|---|---|---|
| Go to Maestro sidecar | gRPC (localhost TCP) | UI input, screenshots, system alerts |
| Go to in-app SDK | Unix domain socket | Pause / resume, hierarchy, coverage, logs, extractors |

The split exists for one reason: only real UI events need the cost of crossing process and OS-API boundaries. Introspection is cheap, frequent, and lives on a fast local socket directly to the app.

## Per-step cycle

The heart of the system is:

```
pause  ─►  capture state  ─►  evaluate properties  ─►  pick action  ─►  resume  ─►  dispatch
```

1. The runner asks the driver to wait until the UI is idle.
2. The runner sends `PAUSE` to the SDK over the agent socket. The SDK freezes the main runloop at a safe point.
3. The SDK sends back a `STATE` message: view hierarchy, coverage delta, logs since last step, exception list, snapshot values.
4. The runner feeds state into goja. Extractors re-read; properties re-evaluate; the action generator returns a weighted tree.
5. The runner writes the trace entry for this step.
6. The runner picks an action by weight.
7. The runner sends `RESUME` to the SDK, then dispatches the action through the driver (gRPC to sidecar, which talks to Maestro, which talks to UIAutomator or XCTest).
8. Loop.

The cycle runs hundreds of times per minute. Every step produces one row in `trace.jsonl` and one screenshot.

