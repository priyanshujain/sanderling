package app.folio.feature.account

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import app.folio.core.data.Repository
import app.folio.navigation.Navigator
import app.folio.navigation.Route
import dev.zacsweers.metro.Inject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class AddAccountUiState(
    val name: String = "",
    val error: String = "",
)

sealed interface AddAccountEvent {
    data class NameChange(val value: String) : AddAccountEvent
    data object Submit : AddAccountEvent
    data object Back : AddAccountEvent
}

@Inject
class AddAccountViewModel(
    private val repository: Repository,
    private val navigator: Navigator,
) : ViewModel() {
    private val _state = MutableStateFlow(AddAccountUiState())
    val state: StateFlow<AddAccountUiState> = _state.asStateFlow()

    fun onEvent(event: AddAccountEvent) {
        when (event) {
            is AddAccountEvent.NameChange -> _state.update { it.copy(name = event.value, error = "") }
            AddAccountEvent.Back -> navigator.back(Route.Home)
            AddAccountEvent.Submit -> submit()
        }
    }

    private fun submit() {
        val trimmed = _state.value.name.trim()
        if (trimmed.isEmpty()) {
            _state.update { it.copy(error = "Account name is required") }
            return
        }
        if (trimmed.length > 40) {
            _state.update { it.copy(error = "Name is too long (max 40 characters)") }
            return
        }
        viewModelScope.launch {
            try {
                repository.createAccount(trimmed)
                navigator.replace(Route.Home)
            } catch (e: IllegalArgumentException) {
                _state.update { it.copy(error = e.message ?: "Could not create account") }
            }
        }
    }
}
