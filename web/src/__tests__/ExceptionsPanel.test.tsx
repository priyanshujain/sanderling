import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import ExceptionsPanel from "../panels/ExceptionsPanel";
import type { Exception } from "../types";

afterEach(() => {
  cleanup();
});

const sampleExceptions: Exception[] = [
  {
    class: "NullPointerException",
    message: "Attempt to invoke virtual method on a null object reference",
    stack_trace: "at com.example.App.onCreate(App.kt:42)\n  at android.app.Activity.performCreate",
    unix_millis: 1_700_000_000_000,
  },
  {
    class: "IllegalStateException",
    message: "Fragment not attached to a context",
    stack_trace: "at com.example.Frag.onResume(Frag.kt:10)",
  },
];

describe("ExceptionsPanel", () => {
  it("renders the empty state when exceptions is undefined", () => {
    render(
      <ExceptionsPanel
        exceptions={undefined}
        onJumpToFirstException={() => {}}
        hasFirstException={false}
      />,
    );
    expect(screen.getByText("no exceptions")).toBeInTheDocument();
  });

  it("renders the empty state when exceptions is an empty array", () => {
    render(
      <ExceptionsPanel
        exceptions={[]}
        onJumpToFirstException={() => {}}
        hasFirstException={false}
      />,
    );
    expect(screen.getByText("no exceptions")).toBeInTheDocument();
  });

  it("renders one row per exception with class and message visible", () => {
    render(
      <ExceptionsPanel
        exceptions={sampleExceptions}
        onJumpToFirstException={() => {}}
        hasFirstException={true}
      />,
    );
    expect(screen.getByText("NullPointerException")).toBeInTheDocument();
    expect(
      screen.getByText("Attempt to invoke virtual method on a null object reference"),
    ).toBeInTheDocument();
    expect(screen.getByText("IllegalStateException")).toBeInTheDocument();
    expect(screen.getByText("Fragment not attached to a context")).toBeInTheDocument();
  });

  it("opens the first row by default and leaves subsequent rows collapsed", () => {
    const { container } = render(
      <ExceptionsPanel
        exceptions={sampleExceptions}
        onJumpToFirstException={() => {}}
        hasFirstException={true}
      />,
    );
    const detailsList = container.querySelectorAll("details");
    expect(detailsList).toHaveLength(2);
    expect(detailsList[0].open).toBe(true);
    expect(detailsList[1].open).toBe(false);
  });

  it("calls onJumpToFirstException when the jump button is clicked", () => {
    const onJump = vi.fn();
    render(
      <ExceptionsPanel
        exceptions={sampleExceptions}
        onJumpToFirstException={onJump}
        hasFirstException={true}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /jump to first exception/i }));
    expect(onJump).toHaveBeenCalledTimes(1);
  });

  it("disables the jump button when hasFirstException is false", () => {
    render(
      <ExceptionsPanel
        exceptions={undefined}
        onJumpToFirstException={() => {}}
        hasFirstException={false}
      />,
    );
    expect(screen.getByRole("button", { name: /jump to first exception/i })).toBeDisabled();
  });

  it("renders the stack trace inside a <pre> when present", () => {
    const { container } = render(
      <ExceptionsPanel
        exceptions={sampleExceptions}
        onJumpToFirstException={() => {}}
        hasFirstException={true}
      />,
    );
    const pres = container.querySelectorAll("pre");
    expect(pres.length).toBeGreaterThanOrEqual(1);
    expect(pres[0].textContent).toContain("com.example.App.onCreate");
  });

  it("renders a timestamp when unix_millis is present", () => {
    render(
      <ExceptionsPanel
        exceptions={sampleExceptions}
        onJumpToFirstException={() => {}}
        hasFirstException={true}
      />,
    );
    expect(screen.getByText(/^\d{2}:\d{2}:\d{2}\.\d{3}$/)).toBeInTheDocument();
  });
});
