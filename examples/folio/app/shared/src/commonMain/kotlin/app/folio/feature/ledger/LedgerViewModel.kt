package app.folio.feature.ledger

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import app.folio.core.data.Account
import app.folio.core.data.Repository
import app.folio.core.data.Transaction
import app.folio.navigation.Navigator
import app.folio.navigation.Route
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn

data class LedgerUiState(
    val account: Account? = null,
    val transactions: List<Transaction> = emptyList(),
    val accountId: String = "",
)

sealed interface LedgerEvent {
    data object Back : LedgerEvent
    data object AddTransaction : LedgerEvent
    data object BackToHome : LedgerEvent
}

class LedgerViewModel(
    private val repository: Repository,
    private val navigator: Navigator,
    private val accountId: String,
) : ViewModel() {
    val state: StateFlow<LedgerUiState> = combine(
        repository.accounts,
        repository.transactions,
    ) { accounts, txns ->
        LedgerUiState(
            account = accounts.firstOrNull { it.id == accountId },
            transactions = txns
                .filter { it.accountId == accountId }
                .sortedByDescending { it.createdAt },
            accountId = accountId,
        )
    }.stateIn(viewModelScope, SharingStarted.Eagerly, LedgerUiState(accountId = accountId))

    fun onEvent(event: LedgerEvent) {
        when (event) {
            LedgerEvent.Back -> navigator.back(Route.Home)
            LedgerEvent.AddTransaction -> navigator.push(Route.AddTransaction(accountId))
            LedgerEvent.BackToHome -> navigator.replace(Route.Home)
        }
    }
}
