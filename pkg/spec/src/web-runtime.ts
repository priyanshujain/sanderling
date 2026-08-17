/// <reference lib="dom" />

// V8-side runtime for `sanderling test --platform web`.
//
// This file is the WEB Host. It installs globalThis.__sanderling__ (extract +
// LTL formula binds) before the spec evaluates, implements the Host interface
// (platform/seed/queryTargets/reportUnsupported) over the live DOM, and then
// delegates ALL action generation to the shared picker via installRuntime
// (runtime-entry.ts -> pick.ts). The goja verifier runs the SAME picker over the
// SAME Pcg, so a given seed yields an identical action stream by construction.
//
// The host invokes window.__sanderlingExtractors__() and
// window.__sanderlingNextAction__() over CDP each tick. LTL predicates are
// stubbed: properties run host-side in goja, which loads its own bundle.
//
// Element references never cross V8/host. queryTargets resolves each element to
// a {x, y} Point via getBoundingClientRect before the picker sees it.

import { installRuntime } from "./runtime-entry.ts";
import type { BuiltinVerb, Candidate, Host, TargetElement } from "./action-tree.ts";

interface Handle {
  readonly current: unknown;
  readonly previous: unknown;
  named(name: string): Handle;
}

interface ExtractorEntry {
  getter: (state: unknown) => unknown;
  handle: Handle;
  name: string;
  currentValue: unknown;
  previousValue: unknown;
}

const extractors: ExtractorEntry[] = [];

// extracting is true only while an extractor getter is running. The current/
// previous accessors consult it so a getter that reaches into another
// extractor's handle throws instead of reading a stale cross-extractor value.
let extracting = false;

function checkNotExtracting(slot: "current" | "previous"): void {
  if (extracting) {
    throw new Error(
      `reading .${slot} of an extractor inside another extractor is not allowed; extractor getters may read only from the state argument`,
    );
  }
}

// SANDERLING_SEED is the host-computed 64-bit seed, injected as a decimal
// string via the bundle define. We parse it into a BigInt without ever going
// through a JS Number (which loses precision above 2^53), matching the goja
// side's rand.NewPCG(seed, 0): hi = seed, lo = 0.
function injectedSeed(): string | undefined {
  try {
    return process.env.SANDERLING_SEED;
  } catch {
    return undefined;
  }
}

function seedBigInt(): bigint {
  const raw = injectedSeed();
  if (!raw) return 0n;
  try {
    return BigInt(raw);
  } catch {
    return 0n;
  }
}

// Read on every call rather than once at module scope. The bundler replaces the
// seed expression with a literal, so production reads a constant either way,
// and parsing one decimal string per run costs nothing. Binding it at module
// scope bound it instead to whenever this module was first imported, which made
// the seed depend on test file ordering: a file importing this module before
// the seed was set froze it at zero, and the failure then surfaced in a
// different file that had set it correctly.

function noopFormula(): unknown {
  const formula: Record<string, unknown> = { __sanderlingFormula: true };
  formula.implies = () => formula;
  formula.or = () => formula;
  formula.and = () => formula;
  formula.not = () => formula;
  formula.within = () => formula;
  return formula;
}

const KNOWN_KEY_TO_CSS: Record<string, (value: string) => string> = {
  id: (v) => `[id="${cssEscape(v)}"]`,
  "resource-id": (v) => `[id="${cssEscape(v)}"]`,
  // The native rule also accepts the local name after Android's "<package>:id/".
  // The DOM has no such prefix, so a plain starts-with is the same rule here.
  idPrefix: (v) => `[id^="${cssEscape(v)}"]`,
  // The native rule accepts the label itself or the label at the head of an
  // iOS merged label ("account_card:7, Tim, $100"). `:is()` keeps that one
  // compound piece, since a multi-key selector concatenates the parts.
  desc: (v) => `:is([aria-label="${cssEscape(v)}"], [aria-label^="${cssEscape(v)}, "])`,
  descPrefix: (v) => `[aria-label^="${cssEscape(v)}"]`,
  // The native table aliases testTag onto resource-id, which the host DOM walk
  // fills from el.id, so the native path already accepts a testTag emitted as
  // an id (what Compose Multiplatform does on web). Accept both here so the
  // tables agree. `:is()` keeps this one compound, since a multi-key selector
  // concatenates the parts.
  testTag: (v) => `:is([data-testid="${cssEscape(v)}"], [id="${cssEscape(v)}"])`,
  testID: (v) => `[data-testid="${cssEscape(v)}"]`,
  "data-testid": (v) => `[data-testid="${cssEscape(v)}"]`,
  className: (v) => `[class~="${cssEscape(v)}"]`,
  class: (v) => `[class~="${cssEscape(v)}"]`,
  tag: tagSelector,
  "aria-label": (v) => `[aria-label="${cssEscape(v)}"]`,
  ariaLabel: (v) => `[aria-label="${cssEscape(v)}"]`,
  accessibilityLabel: (v) => `[aria-label="${cssEscape(v)}"]`,
  contentDescription: (v) => `[aria-label="${cssEscape(v)}"]`,
  "content-desc": (v) => `[aria-label="${cssEscape(v)}"]`,
  label: (v) => `[aria-label="${cssEscape(v)}"]`,
  placeholder: (v) => `[placeholder="${cssEscape(v)}"]`,
  placeholderValue: (v) => `[placeholder="${cssEscape(v)}"]`,
  hintText: (v) => `[placeholder="${cssEscape(v)}"]`,
};

