import assert from "node:assert/strict";
import { test } from "node:test";

import type {
  Action,
  ActionGenerator,
  EventuallyFormula,
  Extracted,
  Formula,
  Sampler,
  State,
  SanderlingRuntime,
  WeightedEntry,
} from "../src/types.ts";

interface RecordedRuntime extends SanderlingRuntime {
  currentState: State;
  extractors: Array<(state: State) => unknown>;
  alwaysArgs: Array<(() => boolean) | Formula>;
  lastPredicate: (() => boolean) | undefined;
}

function installRuntime(initialState: State): RecordedRuntime {
  const extractors: Array<(state: State) => unknown> = [];
  const extracted: Array<{ value: unknown }> = [];
  const alwaysArgs: Array<(() => boolean) | Formula> = [];
  let lastPredicate: (() => boolean) | undefined;

  const runtime = {
    extract: <T>(getter: (state: State) => T): Extracted<T> => {
      extractors.push(getter as (state: State) => unknown);
      const slot = { value: getter(state.currentState) };
      extracted.push(slot);
      return {
        get current(): T {
          return slot.value as T;
        },
        previous: undefined,
      };
    },
    always: (predicateOrFormula: (() => boolean) | Formula): Formula => {
      alwaysArgs.push(predicateOrFormula);
      if (typeof predicateOrFormula === "function") {
        lastPredicate = predicateOrFormula;
      }
      return { __sanderlingFormula: true } as Formula;
    },
    now: () => ({ __sanderlingFormula: true } as Formula),
    next: () => ({ __sanderlingFormula: true } as Formula),
    eventually: () => ({ __sanderlingFormula: true } as EventuallyFormula),
    actions: (generator: () => Action[]): ActionGenerator => ({
      __sanderlingActionGenerator: true,
      generate: generator,
    }),
    weighted: (..._entries: WeightedEntry[]): ActionGenerator => ({
      __sanderlingActionGenerator: true,
      generate: () => [],
    }),
    from: <T>(_items: readonly T[]): Sampler<T> => ({ generate: () => _items[0] as T }),
    tap: ({ on }) => ({ kind: "Tap", on }),
    inputText: ({ into, text }) => ({ kind: "InputText", into, text }),
    swipe: (p) => ({ kind: "Swipe", from: p.from, to: p.to, durationMillis: p.durationMillis }),
    pressKey: ({ key }) => ({ kind: "PressKey", key }),
    wait: ({ durationMillis }) => ({ kind: "Wait", durationMillis }),
    taps: { __sanderlingActionGenerator: true, generate: () => [] } as ActionGenerator,
    swipes: { __sanderlingActionGenerator: true, generate: () => [] } as ActionGenerator,
    waitOnce: { __sanderlingActionGenerator: true, generate: () => [] } as ActionGenerator,
    pressKeys: { __sanderlingActionGenerator: true, generate: () => [] } as ActionGenerator,
  } satisfies SanderlingRuntime;

  const state = { currentState: initialState };
  const recorded = Object.assign(runtime, {
    currentState: initialState,
    extractors,
    alwaysArgs,
    get lastPredicate() {
      return lastPredicate;
    },
  }) as unknown as RecordedRuntime;
  globalThis.__sanderling__ = recorded;
  // Re-bind state ref so subsequent extract() calls read the up-to-date state.
  Object.defineProperty(recorded, "currentState", {
    get() {
      return state.currentState;
    },
    set(next: State) {
      state.currentState = next;
    },
  });
  return recorded;
}

const emptyState: State = {
  snapshots: {},
  ax: { find: () => undefined, findAll: () => [] },
  lastAction: null,
  time: 0,
  logs: [],
  exceptions: [],
};

test("defaults bundle exports formulas tagged as LTL properties", async () => {
  installRuntime({
    ...emptyState,
    logs: [{ unixMillis: 1, level: "W", tag: "X", message: "warn" }],
  });
  const defaults = await import("../src/defaults/properties.ts");
  assert.equal(defaults.noUncaughtExceptions.__sanderlingFormula, true);
  assert.equal(defaults.noLogcatErrors.__sanderlingFormula, true);
});
