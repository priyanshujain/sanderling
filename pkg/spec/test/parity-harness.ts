// Shared cross-runtime parity scenario: the FIXED seed, FIXED candidate list,
// and FIXED action root that both the node test (parity.test.ts) and the goja
// test (internal/verifier/parity_test.go) drive. Each side runs the SAME
// pick.ts over the SAME Pcg and asserts the SAME committed golden
// (fixtures/parity-golden.json); matching one golden on both sides proves the
// two engines agree without either invoking the other.
//
// The candidate ORDER and the per-tick PCG draw order are the parity contract.
// The weighted root mixes a tap branch (1 candidate draw) with a typing branch
// (1 candidate draw + 1 corpus draw), so a tick exercises weighted selection, a
// builtin, and the input corpus together; reordering candidates or adding or
// dropping a draw on either side shifts the stream and fails the golden.

import { Pcg } from "../src/pcg.ts";
import { nextAction } from "../src/pick.ts";
import { serializeAction, type SerializedAction } from "../src/runtime-entry.ts";
import { taps, typing, weighted } from "../src/actions.ts";
import type { BuiltinVerb, Candidate, GeneratorNode, Host } from "../src/action-tree.ts";

export const PARITY_SEED_HI = 0x9e3779b97f4a7c15n;
export const PARITY_STEPS = 20;

export const PARITY_CANDIDATES: Candidate[] = [
  { x: 50, y: 60, selector: "id:alpha", width: 100, height: 40 },
  { x: 150, y: 160, selector: "id:beta", width: 120, height: 48 },
  { x: 250, y: 260, selector: "id:gamma", width: 80, height: 32 },
];

// A 3:1 weighted split over taps and typing. The stub host returns the same
// candidate list for every verb so the only variables are the draw order and
// the JS engine's number/bigint behavior.
export const PARITY_ROOT: GeneratorNode = weighted([3, taps], [1, typing]);

const HOST: Host = {
  platform: () => "android",
  queryCandidates: (_verb: BuiltinVerb) => PARITY_CANDIDATES,
  reportUnsupported: () => {},
  seedHi: () => PARITY_SEED_HI,
  seedLo: () => 0n,
};

// runParity emits the parity scenario's action stream from a fresh Pcg. It
// drives pick.ts directly (not installRuntime, whose globals are locked once)
// so the caller can run it repeatedly to assert determinism.
export function runParity(): (SerializedAction | null)[] {
  const rng = new Pcg(PARITY_SEED_HI, 0n);
  const stream: (SerializedAction | null)[] = [];
  for (let i = 0; i < PARITY_STEPS; i++) {
    stream.push(serializeAction(nextAction(PARITY_ROOT, rng, HOST)));
  }
  return stream;
}
