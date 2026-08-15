# @sanderling/spec

TypeScript spec API for [sanderling](https://github.com/priyanshujain/sanderling), a property-based UI fuzzer for Android, iOS and web apps.

A spec exports properties (what the app must always or eventually do), extractors (structured state read off the UI), and action generators (what sanderling is allowed to do). The `sanderling` CLI evaluates the spec against a running app once per step.

```sh
npm install --save-dev @sanderling/spec
```

[Getting started](https://priyanshujain.github.io/sanderling/manual/getting-started/) installs the CLI and runs a first spec. The [spec language reference](https://priyanshujain.github.io/sanderling/manual/spec-language/) lists every primitive, and the [case study](https://priyanshujain.github.io/sanderling/manual/case-study/) walks a complete spec end to end.

The CLI bundles this package's TypeScript sources at run time, so keep the CLI and the package on the same release.
