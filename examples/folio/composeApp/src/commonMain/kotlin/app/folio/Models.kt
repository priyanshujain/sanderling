package app.folio

data class Account(
    val id: String,
    val name: String,
    val createdAt: Long,
)

enum class TxnType { credit, debit }

data class Transaction(
    val id: String,
    val accountId: String,
    val type: TxnType,
    val amount: Long,
    val note: String,
    val createdAt: Long,
)

data class Session(
    val user: String,
    val loggedInAt: Long,
)
