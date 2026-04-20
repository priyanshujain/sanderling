import { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { getRun, getStep, screenshotUrl } from "../api";
import type { Run, Step } from "../types";
import ActionList from "../panels/ActionList";
import Screenshot from "../panels/Screenshot";
import SnapshotTable from "../panels/SnapshotTable";
import ViolationsPanel from "../panels/ViolationsPanel";
import ExceptionsPanel from "../panels/ExceptionsPanel";
import Timeline, { type LaneStatus, type PropertyLane } from "../panels/Timeline";
import MetricsChart, { type MetricsSample } from "../panels/MetricsChart";
import { useStep } from "../hooks/useStep";
import { useKeyboardNav } from "../hooks/useKeyboardNav";
import { useTheme } from "../hooks/useTheme";

interface RunHistory {
  names: string[];
  lanes: PropertyLane[];
  firstViolationStep?: number;
  firstExceptionStep?: number;
  metricsSamples: MetricsSample[];
  steps: (Step | null)[];
}

export default function RunDetail() {
  const [run, setRun] = useState<Run | null>(null);
  const [history, setHistory] = useState<RunHistory | null>(null);
  const [error, setError] = useState<string | null>(null);
  const { theme, toggle } = useTheme();

  const stepCount = run?.steps.length;
  const { runId, stepIndex, goTo } = useStep(stepCount);

  useEffect(() => {
    if (!runId) return;
    let cancelled = false;
    setRun(null);
    setHistory(null);
    setError(null);
    getRun(runId)
      .then(async (loaded) => {
        if (cancelled) return;
        setRun(loaded);
        const computed = await loadHistory(loaded);
        if (!cancelled) {
          setHistory(computed);
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
  }, [runId]);

  const currentStep = history?.steps[stepIndex - 1] ?? null;
  const previousStep = stepIndex > 1 ? history?.steps[stepIndex - 2] ?? null : null;
  const nextStep = history && stepIndex < history.steps.length ? history.steps[stepIndex] ?? null : null;

  const jumpToFirstViolation = useCallback(() => {
    if (history?.firstViolationStep) {
      goTo(history.firstViolationStep);
    }
  }, [history, goTo]);

  const jumpToFirstException = useCallback(() => {
    if (history?.firstExceptionStep) {
      goTo(history.firstExceptionStep);
    }
  }, [history, goTo]);

  const jumpToNextViolation = useCallback(() => {
    if (!run) return;
    const next = run.steps.find((entry) => entry.index > stepIndex && entry.has_violations);
    if (next) goTo(next.index);
  }, [run, stepIndex, goTo]);

  useKeyboardNav({
    onPrev: () => goTo(stepIndex - 1),
    onNext: () => goTo(stepIndex + 1),
    onJumpStart: () => goTo(1),
    onJumpEnd: () => stepCount && goTo(stepCount),
    onJumpPrev10: () => goTo(stepIndex - 10),
    onJumpNext10: () => goTo(stepIndex + 10),
    onJumpNextViolation: jumpToNextViolation,
  });

  const beforeScreenshot = useMemo(() => {
    if (!runId || !currentStep) return undefined;
    return screenshotUrl(runId, `step-${String(currentStep.step).padStart(5, "0")}.png`);
  }, [runId, currentStep]);

  const afterScreenshot = useMemo(() => {
    if (!runId || !currentStep) return undefined;
    return screenshotUrl(runId, `step-${String(currentStep.step).padStart(5, "0")}-after.png`);
  }, [runId, currentStep]);

  const runStartMillis = useMemo(() => {
    if (!run?.steps.length) return 0;
    return new Date(run.steps[0].timestamp).getTime();
  }, [run]);

  if (error) {
    return <div className="status-block status-error">failed: {error}</div>;
  }
  if (!run) {
    return <div className="status-block">loading run...</div>;
  }

  const violationsBefore = currentStep?.violations ?? [];
  const violationsAfter = nextStep?.violations ?? violationsBefore;
  const residualsBefore = currentStep?.residuals;
  const residualsAfter = nextStep?.residuals ?? residualsBefore;
  const exceptionsForStep = currentStep?.exceptions;

  return (
    <div>
      <div className="detail-toolbar">
        <div className="detail-toolbar-meta">
          <Link to="/">runs</Link>
          <span>{run.id}</span>
          <span>
            <strong>{run.spec_path}</strong> seed={run.seed}
          </span>
          <span>
            step {stepIndex} / {stepCount ?? 0}
          </span>
        </div>
        <button type="button" className="theme-toggle" onClick={toggle}>
          {theme === "dark" ? "light" : "dark"}
        </button>
      </div>
      <div className="detail-grid">
        <aside className="detail-actions detail-panel">
          <h2>actions</h2>
          <ActionList
            steps={run.steps}
            selectedIndex={stepIndex}
            onSelect={goTo}
            runStartMillis={runStartMillis}
            selectedStep={currentStep ?? undefined}
          />
        </aside>

        <section className="detail-state-before detail-panel">
          <h2>state before</h2>
          <Screenshot src={beforeScreenshot} action={currentStep?.action} />
          <SnapshotTable
            snapshots={currentStep?.snapshots}
            previousSnapshots={previousStep?.snapshots ?? undefined}
          />
          <ViolationsPanel
            propertyNames={history?.names ?? []}
            violations={violationsBefore}
            residuals={residualsBefore}
            onJumpToFirstViolation={jumpToFirstViolation}
            hasFirstViolation={history?.firstViolationStep !== undefined}
          />
        </section>

        <section className="detail-state-after detail-panel">
          <h2>state after</h2>
          <Screenshot src={afterScreenshot} action={undefined} />
          <SnapshotTable
            snapshots={nextStep?.snapshots ?? currentStep?.snapshots}
            previousSnapshots={currentStep?.snapshots ?? undefined}
          />
          <ViolationsPanel
            propertyNames={history?.names ?? []}
            violations={violationsAfter}
            residuals={residualsAfter}
            onJumpToFirstViolation={jumpToFirstViolation}
            hasFirstViolation={history?.firstViolationStep !== undefined}
          />
        </section>

        <aside className="detail-side">
          <div className="detail-panel">
            <h2>exceptions</h2>
            <ExceptionsPanel
              exceptions={exceptionsForStep}
              onJumpToFirstException={jumpToFirstException}
              hasFirstException={history?.firstExceptionStep !== undefined}
            />
          </div>
          <div className="detail-panel detail-timeline-inline">
            <h2>timeline</h2>
            <Timeline
              steps={run.steps}
              lanes={history?.lanes ?? []}
              selectedIndex={stepIndex}
              onSelect={goTo}
            />
          </div>
        </aside>

        <section className="detail-metrics detail-panel">
          <MetricsChart
            samples={history?.metricsSamples ?? []}
            selectedIndex={stepIndex}
            onSelect={goTo}
          />
        </section>
      </div>
    </div>
  );
}

async function loadHistory(run: Run): Promise<RunHistory> {
  const responses = await Promise.all(
    run.steps.map((entry) => getStep(run.id, entry.index).catch(() => null)),
  );
  const propertyNames = collectPropertyNames(responses);
  const lanes: PropertyLane[] = propertyNames.map((name) => ({
    name,
    statuses: responses.map((step) => statusForProperty(name, step)),
  }));
  const firstViolationStep = run.steps.find((entry) => entry.has_violations)?.index;
  const firstExceptionStep = run.steps.find((entry) => entry.has_exceptions)?.index;
  const metricsSamples: MetricsSample[] = run.steps.map((entry, position) => ({
    stepIndex: entry.index,
    timestamp: entry.timestamp,
    metrics: responses[position]?.metrics,
  }));
  return {
    names: propertyNames,
    lanes: sortLanes(lanes),
    firstViolationStep,
    firstExceptionStep,
    metricsSamples,
    steps: responses,
  };
}

function collectPropertyNames(steps: (Step | null)[]): string[] {
  const names = new Set<string>();
  for (const step of steps) {
    if (!step?.residuals) continue;
    for (const name of Object.keys(step.residuals)) {
      names.add(name);
    }
  }
  return [...names].sort();
}

function statusForProperty(name: string, step: Step | null): LaneStatus {
  if (!step) return "pending";
  if (step.violations?.includes(name)) return "violated";
  const residual = step.residuals?.[name];
  if (residual && residual.op === "true") return "holds";
  return "pending";
}

function sortLanes(lanes: PropertyLane[]): PropertyLane[] {
  const rank = (lane: PropertyLane): number => {
    const last = lane.statuses[lane.statuses.length - 1];
    if (lane.statuses.includes("violated")) return 0;
    if (last === "pending") return 1;
    return 2;
  };
  return [...lanes].sort((a, b) => {
    const delta = rank(a) - rank(b);
    if (delta !== 0) return delta;
    return a.name.localeCompare(b.name);
  });
}
