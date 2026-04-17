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

// ── Snapshot extractors (fed by UatuExtractors on Android) ─────
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

// ── Screen detection via uiautomator hierarchy ─────────────────
const onLanguageSelect = extract<boolean>((state) =>
  Boolean(state.ax.find("id:select_language")),
);
const englishTile = extract((state) =>
  state.ax.find("id:select_language") ? state.ax.find("text:English") : undefined,
);

const onEnterMobile = extract<boolean>((state) =>
  Boolean(state.ax.find("id:etMobileNumber")),
);
const mobileField = extract((state) => state.ax.find("id:etMobileNumber"));
const termsCheckbox = extract((state) => state.ax.find("id:checkBoxTerms"));
const continueButton = extract((state) => state.ax.find("id:buttonLogin"));

// OTP screen uses six single-digit EditTexts sharing resource-id "otp".
// Typing into the first one auto-fills the rest.
const otpFirstBox = extract((state) => state.ax.findAll("id:otp")[0]);
const onOtp = extract<boolean>(() => Boolean(otpFirstBox.current));

// Multi-device dialog that gates the Home screen when another device is
// signed in. Spec auto-signs-out the other device.
const signOutOthersButton = extract((state) => state.ax.find("text:Sign Out Other Devices"));
const confirmSignOutButton = extract((state) => {
  const cancel = state.ax.find("text:Cancel");
  if (!cancel) return undefined;
  return state.ax.find("text:Sign Out");
});

// Notification-permission modal on Home.
const skipNotificationsButton = extract((state) => state.ax.find("text:Skip"));

// Home screen customer/supplier rows. Compose testTag appears as
// content-desc; rows are suffixed with a stable UUID.
const customerRows = extract((state) => state.ax.findAll("descPrefix:customer_row_"));
const supplierRows = extract((state) => state.ax.findAll("descPrefix:supplier_row_"));
const onHome = extract<boolean>(
  () => customerRows.current.length > 0 || supplierRows.current.length > 0,
);

const onLedger = extract<boolean>(
  () => screen.current === "customer_ledger" || screen.current === "supplier_ledger",
);

// ── Properties ─────────────────────────────────────────────────
export const properties = {
  ledgerBalanceMatchesTxns: always(
    () =>
      !onLedger.current ||
      balance.current === totalGiven.current - totalReceived.current,
  ),
};

// ── Action generators ──────────────────────────────────────────
const selectEnglish = actions(() => {
  if (!onLanguageSelect.current) return [];
  return englishTile.current ? [Tap({ on: englishTile.current })] : [];
});

const enterMobile = actions(() => {
  if (!onEnterMobile.current) return [];
  const field = mobileField.current;
  const terms = termsCheckbox.current;
  const button = continueButton.current;
  const phone = process.env.UATU_TEST_PHONE ?? "";
  if (!field || !phone) return [];
  const steps: ReturnType<typeof Tap | typeof InputText>[] = [
    InputText({ into: field, text: phone }),
  ];
  if (terms) steps.push(Tap({ on: terms }));
  if (button) steps.push(Tap({ on: button }));
  return steps;
});

const enterOtp = actions(() => {
  if (!onOtp.current) return [];
  const field = otpFirstBox.current;
  const otp = process.env.UATU_TEST_OTP ?? "";
  if (!field || !otp) return [];
  return [InputText({ into: field, text: otp })];
});

const dismissMultiDevice = actions(() => {
  if (confirmSignOutButton.current) return [Tap({ on: confirmSignOutButton.current })];
  if (signOutOthersButton.current) return [Tap({ on: signOutOthersButton.current })];
  return [];
});

const dismissNotifications = actions(() => {
  return skipNotificationsButton.current
    ? [Tap({ on: skipNotificationsButton.current })]
    : [];
});

const openCustomerOrSupplier = actions(() => {
  if (!onHome.current) return [];
  const rows = [...customerRows.current, ...supplierRows.current];
  if (rows.length === 0) return [];
  return rows.map((row) => Tap({ on: row }));
});

export const actionsRoot = weighted(
  [100, selectEnglish],
  [100, enterMobile],
  [100, enterOtp],
  [100, dismissMultiDevice],
  [100, dismissNotifications],
  [80, openCustomerOrSupplier],
  [10, taps],
  [2, swipes],
);

(globalThis as { actions?: unknown }).actions = actionsRoot;
