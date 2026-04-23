package app.folio.sanderling

import app.folio.FocusTracker
import app.folio.data.Repository
import app.folio.data.TxnType
import app.folio.feature.ledger.AddTransactionUiState
import app.folio.navigation.Navigator
import app.folio.navigation.Route
import app.folio.platform.balanceOf
import app.folio.platform.signedAmount

object LedgerSnapshots {
    private val activeId
        get() = when (val r = Navigator.current.value) {
            is Route.Ledger -> r.accountId
            is Route.AddTransaction -> r.accountId
            else -> null
        }

    val activeAccountId by SanderlingIos.snapshot { activeId }
    val ledgerRows by SanderlingIos.snapshot {
        val active = activeId ?: return@snapshot emptyList<Any>()
        Repository.transactions.value.filter { it.accountId == active }.map {
            mapOf(
                "id" to it.id,
                "accountId" to it.accountId,
                "type" to if (it.type == TxnType.credit) "credit" else "debit",
                "amount" to it.amount,
                "signed" to signedAmount(it),
            )
        }
    }
    val ledgerBalance by SanderlingIos.snapshot {
        val active = activeId ?: return@snapshot 0L
        balanceOf(Repository.transactions.value.filter { it.accountId == active })
    }
    val focusedInput by SanderlingIos.snapshot { FocusTracker.current.value }
    val txnFormType by SanderlingIos.snapshot { AddTransactionUiState.txnFormType.value }
    val txnFormAccountId by SanderlingIos.snapshot { (Navigator.current.value as? Route.AddTransaction)?.accountId }
    val txnError by SanderlingIos.snapshot { AddTransactionUiState.txnError.value }
}
