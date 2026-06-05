import type { Hierarchy } from "../types";

// Tap points and resolved bounds in the trace share the hierarchy root's
// coordinate space (iOS points, Android pixels, web CSS px). Screenshots may
// be scaled (iOS 3x, web DPR>1), so the overlay viewBox must come from the
// root bounds, not the image's natural pixel size.
export function deviceSpaceOf(
  hierarchy?: Hierarchy,
): { width: number; height: number } | undefined {
  const root = hierarchy?.elements?.[0];
  if (!root) return undefined;
  const width = root.bounds.right;
  const height = root.bounds.bottom;
  if (!(width > 0) || !(height > 0)) return undefined;
  return { width, height };
}
