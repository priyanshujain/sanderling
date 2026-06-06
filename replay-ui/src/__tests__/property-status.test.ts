import { describe, it, expect } from "bun:test";
import {
  STATUS_ORDER,
  statusFor,
  statusForStep,
} from "../lib/property-status";
import { step } from "./fixtures";

// Bug class: RunDetail and ViolationsPanel once carried two copies of this
// status logic; if they drift, the same property shows a different verdict in
// the timeline vs the violations list. Both panels now share statusFor, so its
// precedence and the violated-first ordering must stay pinned.
describe("statusFor", () => {
  it("ranks violated over holds and defaults missing residuals to pending", () => {
    const violations = new Set(["v"]);
    expect(statusFor("v", violations, { v: { op: "true" } })).toBe("violated");
    expect(statusFor("h", violations, { h: { op: "true" } })).toBe("holds");
    expect(statusFor("p", violations, { p: { op: "false" } })).toBe("pending");
    expect(statusFor("x", violations, undefined)).toBe("pending");
  });
});

describe("statusForStep", () => {
  it("matches statusFor for the same step and is pending for a null step", () => {
    const s = step({
      violations: ["v"],
      residuals: { v: { op: "true" }, h: { op: "true" } },
    });
    expect(statusForStep("v", s)).toBe("violated");
    expect(statusForStep("h", s)).toBe("holds");
    expect(statusForStep("v", null)).toBe("pending");
  });
});

describe("STATUS_ORDER", () => {
  it("sorts violated before pending before holds", () => {
    const sorted = ["holds", "violated", "pending"].sort(
      (a, b) => STATUS_ORDER[a as never] - STATUS_ORDER[b as never],
    );
    expect(sorted).toEqual(["violated", "pending", "holds"]);
  });
});
