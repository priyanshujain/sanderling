package app.folio.feature.account

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import app.folio.di.LocalAppGraph
import app.folio.ui.BackHandler
import app.folio.ui.component.AppButton
import app.folio.ui.component.BackButton
import app.folio.ui.component.ButtonStyle
import app.folio.ui.component.ErrorText
import app.folio.ui.component.FieldLabel
import app.folio.ui.component.Header
import app.folio.ui.component.Screen
import app.folio.ui.component.TextInput
import app.folio.ui.theme.LocalTokens
import app.folio.ui.theme.Type

@Composable
fun AddAccountRoute() {
    val graph = LocalAppGraph.current
    val vm: AddAccountViewModel = viewModel { graph.addAccountViewModel }
    val state by vm.state.collectAsState()
    AddAccountScreen(state = state, onEvent = vm::onEvent)
}

@Composable
fun AddAccountScreen(state: AddAccountUiState, onEvent: (AddAccountEvent) -> Unit) {
    val t = LocalTokens.current
    BackHandler { onEvent(AddAccountEvent.Back) }

    Screen(
        modifier = Modifier.testTag("AddAccountScreen"),
        header = {
            Header(title = "New account", left = { BackButton(onClick = { onEvent(AddAccountEvent.Back) }) })
        },
        footer = {
            AppButton(
                text = "Create account",
                onClick = { onEvent(AddAccountEvent.Submit) },
                style = ButtonStyle.Primary,
                enabled = state.name.trim().isNotEmpty(),
                testTag = "AddAccountSubmit",
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
                    value = state.name,
                    onChange = { onEvent(AddAccountEvent.NameChange(it)) },
                    placeholder = "e.g. Checking",
                    invalid = state.error.isNotEmpty(),
                    label = "Account name",
                    testTag = "AccountNameField",
                )
            }
            ErrorText(state.error)
            Text(
                "Use a short, recognizable name. You can create as many accounts as you need.",
                style = Type.caption,
                color = t.textMuted,
            )
        }
    }
}
