package app.folio

import android.app.Application
import app.folio.data.AndroidLedgerContext
import app.folio.data.Repository
import app.folio.data.TxnType
import app.folio.feature.account.AddAccountUiState
import app.folio.feature.auth.LoginUiState
import app.folio.feature.ledger.AddTransactionUiState
import app.folio.navigation.Navigator
import app.folio.navigation.Route
import app.folio.platform.balanceOf
import app.folio.platform.signedAmount
import dev.sanderling.sdk.Sanderling

class FolioApplication : Application() {
    override fun onCreate() {
        super.onCreate()
        AndroidLedgerContext.context = applicationContext
        Repository.init()
        Sanderling.start(this)
        Sanderling.extract("logged_in") { Repository.session.value != null }
        Sanderling.extract("account_count") { Repository.accounts.value.size }
        Sanderling.extract("total_balance") { balanceOf(Repository.transactions.value) }
        Sanderling.extract("route") {
            when (Navigator.current.value) {
                Route.Login -> "login"
                Route.Home -> "home"
                Route.AddAccount -> "add-account"
                is Route.Ledger -> "ledger"
                is Route.AddTransaction -> "add-transaction"
            }
        }
        Sanderling.extract("auth_status") {
            if (Repository.session.value != null) "logged-in" else "logged-out"
        }
        Sanderling.extract("accounts") {
            val txns = Repository.transactions.value
            Repository.accounts.value.map { a ->
                val rows = txns.filter { it.accountId == a.id }
                mapOf(
                    "id" to a.id,
                    "name" to a.name,
                    "balance" to balanceOf(rows),
                    "txnCount" to rows.size,
                )
            }
        }
        Sanderling.extract("active_account_id") {
            when (val r = Navigator.current.value) {
                is Route.Ledger -> r.accountId
                is Route.AddTransaction -> r.accountId
                else -> null
            }
        }
        Sanderling.extract("ledger_rows") {
            val active = when (val r = Navigator.current.value) {
                is Route.Ledger -> r.accountId
                is Route.AddTransaction -> r.accountId
                else -> null
            }
            if (active == null) emptyList()
            else Repository.transactions.value
                .filter { it.accountId == active }
                .map {
                    mapOf(
                        "id" to it.id,
                        "accountId" to it.accountId,
                        "type" to if (it.type == TxnType.credit) "credit" else "debit",
                        "amount" to it.amount,
                        "signed" to signedAmount(it),
                    )
                }
        }
        Sanderling.extract("ledger_balance") {
            val active = when (val r = Navigator.current.value) {
                is Route.Ledger -> r.accountId
                is Route.AddTransaction -> r.accountId
                else -> null
            }
            if (active == null) 0L
            else balanceOf(Repository.transactions.value.filter { it.accountId == active })
        }
        Sanderling.extract("focused_input") { FocusTracker.current.value }
        Sanderling.extract("txn_form_type") { AddTransactionUiState.txnFormType.value }
        Sanderling.extract("txn_form_account_id") {
            (Navigator.current.value as? Route.AddTransaction)?.accountId
        }
        Sanderling.extract("login_error") { LoginUiState.loginError.value }
        Sanderling.extract("add_account_error") { AddAccountUiState.addAccountError.value }
        Sanderling.extract("txn_error") { AddTransactionUiState.txnError.value }
    }
}
