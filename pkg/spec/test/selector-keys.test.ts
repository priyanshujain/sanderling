import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { test } from "node:test";

import { __testing__ } from "../src/web-runtime.ts";

const { SELECTOR_KEYS, unknownSelectorKeyMessage } = __testing__;

const goldenPath = fileURLToPath(new URL("./fixtures/selector-keys.json", import.meta.url));
const golden: {
  keys: string[];
  unknownKeyExample: string[];
  unknownKeyMessage: string;
} = JSON.parse(readFileSync(goldenPath, "utf8"));

// Both runtimes reject an object-selector key they do not know, so the two key
// lists have to be one list. Were they to drift, a spec would be accepted by
// the runtime that lists the key and fail the run on the one that does not, and
// the difference would only show on the platform nobody ran first.
// internal/hierarchy/selector_keys_test.go asserts the SAME file from the
// native side.
test("the web key list is the cross-runtime list", () => {
  assert.deepEqual([...SELECTOR_KEYS], golden.keys);
});

// An author who hits this on Android and again on web must read one sentence,
// not two dialects of it.
test("the web diagnostic is the cross-runtime text", () => {
  assert.equal(unknownSelectorKeyMessage(golden.unknownKeyExample), golden.unknownKeyMessage);
});
