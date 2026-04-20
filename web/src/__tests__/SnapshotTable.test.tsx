import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import SnapshotTable from "../panels/SnapshotTable";

describe("SnapshotTable", () => {
  afterEach(() => {
    cleanup();
  });

  it("renders the empty state when snapshots are undefined", () => {
    render(<SnapshotTable />);
    expect(screen.getByText("no snapshots")).toBeInTheDocument();
  });

  it("renders the empty state when snapshots are an empty object", () => {
    render(<SnapshotTable snapshots={{}} />);
    expect(screen.getByText("no snapshots")).toBeInTheDocument();
  });

  it("renders one row per leaf, sorted alphabetically", () => {
    render(
      <SnapshotTable
        snapshots={{
          zeta: 1,
          alpha: 2,
          mid: 3,
        }}
      />,
    );
    const paths = screen.getAllByRole("term").map((node) => node.textContent);
    expect(paths).toEqual(["alpha", "mid", "zeta"]);
  });

  it("expands nested objects into dotted paths", () => {
    render(
      <SnapshotTable
        snapshots={{
          user: { name: "alice", verified: true },
          ledger: { balance: 1500 },
        }}
      />,
    );
    const paths = screen.getAllByRole("term").map((node) => node.textContent);
    expect(paths).toEqual(["ledger.balance", "user.name", "user.verified"]);
    expect(screen.getByText('"alice"')).toBeInTheDocument();
    expect(screen.getByText("true")).toBeInTheDocument();
    expect(screen.getByText("1500")).toBeInTheDocument();
  });

  it("marks changed values with data-changed and a previous-value title", () => {
    const { container } = render(
      <SnapshotTable
        snapshots={{ ledger: { balance: 1500 } }}
        previousSnapshots={{ ledger: { balance: 1000 } }}
      />,
    );
    const changedRow = container.querySelector('[data-changed="true"]');
    expect(changedRow).not.toBeNull();
    expect(changedRow?.getAttribute("title")).toBe("was: 1000");
  });

  it("does not mark unchanged values", () => {
    const { container } = render(
      <SnapshotTable
        snapshots={{ ledger: { balance: 1500 } }}
        previousSnapshots={{ ledger: { balance: 1500 } }}
      />,
    );
    expect(container.querySelector('[data-changed="true"]')).toBeNull();
  });

  it("does not mark anything when previousSnapshots is omitted", () => {
    const { container } = render(
      <SnapshotTable snapshots={{ ledger: { balance: 1500 } }} />,
    );
    expect(container.querySelector('[data-changed="true"]')).toBeNull();
  });

  it("renders short arrays inline and expands longer arrays into indexed rows", () => {
    render(
      <SnapshotTable
        snapshots={{
          tags: ["a", "b"],
          history: [1, 2, 3, 4],
        }}
      />,
    );
    const paths = screen.getAllByRole("term").map((node) => node.textContent);
    expect(paths).toEqual([
      "history[0]",
      "history[1]",
      "history[2]",
      "history[3]",
      "tags",
    ]);
    expect(screen.getByText('["a","b"]')).toBeInTheDocument();
  });
});
