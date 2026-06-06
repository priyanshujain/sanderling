import { describe, it, expect } from "bun:test";
import {
  canonicalize,
  flatten,
  getAtPath,
  stableStringify,
} from "../lib/snapshot-diff";

// Bug class: a path off-by-one in the getAtPath regex parse resolves the wrong
// node, so every diff between two snapshots is mis-reported. flatten emits the
// paths the diff later feeds back through getAtPath, so every emitted path must
// resolve to the same value flatten recorded.
describe("flatten/getAtPath round-trip", () => {
  const cases: { name: string; input: Record<string, unknown> }[] = [
    { name: "nested objects", input: { a: { b: { c: 1 } } } },
    {
      name: "array longer than inline limit indexes each element",
      input: { items: [10, 20, 30, 40] },
    },
    {
      name: "objects inside an expanded array",
      input: { rows: [{ id: 1 }, { id: 2 }, { id: 3 }] },
    },
    {
      name: "keys with dots are matched verbatim before regex split",
      input: { "a.b": 7 },
    },
    {
      name: "mixed nesting",
      input: { ui: { tabs: ["x", "y", "z", "w"], open: true }, n: 0 },
    },
  ];

  for (const { name, input } of cases) {
    it(name, () => {
      for (const row of flatten(input)) {
        expect(stableStringify(getAtPath(input, row.path))).toBe(
          stableStringify(row.value),
        );
      }
    });
  }

  it("indexes array elements by their real position, not off by one", () => {
    const input = { items: ["a", "b", "c", "d"] };
    const rows = flatten(input);
    expect(getAtPath(input, "items[0]")).toBe("a");
    expect(getAtPath(input, "items[3]")).toBe("d");
    expect(rows.map((r) => r.path)).toEqual([
      "items[0]",
      "items[1]",
      "items[2]",
      "items[3]",
    ]);
  });
});

describe("canonicalize", () => {
  it("orders object keys so reordered snapshots compare equal", () => {
    expect(stableStringify({ b: 1, a: 2 })).toBe(stableStringify({ a: 2, b: 1 }));
    expect(canonicalize({ b: 1, a: 2 })).toEqual({ a: 2, b: 1 });
  });

  it("preserves array order", () => {
    expect(stableStringify([3, 1, 2])).not.toBe(stableStringify([1, 2, 3]));
  });
});
