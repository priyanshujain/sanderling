package dev.uatu.sample.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import dev.uatu.sample.Navigator
import dev.uatu.sample.Repository
import dev.uatu.sample.Route
import dev.uatu.sample.TxnType
import dev.uatu.sample.UiState
import dev.uatu.sample.parseCents

private val AMOUNT_REGEX = Regex("""^\d*(\.\d{0,2})?$""")

@Composable
fun AddTransactionPage(accountId: String) {
    val accounts by Repository.accounts.collectAsState()
    val account = accounts.firstOrNull { it.id == accountId }

    BackHandler { Navigator.back(Route.Ledger(accountId)) }

    if (account == null) {
        Screen(
            header = {
                Header(title = "Add transaction", left = { BackButton { Navigator.back(Route.Home) } })
            },
        ) {
            EmptyState(title = "Account not found", subtitle = "")
            AppButton(
                text = "Back to accounts",
                onClick = { Navigator.replace(Route.Home) },
                style = ButtonStyle.Secondary,
            )
        }
        return
    }

    var type by remember { mutableStateOf(TxnType.credit) }
    var amount by remember { mutableStateOf("") }
    var note by remember { mutableStateOf("") }
    val err by UiState.txnError.collectAsState()

    DisposableEffect(type) {
        UiState.txnFormType.value = if (type == TxnType.credit) "credit" else "debit"
        onDispose { UiState.txnFormType.value = null }
    }

    DisposableEffect(Unit) {
        onDispose { UiState.txnError.value = "" }
    }

    fun submit() {
        val cents = parseCents(amount)
        if (cents == null) {
            UiState.txnError.value = "Enter a valid amount (e.g. 12.34)"; return
        }
        if (cents <= 0) {
            UiState.txnError.value = "Amount must be greater than zero"; return
        }
        try {
            Repository.createTransaction(accountId, type, cents, note)
            UiState.txnError.value = ""
            Navigator.back(Route.Home)
        } catch (e: IllegalArgumentException) {
            UiState.txnError.value = e.message ?: "Could not save transaction"
        }
    }

    Screen(
        header = {
            Header(
                title = "Add transaction",
                subtitle = account.name,
                left = { BackButton { Navigator.back(Route.Ledger(accountId)) } },
            )
        },
        footer = {
            AppButton(
                text = if (type == TxnType.credit) "Add credit" else "Add debit",
                onClick = ::submit,
                style = ButtonStyle.Primary,
                enabled = amount.isNotBlank(),
                description = "txn_submit",
            )
        },
    ) {
        Column(
            modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Segmented(
                selected = if (type == TxnType.credit) 0 else 1,
                labels = listOf("Credit", "Debit"),
                onSelect = { type = if (it == 0) TxnType.credit else TxnType.debit },
                descriptions = listOf("txn_credit", "txn_debit"),
            )
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                FieldLabel("Amount")
                TextInput(
                    value = amount,
                    onChange = {
                        if (AMOUNT_REGEX.matches(it) || it.isEmpty()) {
                            amount = it
                            UiState.txnError.value = ""
                        }
                    },
                    placeholder = "0.00",
                    invalid = err.isNotEmpty(),
                    keyboardType = KeyboardType.Decimal,
                    textAlign = TextAlign.Center,
                    textStyle = Type.amountInput,
                    label = "Amount",
                    description = "txn_amount",
                )
            }
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                FieldLabel("Note (optional)")
                TextInput(
                    value = note,
                    onChange = { note = it.take(80) },
                    placeholder = "What's this for?",
                    label = "Note",
                    description = "txn_note",
                )
            }
            ErrorText(err)
        }
    }
}
