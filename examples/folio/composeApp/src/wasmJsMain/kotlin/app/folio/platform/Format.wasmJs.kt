package app.folio.platform

private val MONTHS = arrayOf(
    "Jan", "Feb", "Mar", "Apr", "May", "Jun",
    "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
)

actual fun formatDate(epochMillis: Long): String {
    val month = jsDateMonth(epochMillis.toDouble())
    val day = jsDateDay(epochMillis.toDouble())
    val hour24 = jsDateHours(epochMillis.toDouble())
    val minute = jsDateMinutes(epochMillis.toDouble())
    val am = hour24 < 12
    val hour12 = when {
        hour24 == 0 -> 12
        hour24 > 12 -> hour24 - 12
        else -> hour24
    }
    val minuteStr = minute.toString().padStart(2, '0')
    val suffix = if (am) "AM" else "PM"
    return "${MONTHS[month]} $day, $hour12:$minuteStr $suffix"
}

private fun jsDateMonth(ms: Double): Int = js("new Date(ms).getMonth()")
private fun jsDateDay(ms: Double): Int = js("new Date(ms).getDate()")
private fun jsDateHours(ms: Double): Int = js("new Date(ms).getHours()")
private fun jsDateMinutes(ms: Double): Int = js("new Date(ms).getMinutes()")
