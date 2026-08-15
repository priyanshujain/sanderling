import assert from "node:assert/strict";
import { test } from "node:test";

// The web runtime is the WEB Host: it parses the injected seed, reports its
// platform, and delegates action generation to the shared picker. These tests
// guard the Host surface and the seed-precision contract. DOM candidate
// enumeration is exercised against a minimal querySelectorAll stub.

// A 64-bit seed that loses precision as a JS Number must survive as a BigInt.
process.env.SANDERLING_SEED = "9007199254740993";

// cssEscape delegates to the browser's CSS.escape, absent in node. Install the
// WHATWG CSSOM escape algorithm so selector-builder tests exercise the real
// escaping production relies on, not a stub.
if (!(globalThis as { CSS?: unknown }).CSS) {
  (globalThis as { CSS?: { escape(v: string): string } }).CSS = {
    escape(value: string): string {
      let out = "";
      for (let i = 0; i < value.length; i++) {
        const c = value.charCodeAt(i);
        if (c === 0) {
          out += "�";
        } else if (
          (c >= 0x1 && c <= 0x1f) ||
          c === 0x7f ||
          (i === 0 && c >= 0x30 && c <= 0x39) ||
          (i === 1 && c >= 0x30 && c <= 0x39 && value.charCodeAt(0) === 0x2d)
        ) {
          out += "\\" + c.toString(16) + " ";
        } else if (i === 0 && c === 0x2d && value.length === 1) {
          out += "\\" + value[i];
        } else if (
          c >= 0x80 ||
          c === 0x2d ||
          c === 0x5f ||
          (c >= 0x30 && c <= 0x39) ||
          (c >= 0x41 && c <= 0x5a) ||
          (c >= 0x61 && c <= 0x7a)
        ) {
          out += value[i];
        } else {
          out += "\\" + value[i];
        }
      }
      return out;
    },
  };
}

const { __testing__ } = await import("../src/web-runtime.ts");
const { host } = __testing__;

test("platform is web", () => {
  assert.equal(host.platform(), "web");
});

test("seedHi parses the injected 64-bit seed without Number precision loss", () => {
  assert.equal(host.seedHi(), 9007199254740993n);
});

test("seedLo is 0 to match goja rand.NewPCG(seed, 0)", () => {
  assert.equal(host.seedLo(), 0n);
});

test("reportUnsupported warns once via console.warn", () => {
  const original = console.warn;
  const messages: string[] = [];
  console.warn = (m: string) => messages.push(m);
  try {
    host.reportUnsupported("swipes");
  } finally {
    console.warn = original;
  }
  assert.equal(messages.length, 1);
  assert.match(messages[0]!, /swipes/);
});

// installRuntime locked the next-action and extractor globals at import.
test("installRuntime defined the host-invoked globals", () => {
  const g = globalThis as Record<string, unknown>;
  assert.equal(typeof g.__sanderlingNextAction__, "function");
  assert.equal(typeof g.__sanderlingExtractors__, "function");
  assert.equal(typeof g.__sanderling__, "object");
});

const { fakeElement, withFakeDocument } = await import("./web-dom-harness.ts");
type FakeElementSpec = Parameters<typeof fakeElement>[0];

// The host reports facts and never routes verbs: which of these a verb may act
// on is decided by the shared rule in src/targets.ts, exercised across both
// engines by host-parity.test.ts.
test("queryTargets reports the tappable selector set as clickable", () => {
  const button = fakeElement({ tag: "button", x: 10, y: 20, width: 40, height: 8, clickable: true });
  const plain = fakeElement({ tag: "div", x: 0, y: 0, width: 100, height: 100 });
  withFakeDocument([button, plain], () => {
    const targets = host.queryTargets();
    assert.equal(targets.length, 2);
    assert.equal(targets[0]!.clickable, true);
    assert.deepEqual({ x: targets[0]!.x, y: targets[0]!.y }, { x: 30, y: 24 });
    assert.equal(targets[1]!.clickable, false);
  });
});

test("queryTargets reports only real text inputs as editable", () => {
  const input = fakeElement({ tag: "input", x: 0, y: 0, width: 100, height: 20, editable: true });
  const checkbox = fakeElement({ tag: "input", x: 0, y: 40, width: 20, height: 20, editable: true });
  checkbox.type = "checkbox";
  withFakeDocument([input, checkbox], () => {
    const targets = host.queryTargets();
    assert.equal(targets[0]!.editable, true);
    assert.deepEqual({ x: targets[0]!.x, y: targets[0]!.y }, { x: 50, y: 10 });
    assert.equal(targets[1]!.editable, false);
  });
});

