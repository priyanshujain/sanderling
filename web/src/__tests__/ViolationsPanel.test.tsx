import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, within } from "@testing-library/react";
import ViolationsPanel from "../panels/ViolationsPanel";
import type { ResidualNode } from "../types";

const propertyNames = ["alpha", "bravo", "charlie"];

const residuals: Record<string, ResidualNode> = {
  alpha: { op: "false" },
  bravo: { op: "true" },
  charlie: { op: "predicate", name: "p" },
};

describe("ViolationsPanel", () => {
  it("renders one row per property name", () => {
    const { container } = render(
      <ViolationsPanel
        propertyNames={propertyNames}
        violations={[]}
        residuals={residuals}
        onJumpToFirstViolation={() => {}}
        hasFirstViolation={false}
      />,
    );
    const rows = container.querySelectorAll(".violations-panel-row");
    expect(rows).toHaveLength(propertyNames.length);
    expect(within(container).getByText("alpha")).toBeInTheDocument();
    expect(within(container).getByText("bravo")).toBeInTheDocument();
    expect(within(container).getByText("charlie")).toBeInTheDocument();
  });

  it("marks violated rows with data-status=violated", () => {
    const { container } = render(
      <ViolationsPanel
        propertyNames={propertyNames}
        violations={["alpha"]}
        residuals={residuals}
        onJumpToFirstViolation={() => {}}
        hasFirstViolation={true}
      />,
    );
    const row = within(container).getByText("alpha").closest("li");
    expect(row).toHaveAttribute("data-status", "violated");
  });

  it("marks rows with residual op=true as holds", () => {
    const { container } = render(
      <ViolationsPanel
        propertyNames={propertyNames}
        violations={[]}
        residuals={residuals}
        onJumpToFirstViolation={() => {}}
        hasFirstViolation={false}
      />,
    );
    const row = within(container).getByText("bravo").closest("li");
    expect(row).toHaveAttribute("data-status", "holds");
  });

  it("marks all other rows as pending", () => {
    const { container } = render(
      <ViolationsPanel
        propertyNames={propertyNames}
        violations={[]}
        residuals={residuals}
        onJumpToFirstViolation={() => {}}
        hasFirstViolation={false}
      />,
    );
    const row = within(container).getByText("charlie").closest("li");
    expect(row).toHaveAttribute("data-status", "pending");
  });

  it("calls the callback when the jump button is clicked", () => {
    const onJump = vi.fn();
    const { container } = render(
      <ViolationsPanel
        propertyNames={propertyNames}
        violations={["alpha"]}
        residuals={residuals}
        onJumpToFirstViolation={onJump}
        hasFirstViolation={true}
      />,
    );
    fireEvent.click(within(container).getByRole("button", { name: /jump to first violation/i }));
    expect(onJump).toHaveBeenCalledTimes(1);
  });

  it("disables the jump button when hasFirstViolation is false", () => {
    const { container } = render(
      <ViolationsPanel
        propertyNames={propertyNames}
        violations={[]}
        residuals={residuals}
        onJumpToFirstViolation={() => {}}
        hasFirstViolation={false}
      />,
    );
    const button = within(container).getByRole("button", { name: /jump to first violation/i });
    expect(button).toBeDisabled();
  });

  it("sorts rows as violated > pending > holds", () => {
    const { container } = render(
      <ViolationsPanel
        propertyNames={propertyNames}
        violations={["charlie"]}
        residuals={residuals}
        onJumpToFirstViolation={() => {}}
        hasFirstViolation={true}
      />,
    );
    const rows = container.querySelectorAll(".violations-panel-row");
    expect(rows).toHaveLength(3);
    expect(rows[0]).toHaveAttribute("data-status", "violated");
    expect(rows[0].textContent).toContain("charlie");
    expect(rows[1]).toHaveAttribute("data-status", "pending");
    expect(rows[1].textContent).toContain("alpha");
    expect(rows[2]).toHaveAttribute("data-status", "holds");
    expect(rows[2].textContent).toContain("bravo");
  });
});
