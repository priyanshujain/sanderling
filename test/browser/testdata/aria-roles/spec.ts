import { always, extract, taps } from "@sanderling/spec";

const selections = extract((s) => {
  const el = s.ax.find({ id: "selected" });
  return el ? parseInt(el.text, 10) || 0 : 0;
}).named("selections");

// Every control on the page is an <li role="option">, the shape the replay UI
// gives its own step rows. Nothing else can move this counter, so the spec
// carries no action of its own: this property firing IS the evidence that the
// default enumeration offered a tap on a role-based control.
const noRowWasEverSelected = always(() => selections.current === 0);

export const properties = { noRowWasEverSelected };

export const actionsRoot = taps;
