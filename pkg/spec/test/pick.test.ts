import { test } from "node:test";
import assert from "node:assert/strict";
import { Pcg } from "../src/pcg.ts";
import { builtinCandidates, nextAction, walk } from "../src/pick.ts";
import { INPUT_CORPUS, NATIVE_PRESS_KEYS, WEB_PRESS_KEYS } from "../src/corpus.ts";
import { resetWarnings } from "../src/verbs.ts";
import type {
  ActionDescriptor,
  BuiltinVerb,
  GeneratorNode,
  Host,
  TargetElement,
} from "../src/action-tree.ts";
import type { Direction, Point } from "../src/types.ts";

type Platform = "android" | "ios" | "web";

// stubHost returns a fixed target list, eligible for every verb, and records
// each reportUnsupported call so warn-once semantics are observable.
function stubHost(
  platform: Platform,
  targets: TargetElement[],
): Host & { unsupported: BuiltinVerb[] } {
  const unsupported: BuiltinVerb[] = [];
  return {
    unsupported,
    platform: () => platform,
    queryTargets: () => targets,
    reportUnsupported: (verb) => {
      unsupported.push(verb);
    },
    seedHi: () => 0n,
    seedLo: () => 0n,
  };
}

const EVERY_FACT = { clickable: true, enabled: true, editable: true, scrollable: true };

