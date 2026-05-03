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
}

/**
 * Object-form selector for `find` / `findAll`. Known attributes are typed
 * via `KnownAttrSelectors`; arbitrary string keys are still allowed for
 * raw driver attributes the typed surface doesn't yet cover.
 */
export type AttrSelector = KnownAttrSelectors & {
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

export interface State {
  snapshots: Snapshots;
  ax: AccessibilityTree;
  lastAction: Action | null;
  time: number;
  logs: readonly LogEntry[];
  exceptions: readonly ExceptionRecord[];
}

/**
 * State as observed inside a web (V8/browser) extractor. Adds the live
 * `document` and `window` handles. `state.document` is V8-only — goja-side
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
}

export interface Point {
  x: number;
  y: number;
}

export type TapAction = { kind: "Tap"; on: string | AccessibilityElement };
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
  | InputTextAction
  | SwipeAction
  | PressKeyAction
  | WaitAction;

export type Key =
  | "back"
  | "home"
  | "enter"
  | "tab"
  | "up"
  | "down"
  | "left"
  | "right";

export interface ActionGenerator {
  readonly __sanderlingActionGenerator: true;
  generate(): Action[];
}

export interface Formula {
  readonly __sanderlingFormula: true;
  implies(other: Formula): Formula;
  or(other: Formula): Formula;
  and(other: Formula): Formula;
  not(): Formula;
}

export interface EventuallyFormula extends Formula {
  within(amount: number, unit: "milliseconds" | "seconds" | "steps"): Formula;
}

export interface Sampler<T> {
  generate(): T;
}

export interface SanderlingRuntime {
  extract: <T>(getter: (state: State) => T) => Extracted<T>;
  always: (predicateOrFormula: (() => boolean) | Formula) => Formula;
  now: (predicate: () => boolean) => Formula;
  next: (predicate: () => boolean) => Formula;
  eventually: (predicate: () => boolean) => EventuallyFormula;
  actions: (generator: () => Action[]) => ActionGenerator;
  weighted: (...entries: WeightedEntry[]) => ActionGenerator;
  from: <T>(items: readonly T[]) => Sampler<T>;
  tap: (parameters: { on: string | AccessibilityElement }) => TapAction;
  inputText: (parameters: {
    into: string | AccessibilityElement;
    text: string;
  }) => InputTextAction;
  swipe: (parameters: {
    from: Point | AccessibilityElement;
    to: Point | AccessibilityElement;
    durationMillis?: number;
  }) => SwipeAction;
  pressKey: (parameters: { key: Key }) => PressKeyAction;
  wait: (parameters: { durationMillis: number }) => WaitAction;
  taps: ActionGenerator;
  swipes: ActionGenerator;
  waitOnce: ActionGenerator;
  pressKeys: ActionGenerator;
}

export type WeightedEntry = readonly [number, ActionGenerator];

declare global {
  // eslint-disable-next-line no-var
  var __sanderling__: SanderlingRuntime;
}
