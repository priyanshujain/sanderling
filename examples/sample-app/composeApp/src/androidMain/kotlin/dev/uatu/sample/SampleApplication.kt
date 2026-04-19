package dev.uatu.sample

import android.app.Application
import dev.uatu.sdk.Uatu

class SampleApplication : Application() {
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
        maybeInjectDebugError()
    }

    // Fires a synthetic Uatu.reportError when the system property
    // `uatu.inject_error` is set (for example via `adb shell setprop`). Used
    // by the e2e test to verify the noUncaughtExceptions property surfaces
    // SDK-captured errors in the trace.
    private fun maybeInjectDebugError() {
        if (System.getProperty("uatu.inject_error") == "true") {
            Uatu.reportError(RuntimeException("synthetic"))
        }
    }
}
