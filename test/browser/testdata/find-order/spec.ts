import { always, extract, taps } from "@sanderling/spec";

// Which of the two #x a selector means is the whole fixture. The reading is
// compared between the two hosts by TestBrowserAxFindAgreesAcrossHosts, which
// drives each host's production path and never asks one what the other said.
//
// Written in the object form on purpose. It used to reach the goja host as a
// plain attribute filter looking for an `id` attribute a dump never carries
// (the web dump files the DOM id under resource-id), so it resolved nothing
// there while the V8 host resolved it against the live DOM.
const found = extract((s) => s.ax.find({ id: "x" })?.text).named("found");

const findsTheShadowMatch = always(() => found.current === "shadow");

export const properties = { findsTheShadowMatch };

export const actionsRoot = taps;
