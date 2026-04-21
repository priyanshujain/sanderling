---
title: sanderling inspect
---

# sanderling inspect

`sanderling inspect` is a local web UI for exploring runs produced by `sanderling test`. It reads `runs/<id>/meta.json` and `runs/<id>/trace.jsonl` and renders each step with its action, screenshot, snapshots, residual formulas, and exceptions.

```
sanderling inspect [run-or-runs-dir] [--port N] [--no-open] [--dev]
```

The positional argument can be either a runs directory or a single run directory (auto-detected by the presence of `meta.json`). When omitted, it defaults to `./runs`.

## Layout

The detail page uses a phone-dominant grid:

- **Actions** (left): vertical step list. Steps with violations are marked with a red dot; steps with exceptions have a dashed-outline marker.
- **Screenshot** (center): the device screenshot for the current step. The runner's resolved tap target is overlaid as a red rectangle, the tap point as an outlined circle. Swipes show an arrow from start to end.
- **Snapshots** (top right): the current step's snapshots flattened into dotted-path rows. Values that changed since the previous step are highlighted; hover to see the previous value.
- **Properties** (middle right): one row per property with status (violated / pending / holds) and an expandable residual formula.
- **Exceptions** (bottom right): SDK-captured uncaught throwables. Stack traces expand inline.
- **Timeline** (bottom): per-property swimlane across all steps; click a cell to seek.

## Keyboard shortcuts

| Key | Action |
|---|---|
| `j`, `Right` | Next step |
| `k`, `Left` | Previous step |
| `Shift+j`, `Shift+Right` | Jump 10 forward |
| `Shift+k`, `Shift+Left` | Jump 10 back |
| `g` | First step |
| `G` | Last step |
| `.` | Next step with a violation |

## URLs

- `/` — run index (auto-refreshes via SSE when new runs land)
- `/runs/:id` — redirects to step 1
- `/runs/:id/steps/:n` — direct deep link

## Theme

Defaults to the system color scheme via `prefers-color-scheme`. The `light`/`dark` button in the toolbar toggles a manual override stored in `localStorage`.

## Development

Two-process loop:

```
make web-dev      # bun + vite, http://127.0.0.1:5173
make inspect-dev  # sanderling inspect --dev, proxies non-API to 5173
```

For a single binary with embedded assets:

```
make sanderling         # builds web/dist, copies to internal/inspect/dist, then go build
```
