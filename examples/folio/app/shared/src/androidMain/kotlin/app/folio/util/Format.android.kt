package app.folio.util

import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

private val DATE_FMT = SimpleDateFormat("MMM d, h:mm a", Locale.US)

actual fun formatDate(epochMillis: Long): String = DATE_FMT.format(Date(epochMillis))
