# Folio (web)

A React + Vite port of the folio personal-ledger app: login with demo
credentials, create accounts, add credits and debits. Exists as the
minimal web example sanderling runs its property-based specs against.

## Stack

- React 19 + Vite + TypeScript
- Stable HTML `id` attributes on every interactive element so the spec
  targets DOM nodes by id, not by aria-label parsing
- `data-*` attributes (`data-cents`, `data-account-id`, `data-txn-count`)
  carry structured numeric state for property assertions

## Prerequisites

- `just`
- [`bun`](https://bun.sh) (installs the JS toolchain; the project uses
  `bun.lock`, not `package-lock.json` or `pnpm-lock.yaml`)
- Chrome installed (sanderling drives it via CDP)

```sh
bun install
```

## Demo credentials

```
email:    demo@ledger.app
password: ledger123
```

## Run a sanderling test

```sh
just test
```

The justfile boots the Vite dev server on port 5173, waits for the port
to respond, then runs `sanderling test --platform web --bundle-id
http://localhost:5173` against it. Override the port with `PORT=4000 just
test`; `DURATION`, `SEED`, and `OUTPUT` work the same way as in the KMP
folio example.

Traces land in `./sanderling/runs/<timestamp>/`.

## How it connects to sanderling

- The spec at `sanderling/spec.ts` finds elements by HTML `id`
  (`#email`, `#add-account`, `#ledger`, `#txn-amount`, ...). Anything
  the spec touches has a stable id in the markup.
- Numeric state the spec asserts on is exposed via `data-cents` and
  similar attributes. The spec reads `el.attrs["data-cents"]` rather
  than parsing currency strings out of `aria-label`.
- Account cards expose `data-account-id` + `data-balance` so the
  spec can correlate cards across steps without depending on DOM order.
- `just test` invokes `sanderling test --platform web`, which connects
  to Chrome via the Chrome DevTools Protocol; no extension or app SDK
  is involved.
