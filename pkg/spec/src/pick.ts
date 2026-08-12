// The shared deterministic action picker.
//
// walk() traverses a GeneratorNode tree, and nextAction() wraps it with the
// 16-attempt retry that matches worker.go NextAction. Both engines (the goja
// verifier and the V8 web runtime) run THIS code, drawing through the shared
// Pcg, so a given seed yields an identical action stream on every platform.
//
// builtinCandidates() is the ONE enumeration of what a builtin verb can do at
// the current step. The seeded policy draws a single entry from it below; the
// model policy (Go, internal/verifier/llm.go) reads the same list through
// __sanderlingEnumerateBuiltin__. Neither policy can reach an action the other
// cannot, because neither owns an enumeration of its own.
//
// PARITY CONTRACT - draw order.
// Every random decision goes through the Pcg in a FIXED, pinned order. Changing
// this order shifts the stream for a seed and breaks cross-engine
// reproducibility, so treat it as load-bearing:
//
//   pickUniform(list): NO draw for an empty or single-entry list, otherwise ONE
//                    intN(list.length) draw. Both the actions node and the
//                    builtin node select through it.
//   weighted node:   ONE float64() draw, then an ASCENDING cumulative scan over
//                    max(0, weight). (matches worker.go pickWeighted.)
//   actions node:    the generator runs FIRST (any from(...).generate() inside
//                    draws intN(itemCount) for >1 items, nothing otherwise),
//                    THEN pickUniform over the returned list.
//   builtin node:    pickUniform over builtinCandidates(verb), THEN the one
//                    value that verb's enumeration leaves to the policy:
//                    `typing` ONE intN(corpusLength) draw for the text,
//                    `swipes` ONE 200 + intN(401) draw for the drag distance.
//                    Every other verb draws nothing further.
//
// The enumerated candidate count per verb, over the host targets the verb
// accepts (targets.ts acceptsTarget), which is what pickUniform draws over:
//
//   taps/doubleTaps/longPresses: one per accepted target
//   typing:                      one per accepted target
//   scrolls:                     two per scrollable container (down, up)
//   swipes:                      four per accepted target (down, up, left, right)
//   pressKeys:                   one per key in the platform's pool
//   waitOnce:                    exactly one, so it never draws
//
// Builtin targets are resolved to a {x, y} Point by the host BEFORE the picker
// sees them, so no element handle crosses into this module.

import type { Pcg } from "./pcg.ts";
import type {
  ActionDescriptor,
  BuiltinVerb,
  Candidate,
  GeneratorNode,
  Host,
} from "./action-tree.ts";
import type { Direction, Point } from "./types.ts";
import { INPUT_CORPUS, NATIVE_PRESS_KEYS, WEB_PRESS_KEYS } from "./corpus.ts";
import { setSamplerRng } from "./sampler-rng.ts";
import { acceptsTarget } from "./targets.ts";
import { supports, warnUnsupportedOnce } from "./verbs.ts";

const WAIT_MILLIS = 500;

// SCROLL_DIRECTIONS and SWIPE_DIRECTIONS are the directions each gesture verb
// enumerates, one candidate per (target, direction). Scrolls stay vertical:
// they target every scrollable container, so they are what makes the enumerated
// list long, and scrolling a mobile list means up and down. Swipes take all
// four, because swipe-to-dismiss and swipe-to-delete are horizontal gestures on
// list rows, and folding them away puts that defect class out of reach.
const SCROLL_DIRECTIONS: readonly Direction[] = ["down", "up"];
const SWIPE_DIRECTIONS: readonly Direction[] = ["down", "up", "left", "right"];

// SWIPE_MIN_MAGNITUDE / SWIPE_MAGNITUDE_SPAN are the free-form swipe distance in
// pixels the seeded policy draws, 200..600 inclusive. SWIPE_NOMINAL_MAGNITUDE is
// the distance the enumeration lists, so every enumerated candidate is already a
// runnable gesture before any policy has drawn anything.
const SWIPE_MIN_MAGNITUDE = 200;
const SWIPE_MAGNITUDE_SPAN = 401;
const SWIPE_NOMINAL_MAGNITUDE = 400;
const SWIPE_DURATION_MILLIS = 250;

