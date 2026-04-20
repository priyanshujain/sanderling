import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { listRuns } from "../api";
import type { RunSummary } from "../types";

function formatDuration(milliseconds?: number): string {
  if (milliseconds === undefined) {
    return "-";
  }
  if (milliseconds < 1000) {
    return `${milliseconds}ms`;
  }
  const seconds = milliseconds / 1000;
  if (seconds < 60) {
    return `${seconds.toFixed(1)}s`;
  }
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = Math.round(seconds - minutes * 60);
  return `${minutes}m${remainingSeconds}s`;
}

function formatStartedAt(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

export default function RunList() {
  const [runs, setRuns] = useState<RunSummary[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    listRuns()
      .then((result) => {
        if (!cancelled) {
          setRuns(result);
        }
      })
      .catch((failure: unknown) => {
        if (!cancelled) {
          setError(failure instanceof Error ? failure.message : String(failure));
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  if (error) {
    return <div className="status-block status-error">failed to load runs: {error}</div>;
  }
  if (!runs) {
    return <div className="status-block">loading runs...</div>;
  }
  if (runs.length === 0) {
    return <div className="status-block">no runs yet. run `uatu pbt` to produce some.</div>;
  }

  return (
    <table className="run-table">
      <thead>
        <tr>
          <th>started</th>
          <th>spec</th>
          <th>seed</th>
          <th>platform</th>
          <th>duration</th>
          <th>steps</th>
          <th>violations</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {runs.map((run) => (
          <tr key={run.id}>
            <td>
              <Link to={`/runs/${run.id}`}>{formatStartedAt(run.startedAt)}</Link>
            </td>
            <td>{run.specPath}</td>
            <td>{run.seed}</td>
            <td>{run.platform}</td>
            <td>{formatDuration(run.durationMillis)}</td>
            <td>{run.stepCount}</td>
            <td>
              {run.violationCount > 0 ? (
                <span className="chip chip-violation">{run.violationCount}</span>
              ) : (
                "0"
              )}
            </td>
            <td>{run.inProgress ? <span className="chip chip-progress">in progress</span> : null}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
