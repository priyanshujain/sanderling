// Cross-runtime parity harness. Run under node (tsx) by the Go test
// internal/verifier/parity_test.go, which feeds the SAME seed and candidate
// list into the goja verifier and asserts an identical action stream. Both
// engines run the SAME pick.ts over the SAME Pcg, so any divergence here is an
// engine-level (number/bigint) parity bug.
//
// Inputs come from env so the Go side controls them:
//   SANDERLING_PARITY_SEED       decimal uint64 seed
//   SANDERLING_PARITY_STEPS      action count to emit
//   SANDERLING_PARITY_CANDIDATES JSON array of {x,y,selector,width,height}
//
// Output: one JSON array of serialized actions on stdout.

import { installRuntime } from "../src/runtime-entry.ts";
import type { BuiltinVerb, Candidate, GeneratorNode, Host } from "../src/action-tree.ts";

const seed = BigInt(process.env.SANDERLING_PARITY_SEED ?? "0");
const steps = Number(process.env.SANDERLING_PARITY_STEPS ?? "0");
const candidates = JSON.parse(process.env.SANDERLING_PARITY_CANDIDATES ?? "[]") as Candidate[];

const host: Host = {
  platform: () => "android",
  queryCandidates: (_verb: BuiltinVerb) => candidates,
  reportUnsupported: () => {},
  seedHi: () => seed,
  seedLo: () => 0n,
};

const root: GeneratorNode = { kind: "builtin", verb: "taps" };

installRuntime(host, root, () => ({}));

const nextAction = (globalThis as { __sanderlingNextAction__: () => unknown })
  .__sanderlingNextAction__;
const stream = [];
for (let i = 0; i < steps; i++) stream.push(nextAction());
process.stdout.write(JSON.stringify(stream));
