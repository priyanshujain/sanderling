---
title: Architecture
---

# Architecture

```mermaid
flowchart TB
    subgraph go["sanderling (Go)"]
        direction LR
        B["Bundler / esbuild"] --> V["Verifier / goja + LTL"]
        V <--> R["Runner"]
        R --> D["DeviceDriver"]
        R --> T["Trace writer\nJSONL + PNG"]
    end

    SC["Maestro sidecar (JVM)"]
    CH["Chrome (CDP)"]
    RD[("runs/")]
    IN["sanderling inspect\nHTTP + SSE"]
    UI["Web UI (React)"]

    D -->|gRPC| SC
    D -->|CDP| CH

    T --> RD --> IN --> UI
```

## Processes

**sanderling (Go).** The top-level binary. Bundles the spec with esbuild, evaluates it in goja, runs the main loop, dispatches actions through the `DeviceDriver` interface, writes the trace.

**Maestro sidecar (JVM).** A Kotlin process that wraps `maestro-client` and exposes a gRPC surface matching the `DeviceDriver` interface. Handles UI input, screenshots, the system accessibility tree, and OS-level alerts. Native platforms only.

**Chrome (CDP).** For web targets, the Go binary drives Chrome directly over the Chrome DevTools Protocol. No sidecar is involved.

## Transports

| Channel | Platform | Transport | Purpose |
|---|---|---|---|
| Go to Maestro sidecar | Native | gRPC (localhost TCP) | UI input, screenshots, system alerts |
| Go to Chrome | Web | Chrome DevTools Protocol | UI input, screenshots, DOM hierarchy, console logs |

On native, the transport split exists because only real UI events need to cross process and OS-API boundaries. Introspection is cheap, frequent, and lives on a fast local socket directly to the app. On web, CDP handles both.

## Inspect UI

`sanderling inspect` is a separate mode of the same Go binary. It serves an embedded React bundle and reads `runs/` from disk, streaming file-watcher events over SSE so the UI updates as new steps land. It has no connection to any driver; it only consumes the trace artifacts.

## Per-step cycle

The heart of the system is:

```
fetch state  ─►  evaluate properties  ─►  pick action  ─►  dispatch
```

**Native (Android / iOS):**

1. The runner asks the driver to wait until the UI is idle.
2. The runner fetches the UI hierarchy and logs from the sidecar.
3. The runner feeds state into goja. Extractors re-read; properties re-evaluate; the action generator returns a weighted tree.
4. The runner writes the trace entry for this step.
5. The runner picks an action by weight and dispatches it through the driver (gRPC to sidecar -> Maestro -> UIAutomator or XCTest).
6. Loop.

**Web (Chrome):**

CDP captures the DOM hierarchy and console logs directly. The rest of the cycle is identical.

The cycle runs hundreds of times per minute. Every step produces one row in `trace.jsonl` and one screenshot.

