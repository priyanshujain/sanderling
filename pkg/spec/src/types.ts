/// <reference lib="dom" />

export type Snapshots = Record<string, unknown>;

/**
 * Attribute names with known canonical types. Cross-platform aliases (e.g.
 * `testTag` -> resource-id / accessibilityIdentifier) are listed so authors
 * get autocomplete on whichever name they prefer.
 *
 * Boolean state attributes accept a native boolean; the runtime stringifies
 * to "true"/"false" before matching.
 */
export interface KnownAttrSelectors {
  testTag?: string;
  testID?: string;
  identifier?: string;
  accessibilityIdentifier?: string;
  "resource-id"?: string;
  id?: string;

  "content-desc"?: string;
  contentDescription?: string;
  accessibilityText?: string;
  accessibilityLabel?: string;
  ariaLabel?: string;
  "aria-label"?: string;
  label?: string;

  text?: string;
  tag?: string;
  class?: string;
  className?: string;
  elementType?: string;
  package?: string;
  placeholderValue?: string;
  placeholder?: string;
  hintText?: string;

  clickable?: boolean;
  enabled?: boolean;
  focused?: boolean;
  checked?: boolean;
  selected?: boolean;
  editable?: boolean;
  secure?: boolean;
}

/**
 * Keys that name a matching rule rather than an attribute a driver reports.
 * They belong to the selector surface only, which is why they are not part of
 * `KnownAttrSelectors` (and so never appear in `RawAttrs`).
 */
export interface PrefixSelectors {
  /** Identifier starts with this, after Android's "<package>:id/" if present. */
  idPrefix?: string;
  /** Accessibility description starts with this. */
  descPrefix?: string;
}

/**
 * Object-form selector for `find` / `findAll`. Known attributes are typed
 * via `KnownAttrSelectors`; arbitrary string keys are still allowed for
 * raw driver attributes the typed surface doesn't yet cover.
 */
export type AttrSelector = KnownAttrSelectors &
  PrefixSelectors & {
    [key: string]: string | boolean | undefined;
  };

export type SelectorPath = readonly AttrSelector[];

/** String-valued attribute names with known canonical keys. */
export type RawAttrs = {
  [K in keyof KnownAttrSelectors]?: KnownAttrSelectors[K] extends boolean | undefined
    ? "true" | "false"
    : string;
} & {
  [key: string]: string | undefined;
};

export interface AccessibilityElement {
  id?: string;
  text?: string;
  desc?: string;
  class?: string;
  clickable?: boolean;
  enabled?: boolean;
  checked?: boolean;
  focused?: boolean;
  selected?: boolean;
  editable?: boolean;
  /** Field masks what is typed into it; null where the platform does not report it. */
  secure?: boolean | null;
  bounds?: { left: number; top: number; right: number; bottom: number };
  x?: number;
  y?: number;
  attrs?: RawAttrs;
  find(selector: string | AttrSelector | SelectorPath): AccessibilityElement | undefined;
  findAll(selector: string | AttrSelector | SelectorPath): AccessibilityElement[];
}

export interface AccessibilityTree {
  find(selector: string | AttrSelector | SelectorPath): AccessibilityElement | undefined;
  findAll(selector: string | AttrSelector | SelectorPath): AccessibilityElement[];
}

export interface LogEntry {
  unixMillis: number;
  level: string;
  tag: string;
  message: string;
}

export interface ExceptionRecord {
  class: string;
  message: string;
  stackTrace: string;
  unixMillis?: number;
}

/**
 * The previous step's action as the runner reports it. `applied` is true when
 * the runner saw the dispatch succeed and null when the apply call failed with
 * the gesture possibly already delivered: an RPC deadline can fire after the
 * tap landed. Null is unknown, not "it did not happen" (`state.lastAction` is
 * itself null for that), so a property attributing an effect to this action
 * has to decline unless `applied` is true.
 *
 * `relaunched` is true when the runner had to bring the app back to the
 * foreground after this action, so the previous reading and the current one
 * straddle a restart. The action itself still happened; what a property cannot
 * assume across it is that app state ran continuously between the two readings,
 * and one demanding an effect of this action has to decline. Null is "not
 * reported", which is weaker than "the app never restarted": a target whose
 * foreground the runner cannot read never relaunches the app and cannot promise
 * that either.
 *
 * `text` is the value as the record renders it: `[redacted]` wherever the
 * target may be a credential entry, which on Android is every target, since it
 * reports nothing either way. Read the field off `ax` to reason about what it
 * holds.
 */
