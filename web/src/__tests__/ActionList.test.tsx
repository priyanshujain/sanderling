import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import ActionList, { formatActionRow, formatElapsed } from "../panels/ActionList";
import type { Step, StepSummary } from "../types";

const runStart = new Date("2026-04-20T10:00:00.000Z").getTime();

const sampleSteps: StepSummary[] = [
  {
    index: 1,
    timestamp: "2026-04-20T10:00:00.000Z",
    action_kind: "Tap",
    action_label: "id:buttonLogin",
    has_violations: false,
    has_exceptions: false,
  },
  {
    index: 2,
    timestamp: "2026-04-20T10:00:01.000Z",
    action_kind: "Swipe",
    action_label: "up",
    has_violations: false,
    has_exceptions: true,
  },
  {
    index: 3,
    timestamp: "2026-04-20T10:00:02.000Z",
    action_kind: "InputText",
    action_label: '"hello"',
    has_violations: true,
    has_exceptions: false,
  },
  {
    index: 4,
    timestamp: "2026-04-20T10:00:03.000Z",
    has_violations: false,
    has_exceptions: false,
  },
];

describe("formatActionRow", () => {
  it("formats Tap with id selector as a tag target", () => {
    const row = formatActionRow({
      index: 1,
      timestamp: "2026-04-20T10:00:00Z",
      action_kind: "Tap",
      action_label: "id:buttonLogin",
      has_violations: false,
      has_exceptions: false,
    });
    expect(row).toEqual({ verb: "Click", target: "buttonLogin", targetIsTag: true });
  });

  it("formats Tap with descPrefix selector with ellipsis", () => {
    const row = formatActionRow({
      index: 1,
      timestamp: "2026-04-20T10:00:00Z",
      action_kind: "Tap",
      action_label: "descPrefix:customer_abc",
      has_violations: false,
      has_exceptions: false,
    });
    expect(row).toEqual({ verb: "Click", target: "customer_abc...", targetIsTag: true });
  });

  it("formats Tap with coordinate label as plain text", () => {
    const row = formatActionRow({
      index: 1,
      timestamp: "2026-04-20T10:00:00Z",
      action_kind: "Tap",
      action_label: "(100,200)",
      has_violations: false,
      has_exceptions: false,
    });
    expect(row).toEqual({ verb: "Click", target: "(100,200)", targetIsTag: false });
  });

  it("formats InputText with quoted text", () => {
    const row = formatActionRow({
      index: 1,
      timestamp: "2026-04-20T10:00:00Z",
      action_kind: "InputText",
      action_label: '"hello"',
      has_violations: false,
      has_exceptions: false,
    });
    expect(row).toEqual({ verb: "Type", target: '"hello"', targetIsTag: false });
  });

  it("formats Swipe direction as tag target", () => {
    const row = formatActionRow({
      index: 1,
      timestamp: "2026-04-20T10:00:00Z",
      action_kind: "Swipe",
      action_label: "up",
      has_violations: false,
      has_exceptions: false,
    });
    expect(row).toEqual({ verb: "Swipe", target: "up", targetIsTag: true });
  });

  it("formats PressKey as Press verb", () => {
    const row = formatActionRow({
      index: 1,
      timestamp: "2026-04-20T10:00:00Z",
      action_kind: "PressKey",
      action_label: "back",
      has_violations: false,
      has_exceptions: false,
    });
    expect(row).toEqual({ verb: "Press", target: "back", targetIsTag: true });
  });

  it("formats Wait as Wait verb", () => {
    const row = formatActionRow({
      index: 1,
      timestamp: "2026-04-20T10:00:00Z",
      action_kind: "Wait",
      action_label: "500ms",
      has_violations: false,
      has_exceptions: false,
    });
    expect(row).toEqual({ verb: "Wait", target: "500ms", targetIsTag: true });
  });

  it("formats missing action_kind as Observe", () => {
    const row = formatActionRow({
      index: 1,
      timestamp: "2026-04-20T10:00:00Z",
      screen: "Home",
      has_violations: false,
      has_exceptions: false,
    });
    expect(row).toEqual({ verb: "Observe", target: "@ Home", targetIsTag: false });
  });
});

describe("formatElapsed", () => {
  it("zero-pads minutes, seconds, and milliseconds", () => {
    expect(formatElapsed(0)).toBe("00:00.000");
    expect(formatElapsed(1234)).toBe("00:01.234");
    expect(formatElapsed(65_042)).toBe("01:05.042");
  });

  it("clamps negative durations to zero", () => {
    expect(formatElapsed(-500)).toBe("00:00.000");
  });
});

