import selectorKeysContract from "../../../pkg/spec/test/fixtures/selector-keys.json";
import type { StepSummary } from "../types";

export interface FormattedRow {
  verb: string;
  target: string;
  targetIsTag: boolean;
}

// Imported from the cross-runtime contract rather than restated, because a
// copy here goes stale silently: an unlisted key renders a real action as raw
// text instead of a formatted row, and nothing fails to tell anyone.
export const SELECTOR_KEYS: readonly string[] = selectorKeysContract.keys;

export function parseSelector(
  selector: string,
): { kind: string; value: string } | null {
  const colonIndex = selector.indexOf(":");
  if (colonIndex <= 0) {
    return null;
  }
  const kind = selector.slice(0, colonIndex);
  const value = selector.slice(colonIndex + 1);
  if (!SELECTOR_KEYS.includes(kind)) {
    return null;
  }
  return { kind, value };
}

export function tagFromSelector(selector: string): string {
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
