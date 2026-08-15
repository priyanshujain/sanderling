import assert from "node:assert/strict";
import { test } from "node:test";

import { oncePerFrame, routeOfFrame } from "../../../examples/folio/sanderling/predicates.ts";

// oncePerFrame is what stops the folio spec re-walking the accessibility tree
// once per extractor, and the whole of its safety is that the state object is a
// fresh one every step (goja's stateObject, web's buildState). These tests pin
// both halves: the same frame is read once, a different frame is a different
// answer. A cache that outlived its frame would freeze every reading the spec
// takes and the properties over them would go quietly vacuous.
const SCREENS = { login: "LoginScreen", home: "HomeScreen" } as const;

interface Frame {
  present: readonly string[];
  finds: number;
}

const frameShowing = (...present: readonly string[]): Frame => ({ present, finds: 0 });

const routeOf = oncePerFrame((frame: Frame) =>
  routeOfFrame(SCREENS, tag => {
    frame.finds++;
    return frame.present.includes(tag);
  }),
);

test("one frame is walked once, however many readings ask", () => {
  const home = frameShowing("HomeScreen");
  assert.equal(routeOf(home), "home");
  assert.equal(routeOf(home), "home");
  assert.equal(routeOf(home), "home");
  assert.equal(home.finds, 2);
});

test("a new frame is a new answer", () => {
  const home = frameShowing("HomeScreen");
  const login = frameShowing("LoginScreen");
  assert.equal(routeOf(home), "home");
  assert.equal(routeOf(login), "login");
  assert.equal(login.finds, 2);
});

test("a transition frame is not answered off the frame before it", () => {
  assert.equal(routeOf(frameShowing("HomeScreen")), "home");
  assert.equal(routeOf(frameShowing("HomeScreen", "LoginScreen")), null);
});

test("returning to an earlier frame re-reads it", () => {
  const home = frameShowing("HomeScreen");
  routeOf(home);
  routeOf(frameShowing("LoginScreen"));
  assert.equal(routeOf(home), "home");
  assert.equal(home.finds, 4);
});

// What makes memoizing the card list worth more than memoizing the route: the
// three readings taken off it share one parse instead of three.
test("a frame's reading is handed back by identity", () => {
  const cardsOf = oncePerFrame((frame: Frame) => frame.present.map(tag => ({ tag })));
  const home = frameShowing("HomeScreen");
  assert.equal(cardsOf(home), cardsOf(home));
  assert.notEqual(cardsOf(home), cardsOf(frameShowing("HomeScreen")));
});
