package app.folio

import platform.Foundation.NSDate
import platform.Foundation.NSDateFormatter
import platform.Foundation.NSLocale
import platform.Foundation.dateWithTimeIntervalSince1970

private val DATE_FMT = NSDateFormatter().apply {
    locale = NSLocale("en_US_POSIX")
    dateFormat = "MMM d, h:mm a"
}

private fun date(epochMillis: Long): NSDate =
    NSDate.dateWithTimeIntervalSince1970(epochMillis / 1000.0)

actual fun formatDate(epochMillis: Long): String = DATE_FMT.stringFromDate(date(epochMillis))
