import type {
  AccessibilityElement,
  Action,
  ActionGenerator,
  InputTextAction,
  TapAction,
  WeightedEntry,
} from "./types.ts";

export function actions(generator: () => Action[]): ActionGenerator {
  return globalThis.__uatu__.actions(generator);
}

export function weighted(...entries: WeightedEntry[]): ActionGenerator {
  return globalThis.__uatu__.weighted(...entries);
}

export function Tap(parameters: { on: string | AccessibilityElement }): TapAction {
  return globalThis.__uatu__.tap(parameters);
}

export function InputText(parameters: { into: string | AccessibilityElement; text: string }): InputTextAction {
  return globalThis.__uatu__.inputText(parameters);
}

export const taps: ActionGenerator = new Proxy({} as ActionGenerator, {
  get(_target, property) {
    return (globalThis.__uatu__.taps as unknown as Record<string | symbol, unknown>)[property];
  },
});

export const swipes: ActionGenerator = new Proxy({} as ActionGenerator, {
  get(_target, property) {
    return (globalThis.__uatu__.swipes as unknown as Record<string | symbol, unknown>)[property];
  },
});
