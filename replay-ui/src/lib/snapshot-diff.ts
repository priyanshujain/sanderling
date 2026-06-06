export interface Row {
  path: string;
  value: unknown;
}

export const INLINE_ARRAY_LIMIT = 2;

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return (
    typeof value === "object" &&
    value !== null &&
    !Array.isArray(value) &&
    Object.getPrototypeOf(value) === Object.prototype
  );
}

export function flatten(input: Record<string, unknown>): Row[] {
  const rows: Row[] = [];
  const walk = (value: unknown, path: string) => {
    if (isPlainObject(value)) {
      const keys = Object.keys(value).sort();
      if (keys.length === 0) {
        rows.push({ path, value: {} });
        return;
      }
      for (const key of keys) {
        const nextPath = path === "" ? key : `${path}.${key}`;
        walk(value[key], nextPath);
      }
      return;
    }
    if (Array.isArray(value)) {
      if (value.length <= INLINE_ARRAY_LIMIT) {
        rows.push({ path, value });
        return;
      }
      for (let i = 0; i < value.length; i++) {
        walk(value[i], `${path}[${i}]`);
      }
      return;
    }
    rows.push({ path, value });
  };
  for (const key of Object.keys(input).sort()) {
    walk(input[key], key);
  }
  return rows.sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
}

export function getAtPath(
  source: Record<string, unknown> | undefined,
  path: string,
): unknown {
  if (!source) {
    return undefined;
  }
  if (Object.prototype.hasOwnProperty.call(source, path)) {
    return source[path];
  }
  const segments = path
    .split(/\.|\[(\d+)\]/)
    .filter((segment) => segment !== undefined && segment !== "");
  let current: unknown = source;
  for (const segment of segments) {
    if (current === null || current === undefined) {
      return undefined;
    }
    if (Array.isArray(current)) {
      const index = Number(segment);
      if (Number.isNaN(index)) {
        return undefined;
      }
      current = current[index];
      continue;
    }
    if (typeof current === "object") {
      current = (current as Record<string, unknown>)[segment];
      continue;
    }
    return undefined;
  }
  return current;
}

export function canonicalize(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map(canonicalize);
  }
  if (isPlainObject(value)) {
    const out: Record<string, unknown> = {};
    for (const key of Object.keys(value).sort()) {
      out[key] = canonicalize(value[key]);
    }
    return out;
  }
  return value;
}

export function stableStringify(value: unknown): string {
  return JSON.stringify(canonicalize(value));
}
