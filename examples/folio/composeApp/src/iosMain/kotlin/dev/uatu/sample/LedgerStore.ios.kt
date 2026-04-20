package dev.uatu.sample

actual fun createLedgerStore(): LedgerStore = SqlLedgerStore(DriverFactory())
