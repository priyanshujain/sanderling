# @uatu/spec

TypeScript spec API for [uatu](https://github.com/priyanshujain/uatu) — a property-based UI fuzzer for mobile apps.

Spec authors write specs in TypeScript that describe what an app should *always* do (safety invariants), generate weighted actions to exercise the app, and extract structured state from the accessibility tree. The `uatu` CLI picks up the spec and drives the app under test.

## Install

```sh
npm install --save-dev @uatu/spec
```

## Usage

```ts
import { extract, always, actions, Tap, weighted } from "@uatu/spec";

export const spec = {
  extract: extract((tree) => ({
    onHomeScreen: tree.some((n) => n.text === "Home"),
  })),

  always: always(({ state }) => state.onHomeScreen || !state.startedOnHome),

  actions: actions(({ tree }) =>
    weighted([
      [1, Tap(tree.first((n) => n.text === "Checkout"))],
    ]),
  ),
};
```

## Version compatibility

`@uatu/spec` is released in lockstep with the uatu CLI. Pin the same major/minor version as your installed `uatu` binary.

## License

Apache-2.0
