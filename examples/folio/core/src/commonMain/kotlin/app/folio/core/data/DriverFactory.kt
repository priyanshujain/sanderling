package app.folio.core.data

import app.cash.sqldelight.db.SqlDriver

expect class DriverFactory {
    suspend fun create(): SqlDriver
}
