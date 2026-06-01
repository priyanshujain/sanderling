import { test } from "node:test";
import assert from "node:assert/strict";
import { supports, warnUnsupportedOnce, resetWarnings } from "../src/verbs.ts";
import type { BuiltinVerb, Host } from "../src/action-tree.ts";

const ALL_VERBS: BuiltinVerb[] = [
  "taps",
  "doubleTaps",
  "longPresses",
  "scrolls",
  "typing",
  "swipes",
  "waitOnce",
  "pressKeys",
];

function countingHost(platform: "android" | "ios" | "web"): Host & { calls: BuiltinVerb[] } {
  const calls: BuiltinVerb[] = [];
  return {
    calls,
    platform: () => platform,
    queryCandidates: () => [],
    reportUnsupported: (verb) => {
      calls.push(verb);
    },
    seedHi: () => 0n,
    seedLo: () => 0n,
  };
}

test("every verb is supported on native platforms", () => {
  for (const verb of ALL_VERBS) {
    assert.equal(supports(verb, "android"), true, `android ${verb}`);
    assert.equal(supports(verb, "ios"), true, `ios ${verb}`);
  }
});

test("web supports the full builtin verb set (declared, not silently no-op)", () => {
  for (const verb of ALL_VERBS) {
    assert.equal(supports(verb, "web"), true, `web ${verb}`);
  }
});

test("warn-once reports a verb exactly once per platform", () => {
  resetWarnings();
  const host = countingHost("web");
  assert.equal(warnUnsupportedOnce(host, "swipes"), true);
  assert.equal(warnUnsupportedOnce(host, "swipes"), false);
  assert.equal(warnUnsupportedOnce(host, "swipes"), false);
  assert.deepEqual(host.calls, ["swipes"]);
});

test("warn-once keys by verb@platform independently", () => {
  resetWarnings();
  const web = countingHost("web");
  const android = countingHost("android");
  warnUnsupportedOnce(web, "scrolls");
  warnUnsupportedOnce(android, "scrolls");
  warnUnsupportedOnce(web, "scrolls");
  // Same verb on two platforms: one report each, suppressed on repeat.
  assert.deepEqual(web.calls, ["scrolls"]);
  assert.deepEqual(android.calls, ["scrolls"]);
});

test("resetWarnings re-arms reporting", () => {
  resetWarnings();
  const host = countingHost("web");
  warnUnsupportedOnce(host, "longPresses");
  resetWarnings();
  warnUnsupportedOnce(host, "longPresses");
  assert.deepEqual(host.calls, ["longPresses", "longPresses"]);
});
