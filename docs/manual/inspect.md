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

The detail page is a four-region grid: actions on the left, state-before and state-after side by side in the middle, metrics along the bottom.

- **Actions** (left): vertical step list with action, target, and elapsed time. Steps with violations get a red dot; steps with exceptions get a dashed-outline marker. Arrow keys move within the list (WAI-ARIA listbox).
- **State before** (center): the state the runner observed before dispatching this step's action. Four tabs:
    - *Screenshot*: device screenshot with the resolved tap target overlaid (red rectangle + outlined tap point); swipes show an arrow from start to end.
    - *Snapshots*: snapshot values flattened into dotted-path rows. Values that changed since the previous step are highlighted; hover for the previous value.
    - *Properties*: one row per property with status (violated / pending / holds) and an expandable residual formula.
    - *Violations*: same as Properties but filtered to violated rows only. The tab badge shows the violation count.
- **State after** (right): the state observed after the action landed. Same four tabs.
- **Metrics** (bottom): HEAP and CPU lanes with a STEPS lane for step-by-step navigation. Click any point or step to seek. Exceptions for the current step render inline below the chart.

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

Arrow keys inside a tablist, listbox, or menu yield to those widgets (so arrow-left/right cycles tabs, arrow-up/down moves within a list). Use `j`/`k` for step navigation when the focus is inside one of those.

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
