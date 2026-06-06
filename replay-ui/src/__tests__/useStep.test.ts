import { describe, it, expect } from "bun:test";
import { clampIndex } from "../hooks/useStep";

// Bug class: a wrong boundary lets the URL address step 0 or a step past the
// end, so the viewer requests a non-existent step and renders nothing. Steps
// are 1-based and capped at maxIndex.
describe("clampIndex", () => {
  const cases: [number, number | undefined, number][] = [
    [0, 5, 1],
    [-3, 5, 1],
    [1, 5, 1],
    [3, 5, 3],
    [5, 5, 5],
    [6, 5, 5],
    [100, 5, 5],
    [3, undefined, 3],
    [0, undefined, 1],
  ];
  for (const [index, max, expected] of cases) {
    it(`clamps (${index}, ${max}) -> ${expected}`, () => {
      expect(clampIndex(index, max)).toBe(expected);
    });
  }
});
