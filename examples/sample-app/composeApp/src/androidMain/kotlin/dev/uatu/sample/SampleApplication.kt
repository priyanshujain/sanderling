package dev.uatu.sample

import android.app.Application
import android.content.pm.ApplicationInfo
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

    // Fires a synthetic Uatu.reportError in debug builds so the sample-app
    // e2e run can verify noUncaughtExceptions surfaces SDK-captured errors
    // in the trace. Production builds skip this.
    private fun maybeInjectDebugError() {
        val isDebuggable = applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE != 0
        if (isDebuggable) {
            Uatu.reportError(RuntimeException("synthetic"))
        }
    }
}
