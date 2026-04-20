package app.folio

import app.folio.data.Account
import app.folio.data.Session
import app.folio.data.Transaction
import app.folio.data.TxnType
import kotlinx.coroutines.flow.StateFlow

object Repository {
    private var _store: LedgerStore? = null
    private val store: LedgerStore
        get() = _store ?: error("Repository not initialized")

    val accounts: StateFlow<List<Account>> get() = store.accounts
    val transactions: StateFlow<List<Transaction>> get() = store.transactions
    val session: StateFlow<Session?> get() = store.session

    fun init() {
        if (_store == null) _store = createLedgerStore()
    }

    fun createAccount(name: String): Account {
        val trimmed = name.trim()
        require(trimmed.isNotEmpty()) { "Name is required" }
        require(trimmed.length <= 40) { "Name is too long (max 40)" }
        require(!store.accountExistsByName(trimmed)) { "An account with that name already exists" }
        val account = Account(id = Platform.makeId(), name = trimmed, createdAt = Platform.now())
        store.insertAccount(account.id, account.name, account.createdAt)
        return account
    }

    fun getAccount(id: String): Account? = accounts.value.firstOrNull { it.id == id }

    fun createTransaction(accountId: String, type: TxnType, amount: Long, note: String): Transaction {
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

    fun setSession(user: String) = store.upsertSession(user, Platform.now())

    fun clearSession() = store.clearSession()
}
