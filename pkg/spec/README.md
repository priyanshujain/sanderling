# @sanderling/spec

TypeScript spec API for [sanderling](https://github.com/priyanshujain/sanderling), a property-based UI fuzzer for mobile and web apps.

Spec authors write properties (what the app must always or eventually do), extractors (structured state from the UI), and action generators (what sanderling is allowed to do). The `sanderling` CLI evaluates the spec in a loop against a running app.

## Install

```sh
npm install --save-dev @sanderling/spec
```

## Usage

```ts
import { extract, always, eventually, now, actions, weighted, taps, swipes, InputText, Tap } from "@sanderling/spec";

const loggedIn = extract((s) => !!s.ax.find("id:home-tab-bar"));
const balance = extract<number>((s) => (s.snapshots.balance as number) ?? 0);

export const properties = {
  balanceNeverNegative: always(() => balance.current >= 0),
  loginSucceeds: eventually(() => loggedIn.current).within(30, "seconds"),
};

const doLogin = actions(() => {
  if (loggedIn.current) return [];
  const email = state.ax.find("id:email-field");
  const submit = state.ax.find("id:sign-in-button");
  if (!email || !submit) return [];
  return [InputText({ into: email, text: "test@example.com" }), Tap({ on: submit })];
});

export const actions = weighted(
  [50, doLogin],
  [10, taps],
  [2,  swipes],
);
```

Works identically across Android, iOS, and web targets.