// cssEscape delegates to the platform CSS.escape (per CSSOM spec). It produces
// output safe for both identifier and string contexts, since CSS string
// literals accept the same `\HEX ` and `\X` escape sequences as identifiers.
function cssEscape(value: string): string {
  return CSS.escape(value);
}

const TAG_NAME = /^[a-zA-Z][a-zA-Z0-9-]*$/;

// tagSelector accepts only valid HTML tag-name characters. Anything else (a
// pseudo-class like `*:hover`, a comma, whitespace) would inject CSS into the
// surrounding selector. Returning a never-matching selector rather than
// throwing keeps the spec running while making the typo visible in logs.
function tagSelector(value: string): string {
  if (!TAG_NAME.test(value)) return ":not(*)";
  return value;
}

// SELECTOR_KEYS is every key an object selector may use, held identical to the
// native list in internal/hierarchy: test/selector-keys.test.ts and
// internal/hierarchy/selector_keys_test.go each assert their own side against
// test/fixtures/selector-keys.json, so a spec cannot be accepted by one runtime
// and rejected by the other. Keys that mean nothing to a DOM (scrollable,
// package, elementType) stay accepted and simply match nothing here, the way an
// iOS-only key matches nothing on Android.
const SELECTOR_KEYS: readonly string[] = [
  "accessibilityIdentifier",
  "accessibilityLabel",
  "accessibilityText",
  "aria-label",
  "ariaLabel",
  "bounds",
  "checked",
  "class",
  "className",
  "clickable",
  "content-desc",
  "contentDescription",
  "data-testid",
  "desc",
  "descPrefix",
  "editable",
  "elementType",
  "enabled",
  "focused",
  "hintText",
  "id",
  "idPrefix",
  "identifier",
  "label",
  "package",
  "placeholder",
  "placeholderValue",
  "resource-id",
  "scrollable",
  "selected",
  "tag",
  "testID",
  "testTag",
  "text",
  "title",
  "value",
];

const SELECTOR_KEY_SET = new Set(SELECTOR_KEYS);

const ATTRIBUTE_NAME = /^[a-zA-Z][a-zA-Z0-9_.:-]*$/;

// domCarriesAttribute is the escape hatch for attributes this list does not
// enumerate: a key some element actually has is a key that can match. A key
// that is not even a legal attribute name can carry no value and would inject
// into the surrounding selector, so it is rejected rather than probed.
function domCarriesAttribute(key: string): boolean {
  if (!ATTRIBUTE_NAME.test(key)) return false;
  try {
    return document.querySelector(`[${key}]`) !== null;
  } catch {
    return false;
  }
}

// unknownSelectorKeyMessage is character for character what
// hierarchy.UnknownSelectorKeyMessage produces, so one mistake reads the same
// whichever runtime the spec ran on.
function unknownSelectorKeyMessage(keys: string[]): string {
  const named = keys.map((key) => JSON.stringify(key)).join(", ");
  return (
    `selector key ${named} cannot match: no element carries that attribute, ` +
    `and it is not one of the accepted keys: ${SELECTOR_KEYS.join(", ")}`
  );
}

// A key naming no rule is a raw attribute name, and a raw attribute matches on a
// substring, a boolean value exactly (docs/manual/spec-language.md), which is
// what internal/hierarchy does with the same key. Matching exactly here made
// `data-state:sent` name the badge on Android and nothing at all on web.
function cssPart(key: string, value: string): string {
  const builder = KNOWN_KEY_TO_CSS[key];
  if (builder) return builder(value);
  const operator = value === "true" || value === "false" ? "=" : "*=";
  return `[${key}${operator}"${cssEscape(value)}"]`;
}

// A selector key that can never match yields an empty result, which reads
// exactly like a screen with no such element: the generator declines to act,
// the runner waits out the step, and the run ends clean having explored
// nothing. Throwing is what makes the mistake visible.
function selectorFromObject(selector: Record<string, string | boolean | undefined>): {
  css?: string;
  xpath?: string;
} {
  const parts: string[] = [];
  let textValue: string | undefined;
  const unknown: string[] = [];
  for (const key of Object.keys(selector)) {
    const raw = selector[key];
    if (raw === undefined) continue;
    const value = typeof raw === "boolean" ? String(raw) : raw;
    if (!SELECTOR_KEY_SET.has(key) && !domCarriesAttribute(key)) {
      if (!unknown.includes(key)) unknown.push(key);
      continue;
    }
    if (key === "text") {
      textValue = value;
      continue;
    }
    parts.push(cssPart(key, value));
  }
  if (unknown.length > 0) {
    throw new Error(unknownSelectorKeyMessage(unknown));
  }
  if (textValue !== undefined && parts.length === 0) {
    return { xpath: innermostTextXPath(textValue) };
  }
  return { css: parts.join("") };
}

