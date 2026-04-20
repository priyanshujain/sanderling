package app.folio

import android.app.Application
import dev.uatu.sdk.Uatu

class FolioApplication : Application() {
    override fun onCreate() {
        super.onCreate()
        AndroidLedgerContext.context = applicationContext
        Repository.init()
        Uatu.start(this)
        Uatu.extract("logged_in") { Repository.session.value != null }
        Uatu.extract("account_count") { Repository.accounts.value.size }
        Uatu.extract("total_balance") { balanceOf(Repository.transactions.value) }
        Uatu.extract("route") {
            when (Navigator.current.value) {
                Route.Login -> "login"
                Route.Home -> "home"
                Route.AddAccount -> "add-account"
                is Route.Ledger -> "ledger"
                is Route.AddTransaction -> "add-transaction"
            }
        }
        Uatu.extract("auth_status") {
            if (Repository.session.value != null) "logged-in" else "logged-out"
        }
        Uatu.extract("accounts") {
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
        Uatu.extract("active_account_id") {
            when (val r = Navigator.current.value) {
                is Route.Ledger -> r.accountId
                is Route.AddTransaction -> r.accountId
                else -> null
            }
        }
        Uatu.extract("ledger_rows") {
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
        Uatu.extract("ledger_balance") {
            val active = when (val r = Navigator.current.value) {
                is Route.Ledger -> r.accountId
                is Route.AddTransaction -> r.accountId
                else -> null
            }
            if (active == null) 0L
            else balanceOf(Repository.transactions.value.filter { it.accountId == active })
        }
        Uatu.extract("focused_input") { FocusTracker.current.value }
        Uatu.extract("txn_form_type") { UiState.txnFormType.value }
        Uatu.extract("txn_form_account_id") {
            (Navigator.current.value as? Route.AddTransaction)?.accountId
        }
        Uatu.extract("login_error") { UiState.loginError.value }
        Uatu.extract("add_account_error") { UiState.addAccountError.value }
        Uatu.extract("txn_error") { UiState.txnError.value }
    }
}
