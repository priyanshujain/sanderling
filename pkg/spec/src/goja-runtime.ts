// Goja-side runtime entry for the native verifier (internal/verifier).
//
// Go installs globalThis.__sanderlingHost__ (platform/seed/queryTargets/
// reportUnsupported, implemented over the hierarchy tree in bindings.go) before
// the spec evaluates. This module bundles AFTER the spec, reads that host, and
// wires the shared picker via installRuntime so the goja verifier and the V8 web
// runtime run the SAME pick.ts over the SAME Pcg.
//
// Extractors are driven by Go (bindExtract + PushSnapshot advance current/
// previous against the Go-built `state`), so the extractor callback is a no-op
// here; Go never invokes __sanderlingExtractors__ on native.

import { installRuntime } from "./runtime-entry.ts";
import type { GeneratorNode, Host } from "./action-tree.ts";

const host = (globalThis as { __sanderlingHost__?: Host }).__sanderlingHost__;
if (!host) throw new Error("goja runtime: __sanderlingHost__ not installed");

installRuntime(
  host,
  () => (globalThis as { actions?: GeneratorNode }).actions ?? null,
  () => ({}),
);
