import { always, extract, taps } from "@sanderling/spec";

const banner = extract((s) => s.ax.find({ id: "banner" })?.text ?? "").named("banner");

const bannerStays = always(() => banner.current === "nothing here answers a tap");

export const properties = { bannerStays };

export const actionsRoot = taps;
