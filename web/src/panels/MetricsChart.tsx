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
const LEFT_GUTTER = 18;
const RIGHT_GUTTER = 44;
const LANE_HEIGHT = 40;
const LANE_GAP = 10;
const AXIS_HEIGHT = 14;
const PLAYHEAD_WIDTH = 22;

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

function formatHeapTop(bytes: number): string {
  if (bytes < MB) return `${Math.round(bytes / 1024)}K`;
  if (bytes < 1024 * MB) return `${Math.round(bytes / MB)}M`;
  return `${(bytes / (1024 * MB)).toFixed(1)}G`;
}

function formatHeapTooltip(bytes: number): string {
  if (bytes === 0) return "0B";
  if (bytes < MB) return `${Math.round(bytes / 1024)}KB`;
  if (bytes < 1024 * MB) return `${Math.round(bytes / MB)}MB`;
  return `${(bytes / (1024 * MB)).toFixed(1)}GB`;
}

function cpuCeiling(maxPercent: number): number {
  if (maxPercent <= 100) return 100;
  return Math.ceil(maxPercent / 50) * 50;
}

function formatClock(millis: number): string {
  const safe = Math.max(0, Math.floor(millis));
  const totalSeconds = Math.floor(safe / 1000);
  const mm = Math.floor(totalSeconds / 60);
  const ss = totalSeconds % 60;
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(mm)}:${pad(ss)}`;
}

function parseMillis(value: string): number {
  const parsed = new Date(value).getTime();
  return Number.isFinite(parsed) ? parsed : 0;
}

function buildTimeTicks(samples: MetricsSample[], count: number): { t: number; label: string }[] {
  const firstMs = parseMillis(samples[0].timestamp);
  const lastMs = parseMillis(samples[samples.length - 1].timestamp);
  const duration = Math.max(0, lastMs - firstMs);
  const ticks: { t: number; label: string }[] = [];
  const segments = Math.max(1, count - 1);
  for (let i = 0; i < count; i += 1) {
    const t = (duration * i) / segments;
    ticks.push({ t, label: formatClock(t) });
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
}: MetricsChartProps) {
  if (samples.length === 0) {
    return <div className="status-block">no metrics</div>;
  }

  const hasMetrics = samples.some((sample) => sample.metrics !== undefined);
  if (!hasMetrics) {
    return <div className="status-block">no metrics</div>;
  }

  const plotLeft = LEFT_GUTTER;
  const plotRight = CHART_WIDTH - RIGHT_GUTTER;
  const plotWidth = plotRight - plotLeft;
  const columnWidth = plotWidth / samples.length;

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

  const heapGeometry: LaneGeometry = {
    top: 0,
    bottom: LANE_HEIGHT,
    plotLeft,
    plotRight,
  };
  const cpuGeometry: LaneGeometry = {
    top: heapGeometry.bottom + LANE_GAP,
    bottom: heapGeometry.bottom + LANE_GAP + LANE_HEIGHT,
    plotLeft,
    plotRight,
  };
  const axisTop = cpuGeometry.bottom + 4;
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

  const selectedColumn = samples.findIndex((sample) => sample.stepIndex === selectedIndex);
  const highlightCenterX =
    selectedColumn >= 0 ? plotLeft + selectedColumn * columnWidth + columnWidth / 2 : null;

  const timeTicks = buildTimeTicks(samples, 5);
  const firstMs = parseMillis(samples[0].timestamp);
  const lastMs = parseMillis(samples[samples.length - 1].timestamp);
  const totalDuration = Math.max(0, lastMs - firstMs);

  return (
    <svg
      className="metrics-chart"
      viewBox={`0 0 ${CHART_WIDTH} ${totalHeight}`}
      role="img"
      aria-label="metrics chart"
    >
      <defs>
        <pattern
          id="metrics-playhead-pattern"
          patternUnits="userSpaceOnUse"
          width="4"
          height="4"
        >
          <circle cx="1" cy="1" r="0.6" className="metrics-playhead-dot" />
        </pattern>
      </defs>

      <g className="metrics-lane" data-lane="HEAP">
        <rect
          x={plotLeft}
          y={heapGeometry.top}
          width={plotWidth}
          height={LANE_HEIGHT}
          className="metrics-lane-bg"
        />
        <text
          className="metrics-lane-label"
          x={LEFT_GUTTER / 2}
          y={heapGeometry.top + LANE_HEIGHT / 2}
          textAnchor="middle"
          transform={`rotate(-90 ${LEFT_GUTTER / 2} ${heapGeometry.top + LANE_HEIGHT / 2})`}
        >
          HEAP
        </text>
        <text
          x={CHART_WIDTH - 4}
          y={heapGeometry.top + 8}
          textAnchor="end"
          className="metrics-tick-label"
        >
          {formatHeapTop(heapTop)}
        </text>
        <text
          x={CHART_WIDTH - 4}
          y={heapGeometry.bottom - 2}
          textAnchor="end"
          className="metrics-tick-label"
        >
          0B
        </text>
        {heapPoints.length > 0 ? (
          <polyline
            className="metrics-line"
            data-series="heap"
            points={heapPoints}
            fill="none"
          />
        ) : null}
        {samples.map((sample) => {
          const value = sample.metrics?.heap_bytes;
          if (value === undefined) return null;
          return (
            <g key={`heap-point-${sample.stepIndex}`} className="metrics-point" data-step-index={sample.stepIndex}>
              <title>{`step ${sample.stepIndex}: ${formatHeapTooltip(value)}`}</title>
            </g>
          );
        })}
        {samples.map((sample, index) => {
          const x = plotLeft + index * columnWidth;
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
          x={plotLeft}
          y={cpuGeometry.top}
          width={plotWidth}
          height={LANE_HEIGHT}
          className="metrics-lane-bg"
        />
        <text
          className="metrics-lane-label"
          x={LEFT_GUTTER / 2}
          y={cpuGeometry.top + LANE_HEIGHT / 2}
          textAnchor="middle"
          transform={`rotate(-90 ${LEFT_GUTTER / 2} ${cpuGeometry.top + LANE_HEIGHT / 2})`}
        >
          CPU
        </text>
        <text
          x={CHART_WIDTH - 4}
          y={cpuGeometry.top + 8}
          textAnchor="end"
          className="metrics-tick-label"
        >
          100%
        </text>
        <text
          x={CHART_WIDTH - 4}
          y={cpuGeometry.bottom - 2}
          textAnchor="end"
          className="metrics-tick-label"
        >
          0%
        </text>
        {cpuPoints.length > 0 ? (
          <polyline
            className="metrics-line"
            data-series="cpu"
            points={cpuPoints}
            fill="none"
          />
        ) : null}
        {samples.map((sample) => {
          const value = sample.metrics?.cpu_percent;
          if (value === undefined) return null;
          return (
            <g key={`cpu-point-${sample.stepIndex}`} className="metrics-point" data-step-index={sample.stepIndex}>
              <title>{`step ${sample.stepIndex}: ${value.toFixed(1)}%`}</title>
            </g>
          );
        })}
        {samples.map((sample, index) => {
          const x = plotLeft + index * columnWidth;
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
        {timeTicks.map((tick, index) => {
          const ratio = totalDuration === 0 ? index / Math.max(1, timeTicks.length - 1) : tick.t / totalDuration;
          const x = plotLeft + ratio * plotWidth;
          const isLast = index === timeTicks.length - 1;
          const isFirst = index === 0;
          const anchor = isLast ? "end" : isFirst ? "start" : "middle";
          return (
            <g key={`axis-${index}`} className="metrics-axis-tick">
              <text
                x={x}
                y={axisTop + AXIS_HEIGHT - 2}
                textAnchor={anchor}
                className="metrics-axis-label"
              >
                {tick.label}
              </text>
            </g>
          );
        })}
      </g>

      {highlightCenterX !== null ? (
        <rect
          className="metrics-highlight"
          data-selected="true"
          x={highlightCenterX - PLAYHEAD_WIDTH / 2}
          y={heapGeometry.top}
          width={PLAYHEAD_WIDTH}
          height={cpuGeometry.bottom - heapGeometry.top}
          fill="url(#metrics-playhead-pattern)"
          pointerEvents="none"
        />
      ) : null}
    </svg>
  );
}
