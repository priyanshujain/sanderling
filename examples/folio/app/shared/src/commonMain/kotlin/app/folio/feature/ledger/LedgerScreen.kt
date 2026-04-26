package app.folio.feature.ledger

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import app.folio.core.data.TxnType
import app.folio.di.LocalAppGraph
import app.folio.util.balanceOf
import app.folio.util.formatCents
import app.folio.util.formatDate
import app.folio.ui.BackHandler
import app.folio.ui.component.AppButton
import app.folio.ui.component.BackButton
import app.folio.ui.component.ButtonStyle
import app.folio.ui.component.Card
import app.folio.ui.component.EmptyState
import app.folio.ui.component.Header
import app.folio.ui.component.Screen
import app.folio.ui.icon.Icons
import app.folio.ui.theme.LocalTokens
import app.folio.ui.theme.RadiusLg
import app.folio.ui.theme.Type

@Composable
fun LedgerRoute(accountId: String) {
    val graph = LocalAppGraph.current
    val vm: LedgerViewModel = viewModel(key = "ledger:$accountId") {
        graph.ledgerViewModelFactory.create(accountId)
    }
    val state by vm.state.collectAsState()
    LedgerScreen(state = state, onEvent = vm::onEvent)
}

@Composable
fun LedgerScreen(state: LedgerUiState, onEvent: (LedgerEvent) -> Unit) {
    val t = LocalTokens.current

    BackHandler { onEvent(LedgerEvent.Back) }

    val account = state.account
    if (account == null) {
        Screen(
            header = {
                Header(title = "Account", left = { BackButton { onEvent(LedgerEvent.Back) } })
            },
        ) {
            EmptyState(title = "Account not found", subtitle = "It may have been deleted.")
            AppButton(
                text = "Back to accounts",
                onClick = { onEvent(LedgerEvent.BackToHome) },
                style = ButtonStyle.Secondary,
            )
        }
        return
    }

    val balance = balanceOf(state.transactions)

    Screen(
        modifier = Modifier.testTag("LedgerScreen"),
        header = {
            Header(
                title = account.name,
                subtitle = "Ledger",
                left = { BackButton { onEvent(LedgerEvent.Back) } },
            )
        },
        footer = {
            AppButton(
                text = "+ Add transaction",
                onClick = { onEvent(LedgerEvent.AddTransaction) },
                style = ButtonStyle.Primary,
                testTag = "AddTransactionButton",
            )
        },
    ) {
        Card {
            Text("BALANCE", style = Type.label, color = t.textMuted)
            Text(
                formatCents(balance),
                style = Type.balance,
                color = t.text,
                modifier = Modifier.testTag("LedgerBalance"),
            )
        }
        Text(
            "ACTIVITY",
            style = Type.label,
            color = t.textFaint,
            modifier = Modifier.padding(top = 4.dp, bottom = 2.dp, start = 4.dp),
        )
        if (state.transactions.isEmpty()) {
            EmptyState(
                title = "No transactions yet",
                subtitle = "Add your first credit or debit to see it here.",
                icon = Icons.Lines,
            )
        } else {
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(RadiusLg))
                    .background(t.surface)
                    .border(1.dp, t.border, RoundedCornerShape(RadiusLg))
                    .padding(horizontal = 16.dp),
            ) {
                state.transactions.forEachIndexed { i, txn ->
                    TxnRow(txn.type, txn.amount, txn.note, formatDate(txn.createdAt))
                    if (i != state.transactions.lastIndex) {
                        Box(Modifier.fillMaxWidth().height(1.dp).background(t.border))
                    }
                }
            }
        }
    }
}

@Composable
private fun TxnRow(type: TxnType, amount: Long, note: String, date: String) {
    val t = LocalTokens.current
    val signed = if (type == TxnType.credit) amount else -amount
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 14.dp)
            .testTag("LedgerRow"),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Box(
            modifier = Modifier
                .size(36.dp)
                .clip(RoundedCornerShape(10.dp))
                .background(t.surface2)
                .border(1.dp, t.border, RoundedCornerShape(10.dp)),
            contentAlignment = Alignment.Center,
        ) {
            Text(if (type == TxnType.credit) "+" else "-", style = Type.bodyStrong, color = t.text)
        }
        Column(Modifier.weight(1f)) {
            Text(
                if (note.isNotEmpty()) note else if (type == TxnType.credit) "Credit" else "Debit",
                style = Type.body,
                color = t.text,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(date, style = Type.caption, color = t.textFaint)
        }
        Text(
            formatCents(signed, signed = true),
            style = Type.bodyStrong,
            color = t.text,
            modifier = Modifier.testTag("TxnAmount"),
        )
    }
}