// innermostTextXPath matches an element whose text contains value and whose
// descendants do not. An element's XPath string value is its whole subtree's
// text, so without the not() clause a badge's ancestors up to <html> answer for
// it and find lands on the document. internal/hierarchy suppresses the same
// matches, and internal/driver/chrome/translate.go builds the same predicate.
function innermostTextXPath(value: string): string {
  const contains = `contains(normalize-space(.), ${xpathStringLiteral(value)})`;
  return `.//*[${contains} and not(.//*[${contains}])]`;
}

// xpathStringLiteral wraps the value in a valid XPath 1.0 string literal.
// XPath 1.0 has no escape syntax, so a value containing both ' and " must be
// composed via concat().
function xpathStringLiteral(value: string): string {
  if (!value.includes('"')) return `"${value}"`;
  if (!value.includes("'")) return `'${value}'`;
  const parts = value.split('"');
  return `concat(${parts.map((p) => `"${p}"`).join(`, '"', `)})`;
}

function selectorFromString(selector: string): { css?: string; xpath?: string } {
  const colon = selector.indexOf(":");
  if (colon <= 0) {
    return { css: selector };
  }
  const kind = selector.slice(0, colon);
  const value = selector.slice(colon + 1);
  // Substring of the element's whole text, the way internal/hierarchy reads the
  // same selector: an element reading "Sent ✓" answers to text:Sent on every
  // platform, and one React wrote as `{count} unsent` answers to text:unsent
  // though its text arrives as two text nodes, which normalize-space(text())
  // reads only the first of. Anchored at the context node, so a scoped .find
  // reads its own subtree rather than the page.
  if (kind === "text") {
    return { xpath: innermostTextXPath(value) };
  }
  // The string form's kind space stays open: "<attr>:<value>" is the documented
  // way to reach a raw driver attribute, and internal/hierarchy resolves an
  // unknown kind to an empty result rather than an error. Only the object form
  // validates, on both sides.
  return { css: cssPart(kind, value) };
}

// deepQueryAll resolves a CSS selector against a root AND every shadow root
// beneath it. querySelectorAll stops dead at a shadow boundary, and a canvas app
// (Compose for Web mounts its canvas and its whole accessibility tree inside a
// shadow root on the mount element) keeps its entire UI on the far side of one:
// without this a spec sees four nodes and can neither enumerate a target nor
// resolve a testTag. Matches come back in the order expandShadowContent walks
// and buildTree (internal/driver/chrome/driver.go) emits: a host, then that
// host's shadow content, then the host's light children. Sweeping the light DOM
// first and descending afterwards put a shadow-hosted match behind a later
// light-DOM one, so find() answered with a different element on each host.
// XPath has no equivalent, so `text:` selectors stop at the boundary.
function deepQueryAll(selector: string, root: ParentNode): Element[] {
  const found: Element[] = [];
  const visit = (scope: ParentNode): void => {
    const matched = new Set<Element>(Array.from(scope.querySelectorAll(selector)));
    for (const element of Array.from(scope.querySelectorAll<HTMLElement>("*"))) {
      if (matched.has(element)) found.push(element);
      if (element.shadowRoot) visit(element.shadowRoot);
    }
  };
  visit(root);
  return found;
}

function queryElement(
  root: ParentNode,
  selector: unknown,
): Element | null {
  if (typeof selector === "string") {
    const { css, xpath } = selectorFromString(selector);
    if (css) return deepQueryAll(css, root)[0] ?? null;
    if (xpath) {
      const result = document.evaluate(
        xpath,
        root as Node,
        null,
        XPathResult.FIRST_ORDERED_NODE_TYPE,
        null,
      );
      return result.singleNodeValue as Element | null;
    }
    return null;
  }
  if (Array.isArray(selector)) {
    let node: ParentNode | null = root;
    for (const segment of selector) {
      if (!node) return null;
      const next = queryElement(node, segment);
      if (!next) return null;
      node = next;
    }
    return node as Element;
  }
  if (selector && typeof selector === "object") {
    const { css, xpath } = selectorFromObject(selector as Record<string, string | boolean | undefined>);
    if (css) return deepQueryAll(css, root)[0] ?? null;
    if (xpath) {
      const result = document.evaluate(
        xpath,
        root as Node,
        null,
        XPathResult.FIRST_ORDERED_NODE_TYPE,
        null,
      );
      return result.singleNodeValue as Element | null;
    }
  }
  return null;
}

function queryAllElements(root: ParentNode, selector: unknown): Element[] {
  if (typeof selector === "string") {
    const { css, xpath } = selectorFromString(selector);
    if (css) return deepQueryAll(css, root);
    if (xpath) return evaluateXPathAll(xpath, root as Node);
    return [];
  }
  // A selector path: every match of the first segment is searched for the rest,
  // concatenated in walk order, mirroring FindAllBySelectorPath in
  // internal/hierarchy. Falling through to the object branch (as this did)
  // returned NOTHING for a path on web while native returned matches, so a spec
  // reading state.ax.findAll([{screen}, {row}]) saw an empty list on web and
  // every property over it passed by having nothing to check.
  if (Array.isArray(selector)) {
    const head = selector[0];
    if (head === undefined) return [];
    const heads = queryAllElements(root, head);
    if (selector.length === 1) return heads;
    const rest = selector.slice(1);
    return heads.flatMap((element) => queryAllElements(element, rest));
  }
  if (selector && typeof selector === "object" && !Array.isArray(selector)) {
    const { css, xpath } = selectorFromObject(selector as Record<string, string | boolean | undefined>);
    if (css) return deepQueryAll(css, root);
    if (xpath) return evaluateXPathAll(xpath, root as Node);
  }
  return [];
}

