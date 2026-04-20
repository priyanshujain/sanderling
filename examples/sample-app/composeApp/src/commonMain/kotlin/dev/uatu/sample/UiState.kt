package dev.uatu.sample

import kotlinx.coroutines.flow.MutableStateFlow

object UiState {
    val loginError = MutableStateFlow("")
    val addAccountError = MutableStateFlow("")
    val txnError = MutableStateFlow("")
    val txnFormType = MutableStateFlow<String?>(null)
    val loginEmail = MutableStateFlow("")
    val loginPasswordLength = MutableStateFlow(0)
    val accountNameInput = MutableStateFlow("")
    val txnAmountInput = MutableStateFlow("")
}
