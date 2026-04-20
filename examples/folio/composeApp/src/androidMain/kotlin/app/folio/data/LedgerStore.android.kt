package app.folio.data

import android.content.Context

object AndroidLedgerContext {
    lateinit var context: Context
}

actual fun createLedgerStore(): LedgerStore =
    SqlLedgerStore(DriverFactory(AndroidLedgerContext.context))