// A disabled control used to be dropped from every verb's candidates, because
// the web host folded `disabled` into its visibility check. It is a fact of its
// own now, so `taps` still skips it while `swipes` can still start on it, which
// is what the native host has always done.
test("queryTargets reports a disabled control rather than dropping it", () => {
  const disabled = fakeElement({
    tag: "button", x: 0, y: 0, width: 40, height: 20, clickable: true, disabled: true,
  });
  withFakeDocument([disabled], () => {
    const targets = host.queryTargets();
    assert.equal(targets.length, 1);
    assert.equal(targets[0]!.clickable, true);
    assert.equal(targets[0]!.enabled, false);
  });
});

// A target with no selector is a target no property can name. The action the
// picker builds from it carries coordinates only, so `lastAction.on` is empty
// and any property matching on WHICH control was acted upon cannot fire.
test("queryTargets names a uniquely identified target", () => {
  const submit = fakeElement({
    tag: "button", x: 0, y: 0, width: 40, height: 20, clickable: true, id: "TxnSubmit",
  });
  const byTestid = fakeElement({
    tag: "button", x: 0, y: 40, width: 40, height: 20, clickable: true, testid: "cancel",
  });
  const byLabel = fakeElement({
    tag: "button", x: 0, y: 80, width: 40, height: 20, clickable: true, label: "Close",
  });
  // alt and title are the fallbacks the hierarchy dump folds into content-desc,
  // so the host has to fall back to them in the same order or a name it calls
  // unique resolves to a different element on the Go side.
  const byAlt = fakeElement({ tag: "img", x: 0, y: 120, width: 40, height: 20, alt: "Logo" });
  const byTitle = fakeElement({ tag: "div", x: 0, y: 160, width: 40, height: 20, title: "Help" });
  const anonymous = fakeElement({ tag: "div", x: 0, y: 200, width: 10, height: 10 });
  withFakeDocument([submit, byTestid, byLabel, byAlt, byTitle, anonymous], () => {
    const targets = host.queryTargets();
    assert.equal(targets[0]!.selector, "id:TxnSubmit");
    assert.equal(targets[1]!.selector, "data-testid:cancel");
    assert.equal(targets[2]!.selector, "desc:Close");
    assert.equal(targets[3]!.selector, "desc:Logo");
    assert.equal(targets[4]!.selector, "desc:Help");
    assert.equal(targets[5]!.selector, undefined);
  });
});

// A repeated id (folio's Home screen renders one AccountCard testTag per
// account) names no single element, so the runner would re-resolve the action
// onto whichever sibling it found first. Better unnamed than mis-aimed.
test("queryTargets leaves duplicated identities unnamed", () => {
  const first = fakeElement({
    tag: "div", x: 0, y: 0, width: 40, height: 20, clickable: true, id: "AccountCard",
  });
  const second = fakeElement({
    tag: "div", x: 0, y: 40, width: 40, height: 20, clickable: true, id: "AccountCard",
  });
  withFakeDocument([first, second], () => {
    const targets = host.queryTargets();
    assert.equal(targets[0]!.selector, undefined);
    assert.equal(targets[1]!.selector, undefined);
  });
});

// The enumeration ORDER is the parity contract. buildTree in
// internal/driver/chrome/driver.go emits a host's shadow children before its
// light ones, and TestHierarchy_DerivesTheSameFactsAsTheWebRuntime compares the
// two enumerations position by position.
test("queryTargets splices shadow content in before the host's light children", () => {
  const page = fakeElement({
    tag: "div", x: 0, y: 0, width: 400, height: 800, id: "page",
    children: [
      {
        tag: "div", x: 0, y: 0, width: 400, height: 100, id: "mount",
        shadow: [
          { tag: "button", x: 0, y: 0, width: 40, height: 20, id: "shadow-save", clickable: true },
        ],
        children: [{ tag: "div", x: 0, y: 20, width: 40, height: 20, id: "mount-light-child" }],
      },
      { tag: "div", x: 0, y: 100, width: 400, height: 100, id: "after" },
    ],
  });
  withFakeDocument([page], () => {
    assert.deepEqual(
      host.queryTargets().map((target) => target.selector),
      ["id:page", "id:mount", "id:shadow-save", "id:mount-light-child", "id:after"],
    );
  });
});

