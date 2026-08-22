import { describe, it, expect } from "bun:test";
import { readFileSync } from "node:fs";
import {
  SELECTOR_KEYS,
  formatActionRow,
  formatElapsed,
  parseSelector,
  tagFromSelector,
} from "../lib/action-format";
import { summary } from "./fixtures";

const contractPath = new URL(
  "../../../pkg/spec/test/fixtures/selector-keys.json",
  import.meta.url,
);
const contract: { keys: string[] } = JSON.parse(
  readFileSync(contractPath, "utf8"),
);

// The runtimes reject a selector key they do not know, so the key list the UI
// reads rows against has to be the same list. A key the UI omits renders a real
// action as raw text; a key only the UI carries formats a selector no runtime
// could ever emit. internal/hierarchy/selector_keys_test.go and
// pkg/spec/test/selector-keys.test.ts assert the SAME file.
describe("selector keys", () => {
  it("is the cross-runtime list", () => {
    expect([...SELECTOR_KEYS]).toEqual(contract.keys);
  });

  it("parses every key the runtimes accept", () => {
    for (const key of contract.keys) {
      expect(parseSelector(`${key}:value`)).toEqual({
        kind: key,
        value: "value",
      });
    }
  });
});

// Bug class: selector parsing mislabels every action row. Splitting on the
// wrong colon, treating a value-with-colon as the kind, or dropping the prefix
// ellipsis would render the wrong target tag for every step.
describe("parseSelector", () => {
  const cases: { input: string; out: ReturnType<typeof parseSelector> }[] = [
    { input: "id:login", out: { kind: "id", value: "login" } },
    { input: "text:Sign In", out: { kind: "text", value: "Sign In" } },
    {
      input: "testTag:AccountCard",
      out: { kind: "testTag", value: "AccountCard" },
    },
    {
      input: "data-testid:ledger-row",
      out: { kind: "data-testid", value: "ledger-row" },
    },
    {
      input: "accessibilityIdentifier:pay_now",
      out: { kind: "accessibilityIdentifier", value: "pay_now" },
    },
    {
      input: "identifier:pay_now",
      out: { kind: "identifier", value: "pay_now" },
    },
    { input: "descPrefix:Hello", out: { kind: "descPrefix", value: "Hello" } },
    {
      input: "idPrefix:customer_row_",
      out: { kind: "idPrefix", value: "customer_row_" },
    },
    { input: "id:com.app:id/btn", out: { kind: "id", value: "com.app:id/btn" } },
    { input: "bogus:x", out: null },
    { input: "textPrefix:Hello", out: null },
    { input: "classPrefix:android.widget", out: null },
    { input: ":leading", out: null },
    { input: "no-colon", out: null },
  ];
  for (const { input, out } of cases) {
    it(`parses ${input}`, () => {
      expect(parseSelector(input)).toEqual(out);
    });
  }
});

describe("tagFromSelector", () => {
  it("appends ellipsis only for prefix selectors", () => {
    expect(tagFromSelector("descPrefix:Hel")).toBe("Hel...");
    expect(tagFromSelector("idPrefix:customer_row_")).toBe("customer_row_...");
    expect(tagFromSelector("text:Hello")).toBe("Hello");
    expect(tagFromSelector("testTag:AccountCard")).toBe("AccountCard");
    expect(tagFromSelector("plain")).toBe("plain");
  });
});

describe("formatActionRow", () => {
  it("observes when no action kind, with and without screen", () => {
    expect(formatActionRow(summary({}))).toEqual({
      verb: "Observe",
      target: "",
      targetIsTag: false,
    });
    expect(formatActionRow(summary({ screen: "Home" }))).toEqual({
      verb: "Observe",
      target: "@ Home",
      targetIsTag: false,
    });
  });

  it("treats a selector label as a tag and a coordinate label as literal", () => {
    expect(
      formatActionRow(summary({ action_kind: "Tap", action_label: "id:btn" })),
    ).toEqual({ verb: "Click", target: "btn", targetIsTag: true });
    expect(
      formatActionRow(summary({ action_kind: "Tap", action_label: "(10, 20)" })),
    ).toEqual({ verb: "Click", target: "(10, 20)", targetIsTag: false });
  });

  // internal/verifier/worker.go labels android and ios taps with testTag,
  // identifier and accessibilityIdentifier; pkg/spec/src/web-runtime.ts labels
  // web taps with data-testid. testTag is the commonest label of the four.
  it("formats the selector keys the verifier and the web runtime actually emit", () => {
    expect(
      formatActionRow(
        summary({ action_kind: "Tap", action_label: "testTag:AccountCard" }),
      ),
    ).toEqual({ verb: "Click", target: "AccountCard", targetIsTag: true });
    expect(
      formatActionRow(
        summary({ action_kind: "Tap", action_label: "data-testid:ledger-row" }),
      ),
    ).toEqual({ verb: "Click", target: "ledger-row", targetIsTag: true });
    expect(
      formatActionRow(
        summary({
          action_kind: "Tap",
          action_label: "accessibilityIdentifier:pay_now",
        }),
      ),
    ).toEqual({ verb: "Click", target: "pay_now", targetIsTag: true });
  });

  it("maps known verbs and falls back to the raw kind", () => {
    expect(formatActionRow(summary({ action_kind: "InputText", action_label: "hi" })).verb).toBe("Type");
    expect(formatActionRow(summary({ action_kind: "Swipe" })).verb).toBe("Swipe");
    expect(formatActionRow(summary({ action_kind: "Custom" })).verb).toBe("Custom");
  });
});

describe("formatElapsed", () => {
  it("formats mm:ss.mmm and clamps negatives to zero", () => {
    expect(formatElapsed(0)).toBe("00:00.000");
    expect(formatElapsed(65_432)).toBe("01:05.432");
    expect(formatElapsed(-50)).toBe("00:00.000");
  });
});
