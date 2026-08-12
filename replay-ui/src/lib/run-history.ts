import type { Run, Step } from "../types";
import type { LaneStatus, PropertyLane } from "../panels/Timeline";
import type { MetricsSample } from "../panels/MetricsChart";
import { statusForStep } from "./property-status";

export interface RunHistory {
  names: string[];
  lanes: PropertyLane[];
  firstViolationStep?: number;
  firstExceptionStep?: number;
  exceptionStepIndices: number[];
  violationStepIndices: number[];
  metricsSamples: MetricsSample[];
  steps: (Step | null)[];
}

// A next/eventually obligation is evaluated one or more steps AFTER the action
// that armed it, so the checker records the violation on the DETECTION step
// while its witness names the CAUSE step. The timeline dot (the backend's
// markViolations) already sits on the cause step; mirror that here so the
// Violations tab and property lanes light up on the same step: the guilty
// action, not the unrelated action that happened to be running when the
// obligation resolved. Steps are cloned, never mutated in place.
export function relocateViolationsToCause(
  steps: (Step | null)[],
): (Step | null)[] {
  const byIndex = new Map<number, Step>();
  for (const s of steps) if (s) byIndex.set(s.step, s);

  const clones = new Map<number, Step>();
  const clone = (s: Step): Step => {
    let c = clones.get(s.step);
    if (!c) {
      c = {
        ...s,
        violations: [...(s.violations ?? [])],
        witnesses: { ...(s.witnesses ?? {}) },
      };
      clones.set(s.step, c);
    }
    return c;
  };

  for (const s of steps) {
    for (const name of s?.violations ?? []) {
      const cause = s?.witnesses?.[name]?.step;
      if (cause === undefined || cause === s?.step) continue;
      const target = byIndex.get(cause);
      if (!target) continue;
      const from = clone(s as Step);
      const to = clone(target);
      from.violations = (from.violations ?? []).filter((n) => n !== name);
      const witness = s?.witnesses?.[name];
      if (witness) {
        delete from.witnesses?.[name];
        (to.witnesses ??= {})[name] = witness;
      }
      if (!(to.violations ?? []).includes(name)) (to.violations ??= []).push(name);
    }
  }

  if (clones.size === 0) return steps;
  return steps.map((s) => (s ? clones.get(s.step) ?? s : s));
}

export function collectPropertyNames(steps: (Step | null)[]): string[] {
  const names = new Set<string>();
  for (const step of steps) {
    if (!step?.residuals) continue;
    for (const name of Object.keys(step.residuals)) {
      names.add(name);
    }
  }
  return [...names].sort();
}

export function statusForProperty(name: string, step: Step | null): LaneStatus {
  return statusForStep(name, step);
}

export function sortLanes(lanes: PropertyLane[]): PropertyLane[] {
  const rank = (lane: PropertyLane): number => {
    const last = lane.statuses[lane.statuses.length - 1];
    if (lane.statuses.includes("violated")) return 0;
    if (last === "pending") return 1;
    return 2;
  };
  return [...lanes].sort((a, b) => {
    const delta = rank(a) - rank(b);
    if (delta !== 0) return delta;
    return a.name.localeCompare(b.name);
  });
}

export function buildRunHistory(
  run: Run,
  responses: (Step | null)[],
): RunHistory {
  const steps = relocateViolationsToCause(responses);
  const propertyNames = collectPropertyNames(steps);
  const lanes: PropertyLane[] = propertyNames.map((name) => ({
    name,
    statuses: steps.map((step) => statusForProperty(name, step)),
  }));
  const firstViolationStep = run.steps.find((entry) => entry.has_violations)?.index;
  const firstExceptionStep = run.steps.find((entry) => entry.has_exceptions)?.index;
  const exceptionStepIndices = run.steps
    .filter((entry) => entry.has_exceptions)
    .map((entry) => entry.index);
  const violationStepIndices = run.steps
    .filter((entry) => entry.has_violations)
    .map((entry) => entry.index);
  const metricsSamples: MetricsSample[] = run.steps.map((entry, position) => ({
    stepIndex: entry.index,
    timestamp: entry.timestamp,
    metrics: steps[position]?.metrics,
  }));
  return {
    names: propertyNames,
    lanes: sortLanes(lanes),
    firstViolationStep,
    firstExceptionStep,
    exceptionStepIndices,
    violationStepIndices,
    metricsSamples,
    steps,
  };
}
