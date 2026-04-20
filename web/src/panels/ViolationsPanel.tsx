import { useMemo } from "react";
import type { ResidualNode } from "../types";
import ResidualNodeView from "../components/ResidualNode";
import "./ViolationsPanel.css";

export interface ViolationsPanelProps {
  propertyNames: string[];
  violations: string[];
  residuals?: Record<string, ResidualNode>;
  onJumpToFirstViolation: () => void;
  hasFirstViolation: boolean;
  /** When true, only render violated rows and hide the header button row. */
  violationsOnly?: boolean;
}

type Status = "violated" | "pending" | "holds";

const STATUS_ORDER: Record<Status, number> = {
  violated: 0,
  pending: 1,
  holds: 2,
};

function statusFor(
  name: string,
  violations: Set<string>,
  residuals?: Record<string, ResidualNode>,
): Status {
  if (violations.has(name)) {
    return "violated";
  }
  const residual = residuals?.[name];
  if (residual && residual.op === "true") {
    return "holds";
  }
  return "pending";
}

export default function ViolationsPanel({
  propertyNames,
  violations,
  residuals,
  onJumpToFirstViolation,
  hasFirstViolation,
  violationsOnly = false,
}: ViolationsPanelProps) {
  const violationSet = useMemo(() => new Set(violations), [violations]);

  const rows = useMemo(() => {
    const sorted = [...propertyNames].sort((a, b) => a.localeCompare(b));
    const all = sorted.map((name) => ({
      name,
      status: statusFor(name, violationSet, residuals),
    }));
    const filtered = violationsOnly ? all.filter((row) => row.status === "violated") : all;
    return filtered.sort((a, b) => {
      const groupDelta = STATUS_ORDER[a.status] - STATUS_ORDER[b.status];
      if (groupDelta !== 0) {
        return groupDelta;
      }
      return a.name.localeCompare(b.name);
    });
  }, [propertyNames, violationSet, residuals, violationsOnly]);

  if (violationsOnly && rows.length === 0) {
    return <div className="status-block">no violations</div>;
  }

  return (
    <section className="violations-panel">
      {violationsOnly ? null : (
        <header className="violations-panel-header">
          <h2 className="violations-panel-title">properties</h2>
          <button
            type="button"
            className="violations-panel-jump"
            onClick={onJumpToFirstViolation}
            disabled={!hasFirstViolation}
          >
            jump to first violation
          </button>
        </header>
      )}
      <ul className="violations-panel-list">
        {rows.map(({ name, status }) => {
          const residual = residuals?.[name];
          return (
            <li key={name} className="violations-panel-row" data-status={status}>
              <div className="violations-panel-row-head">
                <span
                  className="violations-panel-badge"
                  data-status={status}
                  aria-label={`status ${status}`}
                >
                  {status}
                </span>
                <span className="violations-panel-name">{name}</span>
              </div>
              {residual ? (
                <details className="violations-panel-residual">
                  <summary>residual</summary>
                  <ResidualNodeView node={residual} />
                </details>
              ) : null}
            </li>
          );
        })}
      </ul>
    </section>
  );
}
