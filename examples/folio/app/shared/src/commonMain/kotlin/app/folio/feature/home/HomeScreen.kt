package app.folio.feature.home

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
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
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import app.folio.di.LocalAppGraph
import app.folio.util.balanceOf
import app.folio.util.formatCents
import app.folio.util.initialsOf
import app.folio.util.signedAmount
import app.folio.ui.component.AppButton
import app.folio.ui.component.ButtonStyle
import app.folio.ui.component.EmptyState
import app.folio.ui.component.Header
import app.folio.ui.component.IconButton
import app.folio.ui.component.Screen
import app.folio.ui.icon.Icons
import app.folio.ui.theme.LocalTokens
import app.folio.ui.theme.RadiusLg
import app.folio.ui.theme.Type

@Composable
fun HomeRoute() {
    val graph = LocalAppGraph.current
    val vm: HomeViewModel = viewModel { graph.homeViewModel }
    val state by vm.state.collectAsState()
    HomeScreen(state = state, onEvent = vm::onEvent)
}

@Composable
fun HomeScreen(state: HomeUiState, onEvent: (HomeEvent) -> Unit) {
    val t = LocalTokens.current
    val total = balanceOf(state.transactions)

    Screen(
        modifier = Modifier.testTag("HomeScreen"),
        header = {
            Header(
                title = "Accounts",
                subtitle = state.user,
                right = {
                    IconButton(
                        onClick = { onEvent(HomeEvent.Logout) },
                        label = "Log out",
                        icon = Icons.Logout,
                        testTag = "LogoutButton",
                    )
                },
            )
        },
        footer = {
            Row(verticalAlignment = Alignment.Bottom) {
                Column {
                    Text("TOTAL BALANCE", style = Type.label, color = t.textMuted)
                    Text(formatCents(total), style = Type.balance, color = t.text)
                }
                Box(Modifier.weight(1f))
                Text(
                    "${state.accounts.size} account${if (state.accounts.size == 1) "" else "s"}",
                    style = Type.caption,
                    color = t.textFaint,
                )
            }
            AppButton(
                text = "+ Add account",
                onClick = { onEvent(HomeEvent.AddAccount) },
                style = ButtonStyle.Primary,
                testTag = "AddAccountButton",
            )
        },
    ) {
        if (state.accounts.isEmpty()) {
            EmptyState(
                title = "No accounts yet",
                subtitle = "Create your first account to start tracking transactions.",
                icon = Icons.Bank,
            )
        } else {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                state.accounts.forEach { a ->
                    val bal = state.transactions.filter { it.accountId == a.id }.sumOf { signedAmount(it) }
                    val count = state.transactions.count { it.accountId == a.id }
                    AccountCard(
                        name = a.name,
                        initials = initialsOf(a.name),
                        count = count,
                        balance = bal,
                        onClick = { onEvent(HomeEvent.OpenAccount(a.id)) },
                    )
                }
            }
        }
    }
}

@Composable
private fun AccountCard(
    name: String,
    initials: String,
    count: Int,
    balance: Long,
    onClick: () -> Unit,
) {
    val t = LocalTokens.current
    val txnLabel = if (count == 1) "1 transaction" else "$count transactions"
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(RadiusLg))
            .background(t.surface)
            .border(1.dp, t.border, RoundedCornerShape(RadiusLg))
            .testTag("AccountCard")
            .clickable(role = Role.Button, onClick = onClick)
            .padding(16.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Box(
            modifier = Modifier
                .size(40.dp)
                .clip(RoundedCornerShape(12.dp))
                .background(t.surface3),
            contentAlignment = Alignment.Center,
        ) {
            Text(initials, style = Type.bodyStrong, color = t.text)
        }
        Column(Modifier.weight(1f)) {
            Text(
                name,
                style = Type.bodyStrong,
                color = t.text,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(txnLabel, style = Type.caption, color = t.textMuted)
        }
        Text(
            formatCents(balance),
            style = Type.bodyStrong,
            color = t.text,
            modifier = Modifier.testTag("AccountBalance"),
        )
    }
}
