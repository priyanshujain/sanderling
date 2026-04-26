package app.folio.core.data

import app.cash.sqldelight.async.coroutines.awaitCreate
import app.cash.sqldelight.db.SqlDriver
import app.cash.sqldelight.driver.worker.WebWorkerDriver
import app.folio.db.LedgerDatabase
import org.w3c.dom.Worker

actual class DriverFactory {
    actual suspend fun create(): SqlDriver {
        val worker = Worker(workerScriptUrl())
        val driver = WebWorkerDriver(worker)
        LedgerDatabase.Schema.awaitCreate(driver)
        return driver
    }
}

private fun workerScriptUrl(): String =
    js("""new URL("./sqlite.worker.js", import.meta.url).toString()""")
