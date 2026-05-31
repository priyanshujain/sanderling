export interface LedgerRow {
  key: string;
  signed: number;
}

// When new ledger rows appear, the balance delta must equal the SUM of the
// new rows' signed amounts. Single-row sums and multi-row sums BOTH must
// match the delta; a double-submit landing two rows whose total is twice
// the balance change trips the sum check.
export function balanceMatchesAddedSum(
  previousRows: readonly LedgerRow[],
  currentRows: readonly LedgerRow[],
  previousBalance: number,
  currentBalance: number,
): boolean {
  const previousKeys = new Set(previousRows.map(row => row.key));
  const added = currentRows.filter(row => !previousKeys.has(row.key));
  if (added.length === 0) return true;
  const delta = currentBalance - previousBalance;
  const addedSum = added.reduce((sum, row) => sum + row.signed, 0);
  return addedSum === delta;
}
