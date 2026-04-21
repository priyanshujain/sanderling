import type { Run, RunSummary, Step } from "./types";

async function getJson<T>(path: string): Promise<T> {
  const response = await fetch(path, { headers: { Accept: "application/json" } });
  if (!response.ok) {
    throw new Error(`request failed: ${response.status} ${response.statusText} (${path})`);
  }
  return (await response.json()) as T;
}

export function listRuns(): Promise<RunSummary[]> {
  return getJson<RunSummary[]>("/api/runs");
}

export function getRun(runId: string): Promise<Run> {
  return getJson<Run>(`/api/runs/${encodeURIComponent(runId)}`);
}

export function getStep(runId: string, index: number): Promise<Step> {
  return getJson<Step>(`/api/runs/${encodeURIComponent(runId)}/steps/${index}`);
}

export function screenshotUrl(runId: string, name: string): string {
  return `/api/runs/${encodeURIComponent(runId)}/screenshots/${encodeURIComponent(name)}`;
}
