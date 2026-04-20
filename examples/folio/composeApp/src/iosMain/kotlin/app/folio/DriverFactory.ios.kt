package app.folio

import app.cash.sqldelight.db.SqlDriver
import app.cash.sqldelight.driver.native.NativeSqliteDriver
import app.folio.db.LedgerDatabase

actual class DriverFactory {
    actual fun create(): SqlDriver =
        NativeSqliteDriver(LedgerDatabase.Schema, "ledger.db")
}
