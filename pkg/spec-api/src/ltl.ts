import type { EventuallyFormula, Formula } from "./types.ts";

export function always(predicateOrFormula: (() => boolean) | Formula): Formula {
  return globalThis.__uatu__.always(predicateOrFormula);
}

export function now(predicate: () => boolean): Formula {
  return globalThis.__uatu__.now(predicate);
}

export function next(predicate: () => boolean): Formula {
  return globalThis.__uatu__.next(predicate);
}

// An unbounded `eventually` never forces a violation within a finite run —
// prefer `.within(n, unit)` when you want the verifier to fail a property
// that stalls.
export function eventually(predicate: () => boolean): EventuallyFormula {
  return globalThis.__uatu__.eventually(predicate);
}