function evaluateXPathAll(xpath: string, root: Node): Element[] {
  const result = document.evaluate(
    xpath,
    root,
    null,
    XPathResult.ORDERED_NODE_SNAPSHOT_TYPE,
    null,
  );
  const out: Element[] = [];
  for (let i = 0; i < result.snapshotLength; i++) {
    const node = result.snapshotItem(i);
    if (node) out.push(node as Element);
  }
  return out;
}

// rawAttributes keys an element's attributes by the names the markup writes,
// which is what `attrs` means on every other backend. element.dataset would key
// `data-cents` as `cents`, so a spec reading attrs["data-cents"] the way the
// native hosts report it read undefined on web and every assertion over it
// passed vacuously.
function rawAttributes(element: Element): Record<string, string> {
  const out: Record<string, string> = {};
  for (const attribute of Array.from(element.attributes ?? [])) {
    out[attribute.name] = attribute.value;
  }
  return out;
}

// fieldHint names an editable field the way a user reads it, in the order the
// accessible name is computed: its own aria-label, the <label> bound to it, the
// placeholder standing in the empty box, then the name the form gives it. It
// lands on `hintText`, the rung visibleLabel (internal/verifier/llm.go) reads
// first for an editable element, so an authored InputText on web names its field
// the way the same action names it on Android.
function fieldHint(element: Element): string {
  if (!isEditableElement(element as HTMLElement)) return "";
  const ariaLabel = element.getAttribute("aria-label");
  if (ariaLabel) return ariaLabel;
  for (const label of Array.from((element as HTMLInputElement).labels ?? [])) {
    const text = (label.textContent ?? "").trim();
    if (text) return text;
  }
  const placeholder = element.getAttribute("placeholder");
  if (placeholder) return placeholder;
  return element.getAttribute("name") ?? "";
}

// SELECTOR_TAG is the key an ax element carries the selector it was found by,
// the same key the goja host writes (internal/verifier/bindings.go tagSelector).
// The shared serializer (runtime-entry.ts pointOf) reads it off an author
// target, so a spec's Tap({ on: state.ax.find(...) }) reaches the runner naming
// the control it acted on instead of a bare pair of coordinates. Without it
// `lastAction.on` is empty on web for exactly the actions a spec authored.
const SELECTOR_TAG = "__sanderlingSelector";

// selectorTag renders a selector argument in the canonical "k:v" grammar the
// hierarchy package parses, chains joined by " > ". It mirrors
// selectorStringFromJS in internal/verifier/marshal.go, so an element found by
// the same selector is labelled with the SAME string on both hosts.
function selectorTag(selector: unknown): string {
  if (typeof selector === "string") return selector;
  if (Array.isArray(selector)) {
    return selector
      .map(selectorTag)
      .filter((segment) => segment !== "")
      .join(" > ");
  }
  if (selector && typeof selector === "object") {
    const source = selector as Record<string, unknown>;
    return Object.keys(source)
      .filter((key) => key !== SELECTOR_TAG && source[key] !== undefined && source[key] !== null)
      .map((key) => `${key}:${String(source[key])}`)
      .join(" ");
  }
  return "";
}

// selectorTagFor names the element only when no other element in the document
// answers to the selector. The runner prefers tree.Find(action.On) over the
// coordinates the element reported (resolveCoordinates in internal/runner) and
// Find takes the first match, so naming an element by a selector its siblings
// share sends every one of their actions to the first sibling. It is the rule
// selectorsFor already applies to the builtin target enumeration, and it is
// checked document-wide even for a child lookup because the runner re-resolves
// against the whole dump rather than the parent's subtree.
function selectorTagFor(element: Element, selector: unknown): string {
  for (const match of queryAllElements(document, selector)) {
    if (match !== element) return "";
  }
  return selectorTag(selector);
}

// isEnabled answers the `enabled` fact. `.disabled` is a property only real form
// controls have, so it reads undefined on the role-based controls the tappable
// set now covers, and every one of them looked enabled however plainly it was
// marked otherwise. internal/driver/chrome/driver.go answers the same two ways
// for the dump the goja host reads.
function isEnabled(element: Element): boolean {
  if ((element as HTMLButtonElement).disabled) return false;
  return element.getAttribute("aria-disabled") !== "true";
}

// document.activeElement stops at a shadow boundary and names the HOST, so a
// Compose for Web page reported focus on its mount element and never on the
// field. selectAllScript in internal/driver/chrome/driver.go carries the rest of
// it; buildAx descends once per pass and hands the answer down.
function deepestActiveElement(): Element | null {
  let element = document.activeElement;
  while (element?.shadowRoot?.activeElement) element = element.shadowRoot.activeElement;
  return fieldBehindTheCaret(element) ?? element;
}

