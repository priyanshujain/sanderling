package app.folio.core.data

import app.cash.sqldelight.async.coroutines.synchronous
import app.cash.sqldelight.db.SqlDriver
import app.cash.sqldelight.driver.native.NativeSqliteDriver
import app.folio.db.LedgerDatabase

actual class DriverFactory {
    actual suspend fun create(): SqlDriver =
        NativeSqliteDriver(LedgerDatabase.Schema.synchronous(), "ledger.db")
}
