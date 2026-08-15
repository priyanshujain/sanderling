import { always, extract, taps } from "@sanderling/spec";
import { noLogcatErrors } from "@sanderling/spec/defaults/properties";

const presses = extract((s) => {
  const el = s.ax.find({ id: "count" });
  return el ? parseInt(el.text, 10) || 0 : 0;
}).named("presses");

// The run has to actually be driving the page, or noLogcatErrors staying
// satisfied says nothing about whether it can fire at all.
const counterNeverMoves = always(() => presses.current === 0);

export const properties = { noLogcatErrors, counterNeverMoves };

export const actionsRoot = taps;
