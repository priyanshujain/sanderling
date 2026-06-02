// Single source of truth for the data both action-generator engines draw from:
// the goja verifier (internal/verifier/worker.go) and the V8 web runtime
// (web-runtime.ts). The entries and their ORDER are load-bearing: both engines
// index into these arrays with the shared PCG, so any reordering shifts every
// downstream pick for a given seed and breaks cross-engine reproducibility.

// INPUT_CORPUS is the edge-case string pool the typing builtin draws from to
// stress field parsing: empty, single char, overflow length, unicode, blank /
// whitespace, numeric boundaries, and common injection payloads. It must stay
// byte-for-byte identical to worker.go inputCorpus.
export const INPUT_CORPUS: readonly string[] = [
  "",
  "a",
  "a".repeat(4096),
  "🙂🔥💸",
  "  ",
  "\t\n",
  "-1",
  "999999999999999999999",
  "0.0000001",
  "1e10",
  "'; DROP TABLE--",
  "<script>alert(1)</script>",
  "../../etc/passwd",
  "%s%n",
  "NaN",
];

// NATIVE_PRESS_KEYS is the key pool the native (goja) press-key builtin may
// emit. Exploration stays gentle: only "back" is in play today, because
// "home"/"menu" navigate away from the app under test. Kept as an array so the
// matrix can declare native support over the full set without the picker
// hardcoding a single value.
export const NATIVE_PRESS_KEYS: readonly string[] = ["back"];

// WEB_PRESS_KEYS is the key pool the web press-key builtin draws from. Only
// keys with meaningful in-page semantics are included; "back"/"home" would not
// navigate a browser tab the way they navigate a native app.
export const WEB_PRESS_KEYS: readonly string[] = [
  "enter",
  "tab",
  "escape",
  "up",
  "down",
  "left",
  "right",
];
