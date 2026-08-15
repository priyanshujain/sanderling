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

const SEED_HI = seedBigInt();

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

function selectorFromObject(selector: Record<string, string | boolean | undefined>): {
  css?: string;
  xpath?: string;
} {
  const parts: string[] = [];
  let textValue: string | undefined;
  let descPrefix: string | undefined;
  for (const key of Object.keys(selector)) {
    const raw = selector[key];
    if (raw === undefined) continue;
    const value = typeof raw === "boolean" ? String(raw) : raw;
    if (key === "text") {
      textValue = value;
      continue;
    }
    if (key === "descPrefix") {
      descPrefix = value;
      continue;
    }
    const builder = KNOWN_KEY_TO_CSS[key];
    if (builder) {
      parts.push(builder(value));
    } else {
      parts.push(`[${key}="${cssEscape(value)}"]`);
    }
  }
  if (descPrefix !== undefined) {
    parts.push(`[aria-label^="${cssEscape(descPrefix)}"]`);
  }
  if (textValue !== undefined && parts.length === 0) {
    return {
      xpath: `//*[normalize-space(text())=${xpathStringLiteral(textValue)}]`,
    };
  }
  return { css: parts.join("") };
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
  if (kind === "text") {
    return { xpath: `//*[normalize-space(text())=${xpathStringLiteral(value)}]` };
  }
  if (kind === "descPrefix") {
    return { css: `[aria-label^="${cssEscape(value)}"]` };
  }
  return selectorFromObject({ [kind]: value });
}

// deepQueryAll resolves a CSS selector against a root AND every shadow root
// beneath it. querySelectorAll stops dead at a shadow boundary, and a canvas app
// (Compose for Web mounts its canvas and its whole accessibility tree inside a
// shadow root on the mount element) keeps its entire UI on the far side of one:
// without this a spec sees four nodes and can neither enumerate a target nor
// resolve a testTag. Light-DOM matches come first, then shadow content in walk
// order. XPath has no equivalent, so `text:` selectors stop at the boundary.
function deepQueryAll(selector: string, root: ParentNode): Element[] {
  const found: Element[] = [];
  const visit = (scope: ParentNode): void => {
    for (const element of Array.from(scope.querySelectorAll(selector))) found.push(element);
    for (const element of Array.from(scope.querySelectorAll<HTMLElement>("*"))) {
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

function elementHandle(element: Element, selector: unknown): Record<string, unknown> {
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
  return {
    id: element.id,
    text,
    desc: ariaLabel,
    class: (element as HTMLElement).className ?? "",
    clickable: true,
    enabled: !(element as HTMLButtonElement).disabled,
    focused: document.activeElement === element,
    x,
    y,
    bounds: {
      left: Math.round(rect.left),
      top: Math.round(rect.top),
      right: Math.round(rect.right),
      bottom: Math.round(rect.bottom),
    },
    attrs: {
      tag: element.tagName.toLowerCase(),
      "aria-label": ariaLabel,
      ...datasetCopy,
    },
    dataset: datasetCopy,
    [SELECTOR_TAG]: selectorTag(selector),
    find(childSelector: unknown): unknown {
      const child = queryElement(element, childSelector);
      return child ? elementHandle(child, childSelector) : undefined;
    },
    findAll(childSelector: unknown): unknown[] {
      return queryAllElements(element, childSelector).map((child) =>
        elementHandle(child, childSelector),
      );
    },
  };
}

function buildAx(): unknown {
  return {
    find(selector: unknown): unknown {
      const element = queryElement(document, selector);
      return element ? elementHandle(element, selector) : undefined;
    },
    findAll(selector: unknown): unknown[] {
      return queryAllElements(document, selector).map((element) =>
        elementHandle(element, selector),
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

function buildState(): unknown {
  return {
    snapshots: {},
    ax: buildAx(),
    document,
    window,
    lastAction,
    time: 0,
    logs: [],
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
  const editable = new Set<Element>(
    (deepQueryAll(EDITABLE_SELECTOR, document) as HTMLElement[]).filter(isEditableElement),
  );
  const elements = targetElements();
  const selectors = selectorsFor(elements);
  return elements.map((element, index) => ({
    ...pointOf(element),
    selector: selectors[index],
    clickable: clickable.has(element),
    enabled: !(element as HTMLButtonElement).disabled,
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
  seedHi: () => SEED_HI,
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
  selectorTag,
  xpathStringLiteral,
};

export {};
