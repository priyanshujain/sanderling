package app.folio

import platform.Foundation.NSDate
import platform.Foundation.NSUUID
import platform.Foundation.timeIntervalSince1970

actual object Platform {
    actual fun now(): Long = (NSDate().timeIntervalSince1970 * 1000.0).toLong()
    actual fun makeId(): String = NSUUID().UUIDString
}
