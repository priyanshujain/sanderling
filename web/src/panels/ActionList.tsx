import { useEffect, useRef } from "react";
import type { KeyboardEvent } from "react";
import type { Step, StepSummary } from "../types";
import "./ActionList.css";

export interface ActionListProps {
  steps: StepSummary[];
  selectedIndex: number;
  onSelect: (index: number) => void;
  runStartMillis?: number;
  selectedStep?: Step;
}

interface FormattedRow {
  verb: string;
  target: string;
  targetIsTag: boolean;
}

const SELECTOR_PREFIXES = [
  "id",
  "text",
  "textPrefix",
  "desc",
  "descPrefix",
  "class",
  "classPrefix",
  "package",
];

function parseSelector(selector: string): { kind: string; value: string } | null {
  const colonIndex = selector.indexOf(":");
  if (colonIndex <= 0) {
    return null;
  }
  const kind = selector.slice(0, colonIndex);
  const value = selector.slice(colonIndex + 1);
  if (!SELECTOR_PREFIXES.includes(kind)) {
    return null;
  }
  return { kind, value };
}

function tagFromSelector(selector: string): string {
  const parsed = parseSelector(selector);
  if (!parsed) {
    return selector;
  }
  if (parsed.kind.endsWith("Prefix")) {
    return `${parsed.value}...`;
  }
  return parsed.value;
}

export function formatActionRow(step: StepSummary): FormattedRow {
  const kind = step.action_kind;
  const label = step.action_label ?? "";

  if (!kind) {
    if (step.screen) {
      return { verb: "Observe", target: `@ ${step.screen}`, targetIsTag: false };
    }
    return { verb: "Observe", target: "", targetIsTag: false };
  }

  switch (kind) {
    case "Tap": {
      if (!label) {
        return { verb: "Click", target: "", targetIsTag: false };
      }
      if (label.startsWith("(") && label.endsWith(")")) {
        return { verb: "Click", target: label, targetIsTag: false };
      }
      if (parseSelector(label)) {
        return { verb: "Click", target: tagFromSelector(label), targetIsTag: true };
      }
      return { verb: "Click", target: label, targetIsTag: false };
    }
    case "InputText":
      return { verb: "Type", target: label, targetIsTag: false };
    case "Swipe":
      return { verb: "Swipe", target: label, targetIsTag: true };
    case "PressKey":
      return { verb: "Press", target: label, targetIsTag: true };
    case "Wait":
      return { verb: "Wait", target: label, targetIsTag: true };
    default:
      return { verb: kind, target: label, targetIsTag: false };
  }
}

export function formatElapsed(millis: number): string {
  const safe = Math.max(0, Math.floor(millis));
  const totalSeconds = Math.floor(safe / 1000);
  const mm = Math.floor(totalSeconds / 60);
  const ss = totalSeconds % 60;
  const ms = safe % 1000;
  const pad2 = (n: number) => String(n).padStart(2, "0");
  const pad3 = (n: number) => String(n).padStart(3, "0");
  return `${pad2(mm)}:${pad2(ss)}.${pad3(ms)}`;
}

function renderTarget(target: string, isTag: boolean) {
  if (!target) {
    return null;
  }
  if (isTag) {
    return <span className="action-list-target action-list-target-tag">{`<${target}/>`}</span>;
  }
  return <span className="action-list-target">{target}</span>;
}

function contentTextForStep(step: Step): string {
  const action = step.action;
  if (!action) {
    return "";
  }
  if (typeof action.text === "string") {
    return action.text;
  }
  return "";
}

function positionTextForStep(step: Step): string | null {
  const action = step.action;
  if (!action) {
    return null;
  }
  if (action.tapPoint) {
    return `${action.tapPoint.x.toFixed(1)}, ${action.tapPoint.y.toFixed(1)}`;
  }
  if (typeof action.x === "number" && typeof action.y === "number") {
    return `${action.x.toFixed(1)}, ${action.y.toFixed(1)}`;
  }
  if (
    typeof action.from_x === "number" &&
    typeof action.from_y === "number"
  ) {
    return `${action.from_x.toFixed(1)}, ${action.from_y.toFixed(1)}`;
  }
  return null;
}

