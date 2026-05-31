// When the last action is a tap (or double-tap) on the transaction Submit
// button, the absolute change in total balance must equal the amount the
// user typed. A double-submit lands two transactions and shifts the balance
// by 2x the typed amount, tripping this check.
export function submitChangesBalanceByTypedAmount(args: {
  lastAction: { kind?: string; on?: string | object } | null;
  typedAmount: number;
  prevTotalBalance: number;
  currTotalBalance: number;
}): boolean {
  const { lastAction, typedAmount, prevTotalBalance, currTotalBalance } = args;
  if (lastAction == null) return true;
  if (lastAction.kind !== "Tap" && lastAction.kind !== "DoubleTap") return true;
  const on = lastAction.on;
  const onString = typeof on === "string" ? on : on != null ? JSON.stringify(on) : "";
  if (!onString.includes("TxnSubmit")) return true;
  if (typedAmount === 0) return true;
  return Math.abs(currTotalBalance - prevTotalBalance) === typedAmount;
}
