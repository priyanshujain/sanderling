// Sanderling fuzzing sanderling's own replay UI.
//
// The properties here are cross-panel agreements: two panels that derive the
// same fact by different paths have to say the same thing. That holds for any
// trace, so this spec never needs recalibrating when the fixture run changes.
//
// The hooks it drives (data-testid, data-step, ...) were added to the UI for
// this spec. Needing them is the lesson: a UI with no stable handles is a UI
// nothing can assert on, and that is as true for a person writing a test as it
// is for a fuzzer.

import { type AccessibilityElement, type Key, PressKey, Tap, actions, always, extract, from, next, weighted } from "@sanderling/spec";
import { defaultActions, noUncaughtExceptions } from "@sanderling/spec/defaults";

function numberOf(value: string | undefined): number | null {
  if (value === undefined || value === "") return null;
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : null;
}

function dataOf(element: AccessibilityElement | undefined, key: string): string | undefined {
  const attrs = (element as unknown as { attrs?: Record<string, string> } | undefined)?.attrs;
  return attrs ? attrs[key] : undefined;
}

// The toolbar's own claim about which step is on screen, and how many there are.
const toolbar = extract("toolbar", (s) => {
  const indicator = s.ax.find({ "data-testid": "step-indicator" });
  if (!indicator) return null;
  return {
    step: numberOf(dataOf(indicator, "step")),
    stepCount: numberOf(dataOf(indicator, "stepCount")),
  };
});

// The step list's claim: which rows exist, which one is selected, which ones
// carry a violation marker.
const stepRows = extract("stepRows", (s) =>
  s.ax.findAll({ "data-testid": "step-row" }).map((row) => ({
    step: numberOf(dataOf(row, "step")),
    active: dataOf(row, "active") === "true",
    violating: dataOf(row, "violations") === "true",
  })),
);

// The screenshot panels' claim, read off the image URL each one is rendering.
// The "state before" panel comes first in the DOM and shows the selected step;
// "state after" shows the next one.
const screenshotSteps = extract("screenshotSteps", (s) =>
  s.ax.findAll({ "data-testid": "screenshot" }).map((image) => numberOf(dataOf(image, "step"))),
);

// Which tab is selected in each tab strip, as one comparable string.
const activeTabs = extract("activeTabs", (s) =>
  s.ax
    .findAll({ "data-testid": "tab" })
    .filter((tab) => dataOf(tab, "active") === "true")
    .map((tab) => dataOf(tab, "tabId") ?? "")
    .join(","),
);

// Two counts of one fact: the tab badge counts the step's violation records,
// the panel counts the property rows it renders as violated.
const violationBadges = extract("violationBadges", (s) =>
  s.ax.findAll({ "data-testid": "violations-badge" }).map((badge) => numberOf(badge.text)),
);
const violationPanelCounts = extract("violationPanelCounts", (s) =>
  s.ax
    .findAll({ "data-testid": "violations-panel" })
    .map((panel) => numberOf(dataOf(panel, "violationCount"))),
);

// The step in the URL is user input: it survives a reload, a typo, and every
// tap that navigates. It must always land inside the run.
const selectedStepIsInRange = always(() => {
  const current = toolbar.current;
  if (!current || current.step === null || current.stepCount === null) return true;
  return current.step >= 1 && current.step <= current.stepCount;
});

// Exactly one row is highlighted whenever the list is on screen. Zero means the
// toolbar is showing a step the list has no row for, which is what an
// off-by-one or a failed clamp looks like from the list's side.
const exactlyOneStepIsSelected = always(() => {
  const rows = stepRows.current;
  if (rows.length === 0) return true;
  return rows.filter((row) => row.active).length === 1;
});

// The step count the toolbar prints and the number of rows the list renders are
// two readings of the same run.
const stepCountMatchesTheList = always(() => {
  const current = toolbar.current;
  const rows = stepRows.current;
  if (!current || current.stepCount === null || rows.length === 0) return true;
  return current.stepCount === rows.length;
});

// The screenshot panel builds its URL from the loaded run's step; the toolbar
// prints the step from the URL. They must name the same step.
const screenshotShowsTheSelectedStep = always(() => {
  const current = toolbar.current;
  const shown = screenshotSteps.current;
  if (!current || current.step === null || shown.length === 0) return true;
  const before = shown[0];
  return before === null || before === current.step;
});

// Switching a tab is a view change, not a navigation: it must never move the
// run to another step.
const switchingTabsKeepsTheStep = always(
  next(() => {
    const previousTabs = activeTabs.previous;
    const previousToolbar = toolbar.previous;
    const currentToolbar = toolbar.current;
    if (previousTabs === undefined || previousTabs === activeTabs.current) return true;
    if (!previousToolbar || !currentToolbar) return true;
    return previousToolbar.step === currentToolbar.step;
  }),
);

// A badge that counts violations the panel cannot show a row for is a violation
// the operator can see a number for and never read.
const badgeCountMatchesThePanel = always(() => {
  const badges = violationBadges.current;
  const panels = violationPanelCounts.current;
  if (badges.length === 0 || panels.length === 0) return true;
  const badge = badges[0];
  if (badge === null) return true;
  return panels.every((count) => count === null || count === badge);
});

export const properties = {
  noUncaughtExceptions,
  selectedStepIsInRange,
  exactlyOneStepIsSelected,
  stepCountMatchesTheList,
  screenshotShowsTheSelectedStep,
  switchingTabsKeepsTheStep,
  badgeCountMatchesThePanel,
};

// Step rows are <li role="option">, which is not in the tappable selector set
// the default enumeration draws from, so selecting a step needs its own action.
const rowElements = extract("rowElements", (s) => s.ax.findAll({ "data-testid": "step-row" }));

const selectAStep = actions(() => {
  const rows = rowElements.current;
  return rows.length === 0 ? [] : [Tap({ on: from(rows).generate() })];
});

// Left/right are the UI's own prev/next step bindings, and the one path into
// the step navigation that does not go through a tap.
const arrowKeys = from<Key>(["left", "right"]);
const navigateByKeyboard = actions(() => [PressKey({ key: arrowKeys.generate() })]);

// defaultActions carries the rest: the tab strips, the jump-to-violation
// button, the theme toggle, the link back to the run list, and the scrolling.
export const actionsRoot = weighted([35, selectAStep], [25, navigateByKeyboard], [40, defaultActions]);
