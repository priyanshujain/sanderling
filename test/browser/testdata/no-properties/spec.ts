import { extract, taps } from "@sanderling/spec";

export const counter = extract((s) => {
  const el = s.ax.find({ id: "counter" });
  return el ? parseInt(el.text, 10) || 0 : 0;
}).named("counter");

export const properties = {};

export const actionsRoot = taps;
