---
title: Development
---

# Development

- [Design principles](./design-principles/)
- [Architecture](./architecture/)
- [Driver history](./driver-history/): how the driver layer got its current shape, and the bugs that forced each move.
- [CI](./ci/)
- v0.1.0 scope: [milestone #1](https://github.com/priyanshujain/sanderling/milestone/1)

## Building the docs site locally

```
make docs
```

Outputs to `build/site/`. Requires [pandoc](https://pandoc.org/) on your PATH. Preview with:

```
cd build/site && python3 -m http.server 8000
```
