package app.folio.feature.home

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
import kotlinx.coroutines.launch

data class HomeUiState(
    val user: String = "",
    val accounts: List<Account> = emptyList(),
    val transactions: List<Transaction> = emptyList(),
)

sealed interface HomeEvent {
    data object Logout : HomeEvent
    data object AddAccount : HomeEvent
    data class OpenAccount(val accountId: String) : HomeEvent
}

class HomeViewModel(
    private val repository: Repository,
    private val navigator: Navigator,
) : ViewModel() {
    val state: StateFlow<HomeUiState> = combine(
        repository.session,
        repository.accounts,
        repository.transactions,
    ) { session, accounts, transactions ->
        HomeUiState(
            user = session?.user ?: "",
            accounts = accounts,
            transactions = transactions,
        )
    }.stateIn(viewModelScope, SharingStarted.Eagerly, HomeUiState())

    fun onEvent(event: HomeEvent) {
        when (event) {
            HomeEvent.Logout -> viewModelScope.launch {
                repository.clearSession()
                navigator.replace(Route.Login)
            }
            HomeEvent.AddAccount -> navigator.push(Route.AddAccount)
            is HomeEvent.OpenAccount -> navigator.push(Route.Ledger(event.accountId))
        }
    }
}
