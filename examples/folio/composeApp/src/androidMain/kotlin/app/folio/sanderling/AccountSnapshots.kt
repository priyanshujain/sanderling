package app.folio.sanderling

import app.folio.data.Repository
import app.folio.feature.account.AddAccountUiState
import app.folio.platform.balanceOf
import dev.sanderling.sdk.Sanderling

object AccountSnapshots {
    val accountCount by Sanderling.snapshot { Repository.accounts.value.size }
    val totalBalance by Sanderling.snapshot { balanceOf(Repository.transactions.value) }
    val accounts by Sanderling.snapshot {
        val txns = Repository.transactions.value
        Repository.accounts.value.map { a ->
            val rows = txns.filter { it.accountId == a.id }
            mapOf("id" to a.id, "name" to a.name, "balance" to balanceOf(rows), "txnCount" to rows.size)
        }
    }
    val addAccountError by Sanderling.snapshot { AddAccountUiState.addAccountError.value }
}
