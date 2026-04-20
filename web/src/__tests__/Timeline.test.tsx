import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render } from "@testing-library/react";
import Timeline from "../panels/Timeline";
import type { PropertyLane } from "../panels/Timeline";
import type { StepSummary } from "../types";

const sampleSteps: StepSummary[] = [
  { index: 1, timestamp: "t1", action_kind: "Tap", has_violations: false, has_exceptions: false },
  { index: 2, timestamp: "t2", action_kind: "Tap", has_violations: false, has_exceptions: false },
  { index: 3, timestamp: "t3", action_kind: "Tap", has_violations: true, has_exceptions: false },
  { index: 4, timestamp: "t4", action_kind: "Tap", has_violations: false, has_exceptions: false },
];

const sampleLanes: PropertyLane[] = [
  { name: "balance_non_negative", statuses: ["holds", "holds", "violated", "violated"] },
  { name: "eventual_settle", statuses: ["pending", "pending", "pending", "holds"] },
];

describe("Timeline", () => {
  afterEach(() => {
    cleanup();
  });

  it("renders the empty state when steps is empty", () => {
    const { getByText } = render(
      <Timeline steps={[]} lanes={sampleLanes} selectedIndex={1} onSelect={() => {}} />,
    );
    expect(getByText("no timeline data")).toBeInTheDocument();
  });

  it("renders the empty state when lanes is empty", () => {
    const { getByText } = render(
      <Timeline steps={sampleSteps} lanes={[]} selectedIndex={1} onSelect={() => {}} />,
    );
    expect(getByText("no timeline data")).toBeInTheDocument();
  });

  it("renders one lane label per PropertyLane.name", () => {
    const { container } = render(
      <Timeline
        steps={sampleSteps}
        lanes={sampleLanes}
        selectedIndex={1}
        onSelect={() => {}}
      />,
    );
    const labels = Array.from(container.querySelectorAll(".timeline-label")).map(
      (node) => node.textContent,
    );
    expect(labels).toEqual(["balance_non_negative", "eventual_settle"]);
  });

  it("renders one cell per step within each lane", () => {
    const { container } = render(
      <Timeline
        steps={sampleSteps}
        lanes={sampleLanes}
        selectedIndex={1}
        onSelect={() => {}}
      />,
    );
    const lanes = container.querySelectorAll(".timeline-lane");
    expect(lanes).toHaveLength(sampleLanes.length);
    lanes.forEach((lane) => {
      expect(lane.querySelectorAll(".timeline-cell")).toHaveLength(sampleSteps.length);
    });
  });

  it("sets data-status on each cell to match the lane status at that step", () => {
    const { container } = render(
      <Timeline
        steps={sampleSteps}
        lanes={sampleLanes}
        selectedIndex={1}
        onSelect={() => {}}
      />,
    );
    const lanes = container.querySelectorAll(".timeline-lane");
    sampleLanes.forEach((lane, laneIdx) => {
      const cells = lanes[laneIdx].querySelectorAll(".timeline-cell");
      lane.statuses.forEach((status, stepIdx) => {
        expect(cells[stepIdx].getAttribute("data-status")).toBe(status);
      });
    });
  });

  it("calls onSelect with the right step index when a cell is clicked", () => {
    const onSelect = vi.fn();
    const { container } = render(
      <Timeline
        steps={sampleSteps}
        lanes={sampleLanes}
        selectedIndex={1}
        onSelect={onSelect}
      />,
    );
    const cell = container.querySelector('.timeline-cell[data-step="3"]');
    expect(cell).not.toBeNull();
    fireEvent.click(cell as Element);
    expect(onSelect).toHaveBeenCalledWith(3);
  });

  it("renders the selected-step highlight with data-selected=true", () => {
    const { container } = render(
      <Timeline
        steps={sampleSteps}
        lanes={sampleLanes}
        selectedIndex={2}
        onSelect={() => {}}
      />,
    );
    const highlight = container.querySelector('line[data-selected="true"]');
    expect(highlight).not.toBeNull();
  });

  it("renders an action marker dot per step", () => {
    const { container } = render(
      <Timeline
        steps={sampleSteps}
        lanes={sampleLanes}
        selectedIndex={1}
        onSelect={() => {}}
      />,
    );
    expect(container.querySelectorAll(".timeline-action")).toHaveLength(sampleSteps.length);
  });

  it("calls onSelect when an action marker is clicked", () => {
    const onSelect = vi.fn();
    const { container } = render(
      <Timeline
        steps={sampleSteps}
        lanes={sampleLanes}
        selectedIndex={1}
        onSelect={onSelect}
      />,
    );
    const marker = container.querySelector('.timeline-action[data-step="4"]');
    expect(marker).not.toBeNull();
    fireEvent.click(marker as Element);
    expect(onSelect).toHaveBeenCalledWith(4);
  });
});
