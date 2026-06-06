import { Tap, actions } from "@sanderling/spec";
import { noUncaughtExceptions } from "@sanderling/spec/defaults/properties";
import * as defaults from "@sanderling/spec/defaults";

const tapPrimary = actions((state) => {
  const button = state.ax.find("desc:primary");
  return button ? [Tap({ on: button })] : [];
});

export const properties = { noUncaughtExceptions };
export const actionsRoot = tapPrimary;
export const defaultsKeys = Object.keys(defaults);
