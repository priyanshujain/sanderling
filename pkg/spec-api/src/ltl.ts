import type { Formula } from "./types.ts";

export function always(predicate: () => boolean): Formula {
  return globalThis.__uatu__.always(predicate);
}
