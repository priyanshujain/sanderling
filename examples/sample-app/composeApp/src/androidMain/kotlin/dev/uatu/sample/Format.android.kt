package dev.uatu.sample

import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

private val DATE_FMT = SimpleDateFormat("MMM d, h:mm a", Locale.US)
private val CLOCK_FMT = SimpleDateFormat("HH:mm", Locale.US)

actual fun formatDate(epochMillis: Long): String = DATE_FMT.format(Date(epochMillis))

actual fun formatClock(epochMillis: Long): String = CLOCK_FMT.format(Date(epochMillis))
