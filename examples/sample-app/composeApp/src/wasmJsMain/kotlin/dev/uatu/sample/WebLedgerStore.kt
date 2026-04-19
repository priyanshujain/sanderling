package dev.uatu.sample

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.browser.localStorage

private const val STORAGE_KEY = "uatu.ledger.v1"

class WebLedgerStore : LedgerStore {
    private val _accounts = MutableStateFlow<List<Account>>(emptyList())
    private val _transactions = MutableStateFlow<List<Transaction>>(emptyList())
    private val _session = MutableStateFlow<Session?>(null)

    override val accounts: StateFlow<List<Account>> = _accounts.asStateFlow()
    override val transactions: StateFlow<List<Transaction>> = _transactions.asStateFlow()
    override val session: StateFlow<Session?> = _session.asStateFlow()

    init {
        load()
    }

    override fun accountExistsByName(name: String): Boolean =
        _accounts.value.any { it.name == name }

    override fun insertAccount(id: String, name: String, createdAt: Long) {
        _accounts.value = _accounts.value + Account(id, name, createdAt)
        save()
    }

    override fun insertTxn(id: String, accountId: String, type: TxnType, amount: Long, note: String, createdAt: Long) {
        _transactions.value = _transactions.value + Transaction(id, accountId, type, amount, note, createdAt)
        save()
    }

    override fun upsertSession(user: String, loggedInAt: Long) {
        _session.value = Session(user, loggedInAt)
        save()
    }

    override fun clearSession() {
        _session.value = null
        save()
    }

    private fun load() {
        val raw = localStorage.getItem(STORAGE_KEY) ?: return
        val parsed = Snapshot.decode(raw) ?: return
        _accounts.value = parsed.accounts
        _transactions.value = parsed.transactions
        _session.value = parsed.session
    }

    private fun save() {
        val snap = Snapshot(_accounts.value, _transactions.value, _session.value)
        localStorage.setItem(STORAGE_KEY, snap.encode())
    }
}

actual fun createLedgerStore(): LedgerStore = WebLedgerStore()
