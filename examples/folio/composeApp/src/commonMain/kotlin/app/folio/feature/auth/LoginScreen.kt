package app.folio.feature.auth

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
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
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import app.folio.data.Repository
import app.folio.ui.AppButton
import app.folio.ui.ButtonStyle
import app.folio.ui.Card
import app.folio.ui.ErrorText
import app.folio.ui.FieldLabel
import app.folio.ui.Screen
import app.folio.ui.TextInput
import app.folio.ui.theme.LocalTokens
import app.folio.ui.theme.Type

const val DEMO_EMAIL = "demo@folio.app"
const val DEMO_PASSWORD = "ledger123"

private fun checkCredentials(email: String, password: String): Boolean {
    return email.trim().lowercase() == DEMO_EMAIL && password == DEMO_PASSWORD
}

@Composable
fun LoginScreen(onLoggedIn: (String) -> Unit) {
    val t = LocalTokens.current
    var email by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    val err by LoginUiState.loginError.collectAsState()

    DisposableEffect(Unit) {
        onDispose { LoginUiState.loginError.value = "" }
    }

    fun submit() {
        if (email.isBlank() || password.isEmpty()) {
            LoginUiState.loginError.value = "Enter email and password"; return
        }
        if (!checkCredentials(email, password)) {
            LoginUiState.loginError.value = "Invalid email or password"; return
        }
        LoginUiState.loginError.value = ""
        val user = email.trim().lowercase()
        Repository.setSession(user)
        onLoggedIn(user)
    }

    Screen {
        Spacer(Modifier.height(16.dp))
        Column(
            modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                FieldLabel("Email")
                TextInput(
                    value = email,
                    onChange = { email = it; LoginUiState.loginError.value = "" },
                    placeholder = DEMO_EMAIL,
                    invalid = err.isNotEmpty(),
                    keyboardType = KeyboardType.Email,
                    label = "Email",
                    description = "login_email",
                )
            }
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                FieldLabel("Password")
                TextInput(
                    value = password,
                    onChange = { password = it; LoginUiState.loginError.value = "" },
                    placeholder = "••••••••",
                    password = true,
                    invalid = err.isNotEmpty(),
                    keyboardType = KeyboardType.Password,
                    label = "Password",
                    description = "login_password",
                )
            }
            ErrorText(err)
            AppButton(
                text = "Sign in",
                onClick = ::submit,
                style = ButtonStyle.Primary,
                description = "login_submit",
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
