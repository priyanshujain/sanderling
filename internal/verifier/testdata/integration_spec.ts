import {
  InputText,
  Tap,
  actions,
  always,
  eventually,
  extract,
  weighted,
} from "@uatu/spec";
import { noUncaughtExceptions } from "@uatu/spec/defaults/properties";

const route = extract<string>(
  (state) => (state.snapshots.route as string) ?? "",
);
const itemCount = extract<number>(
  (state) => (state.snapshots.item_count as number) ?? 0,
);
const hasSubmitted = extract<boolean>(
  (state) => (state.snapshots.has_submitted as boolean) ?? false,
);

const primaryAction = extract((state) => state.ax.find("desc:primary_action"));
const secondaryAction = extract((state) =>
  state.ax.find("desc:secondary_action"),
);
const textField = extract((state) => state.ax.find("desc:text_field"));

const itemCountNonNegative = always(() => itemCount.current >= 0);
const routeIsKnown = always(
  () => route.current === "list" || route.current === "form",
);
const submitEventually = eventually(() => hasSubmitted.current).within(
  30,
  "seconds",
);

const typeIntoField = actions(() => {
  if (route.current !== "form") return [];
  const field = textField.current;
  if (!field) return [];
  return [InputText({ into: field, text: "hello" })];
});

const tapPrimary = actions(() => {
  const button = primaryAction.current;
  return button ? [Tap({ on: button })] : [];
});

const tapSecondary = actions(() => {
  const button = secondaryAction.current;
  return button ? [Tap({ on: button })] : [];
});

export const properties = {
  itemCountNonNegative,
  routeIsKnown,
  submitEventually,
  noUncaughtExceptions,
};

export const actionsRoot = weighted(
  [40, typeIntoField],
  [30, tapPrimary],
  [30, tapSecondary],
);

(globalThis as { actions?: unknown; properties?: unknown }).actions = actionsRoot;
(globalThis as { properties?: unknown }).properties = properties;
