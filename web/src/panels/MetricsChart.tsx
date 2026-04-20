import { useState } from "react";
import type { Metrics } from "../types";
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
  runStartMillis?: number;
}

const MB = 1024 * 1024;

function formatHeap(bytes: number): string {
  if (bytes <= 0) return "0B";
  if (bytes < MB) return `${Math.round(bytes / 1024)}K`;
  if (bytes < 1024 * MB) return `${Math.round(bytes / MB)}M`;
  return `${(bytes / (1024 * MB)).toFixed(1)}G`;
}

function formatTime(millis: number): string {
  const safe = Math.max(0, Math.floor(millis));
  const seconds = Math.floor(safe / 1000);
  const mm = String(Math.floor(seconds / 60)).padStart(2, "0");
  const ss = String(seconds % 60).padStart(2, "0");
  return `${mm}:${ss}`;
}

function fractionFor(index: number, count: number): number {
  if (count <= 1) return 0.5;
  return index / (count - 1);
}

function buildPath(
  samples: MetricsSample[],
  getValue: (sample: MetricsSample) => number | undefined,
  ceiling: number,
): string {
  const segments: string[] = [];
  let pendingCommand = "M";
  samples.forEach((sample, index) => {
    const value = getValue(sample);
    if (value === undefined) {
      pendingCommand = "M";
      return;
    }
    const x = fractionFor(index, samples.length);
    const ratio = ceiling === 0 ? 0 : Math.min(value / ceiling, 1);
    const y = 1 - ratio;
    segments.push(`${pendingCommand}${x.toFixed(4)},${y.toFixed(4)}`);
    pendingCommand = "L";
  });
  return segments.join(" ");
}

interface LaneDot {
  key: number | string;
  x: number;
  y: number;
}

interface LaneHover {
  fraction: number;
  y: number;
  label: string;
}

interface LaneProps {
  label: string;
  path: string;
  dots: LaneDot[];
  topTickLabel: string;
  bottomTickLabel: string;
  selectedFraction: number | null;
  hover: LaneHover | null;
}

function Lane({
  label,
  path,
  dots,
  topTickLabel,
  bottomTickLabel,
  selectedFraction,
  hover,
}: LaneProps) {
  return (
    <div className="metrics-lane">
      <div className="metrics-lane-label">{label}</div>
      <div className="metrics-lane-plot">
        {selectedFraction !== null ? (
          <div
            className="metrics-lane-highlight"
            style={{ left: `${selectedFraction * 100}%` }}
          />
        ) : null}
        <div className="metrics-lane-canvas">
          <svg
            className="metrics-lane-svg"
            viewBox="0 0 1 1"
            preserveAspectRatio="none"
            aria-hidden="true"
          >
            {path ? <path d={path} className="metrics-lane-path" /> : null}
          </svg>
          {dots.map((dot) => (
            <div
              key={dot.key}
              className="metrics-lane-dot"
              style={{ left: `${dot.x * 100}%`, top: `${dot.y * 100}%` }}
              data-active={hover !== null && Math.abs(hover.fraction - dot.x) < 1e-6 ? "true" : undefined}
            />
          ))}
          {hover ? (
            <div
              className="metrics-lane-tooltip"
              data-placement={hover.y < 0.35 ? "below" : "above"}
              style={{ left: `${hover.fraction * 100}%`, top: `${hover.y * 100}%` }}
            >
              {hover.label}
            </div>
          ) : null}
        </div>
      </div>
      <div className="metrics-lane-ticks">
        <span>{topTickLabel}</span>
        <span>{bottomTickLabel}</span>
      </div>
    </div>
  );
}

function buildDots(
  samples: MetricsSample[],
  getValue: (sample: MetricsSample) => number | undefined,
  ceiling: number,
): LaneDot[] {
  const dots: LaneDot[] = [];
  samples.forEach((sample, index) => {
    const value = getValue(sample);
    if (value === undefined) return;
    const ratio = ceiling === 0 ? 0 : Math.min(value / ceiling, 1);
    dots.push({
      key: sample.stepIndex,
      x: fractionFor(index, samples.length),
      y: 1 - ratio,
    });
  });
  return dots;
}

