export type Snapshots = Record<string, unknown>;

export interface AccessibilityElement {
  id?: string;
  text?: string;
  bounds?: { left: number; top: number; right: number; bottom: number };
}

export interface AccessibilityTree {
  find(selector: string): AccessibilityElement | undefined;
  findAll(selector: string): AccessibilityElement[];
}

export interface State {
  snapshots: Snapshots;
  ax: AccessibilityTree;
}

export interface Extracted<T> {
  readonly current: T;
  readonly previous: T | undefined;
}

export type TapAction = { kind: "Tap"; on: string | AccessibilityElement };
export type InputTextAction = { kind: "InputText"; into: string | AccessibilityElement; text: string };
export type Action = TapAction | InputTextAction;

export interface ActionGenerator {
  readonly __uatuActionGenerator: true;
  generate(): Action[];
}

export interface Formula {
  readonly __uatuFormula: true;
}

export interface UatuRuntime {
  extract: <T>(getter: (state: State) => T) => Extracted<T>;
  always: (predicate: () => boolean) => Formula;
  actions: (generator: () => Action[]) => ActionGenerator;
  weighted: (...entries: WeightedEntry[]) => ActionGenerator;
  tap: (parameters: { on: string | AccessibilityElement }) => TapAction;
  inputText: (parameters: { into: string | AccessibilityElement; text: string }) => InputTextAction;
  taps: ActionGenerator;
  swipes: ActionGenerator;
}

export type WeightedEntry = readonly [number, ActionGenerator];

declare global {
  // eslint-disable-next-line no-var
  var __uatu__: UatuRuntime;
}
