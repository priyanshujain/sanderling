import { describe, it, expect } from "bun:test";
import { screenshotUrl } from "../api";

describe("screenshotUrl", () => {
  it("encodes runId and name", () => {
    expect(screenshotUrl("run-1", "step-00001.png")).toBe(
      "/api/runs/run-1/screenshots/step-00001.png",
    );
  });
});
