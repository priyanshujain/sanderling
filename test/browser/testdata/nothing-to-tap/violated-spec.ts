import { always, extract, taps } from "@sanderling/spec";

const banner = extract((s) => s.ax.find({ id: "banner" })?.text ?? "").named("banner");

const bannerIsEmpty = always(() => banner.current === "");

export const properties = { bannerIsEmpty };

export const actionsRoot = taps;