// The tappable set is resolved by selector, and querySelectorAll stops dead at
// a shadow boundary, so a control inside a shadow root carries the clickable
// fact only if the selector sweep descends. A Compose for Web app keeps every
// control it has on the far side of one boundary.
test("queryTargets reports a shadow-hosted control as clickable", () => {
  const mount = fakeElement({
    tag: "div", x: 0, y: 0, width: 400, height: 100, id: "mount",
    shadow: [
      { tag: "button", x: 0, y: 0, width: 40, height: 20, id: "shadow-save", clickable: true },
      { tag: "input", x: 0, y: 20, width: 40, height: 20, id: "shadow-amount", editable: true },
    ],
  });
  withFakeDocument([mount], () => {
    const targets = host.queryTargets();
    assert.deepEqual(
      targets.map((target) => target.selector),
      ["id:mount", "id:shadow-save", "id:shadow-amount"],
    );
    assert.equal(targets[1]!.clickable, true);
    assert.equal(targets[2]!.editable, true);
  });
});

test("queryTargets caches within a tick until reset", () => {
  const button = fakeElement({ tag: "button", x: 0, y: 0, width: 10, height: 10, clickable: true });
  withFakeDocument([button], () => {
    const first = host.queryTargets();
    const second = host.queryTargets();
    assert.equal(first, second);
    __testing__.resetTargetCache();
    assert.notEqual(host.queryTargets(), first);
  });
});

// evaluateExtractors builds State, which references document and window.
const emptyDocument = {
  querySelector: () => null,
  querySelectorAll: () => [],
};

function withState(run: () => void) {
  const g = globalThis as Record<string, unknown>;
  const originalDocument = g.document;
  const originalWindow = g.window;
  g.document = emptyDocument;
  g.window = {};
  try {
    run();
  } finally {
    g.document = originalDocument;
    g.window = originalWindow;
  }
}

// Every reading leaves the runtime inside a {value} envelope, so an extractor
// whose getter returned undefined keeps its index instead of being dropped by
// JSON.stringify.
function readingOf(values: Record<number, { value?: unknown }>, index: number): unknown {
  return values[index]!.value;
}

test("named() sets the extractor's display name", () => {
  const handle = __testing__.runtime.extract(() => "home").named("route");
  const entry = __testing__.extractors.find((e) => e.handle === handle);
  assert.equal(entry?.name, "route");
});

test("reading another extractor's current inside a getter throws", () => {
  const first = __testing__.runtime.extract(() => 1);
  let caught: Error | undefined;
  __testing__.runtime.extract(() => {
    try {
      return first.current;
    } catch (error) {
      caught = error as Error;
      return undefined;
    }
  });
  withState(() => __testing__.evaluateExtractors());
  assert.match(
    caught?.message ?? "",
    /inside another extractor is not allowed/,
  );
});

// An uncaught cross-extractor read must abort evaluateExtractors loudly, exactly
// like goja's PushSnapshot. If the getter throw were swallowed, web would
// silently yield undefined and the guard would be a no-op for real authors.
test("an uncaught cross-extractor read aborts evaluateExtractors", () => {
  __testing__.extractors.length = 0;
  const first = __testing__.runtime.extract(() => 1);
  __testing__.runtime.extract(() => first.previous);
  let thrown: Error | undefined;
  withState(() => {
    try {
      __testing__.evaluateExtractors();
    } catch (error) {
      thrown = error as Error;
    }
  });
  assert.match(
    thrown?.message ?? "",
    /inside another extractor is not allowed/,
  );
});

