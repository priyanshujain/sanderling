import assert from "node:assert/strict";
import { test } from "node:test";

import {
  InputText,
  PressKey,
  Swipe,
  Tap,
  Wait,
  actions,
  always,
  eventually,
  extract,
  from,
  keyedBy,
  next,
  now,
  pressKey,
  swipes,
  taps,
  waitOnce,
  weighted,
  whenRoute,
} from "../src/index.ts";
import type {
  AccessibilityElement,
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
  extracts: Array<(state: State) => unknown>;
  alwaysArgs: Array<(() => boolean) | Formula>;
  nowPredicates: Array<() => boolean>;
  nextPredicates: Array<() => boolean>;
  eventuallyPredicates: Array<() => boolean>;
  withinCalls: Array<{ amount: number; unit: string }>;
  impliesCalls: number;
  orCalls: number;
  andCalls: number;
  notCalls: number;
  actionGenerators: Array<() => Action[]>;
  weightedCalls: WeightedEntry[][];
  fromCalls: unknown[][];
}

function makeChainableFormula(record: RecordedRuntime): Formula {
  const formula: Formula = {
    __sanderlingFormula: true,
    implies(other: Formula): Formula {
      record.impliesCalls++;
      void other;
      return makeChainableFormula(record);
    },
    or(other: Formula): Formula {
      record.orCalls++;
      void other;
      return makeChainableFormula(record);
    },
    and(other: Formula): Formula {
      record.andCalls++;
      void other;
      return makeChainableFormula(record);
    },
    not(): Formula {
      record.notCalls++;
      return makeChainableFormula(record);
    },
  };
  return formula;
}

function makeChainableEventually(record: RecordedRuntime): EventuallyFormula {
  const base = makeChainableFormula(record);
  return {
    ...base,
    within(amount, unit) {
      record.withinCalls.push({ amount, unit });
      return makeChainableFormula(record);
    },
  };
}

function installFakeRuntime(): RecordedRuntime {
  const calls = {
    extracts: [] as Array<(state: State) => unknown>,
    alwaysArgs: [] as Array<(() => boolean) | Formula>,
    nowPredicates: [] as Array<() => boolean>,
    nextPredicates: [] as Array<() => boolean>,
    eventuallyPredicates: [] as Array<() => boolean>,
    withinCalls: [] as Array<{ amount: number; unit: string }>,
    impliesCalls: 0,
    orCalls: 0,
    andCalls: 0,
    notCalls: 0,
    actionGenerators: [] as Array<() => Action[]>,
    weightedCalls: [] as WeightedEntry[][],
    fromCalls: [] as unknown[][],
  };
  const runtime = {
    extract: <T>(getter: (state: State) => T): Extracted<T> => {
      calls.extracts.push(getter as (state: State) => unknown);
      return { current: undefined as unknown as T, previous: undefined };
    },
    always: (predicateOrFormula: (() => boolean) | Formula): Formula => {
      calls.alwaysArgs.push(predicateOrFormula);
      return makeChainableFormula(recorded);
    },
    now: (predicate: () => boolean): Formula => {
      calls.nowPredicates.push(predicate);
      return makeChainableFormula(recorded);
    },
    next: (predicate: () => boolean): Formula => {
      calls.nextPredicates.push(predicate);
      return makeChainableFormula(recorded);
    },
    eventually: (predicate: () => boolean): EventuallyFormula => {
      calls.eventuallyPredicates.push(predicate);
      return makeChainableEventually(recorded);
    },
    actions: (generator: () => Action[]): ActionGenerator => {
      calls.actionGenerators.push(generator);
      return { __sanderlingActionGenerator: true, generate: generator };
    },
    weighted: (...entries: WeightedEntry[]): ActionGenerator => {
      calls.weightedCalls.push(entries);
      return { __sanderlingActionGenerator: true, generate: () => [] };
    },
    from: <T>(items: readonly T[]): Sampler<T> => {
      calls.fromCalls.push(items as unknown[]);
      return { generate: () => items[0] as T };
    },
    tap: ({ on }) => ({ kind: "Tap", on }),
    inputText: ({ into, text }) => ({ kind: "InputText", into, text }),
    swipe: ({ from: fromPoint, to, durationMillis }) => ({
      kind: "Swipe",
      from: fromPoint,
      to,
      durationMillis,
    }),
    pressKey: ({ key }) => ({ kind: "PressKey", key }),
    wait: ({ durationMillis }) => ({ kind: "Wait", durationMillis }),
    taps: { __sanderlingActionGenerator: true, generate: () => [] },
    swipes: { __sanderlingActionGenerator: true, generate: () => [] },
    waitOnce: { __sanderlingActionGenerator: true, generate: () => [] },
    pressKeys: { __sanderlingActionGenerator: true, generate: () => [] },
  } satisfies SanderlingRuntime;
  const recorded = Object.assign(runtime, calls) as RecordedRuntime;
  globalThis.__sanderling__ = recorded;
  return recorded;
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
  assert.equal(runtime.alwaysArgs[0], predicate);
  assert.equal(formula.__sanderlingFormula, true);
});