export type LastAction = Action & { applied: true | null; relaunched: true | null };

export interface State {
  snapshots: Snapshots;
  ax: AccessibilityTree;
  lastAction: LastAction | null;
  time: number;
  logs: readonly LogEntry[];
  exceptions: readonly ExceptionRecord[];
}

/**
 * State as observed inside a web (V8/browser) extractor. Adds the live
 * `document` and `window` handles. `state.document` is V8-only, so goja-side
 * predicates do not see it; if you need DOM data in a predicate, surface it
 * via an `extract()`.
 */
export interface WebState extends State {
  readonly document: Document;
  readonly window: Window;
}

export interface Extracted<T> {
  readonly current: T;
  readonly previous: T | undefined;
  named(name: string): Extracted<T>;
}

export interface Point {
  x: number;
  y: number;
}

export type Direction = "up" | "down" | "left" | "right";

export type TapAction = { kind: "Tap"; on: string | AccessibilityElement };
export type DoubleTapAction = { kind: "DoubleTap"; on: string | AccessibilityElement };
export type LongPressAction = { kind: "LongPress"; on: string | AccessibilityElement };
export type ScrollAction = {
  kind: "Scroll";
  direction: Direction;
  in?: string | AccessibilityElement;
};
export type InputTextAction = {
  kind: "InputText";
  into: string | AccessibilityElement;
  text: string;
};
export type SwipeAction = {
  kind: "Swipe";
  from: Point | AccessibilityElement;
  to: Point | AccessibilityElement;
  durationMillis?: number;
};
export type PressKeyAction = { kind: "PressKey"; key: Key };
export type WaitAction = { kind: "Wait"; durationMillis: number };
export type Action =
  | TapAction
  | DoubleTapAction
  | LongPressAction
  | ScrollAction
  | InputTextAction
  | SwipeAction
  | PressKeyAction
  | WaitAction;

export type Key =
  | "back"
  | "home"
  | "enter"
  | "tab"
  | "escape"
  | "up"
  | "down"
  | "left"
  | "right";

// ActionGenerator is a node in the action-generator tree (see action-tree.ts).
// Author specs treat it as an opaque handle they compose with weighted(); the
// shared picker walks the underlying GeneratorNode.
export type ActionGenerator = GeneratorNode;

export interface Formula {
  readonly __sanderlingFormula: true;
  implies(other: Formula): Formula;
  or(other: Formula): Formula;
  and(other: Formula): Formula;
  not(): Formula;
}

export interface EventuallyFormula extends Formula {
  // `"milliseconds"` and `"seconds"` bound the window in wall-clock time, which
  // is what a user-perceived deadline means. `"steps"` bounds it in observed
  // steps, so the same window costs the same regardless of how long each step
  // took: use it for anything compared across runs of different speeds.
  within(amount: number, unit: "milliseconds" | "seconds" | "steps"): Formula;
}

export interface Sampler<T> {
  generate(): T;
}

// SanderlingRuntime is the host-bound surface that stays on
// globalThis.__sanderling__: extract plus the LTL formula constructors. Action
// factories no longer live here; they return plain data trees (see actions.ts).
export interface SanderlingRuntime {
  extract: <T>(getter: (state: State) => T, name?: string) => Extracted<T>;
  always: (predicateOrFormula: (() => boolean) | Formula) => Formula;
  now: (predicate: () => boolean) => Formula;
  next: (predicate: () => boolean) => Formula;
  eventually: (predicate: () => boolean) => EventuallyFormula;
}

export type WeightedEntry = readonly [number, ActionGenerator];

import type { GeneratorNode } from "./action-tree.ts";

declare global {
  // eslint-disable-next-line no-var
  var __sanderling__: SanderlingRuntime;
}
