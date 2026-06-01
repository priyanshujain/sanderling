// Verb support matrix and a warn-once helper, shared by both engines.
//
// The matrix is the single declared decision about which builtin verbs each
// platform supports. The picker (pick.ts) consults it before drawing so an
// unsupported verb is surfaced once (via host.reportUnsupported) rather than
// silently producing a null action every tick.

import type { BuiltinVerb, Host } from "./action-tree.ts";

export type Platform = "android" | "ios" | "web";

// VERB_SUPPORT declares, per verb, the platforms that can execute it.
//   Tap/DoubleTap/InputText/Wait: native + web.
//   PressKey: native (full key set incl. back/home) + web (enter/tab/escape/arrows).
//   Swipe/LongPress/Scroll: native yes; web yes (the chrome driver supports
//     pointer drags / long-press / wheel), declared here rather than silently
//     no-op as the older web-runtime did.
const VERB_SUPPORT: Record<BuiltinVerb, ReadonlyArray<Platform>> = {
  taps: ["android", "ios", "web"],
  doubleTaps: ["android", "ios", "web"],
  typing: ["android", "ios", "web"],
  waitOnce: ["android", "ios", "web"],
  pressKeys: ["android", "ios", "web"],
  swipes: ["android", "ios", "web"],
  longPresses: ["android", "ios", "web"],
  scrolls: ["android", "ios", "web"],
};

// supports reports whether a verb is executable on a platform.
export function supports(verb: BuiltinVerb, platform: Platform): boolean {
  return VERB_SUPPORT[verb].includes(platform);
}

// A warn-once registry keyed by `${verb}@${platform}`. Module-level so the
// "warn at most once" guarantee holds across every picker walk in a run.
const warnedKeys = new Set<string>();

function warnKey(verb: BuiltinVerb, platform: Platform): string {
  return `${verb}@${platform}`;
}

// warnUnsupportedOnce calls host.reportUnsupported(verb) at most once per
// verb@platform for the lifetime of the process. Returns true on the first
// (reporting) call for a key, false on subsequent suppressed calls. The picker
// uses the boolean only for testability; behavior is the single report.
export function warnUnsupportedOnce(host: Host, verb: BuiltinVerb): boolean {
  const key = warnKey(verb, host.platform());
  if (warnedKeys.has(key)) return false;
  warnedKeys.add(key);
  host.reportUnsupported(verb);
  return true;
}

// resetWarnings clears the warn-once registry. Test-only: production never
// resets, so a verb is reported once per process.
export function resetWarnings(): void {
  warnedKeys.clear();
}
