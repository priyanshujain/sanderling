// Author-facing action factories. Each returns plain GeneratorNode /
// ActionDescriptor DATA (see action-tree.ts); the shared picker (pick.ts) walks
// the tree and the runtime entry (runtime-entry.ts) serializes the result. No
// factory forwards to globalThis.__sanderling__ anymore: the same data tree
// drives both the goja verifier and the V8 web runtime.

import type {
  AccessibilityElement,
  Action,
  Direction,
  DoubleTapAction,
  InputTextAction,
  Key,
  LongPressAction,
  Point,
  PressKeyAction,
  Sampler,
  ScrollAction,
  SwipeAction,
  TapAction,
  WaitAction,
  WeightedEntry,
} from "./types.ts";
import type { ActionDescriptor, BuiltinVerb, GeneratorNode } from "./action-tree.ts";
import { getSamplerRng } from "./sampler-rng.ts";

export { setSamplerRng } from "./sampler-rng.ts";

function builtinNode(verb: BuiltinVerb): GeneratorNode {
  return { kind: "builtin", verb };
}

export function actions(generator: () => Action[]): GeneratorNode {
  return { kind: "actions", generate: generator as () => ActionDescriptor[] };
}

// llm selects the LLM action backend: instead of the seeded picker drawing a
// random candidate, Go drives an OpenRouter model that chooses which candidate
// to act on from the screenshot + the candidate list. The returned marker is
// inert on the JS picker (pick.ts walks it to null); Go reads its config.model
// off globalThis.actions. API key comes from OPENROUTER_API_KEY.
export function llm(config: { model: string }): GeneratorNode {
  return { kind: "llm", config };
}

export function whenRoute(
  routeExtractor: { readonly current: string | null },
  routes: string | readonly string[],
  body: () => Action[],
): GeneratorNode {
  const allowed = typeof routes === "string" ? [routes] : routes;
  return actions(() => {
    const current = routeExtractor.current;
    if (current === null || !allowed.includes(current)) return [];
    return body();
  });
}

export function weighted(...entries: WeightedEntry[]): GeneratorNode {
  return { kind: "weighted", branches: entries };
}

export function from<T>(items: readonly T[]): Sampler<T> {
  return {
    generate(): T {
      if (items.length <= 1) return items[0] as T;
      const rng = getSamplerRng();
      const index = rng ? rng.intN(items.length) : 0;
      return items[index] as T;
    },
  };
}

export function Tap(parameters: { on: string | AccessibilityElement }): TapAction {
  return { kind: "Tap", on: parameters.on };
}

export function DoubleTap(parameters: { on: string | AccessibilityElement }): DoubleTapAction {
  return { kind: "DoubleTap", on: parameters.on };
}

export function LongPress(parameters: { on: string | AccessibilityElement }): LongPressAction {
  return { kind: "LongPress", on: parameters.on };
}

export function Scroll(parameters: {
  direction: Direction;
  in?: string | AccessibilityElement;
}): ScrollAction {
  return { kind: "Scroll", direction: parameters.direction, in: parameters.in };
}

export function InputText(parameters: {
  into: string | AccessibilityElement;
  text: string;
}): InputTextAction {
  return { kind: "InputText", into: parameters.into, text: parameters.text };
}

export function Swipe(parameters: {
  from: Point | AccessibilityElement;
  to: Point | AccessibilityElement;
  durationMillis?: number;
}): SwipeAction {
  return {
    kind: "Swipe",
    from: parameters.from,
    to: parameters.to,
    durationMillis: parameters.durationMillis,
  };
}

export function PressKey(parameters: { key: Key }): PressKeyAction {
  return { kind: "PressKey", key: parameters.key };
}

export function Wait(parameters: { durationMillis: number }): WaitAction {
  return { kind: "Wait", durationMillis: parameters.durationMillis };
}

export const taps: GeneratorNode = builtinNode("taps");
export const doubleTaps: GeneratorNode = builtinNode("doubleTaps");
export const longPresses: GeneratorNode = builtinNode("longPresses");
export const scrolls: GeneratorNode = builtinNode("scrolls");
export const typing: GeneratorNode = builtinNode("typing");
export const swipes: GeneratorNode = builtinNode("swipes");
export const waitOnce: GeneratorNode = builtinNode("waitOnce");
export const pressKeys: GeneratorNode = builtinNode("pressKeys");
