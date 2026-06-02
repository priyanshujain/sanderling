import { doubleTaps, scrolls, swipes, taps, typing, weighted } from "../actions.ts";
import type { ActionGenerator } from "../types.ts";

export { doubleTaps, scrolls, swipes, taps, typing } from "../actions.ts";

export const defaultActions: ActionGenerator = weighted(
  [100, taps],
  [100, typing],
  [50, scrolls],
  [25, swipes],
  [10, doubleTaps],
);