// Compose for Web takes keystrokes on a 1px input pinned to the caret, a SIBLING
// of the accessibility tree, so descending the shadow roots lands on a node no
// selector can name and the field carrying the test tag reads unfocused.
// selectAllScript in internal/driver/chrome/driver.go re-attributes focus the
// same way for the dump the goja host reads, and carries the reasoning,
// including why the caret's CENTRE decides rather than its whole box.
const CARET_ORIGIN_PROPERTY = "--compose-internal-web-backing-input-left";

function fieldBehindTheCaret(caretInput: Element | null): Element | null {
  if (!caretInput || caretInput.tagName !== "INPUT") return null;
  if (!getComputedStyle(caretInput).getPropertyValue(CARET_ORIGIN_PROPERTY).trim()) return null;
  const caret = caretInput.getBoundingClientRect();
  const x = (caret.left + caret.right) / 2;
  const y = (caret.top + caret.bottom) / 2;
  let field: Element | null = null;
  let fieldArea = Infinity;
  for (const candidate of editableElements()) {
    if (candidate === caretInput) continue;
    const box = candidate.getBoundingClientRect();
    const area = box.width * box.height;
    if (area <= 0 || area >= fieldArea) continue;
    if (x < box.left || x > box.right || y < box.top || y > box.bottom) continue;
    field = candidate;
    fieldArea = area;
  }
  return field;
}

function elementHandle(
  element: Element,
  selector: unknown,
  focusedElement: Element | null,
): Record<string, unknown> {
  const state = element as Partial<HTMLInputElement & HTMLOptionElement>;
  const rect = element.getBoundingClientRect();
  const x = Math.round(rect.left + rect.width / 2);
  const y = Math.round(rect.top + rect.height / 2);
  const ariaLabel = element.getAttribute("aria-label") ?? "";
  const text = (element.textContent ?? "").trim().slice(0, 200);
  const datasetCopy: Record<string, string> = {};
  const dataset = (element as HTMLElement).dataset ?? {};
  for (const key of Object.keys(dataset)) {
    const value = (dataset as Record<string, string | undefined>)[key];
    if (value !== undefined) datasetCopy[key] = value;
  }
  const attrs: Record<string, string> = {
    tag: element.tagName.toLowerCase(),
    "aria-label": ariaLabel,
    ...rawAttributes(element),
  };
  const hint = fieldHint(element);
  if (hint) attrs.hintText = hint;
  return {
    id: element.id,
    text,
    desc: ariaLabel,
    class: (element as HTMLElement).className ?? "",
    // The selector collectTargets and the hierarchy dump (driver.go) both
    // resolve clickable through. Hardcoded true here, every text node and
    // container a spec reached through state.ax claimed to be a tap target.
    clickable: element.matches(TAPPABLE_SELECTOR),
    enabled: isEnabled(element),
    // isContentEditable is inherited, so reading it alone made every span inside
    // a contenteditable container typeable here while collectTargets and the
    // hierarchy dump, which both require the element ITSELF to match
    // EDITABLE_SELECTOR, called the same span inert.
    editable: element.matches(EDITABLE_SELECTOR) && isEditableElement(element as HTMLElement),
    focused: focusedElement === element,
    // Checkbox and option state lives in the DOM PROPERTY: the markup attribute
    // records only what the page started with, so a handle reading it reports a
    // box's initial state however often the user ticks it.
    // internal/driver/chrome/driver.go reads the same two properties for the
    // dump the goja host gets.
    checked: state.checked === true,
    selected: state.selected === true,
    x,
    y,
    bounds: {
      left: Math.round(rect.left),
      top: Math.round(rect.top),
      right: Math.round(rect.right),
      bottom: Math.round(rect.bottom),
    },
    attrs,
    dataset: datasetCopy,
    [SELECTOR_TAG]: selectorTagFor(element, selector),
    find(childSelector: unknown): unknown {
      const child = queryElement(element, childSelector);
      return child ? elementHandle(child, childSelector, focusedElement) : undefined;
    },
    findAll(childSelector: unknown): unknown[] {
      return queryAllElements(element, childSelector).map((child) =>
        elementHandle(child, childSelector, focusedElement),
      );
    },
  };
}

function buildAx(): unknown {
  const focusedElement = deepestActiveElement();
  return {
    find(selector: unknown): unknown {
      const element = queryElement(document, selector);
      return element ? elementHandle(element, selector, focusedElement) : undefined;
    },
    findAll(selector: unknown): unknown[] {
      return queryAllElements(document, selector).map((element) =>
        elementHandle(element, selector, focusedElement),
      );
    },
  };
}

// Uncaught errors and unhandled rejections are buffered here as they fire, so
// the default noUncaughtExceptions property can observe them. Without this the
// web state.exceptions would always be empty and a page that throws would
// silently pass.
interface CapturedException {
  class: string;
  message: string;
  stackTrace: string;
  unixMillis: number;
}

