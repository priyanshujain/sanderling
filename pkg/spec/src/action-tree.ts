// Shared action-generator tree and host interface for W2 approach B.
//
// Both engines (the goja verifier and the V8 web runtime) walk ONE tree with
// ONE picker (pick.ts), drawing through the shared PCG (pcg.ts). This module
// only declares the data shapes; pick.ts holds the traversal and the parity
// contract for draw order.
//
// Element references never cross the V8/host boundary: builtin generators emit
// an ActionDescriptor whose target is already a resolved Point (plus an
// optional selector on native), so the host serializer never has to chase a
// live element handle.

import type {
  AccessibilityElement,
  AttrSelector,
  Direction,
  Point,
  SelectorPath,
} from "./types.ts";

// Target is any target shape an action may carry before serialization. Author
// specs supply a string/selector/element; builtin generators resolve to a
// {x, y} Point (so no element handle crosses the V8/host boundary).
export type Target = string | AttrSelector | SelectorPath | AccessibilityElement | Point;

// BuiltinVerb names a leaf generator backed by host-enumerated candidates
// rather than author-supplied actions.
export type BuiltinVerb =
  | "taps"
  | "doubleTaps"
  | "longPresses"
  | "scrolls"
  | "typing"
  | "swipes"
  | "waitOnce"
  | "pressKeys";

// ActionDescriptor is the camelCase author shape an action takes before the
// engine serializes it to the wire format. Mirrors the factory return shapes
// in actions.ts (Tap/DoubleTap/LongPress {on}; Scroll {direction; in?};
// InputText {into; text}; Swipe {from; to; durationMillis?}; PressKey {key};
// Wait {durationMillis}).
export type ActionDescriptor =
  | { kind: "Tap"; on: Target }
  | { kind: "DoubleTap"; on: Target }
  | { kind: "LongPress"; on: Target }
  | { kind: "Scroll"; direction: Direction; in?: Target }
  | { kind: "InputText"; into: Target; text: string }
  | { kind: "Swipe"; from: Target; to: Target; durationMillis?: number }
  | { kind: "PressKey"; key: string }
  | { kind: "Wait"; durationMillis: number };

// GeneratorNode is a node in the action-generator tree.
//   weighted: probabilistic choice over child nodes, scanned ascending.
//   actions:  author callback returning a list to pick uniformly from.
//   builtin:  host-backed leaf identified by a verb.
export type GeneratorNode =
  | { kind: "weighted"; branches: ReadonlyArray<readonly [number, GeneratorNode]> }
  | { kind: "actions"; generate: () => ActionDescriptor[] }
  | { kind: "builtin"; verb: BuiltinVerb };

// Candidate is one host-enumerated target for a builtin verb. The host
// resolves geometry (and a native selector) so no element handle crosses into
// the picker. width/height let swipe/scroll size a gesture off the element.
export interface Candidate {
  x: number;
  y: number;
  selector?: string;
  width?: number;
  height?: number;
}

// Host is the platform backing the picker draws against. In this foundation
// workflow only the interface is defined and exercised against a stub; the
// goja and DOM implementations land in the rewire workflow.
export interface Host {
  platform(): "android" | "ios" | "web";
  // queryCandidates returns the host-enumerated targets for a verb, in a
  // deterministic order. The picker indexes into this list with the PCG, so
  // the order is part of the parity contract.
  queryCandidates(verb: BuiltinVerb): Candidate[];
  // reportUnsupported is invoked at most once per verb@platform (see verbs.ts)
  // when a verb has no support on this platform.
  reportUnsupported(verb: BuiltinVerb): void;
  seedHi(): bigint;
  seedLo(): bigint;
}
