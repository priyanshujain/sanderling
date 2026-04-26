package app.folio.feature.auth

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import app.folio.di.LocalAppGraph
import app.folio.ui.component.AppButton
import app.folio.ui.component.ButtonStyle
import app.folio.ui.component.Card
import app.folio.ui.component.ErrorText
import app.folio.ui.component.FieldLabel
import app.folio.ui.component.Screen
import app.folio.ui.component.TextInput
import app.folio.ui.theme.LocalTokens
import app.folio.ui.theme.Type

@Composable
fun LoginRoute() {
    val graph = LocalAppGraph.current
    val vm: LoginViewModel = viewModel { graph.loginViewModel }
    val state by vm.state.collectAsState()
    LoginScreen(state = state, onEvent = vm::onEvent)
}

@Composable
fun LoginScreen(state: LoginUiState, onEvent: (LoginEvent) -> Unit) {
    val t = LocalTokens.current
    Screen(modifier = Modifier.testTag("LoginScreen")) {
        Spacer(Modifier.height(16.dp))
        Column(
            modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                FieldLabel("Email")
                TextInput(
                    value = state.email,
                    onChange = { onEvent(LoginEvent.EmailChange(it)) },
                    placeholder = DEMO_EMAIL,
                    invalid = state.error.isNotEmpty(),
                    keyboardType = KeyboardType.Email,
                    label = "Email",
                    testTag = "LoginEmail",
                )
            }
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                FieldLabel("Password")
                TextInput(
                    value = state.password,
                    onChange = { onEvent(LoginEvent.PasswordChange(it)) },
                    placeholder = "••••••••",
                    password = true,
                    invalid = state.error.isNotEmpty(),
                    keyboardType = KeyboardType.Password,
                    label = "Password",
                    testTag = "LoginPassword",
                )
            }
            ErrorText(state.error)
            AppButton(
                text = "Sign in",
                onClick = { onEvent(LoginEvent.Submit) },
                style = ButtonStyle.Primary,
                testTag = "LoginSubmit",
            )
            Spacer(Modifier.height(4.dp))
            Card(dashed = true) {
                Text("Demo credentials", style = Type.caption, color = t.textMuted)
                Text("email: $DEMO_EMAIL", style = Type.body, color = t.text)
                Text("password: $DEMO_PASSWORD", style = Type.body, color = t.text)
            }
        }
    }
}
