import { describe, it, expect } from "bun:test";
import {
  buildRunHistory,
  collectPropertyNames,
  relocateViolationsToCause,
  sortLanes,
  statusForProperty,
} from "../lib/run-history";
import type { PropertyLane } from "../panels/Timeline";
import type { Run } from "../types";
import { step, summary } from "./fixtures";

function lane(name: string, statuses: PropertyLane["statuses"]): PropertyLane {
  return { name, statuses };
}

describe("collectPropertyNames", () => {
  it("dedups and sorts names across steps, skipping null and residual-less steps", () => {
    const names = collectPropertyNames([
      null,
      step({ residuals: { b: { op: "true" }, a: { op: "false" } } }),
      step({}),
      step({ residuals: { a: { op: "true" }, c: { op: "true" } } }),
    ]);
    expect(names).toEqual(["a", "b", "c"]);
  });
});

// Bug class: getting status precedence wrong (e.g. checking residual before
// the violation set, or not defaulting null/absent to pending) would paint a
// violated property lane green.
describe("statusForProperty", () => {
  it("ranks violated over a holding residual and defaults to pending", () => {
    const s = step({ violations: ["p"], residuals: { p: { op: "true" } } });
    expect(statusForProperty("p", s)).toBe("violated");
    expect(statusForProperty("q", step({ residuals: { q: { op: "true" } } }))).toBe("holds");
    expect(statusForProperty("q", step({ residuals: { q: { op: "false" } } }))).toBe("pending");
    expect(statusForProperty("p", null)).toBe("pending");
  });
});

// Bug class: a lane that ever violated must sort first; trailing-pending lanes
// rank ahead of fully-holding ones, else the timeline buries active failures.
describe("sortLanes", () => {
  it("orders violated, then trailing-pending, then holds, ties by name", () => {
    const ordered = sortLanes([
      lane("holds-b", ["holds", "holds"]),
      lane("pending-a", ["holds", "pending"]),
      lane("violated-z", ["holds", "violated", "holds"]),
      lane("holds-a", ["holds", "holds"]),
    ]).map((l) => l.name);
    expect(ordered).toEqual(["violated-z", "pending-a", "holds-a", "holds-b"]);
  });
});

// Bug class: a next/eventually violation records on the DETECTION step but is
// caused earlier; leaving it on the detection step lights up an unrelated
// action in the Violations tab while the timeline dot sits on the cause step.
describe("relocateViolationsToCause", () => {
  it("moves a deferred violation to the step its witness blames", () => {
    const steps = [
      step({ step: 290, residuals: { p: { op: "predicate", name: "p3" } } }),
      step({
        step: 291,
        violations: ["p"],
        witnesses: { p: { step: 290, reason: "predicate false" } },
        residuals: { p: { op: "false" } },
      }),
      step({ step: 292 }),
    ];
    const moved = relocateViolationsToCause(steps);

    expect(moved[0]?.violations).toEqual(["p"]);
    expect(moved[0]?.witnesses?.p?.reason).toBe("predicate false");
    expect(moved[1]?.violations).toEqual([]);
    expect(moved[1]?.witnesses?.p).toBeUndefined();
    // originals are cloned, never mutated
    expect(steps[1]?.violations).toEqual(["p"]);
    expect(steps[1]?.witnesses?.p?.step).toBe(290);
  });

  it("leaves a violation without a witness on its detection step", () => {
    const steps = [step({ step: 5, violations: ["p"], residuals: { p: { op: "false" } } })];
    expect(relocateViolationsToCause(steps)).toBe(steps);
  });

  it("keeps the violation put when the blamed step is absent from the trace", () => {
    const steps = [step({ step: 9, violations: ["p"], witnesses: { p: { step: 3 } } })];
    expect(relocateViolationsToCause(steps)[0]?.violations).toEqual(["p"]);
  });
});

describe("buildRunHistory", () => {
  it("anchors a deferred violation's lane cell to the cause step, not detection", () => {
    const run = {
      id: "run-2",
      steps: [summary({ index: 290, has_violations: true }), summary({ index: 291 })],
    } as unknown as Run;
    const responses = [
      step({ step: 290, residuals: { p: { op: "predicate" } } }),
      step({
        step: 291,
        violations: ["p"],
        witnesses: { p: { step: 290 } },
        residuals: { p: { op: "false" } },
      }),
    ];

    const history = buildRunHistory(run, responses);

    expect(history.lanes[0].statuses).toEqual(["violated", "pending"]);
    expect(history.steps[0]?.violations).toEqual(["p"]);
    expect(history.steps[1]?.violations).toEqual([]);
    expect(history.firstViolationStep).toBe(290);
  });

  it("aligns lane statuses, metrics samples, and first-violation index by position", () => {
    const run = {
      id: "run-1",
      steps: [
        summary({ index: 0 }),
        summary({ index: 1, has_violations: true }),
        summary({ index: 2, has_exceptions: true }),
      ],
    } as unknown as Run;
    const responses = [
      step({ step: 0, residuals: { p: { op: "true" } } }),
      step({ step: 1, violations: ["p"], residuals: { p: { op: "true" } } }),
      null,
    ];

    const history = buildRunHistory(run, responses);

    expect(history.names).toEqual(["p"]);
    expect(history.lanes[0].statuses).toEqual(["holds", "violated", "pending"]);
    expect(history.firstViolationStep).toBe(1);
    expect(history.firstExceptionStep).toBe(2);
    expect(history.violationStepIndices).toEqual([1]);
    expect(history.exceptionStepIndices).toEqual([2]);
    expect(history.metricsSamples.map((m) => m.stepIndex)).toEqual([0, 1, 2]);
  });
});