test("always accepts a formula handle", () => {
  const runtime = installFakeRuntime();
  const inner = now(() => true);
  const wrapped = always(inner);
  assert.equal(runtime.alwaysArgs.at(-1), inner);
  assert.equal(wrapped.__sanderlingFormula, true);
});

test("now/next/eventually forward predicates", () => {
  const runtime = installFakeRuntime();
  const p1 = () => true;
  const p2 = () => false;
  const p3 = () => true;
  now(p1);
  next(p2);
  eventually(p3);
  assert.equal(runtime.nowPredicates[0], p1);
  assert.equal(runtime.nextPredicates[0], p2);
  assert.equal(runtime.eventuallyPredicates[0], p3);
});

test("eventually().within forwards unit and amount", () => {
  const runtime = installFakeRuntime();
  eventually(() => true).within(3, "seconds");
  assert.deepEqual(runtime.withinCalls[0], { amount: 3, unit: "seconds" });
});

test("formula chaining exposes implies/or/and/not", () => {
  const runtime = installFakeRuntime();
  const a = now(() => true);
  const b = now(() => false);
  a.implies(b).or(b).and(b).not();
  assert.equal(runtime.impliesCalls, 1);
  assert.equal(runtime.orCalls, 1);
  assert.equal(runtime.andCalls, 1);
  assert.equal(runtime.notCalls, 1);
});

test("Tap returns a TapAction with the supplied selector", () => {
  installFakeRuntime();
  const action = Tap({ on: "id:login_continue" });
  assert.deepEqual(action, { kind: "Tap", on: "id:login_continue" });
});

test("Tap accepts an AccessibilityElement", () => {
  installFakeRuntime();
  const element: AccessibilityElement = {
    id: "login_continue",
    find: () => undefined,
    findAll: () => [],
  };
  const action = Tap({ on: element });
  assert.equal(action.kind, "Tap");
  assert.equal(action.on, element);
});

test("InputText returns an InputTextAction", () => {
  installFakeRuntime();
  const action = InputText({ into: "id:phone", text: "+1234567890" });
  assert.deepEqual(action, { kind: "InputText", into: "id:phone", text: "+1234567890" });
});

test("Swipe returns a SwipeAction with the supplied endpoints", () => {
  installFakeRuntime();
  const action = Swipe({ from: { x: 10, y: 20 }, to: { x: 30, y: 40 }, durationMillis: 400 });
  assert.deepEqual(action, {
    kind: "Swipe",
    from: { x: 10, y: 20 },
    to: { x: 30, y: 40 },
    durationMillis: 400,
  });
});

test("PressKey returns a PressKeyAction", () => {
  installFakeRuntime();
  const action = PressKey({ key: "back" });
  assert.deepEqual(action, { kind: "PressKey", key: "back" });
});