const capturedExceptions: CapturedException[] = [];

function recordException(error: unknown): void {
  const asError = error instanceof Error ? error : undefined;
  capturedExceptions.push({
    class: asError?.name ?? "Error",
    message: asError?.message ?? String(error),
    stackTrace: asError?.stack ?? "",
    unixMillis: Date.now(),
  });
}

if (typeof globalThis.addEventListener === "function") {
  globalThis.addEventListener("error", (event: ErrorEvent) => {
    recordException(event.error ?? event.message);
  });
  globalThis.addEventListener("unhandledrejection", (event: PromiseRejectionEvent) => {
    recordException(event.reason);
  });
}

// lastAction is what the previous step actually did, pushed in by the Go runner
// (internal/runner, via __sanderlingSetLastAction__) before each extractor
// evaluation, in the shape internal/verifier/marshal.go builds for goja. The
// page cannot derive it: only the runner knows whether the action it picked was
// really applied, and under --generator llm the action is not picked here at
// all. Hardcoding null here, as this file used to, makes every spec property
// that reads state.lastAction vacuously true on web.
let lastAction: unknown = null;

// logs is what the driver captured between the previous step and this one,
// pushed in by the Go runner (via __sanderlingSetLogs__) before each extractor
// evaluation, in the shape internal/verifier/marshal.go builds for goja. The
// page cannot derive it: console output reaches the runner over CDP and nothing
// in the page reads it back. Hardcoding [] here, as this file used to, makes
// every spec property that reads state.logs vacuously true on web, the default
// noLogcatErrors included, because the page's reading is the one that wins.
let logs: unknown[] = [];

function buildState(): unknown {
  return {
    snapshots: {},
    ax: buildAx(),
    document,
    window,
    lastAction,
    time: 0,
    logs,
    exceptions: capturedExceptions.slice(),
  };
}

const runtime = {
  extract<T>(getter: (state: unknown) => T, name?: string): Handle {
    const resolvedName = name && name.length > 0 ? name : `extractor_${extractors.length}`;
    const entry: ExtractorEntry = {
      getter: getter as (s: unknown) => unknown,
      handle: undefined as unknown as Handle,
      name: resolvedName,
      currentValue: undefined,
      previousValue: undefined,
    };
    const handle: Handle = {
      get current() {
        checkNotExtracting("current");
        return entry.currentValue;
      },
      get previous() {
        checkNotExtracting("previous");
        return entry.previousValue;
      },
      named(name: string): Handle {
        entry.name = name;
        return handle;
      },
    };
    entry.handle = handle;
    extractors.push(entry);
    return handle;
  },
  always: noopFormula,
  now: noopFormula,
  next: noopFormula,
  eventually: noopFormula,
};

// Lock the runtime globals so a misbehaving (or malicious) page script can't
// shadow or replace them between AddScriptToEvaluateOnNewDocument running and
// the host invoking the extractor/next-action callbacks.
defineLockedGlobal("__sanderling__", runtime);

// The host calls this once per step, before __sanderlingExtractors__.
defineLockedGlobal("__sanderlingSetLastAction__", (value: unknown) => {
  lastAction = value ?? null;
});

// The host calls this once per step too, alongside __sanderlingSetLastAction__.
defineLockedGlobal("__sanderlingSetLogs__", (value: unknown) => {
  logs = Array.isArray(value) ? value : [];
});

// The host reads the same buffer buildState puts behind state.exceptions, so
// the goja-side state.exceptions is the page's list rather than the empty one
// it held before, and the trace records an error surface an offline oracle can
// read back.
defineLockedGlobal("__sanderlingExceptions__", () => capturedExceptions.slice());

// writable:false stops a page script from shadowing the runtime via plain
// assignment (the realistic in-page threat). configurable:true is required so
// unit tests sharing one process can reinstall a fake via defineProperty; a
// non-configurable lock would poison globalThis.__sanderling__ for every later
// test in the run.
function defineLockedGlobal(name: string, value: unknown): void {
  Object.defineProperty(globalThis, name, {
    value,
    writable: false,
    configurable: true,
    enumerable: false,
  });
}

// Each reading is wrapped in a {value} envelope because JSON has no undefined.
// Written straight into the map, an extractor whose getter returned undefined
// (folio's on(route, tag) off its own screen, which is most extractors on most
// steps) had its whole INDEX dropped by JSON.stringify, and the host kept goja's
// dump-derived reading for it while the rest held the page's. Inside the
// envelope the same drop means "this getter returned undefined", which is what
// the goja host records for the same getter; a JSON null would instead claim it
// returned null, and `x.current === undefined` would answer differently on the
// two hosts.
function evaluateExtractors(): Record<number, { value?: unknown }> {
  const state = buildState();
  const result: Record<number, { value?: unknown }> = {};
  for (let i = 0; i < extractors.length; i++) {
    const entry = extractors[i];
    if (!entry) continue;
    entry.previousValue = entry.currentValue;
    // Let getter throws propagate, matching the goja side, where a getter
    // error aborts PushSnapshot rather than yielding undefined. Swallowing
    // here would silence the cross-extractor read guard (and every other
    // author error) on web only, breaking cross-engine parity.
    let value: unknown;
    extracting = true;
    try {
      value = entry.getter(state);
    } finally {
      extracting = false;
    }
    entry.currentValue = value;
    result[i] = { value: sanitize(value) };
  }
  return result;
}

