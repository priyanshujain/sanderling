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

const { collectTargets, targetElements, buildAx } = __testing__;

// The handle facts are read back off the shipped ax element, the way a spec
// reaches them and the way internal/verifier/llm.go handleLabel reads them, so
// the comparison runs over what the model is actually shown rather than over a
// recomputation of it.
type AxHandle = {
  attrs?: Record<string, string>;
  clickable?: boolean;
  editable?: boolean;
  secure?: boolean | null;
  checked?: boolean;
  selected?: boolean;
  focused?: boolean;
};
type Ax = { find(selector: unknown): AxHandle | undefined };

function handleOf(ax: Ax, id: string): AxHandle | undefined {
  return id ? ax.find({ id }) : undefined;
}

function domFacts(): unknown[] {
  const elements = targetElements();
  const facts = collectTargets();
  const ax = buildAx() as Ax;
  if (elements.length !== facts.length) {
    throw new Error(
      `collectTargets reported ${facts.length} targets over ${elements.length} elements`,
    );
  }
  return elements.map((element, index) => {
    const target = facts[index]!;
    const handle = handleOf(ax, element.id);
    return {
      id: element.id,
      tag: element.tagName.toLowerCase(),
      clickable: target.clickable,
      enabled: target.enabled,
      editable: target.editable,
      scrollable: target.scrollable,
      hintText: handle?.attrs?.hintText ?? "",
      handleClickable: handle?.clickable ?? false,
      handleEditable: handle?.editable ?? false,
      // Three-valued, so it stays null rather than collapsing onto false: the Go
      // side compares it against a dump that leaves the fact out entirely for an
      // element no producer states it for.
      secure: handle?.secure ?? null,
      // The three states the target enumeration does not carry: they decide
      // nothing about which action is offered, so they reach a spec through the
      // handle alone, and a selector naming them resolves against the same
      // reading.
      checked: handle?.checked ?? false,
      selected: handle?.selected ?? false,
      focused: handle?.focused ?? false,
      width: target.width ?? 0,
      height: target.height ?? 0,
    };
  });
}

type FactsGlobal = { __sanderlingDomFacts__: () => unknown[] };

(globalThis as unknown as FactsGlobal).__sanderlingDomFacts__ = domFacts;