const MAX_RETRIES = 16;

// BuiltinCandidate is one enumerated action for a builtin verb, paired with the
// index of the host target it acts on, in host.queryTargets() order (NOT the
// verb-filtered order, so a host can resolve it without repeating the filter).
// targetIndex is -1 for the untargeted verbs (a key press, a wait); the model
// policy uses it to name the control the action lands on.
export interface BuiltinCandidate {
  action: ActionDescriptor;
  targetIndex: number;
  // swipe is set on a `swipes` candidate only. The enumeration fixes where the
  // drag starts and which way it travels and lists a nominal distance; the
  // seeded policy rebuilds the gesture from these at a drawn distance, the way a
  // typing candidate names the field and leaves the text to the policy.
  swipe?: { origin: Point; direction: Direction };
}

// builtinCandidates enumerates EVERY action a builtin verb can yield against the
// host's current state. It is the single candidate producer both policies read,
// over the single eligibility rule both hosts consume. An unsupported verb
// reports once and enumerates nothing, so a platform that cannot dispatch a verb
// never offers it to either policy.
export function builtinCandidates(verb: BuiltinVerb, host: Host): BuiltinCandidate[] {
  if (!supports(verb, host.platform())) {
    warnUnsupportedOnce(host, verb);
    return [];
  }
  if (verb === "waitOnce") {
    return [{ action: { kind: "Wait", durationMillis: WAIT_MILLIS }, targetIndex: -1 }];
  }
  if (verb === "pressKeys") {
    const keys = host.platform() === "web" ? WEB_PRESS_KEYS : NATIVE_PRESS_KEYS;
    return keys.map((key) => ({
      action: { kind: "PressKey", key } as ActionDescriptor,
      targetIndex: -1,
    }));
  }

  const candidates: BuiltinCandidate[] = [];
  host.queryTargets().forEach((target, targetIndex) => {
    if (!acceptsTarget(verb, target)) return;
    const point = withSelector({ x: target.x, y: target.y }, target.selector);
    const add = (action: ActionDescriptor) => candidates.push({ action, targetIndex });
    switch (verb) {
      case "taps":
        add({ kind: "Tap", on: point });
        break;
      case "doubleTaps":
        add({ kind: "DoubleTap", on: point });
        break;
      case "longPresses":
        add({ kind: "LongPress", on: point });
        break;
      case "typing":
        // text is left empty: it is the one value the policy supplies, drawn
        // from the corpus by the seeded arm and written by the model.
        add({ kind: "InputText", into: point, text: "" });
        break;
      case "scrolls":
        for (const direction of SCROLL_DIRECTIONS) {
          add(scrollDescriptor({ x: target.x, y: target.y }, direction, target));
        }
        break;
      case "swipes":
        for (const direction of SWIPE_DIRECTIONS) {
          const origin = { x: target.x, y: target.y };
          candidates.push({
            action: swipeDescriptor(origin, direction, SWIPE_NOMINAL_MAGNITUDE),
            targetIndex,
            swipe: { origin, direction },
          });
        }
        break;
    }
  });
  return candidates;
}

// nextAction resolves an action for the current step, retrying walk() up to 16
// times when it yields null (matches worker.go NextAction). Returns null when
// every attempt comes up empty.
export function nextAction(
  root: GeneratorNode,
  rng: Pcg,
  host: Host,
): ActionDescriptor | null {
  for (let attempt = 0; attempt < MAX_RETRIES; attempt++) {
    const action = walk(root, rng, host);
    if (action !== null) return action;
  }
  return null;
}

// walk resolves a single node to an ActionDescriptor or null.
export function walk(
  node: GeneratorNode,
  rng: Pcg,
  host: Host,
): ActionDescriptor | null {
  switch (node.kind) {
    case "weighted":
      return walkWeighted(node.branches, rng, host);
    case "actions": {
      // Expose the picker's rng to from(...).generate() calls inside the
      // generator so author sampling shares this single deterministic stream.
      setSamplerRng(rng);
      try {
        return pickUniform(node.generate(), rng);
      } finally {
        setSamplerRng(null);
      }
    }
    case "builtin":
      return walkBuiltin(node.verb, rng, host);
    case "llm":
      // The LLM backend is driven by Go (it reads config.model off
      // globalThis.actions and selects via OpenRouter). On the JS picker the
      // marker is inert, so the goja NextAction reports no action and the Go
      // llmSource takes over.
      return null;
  }
}

