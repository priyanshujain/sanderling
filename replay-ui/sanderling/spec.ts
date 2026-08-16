// Sanderling fuzzing sanderling's own replay UI.
//
// Six of the seven properties here are cross-panel agreements: two panels that
// derive the same fact by different paths have to say the same thing. That
// holds for any trace, so this spec never needs recalibrating when the fixture
// run changes. The seventh is the stock noUncaughtExceptions.
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

// What the "state before" panel is showing, read off the image URL it renders.
// Scoped to that panel by name rather than by DOM position: an earlier draft
// took the first screenshot on the page, and the fuzzer put the before panel on
// another tab, which left the after panel's image first and fired the property
// against a UI that was behaving correctly.
const beforeScreenshotStep = extract("beforeScreenshotStep", (s) => {
  const image = s.ax.find([{ "data-testid": "state-before" }, { "data-testid": "screenshot" }]);
  return image ? numberOf(dataOf(image, "step")) : null;
});

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
  const shown = beforeScreenshotStep.current;
  if (!current || current.step === null || shown === null) return true;
  return shown === current.step;
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

// Step rows get their own weight for the same reason tabs do below: the default
// enumeration reaches them now that role="option" is in the tappable set, but
// one row out of the page's clickable elements is a thin chance, and both
// step-facing properties are vacuous on a run that never selects one.
const rowElements = extract("rowElements", (s) => s.ax.findAll({ "data-testid": "step-row" }));

const selectAStep = actions(() => {
  const rows = rowElements.current;
  return rows.length === 0 ? [] : [Tap({ on: from(rows).generate() })];
});

// Left/right are the UI's own prev/next step bindings, and the one path into
// the step navigation that does not go through a tap.
const arrowKeys = from<Key>(["left", "right"]);
const navigateByKeyboard = actions(() => [PressKey({ key: arrowKeys.generate() })]);

// Tabs get their own weight rather than being left to the undirected tap mix:
// with ~15 clickable elements on the page, an undirected run went 40 steps
// without switching a single tab, which left both tab-facing properties
// vacuously true. Weighting them is what makes those properties mean something.
const tabElements = extract("tabElements", (s) => s.ax.findAll({ "data-testid": "tab" }));

const switchATab = actions(() => {
  const tabs = tabElements.current;
  return tabs.length === 0 ? [] : [Tap({ on: from(tabs).generate() })];
});

// badgeCountMatchesThePanel needs two readings on ONE step: the badge, which a
// tab strip renders only for a step that HAS a violation, and a violations
// panel to compare it against, which exists while the properties or violations
// tab is selected. Undirected actions put both on the same step 0 times in the
// 80 of the first dogfood run: the property was reachable in principle and
// judged nothing in practice.
//
// Both halves have to be aimed at. Aiming at the step alone just moved the
// misses to the other side, 0 judged either way. So this selects a step the
// list marks as violating, and once standing on one, opens a panel if none is
// up. It opens the AFTER panel's, because the before panel's screenshot is what
// screenshotShowsTheSelectedStep reads and covering that up trades one
// property's evidence for another's.
const violatingRows = extract("violatingRows", (s) =>
  s.ax.findAll({ "data-testid": "step-row" }).filter((row) => dataOf(row, "violations") === "true"),
);
const afterPropertiesTabs = extract("afterPropertiesTabs", (s) =>
  s.ax
    .findAll([{ "data-testid": "state-after" }, { "data-testid": "tab" }])
    .filter((tab) => dataOf(tab, "tabId") === "properties"),
);

const showAViolatingStepWithItsPanel = actions(() => {
  const rows = violatingRows.current;
  if (!rows.some((row) => dataOf(row, "active") === "true")) {
    return rows.length === 0 ? [] : [Tap({ on: from(rows).generate() })];
  }
  if (violationPanelCounts.current.length > 0) return [];
  const tabs = afterPropertiesTabs.current;
  return tabs.length === 0 ? [] : [Tap({ on: from(tabs).generate() })];
});

// defaultActions carries the rest: the jump-to-violation button, the theme
// toggle, the link back to the run list, and the scrolling.
export const actionsRoot = weighted(
  [30, selectAStep],
  [20, navigateByKeyboard],
  [25, switchATab],
  [20, showAViolatingStepWithItsPanel],
  [25, defaultActions],
);
