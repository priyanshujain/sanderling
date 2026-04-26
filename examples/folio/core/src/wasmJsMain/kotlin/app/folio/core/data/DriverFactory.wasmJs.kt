@file:OptIn(kotlin.js.ExperimentalWasmJsInterop::class)

package app.folio.core.data

import app.cash.sqldelight.async.coroutines.awaitCreate
import app.cash.sqldelight.async.coroutines.awaitMigrate
import app.cash.sqldelight.db.SqlDriver
import app.cash.sqldelight.driver.worker.WebWorkerDriver
import app.folio.db.LedgerDatabase
import org.w3c.dom.Worker

actual class DriverFactory {
    actual suspend fun create(): SqlDriver {
        val driver = WebWorkerDriver(createSqliteWorker())
        val schema = LedgerDatabase.Schema
        val current = readUserVersion(driver)
        val target = schema.version
        when {
            current == 0L && !schemaExists(driver) -> {
                schema.awaitCreate(driver)
                writeUserVersion(driver, target)
            }
            current == 0L -> writeUserVersion(driver, target)
            current < target -> {
                schema.awaitMigrate(driver, current, target)
                writeUserVersion(driver, target)
            }
        }
        return driver
    }
}

private suspend fun readUserVersion(driver: SqlDriver): Long {
    var version = 0L
    driver.executeQuery(
        identifier = null,
        sql = "PRAGMA user_version",
        mapper = { cursor ->
            app.cash.sqldelight.db.QueryResult.AsyncValue {
                if (cursor.next().await()) version = cursor.getLong(0) ?: 0L
                Unit
            }
        },
        parameters = 0,
    ).await()
    return version
}

private suspend fun writeUserVersion(driver: SqlDriver, version: Long) {
    driver.execute(null, "PRAGMA user_version = $version", 0).await()
}

private suspend fun schemaExists(driver: SqlDriver): Boolean {
    var exists = false
    driver.executeQuery(
        identifier = null,
        sql = "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' LIMIT 1",
        mapper = { cursor ->
            app.cash.sqldelight.db.QueryResult.AsyncValue {
                exists = cursor.next().await()
                Unit
            }
        },
        parameters = 0,
    ).await()
    return exists
}

private fun createSqliteWorker(): Worker =
    js("""new Worker(new URL("./sqlite.worker.js", import.meta.url), { type: "module" })""")
