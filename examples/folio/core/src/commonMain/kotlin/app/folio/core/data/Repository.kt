package app.folio.core.data

import app.folio.core.platform.Platform
import dev.zacsweers.metro.Inject
import kotlinx.coroutines.flow.StateFlow

@Inject
class Repository(private val store: LedgerStore) {
    val accounts: StateFlow<List<Account>> = store.accounts
    val transactions: StateFlow<List<Transaction>> = store.transactions
    val session: StateFlow<Session?> = store.session

    suspend fun createAccount(name: String): Account {
        val trimmed = name.trim()
        require(trimmed.isNotEmpty()) { "Name is required" }
        require(trimmed.length <= 40) { "Name is too long (max 40)" }
        require(!store.accountExistsByName(trimmed)) { "An account with that name already exists" }
        val account = Account(id = Platform.makeId(), name = trimmed, createdAt = Platform.now())
        store.insertAccount(account.id, account.name, account.createdAt)
        return account
    }

    fun getAccount(id: String): Account? = accounts.value.firstOrNull { it.id == id }

    suspend fun createTransaction(accountId: String, type: TxnType, amount: Long, note: String): Transaction {
        require(amount > 0) { "Amount must be greater than zero" }
        requireNotNull(getAccount(accountId)) { "Account not found" }
        val txn = Transaction(
            id = Platform.makeId(),
            accountId = accountId,
            type = type,
            amount = amount,
            note = note.trim().take(80),
            createdAt = Platform.now(),
        )
        store.insertTxn(txn.id, txn.accountId, txn.type, txn.amount, txn.note, txn.createdAt)
        return txn
    }

    suspend fun setSession(user: String) = store.upsertSession(user, Platform.now())
    suspend fun clearSession() = store.clearSession()
}
