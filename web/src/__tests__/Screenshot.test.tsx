import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render } from "@testing-library/react";
import Screenshot from "../panels/Screenshot";
import type { Action } from "../types";

afterEach(() => {
  cleanup();
});

describe("Screenshot", () => {
  it("renders the placeholder when src is undefined", () => {
    const { getByTestId } = render(<Screenshot />);
    expect(getByTestId("screenshot-placeholder")).toHaveTextContent("no screenshot");
  });

  it("renders an img with the given src", () => {
    const { getByAltText } = render(
      <Screenshot src="/api/runs/r1/screenshots/step-00001.png" />,
    );
    const img = getByAltText("device screenshot") as HTMLImageElement;
    expect(img).toBeInTheDocument();
    expect(img.getAttribute("src")).toBe("/api/runs/r1/screenshots/step-00001.png");
  });

  it("renders a rect for resolvedBounds", () => {
    const action: Action = {
      kind: "Tap",
      resolvedBounds: { x: 100, y: 200, width: 300, height: 400 },
    };
    const { container } = render(
      <Screenshot
        src="/img.png"
        action={action}
        deviceWidth={1080}
        deviceHeight={1920}
      />,
    );
    const rect = container.querySelector("rect");
    expect(rect).not.toBeNull();
    expect(rect!.getAttribute("x")).toBe("100");
    expect(rect!.getAttribute("y")).toBe("200");
    expect(rect!.getAttribute("width")).toBe("300");
    expect(rect!.getAttribute("height")).toBe("400");
  });

  it("renders a circle for tapPoint", () => {
    const action: Action = {
      kind: "Tap",
      tapPoint: { x: 540, y: 960 },
    };
    const { container } = render(
      <Screenshot
        src="/img.png"
        action={action}
        deviceWidth={1080}
        deviceHeight={1920}
      />,
    );
    const circle = container.querySelector("circle");
    expect(circle).not.toBeNull();
    expect(circle!.getAttribute("cx")).toBe("540");
    expect(circle!.getAttribute("cy")).toBe("960");
  });

  it("renders a line for swipe actions", () => {
    const action: Action = {
      kind: "Swipe",
      from_x: 100,
      from_y: 200,
      to_x: 800,
      to_y: 1500,
    };
    const { container } = render(
      <Screenshot
        src="/img.png"
        action={action}
        deviceWidth={1080}
        deviceHeight={1920}
      />,
    );
    const line = container.querySelector("line");
    expect(line).not.toBeNull();
    expect(line!.getAttribute("x1")).toBe("100");
    expect(line!.getAttribute("y1")).toBe("200");
    expect(line!.getAttribute("x2")).toBe("800");
    expect(line!.getAttribute("y2")).toBe("1500");
    expect(container.querySelector("polygon")).not.toBeNull();
  });

  it("shows the placeholder when the image fails to load", () => {
    const { getByAltText, getByTestId } = render(<Screenshot src="/missing.png" />);
    fireEvent.error(getByAltText("device screenshot"));
    expect(getByTestId("screenshot-placeholder")).toHaveTextContent("no screenshot");
  });
});