// SANITIZE_MAX_DEPTH bounds how far sanitize will recurse. State exposes
// `document` and `window`, both of which contain cycles; without a depth or
// seen-set guard a user extractor returning either crashes the runtime via
// stack overflow.
const SANITIZE_MAX_DEPTH = 32;

function sanitize(value: unknown): unknown {
  return sanitizeAt(value, 0, new WeakSet());
}

function sanitizeAt(value: unknown, depth: number, seen: WeakSet<object>): unknown {
  if (value === null || value === undefined) return value;
  if (typeof value === "function") return undefined;
  if (typeof value !== "object") return value;
  if (depth >= SANITIZE_MAX_DEPTH) return null;
  if (seen.has(value as object)) return null;
  seen.add(value as object);
  if (Array.isArray(value)) {
    return value.map((item) => sanitizeAt(item, depth + 1, seen));
  }
  const out: Record<string, unknown> = {};
  for (const key of Object.keys(value as Record<string, unknown>)) {
    const sub = (value as Record<string, unknown>)[key];
    if (typeof sub === "function") continue;
    out[key] = sanitizeAt(sub, depth + 1, seen);
  }
  return out;
}

// The DOM half of the fact mapping. Which verb may act on which target is NOT
// decided here: queryTargets reports facts and targets.ts acceptsTarget applies
// them, the same rule the native host's targets run through. These selectors are
// only how the DOM answers "is this clickable" / "is this editable", the two
// facts with no direct DOM equivalent of the accessibility attributes native
// platforms expose.
//
// TAPPABLE_ROLES are the ARIA roles whose whole contract is that a user
// activates the element. Covering only role="button" left every other one
// invisible to the enumeration, however plain the control looked: the replay UI
// builds its step rows as <li role="option">, and the spec dogfooding it had to
// hand-write an action to reach them because no default verb could see a single
// row. internal/driver/chrome/driver.go resolves the same set for the hierarchy
// dump the goja host reads, and the two are compared element by element by
// TestHierarchy_DerivesTheSameFactsAsTheWebRuntime.
const TAPPABLE_ROLES = [
  "button", "link", "checkbox", "radio", "switch", "tab", "option",
  "menuitem", "menuitemcheckbox", "menuitemradio", "treeitem",
];
const TAPPABLE_SELECTOR = `a, button, input, select, textarea, ${
  TAPPABLE_ROLES.map((role) => `[role="${role}"]`).join(", ")
}, [onclick]`;
const EDITABLE_SELECTOR = "input, textarea, [contenteditable]";

const NON_TEXT_INPUT_TYPES = [
  "button", "submit", "checkbox", "radio", "range", "color", "file", "image", "reset",
];

function isEditableElement(element: HTMLElement): boolean {
  if (element.isContentEditable) return true;
  const tag = element.tagName.toLowerCase();
  if (tag === "textarea") return true;
  if (tag === "input") {
    const type = ((element as HTMLInputElement).type || "").toLowerCase();
    return !NON_TEXT_INPUT_TYPES.includes(type);
  }
  return false;
}

function editableElements(): Set<Element> {
  return new Set<Element>(
    (deepQueryAll(EDITABLE_SELECTOR, document) as HTMLElement[]).filter(isEditableElement),
  );
}

// isScrollable mirrors the native `scrollable` accessibility attribute: the
// container can actually scroll, i.e. its content overflows its box. The
// document scrolling root is not special-cased in: when the page does not
// overflow there is no scroll to perform, and native would offer none either.
function isScrollable(element: HTMLElement): boolean {
  return element.scrollHeight > element.clientHeight || element.scrollWidth > element.clientWidth;
}

function pointOf(element: Element): Candidate {
  const rect = element.getBoundingClientRect();
  return {
    x: Math.round(rect.left + rect.width / 2),
    y: Math.round(rect.top + rect.height / 2),
    width: Math.round(rect.width),
    height: Math.round(rect.height),
  };
}

// HEAD_SELECTOR is the one subtree the enumeration leaves out. It never renders,
// so no verb can reach it, and the hierarchy dump the goja host reads
// (internal/driver/chrome/driver.go) drops it as well. Enumerating it here would
// put the two hosts on different element sets for every page that has a <head>.
const HEAD_SELECTOR = "head, head *";

// targetElements is the walk the target list is built from: the document in
// pre-order, minus the head subtree, with each shadow host's content spliced in
// directly after the host. That is buildTree's order in
// internal/driver/chrome/driver.go, and the two producers are compared element
// by element in enumeration order.
function targetElements(): HTMLElement[] {
  const inHead = new Set<Element>(Array.from(document.querySelectorAll(HEAD_SELECTOR)));
  const walked: HTMLElement[] = [];
  expandShadowContent(
    Array.from(document.querySelectorAll<HTMLElement>("*")).filter(
      (element) => !inHead.has(element),
    ),
    walked,
  );
  return walked;
}

