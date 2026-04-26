package app.folio.feature.auth

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

const val DEMO_EMAIL = "demo@folio.app"
const val DEMO_PASSWORD = "ledger123"

data class LoginUiState(
    val email: String = "",
    val password: String = "",
    val error: String = "",
)

sealed interface LoginEvent {
    data class EmailChange(val value: String) : LoginEvent
    data class PasswordChange(val value: String) : LoginEvent
    data object Submit : LoginEvent
}

@Inject
class LoginViewModel(
    private val repository: Repository,
    private val navigator: Navigator,
) : ViewModel() {
    private val _state = MutableStateFlow(LoginUiState())
    val state: StateFlow<LoginUiState> = _state.asStateFlow()

    fun onEvent(event: LoginEvent) {
        when (event) {
            is LoginEvent.EmailChange -> _state.update { it.copy(email = event.value, error = "") }
            is LoginEvent.PasswordChange -> _state.update { it.copy(password = event.value, error = "") }
            LoginEvent.Submit -> submit()
        }
    }

    private fun submit() {
        val s = _state.value
        if (s.email.isBlank() || s.password.isEmpty()) {
            _state.update { it.copy(error = "Enter email and password") }
            return
        }
        if (!checkCredentials(s.email, s.password)) {
            _state.update { it.copy(error = "Invalid email or password") }
            return
        }
        val user = s.email.trim().lowercase()
        viewModelScope.launch {
            repository.setSession(user)
            navigator.replace(Route.Home)
        }
    }

    private fun checkCredentials(email: String, password: String): Boolean =
        email.trim().lowercase() == DEMO_EMAIL && password == DEMO_PASSWORD
}
