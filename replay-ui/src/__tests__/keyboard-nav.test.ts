import { describe, it, expect } from "bun:test";
import {
  dispatchKey,
  targetOwnsArrowKeys,
  type KeyboardNavOptions,
} from "../lib/keyboard-nav";

function el(
  over: Partial<{
    tagName: string;
    isContentEditable: boolean;
    role: string | null;
    parent: HTMLElement | null;
  }> = {},
): HTMLElement {
  const role = over.role ?? null;
  return {
    tagName: over.tagName ?? "DIV",
    isContentEditable: over.isContentEditable ?? false,
    getAttribute: (name: string) => (name === "role" ? role : null),
    parentElement: over.parent ?? null,
  } as unknown as HTMLElement;
}

function spyOptions() {
  const calls: string[] = [];
  const make = (name: keyof KeyboardNavOptions) => () => {
    calls.push(name);
  };
  const options: KeyboardNavOptions = {
    onPrev: make("onPrev"),
    onNext: make("onNext"),
    onJumpStart: make("onJumpStart"),
    onJumpEnd: make("onJumpEnd"),
    onJumpPrev10: make("onJumpPrev10"),
    onJumpNext10: make("onJumpNext10"),
    onJumpNextViolation: make("onJumpNextViolation"),
  };
  return { calls, options };
}

describe("targetOwnsArrowKeys", () => {
  it("walks ancestors and matches arrow-owning roles", () => {
    expect(targetOwnsArrowKeys(el({ role: "option" }))).toBe(true);
    const child = el({ role: null, parent: el({ role: "listbox" }) });
    expect(targetOwnsArrowKeys(child)).toBe(true);
    expect(targetOwnsArrowKeys(el({ role: "banner" }))).toBe(false);
    expect(targetOwnsArrowKeys(null)).toBe(false);
  });
});

// Bug class: navigation keys firing while the user types in a form field would
// scrub the timeline out from under them; and arrow keys must yield to a
// listbox/tab that owns them so its own roving focus still works.
describe("dispatchKey ownership", () => {
  it("ignores keys originating in editable targets", () => {
    const { calls, options } = spyOptions();
    expect(dispatchKey({ key: "j", target: el({ tagName: "INPUT" }) }, options)).toBe(false);
    expect(dispatchKey({ key: "g", target: el({ isContentEditable: true }) }, options)).toBe(false);
    expect(calls).toEqual([]);
  });

  it("yields arrow keys to an owning ancestor but still handles letters", () => {
    const { calls, options } = spyOptions();
    const inListbox = el({ parent: el({ role: "listbox" }) });
    expect(dispatchKey({ key: "ArrowRight", target: inListbox }, options)).toBe(false);
    expect(dispatchKey({ key: "j", target: inListbox }, options)).toBe(true);
    expect(calls).toEqual(["onNext"]);
  });

  it("ignores keys combined with a modifier", () => {
    const { calls, options } = spyOptions();
    expect(dispatchKey({ key: "j", metaKey: true }, options)).toBe(false);
    expect(dispatchKey({ key: "j", ctrlKey: true }, options)).toBe(false);
    expect(calls).toEqual([]);
  });
});

describe("dispatchKey routing", () => {
  const cases: [string, boolean, keyof KeyboardNavOptions][] = [
    ["ArrowLeft", false, "onPrev"],
    ["k", false, "onPrev"],
    ["ArrowLeft", true, "onJumpPrev10"],
    ["ArrowRight", false, "onNext"],
    ["j", true, "onJumpNext10"],
    ["g", false, "onJumpStart"],
    ["G", false, "onJumpEnd"],
    [".", false, "onJumpNextViolation"],
  ];
  for (const [key, shiftKey, expected] of cases) {
    it(`${shiftKey ? "shift+" : ""}${key} -> ${expected}`, () => {
      const { calls, options } = spyOptions();
      expect(dispatchKey({ key, shiftKey }, options)).toBe(true);
      expect(calls).toEqual([expected]);
    });
  }

  it("leaves unmapped keys untouched", () => {
    const { calls, options } = spyOptions();
    expect(dispatchKey({ key: "x" }, options)).toBe(false);
    expect(calls).toEqual([]);
  });
});