// expandShadowContent copies a tree-ordered element list into `into`, following
// each host into its shadow root (and into nested hosts) as it goes.
function expandShadowContent(elements: HTMLElement[], into: HTMLElement[]): void {
  for (const element of elements) {
    into.push(element);
    const shadow = element.shadowRoot;
    if (!shadow) continue;
    expandShadowContent(Array.from(shadow.querySelectorAll<HTMLElement>("*")), into);
  }
}

// IDENTITY_KEYS is the ladder a target's selector is built from, mirroring
// selectorForElement in internal/verifier/worker.go: the id first (where
// Compose for Web lands a testTag), then data-testid, then the description.
// Every key here is one the goja host's selector grammar already understands,
// so the runner can re-resolve the target it names. `desc` reads the same three
// attributes, in the same order, that the hierarchy dump folds into
// content-desc (internal/driver/chrome/driver.go); reading fewer of them would
// let a selector this side calls unique resolve to a different element on the
// Go side, which re-routes the action to whatever the dump matched first.
const IDENTITY_KEYS: ReadonlyArray<readonly [string, (element: HTMLElement) => string]> = [
  ["id", (element) => element.id],
  ["data-testid", (element) => element.dataset.testid ?? ""],
  [
    "desc",
    (element) =>
      element.getAttribute("aria-label") ||
      element.getAttribute("alt") ||
      element.getAttribute("title") ||
      "",
  ],
];

// selectorsFor names each enumerated element, or leaves it unnamed. A value is
// only used when it occurs ONCE across the enumeration, so an action carrying
// the selector can never be re-resolved onto a sibling that shares the value
// (folio's Home screen has many AccountCards under one testTag). Unnamed
// elements keep the coordinates-only behaviour the web host always had.
function selectorsFor(elements: readonly HTMLElement[]): Array<string | undefined> {
  const counts = IDENTITY_KEYS.map(() => new Map<string, number>());
  for (const element of elements) {
    IDENTITY_KEYS.forEach(([, read], index) => {
      const value = read(element);
      if (!value) return;
      const seen = counts[index]!;
      seen.set(value, (seen.get(value) ?? 0) + 1);
    });
  }
  return elements.map((element) => {
    for (let index = 0; index < IDENTITY_KEYS.length; index++) {
      const [key, read] = IDENTITY_KEYS[index]!;
      const value = read(element);
      if (value && counts[index]!.get(value) === 1) return `${key}:${value}`;
    }
    return undefined;
  });
}

// collectTargets walks the document ONCE and reports every element with the facts
// the shared eligibility rule reads. The tappable/editable membership sets are
// resolved by selector first so the DOM's answer to "clickable" and "editable"
// stays expressed in CSS, as it always was.
function collectTargets(): TargetElement[] {
  const clickable = new Set<Element>(deepQueryAll(TAPPABLE_SELECTOR, document));
  const editable = editableElements();
  const elements = targetElements();
  const selectors = selectorsFor(elements);
  return elements.map((element, index) => ({
    ...pointOf(element),
    selector: selectors[index],
    clickable: clickable.has(element),
    enabled: isEnabled(element),
    editable: editable.has(element),
    scrollable: isScrollable(element),
  }));
}

// Per-tick target cache: the picker's 16-attempt retry re-queries every tick, so
// we avoid re-walking the DOM and re-flushing layout within one tick.
// installRuntime resets it before each __sanderlingNextAction__ invocation.
let cachedTargets: TargetElement[] | null = null;

function resetTargetCache(): void {
  cachedTargets = null;
}

const host: Host = {
  platform: () => "web",
  seedHi: () => seedBigInt(),
  // lo = 0 matches the goja side's rand.NewPCG(seed, 0).
  seedLo: () => 0n,
  queryTargets(): TargetElement[] {
    if (!cachedTargets) cachedTargets = collectTargets();
    return cachedTargets;
  },
  reportUnsupported(verb: BuiltinVerb): void {
    console.warn(`[sanderling] verb ${verb} is unsupported on web`);
  },
};

// The spec assigns its action root to globalThis.actions, and it runs AFTER
// this module (the web bundle imports the runtime first). Resolve the root
// lazily so installRuntime captures it once the spec has evaluated. The root
// resolver runs once per __sanderlingNextAction__ tick (before the retry loop),
// so it is also where we reset the per-tick target cache.
installRuntime(
  host,
  () => {
    resetTargetCache();
    return (globalThis as { actions?: import("./action-tree.ts").GeneratorNode }).actions ?? null;
  },
  evaluateExtractors,
);

// Test-only exports. The IIFE bundle the host installs has no export surface,
// so these are stripped from production output; they only exist for unit tests.
export const __testing__ = {
  host,
  buildAx,
  seedBigInt,
  collectTargets,
  targetElements,
  TAPPABLE_SELECTOR,
  EDITABLE_SELECTOR,
  resetTargetCache,
  runtime,
  extractors,
  evaluateExtractors,
  selectorFromString,
  selectorFromObject,
  SELECTOR_KEYS,
  unknownSelectorKeyMessage,
  selectorTag,
  xpathStringLiteral,
};

export {};
