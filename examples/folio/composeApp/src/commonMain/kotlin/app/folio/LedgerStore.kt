package app.folio

import app.folio.data.Account
import app.folio.data.Session
import app.folio.data.Transaction
import app.folio.data.TxnType
import kotlinx.coroutines.flow.StateFlow

interface LedgerStore {
    val accounts: StateFlow<List<Account>>
    val transactions: StateFlow<List<Transaction>>
    val session: StateFlow<Session?>

    fun accountExistsByName(name: String): Boolean
    fun insertAccount(id: String, name: String, createdAt: Long)
    fun insertTxn(id: String, accountId: String, type: TxnType, amount: Long, note: String, createdAt: Long)
    fun upsertSession(user: String, loggedInAt: Long)
    fun clearSession()
}

expect fun createLedgerStore(): LedgerStore
