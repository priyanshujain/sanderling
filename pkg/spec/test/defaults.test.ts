import assert from "node:assert/strict";
import { test } from "node:test";

import type {
  EventuallyFormula,
  Extracted,
  Formula,
  State,
  SanderlingRuntime,
} from "../src/types.ts";

// The defaults bundle still calls extract() and the LTL constructors, which
// stay host-bound on __sanderling__. Action factories now return plain data
// trees, so the fake runtime only needs the extract + formula surface.
function installRuntime(initialState: State): void {
  const state = { current: initialState };
  const runtime: SanderlingRuntime = {
    extract: <T>(getter: (s: State) => T): Extracted<T> => {
      const handle: Extracted<T> = {
        current: getter(state.current),
        previous: undefined,
        named: () => handle,
      };
      return handle;
    },
    always: () => ({ __sanderlingFormula: true } as Formula),
    now: () => ({ __sanderlingFormula: true } as Formula),
    next: () => ({ __sanderlingFormula: true } as Formula),
    eventually: () => ({ __sanderlingFormula: true } as EventuallyFormula),
  };
  globalThis.__sanderling__ = runtime;
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

test("defaults bundle exports defaultActions as a weighted node", async () => {
  installRuntime(emptyState);
  const defaults = await import("../src/defaults/actions.ts");
  assert.equal(defaults.defaultActions.kind, "weighted");
});

test("typing resolves as a builtin node", async () => {
  installRuntime(emptyState);
  const { typing } = await import("../src/defaults/actions.ts");
  assert.equal(typing.kind, "builtin");
  assert.equal((typing as { verb: string }).verb, "typing");
});

test("scrolls resolves as a builtin node", async () => {
  installRuntime(emptyState);
  const { scrolls } = await import("../src/defaults/actions.ts");
  assert.equal(scrolls.kind, "builtin");
  assert.equal((scrolls as { verb: string }).verb, "scrolls");
});

test("defaults barrel re-exports both properties and actions", async () => {
  installRuntime(emptyState);
  const barrel = await import("../src/defaults/index.ts");
  assert.equal(barrel.defaultActions.kind, "weighted");
  assert.equal(barrel.noUncaughtExceptions.__sanderlingFormula, true);
});
