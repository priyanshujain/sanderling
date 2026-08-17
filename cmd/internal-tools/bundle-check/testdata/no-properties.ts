import { Tap, actions } from "@sanderling/spec";

export const properties = {};
export const actionsRoot = actions((state) => {
  const button = state.ax.find("desc:primary");
  return button ? [Tap({ on: button })] : [];
});
