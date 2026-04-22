import "./SnapshotTable.css";

export interface SnapshotTableProps {
  snapshots?: Record<string, unknown>;
  previousSnapshots?: Record<string, unknown>;
}

interface Row {
  path: string;
  value: unknown;
}

const INLINE_ARRAY_LIMIT = 2;

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return (
    typeof value === "object" &&
    value !== null &&
    !Array.isArray(value) &&
    Object.getPrototypeOf(value) === Object.prototype
  );
}

function flatten(input: Record<string, unknown>): Row[] {
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

function getAtPath(source: Record<string, unknown> | undefined, path: string): unknown {
  if (!source) {
    return undefined;
  }
  if (Object.prototype.hasOwnProperty.call(source, path)) {
    return source[path];
  }
  const segments = path.split(/\.|\[(\d+)\]/).filter((segment) => segment !== undefined && segment !== "");
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

function canonicalize(value: unknown): unknown {
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

function stableStringify(value: unknown): string {
  return JSON.stringify(canonicalize(value));
}

function formatValue(value: unknown): string {
  if (value === null) {
    return "null";
  }
  if (value === undefined) {
    return "undefined";
  }
  if (typeof value === "string") {
    return JSON.stringify(value);
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  return stableStringify(value);
}

export default function SnapshotTable({ snapshots, previousSnapshots }: SnapshotTableProps) {
  if (!snapshots || Object.keys(snapshots).length === 0) {
    return <div className="status-block">no snapshots</div>;
  }

  const rows = flatten(snapshots);
  const hasPrevious = previousSnapshots !== undefined;
  const previousRows = hasPrevious ? flatten(previousSnapshots) : [];
  const previousByPath = new Map(previousRows.map((row) => [row.path, row.value]));

  return (
    <dl className="snapshot-table">
      {rows.map((row) => {
        const formatted = formatValue(row.value);
        let changed = false;
        let previousFormatted: string | undefined;
        if (hasPrevious) {
          const prevValue = previousByPath.has(row.path)
            ? previousByPath.get(row.path)
            : getAtPath(previousSnapshots, row.path);
          if (stableStringify(prevValue) !== stableStringify(row.value)) {
            changed = true;
            previousFormatted = formatValue(prevValue);
          }
        }
        const rowProps: Record<string, string> = {};
        if (changed) {
          rowProps["data-changed"] = "true";
          rowProps.title = `was: ${previousFormatted}`;
        }
        return (
          <div key={row.path} className="snapshot-row" {...rowProps}>
            <dt className="snapshot-path" title={row.path}>
              {row.path}
            </dt>
            <dd className="snapshot-value" title={formatted}>
              {formatted}
            </dd>
          </div>
        );
      })}
    </dl>
  );
}
