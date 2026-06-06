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
  const propertyNames = collectPropertyNames(responses);
  const lanes: PropertyLane[] = propertyNames.map((name) => ({
    name,
    statuses: responses.map((step) => statusForProperty(name, step)),
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
    metrics: responses[position]?.metrics,
  }));
  return {
    names: propertyNames,
    lanes: sortLanes(lanes),
    firstViolationStep,
    firstExceptionStep,
    exceptionStepIndices,
    violationStepIndices,
    metricsSamples,
    steps: responses,
  };
}
