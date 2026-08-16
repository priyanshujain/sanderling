import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { Pcg } from "../src/pcg.ts";

const here = dirname(fileURLToPath(import.meta.url));
const fixturePath = join(here, "fixtures", "pcg-golden.json");

interface GoldenCase {
  seed: { hi: string; lo: string };
  uint64: string[];
  float64: number[];
  intN: Record<string, number[]>;
}

const cases: GoldenCase[] = JSON.parse(readFileSync(fixturePath, "utf8"));

function seedLabel(c: GoldenCase): string {
  return `seed(${c.seed.hi}, ${c.seed.lo})`;
}

test("golden fixture has the expected shape", () => {
  assert.ok(cases.length >= 5, "expected several seed cases");
  for (const c of cases) {
    assert.equal(c.uint64.length, 40);
    assert.equal(c.float64.length, 40);
    for (const draws of Object.values(c.intN)) {
      assert.equal(draws.length, 40);
    }
  }
});

for (const c of cases) {
  test(`${seedLabel(c)} uint64 sequence is bit-exact`, () => {
    const pcg = new Pcg(BigInt(c.seed.hi), BigInt(c.seed.lo));
    c.uint64.forEach((want, i) => {
      assert.equal(pcg.uint64(), BigInt(want), `uint64 draw ${i}`);
    });
  });

  test(`${seedLabel(c)} float64 sequence is bit-exact`, () => {
    const pcg = new Pcg(BigInt(c.seed.hi), BigInt(c.seed.lo));
    c.float64.forEach((want, i) => {
      assert.equal(pcg.float64(), want, `float64 draw ${i}`);
    });
  });

  for (const [n, expected] of Object.entries(c.intN)) {
    test(`${seedLabel(c)} intN(${n}) sequence is bit-exact`, () => {
      const pcg = new Pcg(BigInt(c.seed.hi), BigInt(c.seed.lo));
      expected.forEach((want, i) => {
        assert.equal(pcg.intN(Number(n)), want, `intN(${n}) draw ${i}`);
      });
    });
  }
}

// A web run's runtime is reinstalled by every page navigation, so the host
// carries this pair across and the seed's stream stays one stream. A restore
// that lost a bit would silently re-explore ground the seed already covered.
test("a restored state continues the sequence it was taken from", () => {
  const source = new Pcg(1n, 0n);
  for (let draw = 0; draw < 5; draw++) source.uint64();
  const carried = source.state();
  const continuation = [source.uint64(), source.uint64(), source.uint64()];

  const fresh = new Pcg(1n, 0n);
  fresh.restore(carried.hi, carried.lo);
  assert.deepEqual([fresh.uint64(), fresh.uint64(), fresh.uint64()], continuation);
});
