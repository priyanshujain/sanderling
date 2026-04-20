package app.folio.feature.ledger

import kotlinx.coroutines.flow.MutableStateFlow

object AddTransactionUiState {
    val txnError = MutableStateFlow("")
    val txnFormType = MutableStateFlow<String?>(null)
}