export default function MetricsChart({
  samples,
  selectedIndex,
  onSelect,
  runStartMillis,
}: MetricsChartProps) {
  if (samples.length === 0 || !samples.some((sample) => sample.metrics !== undefined)) {
    return <div className="status-block">no metrics</div>;
  }

  const heapTop = samples.reduce(
    (max, sample) => Math.max(max, sample.metrics?.heap_bytes ?? 0),
    0,
  );
  const cpuObservedMax = samples.reduce(
    (max, sample) => Math.max(max, sample.metrics?.cpu_percent ?? 0),
    0,
  );
  const cpuTop = Math.max(100, cpuObservedMax);

  const heapPath = buildPath(samples, (sample) => sample.metrics?.heap_bytes, heapTop);
  const cpuPath = buildPath(samples, (sample) => sample.metrics?.cpu_percent, cpuTop);
  const heapDots = buildDots(samples, (sample) => sample.metrics?.heap_bytes, heapTop);
  const cpuDots = buildDots(samples, (sample) => sample.metrics?.cpu_percent, cpuTop);

  const baseMillis = runStartMillis ?? new Date(samples[0].timestamp).getTime();

  const axisCount = Math.min(5, samples.length);
  const axisTicks = Array.from({ length: axisCount }, (_, i) => {
    const idx =
      axisCount === 1
        ? 0
        : Math.round((i / (axisCount - 1)) * (samples.length - 1));
    const sample = samples[idx];
    const sampleMillis = new Date(sample.timestamp).getTime();
    const elapsed =
      Number.isFinite(sampleMillis) && Number.isFinite(baseMillis)
        ? sampleMillis - baseMillis
        : NaN;
    return {
      key: sample.stepIndex,
      fraction: fractionFor(idx, samples.length),
      label: Number.isFinite(elapsed) ? formatTime(elapsed) : String(sample.stepIndex),
    };
  });

  const selectedColumn = samples.findIndex((sample) => sample.stepIndex === selectedIndex);
  const selectedFraction =
    selectedColumn >= 0 ? fractionFor(selectedColumn, samples.length) : null;

  const [hoveredColumn, setHoveredColumn] = useState<number | null>(null);
  const hoveredSample =
    hoveredColumn !== null && hoveredColumn >= 0 && hoveredColumn < samples.length
      ? samples[hoveredColumn]
      : null;
  const hoverFraction =
    hoveredColumn !== null ? fractionFor(hoveredColumn, samples.length) : null;

  const heapHover: LaneHover | null =
    hoveredSample && hoverFraction !== null && hoveredSample.metrics?.heap_bytes !== undefined
      ? {
          fraction: hoverFraction,
          y: 1 - (heapTop === 0 ? 0 : Math.min(hoveredSample.metrics.heap_bytes / heapTop, 1)),
          label: formatHeap(hoveredSample.metrics.heap_bytes),
        }
      : null;
  const cpuHover: LaneHover | null =
    hoveredSample && hoverFraction !== null && hoveredSample.metrics?.cpu_percent !== undefined
      ? {
          fraction: hoverFraction,
          y: 1 - (cpuTop === 0 ? 0 : Math.min(hoveredSample.metrics.cpu_percent / cpuTop, 1)),
          label: `${hoveredSample.metrics.cpu_percent.toFixed(1)}%`,
        }
      : null;

  return (
    <div className="metrics-chart">
      <div className="metrics-lanes">
        <Lane
          label="HEAP"
          path={heapPath}
          dots={heapDots}
          topTickLabel={formatHeap(heapTop)}
          bottomTickLabel="0B"
          selectedFraction={selectedFraction}
          hover={heapHover}
        />
        <Lane
          label="CPU"
          path={cpuPath}
          dots={cpuDots}
          topTickLabel={`${Math.round(cpuTop)}%`}
          bottomTickLabel="0%"
          selectedFraction={selectedFraction}
          hover={cpuHover}
        />
        <div className="metrics-hits" role="presentation">
          {samples.map((sample, index) => {
            const left = (() => {
              if (samples.length === 1) return 0;
              const half = 0.5 / (samples.length - 1);
              return Math.max(0, fractionFor(index, samples.length) - half);
            })();
            const width = samples.length === 1 ? 1 : 1 / (samples.length - 1);
            return (
              <button
                key={sample.stepIndex}
                type="button"
                className="metrics-hit"
                style={{ left: `${left * 100}%`, width: `${width * 100}%` }}
                onClick={() => onSelect(sample.stepIndex)}
                onPointerEnter={() => setHoveredColumn(index)}
                onPointerLeave={() =>
                  setHoveredColumn((current) => (current === index ? null : current))
                }
                aria-label={`select step ${sample.stepIndex}`}
              />
            );
          })}
        </div>
      </div>
      <div className="metrics-axis">
        {axisTicks.map((tick) => (
          <span
            key={tick.key}
            className="metrics-axis-label"
            style={{ left: `${tick.fraction * 100}%` }}
          >
            {tick.label}
          </span>
        ))}
      </div>
    </div>
  );
}
