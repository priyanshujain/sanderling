package app.folio.core.platform

import java.util.UUID

actual object Platform {
    actual fun now(): Long = System.currentTimeMillis()
    actual fun makeId(): String = UUID.randomUUID().toString()
}
