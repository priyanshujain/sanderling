package app.folio.feature.account

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import app.folio.data.Repository
import app.folio.navigation.Navigator
import app.folio.navigation.Route
import app.folio.ui.AppButton
import app.folio.ui.BackButton
import app.folio.ui.BackHandler
import app.folio.ui.ButtonStyle
import app.folio.ui.ErrorText
import app.folio.ui.FieldLabel
import app.folio.ui.Header
import app.folio.ui.Screen
import app.folio.ui.TextInput
import app.folio.ui.theme.LocalTokens
import app.folio.ui.theme.Type

@Composable
fun AddAccountScreen() {
    val t = LocalTokens.current
    var name by remember { mutableStateOf("") }
    val err by AddAccountUiState.addAccountError.collectAsState()

    BackHandler { Navigator.back(Route.Home) }

    DisposableEffect(Unit) {
        onDispose { AddAccountUiState.addAccountError.value = "" }
    }

    fun submit() {
        val trimmed = name.trim()
        if (trimmed.isEmpty()) {
            AddAccountUiState.addAccountError.value = "Account name is required"; return
        }
        if (trimmed.length > 40) {
            AddAccountUiState.addAccountError.value = "Name is too long (max 40 characters)"; return
        }
        try {
            Repository.createAccount(trimmed)
            AddAccountUiState.addAccountError.value = ""
            Navigator.replace(Route.Home)
        } catch (e: IllegalArgumentException) {
            AddAccountUiState.addAccountError.value = e.message ?: "Could not create account"
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
                    onChange = { name = it; AddAccountUiState.addAccountError.value = "" },
                    placeholder = "e.g. Checking",
                    invalid = err.isNotEmpty(),
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
