import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import RunList from "../routes/RunList";
import type { RunSummary } from "../types";

describe("RunList", () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    globalThis.fetch = vi.fn();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("renders a row for each run returned by the api", async () => {
    const sample: RunSummary[] = [
      {
        id: "run-abc",
        started_at: "2026-04-20T10:00:00Z",
        spec_path: "specs/checkout.spec.ts",
        seed: 7,
        platform: "android",
        bundle_id: "com.example.folio",
        duration_millis: 0,
        step_count: 12,
        violation_count: 2,
        in_progress: false,
      },
    ];
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve(sample),
    } as Response);

    render(
      <MemoryRouter>
        <RunList />
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(screen.getByText("specs/checkout.spec.ts")).toBeInTheDocument();
    });
    expect(screen.getByText("2")).toBeInTheDocument();
    expect(screen.getByText("12")).toBeInTheDocument();
  });
});
