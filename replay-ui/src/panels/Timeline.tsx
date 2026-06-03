import type { KeyboardEvent } from "react";
import type { StepSummary } from "../types";
import "./Timeline.css";

export type LaneStatus = "violated" | "pending" | "holds";

export interface PropertyLane {
  name: string;
  statuses: LaneStatus[];
}

export interface TimelineProps {
  steps: StepSummary[];
  lanes: PropertyLane[];
  selectedIndex: number;
  onSelect: (index: number) => void;
}

const LANE_HEIGHT = 22;
const LABEL_WIDTH = 140;
const CELL_GAP = 2;
const CHART_WIDTH = 1000;

const STATUS_FILL: Record<LaneStatus, string> = {
  violated: "var(--accent-violation)",
  pending: "color-mix(in srgb, var(--text-primary) 30%, transparent)",
  holds: "color-mix(in srgb, var(--text-primary) 10%, transparent)",
};

export default function Timeline({ steps, lanes, selectedIndex, onSelect }: TimelineProps) {
  if (steps.length === 0 || lanes.length === 0) {
    return <div className="status-block">no timeline data</div>;
  }

  const cellAreaWidth = CHART_WIDTH - LABEL_WIDTH;
  const cellWidth = cellAreaWidth / steps.length;
  const totalRows = lanes.length + 1;
  const height = totalRows * LANE_HEIGHT;

  const handleKeyDown = (event: KeyboardEvent<SVGRectElement>, stepIndex: number) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      onSelect(stepIndex);
    }
  };

  const selectedColumn = steps.findIndex((step) => step.index === selectedIndex);
  const highlightX =
    selectedColumn >= 0 ? LABEL_WIDTH + selectedColumn * cellWidth + cellWidth / 2 : null;

  return (
    <svg
      className="timeline"
      viewBox={`0 0 ${CHART_WIDTH} ${height}`}
      preserveAspectRatio="none"
      role="img"
      aria-label="property timeline"
    >
      {lanes.map((lane, laneIndex) => {
        const rowY = laneIndex * LANE_HEIGHT;
        return (
          <g key={lane.name} className="timeline-lane" data-lane={lane.name}>
            <text
              className="timeline-label"
              x={LABEL_WIDTH - 8}
              y={rowY + LANE_HEIGHT / 2}
              dominantBaseline="middle"
              textAnchor="end"
            >
              {lane.name}
            </text>
            {steps.map((step, stepCol) => {
              const status = lane.statuses[stepCol] ?? "pending";
              const x = LABEL_WIDTH + stepCol * cellWidth + CELL_GAP / 2;
              const w = Math.max(cellWidth - CELL_GAP, 1);
              const y = rowY + 3;
              const h = LANE_HEIGHT - 6;
              return (
                <rect
                  key={step.index}
                  className="timeline-cell"
                  data-status={status}
                  data-step={step.index}
                  x={x}
                  y={y}
                  width={w}
                  height={h}
                  fill={STATUS_FILL[status]}
                  tabIndex={0}
                  onClick={() => onSelect(step.index)}
                  onKeyDown={(event) => handleKeyDown(event, step.index)}
                >
                  <title>{`step ${step.index}: ${status}`}</title>
                </rect>
              );
            })}
          </g>
        );
      })}
      <g className="timeline-actions" data-row="actions">
        {steps.map((step, stepCol) => {
          const cx = LABEL_WIDTH + stepCol * cellWidth + cellWidth / 2;
          const cy = lanes.length * LANE_HEIGHT + LANE_HEIGHT / 2;
          return (
            <circle
              key={step.index}
              className="timeline-action"
              data-step={step.index}
              cx={cx}
              cy={cy}
              r={3}
              fill="var(--text-muted)"
              tabIndex={0}
              onClick={() => onSelect(step.index)}
              onKeyDown={(event) => {
                if (event.key === "Enter" || event.key === " ") {
                  event.preventDefault();
                  onSelect(step.index);
                }
              }}
            >
              <title>{`step ${step.index}`}</title>
            </circle>
          );
        })}
      </g>
      {highlightX !== null ? (
        <line
          className="timeline-highlight"
          data-selected="true"
          x1={highlightX}
          x2={highlightX}
          y1={0}
          y2={height}
          stroke="var(--text-primary)"
          strokeWidth={2}
          pointerEvents="none"
        />
      ) : null}
    </svg>
  );
}
