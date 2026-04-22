import type { Exception } from "../types";
import "./ExceptionsPanel.css";

export interface ExceptionsPanelProps {
  exceptions?: Exception[];
  onJumpToFirstException: () => void;
  hasFirstException: boolean;
}

function formatTimestamp(unixMillis: number): string {
  const date = new Date(unixMillis);
  const hh = String(date.getHours()).padStart(2, "0");
  const mm = String(date.getMinutes()).padStart(2, "0");
  const ss = String(date.getSeconds()).padStart(2, "0");
  const ms = String(date.getMilliseconds()).padStart(3, "0");
  return `${hh}:${mm}:${ss}.${ms}`;
}

export default function ExceptionsPanel({
  exceptions,
  onJumpToFirstException,
  hasFirstException,
}: ExceptionsPanelProps) {
  return (
    <div className="exceptions-panel">
      <div className="exceptions-panel-toolbar">
        <button
          type="button"
          className="exceptions-panel-jump"
          onClick={onJumpToFirstException}
          disabled={!hasFirstException}
        >
          jump to first exception
        </button>
      </div>
      {!exceptions || exceptions.length === 0 ? (
        <div className="status-block">no exceptions</div>
      ) : (
        <ol className="exceptions-list">
          {exceptions.map((exception, index) => (
            <li key={index} className="exceptions-row">
              <details open={index === 0}>
                <summary className="exceptions-summary">
                  <span className="exceptions-marker" aria-hidden="true" />
                  <span className="exceptions-class">{exception.class}</span>
                  {exception.message ? (
                    <>
                      <span className="exceptions-colon">:</span>
                      <span className="exceptions-message">{exception.message}</span>
                    </>
                  ) : null}
                  {exception.unix_millis !== undefined ? (
                    <span className="exceptions-time">
                      {formatTimestamp(exception.unix_millis)}
                    </span>
                  ) : null}
                </summary>
                {exception.stack_trace ? (
                  <pre className="exceptions-stack">{exception.stack_trace}</pre>
                ) : null}
              </details>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}