// JSON.stringify drops an undefined-valued key, so a reading written straight
// into the table took the extractor's whole INDEX with it when the getter
// returned undefined - folio's on(route, tag) off its own screen, which is most
// of its extractors on most steps. The host then kept goja's dump-derived value
// for those and the page's for the rest, and a property comparing previous to
// current across that split convicts an app that did nothing wrong.
test("an extractor that returned undefined keeps its index through JSON", () => {
  __testing__.extractors.length = 0;
  __testing__.runtime.extract(() => undefined);
  __testing__.runtime.extract(() => null);
  __testing__.runtime.extract(() => 5);
  let table: Record<number, { value?: unknown }> = {};
  withState(() => {
    table = __testing__.evaluateExtractors();
  });

  const overTheWire = JSON.parse(JSON.stringify(table)) as Record<string, { value?: unknown }>;
  assert.deepEqual(Object.keys(overTheWire), ["0", "1", "2"]);
  // undefined and null have to stay distinguishable across the wire: the goja
  // host records undefined for a getter that returned undefined, so reporting
  // null instead would make `x.current === undefined` answer one thing on
  // native and another on web.
  assert.equal("value" in overTheWire["0"]!, false);
  assert.equal(overTheWire["1"]!.value, null);
  assert.equal(overTheWire["2"]!.value, 5);
});

// state.lastAction is the one piece of state the page cannot observe for
// itself: only the runner knows which action it actually applied. While the web
// runtime hardcoded null there, a spec property gated on the last action (e.g.
// folio's submitMovesBalanceByTypedAmount, which only looks at taps on
// TxnSubmit) was vacuously true on web forever, and the run went green having
// checked nothing.
function lastActionSeenByASpec(pushed: unknown): unknown {
  const setLastAction = (globalThis as Record<string, unknown>)
    .__sanderlingSetLastAction__ as (value: unknown) => void;
  __testing__.extractors.length = 0;
  __testing__.runtime.extract((state) => (state as { lastAction: unknown }).lastAction);
  let out: Record<number, { value?: unknown }> = {};
  withState(() => {
    setLastAction(pushed);
    out = __testing__.evaluateExtractors();
  });
  return readingOf(out, 0);
}

test("state.lastAction carries the action the host pushed", () => {
  const action = { kind: "Tap", on: "id:TxnSubmit" };
  assert.deepEqual(lastActionSeenByASpec(action), action);
});

test("state.lastAction is null when the host pushed nothing", () => {
  // The first step of a run, and any step whose action was never applied: the
  // goja host reports null there, so the web host must too.
  assert.equal(lastActionSeenByASpec(null), null);
});

// sanitize runs over every extractor's return value before it leaves the
// runtime. A user extractor that returns a page object reachable from
// document/window can be self-referential, carry functions, or nest deeply;
// without cycle, function, and depth guards extraction overflows the stack or
// emits non-serializable values. These exercise sanitize via the real path.
function sanitizeViaExtract(value: unknown): unknown {
  __testing__.extractors.length = 0;
  __testing__.runtime.extract(() => value);
  let out: Record<number, { value?: unknown }> = {};
  withState(() => {
    out = __testing__.evaluateExtractors();
  });
  return readingOf(out, 0);
}

test("sanitize breaks a self-referential cycle instead of overflowing", () => {
  const cyclic: Record<string, unknown> = { name: "root" };
  cyclic.self = cyclic;
  const result = sanitizeViaExtract(cyclic) as Record<string, unknown>;
  assert.equal(result.name, "root");
  assert.equal(result.self, null);
});

test("sanitize drops function-valued properties", () => {
  const result = sanitizeViaExtract({ keep: 1, fn: () => 7 }) as Record<string, unknown>;
  assert.deepEqual(result, { keep: 1 });
});

test("sanitize drops a top-level function to undefined", () => {
  assert.equal(sanitizeViaExtract(() => 7), undefined);
});

test("sanitize bounds recursion past its depth limit", () => {
  let deep: Record<string, unknown> = { leaf: true };
  for (let i = 0; i < 40; i++) deep = { next: deep };
  // Walk to the depth cap; beyond it sanitize must yield null, not recurse on.
  let node: unknown = sanitizeViaExtract(deep);
  for (let i = 0; i < 32 && node && typeof node === "object"; i++) {
    node = (node as Record<string, unknown>).next;
  }
  assert.equal(node, null);
});

test("sanitize preserves arrays and nested plain values", () => {
  const result = sanitizeViaExtract({ items: [1, "two", { ok: true }] });
  assert.deepEqual(result, { items: [1, "two", { ok: true }] });
});

// xpathStringLiteral builds XPath 1.0 string literals by hand (no escape
// syntax in XPath 1.0). A value carrying a quote that isn't wrapped or
// concat()-composed produces a malformed expression, so document.evaluate
// throws or, worse, matches the wrong node by truncating at the quote.
const { xpathStringLiteral } = __testing__;

