package app.folio.feature.ledger

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import app.folio.core.data.Account
import app.folio.core.data.Repository
import app.folio.core.data.TxnType
import app.folio.navigation.Navigator
import app.folio.navigation.Route
import app.folio.util.parseCents
import dev.zacsweers.metro.Assisted
import dev.zacsweers.metro.AssistedFactory
import dev.zacsweers.metro.AssistedInject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

private val AMOUNT_REGEX = Regex("""^\d*(\.\d{0,2})?$""")

data class AddTransactionUiState(
    val account: Account? = null,
    val type: TxnType = TxnType.credit,
    val amount: String = "",
    val note: String = "",
    val error: String = "",
)

sealed interface AddTransactionEvent {
    data class TypeChange(val type: TxnType) : AddTransactionEvent
    data class AmountChange(val value: String) : AddTransactionEvent
    data class NoteChange(val value: String) : AddTransactionEvent
    data object Submit : AddTransactionEvent
    data object Back : AddTransactionEvent
    data object BackToHome : AddTransactionEvent
}

@AssistedInject
class AddTransactionViewModel(
    private val repository: Repository,
    private val navigator: Navigator,
    @Assisted private val accountId: String,
) : ViewModel() {
    private val form = MutableStateFlow(AddTransactionUiState())

    val state: StateFlow<AddTransactionUiState> = combine(
        form,
        repository.accounts.map { accounts -> accounts.firstOrNull { it.id == accountId } },
    ) { f, account -> f.copy(account = account) }
        .stateIn(viewModelScope, SharingStarted.Eagerly, AddTransactionUiState())

    fun onEvent(event: AddTransactionEvent) {
        when (event) {
            is AddTransactionEvent.TypeChange -> form.update { it.copy(type = event.type, error = "") }
            is AddTransactionEvent.AmountChange -> {
                val v = event.value
                if (v.isEmpty() || AMOUNT_REGEX.matches(v)) {
                    form.update { it.copy(amount = v, error = "") }
                }
            }
            is AddTransactionEvent.NoteChange -> form.update { it.copy(note = event.value.take(80), error = "") }
            AddTransactionEvent.Back -> navigator.back(Route.Ledger(accountId))
            AddTransactionEvent.BackToHome -> navigator.replace(Route.Home)
            AddTransactionEvent.Submit -> submit()
        }
    }

    private fun submit() {
        val s = form.value
        val cents = parseCents(s.amount)
        if (cents == null) {
            form.update { it.copy(error = "Enter a valid amount (e.g. 12.34)") }
            return
        }
        if (cents <= 0) {
            form.update { it.copy(error = "Amount must be greater than zero") }
            return
        }
        viewModelScope.launch {
            try {
                repository.createTransaction(accountId, s.type, cents, s.note)
                navigator.back(Route.Home)
            } catch (e: IllegalArgumentException) {
                form.update { it.copy(error = e.message ?: "Could not save transaction") }
            }
        }
    }

    @AssistedFactory
    fun interface Factory {
        fun create(accountId: String): AddTransactionViewModel
    }
}
