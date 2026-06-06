import { flatten, getAtPath, stableStringify } from "../lib/snapshot-diff";
import "./SnapshotTable.css";

export interface SnapshotTableProps {
  snapshots?: Record<string, unknown>;
  previousSnapshots?: Record<string, unknown>;
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