describe("ActionList", () => {
  afterEach(() => {
    cleanup();
  });

  it("renders one row per step", () => {
    render(
      <ActionList
        steps={sampleSteps}
        selectedIndex={1}
        onSelect={() => {}}
        runStartMillis={runStart}
      />,
    );
    const rows = screen.getAllByRole("button");
    expect(rows).toHaveLength(sampleSteps.length);
  });

  it("renders Click <buttonLogin/> for a Tap with id selector", () => {
    render(
      <ActionList
        steps={sampleSteps}
        selectedIndex={1}
        onSelect={() => {}}
        runStartMillis={runStart}
      />,
    );
    const row = screen.getByRole("button", { name: /step 1 click <buttonlogin\/>/i });
    expect(row).toBeInTheDocument();
    expect(row.textContent).toContain("Click");
    expect(row.textContent).toContain("<buttonLogin/>");
  });

  it('renders Type "hello" for an InputText row', () => {
    render(
      <ActionList
        steps={sampleSteps}
        selectedIndex={1}
        onSelect={() => {}}
        runStartMillis={runStart}
      />,
    );
    const row = screen.getByRole("button", { name: /step 3 type "hello"/i });
    expect(row).toBeInTheDocument();
    expect(row.textContent).toContain("Type");
    expect(row.textContent).toContain('"hello"');
  });

  it("renders Swipe <up/> with tag markup", () => {
    render(
      <ActionList
        steps={sampleSteps}
        selectedIndex={1}
        onSelect={() => {}}
        runStartMillis={runStart}
      />,
    );
    const row = screen.getByRole("button", { name: /step 2 swipe <up\/>/i });
    expect(row.textContent).toContain("<up/>");
  });

  it("renders Observe for a step with no action", () => {
    render(
      <ActionList
        steps={sampleSteps}
        selectedIndex={1}
        onSelect={() => {}}
        runStartMillis={runStart}
      />,
    );
    expect(
      screen.getByRole("button", { name: /step 4 observe/i }),
    ).toBeInTheDocument();
  });

  it("calls onSelect with the step index when a row is clicked", () => {
    const onSelect = vi.fn();
    render(
      <ActionList
        steps={sampleSteps}
        selectedIndex={1}
        onSelect={onSelect}
        runStartMillis={runStart}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /step 2/i }));
    expect(onSelect).toHaveBeenCalledWith(2);
  });

  it("marks the active row with data-active=true", () => {
    render(
      <ActionList
        steps={sampleSteps}
        selectedIndex={3}
        onSelect={() => {}}
        runStartMillis={runStart}
      />,
    );
    const activeRow = screen.getByRole("button", { name: /step 3/i });
    expect(activeRow).toHaveAttribute("data-active", "true");
    const inactiveRow = screen.getByRole("button", { name: /step 1/i });
    expect(inactiveRow).toHaveAttribute("data-active", "false");
  });

  it("shows a violation marker on rows with has_violations", () => {
    render(
      <ActionList
        steps={sampleSteps}
        selectedIndex={1}
        onSelect={() => {}}
        runStartMillis={runStart}
      />,
    );
    const violationMarkers = screen.getAllByLabelText("violations");
    expect(violationMarkers).toHaveLength(1);
    const violationRow = screen.getByRole("button", { name: /step 3/i });
    expect(violationRow).toHaveAttribute("data-violations", "true");
  });

  it("invokes onSelect when Enter is pressed on a focused row", () => {
    const onSelect = vi.fn();
    render(
      <ActionList
        steps={sampleSteps}
        selectedIndex={1}
        onSelect={onSelect}
        runStartMillis={runStart}
      />,
    );
    const row = screen.getByRole("button", { name: /step 2/i });
    row.focus();
    fireEvent.keyDown(row, { key: "Enter" });
    expect(onSelect).toHaveBeenCalledWith(2);
  });

  it("invokes onSelect when Space is pressed on a focused row", () => {
    const onSelect = vi.fn();
    render(
      <ActionList
        steps={sampleSteps}
        selectedIndex={1}
        onSelect={onSelect}
        runStartMillis={runStart}
      />,
    );
    const row = screen.getByRole("button", { name: /step 4/i });
    row.focus();
    fireEvent.keyDown(row, { key: " " });
    expect(onSelect).toHaveBeenCalledWith(4);
  });

  it("renders elapsed time zero-padded relative to runStartMillis", () => {
    const steps: StepSummary[] = [
      {
        index: 1,
        timestamp: new Date(runStart + 1234).toISOString(),
        action_kind: "Tap",
        action_label: "id:submit",
        has_violations: false,
        has_exceptions: false,
      },
    ];
    const { container } = render(
      <ActionList
        steps={steps}
        selectedIndex={1}
        onSelect={() => {}}
        runStartMillis={runStart}
      />,
    );
    const elapsed = container.querySelector(".action-list-elapsed");
    expect(elapsed?.textContent).toBe("00:01.234");
  });

  it("renders Position and Content sub-rows only on the active row when selectedStep is provided", () => {
    const selectedStep: Step = {
      step: 3,
      timestamp: "2026-04-20T10:00:02Z",
      action: {
        kind: "InputText",
        text: "hello",
        x: 512,
        y: 108.9,
      },
    };
    const { container } = render(
      <ActionList
        steps={sampleSteps}
        selectedIndex={3}
        onSelect={() => {}}
        runStartMillis={runStart}
        selectedStep={selectedStep}
      />,
    );
    const detailBlocks = container.querySelectorAll(".action-list-details");
    expect(detailBlocks).toHaveLength(1);
    const activeRow = screen.getByRole("button", { name: /step 3/i });
    expect(activeRow.textContent).toContain("Position");
    expect(activeRow.textContent).toContain("512.0, 108.9");
    expect(activeRow.textContent).toContain("Content");
    expect(activeRow.textContent).toContain("hello");
    const inactiveRow = screen.getByRole("button", { name: /step 1/i });
    expect(inactiveRow.querySelector(".action-list-details")).toBeNull();
  });

  it("renders empty-string content placeholder when selected action has no text", () => {
    const selectedStep: Step = {
      step: 1,
      timestamp: "2026-04-20T10:00:00Z",
      action: {
        kind: "Tap",
        x: 10,
        y: 20,
      },
    };
    const { container } = render(
      <ActionList
        steps={sampleSteps}
        selectedIndex={1}
        onSelect={() => {}}
        runStartMillis={runStart}
        selectedStep={selectedStep}
      />,
    );
    const contentValue = container.querySelectorAll(".action-list-detail-value")[1];
    expect(contentValue?.textContent).toBe('""');
  });

  it("does not render sub-rows when selectedStep is omitted", () => {
    const { container } = render(
      <ActionList
        steps={sampleSteps}
        selectedIndex={3}
        onSelect={() => {}}
        runStartMillis={runStart}
      />,
    );
    expect(container.querySelector(".action-list-details")).toBeNull();
  });
});
