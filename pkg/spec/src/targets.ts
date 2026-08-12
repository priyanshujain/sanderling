// Per-verb target eligibility: the ONE definition both hosts consume.
//
// A host enumerates every element it can offer (the goja verifier over the
// hierarchy tree, the V8 web runtime over the DOM) and reports a fixed set of
// facts about each. It does NOT decide which verb may act on which element:
// acceptsTarget does, here, once. When each host routed verbs itself the two
// drifted, and the same spec induced a different action space per platform --
// web sent `swipes` to scrollable containers only, so swipe-to-dismiss on a
// list row was reachable on native and unreachable on web.
//
// What stays platform-specific is how a fact is COMPUTED, because the source
// models genuinely differ: `clickable` is an accessibility attribute on
// Android/iOS and a CSS selector match on web. The fact vocabulary below is the
// contract; the mapping onto it is each host's business, and it is the only
// place the platforms are allowed to disagree.

import type { BuiltinVerb, TargetElement } from "./action-tree.ts";

// TargetFact names one property of a target a verb can require.
export type TargetFact =
  | "clickable"
  | "enabled"
  | "editable"
  | "scrollable"
  | "positiveBounds";

// VERB_REQUIRED_FACTS lists the facts a target must have for a verb to act on
// it. `null` marks a verb with no target at all: a key press and a wait are
// enumerated by the picker without consulting the host.
const VERB_REQUIRED_FACTS: Record<BuiltinVerb, readonly TargetFact[] | null> = {
  taps: ["clickable", "enabled", "positiveBounds"],
  doubleTaps: ["clickable", "enabled", "positiveBounds"],
  longPresses: ["clickable", "enabled", "positiveBounds"],
  typing: ["editable", "enabled", "positiveBounds"],
  // Scrolls target containers that can actually scroll, and enumerate down/up.
  scrolls: ["scrollable", "positiveBounds"],
  // Any visible element is a valid swipe origin. Swipe-to-dismiss and
  // swipe-to-delete live on list rows and cards, which are not scrollable
  // containers, so scoping swipes to containers would put that whole class of
  // interaction out of reach.
  swipes: ["positiveBounds"],
  pressKeys: null,
  waitOnce: null,
};

// acceptsTarget is the per-verb element filter. Every targeted verb demands
// real bounds: a zero-bounds node centers at (0,0), and a downward gesture from
// the top-left corner is the system gesture that pulls down the notification
// shade, dragging the fuzzer out of the app.
export function acceptsTarget(verb: BuiltinVerb, target: TargetElement): boolean {
  const required = VERB_REQUIRED_FACTS[verb];
  if (required === null) return false;
  return required.every((fact) => hasFact(target, fact));
}

function hasFact(target: TargetElement, fact: TargetFact): boolean {
  switch (fact) {
    case "clickable":
      return target.clickable;
    case "enabled":
      return target.enabled;
    case "editable":
      return target.editable;
    case "scrollable":
      return target.scrollable;
    case "positiveBounds":
      return (target.width ?? 0) > 0 && (target.height ?? 0) > 0;
  }
}
