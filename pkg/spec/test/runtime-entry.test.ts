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

test("an unresolved (string) target drops the action", () => {
  assert.equal(serializeAction({ kind: "Tap", on: "id:never-resolved" }), null);
  assert.equal(serializeAction(null), null);
});
