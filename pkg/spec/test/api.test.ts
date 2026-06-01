import assert from "node:assert/strict";
import { test } from "node:test";

import {
  DoubleTap,
  InputText,
  LongPress,
  PressKey,
  Scroll,
  Swipe,
  Tap,
  Wait,
  actions,
  always,
  doubleTaps,
  eventually,
  extract,
  from,
  keyedBy,
  longPresses,
  next,
  now,
  pressKey,
  scrolls,
  swipes,
  taps,
  typing,
  waitOnce,
  weighted,
  whenRoute,
} from "../src/index.ts";
import { setSamplerRng } from "../src/actions.ts";
import { Pcg } from "../src/pcg.ts";
import type {
  AccessibilityElement,
  EventuallyFormula,
  Extracted,
  Formula,
  State,
  SanderlingRuntime,
} from "../src/types.ts";

// extract() and the LTL constructors stay host-bound on __sanderling__; the
// action factories now return plain data and need no runtime. This fake records
// the host-bound calls so the forwarding tests can assert them.
interface RecordedRuntime extends SanderlingRuntime {
  extracts: Array<(state: State) => unknown>;
  extractNames: Array<string | undefined>;
  alwaysArgs: Array<(() => boolean) | Formula>;
  nowPredicates: Array<() => boolean>;
  nextPredicates: Array<() => boolean>;
  eventuallyPredicates: Array<() => boolean>;
  withinCalls: Array<{ amount: number; unit: string }>;
  impliesCalls: number;
  orCalls: number;
  andCalls: number;
  notCalls: number;
}

function makeChainableFormula(record: RecordedRuntime): Formula {
  return {
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
    extractNames: [] as Array<string | undefined>,
    alwaysArgs: [] as Array<(() => boolean) | Formula>,
    nowPredicates: [] as Array<() => boolean>,
    nextPredicates: [] as Array<() => boolean>,
    eventuallyPredicates: [] as Array<() => boolean>,
    withinCalls: [] as Array<{ amount: number; unit: string }>,
    impliesCalls: 0,
    orCalls: 0,
    andCalls: 0,
    notCalls: 0,
  };
  const runtime: SanderlingRuntime = {
    extract: <T>(getter: (state: State) => T, name?: string): Extracted<T> => {
      calls.extracts.push(getter as (state: State) => unknown);
      calls.extractNames.push(name);
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
  };
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
  assert.equal(runtime.extractNames[0], undefined);
});

test("extract accepts an explicit name", () => {
  const runtime = installFakeRuntime();
  const getter = (state: State) => state.snapshots["balance"];
  extract<unknown>("ledgerBalance", getter);
  assert.equal(runtime.extracts.length, 1);
  assert.equal(runtime.extracts[0], getter);
  assert.equal(runtime.extractNames[0], "ledgerBalance");
});

test("extract throws when given a name with no getter", () => {
  installFakeRuntime();
  assert.throws(
    () => (extract as unknown as (n: string) => unknown)("orphan"),
    /getter is required/,
  );
});

