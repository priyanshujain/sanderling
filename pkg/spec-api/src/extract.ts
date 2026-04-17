import type { Extracted, State } from "./types.ts";

export function extract<T>(getter: (state: State) => T): Extracted<T> {
  return globalThis.__uatu__.extract(getter);
}
