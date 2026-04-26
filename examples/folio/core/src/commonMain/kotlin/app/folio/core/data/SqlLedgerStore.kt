package app.folio.core.data

import app.cash.sqldelight.async.coroutines.awaitAsOne
import app.cash.sqldelight.coroutines.asFlow
import app.cash.sqldelight.coroutines.mapToList
import app.cash.sqldelight.coroutines.mapToOneOrNull
import app.folio.db.LedgerDatabase
import dev.zacsweers.metro.AppScope
import dev.zacsweers.metro.Inject
import dev.zacsweers.metro.SingleIn
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn

@SingleIn(AppScope::class)
@Inject
class SqlLedgerStore(db: LedgerDatabase) : LedgerStore {
    private val queries = db.ledgerQueries
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)

    override val accounts: StateFlow<List<Account>> =
        queries.selectAllAccounts()
            .asFlow()
            .mapToList(Dispatchers.Default)
            .map { rows -> rows.map { Account(it.id, it.name, it.createdAt) } }
            .stateIn(scope, SharingStarted.Eagerly, emptyList())

    override val transactions: StateFlow<List<Transaction>> =
        queries.selectAllTxns()
            .asFlow()
            .mapToList(Dispatchers.Default)
            .map { rows -> rows.map { it.toDomain() } }
            .stateIn(scope, SharingStarted.Eagerly, emptyList())

    override val session: StateFlow<Session?> =
        queries.selectSession()
            .asFlow()
            .mapToOneOrNull(Dispatchers.Default)
            .map { row -> row?.let { Session(it.user, it.loggedInAt) } }
            .stateIn(scope, SharingStarted.Eagerly, null)

    override suspend fun accountExistsByName(name: String): Boolean =
        queries.accountExistsByName(name).awaitAsOne()

    override suspend fun insertAccount(id: String, name: String, createdAt: Long) {
        queries.insertAccount(id, name, createdAt)
    }

    override suspend fun insertTxn(id: String, accountId: String, type: TxnType, amount: Long, note: String, createdAt: Long) {
        queries.insertTxn(id, accountId, type.name, amount, note, createdAt)
    }

    override suspend fun upsertSession(user: String, loggedInAt: Long) {
        queries.upsertSession(user, loggedInAt)
    }

    override suspend fun clearSession() {
        queries.clearSession()
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
