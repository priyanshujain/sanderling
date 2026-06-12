// The single next-action entry shared by both engines (goja verifier and V8 web
// runtime). installRuntime wires the spec's root generator, the shared Pcg, and
// a Host into the globals the host invokes each tick:
//   __sanderlingNextAction__() -> one serialized action (unified wire contract)
//   __sanderlingExtractors__()  -> the engine's extractor snapshot
//
// Both engines call THIS picker over the SAME Pcg, so a given seed yields an
// identical action stream by construction.

import { Pcg } from "./pcg.ts";
import { nextAction, walk } from "./pick.ts";
import { INPUT_CORPUS } from "./corpus.ts";
import type { ActionDescriptor, GeneratorNode, Host } from "./action-tree.ts";
import type { Point } from "./types.ts";

// SerializedAction is the flat, camelCase wire shape JS emits and Go decodes
// (ONE decoder on each side). Builtin targets are already resolved to a Point,
// and serializeAction collapses author targets that carry {x, y}; a target that
// does not resolve to coordinates drops the action (returns null).
export type SerializedAction =
  | { kind: "Tap" | "DoubleTap" | "LongPress"; x: number; y: number; selector?: string }
  | { kind: "InputText"; x: number; y: number; text: string; selector?: string }
  | { kind: "Swipe"; fromX: number; fromY: number; toX: number; toY: number; durationMillis: number }
  | {
      kind: "Scroll";
      direction: string;
      fromX: number;
      fromY: number;
      toX: number;
      toY: number;
      durationMillis: number;
    }
  | { kind: "PressKey"; key: string }
  | { kind: "Wait"; durationMillis: number };

const DEFAULT_SWIPE_DURATION = 250;

// pointOf resolves a target to {x, y, selector?}. Builtins and resolved ax
// elements carry numeric x/y; a bare selector string carries no geometry, so it
// serializes with (0, 0) and the selector, leaving the native runner to
// re-resolve coordinates by id/text. A target with neither shape (null, an
// unrecognized object) returns undefined so the action is dropped.
function pointOf(target: unknown): (Point & { selector?: string }) | undefined {
  if (typeof target === "string") {
    return target.length > 0 ? { x: 0, y: 0, selector: target } : undefined;
  }
  if (!target || typeof target !== "object") return undefined;
  const obj = target as Record<string, unknown>;
  if (typeof obj.x === "number" && typeof obj.y === "number") {
    const point: Point & { selector?: string } = { x: obj.x, y: obj.y };
    // The picker's builtin candidates carry `selector`; a goja ax element
    // carries it under the runtime tag. Either lets the runner re-resolve.
    const selector = obj.selector ?? obj.__sanderlingSelector;
    if (typeof selector === "string" && selector.length > 0) point.selector = selector;
    return point;
  }
  return undefined;
}

export function serializeAction(action: ActionDescriptor | null): SerializedAction | null {
  if (!action) return null;
  switch (action.kind) {
    case "Tap":
    case "DoubleTap":
    case "LongPress": {
      const point = pointOf(action.on);
      if (!point) return null;
      const out: SerializedAction = { kind: action.kind, x: point.x, y: point.y };
      if (point.selector) out.selector = point.selector;
      return out;
    }
    case "InputText": {
      const point = pointOf(action.into);
      if (!point) return null;
      const out: SerializedAction = { kind: "InputText", x: point.x, y: point.y, text: action.text };
      if (point.selector) out.selector = point.selector;
      return out;
    }
    case "Swipe": {
      const from = pointOf(action.from);
      const to = pointOf(action.to);
      if (!from || !to) return null;
      return {
        kind: "Swipe",
        fromX: from.x,
        fromY: from.y,
        toX: to.x,
        toY: to.y,
        durationMillis: action.durationMillis ?? DEFAULT_SWIPE_DURATION,
      };
    }
    case "Scroll": {
      const from = pointOf(action.from) ?? pointOf(action.in);
      if (!from) return null;
      // The builtin generator pre-computes `to`; an author Scroll without it
      // collapses to a zero-length gesture the runner re-derives from bounds.
      const to = pointOf(action.to) ?? from;
      return {
        kind: "Scroll",
        direction: action.direction,
        fromX: from.x,
        fromY: from.y,
        toX: to.x,
        toY: to.y,
        durationMillis: DEFAULT_SWIPE_DURATION,
      };
    }
    case "PressKey":
      return { kind: "PressKey", key: action.key };
    case "Wait":
      return { kind: "Wait", durationMillis: action.durationMillis };
  }
}

// installRuntime defines the next-action and extractor globals for one engine.
// root is the generator the spec assigned; pass a function when the spec runs
// AFTER this call (the web bundle imports the runtime before the spec, so the
// root only exists on globalThis.actions once the spec has evaluated).
// evaluateExtractors is the engine's snapshot of the spec's extract() handles.
//
// Setup precedence: when the spec assigned globalThis.setup, it is walked ONCE
// per tick first; if it yields an action that wins, otherwise the call falls
// through to the action root's 16-attempt retry. This matches the native
// verifier's prior NextAction precedence and applies on both engines.
export function installRuntime(
  host: Host,
  root: GeneratorNode | null | (() => GeneratorNode | null),
  evaluateExtractors: () => Record<number, unknown>,
): void {
  const rng = new Pcg(host.seedHi(), host.seedLo());
  const resolveRoot = typeof root === "function" ? root : () => root;
  const resolveSetup = () =>
    (globalThis as { setup?: GeneratorNode }).setup ?? null;
  // The LLM action backend (Go) types InputText values by drawing from the same
  // edge-case corpus the seeded `typing` builtin uses. Expose that draw here so
  // Go reuses the exact sampler rather than reimplementing the corpus.
  defineLockedGlobal(
    "__sanderlingSampleInput__",
    () => INPUT_CORPUS[rng.intN(INPUT_CORPUS.length)] ?? "",
  );
  defineLockedGlobal("__sanderlingExtractors__", () => evaluateExtractors());
  defineLockedGlobal("__sanderlingNextAction__", () => {
    // resolveRoot runs first: on web it also resets the per-tick candidate
    // cache, which setup's walk below must see fresh.
    const current = resolveRoot();
    const setup = resolveSetup();
    if (setup) {
      const setupAction = walk(setup, rng, host);
      if (setupAction) return serializeAction(setupAction);
    }
    if (!current) return null;
    return serializeAction(nextAction(current, rng, host));
  });
}

function defineLockedGlobal(name: string, value: unknown): void {
  Object.defineProperty(globalThis, name, {
    value,
    writable: false,
    configurable: false,
    enumerable: false,
  });
}
