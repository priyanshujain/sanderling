package app.folio.sanderling

import app.folio.FocusTracker
import app.folio.data.Repository
import app.folio.data.TxnType
import app.folio.feature.ledger.AddTransactionUiState
import app.folio.navigation.Navigator
import app.folio.navigation.Route
import app.folio.platform.balanceOf
import app.folio.platform.signedAmount
import dev.sanderling.sdk.Sanderling

object LedgerSnapshots {
    private val activeId
        get() = when (val r = Navigator.current.value) {
            is Route.Ledger -> r.accountId
            is Route.AddTransaction -> r.accountId
            else -> null
        }

    val activeAccountId by Sanderling.snapshot { activeId }
    val ledgerRows by Sanderling.snapshot {
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
    val ledgerBalance by Sanderling.snapshot {
        val active = activeId ?: return@snapshot 0L
        balanceOf(Repository.transactions.value.filter { it.accountId == active })
    }
    val focusedInput by Sanderling.snapshot { FocusTracker.current.value }
    val txnFormType by Sanderling.snapshot { AddTransactionUiState.txnFormType.value }
    val txnFormAccountId by Sanderling.snapshot { (Navigator.current.value as? Route.AddTransaction)?.accountId }
    val txnError by Sanderling.snapshot { AddTransactionUiState.txnError.value }
}
