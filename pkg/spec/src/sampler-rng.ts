// samplerRng is the picker's Pcg while it evaluates an `actions` node's
// generator (set by pick.ts walkActions). Author sampling, from(...).generate()
// and the values.ts generators, draws from it so every sample shares the single
// deterministic stream. It is null outside a walk; eager spec-time generate()
// calls then fall back to a fixed deterministic default.

import type { Pcg } from "./pcg.ts";

let samplerRng: Pcg | null = null;

export function setSamplerRng(rng: Pcg | null): void {
  samplerRng = rng;
}

export function getSamplerRng(): Pcg | null {
  return samplerRng;
}

// enumeratingCandidates is set while the Go host enumerates the authored leaves
// for the model policy (internal/verifier/llm.go collectActions). That walk runs
// outside the picker's rng scope, so a draw there would silently collapse to
// item 0.
let enumeratingCandidates = false;

export function setEnumeratingCandidates(enumerating: boolean): void {
  enumeratingCandidates = enumerating;
}

// SAMPLER_REFUSAL_NAME marks the refusal below so the Go host can tell it from
// any other error a spec's generator throws, which it still tolerates by
// skipping the leaf.
export const SAMPLER_REFUSAL_NAME = "SanderlingSamplerRefusal";

// refuseWhileEnumerating stops a draw the model policy cannot make. Collapsing
// to item 0 instead would offer the model one fixed target while the seeded
// picker reaches every item, and a comparison between the two policies would
// then be measuring the sampler rather than the policies.
export function refuseWhileEnumerating(itemCount: number): void {
  if (!enumeratingCandidates) return;
  const refusal = new Error(
    `draws 1 of ${itemCount} sampled items, which only the seeded picker can do: ` +
      "the model policy would be offered the first item every time. " +
      "Return one action per item instead of calling generate().",
  );
  refusal.name = SAMPLER_REFUSAL_NAME;
  throw refusal;
}
