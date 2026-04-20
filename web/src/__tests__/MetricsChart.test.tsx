import { describe, expect, it, vi } from "vitest";
import { fireEvent, render } from "@testing-library/react";
import MetricsChart from "../panels/MetricsChart";
import type { MetricsSample } from "../panels/MetricsChart";

const MB = 1024 * 1024;

const samplesWithMetrics: MetricsSample[] = [
  { stepIndex: 1, timestamp: "t1", metrics: { cpu_percent: 12, heap_bytes: 30 * MB } },
  { stepIndex: 2, timestamp: "t2", metrics: { cpu_percent: 48, heap_bytes: 65 * MB } },
  { stepIndex: 3, timestamp: "t3", metrics: { cpu_percent: 72, heap_bytes: 90 * MB } },
  { stepIndex: 4, timestamp: "t4", metrics: { cpu_percent: 20, heap_bytes: 40 * MB } },
];

const samplesWithoutMetrics: MetricsSample[] = [
  { stepIndex: 1, timestamp: "t1" },
  { stepIndex: 2, timestamp: "t2" },
];

describe("MetricsChart", () => {
  it("renders the empty state when samples is empty", () => {
    const { getByText } = render(
      <MetricsChart samples={[]} selectedIndex={1} onSelect={() => {}} />,
    );
    expect(getByText("no metrics")).toBeInTheDocument();
  });

  it("renders the empty state when no sample has metrics", () => {
    const { getByText } = render(
      <MetricsChart
        samples={samplesWithoutMetrics}
        selectedIndex={1}
        onSelect={() => {}}
      />,
    );
    expect(getByText("no metrics")).toBeInTheDocument();
  });

  it("renders HEAP and CPU lane groups when samples have metrics", () => {
    const { container } = render(
      <MetricsChart samples={samplesWithMetrics} selectedIndex={1} onSelect={() => {}} />,
    );
    expect(container.querySelector('[data-lane="HEAP"]')).not.toBeNull();
    expect(container.querySelector('[data-lane="CPU"]')).not.toBeNull();
  });

  it("draws a polyline per lane when metrics are present", () => {
    const { container } = render(
      <MetricsChart samples={samplesWithMetrics} selectedIndex={1} onSelect={() => {}} />,
    );
    expect(container.querySelector('polyline[data-series="heap"]')).not.toBeNull();
    expect(container.querySelector('polyline[data-series="cpu"]')).not.toBeNull();
  });

  it("skips undefined values when drawing the line", () => {
    const mixed: MetricsSample[] = [
      { stepIndex: 1, timestamp: "t1", metrics: { cpu_percent: 10, heap_bytes: 10 * MB } },
      { stepIndex: 2, timestamp: "t2" },
      { stepIndex: 3, timestamp: "t3", metrics: { cpu_percent: 30, heap_bytes: 30 * MB } },
    ];
    const { container } = render(
      <MetricsChart samples={mixed} selectedIndex={1} onSelect={() => {}} />,
    );
    const heap = container.querySelector('polyline[data-series="heap"]');
    expect(heap).not.toBeNull();
    const points = heap?.getAttribute("points") ?? "";
    expect(points.trim().split(/\s+/)).toHaveLength(2);
  });

  it("renders a clickable hit rect per step per lane with data-step-index", () => {
    const { container } = render(
      <MetricsChart samples={samplesWithMetrics} selectedIndex={1} onSelect={() => {}} />,
    );
    const heapHits = container.querySelectorAll('[data-lane-hit="HEAP"]');
    const cpuHits = container.querySelectorAll('[data-lane-hit="CPU"]');
    expect(heapHits).toHaveLength(samplesWithMetrics.length);
    expect(cpuHits).toHaveLength(samplesWithMetrics.length);
  });

  it("calls onSelect with the step index when a hit rect is clicked", () => {
    const onSelect = vi.fn();
    const { container } = render(
      <MetricsChart samples={samplesWithMetrics} selectedIndex={1} onSelect={onSelect} />,
    );
    const hit = container.querySelector(
      '[data-lane-hit="HEAP"][data-step-index="3"]',
    ) as Element | null;
    expect(hit).not.toBeNull();
    fireEvent.click(hit as Element);
    expect(onSelect).toHaveBeenCalledWith(3);
  });

  it("calls onSelect from the CPU lane hit rect as well", () => {
    const onSelect = vi.fn();
    const { container } = render(
      <MetricsChart samples={samplesWithMetrics} selectedIndex={1} onSelect={onSelect} />,
    );
    const hit = container.querySelector(
      '[data-lane-hit="CPU"][data-step-index="4"]',
    ) as Element | null;
    expect(hit).not.toBeNull();
    fireEvent.click(hit as Element);
    expect(onSelect).toHaveBeenCalledWith(4);
  });

  it("renders the selected-step highlight with data-selected=true", () => {
    const { container } = render(
      <MetricsChart samples={samplesWithMetrics} selectedIndex={2} onSelect={() => {}} />,
    );
    const highlight = container.querySelector('[data-selected="true"]');
    expect(highlight).not.toBeNull();
  });

  it("positions the highlight centered on the selected index column", () => {
    const { container } = render(
      <MetricsChart samples={samplesWithMetrics} selectedIndex={3} onSelect={() => {}} />,
    );
    const highlight = container.querySelector(
      'rect[data-selected="true"]',
    ) as SVGRectElement | null;
    expect(highlight).not.toBeNull();
    const x = Number(highlight?.getAttribute("x"));
    const width = Number(highlight?.getAttribute("width"));
    const center = x + width / 2;
    // selectedIndex=3 is the 3rd of 4 samples (index 2)
    // plotWidth = 1000 - 18 - 44 = 938; columnWidth = 938/4 = 234.5
    // expected center = 18 + 2*234.5 + 234.5/2 = 604.25
    expect(center).toBeCloseTo(604.25, 1);
  });

  it("renders point tooltips with step number and formatted value", () => {
    const { container } = render(
      <MetricsChart samples={samplesWithMetrics} selectedIndex={1} onSelect={() => {}} />,
    );
    const titles = Array.from(container.querySelectorAll(".metrics-point title")).map(
      (node) => node.textContent,
    );
    expect(titles.some((text) => text?.startsWith("step 1:"))).toBe(true);
    expect(titles.some((text) => text?.includes("MB"))).toBe(true);
    expect(titles.some((text) => text?.includes("%"))).toBe(true);
  });

  it("omits the highlight when selectedIndex is not among samples", () => {
    const { container } = render(
      <MetricsChart samples={samplesWithMetrics} selectedIndex={999} onSelect={() => {}} />,
    );
    expect(container.querySelector('[data-selected="true"]')).toBeNull();
  });

  it("does not render a STEPS status lane", () => {
    const { container } = render(
      <MetricsChart samples={samplesWithMetrics} selectedIndex={1} onSelect={() => {}} />,
    );
    expect(container.querySelector('[data-lane="STATUS"]')).toBeNull();
    expect(container.querySelector(".metrics-status-cell")).toBeNull();
  });

  it("does not render circle dot markers on the polylines", () => {
    const { container } = render(
      <MetricsChart samples={samplesWithMetrics} selectedIndex={1} onSelect={() => {}} />,
    );
    expect(container.querySelectorAll("circle.metrics-point")).toHaveLength(0);
  });

  it("renders mm:ss time-axis labels instead of step indices", () => {
    const timed: MetricsSample[] = [
      { stepIndex: 1, timestamp: "2024-01-01T00:00:00.000Z", metrics: { cpu_percent: 10, heap_bytes: 10 * MB } },
      { stepIndex: 2, timestamp: "2024-01-01T00:00:12.000Z", metrics: { cpu_percent: 20, heap_bytes: 20 * MB } },
      { stepIndex: 3, timestamp: "2024-01-01T00:00:24.000Z", metrics: { cpu_percent: 30, heap_bytes: 30 * MB } },
    ];
    const { container } = render(
      <MetricsChart samples={timed} selectedIndex={1} onSelect={() => {}} />,
    );
    const labels = Array.from(container.querySelectorAll(".metrics-axis-label")).map(
      (node) => node.textContent,
    );
    expect(labels[0]).toBe("00:00");
    expect(labels[labels.length - 1]).toBe("00:24");
    expect(labels.every((label) => /^\d{2}:\d{2}$/.test(label ?? ""))).toBe(true);
  });

  it("renders only min and max tick labels per lane with compact units", () => {
    const { container } = render(
      <MetricsChart samples={samplesWithMetrics} selectedIndex={1} onSelect={() => {}} />,
    );
    const labels = Array.from(container.querySelectorAll(".metrics-tick-label")).map(
      (node) => node.textContent,
    );
    expect(labels).toContain("0B");
    expect(labels).toContain("0%");
    expect(labels).toContain("100%");
    expect(labels.some((label) => label?.endsWith("M"))).toBe(true);
    expect(labels.every((label) => !label?.endsWith("MB"))).toBe(true);
  });
});
