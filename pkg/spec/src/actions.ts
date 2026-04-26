import type {
  AccessibilityElement,
  Action,
  ActionGenerator,
  InputTextAction,
  Key,
  Point,
  PressKeyAction,
  Sampler,
  SwipeAction,
  TapAction,
  WaitAction,
  WeightedEntry,
} from "./types.ts";

export function actions(generator: () => Action[]): ActionGenerator {
  return globalThis.__sanderling__.actions(generator);
}

export function whenRoute(
  routeExtractor: { readonly current: string | null },
  routes: string | readonly string[],
  body: () => Action[],
): ActionGenerator {
  const allowed = typeof routes === "string" ? [routes] : routes;
  return actions(() => {
    const current = routeExtractor.current;
    if (current === null || !allowed.includes(current)) return [];
    return body();
  });
}

export function weighted(...entries: WeightedEntry[]): ActionGenerator {
  return globalThis.__sanderling__.weighted(...entries);
}

export function from<T>(items: readonly T[]): Sampler<T> {
  return globalThis.__sanderling__.from(items);
}

export function Tap(parameters: { on: string | AccessibilityElement }): TapAction {
  return globalThis.__sanderling__.tap(parameters);
}

export function InputText(parameters: {
  into: string | AccessibilityElement;
  text: string;
}): InputTextAction {
  return globalThis.__sanderling__.inputText(parameters);
}

export function Swipe(parameters: {
  from: Point | AccessibilityElement;
  to: Point | AccessibilityElement;
  durationMillis?: number;
}): SwipeAction {
  return globalThis.__sanderling__.swipe(parameters);
}

export function PressKey(parameters: { key: Key }): PressKeyAction {
  return globalThis.__sanderling__.pressKey(parameters);
}

export function Wait(parameters: { durationMillis: number }): WaitAction {
  return globalThis.__sanderling__.wait(parameters);
}

function builtinGenerator(name: "taps" | "swipes" | "waitOnce" | "pressKeys"): ActionGenerator {
  return new Proxy({} as ActionGenerator, {
    get(_target, property) {
      const runtime = globalThis.__sanderling__[name] as unknown as Record<
        string | symbol,
        unknown
      >;
      return runtime[property];
    },
  });
}

export const taps: ActionGenerator = builtinGenerator("taps");
export const swipes: ActionGenerator = builtinGenerator("swipes");
export const waitOnce: ActionGenerator = builtinGenerator("waitOnce");
export const pressKey: ActionGenerator = builtinGenerator("pressKeys");
