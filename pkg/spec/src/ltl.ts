import type { EventuallyFormula, Formula } from "./types.ts";

export function always(predicateOrFormula: (() => boolean) | Formula): Formula {
  return globalThis.__sanderling__.always(predicateOrFormula);
}

export function now(predicate: () => boolean): Formula {
  return globalThis.__sanderling__.now(predicate);
}

export function next(predicate: () => boolean): Formula {
  return globalThis.__sanderling__.next(predicate);
}

// An unbounded `eventually` that never fires is violated when the run ends,
// with the reason "eventually never satisfied", so a goal the run does not
// reach is a violation every time. `.within(n, unit)` convicts at the step the
// window closes instead of at run end. `"steps"` counts observed steps rather
// than wall-clock time, which is what keeps the window the same size across
// runs of different speeds.
export function eventually(predicate: () => boolean): EventuallyFormula {
  return globalThis.__sanderling__.eventually(predicate);
}
