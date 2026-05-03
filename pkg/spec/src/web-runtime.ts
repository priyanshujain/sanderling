/// <reference lib="dom" />

// V8-side runtime for `sanderling test --platform web`.
//
// The user spec is bundled with this file as the first import so that
// globalThis.__sanderling__ is installed before the spec evaluates. The host
// invokes window.__sanderlingExtractors__() and window.__sanderlingNextAction__()
// over CDP each tick. LTL predicates are intentionally stubbed: properties
// run host-side in goja, which loads its own bundle of the same spec.
//
// Element references never cross V8/host. Action targets that reference an
// AccessibilityElement collapse to `{x, y}` from getBoundingClientRect()
// before serialization.

interface Handle {
  current: unknown;
  previous: unknown;
}

interface ExtractorEntry {
  getter: (state: unknown) => unknown;
  handle: Handle;
}

interface ActionGeneratorHandle {
  __sanderlingActionGenerator: true;
  __sanderlingKind: string;
  generate?: () => unknown;
  entries?: readonly [number, ActionGeneratorHandle][];
}

const extractors: ExtractorEntry[] = [];
let actionsRoot: ActionGeneratorHandle | null = null;

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
  testTag: (v) => `[data-testid="${cssEscape(v)}"]`,
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

function queryElement(
  root: ParentNode,
  selector: unknown,
): Element | null {
  if (typeof selector === "string") {
    const { css, xpath } = selectorFromString(selector);
    if (css) return root.querySelector(css);
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
    if (css) return root.querySelector(css);
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
    if (css) return Array.from(root.querySelectorAll(css));
    if (xpath) return evaluateXPathAll(xpath, root as Node);
    return [];
  }
  if (selector && typeof selector === "object" && !Array.isArray(selector)) {
    const { css, xpath } = selectorFromObject(selector as Record<string, string | boolean | undefined>);
    if (css) return Array.from(root.querySelectorAll(css));
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

function elementHandle(element: Element): Record<string, unknown> {
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
    find(selector: unknown): unknown {
      const child = queryElement(element, selector);
      return child ? elementHandle(child) : undefined;
    },
    findAll(selector: unknown): unknown[] {
      return queryAllElements(element, selector).map(elementHandle);
    },
  };
}

function buildAx(): unknown {
  return {
    find(selector: unknown): unknown {
      const element = queryElement(document, selector);
      return element ? elementHandle(element) : undefined;
    },
    findAll(selector: unknown): unknown[] {
      return queryAllElements(document, selector).map(elementHandle);
    },
  };
}

function buildState(): unknown {
  return {
    snapshots: {},
    ax: buildAx(),
    document,
    window,
    lastAction: null,
    time: 0,
    logs: [],
    exceptions: [],
  };
}

const runtime = {
  extract<T>(getter: (state: unknown) => T): Handle {
    const handle: Handle = { current: undefined, previous: undefined };
    extractors.push({ getter: getter as (s: unknown) => unknown, handle });
    return handle;
  },
  always: noopFormula,
  now: noopFormula,
  next: noopFormula,
  eventually: noopFormula,
  actions(generator: () => unknown): ActionGeneratorHandle {
    const handle: ActionGeneratorHandle = {
      __sanderlingActionGenerator: true,
      __sanderlingKind: "actions",
      generate: generator,
    };
    if (!actionsRoot) actionsRoot = handle;
    return handle;
  },
  weighted(...entries: [number, ActionGeneratorHandle][]): ActionGeneratorHandle {
    const handle: ActionGeneratorHandle = {
      __sanderlingActionGenerator: true,
      __sanderlingKind: "weighted",
      entries,
    };
    actionsRoot = handle;
    return handle;
  },
  from<T>(items: readonly T[]): { generate: () => T | undefined } {
    return {
      generate(): T | undefined {
        if (items.length === 0) return undefined;
        return items[Math.floor(Math.random() * items.length)];
      },
    };
  },
  tap(p: { on: unknown }): unknown {
    return { kind: "Tap", on: p.on };
  },
  inputText(p: { into: unknown; text: string }): unknown {
    return { kind: "InputText", into: p.into, text: p.text };
  },
  swipe(_p: { from: unknown; to: unknown; durationMillis?: number }): unknown {
    // Why: web has no swipe gesture; the factory returns null so Swipe() calls in specs no-op.
    return null;
  },
  pressKey(p: { key: string }): unknown {
    return { kind: "PressKey", key: p.key };
  },
  wait(p: { durationMillis: number }): unknown {
    return { kind: "Wait", durationMillis: p.durationMillis };
  },
  taps: { __sanderlingActionGenerator: true, __sanderlingKind: "taps" } as ActionGeneratorHandle,
  swipes: { __sanderlingActionGenerator: true, __sanderlingKind: "swipes" } as ActionGeneratorHandle,
  waitOnce: { __sanderlingActionGenerator: true, __sanderlingKind: "waitOnce" } as ActionGeneratorHandle,
  pressKeys: { __sanderlingActionGenerator: true, __sanderlingKind: "pressKey" } as ActionGeneratorHandle,
};

// Lock the runtime globals so a misbehaving (or malicious) page script can't
// shadow or replace them between AddScriptToEvaluateOnNewDocument running and
// the host invoking the extractor/next-action callbacks.
defineLockedGlobal("__sanderling__", runtime);

function defineLockedGlobal(name: string, value: unknown): void {
  Object.defineProperty(globalThis, name, {
    value,
    writable: false,
    configurable: false,
    enumerable: false,
  });
}

function evaluateExtractors(): Record<number, unknown> {
  const state = buildState();
  const result: Record<number, unknown> = {};
  for (let i = 0; i < extractors.length; i++) {
    const entry = extractors[i];
    if (!entry) continue;
    entry.handle.previous = entry.handle.current;
    let value: unknown;
    try {
      value = entry.getter(state);
    } catch {
      value = undefined;
    }
    entry.handle.current = value;
    result[i] = sanitize(value);
  }
  return result;
}

function sanitize(value: unknown): unknown {
  if (value === null || value === undefined) return value;
  if (typeof value === "function") return undefined;
  if (Array.isArray(value)) return value.map(sanitize);
  if (typeof value === "object") {
    const out: Record<string, unknown> = {};
    for (const key of Object.keys(value as Record<string, unknown>)) {
      const sub = (value as Record<string, unknown>)[key];
      if (typeof sub === "function") continue;
      out[key] = sanitize(sub);
    }
    return out;
  }
  return value;
}

function pickWeighted(handle: ActionGeneratorHandle): ActionGeneratorHandle | null {
  const entries = handle.entries ?? [];
  if (entries.length === 0) return null;
  let total = 0;
  for (const [weight] of entries) total += Math.max(0, weight);
  if (total <= 0) return null;
  let pick = Math.random() * total;
  for (const [weight, generator] of entries) {
    pick -= Math.max(0, weight);
    if (pick <= 0) return generator;
  }
  return entries[entries.length - 1]?.[1] ?? null;
}

function resolveGenerator(handle: ActionGeneratorHandle): unknown {
  switch (handle.__sanderlingKind) {
    case "actions": {
      const generated = handle.generate?.();
      return pickFromArray(generated);
    }
    case "weighted": {
      const inner = pickWeighted(handle);
      if (!inner) return null;
      return resolveGenerator(inner);
    }
    case "taps":
      return randomTap();
    case "swipes":
      return randomSwipe();
    case "waitOnce":
      return { kind: "Wait", durationMillis: 500 };
    case "pressKey":
      return randomPressKey();
    default:
      return null;
  }
}

function randomTap(): unknown {
  const candidates = Array.from(
    document.querySelectorAll<HTMLElement>(
      'a, button, input, select, textarea, [role="button"], [onclick]',
    ),
  ).filter((element) => {
    if ((element as HTMLButtonElement).disabled) return false;
    const rect = element.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0;
  });
  if (candidates.length === 0) return null;
  const picked = candidates[Math.floor(Math.random() * candidates.length)];
  if (!picked) return null;
  const rect = picked.getBoundingClientRect();
  return {
    kind: "Tap",
    on: {
      x: Math.round(rect.left + rect.width / 2),
      y: Math.round(rect.top + rect.height / 2),
    },
  };
}

function randomSwipe(): unknown {
  // Why: web has no swipe gesture; pointer drags into empty divs are noise.
  return null;
}

const WEB_PRESS_KEYS = ["enter", "tab", "escape", "up", "down", "left", "right"];

function randomPressKey(): unknown {
  // Why: only emit keys with meaningful browser semantics; "back"/"home" don't navigate.
  const key = WEB_PRESS_KEYS[Math.floor(Math.random() * WEB_PRESS_KEYS.length)];
  return { kind: "PressKey", key };
}

function pickFromArray(value: unknown): unknown {
  if (!value) return null;
  if (!Array.isArray(value)) return value;
  if (value.length === 0) return null;
  return value[Math.floor(Math.random() * value.length)];
}

function serializeAction(action: unknown): unknown {
  if (!action || typeof action !== "object") return null;
  const obj = action as Record<string, unknown>;
  switch (obj.kind) {
    case "Tap": {
      const point = pointOf(obj.on);
      if (!point) {
        console.warn("[sanderling] Tap target did not resolve to coordinates");
        return null;
      }
      return { kind: "Tap", x: point.x, y: point.y };
    }
    case "InputText": {
      const point = pointOf(obj.into);
      if (!point) {
        console.warn("[sanderling] InputText target did not resolve to coordinates");
        return null;
      }
      return {
        kind: "InputText",
        x: point.x,
        y: point.y,
        text: obj.text ?? "",
      };
    }
    case "Swipe": {
      const from = pointOf(obj.from);
      const to = pointOf(obj.to);
      if (!from || !to) {
        console.warn("[sanderling] Swipe endpoints did not resolve to coordinates");
        return null;
      }
      return {
        kind: "Swipe",
        from_x: from.x,
        from_y: from.y,
        to_x: to.x,
        to_y: to.y,
        duration_millis: obj.durationMillis ?? 250,
      };
    }
    case "PressKey":
      return { kind: "PressKey", key: obj.key };
    case "Wait":
      return { kind: "Wait", duration_millis: obj.durationMillis ?? 0 };
    default:
      return null;
  }
}

function pointOf(value: unknown): { x: number; y: number } | undefined {
  if (!value || typeof value !== "object") return undefined;
  const obj = value as Record<string, unknown>;
  if (typeof obj.x === "number" && typeof obj.y === "number") {
    return { x: obj.x, y: obj.y };
  }
  return undefined;
}

defineLockedGlobal("__sanderlingExtractors__", function (): Record<number, unknown> {
  return evaluateExtractors();
});

defineLockedGlobal("__sanderlingNextAction__", function (): unknown {
  if (!actionsRoot) return null;
  // Match the goja runtime: retry up to 16 times when a weighted entry's
  // generator returns []. Otherwise on routes where most generators are
  // gated to other pages, ~80% of ticks would emit no action.
  for (let attempt = 0; attempt < 16; attempt++) {
    const action = serializeAction(resolveGenerator(actionsRoot));
    if (action !== null) return action;
  }
  return null;
});

export {};
