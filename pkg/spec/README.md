# @sanderling/spec

TypeScript spec API for [sanderling](https://github.com/priyanshujain/sanderling), a property-based UI fuzzer for mobile and web apps.

Spec authors write properties (what the app must always or eventually do), extractors (structured state from the UI), and action generators (what sanderling is allowed to do). The `sanderling` CLI evaluates the spec in a loop against a running app.

## Install

```sh
npm install --save-dev @sanderling/spec
```

## Usage

```ts
import { extract, always, eventually, actions, weighted, taps, swipes, InputText, Tap } from "@sanderling/spec";

const loggedIn = extract((s) => !!s.ax.find("id:home-tab-bar"));
const balance = extract<number>((s) => (s.snapshots.balance as number) ?? 0);
const emailField = extract((s) => s.ax.find("id:email-field"));
const submitButton = extract((s) => s.ax.find("id:sign-in-button"));

export const properties = {
  balanceNeverNegative: always(() => balance.current >= 0),
  loginSucceeds: eventually(() => loggedIn.current).within(30, "seconds"),
};

const doLogin = actions(() => {
  if (loggedIn.current) return [];
  const email = emailField.current;
  const submit = submitButton.current;
  if (!email || !submit) return [];
  return [InputText({ into: email, text: "test@example.com" }), Tap({ on: submit })];
});

export const actionsRoot = weighted(
  [50, doLogin],
  [10, taps],
  [2,  swipes],
);
```

## Setup actions

Some action generators are not fuzz targets but preconditions: they drive the
app from a fresh state into the surface you actually want to fuzz (login,
onboarding, permission grants, seed data). Export them as `setup` instead of
mixing them into `actionsRoot`. The runner tries `setup` first; if it yields
no action, it falls through to `actionsRoot`. State regressing back across the
precondition (e.g. logout under fuzz) automatically re-engages setup.

```ts
const login = actions(() => {
  if (loggedIn.current) return [];
  return [InputText({ into: emailField.current!, text: "demo@app.test" }), Tap({ on: submitButton.current! })];
});

export const setup = login;
export const actionsRoot = weighted([60, browse], [40, edit]);

(globalThis as { setup?: unknown }).setup = setup;
```

Setup is just an `ActionGenerator`; compose with `actions`, `weighted`, or
`whenRoute` exactly like the main pool.

Works identically across Android, iOS, and web targets.
