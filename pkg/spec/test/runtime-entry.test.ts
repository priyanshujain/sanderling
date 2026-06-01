import assert from "node:assert/strict";
import { test } from "node:test";

import { serializeAction } from "../src/runtime-entry.ts";
import type { ActionDescriptor } from "../src/action-tree.ts";

// decodeWire mirrors the Go decodeAction reading the unified flat contract: a
// field rename (fromX vs from_x) would make these assertions fail here, the
// same way it would silently no-op web actions in production.
const POINT = { x: 12, y: 34 };

test("Tap serializes to the flat point contract", () => {
  assert.deepEqual(serializeAction({ kind: "Tap", on: POINT }), {
    kind: "Tap",
    x: 12,
    y: 34,
  });
});

test("Tap carries a resolved selector when present", () => {
  const target = { x: 1, y: 2, selector: "id:save" };
  assert.deepEqual(serializeAction({ kind: "Tap", on: target }), {
    kind: "Tap",
    x: 1,
    y: 2,
    selector: "id:save",
  });
});

test("DoubleTap and LongPress share the point contract", () => {
  assert.deepEqual(serializeAction({ kind: "DoubleTap", on: POINT }), {
    kind: "DoubleTap",
    x: 12,
    y: 34,
  });
  assert.deepEqual(serializeAction({ kind: "LongPress", on: POINT }), {
    kind: "LongPress",
    x: 12,
    y: 34,
  });
});

test("InputText carries x, y and text", () => {
  assert.deepEqual(serializeAction({ kind: "InputText", into: POINT, text: "hi" }), {
    kind: "InputText",
    x: 12,
    y: 34,
    text: "hi",
  });
});

test("Swipe emits camelCase fromX/fromY/toX/toY/durationMillis", () => {
  const action: ActionDescriptor = {
    kind: "Swipe",
    from: { x: 5, y: 6 },
    to: { x: 7, y: 8 },
    durationMillis: 300,
  };
  assert.deepEqual(serializeAction(action), {
    kind: "Swipe",
    fromX: 5,
    fromY: 6,
    toX: 7,
    toY: 8,
    durationMillis: 300,
  });
});

test("Swipe defaults durationMillis to 250", () => {
  const action = serializeAction({
    kind: "Swipe",
    from: { x: 0, y: 0 },
    to: { x: 0, y: 1 },
  }) as { durationMillis: number };
  assert.equal(action.durationMillis, 250);
});

test("Scroll emits direction plus from/to point and duration", () => {
  assert.deepEqual(serializeAction({ kind: "Scroll", direction: "down", in: POINT }), {
    kind: "Scroll",
    direction: "down",
    fromX: 12,
    fromY: 34,
    toX: 12,
    toY: 34,
    durationMillis: 250,
  });
});

test("PressKey and Wait pass through their fields", () => {
  assert.deepEqual(serializeAction({ kind: "PressKey", key: "enter" }), {
    kind: "PressKey",
    key: "enter",
  });
  assert.deepEqual(serializeAction({ kind: "Wait", durationMillis: 500 }), {
    kind: "Wait",
    durationMillis: 500,
  });
});

test("builtin Scroll carries the pre-computed to endpoint", () => {
  assert.deepEqual(
    serializeAction({
      kind: "Scroll",
      direction: "down",
      in: { x: 100, y: 200 },
      from: { x: 100, y: 200 },
      to: { x: 100, y: 120 },
    }),
    {
      kind: "Scroll",
      direction: "down",
      fromX: 100,
      fromY: 200,
      toX: 100,
      toY: 120,
      durationMillis: 250,
    },
  );
});

test("a string target serializes selector-only for the runner to re-resolve", () => {
  assert.deepEqual(serializeAction({ kind: "Tap", on: "id:save" }), {
    kind: "Tap",
    x: 0,
    y: 0,
    selector: "id:save",
  });
});

test("an empty target or null action is dropped", () => {
  assert.equal(serializeAction({ kind: "Tap", on: "" }), null);
  assert.equal(serializeAction(null), null);
});

// Wire round-trip: serialize -> JSON -> parse asserts every field the Go
// decodeAction reads survives by its exact camelCase name. A rename (fromX vs
// from_x) would silently turn web/native actions into no-ops; this catches it.
test("serialized actions JSON round-trip with the decoder's field names", () => {
  const cases: ActionDescriptor[] = [
    { kind: "Tap", on: { x: 1, y: 2, selector: "id:a" } as never },
    { kind: "InputText", into: { x: 3, y: 4 } as never, text: "x" },
    { kind: "Swipe", from: { x: 5, y: 6 }, to: { x: 7, y: 8 }, durationMillis: 9 },
    { kind: "Scroll", direction: "up", in: { x: 1, y: 1 } as never },
    { kind: "PressKey", key: "enter" },
    { kind: "Wait", durationMillis: 10 },
  ];
  for (const descriptor of cases) {
    const wire = serializeAction(descriptor);
    assert.ok(wire, `expected ${descriptor.kind} to serialize`);
    const decoded = JSON.parse(JSON.stringify(wire)) as Record<string, unknown>;
    assert.equal(decoded.kind, wire!.kind);
    switch (wire!.kind) {
      case "Tap":
        assert.equal(typeof decoded.x, "number");
        assert.equal(typeof decoded.y, "number");
        assert.equal(decoded.selector, "id:a");
        break;
      case "InputText":
        assert.equal(typeof decoded.x, "number");
        assert.equal(decoded.text, "x");
        break;
      case "Swipe":
        assert.equal(decoded.fromX, 5);
        assert.equal(decoded.toY, 8);
        assert.equal(decoded.durationMillis, 9);
        break;
      case "Scroll":
        assert.equal(decoded.direction, "up");
        assert.equal(typeof decoded.fromX, "number");
        assert.equal(typeof decoded.toX, "number");
        break;
      case "PressKey":
        assert.equal(decoded.key, "enter");
        break;
      case "Wait":
        assert.equal(decoded.durationMillis, 10);
        break;
    }
  }
});
