package app.folio.data

import app.cash.sqldelight.coroutines.asFlow
import app.cash.sqldelight.coroutines.mapToList
import app.cash.sqldelight.coroutines.mapToOneOrNull
import app.folio.db.LedgerDatabase
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn

class SqlLedgerStore(factory: DriverFactory) : LedgerStore {
    private val db = LedgerDatabase(factory.create())
    private val q = db.ledgerQueries
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)

    override val accounts: StateFlow<List<Account>> =
        q.selectAllAccounts()
            .asFlow()
            .mapToList(Dispatchers.Default)
            .map { rows -> rows.map { Account(it.id, it.name, it.createdAt) } }
            .stateIn(scope, SharingStarted.Eagerly, emptyList())

    override val transactions: StateFlow<List<Transaction>> =
        q.selectAllTxns()
            .asFlow()
            .mapToList(Dispatchers.Default)
            .map { rows -> rows.map { it.toDomain() } }
            .stateIn(scope, SharingStarted.Eagerly, emptyList())

    override val session: StateFlow<Session?> =
        q.selectSession()
            .asFlow()
            .mapToOneOrNull(Dispatchers.Default)
            .map { it?.let { row -> Session(row.user, row.loggedInAt) } }
            .stateIn(scope, SharingStarted.Eagerly, null)

    override fun accountExistsByName(name: String): Boolean =
        q.accountExistsByName(name).executeAsOne()

    override fun insertAccount(id: String, name: String, createdAt: Long) {
        q.insertAccount(id, name, createdAt)
    }

    override fun insertTxn(id: String, accountId: String, type: TxnType, amount: Long, note: String, createdAt: Long) {
        q.insertTxn(id, accountId, type.name, amount, note, createdAt)
    }

    override fun upsertSession(user: String, loggedInAt: Long) {
        q.upsertSession(user, loggedInAt)
    }

    override fun clearSession() {
        q.clearSession()
    }
}

private fun app.folio.db.Txns.toDomain(): Transaction =
    Transaction(
        id = id,
        accountId = accountId,
        type = TxnType.valueOf(type),
        amount = amount,
        note = note,
        createdAt = createdAt,
    )
