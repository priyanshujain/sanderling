package app.folio

import kotlinx.coroutines.flow.MutableStateFlow

object UiState {
    val loginError = MutableStateFlow("")
    val addAccountError = MutableStateFlow("")
    val txnError = MutableStateFlow("")
    val txnFormType = MutableStateFlow<String?>(null)
}
