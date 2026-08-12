import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { test } from "node:test";

import { builtinCandidates } from "../src/pick.ts";
import { resetWarnings } from "../src/verbs.ts";
import type { BuiltinVerb } from "../src/action-tree.ts";
import { fakeElement, withFakeDocument, type FakeElementSpec } from "./web-dom-harness.ts";
import { __testing__ } from "../src/web-runtime.ts";

const { host } = __testing__;

const goldenPath = fileURLToPath(new URL("./fixtures/host-parity-golden.json", import.meta.url));
const golden: Record<string, string[]> = JSON.parse(readFileSync(goldenPath, "utf8"));

const VERBS: BuiltinVerb[] = [
  "taps",
  "doubleTaps",
  "longPresses",
  "typing",
  "scrolls",
  "swipes",
  "pressKeys",
  "waitOnce",
];

// SCREEN is the canonical screen both hosts are driven over: one row per fact
// combination that any verb distinguishes. The native host builds the same rows,
// in the same order, as a hierarchy tree in
// internal/verifier/host_parity_test.go.
const SCREEN: (FakeElementSpec & { name: string })[] = [
  { name: "root", tag: "html", x: 0, y: 0, width: 400, height: 800, overflows: true },
  { name: "save", tag: "button", x: 0, y: 0, width: 200, height: 60, clickable: true },
  {
    name: "cancel",
    tag: "button",
    x: 200,
    y: 0,
    width: 200,
    height: 60,
    clickable: true,
    disabled: true,
  },
  { name: "amount", tag: "input", x: 0, y: 100, width: 400, height: 60, editable: true },
  { name: "list", tag: "div", x: 0, y: 200, width: 400, height: 400, overflows: true },
  { name: "row", tag: "div", x: 0, y: 600, width: 400, height: 80 },
  { name: "collapsed", tag: "button", x: 0, y: 0, width: 0, height: 0, clickable: true },
];

// The web host and the native host used to route verbs themselves and had
// drifted: web sent `swipes` to scrollable containers only, so a swipe on a list
// row was reachable on Android and unreachable on web for the same spec.
//
// Per-verb eligibility now has ONE definition (src/targets.ts); a host reports
// facts and never filters. This test is what notices if a second definition
// grows back on either side. internal/verifier/host_parity_test.go asserts the
// SAME golden from the native host, so a match on both sides proves the two
// hosts agree without either invoking the other.
test("web host targets match the cross-host golden, verb for verb", () => {
  const elements = SCREEN.map(fakeElement);
  withFakeDocument(elements, () => {
    for (const verb of VERBS) {
      resetWarnings();
      const candidates = builtinCandidates(verb, host);
      assert.notEqual(candidates.length, 0, `${verb} enumerated nothing at all`);
      const named = candidates
        .filter((candidate) => candidate.targetIndex >= 0)
        .map((candidate) => SCREEN[candidate.targetIndex]!.name);
      assert.deepEqual(named, golden[verb], `web host targets for ${verb}`);
    }
  });
});
