import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { listRuns } from "../api";
import type { RunSummary } from "../types";

describe("listRuns", () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    globalThis.fetch = vi.fn();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("calls /api/runs and returns parsed runs", async () => {
    const sample: RunSummary[] = [
      {
        id: "run-1",
        started_at: "2026-04-20T10:00:00Z",
        spec_path: "specs/login.spec.ts",
        seed: 42,
        platform: "android",
        bundle_id: "com.example.folio",
        duration_millis: 0,
        step_count: 5,
        violation_count: 0,
        in_progress: false,
      },
    ];
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve(sample),
    } as Response);

    const result = await listRuns();

    expect(globalThis.fetch).toHaveBeenCalledWith("/api/runs", expect.any(Object));
    expect(result).toEqual(sample);
  });

  it("throws when the response is non-2xx", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValue({
      ok: false,
      status: 500,
      statusText: "Internal Server Error",
    } as Response);

    await expect(listRuns()).rejects.toThrow(/500/);
  });
});
