package dev.uatu.sample

import kotlin.math.absoluteValue

private const val CURRENCY = "$"
private val AMOUNT_REGEX = Regex("""^\d+(\.\d{1,2})?$""")

fun formatCents(cents: Long, signed: Boolean = false): String {
    val abs = cents.absoluteValue
    val dollars = abs / 100
    val rem = (abs % 100).toInt()
    val dollarsStr = withThousandsSeparator(dollars)
    val remStr = rem.toString().padStart(2, '0')
    val body = "$CURRENCY$dollarsStr.$remStr"
    return when {
        cents < 0 -> "-$body"
        signed && cents > 0 -> "+$body"
        else -> body
    }
}

private fun withThousandsSeparator(value: Long): String {
    val s = value.toString()
    if (s.length <= 3) return s
    val out = StringBuilder()
    var count = 0
    for (i in s.length - 1 downTo 0) {
        out.append(s[i])
        count++
        if (count == 3 && i != 0) {
            out.append(',')
            count = 0
        }
    }
    return out.reverse().toString()
}

fun parseCents(input: String): Long? {
    val trimmed = input.trim().replace(",", "")
    if (trimmed.isEmpty()) return null
    if (!AMOUNT_REGEX.matches(trimmed)) return null
    val dot = trimmed.indexOf('.')
    val whole: String
    val frac: String
    if (dot < 0) {
        whole = trimmed
        frac = ""
    } else {
        whole = trimmed.substring(0, dot)
        frac = trimmed.substring(dot + 1)
    }
    val fracPadded = (frac + "00").substring(0, 2)
    val wholeLong = whole.toLongOrNull() ?: return null
    val fracLong = fracPadded.toLongOrNull() ?: return null
    val total = wholeLong * 100 + fracLong
    if (total < 0) return null
    return total
}

fun signedAmount(t: Transaction): Long = if (t.type == TxnType.credit) t.amount else -t.amount

fun balanceOf(txns: List<Transaction>): Long = txns.sumOf { signedAmount(it) }

fun initialsOf(name: String): String {
    val parts = name.trim().split(Regex("\\s+")).filter { it.isNotEmpty() }
    if (parts.isEmpty()) return "?"
    if (parts.size == 1) return parts[0].take(2).uppercase()
    return (parts.first().first().toString() + parts.last().first().toString()).uppercase()
}

expect fun formatDate(epochMillis: Long): String

expect fun formatClock(epochMillis: Long): String
