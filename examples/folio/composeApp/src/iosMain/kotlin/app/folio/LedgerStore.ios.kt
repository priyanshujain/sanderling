package app.folio

actual fun createLedgerStore(): LedgerStore = SqlLedgerStore(DriverFactory())
