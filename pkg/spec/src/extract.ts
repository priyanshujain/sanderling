import type { AccessibilityElement, Extracted, State } from "./types.ts";

export function extract<T>(getter: (state: State) => T): Extracted<T>;
export function extract<T>(name: string, getter: (state: State) => T): Extracted<T>;
export function extract<T>(
  nameOrGetter: string | ((state: State) => T),
  maybeGetter?: (state: State) => T,
): Extracted<T> {
  if (typeof nameOrGetter === "string") {
    if (maybeGetter == null) {
      throw new TypeError("extract(name, getter): getter is required when a name is supplied");
    }
    return globalThis.__sanderling__.extract(maybeGetter, nameOrGetter);
  }
  return globalThis.__sanderling__.extract(nameOrGetter);
}

const KEY_DELIMITER = "\x1f";

export function keyedBy(
  element: AccessibilityElement | undefined,
  tags: readonly string[],
): string {
  if (!element) return "";
  return tags
    .map(tag => element.find({ testTag: tag })?.text ?? "")
    .join(KEY_DELIMITER);
}
