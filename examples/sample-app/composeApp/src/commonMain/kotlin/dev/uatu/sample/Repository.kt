package dev.uatu.sample

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

@Serializable
private data class PersistedState(
    val accounts: List<Account> = emptyList(),
    val transactions: List<Transaction> = emptyList(),
    val session: Session? = null,
)

object Repository {
    private const val FILE = "state.json"
    private val json = Json { ignoreUnknownKeys = true; prettyPrint = false }

    private val _accounts = MutableStateFlow<List<Account>>(emptyList())
    val accounts: StateFlow<List<Account>> = _accounts.asStateFlow()

    private val _transactions = MutableStateFlow<List<Transaction>>(emptyList())
    val transactions: StateFlow<List<Transaction>> = _transactions.asStateFlow()

    private val _session = MutableStateFlow<Session?>(null)
    val session: StateFlow<Session?> = _session.asStateFlow()

    @Volatile private var loaded = false

    fun load() {
        if (loaded) return
        loaded = true
        val raw = Platform.readFile(FILE) ?: return
        val state = runCatching { json.decodeFromString<PersistedState>(raw) }.getOrNull() ?: return
        _accounts.value = state.accounts.sortedBy { it.createdAt }
        _transactions.value = state.transactions
        _session.value = state.session
    }

    private fun persist() {
        val state = PersistedState(
            accounts = _accounts.value,
            transactions = _transactions.value,
            session = _session.value,
        )
        Platform.writeFile(FILE, json.encodeToString(PersistedState.serializer(), state))
    }

    fun accountNameExists(name: String): Boolean =
        _accounts.value.any { it.name.equals(name, ignoreCase = true) }

    fun createAccount(name: String): Account {
        val trimmed = name.trim()
        require(trimmed.isNotEmpty()) { "Name is required" }
        require(trimmed.length <= 40) { "Name is too long (max 40)" }
        require(!accountNameExists(trimmed)) { "An account with that name already exists" }
        val account = Account(id = Platform.makeId(), name = trimmed, createdAt = Platform.now())
        _accounts.value = _accounts.value + account
        persist()
        return account
    }

    fun getAccount(id: String): Account? = _accounts.value.firstOrNull { it.id == id }

    fun transactionsFor(accountId: String): List<Transaction> =
        _transactions.value.filter { it.accountId == accountId }.sortedByDescending { it.createdAt }

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
        _transactions.value = _transactions.value + txn
        persist()
        return txn
    }

    fun setSession(user: String): Session {
        val s = Session(user = user, loggedInAt = Platform.now())
        _session.value = s
        persist()
        return s
    }

    fun clearSession() {
        _session.value = null
        persist()
    }
}
