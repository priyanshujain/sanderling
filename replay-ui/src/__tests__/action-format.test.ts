import { describe, it, expect } from "bun:test";
import {
  formatActionRow,
  formatElapsed,
  parseSelector,
  tagFromSelector,
} from "../lib/action-format";
import type { StepSummary } from "../types";

function summary(over: Partial<StepSummary>): StepSummary {
  return {
    index: 0,
    timestamp: "1970-01-01T00:00:00.000Z",
    has_violations: false,
    has_exceptions: false,
    ...over,
  };
}

// Bug class: selector parsing mislabels every action row — splitting on the
// wrong colon, treating a value-with-colon as the kind, or dropping the prefix
// ellipsis would render the wrong target tag for every step.
describe("parseSelector", () => {
  const cases: { input: string; out: ReturnType<typeof parseSelector> }[] = [
    { input: "id:login", out: { kind: "id", value: "login" } },
    { input: "text:Sign In", out: { kind: "text", value: "Sign In" } },
    { input: "textPrefix:Hello", out: { kind: "textPrefix", value: "Hello" } },
    { input: "id:com.app:id/btn", out: { kind: "id", value: "com.app:id/btn" } },
    { input: "bogus:x", out: null },
    { input: ":leading", out: null },
    { input: "no-colon", out: null },
  ];
  for (const { input, out } of cases) {
    it(`parses ${input}`, () => {
      expect(parseSelector(input)).toEqual(out);
    });
  }
});

describe("tagFromSelector", () => {
  it("appends ellipsis only for prefix selectors", () => {
    expect(tagFromSelector("textPrefix:Hel")).toBe("Hel...");
    expect(tagFromSelector("text:Hello")).toBe("Hello");
    expect(tagFromSelector("plain")).toBe("plain");
  });
});

describe("formatActionRow", () => {
  it("observes when no action kind, with and without screen", () => {
    expect(formatActionRow(summary({}))).toEqual({
      verb: "Observe",
      target: "",
      targetIsTag: false,
    });
    expect(formatActionRow(summary({ screen: "Home" }))).toEqual({
      verb: "Observe",
      target: "@ Home",
      targetIsTag: false,
    });
  });

  it("treats a selector label as a tag and a coordinate label as literal", () => {
    expect(
      formatActionRow(summary({ action_kind: "Tap", action_label: "id:btn" })),
    ).toEqual({ verb: "Click", target: "btn", targetIsTag: true });
    expect(
      formatActionRow(summary({ action_kind: "Tap", action_label: "(10, 20)" })),
    ).toEqual({ verb: "Click", target: "(10, 20)", targetIsTag: false });
  });

  it("maps known verbs and falls back to the raw kind", () => {
    expect(formatActionRow(summary({ action_kind: "InputText", action_label: "hi" })).verb).toBe("Type");
    expect(formatActionRow(summary({ action_kind: "Swipe" })).verb).toBe("Swipe");
    expect(formatActionRow(summary({ action_kind: "Custom" })).verb).toBe("Custom");
  });
});

describe("formatElapsed", () => {
  it("formats mm:ss.mmm and clamps negatives to zero", () => {
    expect(formatElapsed(0)).toBe("00:00.000");
    expect(formatElapsed(65_432)).toBe("01:05.432");
    expect(formatElapsed(-50)).toBe("00:00.000");
  });
});
