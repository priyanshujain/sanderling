import { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { getRun, getStep, screenshotUrl } from "../api";
import type { Run, Step } from "../types";
import ActionList from "../panels/ActionList";
import HierarchyPanel from "../panels/HierarchyPanel";
import Screenshot from "../panels/Screenshot";
import SnapshotTable from "../panels/SnapshotTable";
import ViolationsPanel from "../panels/ViolationsPanel";
import ExceptionsPanel from "../panels/ExceptionsPanel";
import type { LaneStatus, PropertyLane } from "../panels/Timeline";
import MetricsChart, { type MetricsSample } from "../panels/MetricsChart";
import Tabs, { type TabDefinition } from "../components/Tabs";
import { useStep } from "../hooks/useStep";
import { useKeyboardNav } from "../hooks/useKeyboardNav";
import { useTheme } from "../hooks/useTheme";

interface RunHistory {
  names: string[];
  lanes: PropertyLane[];
  firstViolationStep?: number;
  firstExceptionStep?: number;
  exceptionStepIndices: number[];
  violationStepIndices: number[];
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

  const beforeTabs: TabDefinition[] = [
    {
      id: "screenshot",
      label: "Screenshot",
      content: <Screenshot src={beforeScreenshot} action={currentStep?.action} />,
    },
    {
      id: "snapshots",
      label: "Snapshots",
      content: (
        <SnapshotTable
          snapshots={currentStep?.snapshots}
          previousSnapshots={previousStep?.snapshots ?? undefined}
        />
      ),
    },
    {
      id: "hierarchy",
      label: "Hierarchy",
      content: <HierarchyPanel hierarchy={currentStep?.hierarchy} />,
    },
    {
      id: "properties",
      label: "Properties",
      content: (
        <ViolationsPanel
          propertyNames={history?.names ?? []}
          violations={violationsBefore}
          residuals={residualsBefore}
          onJumpToFirstViolation={jumpToFirstViolation}
          hasFirstViolation={history?.firstViolationStep !== undefined}
        />
      ),
    },
    {
      id: "violations",
      label: "Violations",
      badge:
        violationsBefore.length > 0 ? (
          <span className="tabs-badge" data-kind="violation">
            {violationsBefore.length}
          </span>
        ) : undefined,
      content: (
        <ViolationsPanel
          propertyNames={history?.names ?? []}
          violations={violationsBefore}
          residuals={residualsBefore}
          onJumpToFirstViolation={jumpToFirstViolation}
          hasFirstViolation={history?.firstViolationStep !== undefined}
          violationsOnly
        />
      ),
    },
  ];

  const afterTabs: TabDefinition[] = [
    {
      id: "screenshot",
      label: "Screenshot",
      content: <Screenshot src={afterScreenshot} action={undefined} />,
    },
    {
      id: "snapshots",
      label: "Snapshots",
      content: (
        <SnapshotTable
          snapshots={nextStep?.snapshots ?? currentStep?.snapshots}
          previousSnapshots={currentStep?.snapshots ?? undefined}
        />
      ),
    },
    {
      id: "hierarchy",
      label: "Hierarchy",
      content: <HierarchyPanel hierarchy={nextStep?.hierarchy ?? currentStep?.hierarchy} />,
    },
    {
      id: "properties",
      label: "Properties",
      content: (
        <ViolationsPanel
          propertyNames={history?.names ?? []}
          violations={violationsAfter}
          residuals={residualsAfter}
          onJumpToFirstViolation={jumpToFirstViolation}
          hasFirstViolation={history?.firstViolationStep !== undefined}
        />
      ),
    },
    {
      id: "violations",
      label: "Violations",
      badge:
        violationsAfter.length > 0 ? (
          <span className="tabs-badge" data-kind="violation">
            {violationsAfter.length}
          </span>
        ) : undefined,
      content: (
        <ViolationsPanel
          propertyNames={history?.names ?? []}
          violations={violationsAfter}
          residuals={residualsAfter}
          onJumpToFirstViolation={jumpToFirstViolation}
          hasFirstViolation={history?.firstViolationStep !== undefined}
          violationsOnly
        />
      ),
    },
  ];

  return (
    <div className="detail-root">
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
        <button
          type="button"
          className="theme-toggle"
          onClick={toggle}
          aria-label={theme === "dark" ? "switch to light theme" : "switch to dark theme"}
          title={theme === "dark" ? "switch to light theme" : "switch to dark theme"}
        >
          {theme === "dark" ? (
            <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true" focusable="false">
              <circle cx="8" cy="8" r="3" fill="none" stroke="currentColor" strokeWidth="1.25" />
              <g stroke="currentColor" strokeWidth="1.25" strokeLinecap="round">
                <line x1="8" y1="1.5" x2="8" y2="3.5" />
                <line x1="8" y1="12.5" x2="8" y2="14.5" />
                <line x1="1.5" y1="8" x2="3.5" y2="8" />
                <line x1="12.5" y1="8" x2="14.5" y2="8" />
                <line x1="3.4" y1="3.4" x2="4.8" y2="4.8" />
                <line x1="11.2" y1="11.2" x2="12.6" y2="12.6" />
                <line x1="3.4" y1="12.6" x2="4.8" y2="11.2" />
                <line x1="11.2" y1="4.8" x2="12.6" y2="3.4" />
              </g>
            </svg>
          ) : (
            <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true" focusable="false">
              <path
                d="M13.2 9.8A5.2 5.2 0 0 1 6.2 2.8a5.2 5.2 0 1 0 7 7Z"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.25"
                strokeLinejoin="round"
              />
            </svg>
          )}
        </button>
      </div>
      <div className="detail-grid">
        <aside className="detail-actions detail-panel">
          <h2>actions</h2>
          <div className="detail-panel-body">
            <ActionList
              steps={run.steps}
              selectedIndex={stepIndex}
              onSelect={goTo}
              runStartMillis={runStartMillis}
              selectedStep={currentStep ?? undefined}
            />
          </div>
        </aside>

        <section className="detail-state-before detail-panel">
          <h2>state before</h2>
          <div className="detail-panel-body">
            <Tabs tabs={beforeTabs} defaultTabId="screenshot" ariaLabel="state before" />
          </div>
        </section>

        <section className="detail-state-after detail-panel">
          <h2>state after</h2>
          <div className="detail-panel-body">
            <Tabs tabs={afterTabs} defaultTabId="screenshot" ariaLabel="state after" />
          </div>
        </section>

        <section className="detail-metrics detail-panel">
          <div className="detail-panel-body">
            <MetricsChart
              samples={history?.metricsSamples ?? []}
              selectedIndex={stepIndex}
              onSelect={goTo}
              runStartMillis={runStartMillis}
            />
            {(exceptionsForStep && exceptionsForStep.length > 0) ||
            history?.firstExceptionStep !== undefined ? (
              <div className="detail-metrics-exceptions">
                <ExceptionsPanel
                  exceptions={exceptionsForStep}
                  onJumpToFirstException={jumpToFirstException}
                  hasFirstException={history?.firstExceptionStep !== undefined}
                />
              </div>
            ) : null}
          </div>
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
  const exceptionStepIndices = run.steps
    .filter((entry) => entry.has_exceptions)
    .map((entry) => entry.index);
  const violationStepIndices = run.steps
    .filter((entry) => entry.has_violations)
    .map((entry) => entry.index);
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
    exceptionStepIndices,
    violationStepIndices,
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
