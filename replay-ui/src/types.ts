export interface Meta {
  seed: number;
  spec_path: string;
  bundle_sha256: string;
  platform: string;
  bundle_id: string;
  started_at: string;
  ended_at?: string;
  sanderling_version: string;
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
  tag?: string;
  package?: string;
  clickable?: boolean;
  enabled?: boolean;
  checked?: boolean;
  focused?: boolean;
  selected?: boolean;
  bounds: BoundsRect;
  attributes?: Record<string, string>;
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
  resolved_bounds?: BoundsRecord;
  tap_point?: PointRecord;
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
  | { op: "now" | "next" | "not"; arg: ResidualNode }
  | { op: "always"; arg: ResidualNode; within?: { amount: number; unit: string } }
  | { op: "eventually"; arg: ResidualNode; within?: { amount: number; unit: string } }
  | { op: "and" | "or" | "implies"; left: ResidualNode; right: ResidualNode }
  | { op: "predicate"; name?: string }
  | { op: "error"; message: string };

export interface Metrics {
  cpu_percent?: number;
  heap_bytes?: number;
  total_memory_bytes?: number;
}

export interface ExtractorChange {
  prev: unknown;
  curr: unknown;
}

export interface Witness {
  reason?: string;
  is_error?: boolean;
  // step is the step the failed obligation originated at: the causing step,
  // which for deferred obligations (next, eventually) is earlier than the
  // step whose record carries the witness.
  step?: number;
  extractors?: Record<string, unknown>;
}

export interface Step {
  step: number;
  timestamp: string;
  screen?: string;
  snapshots?: Record<string, unknown>;
  // next_action is the action chosen for the next iteration based on observing this step.
  next_action?: Action;
  exceptions?: Exception[];
  violations?: string[];
  hierarchy?: Hierarchy;
  residuals?: Record<string, ResidualNode>;
  metrics?: Metrics;
  extractor_changes?: Record<string, ExtractorChange>;
  witnesses?: Record<string, Witness>;
}
