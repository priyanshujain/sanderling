import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import ResidualNode from "../components/ResidualNode";
import type { ResidualNode as ResidualNodeType } from "../types";

describe("ResidualNode", () => {
  it("renders the true leaf", () => {
    const { container } = render(<ResidualNode node={{ op: "true" }} />);
    expect(container.textContent).toBe("true");
  });

  it("renders the false leaf", () => {
    const { container } = render(<ResidualNode node={{ op: "false" }} />);
    expect(container.textContent).toBe("false");
  });

  it("renders unary operators always, now, next, not with their arg", () => {
    const ops: Array<"always" | "now" | "next" | "not"> = ["always", "now", "next", "not"];
    for (const op of ops) {
      const node: ResidualNodeType = { op, arg: { op: "predicate", name: "p" } };
      const { container, unmount } = render(<ResidualNode node={node} />);
      expect(container.textContent).toContain(op);
      expect(container.textContent).toContain("(");
      expect(container.textContent).toContain("pred");
      expect(container.textContent).toContain("p");
      unmount();
    }
  });

  it("renders implies with both sides and a => label", () => {
    const { container } = render(
      <ResidualNode
        node={{
          op: "implies",
          left: { op: "predicate", name: "leftP" },
          right: { op: "predicate", name: "rightP" },
        }}
      />,
    );
    expect(container.textContent).toContain("=>");
    expect(container.textContent).toContain("leftP");
    expect(container.textContent).toContain("rightP");
  });

  it("renders and with both sides", () => {
    const { container } = render(
      <ResidualNode
        node={{
          op: "and",
          left: { op: "predicate", name: "leftP" },
          right: { op: "predicate", name: "rightP" },
        }}
      />,
    );
    const labels = container.querySelectorAll(".op-label");
    expect(Array.from(labels).map((n) => n.textContent)).toContain("and");
    expect(container.textContent).toContain("leftP");
    expect(container.textContent).toContain("rightP");
  });

  it("renders or with both sides", () => {
    const { container } = render(
      <ResidualNode
        node={{
          op: "or",
          left: { op: "predicate", name: "leftP" },
          right: { op: "predicate", name: "rightP" },
        }}
      />,
    );
    const labels = container.querySelectorAll(".op-label");
    expect(Array.from(labels).map((n) => n.textContent)).toContain("or");
  });

  it("renders eventually with within bound", () => {
    const { container } = render(
      <ResidualNode
        node={{
          op: "eventually",
          arg: { op: "predicate", name: "p" },
          within: { amount: 3, unit: "steps" },
        }}
      />,
    );
    const labels = container.querySelectorAll(".op-label");
    expect(Array.from(labels).map((n) => n.textContent)).toContain("eventually");
    expect(container.textContent).toContain("within 3 steps");
  });

  it("renders predicate with its name", () => {
    const { container } = render(<ResidualNode node={{ op: "predicate", name: "balance>=0" }} />);
    expect(container.textContent).toContain("pred");
    expect(container.textContent).toContain("balance>=0");
  });

  it("renders predicate without a name", () => {
    const { container } = render(<ResidualNode node={{ op: "predicate" }} />);
    expect(container.textContent).toContain("pred");
    expect(container.textContent).not.toContain("(");
  });

  it("renders error in a violation-styled chip with the message", () => {
    const { container, getByRole } = render(
      <ResidualNode node={{ op: "error", message: "boom" }} />,
    );
    const chip = getByRole("status");
    expect(chip).toHaveClass("residual-error");
    expect(container.textContent).toContain("boom");
    expect(container.textContent).toContain("error");
  });
});
