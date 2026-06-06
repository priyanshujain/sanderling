import type { ResidualNode, Step } from "../types";

export type Status = "violated" | "pending" | "holds";

export const STATUS_ORDER: Record<Status, number> = {
  violated: 0,
  pending: 1,
  holds: 2,
};

export function statusFor(
  name: string,
  violations: Set<string>,
  residuals?: Record<string, ResidualNode>,
): Status {
  if (violations.has(name)) {
    return "violated";
  }
  const residual = residuals?.[name];
  if (residual && residual.op === "true") {
    return "holds";
  }
  return "pending";
}

export function statusForStep(name: string, step: Step | null): Status {
  if (!step) return "pending";
  return statusFor(name, new Set(step.violations ?? []), step.residuals);
}
