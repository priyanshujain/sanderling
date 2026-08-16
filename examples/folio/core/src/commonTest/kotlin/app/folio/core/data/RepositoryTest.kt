package app.folio.core.data

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

private class FakeLedgerStore : LedgerStore {
    override val accounts = MutableStateFlow<List<Account>>(emptyList())
    override val transactions = MutableStateFlow<List<Transaction>>(emptyList())
    override val session = MutableStateFlow<Session?>(null)

    override suspend fun accountExistsByName(name: String): Boolean =
        accounts.value.any { it.name == name }

    override suspend fun insertAccount(id: String, name: String, createdAt: Long) {
        accounts.value = accounts.value + Account(id, name, createdAt)
    }

    override suspend fun insertTxn(
        id: String,
        accountId: String,
        type: TxnType,
        amount: Long,
        note: String,
        createdAt: Long,
    ) {
        transactions.value = transactions.value + Transaction(id, accountId, type, amount, note, createdAt)
    }

    override suspend fun upsertSession(user: String, loggedInAt: Long) {
        session.value = Session(user, loggedInAt)
    }

    override suspend fun clearSession() {
        session.value = null
    }
}

class RepositoryTest {
    @Test
    fun rejectsAmountAboveOneMillionDollars() = runTest {
        val repository = Repository(FakeLedgerStore())
        val account = repository.createAccount("Checking")

        assertFailsWith<IllegalArgumentException> {
            repository.createTransaction(account.id, TxnType.credit, 100_000_001L, "")
        }
        assertFailsWith<IllegalArgumentException> {
            repository.createTransaction(account.id, TxnType.credit, Long.MAX_VALUE, "")
        }
        assertTrue(repository.transactions.value.isEmpty())
    }

    @Test
    fun acceptsAmountAtOneMillionDollars() = runTest {
        val repository = Repository(FakeLedgerStore())
        val account = repository.createAccount("Checking")

        repository.createTransaction(account.id, TxnType.credit, 100_000_000L, "rent")

        assertEquals(listOf(100_000_000L), repository.transactions.value.map { it.amount })
    }
}