test("Wait returns a WaitAction", () => {
  installFakeRuntime();
  const action = Wait({ durationMillis: 500 });
  assert.deepEqual(action, { kind: "Wait", durationMillis: 500 });
});

test("actions wraps a generator into the runtime's ActionGenerator", () => {
  const runtime = installFakeRuntime();
  const generator = () => [Tap({ on: "id:x" })];
  const wrapped = actions(generator);
  assert.equal(runtime.actionGenerators[0], generator);
  assert.equal(wrapped.__sanderlingActionGenerator, true);
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

test("from forwards items to the runtime", () => {
  const runtime = installFakeRuntime();
  const sampler = from(["a", "b", "c"]);
  assert.deepEqual(runtime.fromCalls[0], ["a", "b", "c"]);
  assert.equal(sampler.generate(), "a");
});

function elementWithChildren(
  cells: Record<string, string>,
): AccessibilityElement {
  return {
    find: selector => {
      if (typeof selector === "string" || Array.isArray(selector)) return undefined;
      const tag = (selector as Record<string, string>).testTag;
      if (!tag) return undefined;
      const text = cells[tag];
      if (text === undefined) return undefined;
      return { text, find: () => undefined, findAll: () => [] };
    },
    findAll: () => [],
  };
}

test("keyedBy joins testTag-resolved texts with a stable delimiter", () => {
  installFakeRuntime();
  const row = elementWithChildren({
    TxnDate: "2026-04-26",
    TxnNote: "Coffee",
    TxnAmount: "$5.00",
  });
  const key = keyedBy(row, ["TxnDate", "TxnNote", "TxnAmount"]);
  assert.equal(key, "2026-04-26\x1fCoffee\x1f$5.00");
});

test("keyedBy returns empty string for an undefined element", () => {
  installFakeRuntime();
  assert.equal(keyedBy(undefined, ["TxnDate"]), "");
});

test("keyedBy substitutes empty strings for missing children", () => {
  installFakeRuntime();
  const row = elementWithChildren({ TxnDate: "2026-04-26" });
  assert.equal(
    keyedBy(row, ["TxnDate", "TxnNote", "TxnAmount"]),
    "2026-04-26\x1f\x1f",
  );
});

test("whenRoute returns [] when current route does not match", () => {
  installFakeRuntime();
  const route = { current: "home" as string | null };
  let bodyCalled = false;
  const generator = whenRoute(route, "ledger", () => {
    bodyCalled = true;
    return [Tap({ on: "id:x" })];
  });
  assert.deepEqual(generator.generate(), []);
  assert.equal(bodyCalled, false);
});

test("whenRoute calls body when current route matches", () => {
  installFakeRuntime();
  const route = { current: "ledger" as string | null };
  const generator = whenRoute(route, "ledger", () => [Tap({ on: "id:x" })]);
  const result = generator.generate();
  assert.equal(result.length, 1);
  assert.equal(result[0]?.kind, "Tap");
});

test("whenRoute accepts an array of allowed routes", () => {
  installFakeRuntime();
  const route = { current: "add-account" as string | null };
  const generator = whenRoute(route, ["home", "add-account"], () => [Tap({ on: "id:x" })]);
  assert.equal(generator.generate().length, 1);
});

test("whenRoute returns [] for null route", () => {
  installFakeRuntime();
  const route = { current: null as string | null };
  const generator = whenRoute(route, ["home"], () => [Tap({ on: "id:x" })]);
  assert.deepEqual(generator.generate(), []);
});

test("default generators proxy through to the runtime", () => {
  installFakeRuntime();
  assert.equal(taps.__sanderlingActionGenerator, true);
  assert.equal(swipes.__sanderlingActionGenerator, true);
  assert.equal(waitOnce.__sanderlingActionGenerator, true);
  assert.equal(pressKey.__sanderlingActionGenerator, true);
});
