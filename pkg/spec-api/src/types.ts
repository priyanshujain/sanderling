export type Snapshots = Record<string, unknown>;

export interface AccessibilityElement {
  id?: string;
  text?: string;
  bounds?: { left: number; top: number; right: number; bottom: number };
  x?: number;
  y?: number;
}

export interface AccessibilityTree {
  find(selector: string): AccessibilityElement | undefined;
  findAll(selector: string): AccessibilityElement[];
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
  readonly __uatuActionGenerator: true;
  generate(): Action[];
}

export interface Formula {
  readonly __uatuFormula: true;
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

export interface UatuRuntime {
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
  var __uatu__: UatuRuntime;
}
