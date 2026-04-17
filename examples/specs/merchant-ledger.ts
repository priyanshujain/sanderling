import {
  extract,
  always,
  actions,
  weighted,
  Tap,
  InputText,
  taps,
  swipes,
} from "@uatu/spec";
import type { State } from "@uatu/spec";

// ── Snapshot extractors ────────────────────────────────────────
const screen = extract<string>((state) => (state.snapshots.screen as string) ?? "");
const balance = extract<number>(
  (state) => (state.snapshots["ledger.balance"] as number) ?? 0,
);
const totalGiven = extract<number>(
  (state) => (state.snapshots["ledger.totalGiven"] as number) ?? 0,
);
const totalReceived = extract<number>(
  (state) => (state.snapshots["ledger.totalReceived"] as number) ?? 0,
);

// ── Screen detection via accessibility tree ────────────────────
const onLoginPhone = extract<boolean>((state) =>
  Boolean(state.ax.find("id:login_phone_field")),
);
const onLoginOtp = extract<boolean>((state) =>
  Boolean(state.ax.find("id:login_otp_field")),
);
const onHome = extract<boolean>(
  (state) =>
    state.ax.findAll("id:home_customer_row").length > 0 ||
    state.ax.findAll("id:home_supplier_row").length > 0,
);
const onLedger = extract<boolean>(
  () => screen.current === "customer_ledger" || screen.current === "supplier_ledger",
);

// ── Properties ─────────────────────────────────────────────────
export const properties = {
  // The displayed balance is server-fed; the totals are local-DB-fed.
  // Any divergence — stale cache, partial sync, optimistic-update glitch,
  // mishandled deleted txn — is a violation.
  ledgerBalanceMatchesTxns: always(
    () =>
      !onLedger.current ||
      balance.current === totalGiven.current - totalReceived.current,
  ),
};

// ── Action generators (gated by screen state) ──────────────────
const enterPhone = actions(() =>
  onLoginPhone.current
    ? [
        InputText({
          into: "id:login_phone_field",
          text: process.env.UATU_TEST_PHONE ?? "",
        }),
        Tap({ on: "id:login_continue" }),
      ]
    : [],
);

const enterOtp = actions(() =>
  onLoginOtp.current
    ? [
        InputText({
          into: "id:login_otp_field",
          text: process.env.UATU_TEST_OTP ?? "",
        }),
      ]
    : [],
);

const openCustomerOrSupplier = actions((): ReturnType<typeof Tap>[] => {
  if (!onHome.current) return [];
  // The verifier resolves the selector; we just hand back a list of
  // candidate row taps weighted equally by the runtime's pickFromResult.
  return [
    Tap({ on: "id:home_customer_row" }),
    Tap({ on: "id:home_supplier_row" }),
  ];
});

export const actionsRoot = weighted(
  [100, enterPhone],
  [100, enterOtp],
  [80, openCustomerOrSupplier],
  [10, taps],
  [2, swipes],
);

// Verifier looks for `globalThis.actions`; re-export the weighted root.
(globalThis as { actions?: unknown }).actions = actionsRoot;
