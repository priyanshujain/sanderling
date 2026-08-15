import { always, extract, taps } from "@sanderling/spec";

// Which of the two #x a selector means is the whole fixture. The reading is
// compared between the two hosts by TestBrowserAxFindAgreesAcrossHosts, which
// drives each host's production path and never asks one what the other said.
//
// Written in the "k:v" string grammar because that is the one form both hosts
// resolve the same way: the object form {id: "x"} reaches the goja host as a
// plain attribute filter, and a web hierarchy dump carries the id under
// resource-id, so it matches nothing there.
const found = extract((s) => s.ax.find("id:x")?.text).named("found");

const findsTheShadowMatch = always(() => found.current === "shadow");

export const properties = { findsTheShadowMatch };

export const actionsRoot = taps;
