# sanderling

Autonomous property-based testing for mobile and web apps.

You write rules that must always hold about your app. sanderling explores the app on its own for minutes or hours, performing thousands of taps, swipes, and inputs, and records every step where a rule breaks. No scripted test paths. One TypeScript spec runs against Android, iOS, and web builds of the same app.

```ts
import { extract, always } from "@sanderling/spec";
import { defaultActions } from "@sanderling/spec/defaults";
import { noUncaughtExceptions } from "@sanderling/spec/defaults/properties";

const balance = extract("balance", s =>
  parseInt(s.ax.find({ testTag: "Balance" })?.text ?? "0", 10));

export const properties = {
  noUncaughtExceptions,
  balanceNeverNegative: always(() => balance.current >= 0),
};

export const actionsRoot = defaultActions;
```

Every run produces a trace: one JSON line and one screenshot per step. `sanderling replay` opens it in a web UI for stepping through actions, screenshots, property timelines, and violations.

> Alpha. Android, iOS, and web (Chrome driver only). Full scope in the [v0.1.0 roadmap](https://github.com/priyanshujain/sanderling/milestone/1).

## Docs

- [Introduction](https://priyanshujain.github.io/sanderling/manual/introduction/): what property-based testing is and how sanderling works
- [Case study: Folio](https://priyanshujain.github.io/sanderling/manual/case-study/): sanderling finding a real bug in a mobile app
- [Getting started](https://priyanshujain.github.io/sanderling/manual/getting-started/): install the CLI and run it against Folio
- [Spec language reference](https://priyanshujain.github.io/sanderling/manual/spec-language/)
- Examples: [folio](https://github.com/priyanshujain/sanderling/tree/master/examples/folio) (KMP, Android/iOS/web), [folio-web](https://github.com/priyanshujain/sanderling/tree/master/examples/folio-web) (React + Vite)
- [Architecture](https://priyanshujain.github.io/sanderling/development/architecture/) for contributors

---

<img src="docs/_assets/sanderling.jpeg" alt="sanderling" width="420" />

> sanderling, a wading bird that probes the shoreline for bugs that lie beneath.