test("xpathStringLiteral table: quote handling stays well-formed", () => {
  const cases: Array<[string, string]> = [
    ["plain", '"plain"'],
    ['has"double', `'has"double'`],
    ["has'single", `"has'single"`],
    [`both"and'`, `concat("both", '"', "and'")`],
    [`"`, `'"'`],
  ];
  for (const [input, want] of cases) {
    assert.equal(xpathStringLiteral(input), want, input);
  }
});

// selectorFromString routes a "kind:value" prefix; text becomes an XPath
// equality, everything else a CSS attribute selector. A value containing a
// colon must not be re-split, and a quote in a text value must reach the
// well-formed XPath literal rather than corrupting the predicate.
const { selectorFromString, selectorFromObject } = __testing__;

test("selectorFromString routes text to a normalize-space XPath", () => {
  assert.deepEqual(selectorFromString("text:Hello"), {
    xpath: `//*[normalize-space(text())="Hello"]`,
  });
});

test("selectorFromString keeps colons in the value intact", () => {
  // Only the first colon splits kind from value; the rest is the value.
  assert.deepEqual(selectorFromString("text:a:b:c"), {
    xpath: `//*[normalize-space(text())="a:b:c"]`,
  });
});

test("selectorFromString text value with both quote kinds uses concat", () => {
  assert.deepEqual(selectorFromString(`text:say "hi" o'clock`), {
    xpath: `//*[normalize-space(text())=concat("say ", '"', "hi", '"', " o'clock")]`,
  });
});

test("selectorFromObject escapes attribute values to prevent injection", () => {
  // A quote in an id value, left unescaped, would close the attribute selector
  // early and match a different element.
  assert.deepEqual(selectorFromObject({ id: 'a"]' }), {
    css: `[id="a\\"\\]"]`,
  });
});

test("selectorFromObject maps known keys to their canonical attribute", () => {
  assert.deepEqual(selectorFromObject({ testID: "submit" }), {
    css: `[data-testid="submit"]`,
  });
});

// Compose Multiplatform emits its testTag into `id`, which the native table
// already accepts via the resource-id alias. The web table must not be the one
// place that rejects it.
test("selectorFromObject resolves testTag through data-testid or id", () => {
  assert.deepEqual(selectorFromObject({ testTag: "LoginSubmit" }), {
    css: `:is([data-testid="LoginSubmit"], [id="LoginSubmit"])`,
  });
});

// Multi-key selectors concatenate their parts into one compound, so the
// two-attribute testTag match has to stay a single compound piece.
test("selectorFromObject composes testTag with a second key", () => {
  assert.deepEqual(selectorFromObject({ testTag: "Row", "aria-label": "first" }), {
    css: `:is([data-testid="Row"], [id="Row"])[aria-label="first"]`,
  });
});

test("selectorFromObject falls back to a literal attribute for unknown keys", () => {
  assert.deepEqual(selectorFromObject({ "data-foo": "bar" }), {
    css: `[data-foo="bar"]`,
  });
});

test("selectorFromObject text-only selector becomes an XPath", () => {
  assert.deepEqual(selectorFromObject({ text: "Go" }), {
    xpath: `//*[normalize-space(text())="Go"]`,
  });
});

// An ax element is labelled with the selector it was found by, in the same
// canonical grammar selectorStringFromJS emits in internal/verifier/marshal.go.
// The label is what a spec's own Tap({ on: state.ax.find(...) }) carries to the
// runner: with no label the action is coordinates only, `lastAction.on` is
// empty, and a property matching on WHICH control was tapped cannot fire.
const { selectorTag } = __testing__;

test("selectorTag renders the selector shapes the goja host renders", () => {
  assert.equal(selectorTag("testTag:TxnSubmit"), "testTag:TxnSubmit");
  assert.equal(selectorTag({ testTag: "TxnSubmit" }), "testTag:TxnSubmit");
  assert.equal(
    selectorTag([{ testTag: "AddTransactionScreen" }, { testTag: "TxnSubmit" }]),
    "testTag:AddTransactionScreen > testTag:TxnSubmit",
  );
  assert.equal(selectorTag({ testTag: "Row", "aria-label": "first" }), "testTag:Row aria-label:first");
  assert.equal(selectorTag(undefined), "");
});

