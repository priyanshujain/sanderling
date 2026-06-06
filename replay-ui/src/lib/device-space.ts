import type { Hierarchy } from "../types";

// Tap points and resolved bounds in the trace share the hierarchy root's
// coordinate space (iOS points, Android pixels, web CSS px). Screenshots may
// be scaled (iOS 3x, web DPR>1), so the overlay viewBox must come from the
// root bounds, not the image's natural pixel size. Elements are in pre-order;
// the first one with positive extent is the root window (iOS prepends a
// synthetic zero-bounds node, so plain elements[0] is not enough).
export function deviceSpaceOf(
  hierarchy?: Hierarchy,
): { width: number; height: number } | undefined {
  for (const element of hierarchy?.elements ?? []) {
    const width = element.bounds.right;
    const height = element.bounds.bottom;
    if (width > 0 && height > 0) {
      return { width, height };
    }
  }
  return undefined;
}
