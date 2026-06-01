import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { test } from "node:test";

import { PARITY_STEPS, runParity } from "./parity-harness.ts";

const goldenPath = fileURLToPath(new URL("./fixtures/parity-golden.json", import.meta.url));
const golden = JSON.parse(readFileSync(goldenPath, "utf8"));

// The node side of the W2 acceptance gate: the shared picker's stream for the
// fixed parity scenario must match the committed golden byte-for-byte. The goja
// test asserts the SAME golden, so a match on both sides proves cross-runtime
// parity. A drift here means pick.ts, the corpus, or the Pcg changed.
test("node picker matches the cross-runtime golden", () => {
  const stream = runParity();
  assert.equal(stream.length, PARITY_STEPS);
  assert.deepEqual(stream, golden);
});

// Determinism: a second run from the same seed yields the identical stream.
test("node picker is deterministic across repeated runs", () => {
  assert.deepEqual(runParity(), runParity());
});
