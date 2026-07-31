import { describe, it, expect } from "bun:test";
import { deviceSpaceOf } from "../lib/device-space";
import type { Hierarchy } from "../types";

function hierarchyWithRoot(right: number, bottom: number): Hierarchy {
  return {
    elements: [
      {
        bounds: { left: 0, top: 0, right, bottom },
      },
    ],
  };
}

describe("deviceSpaceOf", () => {
  it("returns the root bounds extent", () => {
    expect(deviceSpaceOf(hierarchyWithRoot(393, 852))).toEqual({
      width: 393,
      height: 852,
    });
  });

  it("returns undefined without a hierarchy", () => {
    expect(deviceSpaceOf(undefined)).toBeUndefined();
  });

  it("returns undefined for an empty hierarchy", () => {
    expect(deviceSpaceOf({ elements: [] })).toBeUndefined();
  });

  it("returns undefined for non-positive root bounds", () => {
    expect(deviceSpaceOf(hierarchyWithRoot(0, 852))).toBeUndefined();
    expect(deviceSpaceOf(hierarchyWithRoot(393, 0))).toBeUndefined();
    expect(deviceSpaceOf(hierarchyWithRoot(-1, -1))).toBeUndefined();
  });

  it("skips the iOS synthetic zero-bounds root", () => {
    const hierarchy: Hierarchy = {
      elements: [
        { bounds: { left: 0, top: 0, right: 0, bottom: 0 } },
        { bounds: { left: 0, top: 0, right: 402, bottom: 874 } },
        { bounds: { left: 20, top: 100, right: 380, bottom: 150 } },
      ],
    };
    expect(deviceSpaceOf(hierarchy)).toEqual({ width: 402, height: 874 });
  });

  it("uses the screen extent, not a short status-bar node listed first", () => {
    // Regression: a 320x24 status bar precedes the 320x640 screen. Picking the
    // first positive-bounds element gave a 320/24 aspect ratio, squashing the
    // screenshot overlay into a grey horizontal band.
    const hierarchy: Hierarchy = {
      elements: [
        { bounds: { left: 0, top: 0, right: 320, bottom: 24 } },
        { bounds: { left: 0, top: 0, right: 320, bottom: 640 } },
        { bounds: { left: 20, top: 271, right: 286, bottom: 319 } },
      ],
    };
    expect(deviceSpaceOf(hierarchy)).toEqual({ width: 320, height: 640 });
  });
});
