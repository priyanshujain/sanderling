import { useEffect, useRef } from "react";
import type { KeyboardEvent } from "react";
import type { StepSummary } from "../types";
import "./ActionList.css";

export interface ActionListProps {
  steps: StepSummary[];
  selectedIndex: number;
  onSelect: (index: number) => void;
}

export default function ActionList({ steps, selectedIndex, onSelect }: ActionListProps) {
  const itemRefs = useRef<Map<number, HTMLLIElement>>(new Map());

  useEffect(() => {
    const node = itemRefs.current.get(selectedIndex);
    if (node && typeof node.scrollIntoView === "function") {
      node.scrollIntoView({ block: "nearest" });
    }
  }, [selectedIndex]);

  const handleKeyDown = (event: KeyboardEvent<HTMLLIElement>, index: number) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      onSelect(index);
    }
  };

  return (
    <ol className="action-list">
      {steps.map((step) => {
        const isActive = step.index === selectedIndex;
        const kindLabel = step.action_kind ?? "observe";
        const detailLabel = step.action_label ?? (step.screen ? `@ ${step.screen}` : "");
        const ariaLabel = detailLabel
          ? `Step ${step.index} ${kindLabel} ${detailLabel}`
          : `Step ${step.index} ${kindLabel}`;
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
            role="button"
            tabIndex={0}
            aria-label={ariaLabel}
            data-active={isActive ? "true" : "false"}
            data-violations={step.has_violations ? "true" : "false"}
            data-exceptions={step.has_exceptions ? "true" : "false"}
            onClick={() => onSelect(step.index)}
            onKeyDown={(event) => handleKeyDown(event, step.index)}
            title={detailLabel || undefined}
          >
            <span className="action-list-index">{step.index}</span>
            <span className="action-list-kind">{kindLabel}</span>
            <span className="action-list-detail">{detailLabel}</span>
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
          </li>
        );
      })}
    </ol>
  );
}
