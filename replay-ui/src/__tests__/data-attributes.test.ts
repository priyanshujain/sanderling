import { describe, it, expect } from "bun:test";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { attributeOf } from "../../sanderling/data-attributes";

// Bug class: the web runtime keys attrs by the raw markup name, so a spec
// reading attrs["stepCount"] gets undefined and every property over it passes
// vacuously against a UI that is broken.
describe("attributeOf", () => {
  it("names the attribute the markup writes for each logical key", () => {
    expect(attributeOf("step")).toBe("data-step");
    expect(attributeOf("stepCount")).toBe("data-step-count");
    expect(attributeOf("active")).toBe("data-active");
    expect(attributeOf("violations")).toBe("data-violations");
    expect(attributeOf("tabId")).toBe("data-tab-id");
    expect(attributeOf("violationCount")).toBe("data-violation-count");
  });
});

const here = new URL(".", import.meta.url).pathname;
const specPath = join(here, "../../sanderling/spec.ts");
const sourceRoot = join(here, "..");

function sourceFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) return entry.name === "__tests__" ? [] : sourceFiles(path);
    return entry.name.endsWith(".tsx") || entry.name.endsWith(".ts") ? [path] : [];
  });
}

function keysReadByTheSpec(): string[] {
  const spec = readFileSync(specPath, "utf8");
  const keys = new Set<string>();
  for (const match of spec.matchAll(/dataOf\([^,]+,\s*"([^"]+)"\)/g)) keys.add(match[1]);
  return [...keys];
}

describe("the keys the spec reads", () => {
  const keys = keysReadByTheSpec();

  it("covers every dataOf call site", () => {
    expect(keys.sort()).toEqual([
      "active",
      "step",
      "stepCount",
      "tabId",
      "violationCount",
      "violations",
    ]);
  });

  it("names attributes the UI renders", () => {
    const markup = sourceFiles(sourceRoot)
      .map((path) => readFileSync(path, "utf8"))
      .join("\n");
    const missing = keys.map(attributeOf).filter((attribute) => !markup.includes(`${attribute}=`));
    expect(missing).toEqual([]);
  });
});
