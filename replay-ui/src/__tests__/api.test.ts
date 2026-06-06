import { describe, it, expect } from "bun:test";
import { getJson, screenshotUrl } from "../api";

describe("screenshotUrl", () => {
  it("percent-encodes runId and name with reserved characters", () => {
    expect(screenshotUrl("run #1/a", "step 00001.png")).toBe(
      "/api/runs/run%20%231%2Fa/screenshots/step%2000001.png",
    );
  });
});

describe("getJson", () => {
  it("returns the decoded body on a 200 response", async () => {
    const server = Bun.serve({
      port: 0,
      fetch: () => Response.json({ ok: true }),
    });
    try {
      const body = await getJson<{ ok: boolean }>(server.url.href);
      expect(body).toEqual({ ok: true });
    } finally {
      server.stop(true);
    }
  });

  it("throws on a non-ok response instead of returning the error body", async () => {
    const server = Bun.serve({
      port: 0,
      fetch: () => new Response("boom", { status: 500 }),
    });
    try {
      await expect(getJson(server.url.href)).rejects.toThrow("500");
    } finally {
      server.stop(true);
    }
  });
});
