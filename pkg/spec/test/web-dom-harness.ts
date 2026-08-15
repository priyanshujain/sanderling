// A minimal stand-in for the DOM surface the web host reads, shared by the web
// runtime's own tests and the cross-host eligibility test. The host asks the
// document for three things -- every element, the tappable set, the editable set
// -- and reads geometry, `disabled` and the scroll extents off each element, so
// that is all a fake has to answer.

import { __testing__ } from "../src/web-runtime.ts";

const { TAPPABLE_SELECTOR, EDITABLE_SELECTOR } = __testing__;

export interface FakeElementSpec {
  tag: string;
  x: number;
  y: number;
  width: number;
  height: number;
  // id/testid/label/alt/title are how the host names a target: it builds the
  // selector an action carries from them, so a fake needs them to exercise that
  // naming. alt and title are the fallbacks the hierarchy dump folds into
  // content-desc, which the host has to fall back to in the same order.
  id?: string;
  testid?: string;
  label?: string;
  alt?: string;
  title?: string;
  // clickable/editable place the element in the selector sets the host queries;
  // the fake answers those queries directly rather than matching CSS.
  clickable?: boolean;
  editable?: boolean;
  disabled?: boolean;
  // overflows makes the element's content taller than its box, which is how the
  // host decides an element is scrollable.
  overflows?: boolean;
}

export interface FakeElement extends FakeElementSpec {
  tagName: string;
  type: string;
  isContentEditable: boolean;
  id: string;
  dataset: Record<string, string | undefined>;
  getAttribute(name: string): string | null;
  scrollHeight: number;
  clientHeight: number;
  scrollWidth: number;
  clientWidth: number;
  getBoundingClientRect(): {
    left: number;
    top: number;
    width: number;
    height: number;
    right: number;
    bottom: number;
  };
}

export function fakeElement(spec: FakeElementSpec): FakeElement {
  const editable = spec.editable ?? false;
  return {
    ...spec,
    tagName: spec.tag.toUpperCase(),
    type: spec.tag === "input" ? "text" : "",
    isContentEditable: editable && spec.tag !== "input" && spec.tag !== "textarea",
    id: spec.id ?? "",
    dataset: { testid: spec.testid },
    getAttribute: (name: string) =>
      ({ "aria-label": spec.label, alt: spec.alt, title: spec.title })[name] ?? null,
    scrollHeight: spec.overflows ? spec.height * 2 : spec.height,
    clientHeight: spec.height,
    scrollWidth: spec.width,
    clientWidth: spec.width,
    getBoundingClientRect: () => ({
      left: spec.x,
      top: spec.y,
      width: spec.width,
      height: spec.height,
      right: spec.x + spec.width,
      bottom: spec.y + spec.height,
    }),
  };
}

// withFakeDocument installs a document answering the host's three queries over
// `elements`, resets the host's per-tick cache, and restores the real document
// afterwards.
export function withFakeDocument(elements: FakeElement[], run: () => void): void {
  const global = globalThis as Record<string, unknown>;
  const original = global.document;
  const answers: Record<string, FakeElement[]> = {
    "*": elements,
    [TAPPABLE_SELECTOR]: elements.filter((element) => element.clickable),
    [EDITABLE_SELECTOR]: elements.filter((element) => element.editable),
  };
  global.document = {
    querySelectorAll: (selector: string) => answers[selector] ?? [],
  };
  __testing__.resetTargetCache();
  try {
    run();
  } finally {
    __testing__.resetTargetCache();
    global.document = original;
  }
}
