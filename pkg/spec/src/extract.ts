import type { AccessibilityElement, Extracted, State } from "./types.ts";

export function extract<T>(getter: (state: State) => T): Extracted<T> {
  return globalThis.__sanderling__.extract(getter);
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
