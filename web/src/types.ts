export interface RunSummary {
  id: string;
  startedAt: string;
  endedAt?: string;
  specPath: string;
  seed: number;
  platform: string;
  bundleId: string;
  durationMillis?: number;
  stepCount: number;
  violationCount: number;
  inProgress: boolean;
}

export interface StepSummary {
  index: number;
  timestamp: string;
  screen?: string;
  actionKind?: string;
  hasViolations: boolean;
  hasExceptions: boolean;
}

export interface BoundsRect {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

export interface HierarchyElement {
  resourceId?: string;
  text?: string;
  description?: string;
  class?: string;
  package?: string;
  clickable?: boolean;
  bounds: BoundsRect;
}

export interface Hierarchy {
  elements: HierarchyElement[];
}

export interface Action {
  kind: string;
  x?: number;
  y?: number;
  selector?: string;
  resolvedBounds?: { x: number; y: number; width: number; height: number };
  tapPoint?: { x: number; y: number };
  fromX?: number;
  fromY?: number;
  toX?: number;
  toY?: number;
  durationMillis?: number;
  key?: string;
  text?: string;
}

export interface Exception {
  class: string;
  message?: string;
  stackTrace?: string;
  unixMillis?: number;
}

export type ResidualNode =
  | { op: "true" }
  | { op: "false" }
  | { op: "always" | "now" | "next" | "not"; arg: ResidualNode }
  | { op: "eventually"; arg: ResidualNode; within?: { amount: number; unit: string } }
  | { op: "and" | "or" | "implies"; left: ResidualNode; right: ResidualNode }
  | { op: "predicate"; name?: string }
  | { op: "error"; message: string };

export interface Step extends StepSummary {
  snapshots?: Record<string, unknown>;
  action?: Action;
  exceptions?: Exception[];
  violations?: string[];
  hierarchy?: Hierarchy;
  residuals?: Record<string, ResidualNode>;
}

export interface Run {
  meta: RunSummary;
  steps: StepSummary[];
}
