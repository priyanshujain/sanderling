package app.folio.ui.component

import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import app.folio.ui.theme.LocalTokens
import app.folio.ui.theme.Type

@Composable
fun FieldLabel(text: String) {
    val t = LocalTokens.current
    Text(text.uppercase(), style = Type.label, color = t.textMuted)
}
