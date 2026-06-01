// The shared deterministic action picker for W2 approach B.
//
// walk() traverses a GeneratorNode tree, and nextAction() wraps it with the
// 16-attempt retry that matches worker.go NextAction. Both engines (the goja
// verifier and the V8 web runtime) run THIS code, drawing through the shared
// Pcg, so a given seed yields an identical action stream on every platform.
//
// PARITY CONTRACT - draw order.
// Every random decision goes through the Pcg in a FIXED, pinned order. Changing
// this order shifts the stream for a seed and breaks cross-engine
// reproducibility, so treat it as load-bearing:
//
//   weighted node:   ONE float64() draw, then an ASCENDING cumulative scan over
//                    max(0, weight). (matches worker.go pickWeighted.)
//   actions node:    if the generated list has >1 entry, ONE intN(len) draw;
//                    a 0- or 1-element list draws nothing. (pickFromResult.)
//   builtin node, per verb, in this exact sequence:
//     taps/doubleTaps/longPresses: intN(candidateCount)            [1 draw]
//     typing:                      intN(candidateCount), intN(corpusLength)
//     swipes:                      intN(candidateCount),
//                                  200 + intN(401) magnitude, intN(4) direction
//     scrolls:                     intN(candidateCount), intN(4) direction
//     pressKeys:                   intN(keyCount)
//     waitOnce:                    no draw
//
// Builtin targets are resolved to a {x, y} Point by the host BEFORE the picker
// sees them, so no element handle crosses into this module.

import type { Pcg } from "./pcg.ts";
import type {
  ActionDescriptor,
  BuiltinVerb,
  GeneratorNode,
  Host,
} from "./action-tree.ts";
import type { Direction, Point } from "./types.ts";
import { INPUT_CORPUS, NATIVE_PRESS_KEYS, WEB_PRESS_KEYS } from "./corpus.ts";
import { supports, warnUnsupportedOnce } from "./verbs.ts";

// SWIPE_MIN_MAGNITUDE / SWIPE_MAGNITUDE_SPAN reproduce worker.go's
// `200 + rng.IntN(401)` swipe distance in pixels (200..600 inclusive).
const SWIPE_MIN_MAGNITUDE = 200;
const SWIPE_MAGNITUDE_SPAN = 401;
const SWIPE_DURATION_MILLIS = 250;

const DIRECTIONS: readonly Direction[] = ["up", "down", "left", "right"];

const MAX_RETRIES = 16;

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
    case "actions":
      return walkActions(node.generate(), rng);
    case "builtin":
      return walkBuiltin(node.verb, rng, host);
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

function walkActions(
  generated: ActionDescriptor[],
  rng: Pcg,
): ActionDescriptor | null {
  if (generated.length === 0) return null;
  if (generated.length === 1) return generated[0] ?? null;
  return generated[rng.intN(generated.length)] ?? null;
}

function walkBuiltin(
  verb: BuiltinVerb,
  rng: Pcg,
  host: Host,
): ActionDescriptor | null {
  if (!supports(verb, host.platform())) {
    warnUnsupportedOnce(host, verb);
    return null;
  }
  if (verb === "waitOnce") {
    return { kind: "Wait", durationMillis: 500 };
  }
  if (verb === "pressKeys") {
    return walkPressKey(rng, host);
  }

  const candidates = host.queryCandidates(verb);
  if (candidates.length === 0) return null;
  const picked = candidates[rng.intN(candidates.length)];
  if (!picked) return null;
  const point: Point = { x: picked.x, y: picked.y };

  switch (verb) {
    case "taps":
      return { kind: "Tap", on: point };
    case "doubleTaps":
      return { kind: "DoubleTap", on: point };
    case "longPresses":
      return { kind: "LongPress", on: point };
    case "typing": {
      const text = INPUT_CORPUS[rng.intN(INPUT_CORPUS.length)] ?? "";
      return { kind: "InputText", into: point, text };
    }
    case "swipes":
      return buildSwipe(point, rng);
    case "scrolls": {
      const direction = DIRECTIONS[rng.intN(DIRECTIONS.length)] ?? "down";
      return { kind: "Scroll", direction, in: point };
    }
  }
}

function walkPressKey(rng: Pcg, host: Host): ActionDescriptor | null {
  const keys = host.platform() === "web" ? WEB_PRESS_KEYS : NATIVE_PRESS_KEYS;
  if (keys.length === 0) return null;
  const key = keys[rng.intN(keys.length)] ?? keys[0];
  if (key === undefined) return null;
  return { kind: "PressKey", key };
}

function buildSwipe(from: Point, rng: Pcg): ActionDescriptor {
  const magnitude = SWIPE_MIN_MAGNITUDE + rng.intN(SWIPE_MAGNITUDE_SPAN);
  let toX = from.x;
  let toY = from.y;
  switch (rng.intN(4)) {
    case 0:
      toY = from.y - magnitude;
      break;
    case 1:
      toY = from.y + magnitude;
      break;
    case 2:
      toX = from.x - magnitude;
      break;
    case 3:
      toX = from.x + magnitude;
      break;
  }
  return {
    kind: "Swipe",
    from,
    to: { x: Math.max(0, toX), y: Math.max(0, toY) },
    durationMillis: SWIPE_DURATION_MILLIS,
  };
}

