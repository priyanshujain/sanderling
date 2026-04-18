import {
  extract,
  always,
  actions,
  weighted,
  Tap,
  InputText,
  taps,
  swipes,
} from "@uatu/spec";

// ── Snapshot extractors (fed by SampleApplication.kt) ──────────
// See ./android/src/main/kotlin/dev/uatu/sample/SampleApplication.kt
const clickCount = extract<number>(
  (state) => (state.snapshots.click_count as number) ?? 0,
);
const username = extract<string>(
  (state) => (state.snapshots.username as string) ?? "",
);

// ── UI elements ────────────────────────────────────────────────
const clickButton = extract((state) => state.ax.find("text:Click me"));
const usernameField = extract((state) => state.ax.find("desc:username_field"));

// ── Properties ─────────────────────────────────────────────────
export const properties = {
  clickCountNonNegative: always(() => clickCount.current >= 0),
  clickCountNeverDecreases: always(() => {
    const previous = clickCount.previous;
    return previous === undefined || clickCount.current >= previous;
  }),
  usernameNeverShrinks: always(() => {
    const previous = username.previous;
    return previous === undefined || username.current.length >= previous.length;
  }),
};

// ── Actions ────────────────────────────────────────────────────
const tapClickMe = actions(() => {
  return clickButton.current ? [Tap({ on: clickButton.current })] : [];
});

const typeUsername = actions(() => {
  return usernameField.current
    ? [InputText({ into: usernameField.current, text: "alice" })]
    : [];
});

export const actionsRoot = weighted(
  [50, tapClickMe],
  [50, typeUsername],
  [10, taps],
  [2, swipes],
);

(globalThis as { actions?: unknown; properties?: unknown }).actions = actionsRoot;
(globalThis as { properties?: unknown }).properties = properties;
