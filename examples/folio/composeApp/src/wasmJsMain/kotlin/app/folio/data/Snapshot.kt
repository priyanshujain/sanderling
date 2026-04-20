package app.folio.data

internal data class Snapshot(
    val accounts: List<Account>,
    val transactions: List<Transaction>,
    val session: Session?,
) {
    fun encode(): String = buildString {
        for (a in accounts) {
            append("A\t").append(esc(a.id)).append('\t').append(esc(a.name)).append('\t').append(a.createdAt).append('\n')
        }
        for (t in transactions) {
            append("T\t").append(esc(t.id)).append('\t').append(esc(t.accountId)).append('\t')
                .append(t.type.name).append('\t').append(t.amount).append('\t')
                .append(esc(t.note)).append('\t').append(t.createdAt).append('\n')
        }
        session?.let { append("S\t").append(esc(it.user)).append('\t').append(it.loggedInAt).append('\n') }
    }

    companion object {
        fun decode(raw: String): Snapshot? {
            val accounts = mutableListOf<Account>()
            val transactions = mutableListOf<Transaction>()
            var session: Session? = null
            for (line in raw.split('\n')) {
                if (line.isEmpty()) continue
                val parts = line.split('\t')
                when (parts[0]) {
                    "A" -> if (parts.size == 4) accounts += Account(unesc(parts[1]), unesc(parts[2]), parts[3].toLong())
                    "T" -> if (parts.size == 7) transactions += Transaction(
                        id = unesc(parts[1]),
                        accountId = unesc(parts[2]),
                        type = TxnType.valueOf(parts[3]),
                        amount = parts[4].toLong(),
                        note = unesc(parts[5]),
                        createdAt = parts[6].toLong(),
                    )
                    "S" -> if (parts.size == 3) session = Session(unesc(parts[1]), parts[2].toLong())
                }
            }
            return Snapshot(accounts, transactions, session)
        }

        private fun esc(s: String) = s.replace("\\", "\\\\").replace("\t", "\\t").replace("\n", "\\n")
        private fun unesc(s: String): String {
            val out = StringBuilder(s.length)
            var i = 0
            while (i < s.length) {
                val c = s[i]
                if (c == '\\' && i + 1 < s.length) {
                    when (s[i + 1]) {
                        't' -> out.append('\t')
                        'n' -> out.append('\n')
                        '\\' -> out.append('\\')
                        else -> out.append(s[i + 1])
                    }
                    i += 2
                } else {
                    out.append(c); i++
                }
            }
            return out.toString()
        }
    }
}
