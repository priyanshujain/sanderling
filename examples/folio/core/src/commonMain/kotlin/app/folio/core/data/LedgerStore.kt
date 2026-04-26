package app.folio.core.data

import kotlinx.coroutines.flow.StateFlow

interface LedgerStore {
    val accounts: StateFlow<List<Account>>
    val transactions: StateFlow<List<Transaction>>
    val session: StateFlow<Session?>

    suspend fun accountExistsByName(name: String): Boolean
    suspend fun insertAccount(id: String, name: String, createdAt: Long)
    suspend fun insertTxn(id: String, accountId: String, type: TxnType, amount: Long, note: String, createdAt: Long)
    suspend fun upsertSession(user: String, loggedInAt: Long)
    suspend fun clearSession()
}
