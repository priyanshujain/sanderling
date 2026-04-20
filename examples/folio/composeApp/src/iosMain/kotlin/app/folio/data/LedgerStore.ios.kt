package app.folio.data

actual fun createLedgerStore(): LedgerStore = SqlLedgerStore(DriverFactory())
