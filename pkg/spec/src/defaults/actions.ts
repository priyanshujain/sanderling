import { doubleTaps, scrolls, swipes, taps, typing, weighted } from "../actions.ts";
import type { ActionGenerator } from "../types.ts";

export { doubleTaps, pressKeys, scrolls, swipes, taps, typing, waitOnce } from "../actions.ts";
// longPresses is an opt-in generator: not part of defaultActions, but authors
// can include it in their own weighted() set.
export { longPresses } from "../actions.ts";

// Broad exploration: tap, type edge-case values, scroll to reveal content, and
// swipe. Layer it under targeted flows so the fuzzer wanders the whole app while
// still driving the paths an author wrote. It omits the hardware back key:
// backing out past the app's root exits the app under test, and exploration must
// stay inside it.
//
// Weights are relative integers, not percentages: the picker scales each draw by
// their total, so only the ratios matter and any one can be retuned on its own.
// Tap and type are co-primary (typing variety is what exercises input
// validation), scroll is the major reveal behavior at half a primary, swipe is a
// secondary gesture, and doubleTaps is rare but kept because it is the only
// default source of sub-100ms event spacing, which keeps race-window bugs
// (double-submit, init races) reachable.
export const defaultActions: ActionGenerator = weighted(
  [100, taps],
  [100, typing],
  [50, scrolls],
  [25, swipes],
  [10, doubleTaps],
);
