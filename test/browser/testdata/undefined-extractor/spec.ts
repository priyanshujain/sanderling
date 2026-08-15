import { always, extract, taps } from "@sanderling/spec";

// Two extractors that resolve nothing, the shape every route-scoped extractor
// has on the steps it is off its own screen (folio has nine of them). JSON has
// no undefined, so these are what the {value} envelope exists for.
const absent = extract((s) => s.ax.find({ id: "nothing-is-ever-here" })).named("absent");
const absentText = extract((s) => s.ax.find({ id: "missing" })?.text).named("absentText");

const bumps = extract((s) => {
  const el = s.ax.find({ id: "count" });
  return el ? parseInt(el.text, 10) || 0 : 0;
}).named("bumps");

// A reading the page could not take must arrive as undefined, not as null and
// not as goja's reading of the dump. This property is what a spec sees.
const undefinedStaysUndefined = always(
  () => absent.current === undefined && absentText.current === undefined,
);

// The run has to actually be doing something, or the property above holds for
// the wrong reason.
const counterNeverMoves = always(() => bumps.current === 0);

export const properties = { undefinedStaysUndefined, counterNeverMoves };

export const actionsRoot = taps;
