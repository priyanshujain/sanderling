import { test } from "node:test";
import assert from "node:assert/strict";
import { Pcg } from "../src/pcg.ts";
import {
  SAMPLER_REFUSAL_NAME,
  setEnumeratingCandidates,
  setSamplerRng,
} from "../src/sampler-rng.ts";
import { edgeCaseText, emails, integers, strings } from "../src/values.ts";
import { INPUT_CORPUS } from "../src/corpus.ts";
import type { Sampler } from "../src/types.ts";

function withRng<T>(seed: bigint, body: () => T): T {
  setSamplerRng(new Pcg(seed, 0n));
  try {
    return body();
  } finally {
    setSamplerRng(null);
  }
}

test("same seed yields an identical draw sequence", () => {
  const drawAll = () => [
    strings().generate(),
    integers().between(0, 999).generate(),
    emails().domain("folio.app").generate(),
    edgeCaseText().generate(),
  ];
  assert.deepEqual(withRng(7n, drawAll), withRng(7n, drawAll));
});

test("a different seed yields a different sequence", () => {
  const drawAll = () => [strings().generate(), integers().generate()];
  assert.notDeepEqual(withRng(1n, drawAll), withRng(2n, drawAll));
});

test("integers().between constrains the output range", () => {
  withRng(42n, () => {
    for (let i = 0; i < 200; i++) {
      const value = integers().between(5, 9).generate();
      assert.ok(Number.isInteger(value));
      assert.ok(value >= 5 && value <= 9, `out of range: ${value}`);
    }
  });
});

test("strings().length(min, max).alpha() constrains length and charset", () => {
  withRng(99n, () => {
    for (let i = 0; i < 200; i++) {
      const value = strings().length(3, 6).alpha().generate();
      assert.ok(value.length >= 3 && value.length <= 6, `bad length: ${value}`);
      assert.match(value, /^[A-Za-z]*$/);
    }
  });
});

test("emails().domain sets the host and edgeCaseText draws from the corpus", () => {
  withRng(3n, () => {
    const email = emails().domain("folio.app").generate();
    assert.match(email, /^[A-Za-z]+@folio\.app$/);
    assert.ok(INPUT_CORPUS.includes(edgeCaseText().generate()));
  });
});

test("a builder is itself a Sampler<T>", () => {
  withRng(11n, () => {
    const sampler: Sampler<number> = integers().between(0, 9);
    const value = sampler.generate();
    assert.ok(value >= 0 && value <= 9);
  });
});

test("outside a walk generators return a fixed deterministic default", () => {
  setSamplerRng(null);
  assert.equal(integers().between(10, 20).generate(), 10);
  assert.equal(strings().length(4, 8).alpha().generate(), "aaaa");
  assert.equal(emails().domain("folio.app").generate(), "aaa@folio.app");
  assert.equal(edgeCaseText().generate(), INPUT_CORPUS[0]);
});

function whileEnumerating<T>(body: () => T): T {
  setEnumeratingCandidates(true);
  try {
    return body();
  } finally {
    setEnumeratingCandidates(false);
  }
}

test("every value generator refuses a multi-value draw while the model policy enumerates", () => {
  whileEnumerating(() => {
    assert.throws(() => integers().between(1, 500).generate(), {
      name: SAMPLER_REFUSAL_NAME,
      message: /random value from integers\(\)/,
    });
    assert.throws(() => strings().length(3, 6).alpha().generate(), {
      name: SAMPLER_REFUSAL_NAME,
      message: /random value from strings\(\)/,
    });
    assert.throws(() => emails().domain("folio.app").generate(), {
      name: SAMPLER_REFUSAL_NAME,
      message: /random value from emails\(\)/,
    });
    assert.throws(() => edgeCaseText().generate(), {
      name: SAMPLER_REFUSAL_NAME,
      message: /random value from edgeCaseText\(\)/,
    });
  });
});

test("a fixed-length string still refuses, because its characters vary", () => {
  whileEnumerating(() => {
    assert.throws(() => strings().length(4, 4).alpha().generate(), {
      name: SAMPLER_REFUSAL_NAME,
    });
  });
});

test("a single-valued generator keeps drawing while the model policy enumerates", () => {
  whileEnumerating(() => {
    assert.equal(integers().between(7, 7).generate(), 7);
    assert.equal(strings().length(0, 0).generate(), "");
  });
});

test("a refused generator draws again once enumeration ends", () => {
  whileEnumerating(() => {
    assert.throws(() => integers().between(1, 500).generate());
  });
  withRng(5n, () => {
    const value = integers().between(1, 500).generate();
    assert.ok(value >= 1 && value <= 500);
  });
});
