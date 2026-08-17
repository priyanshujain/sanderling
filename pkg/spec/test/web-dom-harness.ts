// A small DOM the web runtime can be driven over, shared by the web runtime's
// own tests and the cross-host eligibility test.
//
// It is a fake, but the structure is real: elements nest, a host owns a shadow
// root, and querySelectorAll WALKS the tree and stops at a shadow boundary
// exactly as the browser's does. That is what makes the shadow descent in
// deepQueryAll and expandShadowContent (src/web-runtime.ts) observable here at
// all; the previous harness answered three fixed selectors from a flat list, so
// deleting either descent changed no test result.
//
// What it fabricates is layout: getBoundingClientRect, scrollHeight and
// clientHeight are handed over from the spec. No headless DOM computes those,
// and they are precisely the facts collectTargets reads, so a real DOM
// implementation would have to be stubbed for them anyway.
//
// An unsupported selector throws rather than matching nothing, so a test whose
// selector this cannot parse fails loudly instead of quietly asserting over an
// empty list.

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
  // text is what an ax element handle reports as `text`, the same field the
  // goja host reads off a hierarchy node, so a test can name WHICH of two
  // same-id elements a lookup resolved to.
  text?: string;
  attrs?: Record<string, string>;
  // clickable/editable place the element in the two fact sets the host queries
  // by selector. They are answered from these flags rather than by matching
  // their CSS: the cross-host golden (fixtures/host-parity-golden.json, built
  // row for row in internal/verifier/host_parity_test.go) pins fact
  // combinations no CSS can produce, such as an <input> that is editable and
  // not clickable. A test states the facts there; this harness reports them.
  clickable?: boolean;
  editable?: boolean;
  disabled?: boolean;
  // overflows makes the element's content taller than its box, which is how the
  // host decides an element is scrollable.
  overflows?: boolean;
  focused?: boolean;
  children?: FakeElementSpec[];
  shadow?: FakeElementSpec[];
}

export interface FakeRoot {
  children: FakeElement[];
  activeElement: FakeElement | null;
  querySelectorAll(selector: string): FakeElement[];
}

