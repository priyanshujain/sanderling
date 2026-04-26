package app.folio.core.data

import android.content.Context
import app.cash.sqldelight.async.coroutines.synchronous
import app.cash.sqldelight.db.SqlDriver
import app.cash.sqldelight.driver.android.AndroidSqliteDriver
import app.folio.db.LedgerDatabase

actual class DriverFactory(private val context: Context) {
    actual suspend fun create(): SqlDriver =
        AndroidSqliteDriver(LedgerDatabase.Schema.synchronous(), context, "ledger.db")
}
