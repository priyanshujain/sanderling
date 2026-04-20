package dev.uatu.sample

import app.cash.sqldelight.db.SqlDriver
import app.cash.sqldelight.driver.native.NativeSqliteDriver
import dev.uatu.sample.db.LedgerDatabase

actual class DriverFactory {
    actual fun create(): SqlDriver =
        NativeSqliteDriver(LedgerDatabase.Schema, "ledger.db")
}