function walkWeighted(
  branches: ReadonlyArray<readonly [number, GeneratorNode]>,
  rng: Pcg,
  host: Host,
): ActionDescriptor | null {
  let total = 0;
  for (const [weight] of branches) total += Math.max(0, weight);
  if (total <= 0) return null;
  const draw = rng.float64() * total;
  let cumulative = 0;
  for (const [weight, child] of branches) {
    cumulative += Math.max(0, weight);
    if (draw < cumulative) return walk(child, rng, host);
  }
  // Floating-point slack: draw can equal total. Fall to the last branch,
  // matching worker.go's trailing `return generators[length-1]`.
  const last = branches[branches.length - 1];
  return last ? walk(last[1], rng, host) : null;
}

// pickUniform selects one entry from a list, drawing nothing when there is no
// choice to make. It is the only selection rule in this module: the seeded
// policy is exactly "enumerate, then pickUniform".
function pickUniform<T>(list: readonly T[], rng: Pcg): T | null {
  if (list.length === 0) return null;
  if (list.length === 1) return list[0] ?? null;
  return list[rng.intN(list.length)] ?? null;
}

function walkBuiltin(
  verb: BuiltinVerb,
  rng: Pcg,
  host: Host,
): ActionDescriptor | null {
  const picked = pickUniform(builtinCandidates(verb, host), rng);
  if (!picked) return null;
  if (picked.action.kind === "InputText") {
    return { ...picked.action, text: INPUT_CORPUS[rng.intN(INPUT_CORPUS.length)] ?? "" };
  }
  if (picked.swipe) {
    const magnitude = SWIPE_MIN_MAGNITUDE + rng.intN(SWIPE_MAGNITUDE_SPAN);
    const { origin, direction } = picked.swipe;
    return swipeDescriptor(origin, direction, magnitude);
  }
  return picked.action;
}

// withSelector attaches a native selector to a resolved Point so the runner can
// re-resolve the target by id/text. The web host omits it (point-only).
function withSelector(point: Point, selector?: string): Point {
  if (selector === undefined) return point;
  return { ...point, selector } as Point;
}

// scrollDescriptor lowers a scroll to a drag over the container, matching
// runner.go's geometry: the gesture drags opposite the named content motion,
// magnitude 40% of the container extent. Missing width/height (web root) yields
// a zero-length endpoint, which the runner re-derives from container bounds.
function scrollDescriptor(
  from: Point,
  direction: Direction,
  candidate: Candidate,
): ActionDescriptor {
  const height = candidate.height ?? 0;
  let toY = from.y;
  if (direction === "down") toY = from.y - Math.trunc((4 * height) / 10);
  if (direction === "up") toY = from.y + Math.trunc((4 * height) / 10);
  return {
    kind: "Scroll",
    direction,
    in: from,
    from,
    to: { x: from.x, y: Math.max(0, toY) },
  } as ActionDescriptor;
}

// swipeDescriptor builds a free-form drag from a point over a raw pixel
// distance, rather than a fraction of a container extent: `swipes` targets any
// element with real bounds, and most of those have no scroll extent to size a
// gesture against. `direction` names where the finger travels, because a swipe
// IS the gesture; a scroll names content motion and drags the other way.
function swipeDescriptor(
  from: Point,
  direction: Direction,
  magnitude: number,
): ActionDescriptor {
  const horizontal = direction === "left" || direction === "right";
  const forward = direction === "down" || direction === "right";
  const travel = forward ? magnitude : -magnitude;
  const to = horizontal
    ? { x: Math.max(0, from.x + travel), y: from.y }
    : { x: from.x, y: Math.max(0, from.y + travel) };
  return { kind: "Swipe", from, to, durationMillis: SWIPE_DURATION_MILLIS };
}
