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
import androidx.compose.runtime.getValue
import androidx.compose.runtime.collectAsState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import app.folio.data.Repository
import app.folio.navigation.Navigator
import app.folio.navigation.Route
import app.folio.data.TxnType
import app.folio.platform.balanceOf
import app.folio.platform.formatCents
import app.folio.platform.formatDate
import app.folio.platform.signedAmount
import app.folio.ui.AppButton
import app.folio.ui.BackButton
import app.folio.ui.BackHandler
import app.folio.ui.ButtonStyle
import app.folio.ui.Card
import app.folio.ui.EmptyState
import app.folio.ui.Header
import app.folio.ui.Icons
import app.folio.ui.LocalTokens
import app.folio.ui.RadiusLg
import app.folio.ui.Screen
import app.folio.ui.Type

@Composable
fun LedgerScreen(accountId: String) {
    val t = LocalTokens.current
    val accounts by Repository.accounts.collectAsState()
    val allTxns by Repository.transactions.collectAsState()

    BackHandler { Navigator.back(Route.Home) }

    val account = accounts.firstOrNull { it.id == accountId }
    if (account == null) {
        Screen(
            header = {
                Header(title = "Account", left = { BackButton { Navigator.back(Route.Home) } })
            },
        ) {
            EmptyState(title = "Account not found", subtitle = "It may have been deleted.")
            AppButton(
                text = "Back to accounts",
                onClick = { Navigator.replace(Route.Home) },
                style = ButtonStyle.Secondary,
            )
        }
        return
    }

    val txns = allTxns
        .filter { it.accountId == accountId }
        .sortedByDescending { it.createdAt }
    val balance = balanceOf(txns)

    Screen(
        header = {
            Header(
                title = account.name,
                subtitle = "Ledger",
                left = { BackButton { Navigator.back(Route.Home) } },
            )
        },
        footer = {
            AppButton(
                text = "+ Add transaction",
                onClick = { Navigator.push(Route.AddTransaction(accountId)) },
                style = ButtonStyle.Primary,
                description = "add_txn_button",
            )
        },
    ) {
        Card {
            Text("BALANCE", style = Type.label, color = t.textMuted)
            Text(formatCents(balance), style = Type.balance, color = t.text)
        }
        Text(
            "ACTIVITY",
            style = Type.label,
            color = t.textFaint,
            modifier = Modifier.padding(top = 4.dp, bottom = 2.dp, start = 4.dp),
        )
        if (txns.isEmpty()) {
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
                txns.forEachIndexed { i, txn ->
                    TxnRow(txn.id, txn.type, txn.amount, txn.note, formatDate(txn.createdAt))
                    if (i != txns.lastIndex) {
                        Box(Modifier.fillMaxWidth().height(1.dp).background(t.border))
                    }
                }
            }
        }
    }
}

@Composable
private fun TxnRow(id: String, type: TxnType, amount: Long, note: String, date: String) {
    val t = LocalTokens.current
    val signed = if (type == TxnType.credit) amount else -amount
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 14.dp)
            .semantics(mergeDescendants = true) { contentDescription = "txn_row:$id" },
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
        )
    }
}
