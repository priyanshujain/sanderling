/// <reference lib="dom" />

// The V8 host's selector matcher, read back match by match.
//
// internal/driver/chrome/selector_parity_test.go bundles this probe into a live
// page and compares what it returns against what internal/hierarchy resolves
// from the dump of the SAME page. Selector matching is written once per runtime,
// and only a comparison over one page can say whether the two mean the same
// thing by one selector. buildAx is the shipped `state.ax`, so the answer comes
// from production code rather than a copy of it.

import { __testing__ } from "../src/web-runtime.ts";

const { buildAx } = __testing__;

interface Handle {
  id?: string;
}

interface Ax {
  findAll(selector: unknown): Handle[];
}

function selectorMatches(selector: string): string[] {
  const ax = buildAx() as Ax;
  return ax.findAll(selector).map((element) => element.id ?? "");
}

type SelectorGlobal = {
  __sanderlingSelectorMatches__: (selector: string) => string[];
};

(globalThis as unknown as SelectorGlobal).__sanderlingSelectorMatches__ = selectorMatches;
