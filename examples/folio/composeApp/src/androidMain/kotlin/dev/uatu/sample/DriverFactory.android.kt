package dev.uatu.sample

import android.content.Context
import app.cash.sqldelight.db.SqlDriver
import app.cash.sqldelight.driver.android.AndroidSqliteDriver
import dev.uatu.sample.db.LedgerDatabase

actual class DriverFactory(private val context: Context) {
    actual fun create(): SqlDriver =
        AndroidSqliteDriver(LedgerDatabase.Schema, context, "ledger.db")
}
