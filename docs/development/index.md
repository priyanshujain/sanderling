---
title: Development
---

# Development

- [Design principles](./design-principles/)
- [Architecture](./architecture/)
- v0.1.0 scope: [issue #4](https://github.com/priyanshujain/sanderling/issues/4)

## Building the docs site locally

```
make docs
```

Outputs to `build/site/`. Requires [pandoc](https://pandoc.org/) on your PATH. Preview with:

```
cd build/site && python3 -m http.server 8000
```
