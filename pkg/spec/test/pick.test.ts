import { test } from "node:test";
import assert from "node:assert/strict";
import { Pcg } from "../src/pcg.ts";
import { nextAction, walk } from "../src/pick.ts";
import { INPUT_CORPUS, NATIVE_PRESS_KEYS, WEB_PRESS_KEYS } from "../src/corpus.ts";
import { resetWarnings } from "../src/verbs.ts";
import type {
  ActionDescriptor,
  BuiltinVerb,
  Candidate,
  GeneratorNode,
  Host,
} from "../src/action-tree.ts";

type Platform = "android" | "ios" | "web";

// stubHost returns a fixed candidate list for every verb and records each
// reportUnsupported call so warn-once semantics are observable.
function stubHost(
  platform: Platform,
  candidates: Candidate[],
): Host & { unsupported: BuiltinVerb[] } {
  const unsupported: BuiltinVerb[] = [];
  return {
    unsupported,
    platform: () => platform,
    queryCandidates: () => candidates,
    reportUnsupported: (verb) => {
      unsupported.push(verb);
    },
    seedHi: () => 0n,
    seedLo: () => 0n,
  };
}

const POINTS: Candidate[] = [
  { x: 10, y: 20, selector: "id:a", width: 100, height: 200 },
  { x: 30, y: 40, selector: "id:b", width: 100, height: 200 },
  { x: 50, y: 60, selector: "id:c", width: 100, height: 200 },
];

function builtin(verb: BuiltinVerb): GeneratorNode {
  return { kind: "builtin", verb };
}

test("taps draws one candidate index and targets its point", () => {
  resetWarnings();
  const host = stubHost("android", POINTS);
  const rng = new Pcg(42n, 0n);
  const oracle = new Pcg(42n, 0n);
  const expectedIndex = oracle.intN(POINTS.length);

  const action = walk(builtin("taps"), rng, host);
  assert.deepEqual(action, {
    kind: "Tap",
    on: {
      x: POINTS[expectedIndex]!.x,
      y: POINTS[expectedIndex]!.y,
      selector: POINTS[expectedIndex]!.selector,
    },
  });
});

test("typing draws candidate index then corpus index, in that order", () => {
  resetWarnings();
  const host = stubHost("android", POINTS);
  const rng = new Pcg(42n, 0n);
  const oracle = new Pcg(42n, 0n);
  const candidateIndex = oracle.intN(POINTS.length);
  const corpusIndex = oracle.intN(INPUT_CORPUS.length);

  const action = walk(builtin("typing"), rng, host) as ActionDescriptor & {
    kind: "InputText";
  };
  assert.equal(action.kind, "InputText");
  assert.deepEqual(action.into, {
    x: POINTS[candidateIndex]!.x,
    y: POINTS[candidateIndex]!.y,
    selector: POINTS[candidateIndex]!.selector,
  });
  assert.equal(action.text, INPUT_CORPUS[corpusIndex]);
});

test("swipes draw candidate, magnitude (200+intN(401)), then direction", () => {
  resetWarnings();
  const host = stubHost("android", POINTS);
  const rng = new Pcg(7n, 0n);
  const oracle = new Pcg(7n, 0n);
  const candidateIndex = oracle.intN(POINTS.length);
  const magnitude = 200 + oracle.intN(401);
  const direction = oracle.intN(4);

  const from = { x: POINTS[candidateIndex]!.x, y: POINTS[candidateIndex]!.y };
  const expectedTo = { x: from.x, y: from.y };
  switch (direction) {
    case 0:
      expectedTo.y = Math.max(0, from.y - magnitude);
      break;
    case 1:
      expectedTo.y = Math.max(0, from.y + magnitude);
      break;
    case 2:
      expectedTo.x = Math.max(0, from.x - magnitude);
      break;
    case 3:
      expectedTo.x = Math.max(0, from.x + magnitude);
      break;
  }

  const action = walk(builtin("swipes"), rng, host) as ActionDescriptor & {
    kind: "Swipe";
  };
  assert.equal(action.kind, "Swipe");
  assert.deepEqual(action.from, from);
  assert.deepEqual(action.to, expectedTo);
  assert.equal(action.durationMillis, 250);
});

