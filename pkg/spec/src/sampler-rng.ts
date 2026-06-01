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
