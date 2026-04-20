package app.folio.ui.component

import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import app.folio.ui.theme.LocalTokens
import app.folio.ui.theme.Type

@Composable
fun ErrorText(err: String?) {
    val t = LocalTokens.current
    Text(
        err ?: "",
        style = Type.caption,
        color = t.text,
        modifier = Modifier.fillMaxWidth().height(18.dp),
    )
}
