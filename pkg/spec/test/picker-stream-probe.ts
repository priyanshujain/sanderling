// The seeded picker, over a page that gets navigated out from under it.
//
// internal/driver/chrome/picker_stream_test.go bundles this through the
// production web bundler and asks for one action at a time. `taps` over a page
// whose targets never move draws exactly once per call, so the actions it hands
// back ARE the seed's draw stream, and a stream that restarts is visible in
// them.

import { taps } from "../src/actions.ts";

export const actionsRoot = taps;
