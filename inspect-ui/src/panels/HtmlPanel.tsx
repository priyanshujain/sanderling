import { useEffect, useState } from "react";
import { htmlUrl } from "../api";
import "./HtmlPanel.css";

interface HtmlPanelProps {
  runId: string;
  fileName: string;
  available: boolean;
}

type ViewMode = "render" | "source";

export default function HtmlPanel({ runId, fileName, available }: HtmlPanelProps): JSX.Element {
  const [mode, setMode] = useState<ViewMode>("render");
  const [source, setSource] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const url = htmlUrl(runId, fileName);

  useEffect(() => {
    if (!available || mode !== "source") return;
    let cancelled = false;
    setSource(null);
    setError(null);
    fetch(url)
      .then((response) => {
        if (!response.ok) throw new Error(`${response.status} ${response.statusText}`);
        return response.text();
      })
      .then((text) => {
        if (!cancelled) setSource(text);
      })
      .catch((failure: unknown) => {
        if (!cancelled) setError(failure instanceof Error ? failure.message : String(failure));
      });
    return () => {
      cancelled = true;
    };
  }, [available, mode, url]);

  if (!available) {
    return <div className="html-empty">no html captured for this step</div>;
  }

  return (
    <div className="html-panel">
      <div className="html-toolbar">
        <button
          type="button"
          className={mode === "render" ? "html-tab html-tab-active" : "html-tab"}
          onClick={() => setMode("render")}
        >
          render
        </button>
        <button
          type="button"
          className={mode === "source" ? "html-tab html-tab-active" : "html-tab"}
          onClick={() => setMode("source")}
        >
          source
        </button>
      </div>
      {mode === "render" ? (
        <iframe className="html-frame" src={url} sandbox="" title={fileName} />
      ) : error ? (
        <div className="html-error">failed to load: {error}</div>
      ) : source === null ? (
        <div className="html-empty">loading...</div>
      ) : (
        <pre className="html-source">{source}</pre>
      )}
    </div>
  );
}