test("always wraps a predicate into a formula via the runtime", () => {
  const runtime = installFakeRuntime();
  const predicate = () => true;
  const formula = always(predicate);
  assert.equal(runtime.alwaysArgs[0], predicate);
  assert.equal(formula.__sanderlingFormula, true);
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

test("Tap returns a TapAction descriptor", () => {
  assert.deepEqual(Tap({ on: "id:login_continue" }), {
    kind: "Tap",
    on: "id:login_continue",
  });
});

test("DoubleTap returns a DoubleTapAction descriptor", () => {
  assert.deepEqual(DoubleTap({ on: "id:save" }), { kind: "DoubleTap", on: "id:save" });
});

test("LongPress returns a LongPressAction descriptor", () => {
  assert.deepEqual(LongPress({ on: "id:row" }), { kind: "LongPress", on: "id:row" });
});

test("Scroll returns a ScrollAction descriptor", () => {
  assert.deepEqual(Scroll({ direction: "down", in: "id:list" }), {
    kind: "Scroll",
    direction: "down",
    in: "id:list",
  });
});

test("Tap accepts an AccessibilityElement", () => {
  const element: AccessibilityElement = {
    id: "login_continue",
    find: () => undefined,
    findAll: () => [],
  };
  const action = Tap({ on: element });
  assert.equal(action.kind, "Tap");
  assert.equal(action.on, element);
});

test("InputText returns an InputTextAction descriptor", () => {
  assert.deepEqual(InputText({ into: "id:phone", text: "+1234567890" }), {
    kind: "InputText",
    into: "id:phone",
    text: "+1234567890",
  });
});

test("Swipe returns a SwipeAction descriptor", () => {
  assert.deepEqual(
    Swipe({ from: { x: 10, y: 20 }, to: { x: 30, y: 40 }, durationMillis: 400 }),
    { kind: "Swipe", from: { x: 10, y: 20 }, to: { x: 30, y: 40 }, durationMillis: 400 },
  );
});

test("PressKey returns a PressKeyAction descriptor", () => {
  assert.deepEqual(PressKey({ key: "back" }), { kind: "PressKey", key: "back" });
});

test("Wait returns a WaitAction descriptor", () => {
  assert.deepEqual(Wait({ durationMillis: 500 }), { kind: "Wait", durationMillis: 500 });
});

test("actions returns an actions node carrying the generator", () => {
  const generator = () => [Tap({ on: "id:x" })];
  const node = actions(generator);
  assert.equal(node.kind, "actions");
  assert.equal((node as { generate: unknown }).generate, generator);
});

test("weighted returns a weighted node carrying the branches", () => {
  const node = weighted([80, taps], [20, swipes]);
  assert.equal(node.kind, "weighted");
  assert.deepEqual((node as { branches: unknown }).branches, [
    [80, taps],
    [20, swipes],
  ]);
});

test("builtin factories return builtin nodes", () => {
  assert.deepEqual(taps, { kind: "builtin", verb: "taps" });
  assert.deepEqual(doubleTaps, { kind: "builtin", verb: "doubleTaps" });
  assert.deepEqual(longPresses, { kind: "builtin", verb: "longPresses" });
  assert.deepEqual(scrolls, { kind: "builtin", verb: "scrolls" });
  assert.deepEqual(typing, { kind: "builtin", verb: "typing" });
  assert.deepEqual(swipes, { kind: "builtin", verb: "swipes" });
  assert.deepEqual(waitOnce, { kind: "builtin", verb: "waitOnce" });
  assert.deepEqual(pressKey, { kind: "builtin", verb: "pressKeys" });
});

test("from with a single item returns it without drawing", () => {
  const sampler = from(["only"]);
  setSamplerRng(new Pcg(42n, 0n));
  try {
    assert.equal(sampler.generate(), "only");
  } finally {
    setSamplerRng(null);
  }
});

test("from draws intN(len) from the active picker rng", () => {
  const items = ["a", "b", "c"];
  const sampler = from(items);
  const rng = new Pcg(42n, 0n);
  const oracle = new Pcg(42n, 0n);
  setSamplerRng(rng);
  try {
    const index = oracle.intN(items.length);
    assert.equal(sampler.generate(), items[index]);
  } finally {
    setSamplerRng(null);
  }
});

test("from falls back to the first item outside a picker walk", () => {
  setSamplerRng(null);
  assert.equal(from(["a", "b", "c"]).generate(), "a");
});

function elementWithChildren(cells: Record<string, string>): AccessibilityElement {
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
  assert.equal(keyedBy(undefined, ["TxnDate"]), "");
});

test("keyedBy substitutes empty strings for missing children", () => {
  const row = elementWithChildren({ TxnDate: "2026-04-26" });
  assert.equal(keyedBy(row, ["TxnDate", "TxnNote", "TxnAmount"]), "2026-04-26\x1f\x1f");
});

test("whenRoute body is skipped when the current route does not match", () => {
  const route = { current: "home" as string | null };
  let bodyCalled = false;
  const node = whenRoute(route, "ledger", () => {
    bodyCalled = true;
    return [Tap({ on: "id:x" })];
  });
  assert.equal(node.kind, "actions");
  assert.deepEqual((node as { generate: () => unknown }).generate(), []);
  assert.equal(bodyCalled, false);
});

test("whenRoute runs the body when the current route matches", () => {
  const route = { current: "ledger" as string | null };
  const node = whenRoute(route, "ledger", () => [Tap({ on: "id:x" })]);
  const result = (node as { generate: () => Array<{ kind: string }> }).generate();
  assert.equal(result.length, 1);
  assert.equal(result[0]?.kind, "Tap");
});

test("whenRoute accepts an array of allowed routes", () => {
  const route = { current: "add-account" as string | null };
  const node = whenRoute(route, ["home", "add-account"], () => [Tap({ on: "id:x" })]);
  assert.equal((node as { generate: () => unknown[] }).generate().length, 1);
});

test("whenRoute body is skipped for a null route", () => {
  const route = { current: null as string | null };
  const node = whenRoute(route, ["home"], () => [Tap({ on: "id:x" })]);
  assert.deepEqual((node as { generate: () => unknown }).generate(), []);
});
