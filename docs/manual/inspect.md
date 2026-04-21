---
title: sanderling inspect
---

# sanderling inspect

Local web UI for exploring runs produced by `sanderling test`. Reads `runs/<id>/meta.json` and `runs/<id>/trace.jsonl` from disk.

```
sanderling inspect [run-or-runs-dir] [--port N] [--no-open] [--dev]
```

The positional argument can be a runs directory or a single run directory (auto-detected by `meta.json`). Defaults to `./runs`.

![sanderling inspect](../_assets/inspect-ui.png)

## Keyboard shortcuts

| Key | Action |
|---|---|
| `j`, `Right` | Next step |
| `k`, `Left` | Previous step |
| `Shift+j`, `Shift+Right` | Jump 10 forward |
| `Shift+k`, `Shift+Left` | Jump 10 back |
| `g` / `G` | First / last step |
| `.` | Next step with a violation |

Arrow keys inside a tablist or listbox yield to those widgets. Use `j`/`k` when focus is on one.

## Deep links

`/runs/:id/steps/:n` links to a specific step. Use it in issues or PRs when pointing at a violation.

The run index auto-refreshes over SSE as new runs land, so `sanderling inspect` and `sanderling test` can run side by side.

## Development

Two-process loop while iterating on the UI:

```
make web-dev      # bun + vite on http://127.0.0.1:5173
make inspect-dev  # sanderling inspect --dev, proxies non-API to 5173
```

Single binary with the bundle embedded:

```
make sanderling
```
