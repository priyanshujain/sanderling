import { always } from "../ltl.ts";
import { extract } from "../extract.ts";
import type { Formula } from "../types.ts";

const exceptionCount = extract<number>((state) => state.exceptions.length);

// Fails when the SDK captured an uncaught throwable or a Uatu.reportError
// call surfaced one during the run.
export const noUncaughtExceptions: Formula = always(
  () => exceptionCount.current === 0,
);

const errorLogCount = extract<number>(
  (state) => state.logs.reduce((count, log) => count + (log.level === "E" ? 1 : 0), 0),
);

// Fails when the runner's logcat fetch observed any error-level lines since
// the previous step.
export const noLogcatErrors: Formula = always(
  () => errorLogCount.current === 0,
);