test("scrolls draw candidate index then direction", () => {
  resetWarnings();
  const host = stubHost("android", POINTS);
  const rng = new Pcg(99n, 0n);
  const oracle = new Pcg(99n, 0n);
  const candidateIndex = oracle.intN(POINTS.length);
  const directionIndex = oracle.intN(4);
  const directions = ["up", "down", "left", "right"] as const;

  const action = walk(builtin("scrolls"), rng, host) as ActionDescriptor & {
    kind: "Scroll";
  };
  assert.equal(action.kind, "Scroll");
  assert.equal(action.direction, directions[directionIndex]);
  assert.deepEqual(action.in, {
    x: POINTS[candidateIndex]!.x,
    y: POINTS[candidateIndex]!.y,
  });
});

test("doubleTaps and longPresses draw exactly one candidate index", () => {
  resetWarnings();
  for (const verb of ["doubleTaps", "longPresses"] as const) {
    const host = stubHost("android", POINTS);
    const rng = new Pcg(123n, 0n);
    const oracle = new Pcg(123n, 0n);
    const index = oracle.intN(POINTS.length);
    const action = walk(builtin(verb), rng, host);
    const kind = verb === "doubleTaps" ? "DoubleTap" : "LongPress";
    assert.deepEqual(action, {
      kind,
      on: {
        x: POINTS[index]!.x,
        y: POINTS[index]!.y,
        selector: POINTS[index]!.selector,
      },
    });
  }
});

test("waitOnce emits a 500ms wait and draws nothing", () => {
  resetWarnings();
  const host = stubHost("android", POINTS);
  const rng = new Pcg(42n, 0n);
  const before = rng.float64();
  const after = rng.float64();

  const fresh = new Pcg(42n, 0n);
  fresh.float64();
  const action = walk(builtin("waitOnce"), fresh, host);
  const afterWait = fresh.float64();

  assert.deepEqual(action, { kind: "Wait", durationMillis: 500 });
  // The wait consumed no draw, so the stream is identical with or without it.
  assert.equal(afterWait, after);
  assert.notEqual(before, after);
});

test("pressKeys on native draws from NATIVE_PRESS_KEYS", () => {
  resetWarnings();
  const host = stubHost("android", POINTS);
  const rng = new Pcg(42n, 0n);
  const oracle = new Pcg(42n, 0n);
  const index = oracle.intN(NATIVE_PRESS_KEYS.length);
  const action = walk(builtin("pressKeys"), rng, host);
  assert.deepEqual(action, { kind: "PressKey", key: NATIVE_PRESS_KEYS[index] });
});

test("pressKeys on web draws from WEB_PRESS_KEYS", () => {
  resetWarnings();
  const host = stubHost("web", POINTS);
  const rng = new Pcg(2024n, 0n);
  const oracle = new Pcg(2024n, 0n);
  const index = oracle.intN(WEB_PRESS_KEYS.length);
  const action = walk(builtin("pressKeys"), rng, host);
  assert.deepEqual(action, { kind: "PressKey", key: WEB_PRESS_KEYS[index] });
});

test("empty candidate list yields null without drawing", () => {
  resetWarnings();
  const host = stubHost("android", []);
  const rng = new Pcg(42n, 0n);
  assert.equal(walk(builtin("taps"), rng, host), null);
  // No draw consumed: next float64 matches a fresh stream's first draw.
  assert.equal(rng.float64(), new Pcg(42n, 0n).float64());
});

test("weighted: one float64 draw, ascending cumulative scan", () => {
  resetWarnings();
  const host = stubHost("android", POINTS);
  const tap: GeneratorNode = builtin("taps");
  const wait: GeneratorNode = builtin("waitOnce");
  // total = 3; draw = float64()*3 decides the branch by ascending cumulative.
  const node: GeneratorNode = {
    kind: "weighted",
    branches: [
      [1, tap],
      [2, wait],
    ],
  };

  const rng = new Pcg(42n, 0n);
  const oracle = new Pcg(42n, 0n);
  const draw = oracle.float64() * 3;
  const expectFirst = draw < 1; // cumulative after branch 0 is 1.

  const action = walk(node, rng, host);
  if (expectFirst) {
    assert.equal((action as ActionDescriptor).kind, "Tap");
  } else {
    assert.deepEqual(action, { kind: "Wait", durationMillis: 500 });
  }
});

