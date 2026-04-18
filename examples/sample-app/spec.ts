import {
  extract,
  always,
  actions,
  weighted,
  Tap,
  taps,
  swipes,
} from "@uatu/spec";

// ── Snapshot extractors (fed by SampleApplication.kt) ──────────
// See ./android/src/main/kotlin/dev/uatu/sample/SampleApplication.kt
const appState = extract<string>(
  (state) => (state.snapshots.app_state as string) ?? "",
);
const clickCount = extract<number>(
  (state) => (state.snapshots.click_count as number) ?? 0,
);

// ── UI elements ────────────────────────────────────────────────
const clickButton = extract((state) => state.ax.find("text:Click me"));

// ── Properties ─────────────────────────────────────────────────
export const properties = {
  appIsRunning: always(() => appState.current === "running"),
  clickCountNonNegative: always(() => clickCount.current >= 0),
  clickCountNeverDecreases: always(() => {
    const previous = clickCount.previous;
    return previous === undefined || clickCount.current >= previous;
  }),
};

// ── Actions ────────────────────────────────────────────────────
const tapClickMe = actions(() => {
  return clickButton.current ? [Tap({ on: clickButton.current })] : [];
});

export const actionsRoot = weighted(
  [100, tapClickMe],
  [10, taps],
  [2, swipes],
);

(globalThis as { actions?: unknown; properties?: unknown }).actions = actionsRoot;
(globalThis as { properties?: unknown }).properties = properties;
