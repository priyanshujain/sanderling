package dev.uatu.sample

import kotlinx.serialization.Serializable

@Serializable
data class Account(
    val id: String,
    val name: String,
    val createdAt: Long,
)

@Serializable
enum class TxnType { credit, debit }

@Serializable
data class Transaction(
    val id: String,
    val accountId: String,
    val type: TxnType,
    val amount: Long,
    val note: String,
    val createdAt: Long,
)

@Serializable
data class Session(
    val user: String,
    val loggedInAt: Long,
)
