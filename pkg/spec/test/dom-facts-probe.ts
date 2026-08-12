/// <reference lib="dom" />

// The V8 host's fact producer, read back element by element.
//
// internal/driver/chrome/fact_parity_test.go bundles this probe into a live page
// and compares what it returns against the hierarchy dump the goja host reads
// off the SAME page. Both sides derive their facts here, in production code:
// collectTargets is the shipped walk, and targetElements is the shipped
// enumeration domain, so the id at index i names the element facts[i] describes.
// The dump carries that id as `resource-id`, which is what the Go side joins on.

import { __testing__ } from "../src/web-runtime.ts";

const { collectTargets, targetElements } = __testing__;

function domFacts(): unknown[] {
  const elements = targetElements();
  const facts = collectTargets();
  if (elements.length !== facts.length) {
    throw new Error(
      `collectTargets reported ${facts.length} targets over ${elements.length} elements`,
    );
  }
  return elements.map((element, index) => {
    const target = facts[index]!;
    return {
      id: element.id,
      tag: element.tagName.toLowerCase(),
      clickable: target.clickable,
      enabled: target.enabled,
      editable: target.editable,
      scrollable: target.scrollable,
      width: target.width ?? 0,
      height: target.height ?? 0,
    };
  });
}

type FactsGlobal = { __sanderlingDomFacts__: () => unknown[] };

(globalThis as unknown as FactsGlobal).__sanderlingDomFacts__ = domFacts;
