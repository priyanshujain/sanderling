import type { Metrics } from "../types";
import type { PropertyLane } from "./Timeline";
import "./MetricsChart.css";

export interface MetricsSample {
  stepIndex: number;
  timestamp: string;
  metrics?: Metrics;
}

export interface MetricsChartProps {
  samples: MetricsSample[];
  selectedIndex: number;
  onSelect: (stepIndex: number) => void;
  exceptionStepIndices?: number[];
  propertyLanes?: PropertyLane[];
  violationStepIndices?: number[];
}

const CHART_WIDTH = 1000;
const LABEL_WIDTH = 60;
const LANE_HEIGHT = 32;
const LANE_GAP = 6;
const AXIS_HEIGHT = 12;
const STATUS_LANE_HEIGHT = 8;
const TICK_COUNT = 2;

const MB = 1024 * 1024;

interface LaneGeometry {
  top: number;
  bottom: number;
  plotLeft: number;
  plotRight: number;
}

function heapCeiling(maxBytes: number): number {
  if (maxBytes <= 0) return 50 * MB;
  const steps = [
    50 * MB,
    100 * MB,
    200 * MB,
    300 * MB,
    500 * MB,
    750 * MB,
    1024 * MB,
    1536 * MB,
    2048 * MB,
  ];
  for (const step of steps) {
    if (maxBytes <= step) return step;
  }
  const gib = 1024 * MB;
  return Math.ceil(maxBytes / gib) * gib;
}

function formatHeap(bytes: number): string {
  if (bytes === 0) return "0B";
  if (bytes < MB) return `${Math.round(bytes / 1024)}KB`;
  if (bytes < 1024 * MB) return `${Math.round(bytes / MB)}MB`;
  return `${(bytes / (1024 * MB)).toFixed(1)}GB`;
}

function cpuCeiling(maxPercent: number): number {
  if (maxPercent <= 100) return 100;
  return Math.ceil(maxPercent / 50) * 50;
}

function buildTicks(ceiling: number, count: number): number[] {
  const ticks: number[] = [];
  for (let i = 0; i <= count; i += 1) {
    ticks.push((ceiling * i) / count);
  }
  return ticks;
}

function buildPolyline(
  samples: MetricsSample[],
  getValue: (sample: MetricsSample) => number | undefined,
  ceiling: number,
  geometry: LaneGeometry,
  columnWidth: number,
): string {
  const points: string[] = [];
  samples.forEach((sample, index) => {
    const value = getValue(sample);
    if (value === undefined) return;
    const x = geometry.plotLeft + index * columnWidth + columnWidth / 2;
    const ratio = ceiling === 0 ? 0 : Math.min(value / ceiling, 1);
    const y = geometry.bottom - ratio * (geometry.bottom - geometry.top);
    points.push(`${x.toFixed(2)},${y.toFixed(2)}`);
  });
  return points.join(" ");
}

