import { describe, it, expect } from "bun:test";
import { htmlUrl, screenshotUrl } from "../api";

describe("htmlUrl", () => {
  it("encodes runId and name", () => {
    expect(htmlUrl("run-1", "step-00001.html")).toBe(
      "/api/runs/run-1/html/step-00001.html",
    );
    expect(htmlUrl("run with spaces", "after step.html")).toBe(
      "/api/runs/run%20with%20spaces/html/after%20step.html",
    );
  });
});

describe("screenshotUrl", () => {
  it("encodes runId and name", () => {
    expect(screenshotUrl("run-1", "step-00001.png")).toBe(
      "/api/runs/run-1/screenshots/step-00001.png",
    );
  });
});
