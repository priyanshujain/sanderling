export interface Meta {
  seed: number;
  spec_path: string;
  bundle_sha256: string;
  platform: string;
  bundle_id: string;
  started_at: string;
  ended_at?: string;
  uatu_version: string;
}

export interface RunSummary {
  id: string;
  started_at: string;
  ended_at?: string;
  spec_path: string;
  seed: number;
  platform: string;
  bundle_id: string;
  duration_millis: number;
  step_count: number;
  violation_count: number;
  in_progress: boolean;
}

export interface StepSummary {
  index: number;
  timestamp: string;
  screen?: string;
  action_kind?: string;
  action_label?: string;
  has_violations: boolean;
  has_exceptions: boolean;
}

export interface Run extends RunSummary {
  meta: Meta;
  steps: StepSummary[];
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
  enabled?: boolean;
  checked?: boolean;
  focused?: boolean;
  selected?: boolean;
  bounds: BoundsRect;
}

export interface Hierarchy {
  elements: HierarchyElement[];
}

export interface BoundsRecord {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface PointRecord {
  x: number;
  y: number;
}

export interface Action {
  kind: string;
  x?: number;
  y?: number;
  from_x?: number;
  from_y?: number;
  to_x?: number;
  to_y?: number;
  key?: string;
  text?: string;
  duration_millis?: number;
  selector?: string;
  resolvedBounds?: BoundsRecord;
  tapPoint?: PointRecord;
}

export interface Exception {
  class: string;
  message?: string;
  stack_trace?: string;
  unix_millis?: number;
}

export type ResidualNode =
  | { op: "true" }
  | { op: "false" }
  | { op: "always" | "now" | "next" | "not"; arg: ResidualNode }
  | { op: "eventually"; arg: ResidualNode; within?: { amount: number; unit: string } }
  | { op: "and" | "or" | "implies"; left: ResidualNode; right: ResidualNode }
  | { op: "predicate"; name?: string }
  | { op: "error"; message: string };

export interface Step {
  step: number;
  timestamp: string;
  screen?: string;
  snapshots?: Record<string, unknown>;
  action?: Action;
  exceptions?: Exception[];
  violations?: string[];
  hierarchy?: Hierarchy;
  residuals?: Record<string, ResidualNode>;
}
