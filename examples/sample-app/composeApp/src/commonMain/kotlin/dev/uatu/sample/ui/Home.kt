package dev.uatu.sample.ui

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
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.runtime.collectAsState
import dev.uatu.sample.Navigator
import dev.uatu.sample.Repository
import dev.uatu.sample.Route
import dev.uatu.sample.balanceOf
import dev.uatu.sample.formatCents
import dev.uatu.sample.initialsOf
import dev.uatu.sample.signedAmount

@Composable
fun HomePage(user: String, onLogout: () -> Unit) {
    val t = LocalTokens.current
    val accounts by Repository.accounts.collectAsState()
    val txns by Repository.transactions.collectAsState()
    val total = balanceOf(txns)

    Screen(
        header = {
            Header(
                title = "Accounts",
                subtitle = user,
                right = {
                    IconButton(onClick = onLogout, description = "Sign out", icon = Icons.Logout)
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
                    "${accounts.size} account${if (accounts.size == 1) "" else "s"}",
                    style = Type.caption,
                    color = t.textFaint,
                )
            }
            AppButton(
                text = "+ Add account",
                onClick = { Navigator.push(Route.AddAccount) },
                style = ButtonStyle.Primary,
            )
        },
    ) {
        if (accounts.isEmpty()) {
            EmptyState(
                title = "No accounts yet",
                subtitle = "Create your first account to start tracking transactions.",
                icon = Icons.Bank,
            )
        } else {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                accounts.forEach { a ->
                    val bal = txns.filter { it.accountId == a.id }.sumOf { signedAmount(it) }
                    val count = txns.count { it.accountId == a.id }
                    AccountCard(
                        name = a.name,
                        initials = initialsOf(a.name),
                        count = count,
                        balance = bal,
                        onClick = { Navigator.push(Route.Ledger(a.id)) },
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
    val a11y = "$name account, balance ${formatCents(balance)}, $txnLabel"
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(RadiusLg))
            .background(t.surface)
            .border(1.dp, t.border, RoundedCornerShape(RadiusLg))
            .semantics(mergeDescendants = true) { contentDescription = a11y }
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
        Text(formatCents(balance), style = Type.bodyStrong, color = t.text)
    }
}
