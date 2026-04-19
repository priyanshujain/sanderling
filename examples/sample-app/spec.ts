import {
  InputText,
  Tap,
  actions,
  always,
  eventually,
  extract,
  from,
  next,
  now,
  pressKey,
  swipes,
  taps,
  waitOnce,
  weighted,
} from "@uatu/spec";
import { noUncaughtExceptions } from "@uatu/spec/defaults/properties";

// ── Snapshot extractors (fed by SampleApplication.kt) ──────────
const loggedIn = extract<boolean>(
  (state) => (state.snapshots.logged_in as boolean) ?? false,
);
const route = extract<string>(
  (state) => (state.snapshots.route as string) ?? "",
);
const accountCount = extract<number>(
  (state) => (state.snapshots.account_count as number) ?? 0,
);

// ── UI elements ────────────────────────────────────────────────
const phoneField = extract((state) => state.ax.find("desc:phone_field"));
const continueButton = extract((state) => state.ax.find("text:Continue"));
const addAccountButton = extract((state) => state.ax.find("text:Add account"));
const nameField = extract((state) => state.ax.find("desc:account_name"));
const createButton = extract((state) => state.ax.find("text:Create"));

// ── Properties ─────────────────────────────────────────────────
// accountCountNonNegative: the trivial safety property.
const accountCountNonNegative = always(() => accountCount.current >= 0);

// addAccountAdvances: once we land on add-account, the next step must be on
// a different screen. Exercises now(x).implies(next(y)).
const addAccountAdvances = always(
  now(() => route.current === "add-account").implies(
    next(() => route.current !== "add-account"),
  ),
);

// eventuallyLoggedIn: within 30 seconds of the run starting, we expect to
// reach home. Exercises eventually(p).within(n, unit).
const eventuallyLoggedIn = eventually(() => loggedIn.current).within(
  30,
  "seconds",
);

export const properties = {
  accountCountNonNegative,
  addAccountAdvances,
  eventuallyLoggedIn,
  noUncaughtExceptions,
};

// ── Actions ────────────────────────────────────────────────────
// Sampling: random phone numbers for the login screen.
const phoneSampler = from(["+919876543210", "+15555550100", "+442071234567"]);

const typePhone = actions(() => {
  const field = phoneField.current;
  if (!field) return [];
  return [InputText({ into: field, text: phoneSampler.generate() })];
});

const tapContinue = actions(() =>
  continueButton.current ? [Tap({ on: continueButton.current })] : [],
);
const tapAddAccount = actions(() =>
  addAccountButton.current ? [Tap({ on: addAccountButton.current })] : [],
);

const nameSampler = from(["Alice", "Bob", "Charlie", "Dana"]);
const fillName = actions(() => {
  const field = nameField.current;
  if (!field) return [];
  return [InputText({ into: field, text: nameSampler.generate() })];
});
const tapCreate = actions(() =>
  createButton.current ? [Tap({ on: createButton.current })] : [],
);

export const actionsRoot = weighted(
  [30, typePhone],
  [30, tapContinue],
  [20, tapAddAccount],
  [20, fillName],
  [20, tapCreate],
  [10, taps],
  [5, swipes],
  [5, waitOnce],
  [5, pressKey],
);

(globalThis as { actions?: unknown; properties?: unknown }).actions = actionsRoot;
(globalThis as { properties?: unknown }).properties = properties;
