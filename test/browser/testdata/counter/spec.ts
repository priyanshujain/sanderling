import { always, extract, taps } from "@sanderling/spec";

const counter = extract((s) => {
  const el = s.ax.find({ id: "counter" });
  return el ? parseInt(el.text, 10) || 0 : 0;
}).named("counter");

// The + and - buttons each move the counter by exactly 1, so no step ever
// changes it by more than 1. A bug that double-applied a click would break this.
const changesByAtMostOne = always(() => {
  const previous = counter.previous;
  if (previous === undefined) return true;
  return Math.abs(counter.current - previous) <= 1;
});

export const properties = { changesByAtMostOne };

export const actionsRoot = taps;
