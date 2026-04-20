package dev.uatu.sample

import java.util.UUID

actual object Platform {
    actual fun now(): Long = System.currentTimeMillis()
    actual fun makeId(): String = UUID.randomUUID().toString()
}
