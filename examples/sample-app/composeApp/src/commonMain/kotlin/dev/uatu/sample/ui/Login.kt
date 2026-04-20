package dev.uatu.sample.ui

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
import dev.uatu.sample.DEMO_EMAIL
import dev.uatu.sample.DEMO_PASSWORD
import dev.uatu.sample.Repository
import dev.uatu.sample.UiState
import dev.uatu.sample.checkCredentials

@Composable
fun LoginPage(onLoggedIn: (String) -> Unit) {
    val t = LocalTokens.current
    var email by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    val err by UiState.loginError.collectAsState()

    DisposableEffect(Unit) {
        onDispose { UiState.loginError.value = "" }
    }

    fun submit() {
        if (email.isBlank() || password.isEmpty()) {
            UiState.loginError.value = "Enter email and password"; return
        }
        if (!checkCredentials(email, password)) {
            UiState.loginError.value = "Invalid email or password"; return
        }
        UiState.loginError.value = ""
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
                    onChange = { email = it; UiState.loginError.value = "" },
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
                    onChange = { password = it; UiState.loginError.value = "" },
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
