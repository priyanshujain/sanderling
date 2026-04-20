package app.folio.data

enum class TxnType { credit, debit }

data class Transaction(
    val id: String,
    val accountId: String,
    val type: TxnType,
    val amount: Long,
    val note: String,
    val createdAt: Long,
)
