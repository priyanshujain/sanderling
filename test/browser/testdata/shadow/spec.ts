import { always, extract, taps } from "@sanderling/spec";

const bumps = extract((s) => {
  const el = s.ax.find({ id: "count" });
  return el ? parseInt(el.text, 10) || 0 : 0;
}).named("bumps");

// The button, the counter that records the taps, and the canvas that services
// them all live inside a shadow root. A host that stops at the shadow boundary
// enumerates no target to tap and resolves no selector to read, so the counter
// can never leave zero: this property firing IS the evidence that both the
// enumeration and the selector lookup crossed the boundary.
const counterNeverMoves = always(() => bumps.current === 0);

export const properties = { counterNeverMoves };

export const actionsRoot = taps;
