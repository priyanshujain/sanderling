package dev.uatu.sample.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import dev.uatu.sample.Navigator
import dev.uatu.sample.Repository
import dev.uatu.sample.Route

@Composable
fun AddAccountPage() {
    val t = LocalTokens.current
    var name by remember { mutableStateOf("") }
    var err by remember { mutableStateOf<String?>(null) }

    BackHandler { Navigator.back(Route.Home) }

    fun submit() {
        val trimmed = name.trim()
        if (trimmed.isEmpty()) {
            err = "Account name is required"; return
        }
        if (trimmed.length > 40) {
            err = "Name is too long (max 40 characters)"; return
        }
        try {
            Repository.createAccount(trimmed)
            Navigator.replace(Route.Home)
        } catch (e: IllegalArgumentException) {
            err = e.message ?: "Could not create account"
        }
    }

    Screen(
        header = {
            Header(title = "New account", left = { BackButton(onClick = { Navigator.back(Route.Home) }) })
        },
        footer = {
            AppButton(
                text = "Create account",
                onClick = ::submit,
                style = ButtonStyle.Primary,
                enabled = name.trim().isNotEmpty(),
                description = "add_account_submit",
            )
        },
    ) {
        Column(
            modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                FieldLabel("Account name")
                TextInput(
                    value = name,
                    onChange = { name = it; err = null },
                    placeholder = "e.g. Checking",
                    invalid = err != null,
                    label = "Account name",
                    description = "account_name_field",
                )
            }
            ErrorText(err)
            Text(
                "Use a short, recognizable name. You can create as many accounts as you need.",
                style = Type.caption,
                color = t.textMuted,
            )
        }
    }
}
