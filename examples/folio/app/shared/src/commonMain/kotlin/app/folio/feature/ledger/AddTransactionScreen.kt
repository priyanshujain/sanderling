package app.folio.feature.ledger

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import app.folio.core.data.TxnType
import app.folio.di.LocalAppGraph
import app.folio.ui.BackHandler
import app.folio.ui.component.AppButton
import app.folio.ui.component.BackButton
import app.folio.ui.component.ButtonStyle
import app.folio.ui.component.EmptyState
import app.folio.ui.component.ErrorText
import app.folio.ui.component.FieldLabel
import app.folio.ui.component.Header
import app.folio.ui.component.Screen
import app.folio.ui.component.Segmented
import app.folio.ui.component.TextInput
import app.folio.ui.theme.Type

@Composable
fun AddTransactionRoute(accountId: String) {
    val graph = LocalAppGraph.current
    val vm: AddTransactionViewModel = viewModel(key = "addTxn:$accountId") {
        graph.addTransactionViewModelFactory.create(accountId)
    }
    val state by vm.state.collectAsState()
    AddTransactionScreen(state = state, onEvent = vm::onEvent)
}

@Composable
fun AddTransactionScreen(state: AddTransactionUiState, onEvent: (AddTransactionEvent) -> Unit) {
    BackHandler { onEvent(AddTransactionEvent.Back) }

    val account = state.account
    if (account == null) {
        Screen(
            header = {
                Header(title = "Add transaction", left = { BackButton { onEvent(AddTransactionEvent.BackToHome) } })
            },
        ) {
            EmptyState(title = "Account not found", subtitle = "")
            AppButton(
                text = "Back to accounts",
                onClick = { onEvent(AddTransactionEvent.BackToHome) },
                style = ButtonStyle.Secondary,
            )
        }
        return
    }

    Screen(
        modifier = Modifier.testTag("AddTransactionScreen"),
        header = {
            Header(
                title = "Add transaction",
                subtitle = account.name,
                left = { BackButton { onEvent(AddTransactionEvent.Back) } },
            )
        },
        footer = {
            AppButton(
                text = if (state.type == TxnType.credit) "Add credit" else "Add debit",
                onClick = { onEvent(AddTransactionEvent.Submit) },
                style = ButtonStyle.Primary,
                enabled = state.amount.isNotBlank(),
                testTag = "TxnSubmit",
            )
        },
    ) {
        Column(
            modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Segmented(
                selected = if (state.type == TxnType.credit) 0 else 1,
                labels = listOf("Credit", "Debit"),
                onSelect = {
                    onEvent(AddTransactionEvent.TypeChange(if (it == 0) TxnType.credit else TxnType.debit))
                },
                testTags = listOf("TxnTypeCredit", "TxnTypeDebit"),
            )
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                FieldLabel("Amount")
                TextInput(
                    value = state.amount,
                    onChange = { onEvent(AddTransactionEvent.AmountChange(it)) },
                    placeholder = "0.00",
                    invalid = state.error.isNotEmpty(),
                    keyboardType = KeyboardType.Decimal,
                    textAlign = TextAlign.Center,
                    textStyle = Type.amountInput,
                    label = "Amount",
                    testTag = "TxnAmountField",
                )
            }
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                FieldLabel("Note (optional)")
                TextInput(
                    value = state.note,
                    onChange = { onEvent(AddTransactionEvent.NoteChange(it)) },
                    placeholder = "What's this for?",
                    label = "Note",
                    testTag = "TxnNoteField",
                )
            }
            ErrorText(state.error)
        }
    }
}