export interface FakeElement extends Omit<FakeElementSpec, "children" | "shadow"> {
  tagName: string;
  type: string;
  isContentEditable: boolean;
  id: string;
  className: string;
  textContent: string;
  dataset: Record<string, string | undefined>;
  parentElement: FakeElement | null;
  children: FakeElement[];
  shadowRoot: FakeRoot | null;
  getAttribute(name: string): string | null;
  matches(selector: string): boolean;
  querySelectorAll(selector: string): FakeElement[];
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
  const attributes: Record<string, string> = { ...spec.attrs };
  if (spec.id !== undefined) attributes.id = spec.id;
  if (spec.testid !== undefined) attributes["data-testid"] = spec.testid;
  if (spec.label !== undefined) attributes["aria-label"] = spec.label;
  if (spec.alt !== undefined) attributes.alt = spec.alt;
  if (spec.title !== undefined) attributes.title = spec.title;
  const element: FakeElement = {
    ...spec,
    tagName: spec.tag.toUpperCase(),
    type: spec.tag === "input" ? "text" : "",
    isContentEditable: editable && spec.tag !== "input" && spec.tag !== "textarea",
    id: spec.id ?? "",
    className: attributes.class ?? "",
    textContent: spec.text ?? "",
    dataset: { testid: spec.testid },
    parentElement: null,
    children: (spec.children ?? []).map(fakeElement),
    shadowRoot: null,
    getAttribute: (name: string) => attributes[name] ?? null,
    // elementHandle asks an element about itself rather than sweeping the
    // document for it, so a fake that only answers querySelectorAll reports
    // every element as untappable.
    matches: (selector: string) => matchesQuery(element, selector),
    querySelectorAll: (selector: string) => queryScope(element, selector),
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
  for (const child of element.children) child.parentElement = element;
  if (spec.shadow) element.shadowRoot = fakeRoot(spec.shadow.map(fakeElement));
  return element;
}

// A shadow root's children have no parentElement, as in a real DOM, so a
// descendant selector cannot reach across the boundary from either side.
function fakeRoot(children: FakeElement[]): FakeRoot {
  const root: FakeRoot = {
    children,
    get activeElement(): FakeElement | null {
      return activeElementIn(children);
    },
    querySelectorAll: (selector: string) => queryScope(root, selector),
  };
  return root;
}

// A root answers activeElement with a node of its OWN tree, as the browser
// does: focus inside a shadow root names the host, and only that root's own
// activeElement names the field. Reporting the field from both roots would let
// a runtime that never descends still pass.
function activeElementIn(nodes: FakeElement[]): FakeElement | null {
  for (const node of nodes) {
    if (node.focused) return node;
    if (node.shadowRoot?.activeElement) return node;
    const inside = activeElementIn(node.children);
    if (inside) return inside;
  }
  return null;
}

function queryScope(scope: { children: FakeElement[] }, selector: string): FakeElement[] {
  const found: FakeElement[] = [];
  const walk = (nodes: FakeElement[]): void => {
    for (const node of nodes) {
      if (matchesQuery(node, selector)) found.push(node);
      walk(node.children);
    }
  };
  walk(scope.children);
  return found;
}

function matchesQuery(element: FakeElement, selector: string): boolean {
  if (selector === TAPPABLE_SELECTOR) return element.clickable === true;
  if (selector === EDITABLE_SELECTOR) return element.editable === true;
  return matchesSelectorList(element, selector);
}

function matchesSelectorList(element: FakeElement, selector: string): boolean {
  return splitTopLevel(selector, ",").some((complex) => matchesComplex(element, complex));
}

function matchesComplex(element: FakeElement, complex: string): boolean {
  const compounds = splitTopLevel(complex, " ");
  const subject = compounds.pop();
  if (subject === undefined) return false;
  if (!matchesCompound(element, subject)) return false;
  let ancestor = element.parentElement;
  for (const compound of compounds.reverse()) {
    while (ancestor && !matchesCompound(ancestor, compound)) ancestor = ancestor.parentElement;
    if (!ancestor) return false;
    ancestor = ancestor.parentElement;
  }
  return true;
}

const TAG_NAME = /^[a-zA-Z][a-zA-Z0-9-]*/;
const ATTRIBUTE = /^([a-zA-Z][\w-]*)(?:([~^]?)=(.+))?$/;

function matchesCompound(element: FakeElement, compound: string): boolean {
  let rest = compound;
  while (rest.length > 0) {
    if (rest.startsWith("*")) {
      rest = rest.slice(1);
      continue;
    }
    if (rest.startsWith("[")) {
      const end = closingIndex(rest, "[", "]");
      if (!matchesAttribute(element, rest.slice(1, end))) return false;
      rest = rest.slice(end + 1);
      continue;
    }
    if (rest.startsWith(":is(") || rest.startsWith(":not(")) {
      const end = closingIndex(rest, "(", ")");
      const inner = rest.slice(rest.indexOf("(") + 1, end);
      const anyMatched = splitTopLevel(inner, ",").some((part) =>
        matchesSelectorList(element, part),
      );
      if (rest.startsWith(":is(") ? !anyMatched : anyMatched) return false;
      rest = rest.slice(end + 1);
      continue;
    }
    const tag = TAG_NAME.exec(rest);
    if (!tag) throw new Error(`web-dom-harness cannot parse selector ${JSON.stringify(compound)}`);
    if (element.tagName !== tag[0].toUpperCase()) return false;
    rest = rest.slice(tag[0].length);
  }
  return true;
}

function matchesAttribute(element: FakeElement, body: string): boolean {
  const parsed = ATTRIBUTE.exec(body);
  if (!parsed) throw new Error(`web-dom-harness cannot parse attribute [${body}]`);
  const [, name, operator, quoted] = parsed;
  const actual = element.getAttribute(name!);
  if (actual === null) return false;
  if (quoted === undefined) return true;
  const value = unescapeCss(quoted.replace(/^"(.*)"$/, "$1").replace(/^'(.*)'$/, "$1"));
  if (operator === "~") return actual.split(/\s+/).includes(value);
  if (operator === "^") return actual.startsWith(value);
  return actual === value;
}

// Selector values reach the harness escaped by CSS.escape, so `[id="1a"]`
// arrives as `[id="\31 a"]` and comparing it raw would never match.
function unescapeCss(value: string): string {
  return value.replace(/\\([0-9a-fA-F]{1,6}) ?|\\(.)/g, (_, hex: string, literal: string) =>
    hex ? String.fromCodePoint(parseInt(hex, 16)) : literal,
  );
}

function closingIndex(input: string, open: string, close: string): number {
  let depth = 0;
  let quote = "";
  for (let index = input.indexOf(open); index < input.length; index++) {
    const character = input[index]!;
    if (quote) {
      if (character === quote) quote = "";
      continue;
    }
    if (character === '"' || character === "'") quote = character;
    else if (character === open) depth++;
    else if (character === close && --depth === 0) return index;
  }
  throw new Error(`web-dom-harness cannot parse selector ${JSON.stringify(input)}`);
}

function splitTopLevel(input: string, separator: string): string[] {
  const parts: string[] = [];
  let current = "";
  let depth = 0;
  let quote = "";
  for (const character of input) {
    if (quote) {
      current += character;
      if (character === quote) quote = "";
      continue;
    }
    if (character === '"' || character === "'") quote = character;
    else if (character === "(" || character === "[") depth++;
    else if (character === ")" || character === "]") depth--;
    else if (depth === 0 && (character === separator || (separator === " " && /\s/.test(character)))) {
      parts.push(current);
      current = "";
      continue;
    }
    current += character;
  }
  parts.push(current);
  return parts.map((part) => part.trim()).filter((part) => part.length > 0);
}

// withFakeDocument installs a document whose top-level children are `elements`,
// resets the host's per-tick cache, and restores the real globals afterwards.
// window goes in alongside document because buildState reads both, so an
// extractor reaching state.ax needs it.
export function withFakeDocument(elements: FakeElement[], run: () => void): void {
  const global = globalThis as Record<string, unknown>;
  const originalDocument = global.document;
  const originalWindow = global.window;
  const document: FakeRoot = fakeRoot(elements);
  global.document = document;
  global.window = {};
  __testing__.resetTargetCache();
  try {
    run();
  } finally {
    __testing__.resetTargetCache();
    global.document = originalDocument;
    global.window = originalWindow;
  }
}
