import assert from "node:assert/strict";
import { test } from "node:test";

import { __testing__ } from "../src/web-runtime.ts";

const { mulberry32, deriveSeed32, pickWeighted, WEB_PRESS_KEYS } = __testing__;

function entry(weight: number, kind: string) {
  return [weight, { __sanderlingActionGenerator: true, __sanderlingKind: kind }] as const;
}

test("mulberry32 is deterministic for the same seed", () => {
  const a = mulberry32(deriveSeed32("12345"));
  const b = mulberry32(deriveSeed32("12345"));
  const first = Array.from({ length: 8 }, () => a());
  const second = Array.from({ length: 8 }, () => b());
  assert.deepEqual(first, second);
});

test("mulberry32 diverges for different seeds", () => {
  const a = Array.from({ length: 8 }, mulberry32(deriveSeed32("1")));
  const b = Array.from({ length: 8 }, mulberry32(deriveSeed32("2")));
  assert.notDeepEqual(a, b);
});

test("mulberry32 stays in [0, 1)", () => {
  const next = mulberry32(deriveSeed32("seed"));
  for (let i = 0; i < 1000; i++) {
    const value = next();
    assert.ok(value >= 0 && value < 1, `value out of range: ${value}`);
  }
});

test("deriveSeed32 folds a 64-bit seed without precision loss", () => {
  // Two distinct 64-bit values that collide as a JS Number must not collide here.
  const a = deriveSeed32("9007199254740993");
  const b = deriveSeed32("9007199254740992");
  assert.notEqual(a, b);
  assert.equal(deriveSeed32(undefined), 0);
});

test("pickWeighted selects by cumulative ascending scan", () => {
  const handle = { __sanderlingActionGenerator: true, __sanderlingKind: "weighted", entries: [entry(1, "a"), entry(3, "b")] } as const;
  // total = 4. pick = 0.1 -> 0.4 lands in [0,1) -> first; pick = 0.5 -> 2.0 lands in [1,4) -> second.
  assert.equal(pickWeighted(handle as never, () => 0.1)?.__sanderlingKind, "a");
  assert.equal(pickWeighted(handle as never, () => 0.5)?.__sanderlingKind, "b");
});

test("pickWeighted never selects a zero-weight entry when pick lands before it", () => {
  const handle = { __sanderlingActionGenerator: true, __sanderlingKind: "weighted", entries: [entry(1, "a"), entry(0, "zero"), entry(1, "b")] } as const;
  // total = 2. Any pick < 0.5 lands strictly inside the first entry's [0,1) band.
  for (const r of [0, 0.1, 0.25, 0.49]) {
    assert.equal(pickWeighted(handle as never, () => r)?.__sanderlingKind, "a");
  }
  // A zero-weight entry adds 0 to the cumulative, so it can never win.
  for (let i = 0; i < 100; i++) {
    const r = i / 100;
    assert.notEqual(pickWeighted(handle as never, () => r)?.__sanderlingKind, "zero");
  }
});

test("seeded press-key draw is reproducible", () => {
  const draw = (seed: string) => {
    const next = mulberry32(deriveSeed32(seed));
    return Array.from({ length: 12 }, () => WEB_PRESS_KEYS[Math.floor(next() * WEB_PRESS_KEYS.length)]);
  };
  assert.deepEqual(draw("777"), draw("777"));
  assert.notDeepEqual(draw("777"), draw("778"));
});
