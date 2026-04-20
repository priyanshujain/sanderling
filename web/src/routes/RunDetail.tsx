import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { getRun, getStep } from "../api";
import type { Run, Step } from "../types";

export default function RunDetail() {
  const { id, step } = useParams<{ id: string; step: string }>();
  const stepIndex = Number(step ?? "1");
  const [run, setRun] = useState<Run | null>(null);
  const [currentStep, setCurrentStep] = useState<Step | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!id) return;
    let cancelled = false;
    setRun(null);
    getRun(id)
      .then((result) => {
        if (!cancelled) setRun(result);
      })
      .catch((failure: unknown) => {
        if (!cancelled) {
          setError(failure instanceof Error ? failure.message : String(failure));
        }
      });
    return () => {
      cancelled = true;
    };
  }, [id]);

  useEffect(() => {
    if (!id || Number.isNaN(stepIndex)) return;
    let cancelled = false;
    setCurrentStep(null);
    getStep(id, stepIndex)
      .then((result) => {
        if (!cancelled) setCurrentStep(result);
      })
      .catch((failure: unknown) => {
        if (!cancelled) {
          setError(failure instanceof Error ? failure.message : String(failure));
        }
      });
    return () => {
      cancelled = true;
    };
  }, [id, stepIndex]);

  if (error) {
    return <div className="status-block status-error">failed: {error}</div>;
  }
  if (!run) {
    return <div className="status-block">loading run...</div>;
  }

  return (
    <div className="detail-grid">
      <aside className="detail-panel">
        <h2>steps</h2>
        <ol style={{ listStyle: "none", padding: 0, margin: 0 }}>
          {run.steps.map((entry) => {
            const isActive = entry.index === stepIndex;
            return (
              <li key={entry.index} style={{ padding: "4px 0" }}>
                <Link
                  to={`/runs/${run.meta.id}/steps/${entry.index}`}
                  style={{ fontWeight: isActive ? 500 : 400 }}
                >
                  {entry.index}. {entry.actionKind ?? "(no action)"}
                </Link>
                {entry.hasViolations ? (
                  <span className="chip chip-violation" style={{ marginLeft: 8 }}>
                    !
                  </span>
                ) : null}
              </li>
            );
          })}
        </ol>
      </aside>

      <section className="detail-panel">
        <h2>screenshot</h2>
        {currentStep ? (
          <div className="status-block">screenshot panel placeholder (step {currentStep.index})</div>
        ) : (
          <div className="status-block">loading step...</div>
        )}
      </section>

      <aside className="detail-panel">
        <h2>state</h2>
        <div className="status-block">snapshots / violations / exceptions placeholder</div>
      </aside>
    </div>
  );
}
