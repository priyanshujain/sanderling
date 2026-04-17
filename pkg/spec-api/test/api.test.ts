import assert from "node:assert/strict";
import { test } from "node:test";

import { Tap, InputText, actions, always, extract, swipes, taps, weighted } from "../src/index.ts";
import type {
  AccessibilityElement,
  Action,
  ActionGenerator,
  Extracted,
  Formula,
  State,
  UatuRuntime,
  WeightedEntry,
} from "../src/types.ts";

function installFakeRuntime(): UatuRuntime & {
  extracts: Array<(state: State) => unknown>;
  alwaysPredicates: Array<() => boolean>;
  actionGenerators: Array<() => Action[]>;
  weightedCalls: WeightedEntry[][];
} {
  const calls = {
    extracts: [] as Array<(state: State) => unknown>,
    alwaysPredicates: [] as Array<() => boolean>,
    actionGenerators: [] as Array<() => Action[]>,
    weightedCalls: [] as WeightedEntry[][],
  };
  const runtime: UatuRuntime = {
    extract: <T>(getter: (state: State) => T): Extracted<T> => {
      calls.extracts.push(getter as (state: State) => unknown);
      return { current: undefined as unknown as T, previous: undefined };
    },
    always: (predicate: () => boolean): Formula => {
      calls.alwaysPredicates.push(predicate);
      return { __uatuFormula: true };
    },
    actions: (generator: () => Action[]): ActionGenerator => {
      calls.actionGenerators.push(generator);
      return { __uatuActionGenerator: true, generate: generator };
    },
    weighted: (...entries: WeightedEntry[]): ActionGenerator => {
      calls.weightedCalls.push(entries);
      return { __uatuActionGenerator: true, generate: () => [] };
    },
    tap: ({ on }) => ({ kind: "Tap", on }),
    inputText: ({ into, text }) => ({ kind: "InputText", into, text }),
    taps: { __uatuActionGenerator: true, generate: () => [] },
    swipes: { __uatuActionGenerator: true, generate: () => [] },
  };
  globalThis.__uatu__ = runtime;
  return Object.assign(runtime, calls);
}

test("extract forwards the getter to the runtime", () => {
  const runtime = installFakeRuntime();
  const getter = (state: State) => state.snapshots["balance"];
  extract<unknown>(getter);
  assert.equal(runtime.extracts.length, 1);
  assert.equal(runtime.extracts[0], getter);
});

test("always wraps a predicate into a formula via the runtime", () => {
  const runtime = installFakeRuntime();
  const predicate = () => true;
  const formula = always(predicate);
  assert.equal(runtime.alwaysPredicates[0], predicate);
  assert.equal(formula.__uatuFormula, true);
});

test("Tap returns a TapAction with the supplied selector", () => {
  installFakeRuntime();
  const action = Tap({ on: "id:login_continue" });
  assert.deepEqual(action, { kind: "Tap", on: "id:login_continue" });
});

test("Tap accepts an AccessibilityElement", () => {
  installFakeRuntime();
  const element: AccessibilityElement = { id: "login_continue" };
  const action = Tap({ on: element });
  assert.equal(action.kind, "Tap");
  assert.equal(action.on, element);
});

test("InputText returns an InputTextAction", () => {
  installFakeRuntime();
  const action = InputText({ into: "id:phone", text: "+1234567890" });
  assert.deepEqual(action, { kind: "InputText", into: "id:phone", text: "+1234567890" });
});

test("actions wraps a generator into the runtime's ActionGenerator", () => {
  const runtime = installFakeRuntime();
  const generator = () => [Tap({ on: "id:x" })];
  const wrapped = actions(generator);
  assert.equal(runtime.actionGenerators[0], generator);
  assert.equal(wrapped.__uatuActionGenerator, true);
});

test("weighted forwards weighted entries to the runtime", () => {
  const runtime = installFakeRuntime();
  const entries: WeightedEntry[] = [
    [80, taps],
    [20, swipes],
  ];
  weighted(...entries);
  assert.deepEqual(runtime.weightedCalls[0], entries);
});

test("taps and swipes proxy through to the runtime defaults", () => {
  installFakeRuntime();
  assert.equal(taps.__uatuActionGenerator, true);
  assert.equal(swipes.__uatuActionGenerator, true);
  assert.equal(typeof taps.generate, "function");
});