// A selector path scopes the second segment to each match of the first. It
// returned nothing at all on web while returning matches on native, so folio's
// accounts/totalBalance extractors (findAll([{HomeScreen}, {AccountCard}]))
// were empty on every web step and the properties over them checked nothing.
test("ax.findAll resolves a selector path segment by segment", () => {
  const card = (id: string, y: number): FakeElementSpec => ({
    tag: "div", x: 0, y, width: 10, height: 10, testid: "AccountCard", text: id,
  });
  // The stray card is outside HomeScreen, so a document-wide sweep for the
  // second segment picks it up and the scoping assertion below fails.
  const page = fakeElement({
    tag: "div", x: 0, y: 0, width: 100, height: 100,
    children: [
      {
        tag: "div", x: 0, y: 0, width: 100, height: 50, testid: "HomeScreen",
        children: [card("first", 0), card("second", 10)],
      },
      card("stray", 60),
    ],
  });

  withFakeDocument([page], () => {
    __testing__.extractors.length = 0;
    __testing__.runtime.extract((state) => {
      const ax = (state as { ax: { findAll(s: unknown): Record<string, unknown>[] } }).ax;
      return ax
        .findAll([{ testTag: "HomeScreen" }, { testTag: "AccountCard" }])
        .map((card) => card.text);
    });
    const values = __testing__.evaluateExtractors();
    // Scoped to the head match: the cards come from the HomeScreen node, not
    // from a document-wide sweep for AccountCard.
    assert.deepEqual(readingOf(values, 0), ["first", "second"]);
  });
});

test("ax.find and ax.findAll label the element with its selector", () => {
  const submit = fakeElement({
    tag: "div", x: 0, y: 0, width: 10, height: 10, id: "TxnSubmit", text: "Submit",
  });
  withFakeDocument([submit], () => {
    __testing__.extractors.length = 0;
    __testing__.runtime.extract((state) => {
      const ax = (state as { ax: { find(s: unknown): Record<string, unknown> | undefined } }).ax;
      return ax.find({ testTag: "TxnSubmit" });
    });
    __testing__.runtime.extract((state) => {
      const ax = (state as { ax: { findAll(s: unknown): Record<string, unknown>[] } }).ax;
      return ax.findAll({ testTag: "TxnSubmit" });
    });
    const values = __testing__.evaluateExtractors();
    const found = readingOf(values, 0) as Record<string, unknown>;
    assert.equal(found.__sanderlingSelector, "testTag:TxnSubmit");
    // findAll passes each element through map(); passing the callback by
    // reference would hand the array INDEX to the runtime as the selector.
    const all = readingOf(values, 1) as Record<string, unknown>[];
    assert.equal(all[0]!.__sanderlingSelector, "testTag:TxnSubmit");
  });
});

// One page, one selector, two hosts. The goja host resolves a selector against
// the hierarchy dump, whose buildTree (internal/driver/chrome/driver.go) emits
// a host's shadow children BEFORE its light ones, so a pre-order search there
// reaches a shadow-hosted match first. deepQueryAll swept the whole light DOM
// first and only then descended, so this page answered find({id:"x"}) with the
// light node in V8 and the shadow node in goja, and on web V8's answer is the
// one that reaches the properties.
test("ax.find resolves the shadow-hosted match the hierarchy dump reaches first", () => {
  const page = fakeElement({
    tag: "div", x: 0, y: 0, width: 400, height: 800, id: "page",
    children: [
      {
        tag: "div", x: 0, y: 0, width: 400, height: 100, id: "mount",
        shadow: [{ tag: "span", x: 0, y: 0, width: 40, height: 20, id: "x", text: "shadow" }],
      },
      { tag: "span", x: 0, y: 100, width: 40, height: 20, id: "x", text: "light" },
    ],
  });
  withFakeDocument([page], () => {
    __testing__.extractors.length = 0;
    __testing__.runtime.extract((state) => {
      const ax = (state as { ax: { find(s: unknown): Record<string, unknown> | undefined } }).ax;
      return ax.find({ id: "x" })?.text;
    });
    __testing__.runtime.extract((state) => {
      const ax = (state as { ax: { findAll(s: unknown): Record<string, unknown>[] } }).ax;
      return ax.findAll({ id: "x" }).map((element) => element.text);
    });
    const values = __testing__.evaluateExtractors();
    assert.equal(readingOf(values, 0), "shadow");
    assert.deepEqual(readingOf(values, 1), ["shadow", "light"]);
  });
});
