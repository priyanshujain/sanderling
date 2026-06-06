import { describe, it, expect } from "bun:test";
import {
  buildRunHistory,
  collectPropertyNames,
  sortLanes,
  statusForProperty,
} from "../lib/run-history";
import type { PropertyLane } from "../panels/Timeline";
import type { Run, Step, StepSummary } from "../types";

function step(over: Partial<Step>): Step {
  return { step: 0, timestamp: "1970-01-01T00:00:00.000Z", ...over };
}

function summary(over: Partial<StepSummary>): StepSummary {
  return {
    index: 0,
    timestamp: "1970-01-01T00:00:00.000Z",
    has_violations: false,
    has_exceptions: false,
    ...over,
  };
}

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

describe("buildRunHistory", () => {
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
