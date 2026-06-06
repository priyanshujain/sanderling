import assert from "node:assert/strict";
import { test } from "node:test";

// The web runtime is the WEB Host: it parses the injected seed, reports its
// platform, and delegates action generation to the shared picker. These tests
// guard the Host surface and the seed-precision contract. DOM candidate
// enumeration is exercised against a minimal querySelectorAll stub.

// A 64-bit seed that loses precision as a JS Number must survive as a BigInt.
process.env.SANDERLING_SEED = "9007199254740993";

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

// A button and a text input, each with a deterministic bounding box, exercise
// the per-verb selector routing without a full DOM.
function fakeElement(tag: string, rect: { x: number; y: number; w: number; h: number }) {
  return {
    tagName: tag.toUpperCase(),
    disabled: false,
    isContentEditable: false,
    type: tag === "input" ? "text" : "",
    scrollHeight: 0,
    clientHeight: 0,
    scrollWidth: 0,
    clientWidth: 0,
    getBoundingClientRect: () => ({
      left: rect.x,
      top: rect.y,
      width: rect.w,
      height: rect.h,
      right: rect.x + rect.w,
      bottom: rect.y + rect.h,
    }),
  };
}

function withFakeDocument(map: Record<string, unknown[]>, run: () => void) {
  const g = globalThis as Record<string, unknown>;
  const original = g.document;
  g.document = {
    querySelectorAll: (selector: string) => map[selector] ?? [],
    scrollingElement: null,
    documentElement: null,
  };
  try {
    run();
  } finally {
    g.document = original;
  }
}

test("queryCandidates routes taps to the tappable selector set", () => {
  const button = fakeElement("button", { x: 10, y: 20, w: 40, h: 8 });
  withFakeDocument(
    { 'a, button, input, select, textarea, [role="button"], [onclick]': [button] },
    () => {
      __testing__.resetCandidateCache();
      const candidates = host.queryCandidates("taps");
      assert.equal(candidates.length, 1);
      assert.deepEqual({ x: candidates[0]!.x, y: candidates[0]!.y }, { x: 30, y: 24 });
    },
  );
});

test("queryCandidates routes typing to editable inputs only", () => {
  const input = fakeElement("input", { x: 0, y: 0, w: 100, h: 20 });
  withFakeDocument({ "input, textarea, [contenteditable]": [input] }, () => {
    __testing__.resetCandidateCache();
    const candidates = host.queryCandidates("typing");
    assert.equal(candidates.length, 1);
    assert.deepEqual({ x: candidates[0]!.x, y: candidates[0]!.y }, { x: 50, y: 10 });
  });
});

test("queryCandidates caches within a tick until reset", () => {
  const first = fakeElement("button", { x: 0, y: 0, w: 10, h: 10 });
  withFakeDocument(
    { 'a, button, input, select, textarea, [role="button"], [onclick]': [first] },
    () => {
      __testing__.resetCandidateCache();
      const a = host.queryCandidates("taps");
      const b = host.queryCandidates("taps");
      assert.equal(a, b);
    },
  );
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

// sanitize runs over every extractor's return value before it leaves the
// runtime. A user extractor that returns a page object reachable from
// document/window can be self-referential, carry functions, or nest deeply;
// without cycle, function, and depth guards extraction overflows the stack or
// emits non-serializable values. These exercise sanitize via the real path.
function sanitizeViaExtract(value: unknown): unknown {
  __testing__.extractors.length = 0;
  __testing__.runtime.extract(() => value);
  let out: Record<number, unknown> = {};
  withState(() => {
    out = __testing__.evaluateExtractors();
  });
  return out[0];
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
