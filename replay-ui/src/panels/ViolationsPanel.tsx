import { useMemo } from "react";
import type { ResidualNode, Witness } from "../types";
import { STATUS_ORDER, statusFor } from "../lib/property-status";
import ResidualNodeView from "../components/ResidualNode";
import "./ViolationsPanel.css";

export interface ViolationsPanelProps {
  propertyNames: string[];
  violations: string[];
  residuals?: Record<string, ResidualNode>;
  witnesses?: Record<string, Witness>;
  onJumpToFirstViolation: () => void;
  hasFirstViolation: boolean;
  onJumpToStep?: (step: number) => void;
  /** When true, only render violated rows and hide the header button row. */
  violationsOnly?: boolean;
}

function formatValue(value: unknown): string {
  const encoded = JSON.stringify(value);
  return encoded === undefined ? String(value) : encoded;
}

function WitnessView({
  witness,
  onJumpToStep,
  open,
}: {
  witness: Witness;
  onJumpToStep?: (step: number) => void;
  open: boolean;
}) {
  const evidence = Object.entries(witness.extractors ?? {})
    .filter(([, value]) => value !== null && value !== undefined)
    .sort(([a], [b]) => a.localeCompare(b));
  return (
    <div className="violations-panel-witness">
      {witness.reason ? (
        <div className="violations-panel-witness-line">
          <span className="violations-panel-witness-key">
            {witness.is_error ? "error" : "reason"}
          </span>
          <span className="violations-panel-witness-value">{witness.reason}</span>
        </div>
      ) : null}
      {witness.step ? (
        <div className="violations-panel-witness-line">
          <span className="violations-panel-witness-key">caused at</span>
          {onJumpToStep ? (
            <button
              type="button"
              className="violations-panel-witness-step"
              onClick={() => onJumpToStep(witness.step as number)}
            >
              step {witness.step}
            </button>
          ) : (
            <span className="violations-panel-witness-value">step {witness.step}</span>
          )}
        </div>
      ) : null}
      {evidence.length > 0 ? (
        <details className="violations-panel-residual" open={open}>
          <summary>witness</summary>
          <dl className="violations-panel-witness-evidence">
            {evidence.map(([name, value]) => (
              <div key={name} className="violations-panel-witness-line">
                <dt className="violations-panel-witness-key">{name}</dt>
                <dd className="violations-panel-witness-value">{formatValue(value)}</dd>
              </div>
            ))}
          </dl>
        </details>
      ) : null}
    </div>
  );
}

export default function ViolationsPanel({
  propertyNames,
  violations,
  residuals,
  witnesses,
  onJumpToFirstViolation,
  hasFirstViolation,
  onJumpToStep,
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
          const witness = status === "violated" ? witnesses?.[name] : undefined;
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
              {witness ? (
                <WitnessView witness={witness} onJumpToStep={onJumpToStep} open={violationsOnly} />
              ) : null}
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
