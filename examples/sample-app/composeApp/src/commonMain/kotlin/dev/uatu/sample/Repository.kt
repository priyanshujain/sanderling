package dev.uatu.sample

import app.cash.sqldelight.coroutines.asFlow
import app.cash.sqldelight.coroutines.mapToList
import app.cash.sqldelight.coroutines.mapToOneOrNull
import dev.uatu.sample.db.LedgerDatabase
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn

object Repository {
    private lateinit var db: LedgerDatabase
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)

    lateinit var accounts: StateFlow<List<Account>>
        private set
    lateinit var transactions: StateFlow<List<Transaction>>
        private set
    lateinit var session: StateFlow<Session?>
        private set

    fun init(factory: DriverFactory) {
        if (::db.isInitialized) return
        db = LedgerDatabase(factory.create())
        val q = db.ledgerQueries

        accounts = q.selectAllAccounts()
            .asFlow()
            .mapToList(Dispatchers.Default)
            .map { rows -> rows.map { Account(it.id, it.name, it.createdAt) } }
            .stateIn(scope, SharingStarted.Eagerly, emptyList())

        transactions = q.selectAllTxns()
            .asFlow()
            .mapToList(Dispatchers.Default)
            .map { rows -> rows.map { it.toDomain() } }
            .stateIn(scope, SharingStarted.Eagerly, emptyList())

        session = q.selectSession()
            .asFlow()
            .mapToOneOrNull(Dispatchers.Default)
            .map { it?.let { row -> Session(row.user, row.loggedInAt) } }
            .stateIn(scope, SharingStarted.Eagerly, null)
    }

    fun accountNameExists(name: String): Boolean =
        db.ledgerQueries.accountExistsByName(name).executeAsOne()

    fun createAccount(name: String): Account {
        val trimmed = name.trim()
        require(trimmed.isNotEmpty()) { "Name is required" }
        require(trimmed.length <= 40) { "Name is too long (max 40)" }
        require(!accountNameExists(trimmed)) { "An account with that name already exists" }
        val account = Account(id = Platform.makeId(), name = trimmed, createdAt = Platform.now())
        db.ledgerQueries.insertAccount(account.id, account.name, account.createdAt)
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
        db.ledgerQueries.insertTxn(txn.id, txn.accountId, txn.type.name, txn.amount, txn.note, txn.createdAt)
        return txn
    }

    fun setSession(user: String) {
        db.ledgerQueries.upsertSession(user, Platform.now())
    }

    fun clearSession() {
        db.ledgerQueries.clearSession()
    }
}

private fun dev.uatu.sample.db.Txns.toDomain(): Transaction =
    Transaction(
        id = id,
        accountId = accountId,
        type = TxnType.valueOf(type),
        amount = amount,
        note = note,
        createdAt = createdAt,
    )