export default function MetricsChart({
  samples,
  selectedIndex,
  onSelect,
  exceptionStepIndices,
  propertyLanes,
  violationStepIndices,
}: MetricsChartProps) {
  if (samples.length === 0) {
    return <div className="status-block">no metrics</div>;
  }

  const hasMetrics = samples.some((sample) => sample.metrics !== undefined);
  if (!hasMetrics) {
    return <div className="status-block">no metrics</div>;
  }

  const plotWidth = CHART_WIDTH - LABEL_WIDTH;
  const columnWidth = plotWidth / samples.length;
  const violationSet = new Set(violationStepIndices ?? []);
  const exceptionSet = new Set(exceptionStepIndices ?? []);

  const heapMax = samples.reduce((max, sample) => {
    const value = sample.metrics?.heap_bytes;
    return value !== undefined && value > max ? value : max;
  }, 0);
  const heapTop = heapCeiling(heapMax);

  const cpuMax = samples.reduce((max, sample) => {
    const value = sample.metrics?.cpu_percent;
    return value !== undefined && value > max ? value : max;
  }, 0);
  const cpuTop = cpuCeiling(cpuMax);

  const statusTop = 0;
  const statusBottom = statusTop + STATUS_LANE_HEIGHT;
  const heapTopY = statusBottom + LANE_GAP;
  const heapGeometry: LaneGeometry = {
    top: heapTopY,
    bottom: heapTopY + LANE_HEIGHT,
    plotLeft: LABEL_WIDTH,
    plotRight: CHART_WIDTH,
  };
  const cpuGeometry: LaneGeometry = {
    top: heapGeometry.bottom + LANE_GAP,
    bottom: heapGeometry.bottom + LANE_GAP + LANE_HEIGHT,
    plotLeft: LABEL_WIDTH,
    plotRight: CHART_WIDTH,
  };
  const axisTop = cpuGeometry.bottom + LANE_GAP / 2;
  const totalHeight = axisTop + AXIS_HEIGHT;

  const heapPoints = buildPolyline(
    samples,
    (sample) => sample.metrics?.heap_bytes,
    heapTop,
    heapGeometry,
    columnWidth,
  );
  const cpuPoints = buildPolyline(
    samples,
    (sample) => sample.metrics?.cpu_percent,
    cpuTop,
    cpuGeometry,
    columnWidth,
  );

  const heapTicks = buildTicks(heapTop, TICK_COUNT);
  const cpuTicks = buildTicks(cpuTop, TICK_COUNT);

  const selectedColumn = samples.findIndex((sample) => sample.stepIndex === selectedIndex);
  const highlightX =
    selectedColumn >= 0 ? LABEL_WIDTH + selectedColumn * columnWidth + columnWidth / 2 : null;

  const anyStatus = (propertyLanes?.length ?? 0) > 0 || violationSet.size > 0;

  return (
    <svg
      className="metrics-chart"
      viewBox={`0 0 ${CHART_WIDTH} ${totalHeight}`}
      role="img"
      aria-label="metrics chart"
    >
      <g className="metrics-lane" data-lane="STATUS">
        <text
          className="metrics-lane-label"
          x={LABEL_WIDTH - 8}
          y={statusTop + STATUS_LANE_HEIGHT / 2}
          dominantBaseline="middle"
          textAnchor="end"
        >
          STEPS
        </text>
        {samples.map((sample, index) => {
          const x = LABEL_WIDTH + index * columnWidth + 1;
          const w = Math.max(columnWidth - 2, 1);
          let status: "violated" | "holds" | "pending" | "exception" = "holds";
          if (propertyLanes && propertyLanes.length > 0) {
            const statuses = propertyLanes.map((lane) => lane.statuses[index]);
            if (statuses.includes("violated")) status = "violated";
            else if (statuses.every((s) => s === "holds")) status = "holds";
            else status = "pending";
          } else {
            status = violationSet.has(sample.stepIndex) ? "violated" : "holds";
          }
          if (exceptionSet.has(sample.stepIndex) && status !== "violated") {
            status = "exception";
          }
          return (
            <rect
              key={`status-${sample.stepIndex}`}
              className="metrics-status-cell"
              data-status={status}
              data-step-index={sample.stepIndex}
              x={x}
              y={statusTop}
              width={w}
              height={STATUS_LANE_HEIGHT}
              onClick={() => onSelect(sample.stepIndex)}
            >
              <title>{`step ${sample.stepIndex}: ${status}`}</title>
            </rect>
          );
        })}
        {!anyStatus ? null : null}
      </g>

      <g className="metrics-lane" data-lane="HEAP">
        <rect
          x={LABEL_WIDTH}
          y={heapGeometry.top}
          width={plotWidth}
          height={LANE_HEIGHT}
          className="metrics-lane-bg"
        />
        <text
          className="metrics-lane-label"
          x={LABEL_WIDTH - 8}
          y={heapGeometry.top + LANE_HEIGHT / 2}
          dominantBaseline="middle"
          textAnchor="end"
        >
          HEAP
        </text>
        {heapTicks.map((tick, index) => {
          if (index === 0) return null;
          const ratio = heapTop === 0 ? 0 : tick / heapTop;
          const y = heapGeometry.bottom - ratio * (heapGeometry.bottom - heapGeometry.top);
          return (
            <g key={`heap-tick-${index}`} className="metrics-tick">
              <line
                x1={LABEL_WIDTH}
                x2={CHART_WIDTH}
                y1={y}
                y2={y}
                className="metrics-gridline"
              />
              <text
                x={CHART_WIDTH - 4}
                y={y - 2}
                textAnchor="end"
                className="metrics-tick-label"
              >
                {formatHeap(tick)}
              </text>
            </g>
          );
        })}
        {heapPoints.length > 0 ? (
          <polyline
            className="metrics-line"
            data-series="heap"
            points={heapPoints}
            fill="none"
          />
        ) : null}
        {samples.map((sample, index) => {
          const value = sample.metrics?.heap_bytes;
          if (value === undefined) return null;
          const ratio = heapTop === 0 ? 0 : Math.min(value / heapTop, 1);
          const cx = LABEL_WIDTH + index * columnWidth + columnWidth / 2;
          const cy = heapGeometry.bottom - ratio * (heapGeometry.bottom - heapGeometry.top);
          return (
            <circle
              key={`heap-point-${sample.stepIndex}`}
              className="metrics-point"
              data-step-index={sample.stepIndex}
              cx={cx}
              cy={cy}
              r={2}
            >
              <title>{`step ${sample.stepIndex}: ${formatHeap(value)}`}</title>
            </circle>
          );
        })}
        {samples.map((sample, index) => {
          const x = LABEL_WIDTH + index * columnWidth;
          return (
            <rect
              key={`heap-hit-${sample.stepIndex}`}
              className="metrics-hit"
              data-step-index={sample.stepIndex}
              data-lane-hit="HEAP"
              x={x}
              y={heapGeometry.top}
              width={columnWidth}
              height={LANE_HEIGHT}
              onClick={() => onSelect(sample.stepIndex)}
            >
              <title>{`step ${sample.stepIndex}`}</title>
            </rect>
          );
        })}
      </g>

      <g className="metrics-lane" data-lane="CPU">
        <rect
          x={LABEL_WIDTH}
          y={cpuGeometry.top}
          width={plotWidth}
          height={LANE_HEIGHT}
          className="metrics-lane-bg"
        />
        <text
          className="metrics-lane-label"
          x={LABEL_WIDTH - 8}
          y={cpuGeometry.top + LANE_HEIGHT / 2}
          dominantBaseline="middle"
          textAnchor="end"
        >
          CPU
        </text>
        {cpuTicks.map((tick, index) => {
          if (index === 0) return null;
          const ratio = cpuTop === 0 ? 0 : tick / cpuTop;
          const y = cpuGeometry.bottom - ratio * (cpuGeometry.bottom - cpuGeometry.top);
          return (
            <g key={`cpu-tick-${index}`} className="metrics-tick">
              <line
                x1={LABEL_WIDTH}
                x2={CHART_WIDTH}
                y1={y}
                y2={y}
                className="metrics-gridline"
              />
              <text
                x={CHART_WIDTH - 4}
                y={y - 2}
                textAnchor="end"
                className="metrics-tick-label"
              >
                {`${Math.round(tick)}%`}
              </text>
            </g>
          );
        })}
        {cpuPoints.length > 0 ? (
          <polyline
            className="metrics-line"
            data-series="cpu"
            points={cpuPoints}
            fill="none"
          />
        ) : null}
        {samples.map((sample, index) => {
          const value = sample.metrics?.cpu_percent;
          if (value === undefined) return null;
          const ratio = cpuTop === 0 ? 0 : Math.min(value / cpuTop, 1);
          const cx = LABEL_WIDTH + index * columnWidth + columnWidth / 2;
          const cy = cpuGeometry.bottom - ratio * (cpuGeometry.bottom - cpuGeometry.top);
          return (
            <circle
              key={`cpu-point-${sample.stepIndex}`}
              className="metrics-point"
              data-step-index={sample.stepIndex}
              cx={cx}
              cy={cy}
              r={2}
            >
              <title>{`step ${sample.stepIndex}: ${value.toFixed(1)}%`}</title>
            </circle>
          );
        })}
        {samples.map((sample, index) => {
          const x = LABEL_WIDTH + index * columnWidth;
          return (
            <rect
              key={`cpu-hit-${sample.stepIndex}`}
              className="metrics-hit"
              data-step-index={sample.stepIndex}
              data-lane-hit="CPU"
              x={x}
              y={cpuGeometry.top}
              width={columnWidth}
              height={LANE_HEIGHT}
              onClick={() => onSelect(sample.stepIndex)}
            >
              <title>{`step ${sample.stepIndex}`}</title>
            </rect>
          );
        })}
      </g>

      <g className="metrics-axis" data-row="axis">
        {samples.map((sample, index) => {
          const cx = LABEL_WIDTH + index * columnWidth + columnWidth / 2;
          return (
            <g key={`axis-${sample.stepIndex}`} className="metrics-axis-tick">
              <line
                x1={cx}
                x2={cx}
                y1={axisTop}
                y2={axisTop + 4}
                className="metrics-axis-mark"
              />
              <text
                x={cx}
                y={axisTop + AXIS_HEIGHT - 2}
                textAnchor="middle"
                className="metrics-axis-label"
              >
                {sample.stepIndex}
              </text>
            </g>
          );
        })}
      </g>

      {exceptionStepIndices && exceptionStepIndices.length > 0
        ? exceptionStepIndices.map((stepIndex) => {
            const column = samples.findIndex((sample) => sample.stepIndex === stepIndex);
            if (column < 0) return null;
            const x = LABEL_WIDTH + column * columnWidth + columnWidth / 2;
            return (
              <line
                key={`exc-${stepIndex}`}
                className="metrics-exception-marker"
                data-step-index={stepIndex}
                x1={x}
                x2={x}
                y1={statusTop}
                y2={cpuGeometry.bottom}
                pointerEvents="none"
              />
            );
          })
        : null}

      {highlightX !== null ? (
        <line
          className="metrics-highlight"
          data-selected="true"
          x1={highlightX}
          x2={highlightX}
          y1={statusTop}
          y2={cpuGeometry.bottom}
          stroke="var(--text-primary)"
          strokeWidth={1}
          pointerEvents="none"
        />
      ) : null}
    </svg>
  );
}
