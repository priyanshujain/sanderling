import { describe, it, expect } from "bun:test";
import {
  buildPath,
  formatHeap,
  formatTime,
  fractionFor,
} from "../lib/metrics-format";

describe("formatHeap", () => {
  const cases: [number, string][] = [
    [0, "0B"],
    [-1, "0B"],
    [2048, "2K"],
    [1024 * 1024, "1M"],
    [5 * 1024 * 1024, "5M"],
    [2 * 1024 * 1024 * 1024, "2.0G"],
  ];
  for (const [bytes, expected] of cases) {
    it(`${bytes} -> ${expected}`, () => {
      expect(formatHeap(bytes)).toBe(expected);
    });
  }
});

describe("formatTime", () => {
  it("formats mm:ss and clamps negatives", () => {
    expect(formatTime(0)).toBe("00:00");
    expect(formatTime(65_000)).toBe("01:05");
    expect(formatTime(-10)).toBe("00:00");
  });
});

describe("fractionFor", () => {
  it("centers a lone point and spreads the rest across 0..1", () => {
    expect(fractionFor(0, 1)).toBe(0.5);
    expect(fractionFor(0, 5)).toBe(0);
    expect(fractionFor(4, 5)).toBe(1);
    expect(fractionFor(2, 5)).toBe(0.5);
  });
});

// Bug class: a missing sample must lift the pen (M) so the chart does not draw
// a straight line bridging the gap, which would imply data that was never
// measured.
describe("buildPath gap handling", () => {
  it("starts a new subpath after each undefined value", () => {
    const samples = [{ v: 0 }, { v: 100 }, { v: undefined }, { v: 50 }];
    const path = buildPath(samples, (s) => s.v, 100);
    const commands = path.match(/[ML]/g);
    expect(commands).toEqual(["M", "L", "M"]);
    expect(path.startsWith("M0.0000,1.0000")).toBe(true);
  });

  it("emits empty string when every sample is missing", () => {
    const samples = [{ v: undefined }, { v: undefined }];
    expect(buildPath(samples, (s) => s.v, 100)).toBe("");
  });

  it("clamps values above the ceiling to the top of the lane", () => {
    const path = buildPath([{ v: 200 }], (s) => s.v, 100);
    expect(path).toBe("M0.5000,0.0000");
  });
});
