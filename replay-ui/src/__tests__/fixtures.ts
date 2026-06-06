import type { Step, StepSummary } from "../types";

export function step(over: Partial<Step>): Step {
  return { step: 0, timestamp: "1970-01-01T00:00:00.000Z", ...over };
}

export function summary(over: Partial<StepSummary>): StepSummary {
  return {
    index: 0,
    timestamp: "1970-01-01T00:00:00.000Z",
    has_violations: false,
    has_exceptions: false,
    ...over,
  };
}