const POINTS: TargetElement[] = [
  { x: 10, y: 20, selector: "id:a", width: 100, height: 200, ...EVERY_FACT },
  { x: 30, y: 40, selector: "id:b", width: 100, height: 200, ...EVERY_FACT },
  { x: 50, y: 60, selector: "id:c", width: 100, height: 200, ...EVERY_FACT },
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

// scrollCandidate mirrors what the picker builds for one (container, direction)
// pair: a drag opposite the named content motion, 40% of the container extent.
function scrollCandidate(target: TargetElement, direction: "down" | "up") {
  const from = { x: target.x, y: target.y };
  const extent = Math.trunc((4 * (target.height ?? 0)) / 10);
  const toY = direction === "down" ? from.y - extent : from.y + extent;
  return {
    kind: "Scroll",
    direction,
    in: from,
    from,
    to: { x: from.x, y: Math.max(0, toY) },
  };
}

// swipeCandidate mirrors the free-form drag: a raw pixel distance, with the
// direction naming where the finger travels.
function swipeCandidate(
  target: Point,
  direction: Direction,
  magnitude: number,
) {
  const from = { x: target.x, y: target.y };
  const horizontal = direction === "left" || direction === "right";
  const forward = direction === "down" || direction === "right";
  const travel = forward ? magnitude : -magnitude;
  return {
    kind: "Swipe",
    from,
    to: horizontal
      ? { x: Math.max(0, from.x + travel), y: from.y }
      : { x: from.x, y: Math.max(0, from.y + travel) },
    durationMillis: 250,
  };
}

const NOMINAL_SWIPE_MAGNITUDE = 400;

const SCROLL_DIRECTIONS = ["down", "up"] as const;
const SWIPE_DIRECTIONS = ["down", "up", "left", "right"] as const;

// swipeDirection recovers which way a drawn swipe travelled from its endpoints.
function swipeDirection(from: Point, to: Point): Direction {
  if (to.x !== from.x) return to.x > from.x ? "right" : "left";
  return to.y > from.y ? "down" : "up";
}

test("scrolls enumerate every container up and down only", () => {
  resetWarnings();
  const host = stubHost("android", POINTS);
  assert.deepEqual(
    builtinCandidates("scrolls", host),
    POINTS.flatMap((target, targetIndex) =>
      SCROLL_DIRECTIONS.map((direction) => ({
        action: scrollCandidate(target, direction),
        targetIndex,
      })),
    ),
  );
});

test("swipes enumerate a free-form drag per target in all four directions", () => {
  resetWarnings();
  const host = stubHost("android", POINTS);
  // The swipe candidate is its own action shape, sized in raw pixels rather than
  // off the target's extent, and it carries what the policy needs to redraw the
  // distance. It is not the scroll gesture under a second name.
  assert.deepEqual(
    builtinCandidates("swipes", host),
    POINTS.flatMap((target, targetIndex) =>
      SWIPE_DIRECTIONS.map((direction) => ({
        action: swipeCandidate(target, direction, NOMINAL_SWIPE_MAGNITUDE),
        targetIndex,
        swipe: { origin: { x: target.x, y: target.y }, direction },
      })),
    ),
  );
});

test("the gesture verbs differ in target filter and in direction set", () => {
  resetWarnings();
  // Same host, same targets: what separates the two verbs here is the direction
  // set alone. Scrolls stay vertical because every scrollable container gets a
  // candidate; swipes reach sideways because swipe-to-dismiss does.
  const host = stubHost("android", POINTS);
  const scrolls = builtinCandidates("scrolls", host);
  const swipes = builtinCandidates("swipes", host);

  const scrollDirections = new Set(
    scrolls.map(
      (entry) => (entry.action as ActionDescriptor & { kind: "Scroll" }).direction,
    ),
  );
  const swipeDirections = new Set(swipes.map((entry) => entry.swipe!.direction));

  assert.deepEqual([...scrollDirections].sort(), ["down", "up"]);
  assert.deepEqual([...swipeDirections].sort(), ["down", "left", "right", "up"]);
  assert.equal(scrolls.length, POINTS.length * 2);
  assert.equal(swipes.length, POINTS.length * 4);
});

test("scrolls draw one index over the enumerated candidates", () => {
  resetWarnings();
  const host = stubHost("android", POINTS);
  const enumerated = builtinCandidates("scrolls", host);
  const oracle = new Pcg(99n, 0n);
  const index = oracle.intN(enumerated.length);

  const action = walk(builtin("scrolls"), new Pcg(99n, 0n), host);
  assert.deepEqual(action, enumerated[index]!.action);
});

test("swipes draw candidate index then magnitude, in that order", () => {
  resetWarnings();
  const host = stubHost("android", POINTS);
  const enumerated = builtinCandidates("swipes", host);
  const oracle = new Pcg(7n, 0n);
  const index = oracle.intN(enumerated.length);
  const magnitude = 200 + oracle.intN(401);
  const picked = enumerated[index]!.swipe!;

  const action = walk(builtin("swipes"), new Pcg(7n, 0n), host);
  assert.deepEqual(
    action,
    swipeCandidate(picked.origin, picked.direction, magnitude),
  );
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

test("pressKeys enumerates the platform's whole key pool", () => {
  resetWarnings();
  for (const [platform, keys] of [
    ["android", NATIVE_PRESS_KEYS],
    ["web", WEB_PRESS_KEYS],
  ] as const) {
    const enumerated = builtinCandidates("pressKeys", stubHost(platform, POINTS));
    assert.deepEqual(
      enumerated,
      keys.map((key) => ({ action: { kind: "PressKey", key }, targetIndex: -1 })),
    );
  }
});

test("pressKeys on native emits the only key without drawing", () => {
  resetWarnings();
  const host = stubHost("android", POINTS);
  const rng = new Pcg(42n, 0n);
  const action = walk(builtin("pressKeys"), rng, host);
  assert.deepEqual(action, { kind: "PressKey", key: NATIVE_PRESS_KEYS[0] });
  // One key is no choice, so the pool consumed no draw.
  assert.equal(rng.float64(), new Pcg(42n, 0n).float64());
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

test("an unsupported verb enumerates nothing and reports once", () => {
  resetWarnings();
  // No platform in the matrix declines a verb today, so the branch is reached
  // through a platform the matrix has never heard of.
  const host = stubHost("desktop" as Platform, POINTS);
  assert.deepEqual(builtinCandidates("taps", host), []);
  assert.deepEqual(builtinCandidates("taps", host), []);
  assert.deepEqual(host.unsupported, ["taps"]);
});

test("the seeded pick is an index into the shared enumeration", () => {
  resetWarnings();
  // Whatever the verb, the drawn action is one of the enumerated entries: the
  // seeded policy adds a choice, never an action the model policy cannot see.
  // `typing` and `swipes` are covered separately, being the two verbs whose
  // enumeration leaves one value for the policy to fill in.
  const host = stubHost("android", POINTS);
  for (const verb of [
    "taps",
    "doubleTaps",
    "longPresses",
    "scrolls",
    "pressKeys",
    "waitOnce",
  ] as const) {
    const enumerated = builtinCandidates(verb, host).map((entry) =>
      JSON.stringify(entry.action),
    );
    for (let seed = 1; seed <= 50; seed++) {
      const action = walk(builtin(verb), new Pcg(BigInt(seed), 0n), host);
      assert.ok(
        enumerated.includes(JSON.stringify(action)),
        `${verb} drew ${JSON.stringify(action)}, which is not an enumerated candidate`,
      );
    }
  }
});

test("typing enumerates the field and the policy supplies the text", () => {
  resetWarnings();
  const host = stubHost("android", POINTS);
  const enumerated = builtinCandidates("typing", host);
  // The candidate set names fields only: an empty text is the slot the seeded
  // corpus draw and the model's own value both fill.
  for (const entry of enumerated) {
    assert.equal((entry.action as ActionDescriptor & { kind: "InputText" }).text, "");
  }
  const oracle = new Pcg(2024n, 0n);
  const index = oracle.intN(enumerated.length);
  const text = INPUT_CORPUS[oracle.intN(INPUT_CORPUS.length)];
  assert.deepEqual(walk(builtin("typing"), new Pcg(2024n, 0n), host), {
    ...enumerated[index]!.action,
    text,
  });
});

test("swipes enumerate origin and direction, the policy adds distance", () => {
  resetWarnings();
  const host = stubHost("android", POINTS);
  const origins = new Set(
    builtinCandidates("swipes", host).map((entry) =>
      JSON.stringify([entry.swipe!.origin, entry.swipe!.direction]),
    ),
  );
  const drawn = new Set<Direction>();
  for (let seed = 1; seed <= 200; seed++) {
    const action = walk(builtin("swipes"), new Pcg(BigInt(seed), 0n), host) as
      ActionDescriptor & { kind: "Swipe"; from: Point; to: Point };
    assert.equal(action.kind, "Swipe");
    const direction = swipeDirection(action.from, action.to);
    drawn.add(direction);
    assert.ok(
      origins.has(JSON.stringify([action.from, direction])),
      `swipe from ${JSON.stringify(action.from)} going ${direction} is not enumerated`,
    );
    // The drawn distance stays inside 200..600, clamped at the screen edge.
    const horizontal = direction === "left" || direction === "right";
    const distance = horizontal
      ? Math.abs(action.to.x - action.from.x)
      : Math.abs(action.to.y - action.from.y);
    const clamped = horizontal ? action.to.x === 0 : action.to.y === 0;
    assert.ok(distance <= 600, `drag of ${distance}px exceeds the drawn range`);
    assert.ok(
      distance >= 200 || clamped,
      `drag of ${distance}px is under the drawn range and not clamped`,
    );
  }
  // Every enumerated direction is reachable by a draw, sideways included.
  assert.deepEqual([...drawn].sort(), ["down", "left", "right", "up"]);
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
