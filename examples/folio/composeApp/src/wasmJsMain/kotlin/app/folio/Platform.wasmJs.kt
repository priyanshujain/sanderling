package app.folio

import kotlin.random.Random

actual object Platform {
    actual fun now(): Long = jsNow().toLong()
    actual fun makeId(): String = cryptoRandomUuid() ?: fallbackUuid()
}

private fun jsNow(): Double = js("Date.now()")

private fun cryptoRandomUuid(): String? = js(
    "(typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') ? crypto.randomUUID() : null"
)

private fun fallbackUuid(): String {
    val bytes = ByteArray(16) { Random.nextInt(0, 256).toByte() }
    bytes[6] = ((bytes[6].toInt() and 0x0f) or 0x40).toByte()
    bytes[8] = ((bytes[8].toInt() and 0x3f) or 0x80).toByte()
    val hex = bytes.joinToString("") { ((it.toInt() and 0xff) or 0x100).toString(16).substring(1) }
    return "${hex.substring(0, 8)}-${hex.substring(8, 12)}-${hex.substring(12, 16)}-${hex.substring(16, 20)}-${hex.substring(20)}"
}
