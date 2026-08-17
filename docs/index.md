---
title: Sanderling Manual
---

# Sanderling Manual

sanderling tests mobile and web apps by exploring them on its own and checking rules you write. You state properties that must always hold ("a new account starts at zero", "the app never throws"). sanderling drives the app for minutes or hours through thousands of taps, swipes, and inputs, and records every step where a property breaks. One TypeScript spec runs against Android, iOS, and web builds of the same app.

Alpha: the scope of v0.1.0 is tracked in the [v0.1.0 milestone](https://github.com/priyanshujain/sanderling/milestone/1).

## Manual

- [Introduction](./manual/introduction/): what property-based testing is and how sanderling works.
- [Case study: Folio](./manual/case-study/): sanderling finding a real bug in a mobile app, and how the spec is written.
- [Getting started](./manual/getting-started/): install the CLI and run it against Folio.
- [Spec language reference](./manual/spec-language/): every selector, operator, action, and sampler.
- [Runs](./manual/runs/): what happens during a run and why runs are long.
- [Replay](./manual/replay/): the trace browser.
- [CLI reference](./manual/cli/): every command and flag.

## Development

- [Architecture](./development/architecture/) and [design principles](./development/design-principles/) for contributors.
- [Driver history](./development/driver-history/): why the driver layer looks the way it does.

---

<img src="./_assets/sanderling.jpeg" alt="sanderling" width="420" />

> sanderling, a wading bird that probes the shoreline for bugs that lie beneath.