test("weighted: a zero-weight branch is never selected", () => {
  resetWarnings();
  const host = stubHost("android", POINTS);
  const poison: GeneratorNode = {
    kind: "actions",
    generate: () => {
      throw new Error("zero-weight branch must never be walked");
    },
  };
  const node: GeneratorNode = {
    kind: "weighted",
    branches: [
      [0, poison],
      [1, builtin("waitOnce")],
    ],
  };
  // Drive many seeds: the zero-weight branch must never be entered.
  for (let seed = 1; seed <= 200; seed++) {
    const action = walk(node, new Pcg(BigInt(seed), 0n), host);
    assert.deepEqual(action, { kind: "Wait", durationMillis: 500 });
  }
});

test("weighted: all-zero (or negative) weights return null", () => {
  resetWarnings();
  const host = stubHost("android", POINTS);
  const node: GeneratorNode = {
    kind: "weighted",
    branches: [
      [0, builtin("waitOnce")],
      [-5, builtin("taps")],
    ],
  };
  assert.equal(walk(node, new Pcg(42n, 0n), host), null);
});

test("actions: single-element list draws nothing", () => {
  resetWarnings();
  const only: ActionDescriptor = { kind: "Wait", durationMillis: 10 };
  const node: GeneratorNode = { kind: "actions", generate: () => [only] };
  const rng = new Pcg(42n, 0n);
  const action = walk(node, rng, stubHost("android", POINTS));
  assert.deepEqual(action, only);
  // No draw consumed for a singleton list.
  assert.equal(rng.float64(), new Pcg(42n, 0n).float64());
});

test("actions: multi-element list draws one intN(len) index", () => {
  resetWarnings();
  const list: ActionDescriptor[] = [
    { kind: "Wait", durationMillis: 1 },
    { kind: "Wait", durationMillis: 2 },
    { kind: "Wait", durationMillis: 3 },
  ];
  const node: GeneratorNode = { kind: "actions", generate: () => [...list] };
  const rng = new Pcg(42n, 0n);
  const oracle = new Pcg(42n, 0n);
  const index = oracle.intN(list.length);
  assert.deepEqual(walk(node, rng, stubHost("android", POINTS)), list[index]);
});

test("actions: empty list yields null", () => {
  resetWarnings();
  const node: GeneratorNode = { kind: "actions", generate: () => [] };
  assert.equal(walk(node, new Pcg(42n, 0n), stubHost("android", POINTS)), null);
});

test("corpus reproducibility: same seed yields the same typed text", () => {
  resetWarnings();
  const host = stubHost("android", POINTS);
  const first = walk(builtin("typing"), new Pcg(2024n, 0n), host) as ActionDescriptor & {
    kind: "InputText";
  };
  const second = walk(builtin("typing"), new Pcg(2024n, 0n), host) as ActionDescriptor & {
    kind: "InputText";
  };
  assert.equal(first.text, second.text);
  assert.ok(INPUT_CORPUS.includes(first.text));
});

test("nextAction retries up to 16 times then returns null", () => {
  resetWarnings();
  // An empty weighted tree always returns null; nextAction must give up, not loop.
  const empty: GeneratorNode = { kind: "weighted", branches: [] };
  assert.equal(nextAction(empty, new Pcg(42n, 0n), stubHost("android", POINTS)), null);
});

test("nextAction returns the first non-null walk result", () => {
  resetWarnings();
  let calls = 0;
  // First two walks yield null (empty list), third yields an action. nextAction
  // should return on the third attempt.
  const node: GeneratorNode = {
    kind: "actions",
    generate: () => {
      calls++;
      return calls >= 3 ? [{ kind: "Wait", durationMillis: 5 }] : [];
    },
  };
  const action = nextAction(node, new Pcg(42n, 0n), stubHost("android", POINTS));
  assert.deepEqual(action, { kind: "Wait", durationMillis: 5 });
  assert.equal(calls, 3);
});
