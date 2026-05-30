import { doubleTaps, swipes, taps, typing, weighted } from "../actions.ts";
import type { ActionGenerator } from "../types.ts";

export { doubleTaps, pressKey, swipes, taps, typing, waitOnce } from "../actions.ts";

// A broad exploration generator: tap things, type edge-case values into fields,
// and swipe. Layer it under targeted depth flows so the fuzzer wanders the whole
// app while still driving the paths an author wrote. It deliberately omits the
// hardware back key: backing out past the app's root screen exits the app under
// test, and exploration must stay within the app.
//
// doubleTaps fires the same target twice ~50ms apart inside one step. Real users
// double-tap (image zoom, like-to-favorite, play/pause); without this the fuzzer
// can never produce sub-100ms event spacing, so race-window bugs (debounce gaps,
// in-flight guards, init races) stay unreachable.
export const defaultActions: ActionGenerator = weighted(
  [45, taps],
  [15, doubleTaps],
  [25, typing],
  [15, swipes],
);
