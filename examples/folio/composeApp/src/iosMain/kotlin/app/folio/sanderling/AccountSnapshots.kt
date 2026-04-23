package app.folio.sanderling

import app.folio.data.Repository
import app.folio.feature.account.AddAccountUiState
import app.folio.platform.balanceOf

object AccountSnapshots {
    val accountCount by SanderlingIos.snapshot { Repository.accounts.value.size }
    val totalBalance by SanderlingIos.snapshot { balanceOf(Repository.transactions.value) }
    val accounts by SanderlingIos.snapshot {
        val txns = Repository.transactions.value
        Repository.accounts.value.map { a ->
            val rows = txns.filter { it.accountId == a.id }
            mapOf("id" to a.id, "name" to a.name, "balance" to balanceOf(rows), "txnCount" to rows.size)
        }
    }
    val addAccountError by SanderlingIos.snapshot { AddAccountUiState.addAccountError.value }
}
