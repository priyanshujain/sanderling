import type { Hierarchy } from "../types";

// Tap points and resolved bounds in the trace share the hierarchy root's
// coordinate space (iOS points, Android pixels, web CSS px). Screenshots may
// be scaled (iOS 3x, web DPR>1), so the overlay viewBox must come from the
// device bounds, not the image's natural pixel size.
//
// Use the maximum extent across all elements (mirroring the runner's
// screenBounds), NOT the first positive-bounds element: the first element is
// often a status-bar/decor node a few px tall (e.g. 320x24 on Android), which
// would give a wildly wrong aspect ratio and squash the overlay into a band.
export function deviceSpaceOf(
  hierarchy?: Hierarchy,
): { width: number; height: number } | undefined {
  let width = 0;
  let height = 0;
  for (const element of hierarchy?.elements ?? []) {
    if (element.bounds.right > width) width = element.bounds.right;
    if (element.bounds.bottom > height) height = element.bounds.bottom;
  }
  if (width > 0 && height > 0) {
    return { width, height };
  }
  return undefined;
}
