import { swipes, taps, typing, weighted } from "../actions.ts";
import type { ActionGenerator } from "../types.ts";

export { pressKey, swipes, taps, typing, waitOnce } from "../actions.ts";

// A broad exploration generator: tap things, type edge-case values into fields,
// and swipe. Layer it under targeted depth flows so the fuzzer wanders the whole
// app while still driving the paths an author wrote. It deliberately omits the
// hardware back key: backing out past the app's root screen exits the app under
// test, and exploration must stay within the app.
export const defaultActions: ActionGenerator = weighted(
  [55, taps],
  [30, typing],
  [15, swipes],
);