export default function ActionList({
  steps,
  selectedIndex,
  onSelect,
  runStartMillis,
  selectedStep,
}: ActionListProps) {
  const itemRefs = useRef<Map<number, HTMLLIElement>>(new Map());
  const baseMillis =
    runStartMillis ?? (steps[0] ? new Date(steps[0].timestamp).getTime() : 0);

  useEffect(() => {
    const node = itemRefs.current.get(selectedIndex);
    if (node && typeof node.scrollIntoView === "function") {
      node.scrollIntoView({ block: "nearest" });
    }
  }, [selectedIndex]);

  const focusIndex = (index: number) => {
    const node = itemRefs.current.get(index);
    if (node && typeof node.focus === "function") {
      node.focus();
    }
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLLIElement>, index: number) => {
    const position = steps.findIndex((entry) => entry.index === index);
    switch (event.key) {
      case "Enter":
      case " ":
        event.preventDefault();
        onSelect(index);
        return;
      case "ArrowDown": {
        if (position < 0 || position >= steps.length - 1) return;
        event.preventDefault();
        const next = steps[position + 1].index;
        onSelect(next);
        focusIndex(next);
        return;
      }
      case "ArrowUp": {
        if (position <= 0) return;
        event.preventDefault();
        const prev = steps[position - 1].index;
        onSelect(prev);
        focusIndex(prev);
        return;
      }
      case "Home": {
        event.preventDefault();
        const first = steps[0]?.index;
        if (first !== undefined) {
          onSelect(first);
          focusIndex(first);
        }
        return;
      }
      case "End": {
        event.preventDefault();
        const last = steps[steps.length - 1]?.index;
        if (last !== undefined) {
          onSelect(last);
          focusIndex(last);
        }
        return;
      }
    }
  };

  return (
    <ol className="action-list" role="listbox" aria-label="Steps">
      {steps.map((step) => {
        const isActive = step.index === selectedIndex;
        const { verb, target, targetIsTag } = formatActionRow(step);
        const elapsedMillis = new Date(step.timestamp).getTime() - baseMillis;
        const elapsed = formatElapsed(elapsedMillis);
        const spokenTarget = target
          ? targetIsTag
            ? `<${target}/>`
            : target
          : "";
        const ariaLabel = spokenTarget
          ? `Step ${step.index} ${verb} ${spokenTarget}`
          : `Step ${step.index} ${verb}`;
        const showDetails = isActive && selectedStep && selectedStep.step === step.index;
        const positionText = showDetails ? positionTextForStep(selectedStep) : null;
        const contentText = showDetails ? contentTextForStep(selectedStep) : "";
        return (
          <li
            key={step.index}
            ref={(node) => {
              if (node) {
                itemRefs.current.set(step.index, node);
              } else {
                itemRefs.current.delete(step.index);
              }
            }}
            className="action-list-item"
            role="option"
            tabIndex={isActive ? 0 : -1}
            aria-selected={isActive}
            aria-label={ariaLabel}
            data-active={isActive ? "true" : "false"}
            data-violations={step.has_violations ? "true" : "false"}
            data-exceptions={step.has_exceptions ? "true" : "false"}
            onClick={() => onSelect(step.index)}
            onKeyDown={(event) => handleKeyDown(event, step.index)}
            title={target || undefined}
          >
            <div className="action-list-row">
              <span className="action-list-index">{step.index}.</span>
              <span className="action-list-body">
                <span className="action-list-verb">{verb}</span>
                {renderTarget(target, targetIsTag)}
              </span>
              <span className="action-list-markers">
                {step.has_exceptions ? (
                  <span
                    className="action-list-marker-exception"
                    aria-label="exceptions"
                    role="img"
                  />
                ) : null}
                {step.has_violations ? (
                  <span
                    className="action-list-marker-violation"
                    aria-label="violations"
                    role="img"
                  />
                ) : null}
              </span>
              <span className="action-list-elapsed">{elapsed}</span>
            </div>
            {showDetails ? (
              <div className="action-list-details">
                <div className="action-list-detail-row">
                  <span className="action-list-detail-label">Position</span>
                  <span className="action-list-detail-value">
                    {positionText ?? ""}
                  </span>
                </div>
                <div className="action-list-detail-row">
                  <span className="action-list-detail-label">Content</span>
                  <span className="action-list-detail-value">
                    {contentText ? contentText : `""`}
                  </span>
                </div>
              </div>
            ) : null}
          </li>
        );
      })}
    </ol>
  );
}
