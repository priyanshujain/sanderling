import { pressKey, swipes, taps, typing, weighted } from "../actions.ts";
import type { ActionGenerator } from "../types.ts";

export { pressKey, swipes, taps, typing, waitOnce } from "../actions.ts";

// A broad exploration generator: tap things, type edge-case values into fields,
// swipe, and occasionally press back. Layer it under targeted depth flows so the
// fuzzer wanders the whole app while still driving the paths an author wrote.
export const defaultActions: ActionGenerator = weighted(
  [50, taps],
  [30, typing],
  [10, swipes],
  [10, pressKey],
);
