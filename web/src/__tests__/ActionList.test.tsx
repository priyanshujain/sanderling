import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import ActionList from "../panels/ActionList";
import type { StepSummary } from "../types";

const sampleSteps: StepSummary[] = [
  {
    index: 1,
    timestamp: "2026-04-20T10:00:00Z",
    action_kind: "Tap",
    has_violations: false,
    has_exceptions: false,
  },
  {
    index: 2,
    timestamp: "2026-04-20T10:00:01Z",
    action_kind: "Swipe",
    has_violations: false,
    has_exceptions: true,
  },
  {
    index: 3,
    timestamp: "2026-04-20T10:00:02Z",
    action_kind: "InputText",
    has_violations: true,
    has_exceptions: false,
  },
  {
    index: 4,
    timestamp: "2026-04-20T10:00:03Z",
    has_violations: false,
    has_exceptions: false,
  },
];

describe("ActionList", () => {
  afterEach(() => {
    cleanup();
  });

  it("renders one row per step with index and action kind", () => {
    render(<ActionList steps={sampleSteps} selectedIndex={1} onSelect={() => {}} />);
    const rows = screen.getAllByRole("button");
    expect(rows).toHaveLength(sampleSteps.length);
    expect(screen.getByRole("button", { name: /step 1 tap/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /step 2 swipe/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /step 3 inputtext/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /step 4 observe/i })).toBeInTheDocument();
  });

  it("calls onSelect with the step index when a row is clicked", () => {
    const onSelect = vi.fn();
    render(<ActionList steps={sampleSteps} selectedIndex={1} onSelect={onSelect} />);
    fireEvent.click(screen.getByRole("button", { name: /step 2 swipe/i }));
    expect(onSelect).toHaveBeenCalledWith(2);
  });

  it("marks the active row with data-active=true", () => {
    render(<ActionList steps={sampleSteps} selectedIndex={3} onSelect={() => {}} />);
    const activeRow = screen.getByRole("button", { name: /step 3/i });
    expect(activeRow).toHaveAttribute("data-active", "true");
    const inactiveRow = screen.getByRole("button", { name: /step 1/i });
    expect(inactiveRow).toHaveAttribute("data-active", "false");
  });

  it("shows a violation marker on rows with has_violations", () => {
    render(<ActionList steps={sampleSteps} selectedIndex={1} onSelect={() => {}} />);
    const violationMarkers = screen.getAllByLabelText("violations");
    expect(violationMarkers).toHaveLength(1);
    const violationRow = screen.getByRole("button", { name: /step 3/i });
    expect(violationRow).toHaveAttribute("data-violations", "true");
  });

  it("invokes onSelect when Enter is pressed on a focused row", () => {
    const onSelect = vi.fn();
    render(<ActionList steps={sampleSteps} selectedIndex={1} onSelect={onSelect} />);
    const row = screen.getByRole("button", { name: /step 2 swipe/i });
    row.focus();
    fireEvent.keyDown(row, { key: "Enter" });
    expect(onSelect).toHaveBeenCalledWith(2);
  });

  it("renders the action_label next to the kind when present", () => {
    const steps: StepSummary[] = [
      {
        index: 1,
        timestamp: "2026-04-20T10:00:00Z",
        action_kind: "Tap",
        action_label: "id:save",
        has_violations: false,
        has_exceptions: false,
      },
      {
        index: 2,
        timestamp: "2026-04-20T10:00:01Z",
        action_kind: "InputText",
        action_label: '"hello"',
        has_violations: false,
        has_exceptions: false,
      },
    ];
    render(<ActionList steps={steps} selectedIndex={1} onSelect={() => {}} />);
    expect(screen.getByRole("button", { name: /step 1 tap id:save/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /step 2 inputtext "hello"/i })).toBeInTheDocument();
  });

  it("invokes onSelect when Space is pressed on a focused row", () => {
    const onSelect = vi.fn();
    render(<ActionList steps={sampleSteps} selectedIndex={1} onSelect={onSelect} />);
    const row = screen.getByRole("button", { name: /step 4/i });
    row.focus();
    fireEvent.keyDown(row, { key: " " });
    expect(onSelect).toHaveBeenCalledWith(4);
  });
});
